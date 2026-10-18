package ui

import (
	"context"
	"errors"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
)

// YouTube Music en Buscar. Solo si el usuario puso su propio yt-dlp junto a
// audiofilus (ytm.Find): al terminar de escribir se busca también allí y los
// resultados salen debajo de los de la biblioteca, marcados si probablemente
// ya está la canción (por su youtube_id en .library.json de ytm-gym o por
// artista y título). A o Start abren su menú: escucharla sin guardarla (se
// baja a una caché en /tmp y suena), descargarla a la biblioteca con la
// estructura de ytm-gym (y la biblioteca se pone al día sola) o, si ya está,
// poner la propia.
//
// yt-dlp tarda (en la consola, ~15 s en sacar la URL y, al descargar, otro
// tanto en pasar a MP3), así que todo es un trabajo con su estado: una barra
// sobre la ayuda de abajo dice qué se está haciendo y cuánto va, un icono
// arriba cuenta las descargas, y el panel de Descargas las lista. Escuchar va
// aparte y enseguida (si se pide otra, la anterior se cancela); las descargas,
// en cola y de una en una, para no ahogar la consola.

// ytCacheKeep son las canciones escuchadas que se guardan en la caché.
const ytCacheKeep = 15

// ytCacheDir es la caché de lo que se escucha sin guardar.
func ytCacheDir() string { return filepath.Join(os.TempDir(), "audiofilus-ytm") }

// streamTrack es la canción de YouTube Music que suena sin estar guardada (la
// de la caché), si es eso lo que suena.
func (a *App) streamTrack() (ytm.Track, bool) {
	p := a.playingSong.Path
	if p == "" || filepath.Dir(p) != ytCacheDir() || !strings.HasSuffix(p, ".m4a") {
		return ytm.Track{}, false
	}
	t := ytm.Track{ID: strings.TrimSuffix(filepath.Base(p), ".m4a"), Title: a.playingSong.Title, Album: a.playingSong.Album}
	if a.playingSong.Artist != "" {
		t.Artists = strings.Split(a.playingSong.Artist, ", ")
	}
	return t, true
}

// streamSavable es la canción en streaming si aún no se guardó ni se está
// guardando.
func (a *App) streamSavable() (ytm.Track, bool) {
	t, ok := a.streamTrack()
	if !ok || a.yt.ids[t.ID] {
		return t, false
	}
	for _, j := range a.yt.jobs {
		if !j.listen && j.track.ID == t.ID && j.state != jobError {
			return t, false
		}
	}
	return t, true
}

// Estados de un trabajo de YouTube Music.
const (
	jobWaiting  = iota // en la cola de descargas
	jobResolve         // yt-dlp sacando la URL
	jobDownload        // bajando (done/total)
	jobConvert         // pasando a MP3, la portada...
	jobDone
	jobError
)

// ytJob es escuchar o descargar una canción.
type ytJob struct {
	track  ytm.Track
	listen bool
	remote bool // de un servidor (si no, de YouTube Music)
	// run hace el trabajo (las descargas de un servidor); si es nil, es una
	// descarga de YouTube Music.
	run         func(ctx context.Context, progress func(ytm.Progress)) (string, error)
	podcast     bool              // un episodio de podcast (icono y sin reescanear la biblioteca)
	after       func(path string) // lo que se hace al terminar bien (poner el episodio)
	state       int
	done, total int64
	path        string
	err         error
}

// active dice si el trabajo aún no terminó.
func (j *ytJob) active() bool { return j.state < jobDone }

// ytEvent es un aviso de un trabajo, desde su goroutine.
type ytEvent struct {
	job  *ytJob
	p    ytm.Progress
	fin  bool
	path string
	err  error
}

