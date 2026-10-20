package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/modsrc"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
)

// Los módulos: una pantalla de la barra de Select con los archivos de música
// de tracker y de consolas de la red (The Mod Archive, Modland, VGMRips y
// AMP), una pestaña por sitio. Cada una empieza con lo que el sitio ofrece
// para explorar (lo más bajado, al azar, lo nuevo) y X busca. A escucha
// (baja el archivo a data/cache/modules y lo pone; un pack de VGMRips pone
// todas sus pistas); Start: guardarlo en la biblioteca, abrirlo en el
// tracker (MOD y XM), más del mismo autor, o explorar de otra forma.

// modState es la pantalla de los módulos.
type modState struct {
	srcs     []modsrc.Source
	tab      int
	tabFocus bool
	sel      []int
	lists    [][]modsrc.Item
	what     []string // lo que muestra cada pestaña ("top", "random", "new", la búsqueda o el autor)
	loading  []bool
	scr      []scroller
	results  chan modResult
	online   chan []modsrc.Item // lo encontrado para el tracker (trackerOnlineSearch)
	started  bool
	// La búsqueda de Buscar (el filtro Módulos): en todos los sitios a la vez.
	squery   string
	sitems   []modsrc.Item
	spending bool
	sdone    chan modSearchDone
}

// modSearchDone es lo que vuelve de buscar en todos los sitios.
type modSearchDone struct {
	query string
	items []modsrc.Item
}

// modResult es una lista que llegó de un sitio.
type modResult struct {
	tab   int
	what  string
	items []modsrc.Item
	err   error
}

// modBrowse son las formas de explorar, en el orden en que se prueban al
// abrir una pestaña.
var modBrowse = []string{"top", "new", "random"}

func (a *App) modCacheDir() string { return filepath.Join(a.paths.Data, "cache", "modules") }

// modInit prepara los sitios (la primera vez que se abre la pantalla).
func (a *App) modInit() {
	m := &a.mods
	if m.started {
		return
	}
	m.started = true
	m.srcs = modsrc.Sources(filepath.Join(a.paths.Data, "cache", "modsrc"))
	n := len(m.srcs)
	m.sel, m.lists, m.what, m.loading, m.scr = make([]int, n), make([][]modsrc.Item, n), make([]string, n), make([]bool, n), make([]scroller, n)
	m.results = make(chan modResult, 8)
	m.online = make(chan []modsrc.Item, 1)
	m.sdone = make(chan modSearchDone, 4)
	m.tabFocus = true
	a.modExplore(0, "")
}

// modExplore llena una pestaña con la primera forma de explorar que el sitio
// tenga (o con what, si se pidió una).
func (a *App) modExplore(tab int, what string) {
	m := &a.mods
	src := m.srcs[tab]
	m.loading[tab] = true
	go func() {
		ctx := context.Background()
		ways := modBrowse
		if what != "" {
			ways = []string{what}
		}
		r := modResult{tab: tab}
		for _, w := range ways {
			r.what = w
			r.items, r.err = src.Browse(ctx, w)
			if !errors.Is(r.err, modsrc.ErrUnsupported) {
				break
			}
		}
		if errors.Is(r.err, modsrc.ErrUnsupported) {
			r.err, r.what = nil, "" // el sitio solo busca: la pestaña invita a buscar
		}
		m.results <- r
		ebiten.ScheduleFrame()
	}()
}

// modSearch busca en el sitio de la pestaña.
func (a *App) modSearch(tab int, q string) {
	m := &a.mods
	src := m.srcs[tab]
	m.loading[tab] = true
	go func() {
		items, err := src.Search(context.Background(), q)
		m.results <- modResult{tab: tab, what: "\"" + q + "\"", items: items, err: err}
		ebiten.ScheduleFrame()
	}()
}

// modArtist lista lo de un autor en el sitio de la pestaña.
func (a *App) modArtist(tab int, artist string) {
	m := &a.mods
	src := m.srcs[tab]
	m.loading[tab] = true
	go func() {
		items, err := src.ByArtist(context.Background(), artist)
		m.results <- modResult{tab: tab, what: artist, items: items, err: err}
		ebiten.ScheduleFrame()
	}()
}

// pollModules atiende las listas que llegan.
func (a *App) pollModules() {
	m := &a.mods
	if !m.started {
		return
	}
	for {
		select {
		case items := <-m.online:
			a.trackerOnlineMenu(items)
		case r := <-m.sdone:
			if r.query == m.squery {
				m.sitems, m.spending = r.items, false
				a.search.lastQuery = "" // vuelve a juntar los resultados de Buscar
				a.updateSearchResults()
			}
		case r := <-m.results:
			m.loading[r.tab] = false
			if r.err != nil {
				a.flash(T("md_error", r.err))
				continue
			}
			m.lists[r.tab], m.what[r.tab], m.sel[r.tab] = r.items, r.what, 0
		default:
			return
		}
	}
}

