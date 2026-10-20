package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// La barra de pantallas: Select la abre, con una fila de iconos (una por
// pantalla, y Buscar). Al abrirse ya está sobre la pantalla de antes, así
// que Select dos veces salta entre las dos últimas, como Alt+Tab. Con ←/→ se
// elige, Select o A van y B cierra. B en Cola, Ajustes y Visualizador vuelve
// a la pantalla de antes (goBack).

// dockEntry es un hueco de la barra.
type dockEntry struct {
	screen Screen
	search bool // Buscar: la biblioteca, escribiendo
	icon   string
	label  string // clave de lang.go
	source int    // en la biblioteca: 0 la tarjeta, i>0 el servidor i-1
	name   string // el nombre, si no sale de lang.go (un servidor)
}

// dockEntries son los huecos, en orden: la música, lo visual, los juegos y
// los ajustes.
var dockEntries = []dockEntry{
	{screen: NowPlaying, search: false, icon: "disc", label: "scr_now"},
	{screen: LibraryScreen, search: false, icon: "folder", label: "scr_library"},
	{screen: LibraryScreen, search: true, icon: "search", label: "tab_search"},
	{screen: Radio, search: false, icon: "radio", label: "scr_radio"},
	{screen: Podcasts, search: false, icon: "mic", label: "scr_podcasts"},
	{screen: Modules, search: false, icon: "tracker", label: "scr_modules"},
	{screen: Queue, search: false, icon: "list", label: "scr_queue"},
	{screen: Visual, search: false, icon: "visual", label: "scr_visual"},
	{screen: Downloads, search: false, icon: "download", label: "ytm_jobs"},
	{screen: Games, search: false, icon: "pad", label: "scr_games"},
	{screen: Settings, search: false, icon: "gear", label: "scr_settings"},
}

// dockState es la barra abierta o cerrada.
type dockState struct {
	open bool
	sel  int
	at   float64 // cuándo se abrió (la entrada)
}

// dockIndex es el hueco de una pantalla (el primero que la lleva).
func dockIndex(s Screen) int {
	for i, e := range dockEntries {
		if e.screen == s {
			return i
		}
	}
	return 0
}

// docks son los huecos que se ven: Descargas, solo con yt-dlp.
// Los servidores van cada uno con su hueco tras la biblioteca.
func (a *App) docks() []dockEntry {
	var out []dockEntry
	for _, e := range dockEntries {
		if e.screen == Downloads && a.yt.bin == "" && len(a.remotes) == 0 {
			continue
		}
		out = append(out, e)
		if e.screen == LibraryScreen && !e.search {
			for i, r := range a.remotes {
				out = append(out, dockEntry{screen: LibraryScreen, icon: "server", source: i + 1, name: r.srv.Name})
			}
		}
	}
	return out
}

// dockLabel es el nombre de un hueco.
func dockLabel(e dockEntry) string {
	if e.name != "" {
		return e.name
	}
	return T(e.label)
}

// openDock abre la barra sobre la pantalla de antes (o, si no la hay, sobre
// el reproductor; desde el reproductor, sobre la biblioteca).
func (a *App) openDock() {
	prev := a.prevScreen
	if prev == a.screen {
		prev = NowPlaying
		if a.screen == NowPlaying {
			prev = LibraryScreen
		}
	}
	sel := 0
	for i, e := range a.docks() {
		if (e.screen == prev || e.screen == Games && isGame(prev)) && !e.search {
			sel = i
			break
		}
	}
	a.dock = dockState{open: true, sel: sel, at: a.t}
}

// updateDock atiende Select y la barra. Devuelve true si se comió el
// fotograma.
func (a *App) updateDock() bool {
	in := &a.input
	d := &a.dock
	if !d.open {
		if in.Pressed(ActSelect) && a.menu == nil {
			a.openDock()
			return true
		}
		return false
	}
	n := len(a.docks())
	switch {
	case in.Pressed(ActSelect), in.Pressed(ActConfirm):
		a.goDock(a.docks()[d.sel])
	case in.Pressed(ActBack), in.Pressed(ActMenu):
		d.open = false
	case in.Repeat(ActLeft), in.Repeat(ActUp):
		d.sel = (d.sel + n - 1) % n
	case in.Repeat(ActRight), in.Repeat(ActDown):
		d.sel = (d.sel + 1) % n
	}
	return true
}