// ytState es YouTube Music en la interfaz.
type ytState struct {
	bin     string // el yt-dlp ("" si no hay: no se ofrece nada)
	query   string // lo último que se buscó allí
	pending bool   // esperando la respuesta de esa búsqueda
	tracks  []ytm.Track
	results chan ytSearchDone
	events  chan ytEvent
	queue   chan *ytJob        // las descargas, para el trabajador
	jobs    []*ytJob           // los trabajos, el último al final
	cancel  context.CancelFunc // para la escucha en curso
	bgScan  *Scan              // la biblioteca, poniéndose al día tras descargar
	sel     int                // la fila elegida en Descargas
	// Para "ya la tienes": los youtube_id de .library.json y las canciones por
	// artista y título, de la biblioteca keysOf.
	ids    map[string]bool
	keys   map[string]Song
	keysOf *Library
}

type ytSearchDone struct {
	query  string
	tracks []ytm.Track
	err    error
}

// initYT busca el yt-dlp del usuario y arranca el trabajador de descargas.
func (a *App) initYT() {
	a.yt.results = make(chan ytSearchDone, 4)
	a.yt.events = make(chan ytEvent, 256)
	a.yt.queue = make(chan *ytJob, 128)
	a.yt.ids = map[string]bool{}
	if a.offline {
		return
	}
	a.yt.bin = ytm.Find(a.paths.Base)
	if a.yt.bin != "" {
		a.loadHaveIDs()
	}
	bin := a.yt.bin
	// El trabajador de descargas (de YouTube Music y de los servidores), de
	// una en una.
	go func() {
		for j := range a.yt.queue {
			dir := ""
			if dirs := a.cfg.MusicDirs; len(dirs) > 0 {
				dir = dirs[0]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			var path string
			var err error
			if j.run != nil {
				path, err = j.run(ctx, a.ytReporter(j))
			} else {
				path, err = ytm.Download(ctx, bin, j.track, dir, ytCacheDir(), a.ytReporter(j))
			}
			cancel()
			a.yt.events <- ytEvent{job: j, fin: true, path: path, err: err}
			ebiten.ScheduleFrame()
		}
	}()
}

// ytReporter manda el progreso de j a la interfaz (sin bloquear: si se
// acumulan, se pierde alguno, no importa).
func (a *App) ytReporter(j *ytJob) func(ytm.Progress) {
	return func(p ytm.Progress) {
		select {
		case a.yt.events <- ytEvent{job: j, p: p}:
			ebiten.ScheduleFrame()
		default:
		}
	}
}

// ytSearch busca q en YouTube Music en segundo plano.
func (a *App) ytSearch(q string) {
	q = strings.TrimSpace(q)
	if a.yt.bin == "" || q == "" || q == a.yt.query {
		return
	}
	a.yt.query, a.yt.tracks, a.yt.pending = q, nil, true
	a.loadHaveIDs()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := ytm.Search(ctx, q)
		a.yt.results <- ytSearchDone{q, res, err}
		ebiten.ScheduleFrame() // la interfaz puede estar esperando eventos
	}()
}

// updateYT recoge lo que terminó o avanzó en segundo plano. Va en cada Update.
func (a *App) updateYT() {
	for {
		select {
		case r := <-a.yt.results:
			if r.query != a.yt.query {
				continue // se buscó otra cosa mientras
			}
			a.yt.pending = false
			if r.err != nil {
				a.flash(T("ytm_error", r.err))
				continue
			}
			a.yt.tracks = r.tracks
			a.search.lastQuery = "" // vuelve a juntar los resultados
			a.updateSearchResults()
		case e := <-a.yt.events:
			a.ytEvent(e)
		default:
			a.ytScanDone()
			return
		}
	}
}