// updateModules atiende la pantalla de los módulos.
func (a *App) updateModules() {
	a.modInit()
	m, in := &a.mods, &a.input
	n := len(m.srcs)
	if m.tabFocus {
		switch {
		case in.Repeat(ActUp):
			m.tab = (m.tab + n - 1) % n
		case in.Repeat(ActDown):
			m.tab = (m.tab + 1) % n
		case in.Pressed(ActRight), in.Pressed(ActConfirm):
			m.tabFocus = false
		case in.Pressed(ActShuffle):
			a.modAskSearch()
		case in.Pressed(ActBack):
			a.goBack()
			return
		}
		if m.lists[m.tab] == nil && !m.loading[m.tab] && m.what[m.tab] == "" {
			m.lists[m.tab] = []modsrc.Item{} // una vez: explorar al llegar
			a.modExplore(m.tab, "")
		}
		return
	}
	list := m.lists[m.tab]
	sel := &m.sel[m.tab]
	switch {
	case in.Repeat(ActUp):
		if *sel == 0 {
			m.tabFocus = true
		} else {
			*sel--
		}
	case in.Repeat(ActDown):
		*sel = min(max(0, len(list)-1), *sel+1)
	case in.Pressed(ActLeft), in.Pressed(ActBack):
		m.tabFocus = true
	case in.Pressed(ActShuffle):
		a.modAskSearch()
	case in.Pressed(ActConfirm) && len(list) > 0:
		a.modListen(list[*sel])
	case in.Pressed(ActMenu):
		a.modMenu()
	}
}

// modAskSearch pide qué buscar en el sitio de la pestaña.
func (a *App) modAskSearch() {
	m := &a.mods
	tab := m.tab
	a.openRawPrompt(T("md_search_q", m.srcs[tab].Name()), "", false, func(q string) {
		if q = strings.TrimSpace(q); q != "" {
			m.tabFocus = false
			a.modSearch(tab, q)
		}
	})
}

// modMenu es el menú de Start: lo del elegido y las formas de explorar.
func (a *App) modMenu() {
	m := &a.mods
	tab := m.tab
	var items []menuItem
	list := m.lists[tab]
	if !m.tabFocus && len(list) > 0 {
		it := list[m.sel[tab]]
		items = append(items, a.modItemActions(it)...)
		if it.Artist != "" {
			items = append(items, menuItem{label: T("md_more_by", it.Artist), icon: "user", do: func() { a.modArtist(tab, it.Artist) }})
		}
	}
	items = append(items, menuItem{label: T("rd_search"), icon: "search", do: a.modAskSearch})
	for _, w := range modBrowse {
		items = append(items, menuItem{label: T("md_browse_" + w), icon: "list", do: func() { a.modExplore(tab, w) }})
	}
	a.menu = &menu{title: m.srcs[tab].Name(), subtitle: a.modSubtitle(tab), items: items}
}

// modSubtitle es lo que muestra una pestaña, en palabras.
func (a *App) modSubtitle(tab int) string {
	switch w := a.mods.what[tab]; w {
	case "top", "new", "random":
		return T("md_browse_" + w)
	default:
		return w
	}
}

// modFetch baja un archivo (o un pack) como una descarga más, con su
// progreso en Descargas; after recibe los archivos que se pueden tocar.
func (a *App) modFetch(it modsrc.Item, after func(files []string)) {
	dir := filepath.Join(a.modCacheDir(), it.Source)
	var files []string
	j := &ytJob{track: ytm.Track{ID: it.Source + ":" + it.ID, Title: it.Title, Artists: []string{orDefault(it.Artist, it.Source)}}, podcast: true, state: jobWaiting}
	j.run = func(ctx context.Context, progress func(ytm.Progress)) (string, error) {
		progress(ytm.Progress{Stage: "resolve"})
		fs, err := modsrc.Fetch(ctx, it, dir, func(done, total int64) {
			progress(ytm.Progress{Stage: "download", Done: done, Total: total})
		})
		if err != nil {
			return "", err
		}
		if len(fs) == 0 {
			return "", errors.New("nothing playable in the download")
		}
		files = fs
		return fs[0], nil
	}
	j.after = func(string) { after(files) }
	a.addJob(j)
	a.yt.queue <- j
	a.flash(T("ytm_queued", it.Title))
}

// modListen baja y pone un archivo (o todas las pistas de un pack).
func (a *App) modListen(it modsrc.Item) {
	if !modsrc.Playable(it.Format) {
		a.flash(T("md_unplayable", it.Format))
		return
	}
	a.modFetch(it, func(files []string) {
		a.send("load", append([]string{"0"}, files...)...)
		a.screen = NowPlaying
	})
}

