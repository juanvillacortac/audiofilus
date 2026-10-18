package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/podcast"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
)

// Los podcasts: una pantalla de la barra de Select con Suscripciones,
// Populares (el ranking de Apple Podcasts del país de la radio) y Buscar (la
// búsqueda de Apple, sin clave; o añadir un RSS por su dirección). A en un
// podcast abre sus episodios. Los episodios se bajan a data/podcasts y suenan
// desde la tarjeta (sin cortes y sin gastar datos): escuchar uno que no está
// lo baja y lo pone al terminar. Se recuerda por dónde se iba en cada uno
// (se sigue desde ahí) y se marca escuchado al acercarse al final. Por
// podcast, "Bajar los nuevos" baja solo el episodio nuevo al actualizar.

// Las pestañas de los podcasts.
const (
	ptSubs = iota
	ptTop
	ptSearch
	ptCount
)

var podTabKeys = [ptCount]string{"pd_subs", "pd_top", "pd_search"}

// subscription es un podcast suscrito, con sus episodios.
type subscription struct {
	Show     podcast.Show      `json:"show"`
	Auto     bool              `json:"auto,omitempty"` // bajar los nuevos
	Episodes []podcast.Episode `json:"episodes"`
	Checked  time.Time         `json:"checked"`
}

// epProgress es por dónde se va en un episodio.
type epProgress struct {
	Pos  float64 `json:"pos"`
	Dur  float64 `json:"dur"`
	Done bool    `json:"done,omitempty"`
}

// podState es la pantalla de los podcasts.
type podState struct {
	tab      int
	tabFocus bool
	sel      [ptCount]int
	lists    [ptCount][]podcast.Show
	loading  [ptCount]bool
	query    string
	results  chan podResult
	subs     []*subscription
	progress map[string]epProgress // por archivo del episodio
	open     *subscription         // el podcast abierto (sus episodios)
	epSel    int
	loaded   bool
	savedAt  float64
	dirty    bool
}

type podResult struct {
	tab   int
	list  []podcast.Show
	sub   *subscription // un RSS leído (de una suscripción o para abrir)
	eps   []podcast.Episode
	show  podcast.Show
	err   error
	opens bool // abrirlo al llegar
}

func (a *App) podFile() string     { return filepath.Join(a.paths.Data, "podcasts.json") }
func (a *App) podProgFile() string { return filepath.Join(a.paths.Data, "podcast-progress.json") }
func (a *App) podDir() string      { return filepath.Join(a.paths.Data, "podcasts") }

// initPodcasts lee las suscripciones y por dónde se iba.
func (a *App) initPodcasts() {
	p := &a.pod
	p.results = make(chan podResult, 16)
	p.progress = map[string]epProgress{}
	if b, err := os.ReadFile(a.podFile()); err == nil {
		_ = json.Unmarshal(b, &p.subs)
	}
	if b, err := os.ReadFile(a.podProgFile()); err == nil {
		_ = json.Unmarshal(b, &p.progress)
	}
	a.syncSubsList()
}

// syncSubsList pone la lista de suscripciones de la pestaña.
func (a *App) syncSubsList() {
	p := &a.pod
	p.lists[ptSubs] = p.lists[ptSubs][:0]
	for _, s := range p.subs {
		p.lists[ptSubs] = append(p.lists[ptSubs], s.Show)
	}
}

// savePodcasts guarda las suscripciones.
func (a *App) savePodcasts() {
	if b, err := json.Marshal(a.pod.subs); err == nil {
		_ = os.WriteFile(a.podFile(), b, 0o644)
	}
	a.syncSubsList()
}

// subOf es la suscripción de un podcast (por su RSS), si lo está.
func (a *App) subOf(feed string) *subscription {
	for _, s := range a.pod.subs {
		if s.Show.Feed == feed {
			return s
		}
	}
	return nil
}

