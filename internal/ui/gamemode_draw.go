package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// El dibujo del modo juego (gamemode.go). Con la cara de Audiofilus: el fondo
// animado, las fuentes pixel y la paleta del disco que suena; a la izquierda
// la maqueta 3D de la consola girando, con la caja del juego elegido en su
// pantalla, su cartucho o su disco.

// drawGameMode pinta el modo juego.
func (a *App) drawGameMode(dst *ebiten.Image) {
	g := &a.gm
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	switch {
	case g.loading:
		msg := T("gx_loading")
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2, th.Accent)
		a.drawTopBar(dst, T("gx_title"))
	case g.err != "":
		drawTextWrap(dst, g.err, SmallFace, margin, h/2-10, w-2*margin, 10, th.Dim)
		a.drawTopBar(dst, T("gx_title"))
	case len(g.systems) == 0:
		msg := T("gx_empty")
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2, th.Dim)
		a.drawTopBar(dst, T("gx_title"))
	case g.screen == gmSystems:
		a.drawGMSystems(dst)
	case g.screen == gmGames:
		a.drawGMGames(dst)
	case g.screen == gmOptions:
		a.drawGMOptions(dst)
	case g.screen == gmSearch:
		a.drawGMSearch(dst)
	case g.screen == gmSaves:
		a.drawGMSaves(dst)
	case g.screen == gmViewer:
		a.drawGMViewer(dst)
	case g.screen == gmSysMenu:
		a.drawGMSystem(dst)
	case g.screen == gmInfo:
		a.drawGMInfo(dst)
	case g.screen == gmZoom2:
		a.drawGMZoom(dst)
	case g.screen == gmVideoFull:
		a.drawGMVideoFull(dst)
	}
}

// gmSpin es cuánto va de la vuelta que da la maqueta al cambiar (0..1).
func (a *App) gmSpin() float64 { return (a.t - a.gm.changeAt) / 0.7 }

// gmCarStep es por dónde va el carrusel de n tras dt segundos, desde pos y
// persiguiendo a sel por el camino corto (da la vuelta).
func gmCarStep(pos float64, sel, n int, dt float64) float64 {
	diff := gmWrap(float64(sel)-pos, n)
	if math.Abs(diff) > 2.5 { // un salto (la búsqueda, al volver): sin viaje
		pos = float64(sel)
		diff = 0
	}
	pos += diff * math.Min(1, dt*16)
	// Nunca más de una posición detrás: dejando el botón apretado, sel se
	// adelantaba más rápido de lo que el carrusel lo alcanzaba, y la
	// elegida quedaba a un lado con el centro vacío.
	if d := diff * (1 - math.Min(1, dt*16)); d > 1 {
		pos = float64(sel) - 1
	} else if d < -1 {
		pos = float64(sel) + 1
	}
	// Siempre entre 0 y n: dando vueltas hacia la izquierda se iba a los
	// negativos, y tras más de una vuelta la elegida quedaba fuera.
	pos = math.Mod(pos, float64(n))
	if pos < 0 {
		pos += float64(n)
	}
	if math.Abs(gmWrap(float64(sel)-pos, n)) < 0.002 {
		pos = float64(sel)
	}
	return pos
}

// gmWrap lleva d (una distancia en el carrusel de n) a -n/2..n/2: el
// camino corto, dé las vueltas que dé.
func gmWrap(d float64, n int) float64 {
	d = math.Mod(d, float64(n))
	if d > float64(n)/2 {
		d -= float64(n)
	} else if d < -float64(n)/2 {
		d += float64(n)
	}
	return d
}

// gmConsoleBox es dónde va la maqueta: a la izquierda, bajo la barra.
func gmConsoleBox(w, h int) (x, y, cw, ch int) {
	cw = min(w*46/100, 200)
	ch = min(h-topH-bottomH-gmMiniH-38, 130)
	return margin - 2, topH + 6, cw, ch
}