// modSave baja un archivo y lo copia a la biblioteca: a "audiofilus
// modules/<autor>/" de la carpeta de música, y la biblioteca se pone al día.
func (a *App) modSave(it modsrc.Item) {
	if len(a.cfg.MusicDirs) == 0 {
		a.flash(T("tk_no_folder"))
		return
	}
	dst := filepath.Join(a.cfg.MusicDirs[0], "audiofilus modules", remote.Slug(orDefault(it.Artist, it.Source)))
	a.modFetch(it, func(files []string) {
		n := 0
		for _, f := range files {
			if copyFile(f, filepath.Join(dst, filepath.Base(f))) == nil {
				n++
			}
		}
		a.flash(T("md_saved", n))
		if a.yt.bgScan == nil {
			a.yt.bgScan = StartScan(a.paths, a.cfg.MusicDirs)
		}
	})
}

// modToTracker baja un MOD o XM y lo abre en el tracker como canción nueva.
func (a *App) modToTracker(it modsrc.Item) {
	a.modFetch(it, func(files []string) {
		if a.tracker == nil {
			a.tracker = a.newTracker()
		}
		a.screen = Tracker
		a.importTracker(files[0])
	})
}

// copyFile copia src a dst (creando su carpeta).
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst + ".part")
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(dst+".part", dst)
}

// drawModules pinta la pantalla: los sitios a la izquierda y la lista.
func (a *App) drawModules(dst *ebiten.Image) {
	a.modInit()
	m := &a.mods
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	x0, top := a.contentX(), topH+8
	names := make([]string, len(m.srcs))
	for i, s := range m.srcs {
		names[i] = s.Name()
	}
	pickTab := func(i int) { m.tab, m.tabFocus = i, false }
	if w >= 320 {
		for i, name := range names {
			y := topH + 8 + i*16
			a.tapZone(margin-3, y-4, 72, 13, func() { pickTab(i) })
			switch {
			case i == m.tab && m.tabFocus:
				panel(dst, margin-3, y-4, 72, 13, th.Accent)
				drawTextW(dst, name, SmallFace, margin+2, y, 66, th.Bg)
			case i == m.tab:
				panel(dst, margin-3, y-4, 72, 13, mix(th.Bg2, th.Accent, 0.35))
				drawTextW(dst, name, SmallFace, margin+2, y, 66, th.Text)
			default:
				drawTextW(dst, name, SmallFace, margin+2, y, 66, th.Dim)
			}
		}
		rect(dst, margin+75, topH+5, 1, h-topH-bottomH-44, withAlpha(th.Dim, 0x40))
	}
	cw := w - x0 - margin
	bottom := h - bottomH - 36
	const rowH = 22
	if w < 320 && a.input.touch {
		top += a.tabsButton(dst, x0, top, cw, names, m.tab, pickTab)
	} else if w < 320 {
		drawText(dst, names[m.tab], SmallFace, x0, top, th.Accent)
		top += 13
	}
	if sub := a.modSubtitle(m.tab); sub != "" {
		drawTextW(dst, sub, SmallFace, x0, top, cw, th.Accent2)
		top += 12
	}
	list := m.lists[m.tab]
	rows := max(1, (bottom-top)/rowH)
	n := len(list)
	first, off, clip := a.touchList(dst, &m.scr[m.tab], &m.sel[m.tab], n, x0-3, top-1, cw+6, bottom-top, rowH, func() {
		m.tabFocus = false
		a.input.inject(ActConfirm)
	})
	whole := dst
	dst = clip
	for i := 0; i <= rows && first+i < n; i++ {
		it := list[first+i]
		y := top + 3 + i*rowH - off
		if first+i == m.sel[m.tab] && !m.tabFocus {
			panel(dst, x0-3, y-3, cw+6, rowH-2, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x0-3, y-3, 2, rowH-2, th.Accent)
		}
		icCol := th.Accent
		if !modsrc.Playable(it.Format) {
			icCol = th.Dim
		}
		ic := "note"
		if it.Pack {
			ic = "disc"
		}
		drawIcon(dst, ic, x0, y, icCol)
		drawTextW(dst, it.Title, SmallFace, x0+11, y, cw-11, th.Text)
		sub := []string{}
		if it.Artist != "" {
			sub = append(sub, it.Artist)
		}
		if it.Format != "" {
			sub = append(sub, it.Format)
		}
		if it.Size > 0 {
			sub = append(sub, fmt.Sprintf("%d KB", (it.Size+1023)/1024))
		}
		drawTextW(dst, strings.Join(sub, " · "), SmallFace, x0+11, y+9, cw-11, th.Dim)
	}
	dst = whole
	a.scrollBar(dst, x0+cw+4, top, bottom-top, first, rows, n)
	msg := ""
	switch {
	case m.loading[m.tab]:
		msg = T("pd_loading")
	case n == 0:
		msg = T("md_hint")
	}
	if msg != "" {
		drawTextW(dst, msg, SmallFace, x0+max(0, (cw-textWidth(msg, SmallFace))/2), top+30, cw, th.Dim)
	}
	a.drawMiniPlayer(dst)
	a.drawTopBar(dst, T("scr_modules")+" · "+names[m.tab])
	a.drawHints(dst, []hint{{"A", T("h_listen")}, {"X", T("h_search")}, {"START", T("h_more")}, {"SEL", T("h_menu")}})
}

