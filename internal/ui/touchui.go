package ui

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Piezas de la interfaz para el dedo (ver pointer.go): el botón que abre las
// pestañas en un desplegable cuando no caben al lado (en vertical), y las
// listas que se desplazan con el dedo y se tocan.

// tabDrawer es el desplegable de pestañas abierto: baja desde arriba al
// abrirse (at) y sube al cerrarse (closeAt).
type tabDrawer struct {
	names   []string
	sel     int
	pick    func(i int)
	at      float64
	closeAt float64
}

// Lo que tarda el desplegable en abrirse y en cerrarse.
const (
	drawerIn  = 0.18
	drawerOut = 0.12
)

// closeDrawer cierra el desplegable (con su animación); con pick >= 0,
// elige antes esa pestaña.
func (a *App) closeDrawer(pick int) {
	d := a.drawer
	if d == nil || d.closeAt > 0 {
		return
	}
	d.closeAt = a.t
	if pick >= 0 {
		d.sel = pick
		d.pick(pick)
	}
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
		a.drawer = &tabDrawer{names: names, sel: cur, pick: pick, at: a.t}
	})
	return bh + 2
}

// updateDrawer atiende el desplegable de pestañas con la cruceta (con el
// dedo, sus zonas).
func (a *App) updateDrawer() {
	d, in := a.drawer, &a.input
	if d.closeAt > 0 {
		if a.t-d.closeAt >= drawerOut {
			a.drawer = nil // ya subió del todo
		}
		return
	}
	n := len(d.names)
	switch {
	case in.Repeat(ActDown):
		d.sel = (d.sel + 1) % n
	case in.Repeat(ActUp):
		d.sel = (d.sel - 1 + n) % n
	case in.Pressed(ActConfirm):
		a.closeDrawer(d.sel)
	case in.Pressed(ActBack), in.Pressed(ActMenu):
		a.closeDrawer(-1)
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
	// Cuánto está abierto (0..1): baja con un rebote corto y sube rápido.
	open := easeOutBack(math.Min(1, (a.t-d.at)/drawerIn))
	if d.closeAt > 0 {
		open = 1 - math.Min(1, (a.t-d.closeAt)/drawerOut)
		open *= open
	}
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, uint8(0xa0*math.Max(0, math.Min(1, open)))))
	if d.closeAt == 0 {
		a.tapZone(0, 0, w, h, func() { a.closeDrawer(-1) })
	}
	const rowH = 20
	pw := min(w-2*margin, 160)
	ph := len(d.names)*rowH + 8
	px, py := margin, topH+6
	py -= int(math.Round((1 - open) * float64(ph+topH+10))) // baja desde la barra
	// Recortado bajo la barra de arriba: parece salir de ella.
	b := dst.Bounds()
	dst = dst.SubImage(image.Rect(b.Min.X, topH+1, b.Max.X, b.Max.Y)).(*ebiten.Image)
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
		if d.closeAt == 0 {
			a.tapZone(px, y, pw, rowH, func() { a.closeDrawer(i) })
		}
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