// epPath es el archivo de un episodio: data/podcasts/<podcast>/<fecha> <título>.ext.
func (a *App) epPath(show podcast.Show, e podcast.Episode) string {
	name := e.Title
	if !e.Date.IsZero() {
		name = e.Date.Format("2006-01-02") + " " + name
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return filepath.Join(a.podDir(), remote.Slug(show.Title), safeFile(name)+e.Ext())
}

// safeFile quita lo que no vale en un nombre de archivo de exFAT.
func safeFile(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	return strings.TrimRight(s, ". ")
}

// downloaded dice si el episodio ya está en la tarjeta.
func (a *App) downloaded(show podcast.Show, e podcast.Episode) bool {
	_, err := os.Stat(a.epPath(show, e))
	return err == nil
}

// podLoad pide una pestaña (o un RSS) en segundo plano.
func (a *App) podLoad(tab int) {
	p := &a.pod
	if p.loading[tab] {
		return
	}
	p.loading[tab] = true
	query, country := p.query, a.country()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res := podResult{tab: tab}
		switch tab {
		case ptTop:
			res.list, res.err = podcast.Top(ctx, country)
		case ptSearch:
			res.list, res.err = podcast.Search(ctx, query, country)
		}
		p.results <- res
		ebiten.ScheduleFrame()
	}()
}

// podFetch lee el RSS de show en segundo plano; opens: abrirlo al llegar.
func (a *App) podFetch(show podcast.Show, opens bool) {
	p := &a.pod
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		s, eps, err := podcast.Fetch(ctx, show.Feed)
		if s.Title == "" {
			s.Title = show.Title
		}
		if s.Artwork == "" {
			s.Artwork = show.Artwork
		}
		if s.Author == "" {
			s.Author = show.Author
		}
		p.results <- podResult{tab: -1, show: s, eps: eps, err: err, opens: opens}
		ebiten.ScheduleFrame()
	}()
}

// refreshSubs pone al día las suscripciones que llevan más de una hora sin
// mirarse.
func (a *App) refreshSubs(force bool) {
	for _, s := range a.pod.subs {
		if force || time.Since(s.Checked) > time.Hour {
			s.Checked = time.Now()
			a.podFetch(s.Show, false)
		}
	}
}

// updatePodResults recoge lo que llegó. Va en cada Update.
func (a *App) updatePodResults() {
	p := &a.pod
	for {
		select {
		case r := <-p.results:
			if r.tab >= 0 {
				p.loading[r.tab] = false
				if r.err != nil {
					a.flash(T("pd_error", r.err))
					continue
				}
				p.lists[r.tab], p.sel[r.tab] = r.list, 0
				continue
			}
			if r.err != nil {
				a.flash(T("pd_error", r.err))
				continue
			}
			sub := a.subOf(r.show.Feed)
			if sub != nil {
				newest := sub.newest()
				sub.Show, sub.Episodes, sub.Checked = r.show, r.eps, time.Now()
				a.savePodcasts()
				// Bajar los nuevos: el más reciente, si es nuevo y no se oyó.
				if sub.Auto && len(r.eps) > 0 && r.eps[0].GUID != newest && !a.downloaded(sub.Show, r.eps[0]) {
					a.podDownload(sub.Show, r.eps[0], false)
				}
			} else {
				sub = &subscription{Show: r.show, Episodes: r.eps, Checked: time.Now()}
			}
			if r.opens {
				p.open, p.epSel = sub, 0
			}
		default:
			if p.dirty && a.t-p.savedAt > 15 {
				p.dirty, p.savedAt = false, a.t
				if b, err := json.Marshal(p.progress); err == nil {
					_ = os.WriteFile(a.podProgFile(), b, 0o644)
				}
			}
			return
		}
	}
}

// newest es el GUID del episodio más nuevo que se conocía.
func (s *subscription) newest() string {
	if len(s.Episodes) == 0 {
		return ""
	}
	return s.Episodes[0].GUID
}

