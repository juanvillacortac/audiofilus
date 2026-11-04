package ui

import (
	"fmt"
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// La caja del juego en 3D (la pantalla de juegos del modo juego): un prisma
// con la carátula en el frente, con el grosor de la caja de su sistema, que
// se mece y da una vuelta al cambiar de juego como las maquetas.

// boxDepth es el grosor de la caja de un sistema, respecto a su ancho: las
// de cartucho, gruesas; las de disco, finas.
func boxDepth(system string) float64 {
	switch system {
	case "psx", "dreamcast", "saturn", "segacd", "pcenginecd", "3do", "neogeocd":
		return 0.1 // la caja del CD
	case "psp":
		return 0.12
	case "nds", "3ds":
		return 0.11
	case "ports", "pico8", "mame", "fbneo", "arcade":
		return 0.08
	}
	return 0.2 // las de cartucho, de cartón
}

// boxModel es la caja de proporción ar (ancho/alto) y grosor depth (del
// ancho), con la imagen en el frente tomada de la parte uv (0..1) de la
// carátula.
func boxModel(ar, depth float64, uv [4]float64) *consoleModel {
	m := &consoleModel{}
	w, h := ar, 1.0
	if ar > 1 {
		w, h = 1, 1/ar
	}
	d := w * depth
	m.add(box(v3{}, v3{w, h, d}, 0.75)...)
	// El lomo, un poco más claro, y una franja más oscura en el canto de
	// arriba (el cierre de la caja).
	m.detail(box(v3{-w - 0.004, 0, 0}, v3{0.004, h * 0.98, d * 0.98}, 0.95)...)
	q := quadZ(v3{0, 0, d + 0.006}, w*0.985, h*0.985)
	q.uv = [][2]float64{{uv[0], uv[1]}, {uv[2], uv[1]}, {uv[2], uv[3]}, {uv[0], uv[3]}}
	m.tex = append(m.tex, q)
	m.spinFace, m.spinTex = make([]bool, len(m.faces)), make([]bool, len(m.tex))
	m.radius = math.Sqrt(w*w+h*h+d*d) * 0.85
	return m
}

// drawGameBox pinta la caja del juego del sistema system con la carátula
// cover (o lisa si aún no llegó; r es su parte con imagen) en (x, y), de
// w×h.
func (a *App) drawGameBox(dst *ebiten.Image, cv *consoleView, system string, cover *ebiten.Image, r image.Rectangle, coverAlpha float32, x, y, w, h int, spin float64) {
	ar, uv := 0.72, [4]float64{0, 0, 1, 1}
	if cover != nil && !r.Empty() {
		b := cover.Bounds()
		ar = float64(r.Dx()) / float64(r.Dy())
		uv = [4]float64{
			float64(r.Min.X) / float64(b.Dx()), float64(r.Min.Y) / float64(b.Dy()),
			float64(r.Max.X) / float64(b.Dx()), float64(r.Max.Y) / float64(b.Dy()),
		}
	}
	depth := boxDepth(system)
	key := fmt.Sprintf("box:%.3f:%.2f:%.3f:%.3f:%.3f:%.3f", ar, depth, uv[0], uv[1], uv[2], uv[3])
	if cv.models == nil {
		cv.models = map[string]*consoleModel{}
	}
	m, ok := cv.models[key]
	if !ok {
		m = boxModel(ar, depth, uv)
		cv.models[key] = m
	}
	a.drawModel(dst, cv, m, x, y, w, h, spin, cover, coverAlpha, 1, false)
}

// gmBoxCover es la carátula del juego para su caja, con su parte con
// imagen, o nil mientras llega (la caja va lisa hasta tenerla entera: si
// cambiara de forma a mitad del fundido, saltaría).
func (a *App) gmBoxCover(gm *esdata.Game) (*ebiten.Image, image.Rectangle, float32) {
	al := a.gmCover(gm)
	if al == nil {
		return nil, image.Rectangle{}, 0
	}
	img, alpha, r := a.coverContent(al, 96)
	if img == nil || alpha < 1 {
		return nil, image.Rectangle{}, 0
	}
	return img, r, alpha
}

// gmCoverLoading dice si la carátula del juego a size se está preparando
// (tiene, pero aún no llegó).
func (a *App) gmCoverLoading(gm *esdata.Game, size int) bool {
	al := a.gmCover(gm)
	if al == nil || al.missing {
		return false
	}
	img, alpha := a.cover(al, size)
	return img == nil || alpha < 1
}

// drawHourglass pinta un relojito de arena (al doble) centrado en (cx, cy),
// con la arena cayendo: mientras carga una carátula.
func (a *App) drawHourglass(dst *ebiten.Image, cx, cy int) {
	frame := int(a.t*3) % 3
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(2, 2)
	op.GeoM.Translate(float64(cx-7), float64(cy-7))
	op.ColorScale.ScaleWithColor(a.theme.Accent)
	dst.DrawImage(icon(fmt.Sprintf("hourglass%d", frame)), op)
}

// gmFlat dice si los juegos del sistema van con su imagen en plano (y no
// en una caja): las capturas y los ports, que no tienen caja.
func gmFlat(s *gmSystem) bool {
	return gmIsViewer(s) || s.sys.Name == "ports" || s.sys.Group == "ports"
}

// gmPlaceholder es la portada de un juego sin carátula: una caja hecha
// aquí, con los colores del tema, la franja del sistema arriba y el nombre
// del juego en grande. Se guarda por juego y tema.
func (a *App) gmPlaceholder(gm *esdata.Game, sys *esdata.System) *ebiten.Image {
	return a.gmCard(gm.Path, gm.Name, sys.FullName, 96, 132)
}

// gmCard es una portada hecha aquí de w×h para title (con la franja de
// sysName arriba), guardada por id, tamaño y tema: la de las cajas sin
// carátula y, cuadrada, la de las imágenes en plano que no tienen.
func (a *App) gmCard(id, title, sysName string, w, h int) *ebiten.Image {
	g := &a.gm
	th := a.theme
	key := fmt.Sprint(id, "\x00", w, "x", h, th.Accent, th.Bg2)
	if img, ok := g.placeholders[key]; ok {
		return img
	}
	if g.placeholders == nil || len(g.placeholders) > 64 {
		for _, img := range g.placeholders {
			img.Deallocate()
		}
		g.placeholders = map[string]*ebiten.Image{}
	}
	img := ebiten.NewImage(w, h)
	img.Fill(th.Bg2)
	// Rayas en diagonal, tenues.
	for x := -h; x < w; x += 8 {
		for k := 0; k < h; k++ {
			if x+k >= 0 && x+k < w && k%2 == 0 {
				rect(img, x+k, k, 2, 2, withAlpha(th.Accent, 0x22))
			}
		}
	}
	// La franja del sistema.
	rect(img, 0, 0, w, 16, th.Accent)
	drawTextW(img, sysName, SmallFace, 4, 5, w-8, th.Bg)
	// El nombre, en grande, partido en líneas.
	lines := wrapLines(title, BigFace, w-12)
	if len(lines) > 4 {
		lines = append(lines[:3], lines[3]+"…")
	}
	y := 16 + (h-16-len(lines)*15)/2
	for _, l := range lines {
		drawTextW(img, l, BigFace, 6, y, w-12, th.Text)
		y += 15
	}
	rect(img, 6, h-8, w-12, 2, th.Accent)
	g.placeholders[key] = img
	return img
}

// gmBoxArt es la portada para la caja del juego: su carátula, o la hecha
// aquí si no tiene (nil mientras la carátula llega).
func (a *App) gmBoxArt(gm *esdata.Game, sys *esdata.System) (*ebiten.Image, image.Rectangle, float32) {
	if img, r, alpha := a.gmBoxCover(gm); img != nil {
		return img, r, alpha
	}
	if a.gmCoverLoading(gm, 96) {
		return nil, image.Rectangle{}, 0
	}
	ph := a.gmPlaceholder(gm, sys)
	return ph, ph.Bounds(), 1
}

// drawGMFlat pinta en plano, de size×size en (x, y), la imagen al de title
// (del sistema sysName): mientras llega, el relojito; si no tiene, una
// portada hecha aquí con el nombre (no la nota del reproductor).
func (a *App) drawGMFlat(dst *ebiten.Image, al *Album, title, sysName string, x, y, size int) {
	if al != nil && !al.missing {
		img, alpha := a.cover(al, size)
		if img == nil || alpha < 1 {
			panel(dst, x, y, size, size, a.theme.Bg2)
		}
		if img == nil {
			a.drawHourglass(dst, x+size/2, y+size/2)
			return
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(x), float64(y))
		op.ColorScale.ScaleAlpha(alpha)
		dst.DrawImage(img, op)
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	dst.DrawImage(a.gmCard("flat\x00"+title+"\x00"+sysName, title, sysName, size, size), op)
}

// gmLogo es el logo del juego (el <marquee> de su gamelist) como imagen
// para el cargador de portadas, o nil si no tiene.
func (a *App) gmLogo(gm *esdata.Game) *Album {
	g := &a.gm
	if gm == nil || gm.Marquee == "" {
		return nil
	}
	if g.covers == nil {
		g.covers = map[string]*Album{}
	}
	key := "logo\x00" + gm.Marquee
	if al, ok := g.covers[key]; ok {
		return al
	}
	var al *Album
	if local := g.root.Path(gm.Marquee); fileOK(local) {
		al = &Album{Title: gm.Name, coverPath: local, logo: true, clear: true, stray: true, passing: true}
	}
	g.covers[key] = al
	return al
}

// drawGMLogo pinta el logo del juego gm dentro de la caja de maxW×maxH en
// (x, y): centrado en ella si center, si no pegado a la izquierda y arriba;
// con una sombra para que se lea sobre lo que tenga detrás. Dice si lo
// pintó (false si no tiene o aún no llegó).
func (a *App) drawGMLogo(dst *ebiten.Image, gm *esdata.Game, x, y, maxW, maxH int, center bool) bool {
	al := a.gmLogo(gm)
	if al == nil || al.missing {
		return false
	}
	img, alpha, r := a.coverContent(al, maxW)
	if img == nil || r.Empty() {
		return false
	}
	lw, lh := float64(r.Dx()), float64(r.Dy())
	sc := math.Min(1, math.Min(float64(maxW)/lw, float64(maxH)/lh))
	ox, oy := float64(x), float64(y)
	if center {
		ox += math.Floor((float64(maxW) - lw*sc) / 2)
		oy += math.Floor((float64(maxH) - lh*sc) / 2)
	}
	src := img.SubImage(r).(*ebiten.Image)
	for _, pass := range []struct {
		d     float64
		shade bool
	}{{1, true}, {0, false}} {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(sc, sc)
		op.GeoM.Translate(ox+pass.d, oy+pass.d)
		op.Filter = ebiten.FilterLinear
		if pass.shade {
			op.ColorScale.Scale(0, 0, 0, 0.6*alpha) // la sombra, en negro
		} else {
			op.ColorScale.ScaleAlpha(alpha)
		}
		dst.DrawImage(src, op)
	}
	return true
}
