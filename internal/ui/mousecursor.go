package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

// En el escritorio el puntero del ratón lo pinta la interfaz, como en los
// emuladores de antes (ZSNES): una flecha de píxeles del mismo tamaño que los
// de la interfaz, con el cuerpo del color del disco. El del sistema se oculta
// sobre la ventana. Además de verse como el resto, no depende del cursor que
// ponga el escritorio: por XWayland, una ventana sin cursor propio hereda el
// de la raíz, y Omarchy 4.5 lo deja transparente tras su arranque.

// cursorArt es la flecha: X el borde, W el brillo, L entre el brillo y el
// color, A el color del disco. La punta (0, 0) es la que señala.
var cursorArt = [...]string{
	"X.......",
	"XX......",
	"XWX.....",
	"XWLX....",
	"XWLAX...",
	"XWLAAX..",
	"XWLAAAX.",
	"XWAAXXXX",
	"XWXXAX..",
	"XX..XAX.",
	"X...XAX.",
	".....X..",
}

// mouseCursor es el puntero pintado: dónde está, si se ve y su imagen.
type mouseCursor struct {
	hidden   bool    // el del sistema ya está oculto
	on       bool    // se pinta: lo último que se usó fue el ratón
	x, y     float64 // en el lienzo, la última posición
	img      *ebiten.Image
	shadow   *ebiten.Image
	accent   color.RGBA // el color con el que se pintó img
	hasImage bool
}

// update decide si se ve: al mover el ratón o hacer clic, sí; al pulsar una
// tecla o un botón del mando, con el ratón fuera de la ventana o con la
// ventana sin foco, no, como el del sistema en Hyprland. Solo en el escritorio: en las
// portátiles no hay ratón que pintar.
func (c *mouseCursor) update(a *App) {
	if a.status.Platform != "desktop" || handheldUI {
		c.on = false
		return
	}
	if !c.hidden {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
		c.hidden = true
	}
	x, y := ebiten.CursorPositionF()
	moved := x != c.x || y != c.y
	c.x, c.y = x, y
	clicked := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) ||
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight) ||
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle)
	switch {
	case !ebiten.IsFocused() || !ebiten.IsCursorInWindow():
		c.on = false
	case moved || clicked:
		c.on = true
	case len(a.input.keys) > 0 || a.input.any && a.input.usingPad:
		c.on = false
	}
}

// image da la flecha con el color accent, rehecha solo cuando cambia.
func (c *mouseCursor) image(accent color.RGBA) (*ebiten.Image, *ebiten.Image) {
	if c.hasImage && c.accent == accent {
		return c.img, c.shadow
	}
	w, h := len(cursorArt[0]), len(cursorArt)
	if c.img == nil {
		c.img, c.shadow = ebiten.NewImage(w, h), ebiten.NewImage(w, h)
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	colors := map[byte]color.RGBA{
		'X': {0x10, 0x0e, 0x18, 0xff},
		'W': white,
		'L': mix(white, accent, 0.5),
		'A': accent,
	}
	pix := make([]byte, w*h*4)
	shadow := make([]byte, w*h*4)
	for y, row := range cursorArt {
		for x := 0; x < w; x++ {
			col, ok := colors[row[x]]
			if !ok {
				continue
			}
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = col.R, col.G, col.B, col.A
			shadow[i+3] = 0x60 // negro a medias (premultiplicado: el color es 0)
		}
	}
	c.img.WritePixels(pix)
	c.shadow.WritePixels(shadow)
	c.accent, c.hasImage = accent, true
	return c.img, c.shadow
}

// draw pinta la flecha en la pantalla final: la posición del lienzo pasa a la
// ventana con geoM, y cada píxel de la flecha mide k (los de la interfaz). La
// sombra, un píxel abajo a la derecha, la separa de cualquier fondo.
func (c *mouseCursor) draw(screen ebiten.FinalScreen, a *App, geoM ebiten.GeoM, k float64) {
	if !c.on || a.Asleep() {
		return
	}
	img, shadow := c.image(a.theme.Accent)
	px, py := geoM.Apply(c.x, c.y)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(k, k)
	op.GeoM.Translate(float64(int(px)+int(k)), float64(int(py)+int(k)))
	screen.DrawImage(shadow, op)
	op.GeoM.Translate(-k, -k)
	screen.DrawImage(img, op)
}