// ytEvent aplica un aviso de un trabajo.
func (a *App) ytEvent(e ytEvent) {
	j := e.job
	if !e.fin {
		if !j.active() {
			return
		}
		switch e.p.Stage {
		case "resolve":
			j.state = jobResolve
		case "download":
			j.state, j.done, j.total = jobDownload, e.p.Done, e.p.Total
		case "convert":
			j.state = jobConvert
		}
		return
	}
	j.path, j.err = e.path, e.err
	if e.err != nil {
		j.state = jobError
		if !errors.Is(e.err, context.Canceled) {
			a.flash(T("ytm_error", e.err))
		}
		return
	}
	j.state = jobDone
	if j.after != nil {
		j.after(j.path)
	}
	if j.podcast {
		a.flash(T("ytm_saved", j.track.Title))
		return
	}
	if j.listen {
		a.send("load", "0", j.path)
		a.screen = NowPlaying
		return
	}
	if !j.remote {
		a.yt.ids[j.track.ID] = true
	}
	a.flash(T("ytm_saved", j.track.Title))
	if a.yt.bgScan == nil {
		a.yt.bgScan = StartScan(a.paths, a.cfg.MusicDirs)
	}
}

// ytScanDone recoge la biblioteca puesta al día tras una descarga.
func (a *App) ytScanDone() {
	if a.yt.bgScan == nil {
		return
	}
	if lib, err, ok := a.yt.bgScan.Result(); ok {
		a.yt.bgScan = nil
		if err == nil && lib != nil && len(lib.Songs) > 0 {
			a.setLocal(lib)
			a.search.lastQuery = ""
		}
	}
}

// addJob apunta un trabajo (se quedan los 40 últimos).
func (a *App) addJob(j *ytJob) {
	a.yt.jobs = append(a.yt.jobs, j)
	if n := len(a.yt.jobs); n > 40 {
		for k, old := range a.yt.jobs {
			if !old.active() {
				a.yt.jobs = append(a.yt.jobs[:k], a.yt.jobs[k+1:]...)
				break
			}
		}
	}
}

// ytPlay baja t a la caché y la pone a sonar. Si se estaba bajando otra para
// escuchar, se cancela.
func (a *App) ytPlay(t ytm.Track) {
	if a.yt.cancel != nil {
		a.yt.cancel()
	}
	j := &ytJob{track: t, listen: true, state: jobResolve}
	a.addJob(j)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	a.yt.cancel = cancel
	bin, dir := a.yt.bin, ytCacheDir()
	go func() {
		defer cancel()
		path, err := ytm.Fetch(ctx, bin, t, dir, ytCacheKeep, a.ytReporter(j))
		a.yt.events <- ytEvent{job: j, fin: true, path: path, err: err}
		ebiten.ScheduleFrame()
	}()
}

// ytDownload pone t en la cola de descargas.
func (a *App) ytDownload(t ytm.Track) {
	if len(a.cfg.MusicDirs) == 0 {
		return
	}
	for _, j := range a.yt.jobs {
		if !j.listen && j.track.ID == t.ID && (j.active() || j.state == jobDone) {
			a.openDownloads() // ya está: se enseña
			return
		}
	}
	j := &ytJob{track: t, state: jobWaiting}
	a.addJob(j)
	a.yt.queue <- j
	a.flash(T("ytm_queued", t.Title))
}

// downloadJobs son las descargas (no lo que se escucha sin guardar, que solo
// sale en la barra de abajo mientras carga), de la más nueva a la más vieja.
func (a *App) downloadJobs() []*ytJob {
	var out []*ytJob
	for k := len(a.yt.jobs) - 1; k >= 0; k-- {
		if j := a.yt.jobs[k]; !j.listen {
			out = append(out, j)
		}
	}
	return out
}

// ytDownloads es cuántas descargas faltan.
func (a *App) ytDownloads() int {
	n := 0
	for _, j := range a.yt.jobs {
		if !j.listen && j.active() {
			n++
		}
	}
	return n
}

// ytMenu es el menú de un resultado de YouTube Music.
func (a *App) ytMenu(r searchResult) {
	t := r.yt
	sub := t.Artist()
	if t.Album != "" {
		sub += " · " + t.Album
	}
	if t.Duration != "" {
		sub += " · " + t.Duration
	}
	m := &menu{title: t.Title, subtitle: sub}
	if r.have {
		if s, ok := a.ownSong(t); ok {
			m.items = append(m.items, menuItem{label: T("ytm_play_own"), icon: "disc", do: func() {
				a.openResult(searchResult{kind: resSong, song: s})
			}})
		}
	}
	m.items = append(m.items,
		menuItem{label: T("ytm_listen"), icon: "play", do: func() { a.ytPlay(t) }},
		menuItem{label: T("ytm_download"), icon: "download", do: func() { a.ytDownload(t) }})
	if len(a.downloadJobs()) > 0 {
		m.items = append(m.items, menuItem{label: T("ytm_jobs"), icon: "list", do: a.openDownloads})
	}
	a.menu = m
}