// drawGMSystems: el carrusel de sistemas. La maqueta del elegido va grande
// en el centro; las vecinas, más chicas
// y apagadas a los lados, se deslizan al cambiar. Debajo, el nombre y los
// números del sistema.
func (a *App) drawGMSystems(dst *ebiten.Image) {
	g := &a.gm
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	n := len(g.systems)
	s := g.gmSys()
	a.drawGMBackdrop(dst)

	// El carrusel persigue a sel por el camino corto (da la vuelta).
	dt := math.Min(0.1, math.Max(0, a.t-g.carT))
	g.carT = a.t
	g.carPos = gmCarStep(g.carPos, g.sel, n, dt)

	areaY := topH + 4
	areaH := h - topH - bottomH - gmMiniH - 48
	bigW, bigH := min(w*52/100, 210), areaH
	spacing := float64(w) * 0.33
	cx := w / 2

	// Las vecinas primero (detrás), de la más lejana a la más cercana. La
	// del centro también, si aún no es la elegida (el carrusel la alcanza).
	for _, off := range []int{-2, 2, -1, 1, 0} {
		i := ((int(math.Round(g.carPos))+off)%n + n) % n
		if n < 2 || (n < 3 && off < 0) || i == g.sel {
			continue
		}
		rel := float64(off) - (g.carPos - math.Round(g.carPos))
		k := math.Min(1, math.Abs(rel))
		sc := 1 - 0.5*k
		bw, bh := int(float64(bigW)*sc), int(float64(bigH)*sc)
		x := cx + int(rel*spacing) - bw/2
		y := areaY + (bigH-bh)/2 + int(8*k)
		gs := g.systems[i]
		a.drawStillModel(dst, gs.sys.Name, gs.sys.Hardware, x, y, bw, bh, bigW, bigH, float32(1-0.55*k))
	}
	// La elegida, al frente (sin imagen en su pantalla: solo la maqueta).
	rel := gmWrap(float64(g.sel)-g.carPos, n)
	g.cv.models = g.cvModels()
	a.drawConsoleA(dst, &g.cv, s.sys.Name, s.sys.Hardware, cx+int(rel*spacing)-bigW/2, areaY, bigW, bigH, a.gmSpin(), nil, 0, 1)

	// Las flechas a los lados.
	ay := areaY + bigH/2 - 4
	pulse := int(2 * math.Sin(a.t*4))
	drawText(dst, "<", BigFace, margin+pulse*-1, ay, th.Dim)
	drawText(dst, ">", BigFace, w-margin-textWidth(">", BigFace)+pulse, ay, th.Dim)

	// El nombre, centrado, y debajo los números.
	ty := areaY + bigH + 2
	name := s.sys.FullName
	drawText(dst, name, BigFace, cx-textWidth(name, BigFace)/2, ty, th.Text)
	ty += 16
	var sub []string
	if s.sys.Manufacturer != "" {
		sub = append(sub, s.sys.Manufacturer)
	}
	if s.sys.Release != "" && s.sys.Release != "0" {
		sub = append(sub, s.sys.Release)
	}
	line := strings.Join(sub, " · ")
	drawText(dst, line, SmallFace, cx-textWidth(line, SmallFace)/2, ty, th.Dim)
	ty += 10
	stats := N("gx_n_games", len(s.games))
	if s.plays > 0 {
		stats += " · " + N("gx_n_plays", s.plays)
	}
	drawText(dst, stats, SmallFace, cx-textWidth(stats, SmallFace)/2, ty, th.Accent)
	ty += 11
	// Dónde va en la fila: una raya por sistema, la elegida más larga.
	const dw = 5
	tw := n*dw + (n-1)*2
	if tw <= w-2*margin {
		x0 := cx - tw/2
		for i := 0; i < n; i++ {
			c := th.Dim
			hh := 1
			if i == g.sel {
				c, hh = th.Accent, 2
			}
			rect(dst, x0+i*(dw+2), ty, dw, hh, c)
		}
	} else {
		pos := fmt.Sprintf("%d / %d", g.sel+1, n)
		drawText(dst, pos, SmallFace, cx-textWidth(pos, SmallFace)/2, ty-3, th.Dim)
	}

	a.drawGMNowPlaying(dst)
	a.drawTopBar(dst, T("gx_title"))
	a.drawHints(dst, []hint{{"<>", T("h_choose")}, {"A", T("h_open")}, {"X", T("gx_search_short")}, {"START", T("gx_options")}, {"SEL", T("gx_sys_short")}, {"FN", T("gx_fn_music")}})
}

