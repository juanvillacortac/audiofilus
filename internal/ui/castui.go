package ui

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/cast"
	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/dlna"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"github.com/juanvillacortac/audiofilus/internal/remote"
)

// Mandar la música a un altavoz o una tele de la red (internal/cast:
// DLNA/UPnP y Chromecast). Reproduciendo → Start → "Enviar a…" busca los
// aparatos unos segundos y, al elegir uno, la canción sigue allí desde donde
// iba (aquí se pausa). Mientras se manda, los mandos (pausa, siguiente,
// anterior, saltar, elegir en la cola, poner otra lista) van al aparato, la
// barra muestra su posición y arriba sale "→ <aparato>"; la cola sigue sola
// en el aparato. "Dejar de enviar" lo para y la música sigue aquí, donde iba.
//
// El servicio local no sabe nada de esto: se queda en pausa con su cola. Lo
// que se manda es una copia de la cola (rutas) y su posición; al volver, se
// le carga esa cola en esa canción y ese segundo.

// castState es el envío en curso (o la búsqueda de aparatos).
type castState struct {
	srv  *cast.Server   // el servidor de archivos, uno por proceso (se abre al primer envío)
	sess *cast.Session  // la sesión con el aparato (nil: no se manda nada)
	dev  cast.Device    // el aparato
	list []string       // la cola que se manda (rutas) y la canción que suena
	idx  int            //
	st   cast.Status    // lo último que dijo el aparato
	stop chan struct{}  // cierra la goroutine que lee los eventos de la sesión
	ev   chan castEvent // de las goroutines a la interfaz
	busy bool           // buscando aparatos
}

// castEvent es lo que llega de las goroutines: aparatos encontrados, un
// evento de la sesión o un error.
type castEvent struct {
	devs  []cast.Device
	found bool
	ev    *cast.Event
	err   error
	gen   *cast.Session // de qué sesión es (las viejas se ignoran)
}

// casting dice si se está mandando la música a un aparato.
func (a *App) casting() bool { return a.cast.sess != nil }

// castEvents es el canal de avisos (se crea al primer uso).
func (a *App) castEvents() chan castEvent {
	if a.cast.ev == nil {
		a.cast.ev = make(chan castEvent, 32)
	}
	return a.cast.ev
}

// castItems son las opciones de Reproduciendo: enviar, o (mandando) dejar de
// enviar y el volumen del aparato.
func (a *App) castItems() []menuItem {
	if !a.casting() {
		return []menuItem{{label: T("cast_send"), icon: "speaker", do: a.castSearch}}
	}
	items := []menuItem{
		{label: T("cast_stop", a.cast.dev.Name), icon: "power", do: func() { a.castEnd(true) }},
		{label: T("cast_volume"), icon: "speaker",
			do:    func() {},
			value: func() string { return castVolText(a.cast.st.Volume) }},
		{label: T("cast_vol_up"), icon: "plus", do: func() { a.castVolume(0.1) }},
		{label: T("cast_vol_down"), icon: "speaker", do: func() { a.castVolume(-0.1) }},
	}
	if a.cast.dev.Kind == cast.MPD {
		// Su propia biblioteca (mpdremote.go).
		items = append(items, menuItem{label: T("mpd_library", a.cast.dev.Name), icon: "folder", do: a.openMPDLibrary})
	}
	return items
}

// castVolText es el volumen del aparato en palabras.
func castVolText(v float64) string {
	if v < 0 {
		return "?"
	}
	return fmt.Sprintf("%d%%", int(v*100+0.5))
}

// castSearch busca aparatos (unos segundos) y luego los ofrece en un menú.
func (a *App) castSearch() {
	if a.cast.busy {
		return
	}
	a.cast.busy = true
	a.flash(T("cast_searching"))
	ch := a.castEvents()
	go func() {
		devs, err := cast.Discover(context.Background(), 3*time.Second)
		// El renderer DLNA de este mismo audiofilus también aparece: fuera.
		if own := dlna.RendererUDN(a.paths.Data); own != "" {
			devs = slices.DeleteFunc(devs, func(d cast.Device) bool { return strings.EqualFold(d.ID, own) })
		}
		// Y su servidor MPD.
		if a.cfg.MPD.On {
			devs = slices.DeleteFunc(devs, func(d cast.Device) bool { return cast.IsOwnMPD(d, a.cfg.MPD.MPDPort()) })
		}
		ch <- castEvent{devs: devs, found: true, err: err}
		ebiten.ScheduleFrame()
	}()
}