// openDownloads va a la pantalla de Descargas.
func (a *App) openDownloads() {
	a.screen, a.yt.sel = Downloads, 0
}

// jobText es el estado de un trabajo en palabras, y cuánto va (0..1; -1 si no
// se sabe).
func (j *ytJob) status() (string, float64) {
	switch j.state {
	case jobWaiting:
		return T("ytm_st_wait"), 0
	case jobResolve:
		return T("ytm_st_resolve"), -1
	case jobDownload:
		if j.total > 0 {
			f := float64(j.done) / float64(j.total)
			return T("ytm_st_download", int(f*100)), f
		}
		return T("ytm_st_download", 0), -1
	case jobConvert:
		return T("ytm_st_convert"), -1
	case jobDone:
		if j.listen {
			return T("ytm_st_playing"), 1
		}
		return T("ytm_st_done"), 1
	}
	return T("ytm_st_error"), 0
}

// drawJobBar pinta una barra de progreso de w píxeles: llena hasta f, o un
// bloque que va y viene si no se sabe cuánto va (f < 0).
func (a *App) drawJobBar(dst *ebiten.Image, x, y, w int, f float64) {
	th := a.theme
	rect(dst, x, y, w, 3, withAlpha(th.Dim, 0x50))
	if f < 0 {
		bw := w / 4
		p := math.Abs(math.Mod(a.t*0.8, 2) - 1) // 0..1..0
		rect(dst, x+int(p*float64(w-bw)), y, bw, 3, th.Accent)
		return
	}
	rect(dst, x, y, int(float64(w)*f+0.5), 3, th.Accent)
}