// stillModel es una maqueta de los lados del carrusel ya dibujada: su
// imagen, de qué tamaño, con qué colores del tema (las de la paleta) y
// cuándo se dibujó y se usó por última vez.
type stillModel struct {
	img      *ebiten.Image
	w, h     int
	pal      [3]color.RGBA
	at, used float64
}

// gmStillMax es cuántas maquetas quietas se guardan dibujadas.
const gmStillMax = 10

// drawStillModel pinta quieta y tenue (alpha) la maqueta del sistema name
// en (x, y) y de w×h. Se dibuja una vez a refW×refH (su tamaño mayor) y
// luego solo se escala: al deslizarse, las de los lados cambian de tamaño en
// cada fotograma, y volver a dibujarlas cada vez se llevaba la CPU.
func (a *App) drawStillModel(dst *ebiten.Image, name, hardware string, x, y, w, h, refW, refH int, alpha float32) {
	g := &a.gm
	if w < 8 || h < 8 || refW < 8 || refH < 8 {
		return
	}
	g.stillCV.models = g.cvModels()
	m := g.stillCV.model(name, hardware)
	if g.stills == nil {
		g.stills = map[*consoleModel]*stillModel{}
	}
	pal := [3]color.RGBA{a.theme.Bg2, a.theme.Accent, a.theme.Text}
	sm := g.stills[m]
	// Las de la paleta siguen los colores del tema, pero sin volver a
	// dibujarse en cada fotograma mientras el color cambia.
	stale := sm != nil && m.col == nil && sm.pal != pal && a.t-sm.at > 0.25
	if sm == nil || sm.w != refW || sm.h != refH || stale {
		if sm == nil {
			if len(g.stills) >= gmStillMax {
				var old *consoleModel
				for k, v := range g.stills {
					if old == nil || v.used < g.stills[old].used {
						old = k
					}
				}
				g.stills[old].img.Deallocate()
				delete(g.stills, old)
			}
			sm = &stillModel{}
			g.stills[m] = sm
		}
		if sm.img == nil || sm.w != refW || sm.h != refH {
			if sm.img != nil {
				sm.img.Deallocate()
			}
			sm.img = newTarget(refW, refH)
		}
		out := a.renderModel(&g.stillCV, m, refW, refH, 0, nil, 0, true)
		sm.img.Clear()
		sm.img.DrawImage(out, nil)
		sm.w, sm.h, sm.pal, sm.at = refW, refH, pal, a.t
	}
	sm.used = a.t
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(w)/float64(refW), float64(h)/float64(refH))
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleAlpha(alpha)
	op.Filter = ebiten.FilterLinear // al deslizarse, sin parpadeo de píxeles
	dst.DrawImage(sm.img, op)
}

// cvModels son las maquetas ya armadas, compartidas por todas las vistas.
func (g *gameModeState) cvModels() map[string]*consoleModel {
	if g.cv.models == nil {
		g.cv.models = map[string]*consoleModel{}
	}
	return g.cv.models
}

// gmRow es una fila de las listas del modo juego.
type gmRow struct {
	main, sub string
	header    bool // un título de sección (no se elige)
}

// gmRowH es el alto de una fila de las listas.
const gmRowH = 13

// drawGMList pinta una lista con la fila sel a la vista y marcada como en
// la biblioteca; sub va a la derecha, en tenue.
func (a *App) drawGMList(dst *ebiten.Image, rows []gmRow, sel, x, y, w, h int) {
	th := a.theme
	n := len(rows)
	vis := max(1, h/gmRowH)
	first := max(0, min(sel-vis/2, n-vis))
	for i := 0; i < vis && first+i < n; i++ {
		idx := first + i
		r := rows[idx]
		ry := y + i*gmRowH
		mc, sc := th.Text, th.Dim
		if idx == sel {
			panel(dst, x-3, ry-4, w+6, gmRowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x-3, ry-4, 2, gmRowH, th.Accent)
			sc = th.Accent
		}
		sw := 0
		if r.sub != "" {
			sw = textWidth(r.sub, SmallFace)
			drawText(dst, r.sub, SmallFace, x+w-sw, ry, sc)
			sw += 6
		}
		drawTextW(dst, r.main, SmallFace, x+2, ry, w-sw-2, mc)
	}
	a.scrollBar(dst, x+w+4, y-3, h, first, vis, n)
}

