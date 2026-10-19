package ui

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// Piezas de la interfaz para el dedo (ver pointer.go): el botón que abre las
// pestañas en un desplegable cuando no caben al lado (en vertical), y las
// listas que se desplazan con el dedo y se tocan.

// tabDrawer es el desplegable de pestañas abierto.
type tabDrawer struct {
	names []string
	sel   int
	pick  func(i int)
}

// tabsButton pinta, en (x, y) y con ancho w, el botón con la pestaña de
// ahora ("≡ Populares"): tocarlo abre el desplegable con todas. Devuelve lo
// que ocupa de alto.
func (a *App) tabsButton(dst *ebiten.Image, x, y, w int, names []string, cur int, pick func(i int)) int {
	th := a.theme
	const bh = 15
	panel(dst, x-3, y-4, w+6, bh, mix(th.Bg2, th.Accent, 0.25))
	drawIcon(dst, "list", x, y-1, th.Accent)
	drawTextW(dst, names[cur], SmallFace, x+11, y, w-22, th.Text)
	drawIcon(dst, "arrow_d", x+w-8, y-1, th.Dim)
	a.tapZone(x-3, y-4, w+6, bh, func() {
		a.drawer = &tabDrawer{names: names, sel: cur, pick: pick}
	})
	return bh + 2
}

// updateDrawer atiende el desplegable de pestañas con la cruceta (con el
// dedo, sus zonas).
func (a *App) updateDrawer() {
	d, in := a.drawer, &a.input
	n := len(d.names)
	switch {
	case in.Repeat(ActDown):
		d.sel = (d.sel + 1) % n
	case in.Repeat(ActUp):
		d.sel = (d.sel - 1 + n) % n
	case in.Pressed(ActConfirm):
		a.drawer = nil
		d.pick(d.sel)
	case in.Pressed(ActBack), in.Pressed(ActMenu):
		a.drawer = nil
	}
}

// drawDrawer pinta el desplegable encima de todo: una columna con las
// pestañas; tocar una la elige y tocar fuera lo cierra.
func (a *App) drawDrawer(dst *ebiten.Image) {
	d := a.drawer
	if d == nil {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xa0))
	a.tapZone(0, 0, w, h, func() { a.drawer = nil })
	const rowH = 20
	pw := min(w-2*margin, 160)
	px, py := margin, topH+6
	ph := len(d.names)*rowH + 8
	rect(dst, px+3, py+3, pw, ph, th.Shadow)
	panel(dst, px, py, pw, ph, th.Bg2)
	rect(dst, px+1, py, pw-2, 1, th.Accent)
	for i, name := range d.names {
		y := py + 4 + i*rowH
		c := th.Text
		if i == d.sel {
			panel(dst, px+3, y, pw-6, rowH-2, th.Accent)
			c = th.Bg
		}
		drawTextW(dst, name, SmallFace, px+10, y+6, pw-20, c)
		a.tapZone(px, y, pw, rowH, func() {
			a.drawer = nil
			d.pick(i)
		})
	}
}

// touchList prepara una lista de n filas de rowH en (x, y), w×h, para el
// dedo: devuelve la primera fila visible y cuánto se ve cortada (off), y la
// imagen recortada a la lista donde pintar las filas (en y+i*rowH-off).
// Tocar la fila idx la elige (pick, sin que la vista salte) y hace open.
func (a *App) touchList(dst *ebiten.Image, scr *scroller, sel *int, n, x, y, w, h, rowH int, open func()) (first, off int, clip *ebiten.Image) {
	rows := max(1, h/rowH)
	first, off = scr.place(*sel, rows, n, rowH)
	a.scrollArea(x, y, w, h, scr.by)
	for i := 0; i <= rows && first+i < n; i++ {
		idx := first + i
		ty := y + i*rowH - off
		vy0, vy1 := max(ty, y), min(ty+rowH, y+h)
		if vy1 <= vy0 {
			continue
		}
		a.tapZone(x, vy0, w, vy1-vy0, func() {
			scr.pick(sel, idx)
			open()
		})
	}
	b := dst.Bounds()
	clip = dst.SubImage(image.Rect(b.Min.X, y, b.Max.X, y+h)).(*ebiten.Image)
	return first, off, clip
}