// drawYTBar pinta, sobre la ayuda de abajo, lo que se está haciendo: primero
// la canción que se baja para escuchar y si no, la descarga en curso.
func (a *App) drawYTBar(dst *ebiten.Image) {
	var cur *ytJob
	for _, j := range a.yt.jobs {
		if j.active() && j.state != jobWaiting && (cur == nil || j.listen) {
			cur = j
		}
	}
	if cur == nil && a.status.Loading {
		// Una canción de un servidor que aún se está bajando para sonar.
		cur = &ytJob{track: ytm.Track{Title: a.playingSong.Title}, listen: true, state: jobResolve, remote: true}
	}
	if cur == nil {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	y := h - bottomH - 13
	rect(dst, 0, y, w, 13, withAlpha(th.Shadow, 0xd8))
	rect(dst, 0, y, w, 1, withAlpha(th.Accent, 0x60))
	ic, icCol := "download", th.Accent
	switch {
	case cur.remote && cur.listen && radio.IsRadio(a.playingSong.Path):
		ic = "radio"
	case cur.remote && cur.listen:
		ic, icCol = "server", th.Accent
	case cur.listen:
		ic, icCol = "ytm", ytRed
	}
	drawIcon(dst, ic, margin, y+3, icCol)
	st, f := cur.status()
	if cur.remote && cur.state == jobResolve {
		st = T("srv_fetching")
		if radio.IsRadio(a.playingSong.Path) {
			st = T("rd_connecting")
		}
	}
	label := cur.track.Title
	if n := a.ytDownloads(); !cur.listen && n > 1 {
		label = T("ytm_bar_more", label, n-1)
	}
	barW := 46
	stW := textWidth(st, SmallFace)
	right := w - margin - barW - 6 - stW
	drawTextW(dst, label, SmallFace, margin+12, y+4, right-margin-16, th.Text)
	drawText(dst, st, SmallFace, right, y+4, th.Dim)
	a.drawJobBar(dst, w-margin-barW, y+5, barW, f)
}

// updateDownloads atiende la pantalla de Descargas: arriba y abajo eligen; A
// pone la canción descargada (o reintenta si falló); B vuelve a la pantalla
// de antes.
func (a *App) updateDownloads() {
	in := &a.input
	jobs := a.downloadJobs()
	stream, hasStream := a.streamSavable()
	off := 0
	if hasStream {
		off = 1 // la primera fila es lo que suena en streaming
	}
	n := len(jobs) + off
	switch {
	case in.Pressed(ActBack):
		a.goBack()
	case in.Repeat(ActUp):
		a.yt.sel = max(0, a.yt.sel-1)
	case in.Repeat(ActDown):
		a.yt.sel = min(max(0, n-1), a.yt.sel+1)
	case in.Pressed(ActConfirm) && hasStream && a.yt.sel == 0:
		a.ytDownload(stream)
	case in.Pressed(ActConfirm) && n > 0:
		j := jobs[a.yt.sel-off]
		switch {
		case j.state == jobDone:
			a.send("load", "0", j.path)
			a.screen = NowPlaying
		case j.state == jobError:
			j.state, j.err = jobWaiting, nil
			a.yt.queue <- j
		}
	}
}

// drawDownloads pinta la pantalla de Descargas: cada una con su estado y su
// barra, de la más nueva a la más vieja.
func (a *App) drawDownloads(dst *ebiten.Image) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	x, top := margin+4, topH+6
	a.drawTopBar(dst, T("ytm_jobs"))
	if a.yt.bin == "" {
		drawTextW(dst, T("ytm_no_ytdlp"), SmallFace, x, top+6, w-2*x, th.Dim)
	}
	const rowH = 22
	rows := max(1, (h-bottomH-14-top-16)/rowH) // sin tapar la barra de progreso
	jobs := a.downloadJobs()
	stream, hasStream := a.streamSavable()
	off := 0
	if hasStream {
		off = 1
	}
	n := len(jobs) + off
	if n == 0 && a.yt.bin != "" {
		drawText(dst, T("ytm_jobs_none"), SmallFace, x, top+6, th.Dim)
	}
	first := max(0, min(a.yt.sel-rows/2, n-rows))
	for i := 0; i < rows && first+i < n; i++ {
		y := top + 6 + i*rowH
		if first+i == a.yt.sel {
			panel(dst, x-3, y-3, w-2*x+6, rowH-2, mix(th.Bg2, th.Accent, 0.3))
		}
		if hasStream && first+i == 0 {
			// Lo que suena en streaming, para guardarlo de una vez.
			drawIcon(dst, "ytm", x-1, y, ytRed)
			st := T("ytm_st_stream")
			stW := textWidth(st, SmallFace)
			drawTextW(dst, stream.Title, SmallFace, x+12, y, w-2*x-stW-20, th.Text)
			drawText(dst, st, SmallFace, w-x-stW, y, th.Accent)
			drawTextW(dst, T("ytm_stream_hint"), SmallFace, x+12, y+9, w-2*x-12, th.Dim)
			continue
		}
		j := jobs[first+i-off]
		ic, icCol := "download", th.Accent
		if j.remote {
			ic = "server"
		}
		if j.podcast {
			ic = "mic"
		}
		if j.state == jobDone {
			ic, icCol = "check", th.Accent2
		}
		drawIcon(dst, ic, x, y, icCol)
		st, f := j.status()
		stW := textWidth(st, SmallFace)
		drawTextW(dst, j.track.Title, SmallFace, x+12, y, w-2*x-stW-20, th.Text)
		stCol := th.Dim
		if j.state == jobError {
			stCol = th.Accent2
		}
		drawText(dst, st, SmallFace, w-x-stW, y, stCol)
		sub := j.track.Artist()
		if j.err != nil {
			sub = j.err.Error()
		}
		drawTextW(dst, sub, SmallFace, x+12, y+9, w-2*x-70, th.Dim)
		if j.active() {
			a.drawJobBar(dst, w-x-50, y+11, 50, f)
		}
	}
	a.drawHints(dst, []hint{{"A", T("ytm_jobs_open")}, {"B", T("h_back")}, {"SEL", T("h_menu")}})
}