// drawGMGames: los juegos del sistema; a la izquierda la caja del elegido
// (pixelada como las portadas) y sus datos, a la derecha la lista.
func (a *App) drawGMGames(dst *ebiten.Image) {
	g := &a.gm
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	s := g.gmSys()
	rs, gm := s.at(s.sel) // en Favoritos, el juego en su sistema
	a.drawGMGameBackdrop(dst, gm)
	cx, cy, cw, ch := gmConsoleBox(w, h)
	size := min(cw, ch)
	// Al cambiar de juego, la caja entra con un saltito.
	pop := 0.0
	if k := (a.t - g.changeAt) / 0.25; k < 1 {
		pop = (1 - k) * 4
	}
	cover, r, alpha := a.gmBoxCover(gm)
	switch {
	case gmFlat(rs):
		// Las capturas y los ports, en plano.
		bx, by := cx+(cw-size)/2, cy+(ch-size)/2+int(pop)
		a.drawGMFlat(dst, a.gmCover(gm), gm.Name, rs.sys.FullName, bx, by, size)
	case cover == nil && a.gmCoverLoading(gm, 96):
		// Mientras llega: la caja lisa con el relojito.
		a.drawGameBox(dst, &g.box, rs.sys.Name, nil, image.Rectangle{}, 0, cx, cy+int(pop), cw, ch, a.gmSpin())
		a.drawHourglass(dst, cx+cw/2, cy+ch/2+int(pop))
	case cover == nil:
		// Sin carátula: una hecha aquí, con el nombre.
		ph := a.gmPlaceholder(gm, &rs.sys)
		a.drawGameBox(dst, &g.box, rs.sys.Name, ph, ph.Bounds(), 1, cx, cy+int(pop), cw, ch, a.gmSpin())
	default:
		a.drawGameBox(dst, &g.box, rs.sys.Name, cover, r, alpha, cx, cy+int(pop), cw, ch, a.gmSpin())
	}
	// El logo del juego, si tiene, delante de la parte de abajo de la caja.
	a.drawGMLogo(dst, gm, cx+4, cy+ch-gmLogoH, cw-8, gmLogoH, true)

	ty := cy + ch + 4
	var info []string
	if y := gm.Year(); y != "" {
		info = append(info, y)
	}
	if gm.Developer != "" {
		info = append(info, gm.Developer)
	}
	drawTextW(dst, strings.Join(info, " · "), SmallFace, margin, ty, cw, th.Dim)
	ty += 10
	if gm.Genre != "" {
		drawTextW(dst, gm.Genre, SmallFace, margin, ty, cw, th.Dim)
		ty += 10
	}
	if gm.PlayCount > 0 {
		drawTextW(dst, N("gx_n_plays", gm.PlayCount), SmallFace, margin, ty, cw, th.Accent)
	} else {
		drawTextW(dst, T("gx_never"), SmallFace, margin, ty, cw, th.Accent)
	}

	lx := margin + cw + 10
	lw := w - lx - margin - 4
	rows := make([]gmRow, len(s.games))
	for i := range s.games {
		rs, rg := s.at(i)
		sub := ""
		switch {
		case s.isCollection(), s.group && rs.sys.Name != s.sys.Name:
			sub = rs.sys.Name // en Favoritos o un grupo, de qué sistema es
		case rg.Favorite:
			sub = "*"
		}
		rows[i] = gmRow{main: s.games[i].Name, sub: sub}
	}
	a.drawGMList(dst, rows, s.sel, lx, topH+8, lw, h-topH-bottomH-gmMiniH-12)
	a.drawGMNowPlaying(dst)
	a.drawTopBar(dst, fmt.Sprintf("%s · %d/%d", s.sys.FullName, s.sel+1, len(s.games)))
	a.drawHints(dst, []hint{{"A", T("gx_play")}, {"A ...", T("gx_info_short")}, {"X", T("h_random")}, {"SEL", T("gx_search_short")}, {"START", T("gx_options")}, {"B", T("h_back")}})
}