// goDock va a lo elegido en la barra y la cierra.
func (a *App) goDock(e dockEntry) {
	a.dock.open = false
	a.saveTetBest()
	a.saveRhythmBest()
	a.saveCryptBest()
	a.screen, a.tabFocus = e.screen, false
	if e.screen == LibraryScreen && !e.search {
		a.setSource(e.source)
	}
	if e.search {
		a.libTab = tabSearch
		a.search.typing = true
	}
}

// goBack vuelve a la pantalla de antes (B en las pantallas que no tienen
// otro "atrás"); si no la hay, al reproductor.
func (a *App) goBack() {
	a.saveTetBest()
	a.saveRhythmBest()
	a.saveCryptBest()
	to := a.prevScreen
	if to == a.screen {
		to = NowPlaying
	}
	a.screen, a.tabFocus = to, false
}

// drawDock pinta la barra: el fondo oscurecido, el nombre de lo elegido y
// una fila de iconos al doble que sube desde abajo con un rebote corto. La
// pantalla actual lleva un punto debajo.
func (a *App) drawDock(dst *ebiten.Image) {
	d := &a.dock
	if !d.open {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	n := len(a.docks())
	tw := min(34, (w-16)/n)
	const tileH = 30
	bw := n*tw + 8
	bx := (w - bw) / 2
	by := h - bottomH - tileH - 14
	if p := (a.t - d.at) / 0.14; p < 1 && !a.offline {
		by += int(math.Round(float64(tileH+bottomH+14) * (1 - easeOutBack(math.Max(0, p)))))
	}
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0x90))
	// Tocar fuera la cierra; tocar una pantalla va a ella.
	a.tapZone(0, 0, w, h, func() { a.input.inject(ActBack) })
	for i := range a.docks() {
		a.tapZone(bx+4+i*tw, by, tw, tileH+8, func() { d.sel = i; a.input.inject(ActSelect) })
	}
	rect(dst, bx+2, by+2, bw, tileH+8, th.Shadow)
	panel(dst, bx, by, bw, tileH+8, th.Bg2)
	rect(dst, bx+1, by, bw-2, 1, th.Accent)
	for i, e := range a.docks() {
		x := bx + 4 + i*tw
		y := by + 4
		sel := i == d.sel
		cur := (e.screen == a.screen || e.screen == Games && isGame(a.screen)) && (!e.search || a.libTab == tabSearch) && (e.search || e.screen != LibraryScreen || a.libTab != tabSearch && e.source == a.source)
		ic := th.Dim
		if sel {
			panel(dst, x+1, y, tw-2, tileH, th.Accent)
			ic = th.Bg
		} else if cur {
			ic = th.Text
		}
		img := icon(e.icon)
		iw, ih := img.Bounds().Dx(), img.Bounds().Dy()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(2, 2)
		op.GeoM.Translate(float64(x+(tw-2*iw)/2), float64(y+(tileH-2*ih)/2-2))
		op.ColorScale.ScaleWithColor(ic)
		dst.DrawImage(img, op)
		if cur {
			dc := th.Accent
			if sel {
				dc = th.Bg
			}
			rect(dst, x+tw/2-1, y+tileH-5, 3, 2, dc)
		}
	}
	// El nombre de lo elegido, encima de la barra.
	name := dockLabel(a.docks()[d.sel])
	drawText(dst, name, SmallFace, (w-textWidth(name, SmallFace))/2+1, by-11, th.Shadow)
	drawText(dst, name, SmallFace, (w-textWidth(name, SmallFace))/2, by-12, th.Text)
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg)
	a.drawHints(dst, []hint{{"<>", T("h_choose")}, {"SEL", T("h_go")}, {"B", T("h_close")}})
}

// openVisualMenu es el menú "más" del visualizador: todos los
// visualizadores, la cámara o variante y la música.
func (a *App) openVisualMenu() {
	v := &a.vis
	m := &menu{title: T("vis_title"), subtitle: T(visNames[v.kind])}
	for k := 0; k < visCount; k++ {
		ic := "visual"
		if k == visDrift {
			ic = "car"
		}
		m.items = append(m.items, menuItem{label: T(visNames[k]), icon: ic,
			do:     func() { v.kind, v.switchAt = k, a.t },
			active: func() bool { return v.kind == k }})
	}
	m.items = append(m.items, menuItem{label: T("h_pause"), icon: "pause", do: func() { a.send("toggle") }})
	a.menu = m
}
