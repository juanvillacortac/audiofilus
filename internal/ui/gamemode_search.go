package ui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Buscar en el modo juego: como Buscar de la biblioteca (el mismo teclado en
// pantalla, sin distinguir mayúsculas ni tildes, primero lo que empieza por
// lo buscado), pero entre los juegos de todos los sistemas. Elegir uno lleva
// a su sistema con el juego elegido.

// gmHit es un resultado: el sistema y el juego (índices) y cómo casa.
type gmHit struct {
	sys, game int
	rank      int
}

// gmSearchState es la búsqueda del modo juego.
type gmSearchState struct {
	query     []rune
	typing    bool // el teclado está a la vista (si no, se navega por los resultados)
	row, col  int  // la tecla elegida
	hits      []gmHit
	sel       int
	lastQuery string
	from      gmScreen // de dónde se vino (para volver con B)
}

// gmMaxHits es el máximo de resultados (con más, mejor escribir más).
const gmMaxHits = 200

// gmOpenSearch abre Buscar.
func (a *App) gmOpenSearch() {
	g := &a.gm
	g.search.from = g.screen
	g.search.typing = true
	g.search.row, g.search.col = 1, 0
	g.screen = gmSearch
}

// gmRunSearch busca q entre los juegos.
func (g *gameModeState) gmRunSearch(q string) []gmHit {
	q = strings.TrimSpace(fold(q))
	if q == "" {
		return nil
	}
	var hits []gmHit
	for si, s := range g.systems {
		if s.isCollection() {
			continue // Favoritos: ya salen en su sistema
		}
		for gi := range s.games {
			if r := matchRank(s.games[gi].Name, q); r >= 0 {
				hits = append(hits, gmHit{si, gi, r})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > gmMaxHits {
		hits = hits[:gmMaxHits]
	}
	return hits
}

// updateGMSearch atiende Buscar.
func (a *App) updateGMSearch() {
	g := &a.gm
	s, in := &g.search, &a.input
	if s.typing {
		a.gmTyping()
	} else {
		switch {
		case in.Repeat(ActDown) && len(s.hits) > 0:
			s.sel = min(len(s.hits)-1, s.sel+1)
			g.changeAt = a.t
		case in.Repeat(ActUp):
			if s.sel == 0 {
				s.typing = true
			} else {
				s.sel--
				g.changeAt = a.t
			}
		case in.Pressed(ActBack):
			s.typing = true
		case in.Pressed(ActConfirm) && len(s.hits) > 0:
			h := s.hits[s.sel]
			g.sel = h.sys
			g.systems[h.sys].sel = h.game
			g.screen, g.changeAt = gmGames, a.t
		}
	}
	if q := string(s.query); q != s.lastQuery {
		s.hits, s.sel, s.lastQuery = g.gmRunSearch(q), 0, q
		g.changeAt = a.t
	}
}

// gmTyping es la escritura en Buscar: con el teclado de verdad o con el de
// pantalla (A escribe, B borra, X espacio, Y limpia, Start pasa a los
// resultados).
func (a *App) gmTyping() {
	g := &a.gm
	s, in := &g.search, &a.input
	done := func() {
		if len(s.hits) > 0 {
			s.typing = false
		}
	}
	back := func() {
		if len(s.query) == 0 {
			g.screen = s.from // sin letras, B sale
			return
		}
		s.query = s.query[:len(s.query)-1]
	}
	if a.pcTyping() {
		for _, r := range ebiten.AppendInputChars(nil) {
			if unicode.IsPrint(r) && len(s.query) < 40 {
				s.query = append(s.query, r)
			}
		}
		d := inpututil.KeyPressDuration(ebiten.KeyBackspace)
		switch {
		case d == 1 || (d > 24 && d%4 == 0):
			back()
		case inpututil.IsKeyJustPressed(ebiten.KeyEnter), inpututil.IsKeyJustPressed(ebiten.KeyDown):
			done()
		case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
			g.screen = s.from
		}
		return
	}
	rows := len(oskKeys)
	switch {
	case in.Repeat(ActUp):
		s.row = (s.row - 1 + rows) % rows
	case in.Repeat(ActDown):
		s.row = (s.row + 1) % rows
	case in.Repeat(ActLeft):
		s.col = (s.col - 1 + len(oskKeys[s.row])) % len(oskKeys[s.row])
	case in.Repeat(ActRight):
		s.col = (s.col + 1) % len(oskKeys[s.row])
	case in.Pressed(ActConfirm):
		switch k := oskKeys[s.row][s.col]; k {
		case "_":
			s.query = append(s.query, ' ')
		case "<":
			back()
		case "OK":
			done()
		default:
			if len(s.query) < 40 {
				s.query = append(s.query, []rune(k)...)
			}
		}
	case in.Repeat(ActBack):
		back()
	case in.Pressed(ActShuffle):
		s.query = append(s.query, ' ')
	case in.Pressed(ActPause):
		s.query = s.query[:0]
	case in.Pressed(ActMenu):
		done()
	}
}

// drawGMSearch: a la izquierda, la caja del juego elegido (o del primero);
// a la derecha, lo escrito, los resultados con su sistema y, mientras se
// escribe con el mando, el teclado en pantalla.
func (a *App) drawGMSearch(dst *ebiten.Image) {
	g := &a.gm
	s, th := &g.search, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	cx, cy, cw, ch := gmConsoleBox(w, h)
	if len(s.hits) > 0 {
		hit := s.hits[s.sel]
		sys := g.systems[hit.sys]
		gm := &sys.games[hit.game]
		cover, r, alpha := a.gmBoxArt(gm, &sys.sys)
		a.drawGameBox(dst, &g.box, sys.sys.Name, cover, r, alpha, cx, cy, cw, ch, a.gmSpin())
		ty := cy + ch + 4
		drawTextW(dst, gm.Name, SmallFace, margin, ty, cw, th.Text)
		drawTextW(dst, sys.sys.FullName, SmallFace, margin, ty+10, cw, th.Accent)
	}

	x := margin + cw + 10
	rw := w - x - margin - 4
	top := topH + 10
	panel(dst, x-3, top-4, rw+6, 14, mix(th.Bg2, th.Accent, map[bool]float64{true: 0.3, false: 0.15}[s.typing]))
	drawIcon(dst, "search", x, top-1, th.Accent)
	q := string(s.query)
	if q == "" {
		drawText(dst, T("gx_search_hint"), SmallFace, x+11, top, th.Dim)
	}
	maxW := rw - 16
	drawTextW(dst, q, SmallFace, x+11, top, maxW, th.Text)
	if s.typing && int(a.t*2)%2 == 0 {
		rect(dst, x+11+min(textWidth(q, SmallFace), maxW)+1, top-1, 1, 7, th.Text)
	}
	y := top + 14
	bottom := h - bottomH - gmMiniH - 4

	kbTop := bottom
	if s.typing && !a.pcTyping() {
		const keyH = 13
		kbTop = bottom - len(oskKeys)*keyH
		kw := rw / 10
		for r, row := range oskKeys {
			for c, k := range row {
				kx, ky := x+c*kw, kbTop+r*keyH
				bg, fg := withAlpha(th.Bg2, 0xd0), th.Text
				if r == s.row && c == s.col {
					bg, fg = th.Accent, th.Bg
				}
				panel(dst, kx, ky, kw-2, keyH-2, bg)
				drawText(dst, k, SmallFace, kx+(kw-2-textWidth(k, SmallFace))/2, ky+(keyH-2-smallCap)/2, fg)
			}
		}
		kbTop -= 4
	}

	if len(s.hits) == 0 {
		if q != "" {
			drawText(dst, T("search_none"), SmallFace, x, y+2, th.Dim)
		}
	} else {
		rows := make([]gmRow, len(s.hits))
		for i, hit := range s.hits {
			sys := g.systems[hit.sys]
			rows[i] = gmRow{main: sys.games[hit.game].Name, sub: sys.sys.Name}
		}
		sel := s.sel
		if s.typing {
			sel = -1
		}
		a.drawGMList(dst, rows, sel, x, y, rw, kbTop-y)
	}

	a.drawGMNowPlaying(dst)
	a.drawTopBar(dst, T("gx_search"))
	switch {
	case s.typing && a.pcTyping():
		a.drawHints(dst, []hint{{"ENTER", T("h_search")}, {"ESC", T("h_back")}})
	case s.typing:
		a.drawHints(dst, []hint{{"A", T("h_type")}, {"B", T("h_del")}, {"X", T("h_space")}, {"START", T("h_search")}})
	default:
		a.drawHints(dst, []hint{{"A", T("h_open")}, {"B", T("h_keys")}})
	}
}