// trackerOnlineSearch busca módulos para el tracker en The Mod Archive y
// Modland (solo MOD y XM, los que el tracker importa) y los ofrece en un menú.
func (a *App) trackerOnlineSearch() {
	a.modInit()
	m := &a.mods
	a.openRawPrompt(T("md_search_q", "MOD · XM"), "", false, func(q string) {
		if q = strings.TrimSpace(q); q == "" {
			return
		}
		a.flash(T("tk_searching"))
		srcs := m.srcs
		go func() {
			var out []modsrc.Item
			for _, s := range srcs {
				items, err := s.Search(context.Background(), q)
				if err != nil {
					continue
				}
				for _, it := range items {
					if f := strings.ToUpper(it.Format); (f == "MOD" || f == "XM") && !it.Pack {
						out = append(out, it)
					}
				}
			}
			m.online <- out
			ebiten.ScheduleFrame()
		}()
	})
}

// trackerOnlineMenu ofrece lo encontrado; elegir uno lo baja y lo importa.
func (a *App) trackerOnlineMenu(items []modsrc.Item) {
	if len(items) == 0 {
		a.flash(T("md_none"))
		return
	}
	var menuItems []menuItem
	for _, it := range items {
		label := it.Title
		if it.Artist != "" {
			label += " · " + it.Artist
		}
		menuItems = append(menuItems, menuItem{label: label, icon: "note", do: func() { a.modToTracker(it) }})
	}
	a.menu = &menu{title: T("tk_import_online"), subtitle: fmt.Sprintf("%d · MOD XM", len(items)), items: menuItems}
}

// modItemActions son lo que se puede hacer con un módulo: escucharlo,
// guardarlo en la biblioteca y, si es MOD o XM, abrirlo en el tracker.
func (a *App) modItemActions(it modsrc.Item) []menuItem {
	items := []menuItem{
		{label: T("h_listen"), icon: "play", do: func() { a.modListen(it) }},
		{label: T("md_save"), icon: "download", do: func() { a.modSave(it) }},
	}
	if f := strings.ToUpper(it.Format); (f == "MOD" || f == "XM") && !it.Pack {
		items = append(items, menuItem{label: T("md_tracker"), icon: "tracker", do: func() { a.modToTracker(it) }})
	}
	return items
}

// modItemMenu es el menú de un módulo encontrado en Buscar.
func (a *App) modItemMenu(it modsrc.Item) {
	sub := it.Source
	if it.Artist != "" {
		sub = it.Artist + " · " + it.Source
	}
	a.menu = &menu{title: it.Title, subtitle: sub, items: a.modItemActions(it)}
}

// modSearchAll busca q en todos los sitios a la vez (para Buscar); lo que
// llega se junta en el orden de los sitios.
func (a *App) modSearchAll(q string) {
	a.modInit()
	m := &a.mods
	q = strings.TrimSpace(q)
	if q == "" || q == m.squery {
		return
	}
	m.squery, m.sitems, m.spending = q, nil, true
	srcs := m.srcs
	go func() {
		lists := make([][]modsrc.Item, len(srcs))
		var wg sync.WaitGroup
		for i, s := range srcs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				items, err := s.Search(ctx, q)
				if err != nil {
					log.Printf("modules search %s: %v", s.Name(), err)
					return
				}
				for _, it := range items {
					if modsrc.Playable(it.Format) {
						lists[i] = append(lists[i], it)
					}
				}
				if len(lists[i]) > 25 {
					lists[i] = lists[i][:25]
				}
			}()
		}
		wg.Wait()
		var out []modsrc.Item
		for _, l := range lists {
			out = append(out, l...)
		}
		m.sdone <- modSearchDone{q, out}
		ebiten.ScheduleFrame()
	}()
}

// modResults son los módulos encontrados para q, como resultados de Buscar.
func (a *App) modResults(q string) []searchResult {
	m := &a.mods
	if strings.TrimSpace(q) != m.squery {
		return nil
	}
	var out []searchResult
	for _, it := range m.sitems {
		sub := it.Format
		if it.Artist != "" {
			sub = it.Artist + " · " + it.Format
		}
		out = append(out, searchResult{kind: resMod, main: it.Title, sub: sub, mod: it})
	}
	return out
}