// castPick es el menú de los aparatos encontrados.
func (a *App) castPick(devs []cast.Device) {
	if len(devs) == 0 {
		a.flash(T("cast_none"))
		return
	}
	var items []menuItem
	for _, d := range devs {
		icon := "speaker"
		switch d.Kind {
		case cast.Chromecast:
			icon = "screen"
		case cast.MPD:
			icon = "server"
		}
		label := d.Name
		if d.Model != "" && d.Model != d.Name {
			label += " · " + d.Model
		}
		items = append(items, menuItem{label: label, icon: icon, do: func() {
			if !d.NeedsPassword {
				a.castStart(d)
				return
			}
			// Un servidor MPD con contraseña: se pide (con la última que
			// funcionó ya escrita).
			a.openRawPrompt(T("mpd_password_for", d.Name), a.mpdPasswords[d.Host], true, func(pw string) {
				d.Password = pw
				a.mpdPasswords[d.Host] = pw
				a.castStart(d)
			})
		}})
	}
	a.menu = &menu{title: T("cast_send"), subtitle: T("cast_found", len(devs)), items: items}
}

// castStart empieza a mandar la cola, en la canción y el segundo de ahora.
func (a *App) castStart(d cast.Device) {
	c := &a.cast
	if c.srv == nil {
		srv, err := cast.NewServer("")
		if err != nil {
			a.flash(T("cast_failed", err))
			return
		}
		c.srv = srv
	}
	sess, err := cast.NewSession(c.srv, d)
	if err != nil {
		a.flash(T("cast_failed", err))
		return
	}
	list := a.queue()
	if len(list) == 0 && a.playingSong.Path != "" {
		list = []string{a.playingSong.Path}
	}
	if len(list) == 0 {
		a.flash(T("cast_nothing"))
		return
	}
	idx := max(0, min(a.status.Index, len(list)-1))
	if a.playingSong.Path != "" && list[idx] != a.playingSong.Path {
		for i, p := range list {
			if p == a.playingSong.Path {
				idx = i
				break
			}
		}
	}
	if a.client != nil && a.isPlaying {
		a.client.Send("pause") // aquí se calla (directo: no pasa por castSend)
	}
	c.sess, c.dev, c.list, c.idx = sess, d, list, idx
	c.st = cast.Status{State: cast.Loading, Position: a.position, Duration: a.duration, Volume: -1}
	c.stop = make(chan struct{})
	ch := a.castEvents()
	go func(stop chan struct{}) { // los eventos de la sesión, a la interfaz
		for {
			select {
			case e := <-sess.Events():
				ch <- castEvent{ev: &e, gen: sess}
				ebiten.ScheduleFrame()
			case <-stop:
				return
			}
		}
	}(c.stop)
	a.castLoad(idx, a.position)
	a.flash(T("cast_on", d.Name))
}

// castPrep es una canción lista para mandar: lo que se sabe sin red (de la
// biblioteca, en el hilo de la interfaz) y, si es de un servidor, con qué
// pedir sus direcciones (en una goroutine: en Jellyfin hay que iniciar la
// sesión).
type castPrep struct {
	item    cast.Item
	srv     *config.Server
	id, art string
}