// gmLogoH es el alto mayor del logo de un juego en la lista y la ficha.
const gmLogoH = 30

// gmMiniH es el alto que ocupa abajo el mini reproductor (con su
// separación), que el modo juego deja libre en todas sus pantallas.
const gmMiniH = 34

// drawGMNowPlaying pinta, sobre la ayuda, lo que suena: el mismo mini
// reproductor que en la biblioteca (la música sigue mientras se eligen
// juegos).
func (a *App) drawGMNowPlaying(dst *ebiten.Image) {
	a.drawMiniPlayer(dst)
}

// drawGMOptions: un menú de opciones, a toda la pantalla: cada fila con su
// valor a la derecha, los títulos de sección, y abajo de qué va la elegida.
func (a *App) drawGMOptions(dst *ebiten.Image) {
	g := &a.gm
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	m := g.opts
	// Las filas, con un título donde empieza cada sección.
	var rows []gmRow
	selLine := 0
	group := ""
	for i, r := range m.rows {
		if r.Group != group && r.Group != "" {
			rows = append(rows, gmRow{main: esTr(r.Group), header: true})
		}
		group = r.Group
		if i == m.sel {
			selLine = len(rows)
		}
		rows = append(rows, gmRow{main: optLabel(r), sub: a.optValue(&r)})
	}
	descH := 24
	x, y := margin+4, topH+8
	lw := w - 2*margin - 12
	vis := max(1, (h-topH-bottomH-descH-12)/gmRowH)
	first := max(0, min(selLine-vis/2, len(rows)-vis))
	for i := 0; i < vis && first+i < len(rows); i++ {
		idx := first + i
		r := rows[idx]
		ry := y + i*gmRowH
		if r.header {
			drawText(dst, r.main, SmallFace, x, ry+1, th.Accent2)
			rect(dst, x+textWidth(r.main, SmallFace)+4, ry+2, lw-textWidth(r.main, SmallFace)-4, 1, withAlpha(th.Accent2, 0x55))
			continue
		}
		mc, vc := th.Dim, th.Accent
		vw := textWidth(r.sub, SmallFace)
		if idx == selLine {
			panel(dst, x-3, ry-4, lw+6, gmRowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x-3, ry-4, 2, gmRowH, th.Accent)
			mc, vc = th.Text, th.Text
			// Las flechas a los lados del valor, que laten.
			if k := m.rows[m.sel].Kind; k == esdata.OptChoice || k == esdata.OptEmulator {
				off := int(a.t*3) % 2
				drawText(dst, "<", SmallFace, x+lw-vw-10-off, ry, th.Accent)
				drawText(dst, ">", SmallFace, x+lw+2+off, ry, th.Accent)
			}
		}
		drawText(dst, r.sub, SmallFace, x+lw-vw-2, ry, vc)
		drawTextW(dst, r.main, SmallFace, x+2, ry, lw-vw-20, mc)
	}
	a.scrollBar(dst, w-margin, y-3, vis*gmRowH, first, vis, len(rows))

	// De qué va la elegida.
	if len(m.rows) > 0 {
		r := m.rows[m.sel]
		desc := esTr(r.Description)
		if r.Inherited != "" && r.Current(g.conf) == 0 {
			if desc != "" {
				desc += " "
			}
			desc += T("gx_inherited", valueName(r.Inherited))
		}
		dy := h - bottomH - descH - 2
		rect(dst, 0, dy-3, w, descH+2, withAlpha(th.Shadow, 0x88))
		drawTextWrap(dst, desc, SmallFace, margin, dy+2, w-2*margin, 10, th.Dim)
	}

	title := T("gx_options_of", m.title)
	if m.parent != nil {
		title = m.parent.title + " · " + esTr(m.title)
	}
	a.drawTopBar(dst, title)
	a.drawHints(dst, []hint{{"^v", T("h_choose")}, {"<>", T("h_change")}, {"B", T("gx_save_back")}})
}