// loadHaveIDs lee los youtube_id de las .library.json de las carpetas de
// música (ytm-gym y las descargas de aquí).
func (a *App) loadHaveIDs() {
	a.yt.ids = map[string]bool{}
	for _, d := range a.cfg.MusicDirs {
		for id := range ytm.LoadLibrary(d) {
			a.yt.ids[id] = true
		}
	}
}

// songKey es artista y título reducidos para comparar: el primer artista y el
// título sin lo que va entre paréntesis o corchetes ni tras " - " (feat.,
// remaster, versión...), sin tildes ni signos.
func songKey(artist, title string) string {
	for _, sep := range []string{" (", " [", " - "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	clean := func(s string) string {
		return strings.Join(strings.FieldsFunc(fold(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
	}
	return clean(firstArtist(artist)) + "|" + clean(title)
}

// ownSong es la canción de la biblioteca que probablemente es t.
func (a *App) ownSong(t ytm.Track) (Song, bool) {
	if a.lib == nil {
		return Song{}, false
	}
	if a.yt.keysOf != a.lib {
		a.yt.keys, a.yt.keysOf = map[string]Song{}, a.lib
		for _, s := range a.lib.Songs {
			a.yt.keys[songKey(s.Artist, s.Title)] = s
		}
	}
	a0 := ""
	if len(t.Artists) > 0 {
		a0 = t.Artists[0]
	}
	s, ok := a.yt.keys[songKey(a0, t.Title)]
	return s, ok
}

// ytResults son los resultados de YouTube Music de la búsqueda actual.
func (a *App) ytResults(q string) []searchResult {
	if a.yt.bin == "" || strings.TrimSpace(q) != a.yt.query {
		return nil
	}
	var out []searchResult
	for _, t := range a.yt.tracks {
		_, own := a.ownSong(t)
		have := a.yt.ids[t.ID] || own
		sub := t.Artist()
		if t.Duration != "" {
			sub += " · " + t.Duration
		}
		out = append(out, searchResult{kind: resYT, main: t.Title, sub: sub, yt: t, have: have})
	}
	return out
}

// downloadRemote pone en la cola de descargas las canciones de un servidor
// que no estén ya (el archivo original, a la primera carpeta de música).
func (a *App) downloadRemote(songs []Song) {
	n := 0
	for _, s := range songs {
		src, rs, ok := a.remoteSong(s.Path)
		if !ok || len(a.cfg.MusicDirs) == 0 {
			continue
		}
		dup := false
		for _, j := range a.yt.jobs {
			if j.remote && !j.listen && j.track.ID == rs.ID && j.state != jobError {
				dup = true
			}
		}
		if dup {
			continue
		}
		server, dir, store := src.srv.Name, a.cfg.MusicDirs[0], a.store
		j := &ytJob{track: ytm.Track{ID: rs.ID, Title: rs.Title, Artists: []string{rs.Artist}, Album: rs.Album}, remote: true, state: jobWaiting}
		j.run = func(ctx context.Context, progress func(ytm.Progress)) (string, error) {
			progress(ytm.Progress{Stage: "resolve"})
			return store.Download(ctx, rs, server, dir, func(done, total int64) {
				progress(ytm.Progress{Stage: "download", Done: done, Total: max(total, done+1)})
			})
		}
		a.addJob(j)
		a.yt.queue <- j
		n++
	}
	if n > 0 {
		a.flash(T("srv_queued", N("n_songs", n)))
	}
}

// isRemote dice si todas las canciones son de un servidor.
func isRemoteSongs(songs []Song) bool {
	for _, s := range songs {
		if !remote.IsRemote(s.Path) {
			return false
		}
	}
	return len(songs) > 0
}