// castMeta prepara path: un archivo local (que sirve el servidor,
// convirtiéndolo si hace falta) o una dirección http(s) (las canciones de un
// servidor y las radios).
func (a *App) castMeta(path string) castPrep {
	if strings.HasPrefix(path, mpdLibScheme) {
		return castPrep{item: a.mpdCastItem(path)} // mpdremote.go
	}
	it := cast.Item{Path: path}
	if s, ok := a.lib.Song(path); ok {
		it.Title, it.Artist, it.Album = s.Title, s.Artist, s.Album
	} else if a.local != nil {
		if s, ok := a.local.Song(path); ok {
			it.Title, it.Artist, it.Album = s.Title, s.Artist, s.Album
		}
	}
	if it.Title == "" {
		it.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	p := castPrep{item: it}
	switch {
	case remote.IsRemote(path):
		if r, rs, ok := a.remoteSong(path); ok {
			srv := r.srv
			p.srv, p.id, p.art = &srv, rs.ID, rs.CoverArt
			p.item.Title, p.item.Artist, p.item.Album = rs.Title, rs.Artist, rs.Album
		}
	case radio.IsRadio(path):
		if a.radio.stations != nil {
			if st, ok := a.radio.stations.Get(strings.TrimPrefix(path, radio.Scheme)); ok {
				p.item.Path, p.item.URL, p.item.Title = "", st.URL, st.Name
			}
		}
	default:
		if al, _ := a.lib.AlbumOf(path); al != nil {
			switch strings.ToLower(filepath.Ext(al.coverPath)) {
			case ".jpg", ".jpeg", ".png", ".webp":
				p.item.Cover = al.coverPath
			}
		}
	}
	return p
}

// resolve termina de preparar la canción (las direcciones de un servidor).
func (p castPrep) resolve(ctx context.Context) cast.Item {
	it := p.item
	if p.srv != nil {
		b := remote.New(*p.srv)
		if err := b.Ping(ctx); err != nil {
			log.Printf("cast: %s: %v", p.srv.Name, err)
		}
		it.Path, it.URL = "", b.StreamURL(p.id)
		if p.art != "" {
			it.Cover = b.CoverURL(p.art, 500)
		}
	}
	return it
}

// castLoad manda la canción i de la cola (desde start segundos) y deja
// preparada la siguiente.
func (a *App) castLoad(i int, start float64) {
	c := &a.cast
	if c.sess == nil || i < 0 || i >= len(c.list) {
		return
	}
	c.idx = i
	sess, ch := c.sess, a.castEvents()
	cur := a.castMeta(c.list[i])
	var next *castPrep
	if i+1 < len(c.list) {
		n := a.castMeta(c.list[i+1])
		next = &n
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := sess.Cast(ctx, cur.resolve(ctx), start); err != nil {
			ch <- castEvent{err: err, gen: sess}
			ebiten.ScheduleFrame()
			return
		}
		if next != nil {
			_ = sess.Queue(ctx, next.resolve(ctx))
		}
	}()
}

// castDo hace una orden en el aparato sin parar la interfaz.
func (a *App) castDo(f func(ctx context.Context, s *cast.Session) error) {
	sess, ch := a.cast.sess, a.castEvents()
	if sess == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := f(ctx, sess); err != nil {
			ch <- castEvent{err: err, gen: sess}
			ebiten.ScheduleFrame()
		}
	}()
}

// castVolume sube o baja el volumen del aparato.
func (a *App) castVolume(d float64) {
	v := a.cast.st.Volume
	if v < 0 {
		v = 0.5
	}
	v = max(0, min(1, v+d))
	a.cast.st.Volume = v
	a.castDo(func(ctx context.Context, s *cast.Session) error { return s.SetVolume(ctx, v) })
}

// castSend atiende una orden de la interfaz al servicio mientras se manda la
// música: la de reproducción va al aparato (y no al servicio). Dice si la
// atendió.
func (a *App) castSend(cmd string, args []string) bool {
	c := &a.cast
	if c.sess == nil {
		return false
	}
	switch cmd {
	case "toggle", "pause", "resume":
		playing := c.st.State == cast.Playing || c.st.State == cast.Loading
		if cmd == "pause" || (cmd == "toggle" && playing) {
			c.st.State = cast.Paused
			a.castDo(func(ctx context.Context, s *cast.Session) error { return s.Pause(ctx) })
		} else {
			c.st.State = cast.Playing
			a.castDo(func(ctx context.Context, s *cast.Session) error { return s.Resume(ctx) })
		}
	case "next":
		if c.idx+1 < len(c.list) {
			a.castLoad(c.idx+1, 0)
		}
	case "prev":
		if c.st.Position > 3 || c.idx == 0 {
			a.castSeek(0)
		} else {
			a.castLoad(c.idx-1, 0)
		}
	case "seek":
		if len(args) == 0 {
			return true
		}
		v, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			return true
		}
		if strings.HasPrefix(args[0], "+") || strings.HasPrefix(args[0], "-") {
			v += c.st.Position
		}
		a.castSeek(max(0, v))
	case "jump":
		if len(args) > 0 {
			if i, err := strconv.Atoi(args[0]); err == nil {
				a.castLoad(i, 0)
			}
		}
	case "load":
		// Otra lista: suena en el aparato (el servicio no se toca).
		if len(args) < 2 {
			return true
		}
		start, _ := strconv.Atoi(args[0])
		c.list = append([]string(nil), args[1:]...)
		a.castLoad(max(0, min(start, len(c.list)-1)), 0)
	case "add", "playnext":
		// La cola del aparato también; el servicio (en pausa) la recibe igual.
		if cmd == "add" {
			c.list = append(c.list, args...)
		} else {
			c.list = append(c.list[:c.idx+1:c.idx+1], append(append([]string(nil), args...), c.list[c.idx+1:]...)...)
		}
		return false
	default:
		return false
	}
	return true
}