// trackPodcast apunta por dónde va el episodio que suena. Va en cada Update.
func (a *App) trackPodcast() {
	path := a.playingSong.Path
	if !strings.HasPrefix(path, a.podDir()) || a.duration <= 0 {
		return
	}
	pr := a.pod.progress[path]
	pos := a.position
	if pos == pr.Pos && a.duration == pr.Dur {
		return
	}
	pr.Pos, pr.Dur = pos, a.duration
	if pos > a.duration-30 || pos > 0.95*a.duration {
		pr.Done = true // casi al final: escuchado
	}
	a.pod.progress[path] = pr
	a.pod.dirty = true
}

// podPlay pone a sonar un episodio desde donde se iba; si no está, lo baja
// primero.
func (a *App) podPlay(show podcast.Show, e podcast.Episode) {
	path := a.epPath(show, e)
	if _, err := os.Stat(path); err != nil {
		a.podDownload(show, e, true)
		return
	}
	a.send("load", "0", path)
	if pr := a.pod.progress[path]; pr.Pos > 10 && !pr.Done {
		a.send("seek", fmt.Sprint(int(pr.Pos)-3)) // un poco antes, para situarse
	}
	a.screen = NowPlaying
}

// podDownload baja un episodio (y su portada y etiquetas al lado, para el
// reproductor); con play, lo pone al terminar.
func (a *App) podDownload(show podcast.Show, e podcast.Episode, play bool) {
	path := a.epPath(show, e)
	for _, j := range a.yt.jobs {
		if j.podcast && j.path == path && j.active() {
			if play {
				a.flash(T("pd_wait"))
			}
			return
		}
	}
	j := &ytJob{track: ytm.Track{ID: e.GUID, Title: e.Title, Artists: []string{show.Title}}, podcast: true, state: jobWaiting, path: path}
	j.run = func(ctx context.Context, progress func(ytm.Progress)) (string, error) {
		progress(ytm.Progress{Stage: "resolve"})
		err := podcast.Download(ctx, e, path, func(done, total int64) {
			progress(ytm.Progress{Stage: "download", Done: done, Total: total})
		})
		if err != nil {
			return "", err
		}
		tags, _ := json.Marshal(map[string]string{"title": e.Title, "artist": show.Title, "album": show.Title})
		_ = os.WriteFile(path+".tags.json", tags, 0o644)
		fetchFile(ctx, show.Artwork, path+".jpg")
		return path, nil
	}
	if play {
		j.after = func(p string) {
			a.send("load", "0", p)
			a.screen = NowPlaying
		}
	}
	a.addJob(j)
	a.yt.queue <- j
	a.flash(T("ytm_queued", e.Title))
}

