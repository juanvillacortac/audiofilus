package ui

import (
	"fmt"
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// Ver una imagen a pantalla completa y acercarla: las capturas (el visor) y
// la caja o la captura de la ficha de un juego. A acerca (×1, ×2, ×4) y,
// acercada, las flechas la mueven.

// gmZoomSize es el tamaño al que se prepara la imagen (el doble del alto de
// la pantalla: acercada al doble aún se ve con detalle).
const gmZoomSize = 480

// gmZoom es cómo se está viendo la imagen: cuánto acercada y qué punto
// (de 0 a 1 en cada eje) queda en el centro.
type gmZoom struct {
	level  int // 0 o 1: entera
	cx, cy float64
}

// gmZoomLevels son los acercamientos, en orden.
var gmZoomLevels = []int{1, 2, 4}

// updateZoom atiende el zoom: A pasa al siguiente acercamiento y, con
// zoom, las flechas mueven la imagen. Dice si se usaron las flechas (con
// zoom, no pasan de imagen).
func (a *App) updateZoom(z *gmZoom) bool {
	in := &a.input
	if z.level == 0 {
		z.level, z.cx, z.cy = 1, 0.5, 0.5
	}
	if in.Pressed(ActConfirm) {
		for i, l := range gmZoomLevels {
			if l == z.level {
				z.level = gmZoomLevels[(i+1)%len(gmZoomLevels)]
				break
			}
		}
		if z.level == 1 {
			z.cx, z.cy = 0.5, 0.5
		}
	}
	if z.level <= 1 {
		return false
	}
	// Mientras se mantiene una flecha, la imagen se mueve (más despacio
	// cuanto más acercada).
	step := 0.012 / float64(z.level)
	if in.duration(ActLeft) > 0 {
		z.cx -= step
	}
	if in.duration(ActRight) > 0 {
		z.cx += step
	}
	if in.duration(ActUp) > 0 {
		z.cy -= step
	}
	if in.duration(ActDown) > 0 {
		z.cy += step
	}
	return true
}

// drawZoomed pinta al a pantalla completa según z, con el título arriba y
// la posición (o el acercamiento) a la derecha.
func (a *App) drawZoomed(dst *ebiten.Image, al *Album, z *gmZoom, title, pos string) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	dst.Fill(th.Bg)
	level := max(1, z.level)
	var img *ebiten.Image
	var r image.Rectangle
	if al != nil {
		img, _, r = a.coverContent(al, gmZoomSize)
	}
	switch {
	case al == nil:
		msg := T("gx_no_preview")
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2, th.Dim)
	case img == nil:
		a.drawHourglass(dst, w/2, h/2)
	default:
		cw, ch := float64(r.Dx()), float64(r.Dy())
		fit := min(float64(w)/cw, float64(h)/ch)
		sc := fit * float64(level)
		// El centro, sin dejar ver fuera de la imagen.
		vw, vh := float64(w)/sc, float64(h)/sc // lo que se ve, en píxeles de la imagen
		cx, cy := z.cx*cw, z.cy*ch
		if vw < cw {
			cx = min(max(cx, vw/2), cw-vw/2)
		} else {
			cx = cw / 2
		}
		if vh < ch {
			cy = min(max(cy, vh/2), ch-vh/2)
		} else {
			cy = ch / 2
		}
		z.cx, z.cy = cx/cw, cy/ch
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(-cx, -cy)
		op.GeoM.Scale(sc, sc)
		op.GeoM.Translate(float64(w)/2, float64(h)/2)
		dst.DrawImage(img.SubImage(r).(*ebiten.Image), op)
	}
	// Arriba, el título y dónde se está, sobre una franja.
	rect(dst, 0, 0, w, topH, withAlpha(th.Shadow, 0xb0))
	if level > 1 {
		pos = fmt.Sprintf("×%d", level)
	}
	pw := textWidth(pos, SmallFace)
	drawTextW(dst, title, SmallFace, margin, 5, w-2*margin-pw-8, th.Text)
	drawText(dst, pos, SmallFace, w-margin-pw, 5, th.Accent)
}

// updateGMZoom atiende la imagen de la ficha a pantalla completa.
func (a *App) updateGMZoom() {
	g := &a.gm
	if a.input.Pressed(ActBack) {
		g.zoom = gmZoom{}
		g.screen = gmInfo
		return
	}
	a.updateZoom(&g.zoom)
}

// drawGMZoom pinta la imagen de la ficha a pantalla completa.
func (a *App) drawGMZoom(dst *ebiten.Image) {
	g := &a.gm
	s := g.gmSys()
	_, gm := s.at(s.sel)
	a.drawZoomed(dst, g.zoomAl, &g.zoom, gm.Name, "")
	if g.zoom.level > 1 {
		a.drawHints(dst, []hint{{"<>^v", T("gx_pan")}, {"A", T("gx_zoom")}, {"B", T("h_back")}})
	} else {
		a.drawHints(dst, []hint{{"A", T("gx_zoom")}, {"B", T("h_back")}})
	}
}