// castSeek salta a sec segundos en el aparato.
func (a *App) castSeek(sec float64) {
	a.cast.st.Position = sec
	a.castDo(func(ctx context.Context, s *cast.Session) error { return s.Seek(ctx, sec) })
}

// castEnd deja de mandar: para el aparato y, con resume, la música sigue
// aquí en la canción y el segundo en que iba.
func (a *App) castEnd(resume bool) {
	c := &a.cast
	sess := c.sess
	if sess == nil {
		return
	}
	pos, list, idx := c.st.Position, c.list, c.idx
	c.sess = nil
	close(c.stop)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sess.Close(ctx)
	}()
	// Lo de la biblioteca del aparato no está aquí: eso no sigue.
	if slices.ContainsFunc(list, func(p string) bool { return strings.HasPrefix(p, mpdLibScheme) }) {
		resume = false
	}
	if resume && a.client != nil && len(list) > 0 {
		a.client.Send("load", append([]string{strconv.Itoa(idx)}, list...)...)
		if pos > 1 {
			a.client.Send("seek", fmt.Sprintf("%.1f", pos))
		}
	}
	a.flash(T("cast_off"))
}

// pollCast atiende lo que llega de las goroutines y, mandando, pone en la
// pantalla lo del aparato: la canción, la posición y si suena. Va después
// de syncStatus, que pone lo del servicio (en pausa).
func (a *App) pollCast() {
	c := &a.cast
	for done := false; !done; {
		select {
		case e := <-a.castEvents():
			a.castEvent(e)
		default:
			done = true
		}
	}
	if c.sess == nil {
		return
	}
	if st := c.sess.Status(); st.State != "" {
		c.st.State, c.st.Position = st.State, st.Position
		if st.Duration > 0 {
			c.st.Duration = st.Duration
		}
		if st.Volume >= 0 {
			c.st.Volume = st.Volume
		}
	}
	a.position, a.duration = c.st.Position, c.st.Duration
	a.isPlaying = c.st.State == cast.Playing
	if c.idx < len(c.list) && a.playingSong.Path != c.list[c.idx] {
		p := c.list[c.idx]
		if al, i := a.lib.AlbumOf(p); al != nil && i >= 0 {
			a.playing, a.playingIdx, a.playingSong = al, i, al.Songs[i]
		} else {
			it := a.castMeta(p).item
			a.playingSong = Song{Path: p, Title: it.Title, Artist: it.Artist, Album: it.Album}
		}
		a.lastTrack = p
	}
}

// castEvent atiende un aviso.
func (a *App) castEvent(e castEvent) {
	c := &a.cast
	switch {
	case e.found:
		c.busy = false
		if e.err != nil && len(e.devs) == 0 {
			a.flash(T("cast_failed", e.err))
			return
		}
		a.castPick(e.devs)
	case e.gen != c.sess || c.sess == nil:
		// De una sesión que ya terminó.
	case e.err != nil:
		a.flash(T("cast_failed", e.err))
	case e.ev != nil:
		switch e.ev.Kind {
		case cast.Advanced:
			// El aparato pasó a la siguiente: se prepara la que sigue.
			c.idx++
			c.st.Position, c.st.Duration = 0, e.ev.Item.Duration
			if c.idx+1 < len(c.list) {
				next := a.castMeta(c.list[c.idx+1])
				a.castDo(func(ctx context.Context, s *cast.Session) error { return s.Queue(ctx, next.resolve(ctx)) })
			}
		case cast.Finished:
			a.flash(T("cast_finished"))
			a.castEnd(false)
		case cast.Failed:
			a.flash(T("cast_failed", e.ev.Err))
			a.castEnd(false)
		}
	}
}

// castTitle añade el aparato al título de arriba mientras se manda.
func (a *App) castTitle(title string) string {
	if a.casting() {
		return title + " → " + a.cast.dev.Name
	}
	return title
}