// fetchFile baja url a path (la portada de un podcast), si no estaba.
func fetchFile(ctx context.Context, url, path string) {
	if url == "" {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	if b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// updatePodcasts atiende la pantalla de los podcasts.
func (a *App) updatePodcasts() {
	p, in := &a.pod, &a.input
	if !p.loaded {
		p.loaded = true
		a.podLoad(ptTop)
		a.refreshSubs(false)
	}
	if p.open != nil {
		a.updateEpisodes()
		return
	}
	if p.tabFocus {
		switch {
		case in.Repeat(ActUp):
			p.tab = (p.tab + ptCount - 1) % ptCount
		case in.Repeat(ActDown):
			p.tab = (p.tab + 1) % ptCount
		case in.Pressed(ActRight), in.Pressed(ActConfirm):
			p.tabFocus = false
			if p.tab == ptSearch && len(p.lists[ptSearch]) == 0 {
				a.podAskSearch()
			}
		case in.Pressed(ActBack):
			a.goBack()
		}
		return
	}
	list := p.lists[p.tab]
	sel := &p.sel[p.tab]
	n := len(list)
	switch {
	case in.Repeat(ActUp):
		if *sel == 0 {
			p.tabFocus = true
		} else {
			*sel--
		}
	case in.Repeat(ActDown):
		*sel = min(max(0, n-1), *sel+1)
	case in.Pressed(ActLeft), in.Pressed(ActBack):
		p.tabFocus = true
	case in.Pressed(ActConfirm) && n > 0:
		a.podOpen(list[*sel])
	case in.Pressed(ActMenu) && n > 0:
		a.showMenu(list[*sel])
	case in.Pressed(ActMenu), in.Pressed(ActShuffle) && p.tab == ptSearch:
		if p.tab == ptSubs {
			a.podAskURL()
		} else {
			a.podAskSearch()
		}
	}
}

// podOpen abre los episodios de un podcast (si está suscrito, ya los tiene).
func (a *App) podOpen(show podcast.Show) {
	if sub := a.subOf(show.Feed); sub != nil && len(sub.Episodes) > 0 {
		a.pod.open, a.pod.epSel = sub, 0
		return
	}
	a.flash(T("pd_loading_feed", show.Title))
	a.podFetch(show, true)
}

// podAskSearch pide qué buscar.
func (a *App) podAskSearch() {
	a.openRawPrompt(T("pd_search_q"), a.pod.query, false, func(q string) {
		a.pod.query, a.pod.tab = q, ptSearch
		a.podLoad(ptSearch)
	})
}

// podAskURL pide la dirección del RSS de un podcast y se suscribe.
func (a *App) podAskURL() {
	a.openRawPrompt(T("pd_url_q"), "https://", false, func(u string) {
		a.subscribe(podcast.Show{Feed: u, Title: u})
		a.podFetch(podcast.Show{Feed: u, Title: u}, false)
	})
}

// subscribe se suscribe a un podcast.
func (a *App) subscribe(show podcast.Show) {
	if a.subOf(show.Feed) != nil {
		return
	}
	a.pod.subs = append(a.pod.subs, &subscription{Show: show})
	a.savePodcasts()
	a.podFetch(show, false)
	a.flash(T("pd_subscribed", show.Title))
}

// unsubscribe quita la suscripción (los episodios bajados se quedan).
func (a *App) unsubscribe(feed string) {
	for i, s := range a.pod.subs {
		if s.Show.Feed == feed {
			a.pod.subs = append(a.pod.subs[:i], a.pod.subs[i+1:]...)
			a.savePodcasts()
			a.flash(T("pd_unsubscribed", s.Show.Title))
			return
		}
	}
}

// showMenu es el menú (Start) de un podcast.
func (a *App) showMenu(show podcast.Show) {
	m := &menu{title: show.Title, subtitle: show.Author}
	m.items = append(m.items, menuItem{label: T("pd_episodes"), icon: "list", do: func() { a.podOpen(show) }})
	if sub := a.subOf(show.Feed); sub != nil {
		m.items = append(m.items,
			menuItem{label: T("pd_auto"), icon: "download",
				active: func() bool { return sub.Auto },
				value:  func() string { return onOffLabel(sub.Auto) },
				do:     func() { sub.Auto = !sub.Auto; a.savePodcasts() }},
			menuItem{label: T("pd_refresh"), icon: "repeat", do: func() { a.podFetch(sub.Show, false); a.flash(T("pd_loading_feed", show.Title)) }},
			menuItem{label: T("pd_unsub"), icon: "trash", danger: true, do: func() { a.unsubscribe(show.Feed) }})
	} else {
		m.items = append(m.items, menuItem{label: T("pd_sub"), icon: "plus", do: func() { a.subscribe(show) }})
	}
	if a.pod.tab == ptSubs {
		m.items = append(m.items, menuItem{label: T("pd_add_url"), icon: "pencil", do: a.podAskURL},
			menuItem{label: T("pd_refresh_all"), icon: "repeat", do: func() { a.refreshSubs(true) }})
	}
	a.menu = m
}

// updateEpisodes atiende la lista de episodios de un podcast.
func (a *App) updateEpisodes() {
	p, in := &a.pod, &a.input
	sub := p.open
	n := len(sub.Episodes)
	switch {
	case in.Repeat(ActUp):
		p.epSel = max(0, p.epSel-1)
	case in.Repeat(ActDown):
		p.epSel = min(max(0, n-1), p.epSel+1)
	case in.Repeat(ActLeft):
		p.epSel = max(0, p.epSel-10)
	case in.Repeat(ActRight):
		p.epSel = min(max(0, n-1), p.epSel+10)
	case in.Pressed(ActBack):
		p.open = nil
	case in.Pressed(ActConfirm) && n > 0:
		a.podPlay(sub.Show, sub.Episodes[p.epSel])
	case in.Pressed(ActMenu) && n > 0:
		a.episodeMenu(sub, sub.Episodes[p.epSel])
	case in.Pressed(ActMenu):
		a.showMenu(sub.Show)
	}
}

// episodeMenu es el menú (Start) de un episodio.
func (a *App) episodeMenu(sub *subscription, e podcast.Episode) {
	path := a.epPath(sub.Show, e)
	m := &menu{title: e.Title, subtitle: sub.Show.Title}
	m.items = append(m.items, menuItem{label: T("ytm_listen"), icon: "play", do: func() { a.podPlay(sub.Show, e) }})
	if a.downloaded(sub.Show, e) {
		m.items = append(m.items, menuItem{label: T("pd_delete"), icon: "trash", danger: true, do: func() {
			for _, f := range []string{path, path + ".jpg", path + ".tags.json"} {
				_ = os.Remove(f)
			}
			a.flash(T("pd_deleted"))
		}})
	} else {
		m.items = append(m.items, menuItem{label: T("pd_download"), icon: "download", do: func() { a.podDownload(sub.Show, e, false) }})
	}
	done := a.pod.progress[path].Done
	label := T("pd_mark_done")
	if done {
		label = T("pd_mark_new")
	}
	m.items = append(m.items, menuItem{label: label, icon: "check", do: func() {
		pr := a.pod.progress[path]
		pr.Done = !done
		if !pr.Done {
			pr.Pos = 0
		}
		a.pod.progress[path] = pr
		a.pod.dirty = true
	}})
	if a.subOf(sub.Show.Feed) == nil {
		m.items = append(m.items, menuItem{label: T("pd_sub"), icon: "plus", do: func() { a.subscribe(sub.Show) }})
	}
	a.menu = m
}

// fmtDur es una duración en "1:02:03" o "12:34".
func fmtDur(sec int) string {
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// drawPodcasts pinta la pantalla de los podcasts.
func (a *App) drawPodcasts(dst *ebiten.Image) {
	p := &a.pod
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	x0, top := a.contentX(), topH+8
	if w >= 320 {
		for i := 0; i < ptCount; i++ {
			y := topH + 8 + i*16
			t := T(podTabKeys[i])
			switch {
			case i == p.tab && p.tabFocus && p.open == nil:
				panel(dst, margin-3, y-4, 72, 13, th.Accent)
				drawText(dst, t, SmallFace, margin+2, y, th.Bg)
			case i == p.tab:
				panel(dst, margin-3, y-4, 72, 13, mix(th.Bg2, th.Accent, 0.35))
				drawText(dst, t, SmallFace, margin+2, y, th.Text)
			default:
				drawText(dst, t, SmallFace, margin+2, y, th.Dim)
			}
		}
		rect(dst, margin+75, topH+5, 1, h-topH-bottomH-44, withAlpha(th.Dim, 0x40))
	}
	cw := w - x0 - margin
	bottom := h - bottomH - 36
	const rowH = 22
	title := T("scr_podcasts") + " · " + T(podTabKeys[p.tab])
	var hints []hint
	if p.open != nil {
		sub := p.open
		title = T("scr_podcasts") + " · " + sub.Show.Title
		drawTextW(dst, "< "+sub.Show.Title, SmallFace, x0, top, cw, th.Accent2)
		top += 12
		rows := max(1, (bottom-top)/rowH)
		n := len(sub.Episodes)
		first := max(0, min(p.epSel-rows/2, n-rows))
		for i := 0; i < rows && first+i < n; i++ {
			e := sub.Episodes[first+i]
			y := top + 3 + i*rowH
			if first+i == p.epSel {
				panel(dst, x0-3, y-3, cw+6, rowH-2, mix(th.Bg2, th.Accent, 0.35))
				rect(dst, x0-3, y-3, 2, rowH-2, th.Accent)
			}
			path := a.epPath(sub.Show, e)
			pr := a.pod.progress[path]
			ic, icCol := "mic", th.Dim
			state := ""
			switch {
			case pr.Done:
				ic, icCol, state = "check", th.Accent2, T("pd_st_done")
			case pr.Pos > 10:
				ic, icCol = "play", th.Accent
				state = T("pd_st_partial", fmtDur(int(pr.Pos)))
			case a.downloaded(sub.Show, e):
				ic, icCol, state = "download", th.Accent, T("pd_st_down")
			}
			drawIcon(dst, ic, x0, y, icCol)
			txtCol := th.Text
			if pr.Done {
				txtCol = th.Dim
			}
			drawTextW(dst, e.Title, SmallFace, x0+11, y, cw-11, txtCol)
			sub := []string{}
			if !e.Date.IsZero() {
				sub = append(sub, e.Date.Format("2006-01-02"))
			}
			if e.Duration > 0 {
				sub = append(sub, fmtDur(e.Duration))
			}
			if state != "" {
				sub = append(sub, state)
			}
			drawTextW(dst, strings.Join(sub, " · "), SmallFace, x0+11, y+9, cw-11, th.Dim)
		}
		a.scrollBar(dst, x0+cw+4, top, bottom-top, first, rows, n)
		if n == 0 {
			drawText(dst, T("empty"), SmallFace, x0+(cw-textWidth(T("empty"), SmallFace))/2, top+30, th.Dim)
		}
		hints = []hint{{"A", T("h_listen")}, {"START", T("h_more")}, {"B", T("h_back")}}
	} else {
		if w < 320 {
			drawText(dst, T(podTabKeys[p.tab]), SmallFace, x0, top, th.Accent)
			top += 13
		}
		switch {
		case p.tab == ptTop:
			drawTextW(dst, T("pd_top_of", countryName(a.country())), SmallFace, x0, top, cw, th.Accent2)
			top += 12
		case p.tab == ptSearch && p.query != "":
			drawTextW(dst, "\""+p.query+"\"", SmallFace, x0, top, cw, th.Accent2)
			top += 12
		}
		list := p.lists[p.tab]
		rows := max(1, (bottom-top)/rowH)
		n := len(list)
		sel := p.sel[p.tab]
		first := max(0, min(sel-rows/2, n-rows))
		for i := 0; i < rows && first+i < n; i++ {
			s := list[first+i]
			y := top + 3 + i*rowH
			if first+i == sel && !p.tabFocus {
				panel(dst, x0-3, y-3, cw+6, rowH-2, mix(th.Bg2, th.Accent, 0.35))
				rect(dst, x0-3, y-3, 2, rowH-2, th.Accent)
			}
			icCol := th.Accent
			if a.subOf(s.Feed) != nil {
				icCol = th.Accent2
			}
			drawIcon(dst, "mic", x0, y, icCol)
			drawTextW(dst, s.Title, SmallFace, x0+11, y, cw-11, th.Text)
			drawTextW(dst, s.Author, SmallFace, x0+11, y+9, cw-11, th.Dim)
		}
		a.scrollBar(dst, x0+cw+4, top, bottom-top, first, rows, n)
		msg := ""
		switch {
		case p.loading[p.tab]:
			msg = T("pd_loading")
		case n == 0 && p.tab == ptSubs:
			msg = T("pd_no_subs")
		case n == 0 && p.tab == ptSearch:
			msg = T("pd_search_hint")
		case n == 0:
			msg = T("empty")
		}
		if msg != "" {
			drawTextW(dst, msg, SmallFace, x0+max(0, (cw-textWidth(msg, SmallFace))/2), top+30, cw, th.Dim)
		}
		hints = []hint{{"A", T("pd_h_open")}, {"START", T("h_more")}, {"SEL", T("h_menu")}}
		if p.tab == ptSearch {
			hints = append(hints[:2], hint{"X", T("rd_search")}, hints[2])
		}
	}
	a.drawMiniPlayer(dst)
	a.drawTopBar(dst, title)
	a.drawHints(dst, hints)
}