// optLabel es el texto de una fila (el del emulador, traducido).
func optLabel(r esdata.Option) string {
	if r.Kind == esdata.OptEmulator {
		return T("gx_emulator")
	}
	return esTr(r.Label)
}

// optValue es el valor de una fila tal como se ve: los especiales
// traducidos, y en AUTO lo que hereda.
func (a *App) optValue(r *esdata.Option) string {
	g := &a.gm
	switch r.Kind {
	case esdata.OptFavorite:
		if g.opts != nil && g.opts.scope.Game != nil && g.opts.scope.Game.Favorite {
			return valueName(esdata.ValOn)
		}
		return valueName(esdata.ValOff)
	case esdata.OptSubmenu:
		return ">"
	case esdata.OptText:
		if v := g.conf.Get(r.Key); v != "" {
			return v
		}
		return "-"
	}
	if len(r.Values) == 0 {
		return ""
	}
	i := r.Current(g.conf)
	name := valueName(r.Values[i].Name)
	if i == 0 && r.Inherited != "" && r.Values[0].Name == esdata.ValAuto {
		name += " (" + valueName(r.Inherited) + ")"
	}
	return name
}

// valueName traduce los valores especiales (AUTO, ON, OFF, NONE).
func valueName(s string) string {
	switch s {
	case esdata.ValAuto:
		return T("gx_auto")
	case esdata.ValOn:
		return T("on")
	case esdata.ValOff:
		return T("off")
	case esdata.ValNone:
		return T("gx_none")
	}
	return esTr(s)
}

// esCatalog es el catálogo de traducciones de ES del idioma de Audiofilus
// (esdata.LoadCatalog), para los nombres de las opciones; esCatalogLang, el
// idioma con que se leyó y esCatalogRoot, de dónde. Solo se toca desde el
// hilo de la interfaz.
var (
	esCatalog     esdata.Catalog
	esCatalogLang string
	esCatalogRoot esdata.Root
)

// esTr es s (un texto de es_features.cfg) en el idioma de Audiofilus, con
// las traducciones de ES. Si cambió el idioma, se lee el catálogo nuevo.
func esTr(s string) string {
	if esCatalogLang != lang && esCatalogRoot != "" {
		esCatalog, esCatalogLang = esCatalogRoot.LoadCatalog(lang), lang
	}
	return esCatalog.Tr(s)
}

// Cuánto se gira a mano: a fondo, gmSpinSpeed radianes por segundo; hacia
// arriba o abajo, hasta gmSpinPitchMax. Al soltar, tras gmSpinHold
// segundos vuelve sola a su sitio.
const (
	gmSpinSpeed    = 3.0
	gmSpinPitchMax = 1.2
	gmSpinHold     = 1.5
)

// updateFreeSpin gira la maqueta (o la caja del juego) con el joystick
// derecho, en todas direcciones; sin tocarlo un rato, vuelve despacio a su
// vaivén.
func (a *App) updateFreeSpin() {
	g := &a.gm
	x, y := a.input.rightStick()
	if x != 0 || y != 0 {
		g.spinYaw += x * gmSpinSpeed / 60
		g.spinPitch = max(-gmSpinPitchMax, min(gmSpinPitchMax, g.spinPitch+y*gmSpinSpeed/60))
		g.spinAt = a.t
		return
	}
	if a.t-g.spinAt < gmSpinHold || g.spinYaw == 0 && g.spinPitch == 0 {
		return
	}
	// De vuelta por el camino corto (el giro puede llevar varias vueltas).
	g.spinYaw = math.Remainder(g.spinYaw, 2*math.Pi)
	g.spinYaw *= 0.9
	g.spinPitch *= 0.9
	if math.Abs(g.spinYaw) < 0.002 && math.Abs(g.spinPitch) < 0.002 {
		g.spinYaw, g.spinPitch = 0, 0
	}
}
