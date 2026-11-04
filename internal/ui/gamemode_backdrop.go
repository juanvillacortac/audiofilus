package ui

import (
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// El fondo del carrusel de sistemas: una imagen de un juego al azar del
// sistema elegido (su captura, o su caja), a toda la pantalla, tenue y
// teñida con el color del tema, que se acerca y se desplaza despacio. Al
// cambiar de sistema, la nueva entra con un fundido sobre la anterior.

// gmBackdrop es el fondo: de qué es (el sistema o el juego), la imagen de
// ahora y la de antes (que se va), y cuándo llegó cada una.
type gmBackdrop struct {
	key           string
	cur, prev     *Album
	curAt, prevAt float64
	// want es el que toca (key cambia a él cuando lleva gmBackdropWait sin
	// cambiar) y wantAt, desde cuándo.
	want   string
	wantAt float64
}

// Cuánto se ve el fondo y cuánto tarda el fundido.
const (
	gmBackdropAlpha = 0.18
	gmBackdropFade  = 0.6
	// gmBackdropWait: el fondo cambia cuando se para en un sistema o un
	// juego. Pasando rápido se pedía (y se preparaba) una imagen por cada
	// uno, y eso daba tirones.
	gmBackdropWait = 0.3
	// gmBackdropSize es el tamaño al que se prepara: tenue y en movimiento,
	// no necesita más que la pantalla lógica.
	gmBackdropSize = 256
)

// gmBackdropPick elige la imagen de un juego al azar del sistema (nil si
// ninguno tiene).
func (a *App) gmBackdropPick(s *gmSystem) *Album {
	g := &a.gm
	if s == nil || len(s.games) == 0 {
		return nil
	}
	for try := 0; try < 24; try++ {
		_, gm := s.at(rand.IntN(len(s.games)))
		if al := a.gmImage(gm); al != nil {
			return al
		}
	}
	return a.gmCover(g.gmGame())
}

// gmImage es la captura del juego como imagen para mostrar (o su caja si
// no tiene captura), o nil.
func (a *App) gmImage(gm *esdata.Game) *Album {
	if gm == nil {
		return nil
	}
	p := gm.Image
	if p == "" {
		return a.gmCover(gm)
	}
	g := &a.gm
	if g.covers == nil {
		g.covers = map[string]*Album{}
	}
	if al, ok := g.covers[p]; ok {
		return al
	}
	var al *Album
	if local := g.root.Path(p); fileOK(local) {
		al = &Album{Title: gm.Name, coverPath: local, logo: true, clear: true, stray: true, passing: true}
	}
	g.covers[p] = al
	return al
}

// drawGMBackdrop pinta el fondo del carrusel (antes que las maquetas): un
// juego al azar del sistema elegido.
func (a *App) drawGMBackdrop(dst *ebiten.Image) {
	s := a.gm.gmSys()
	a.drawBackdropFor(dst, "sys:"+s.sys.Name, func() *Album { return a.gmBackdropPick(s) })
}

// drawGMGameBackdrop pinta el fondo de la lista de juegos: la captura del
// juego elegido (si no tiene, ninguno: su caja ya está delante).
func (a *App) drawGMGameBackdrop(dst *ebiten.Image, gm *esdata.Game) {
	a.drawBackdropFor(dst, "game:"+gm.Path, func() *Album {
		if gm.Image == "" || gm.Image == gm.Thumbnail {
			return nil
		}
		return a.gmImage(gm)
	})
}

// drawBackdropFor pinta el fondo de key (pick elige su imagen cuando
// cambia), con el fundido desde el anterior.
func (a *App) drawBackdropFor(dst *ebiten.Image, key string, pick func() *Album) {
	b := &a.gm.backdrop
	if key != b.want {
		b.want, b.wantAt = key, a.t
	}
	if key != b.key && (a.t-b.wantAt >= gmBackdropWait || b.key == "") {
		if b.cur != nil {
			b.prev, b.prevAt = b.cur, b.curAt
		}
		b.key, b.cur, b.curAt = key, pick(), a.t
	}
	if b.prev != nil {
		if k := (a.t - b.curAt) / gmBackdropFade; k < 1 {
			a.drawBackdropImage(dst, b.prev, b.prevAt, 1-k)
		} else {
			b.prev = nil
		}
	}
	if b.cur != nil {
		a.drawBackdropImage(dst, b.cur, b.curAt, math.Min(1, (a.t-b.curAt)/gmBackdropFade))
	}
}

// drawBackdropImage pinta al llenando la pantalla, con el acercamiento
// lento que empezó en since y la opacidad k (0..1) del fundido.
func (a *App) drawBackdropImage(dst *ebiten.Image, al *Album, since, k float64) {
	img, ready, r := a.coverContent(al, gmBackdropSize)
	if img == nil || r.Empty() {
		return
	}
	w, h := float64(dst.Bounds().Dx()), float64(dst.Bounds().Dy())
	cw, ch := float64(r.Dx()), float64(r.Dy())
	// Llena la pantalla (se recorta lo que sobra), un poco acercada: se
	// acerca y se desplaza despacio, distinto en cada imagen.
	t := a.t - since
	zoom := 1.12 + 0.08*math.Sin(t*0.12)
	sc := math.Max(w/cw, h/ch) * zoom
	panX := 0.5 + 0.35*math.Sin(t*0.07+since)
	panY := 0.5 + 0.35*math.Cos(t*0.05+since*1.3)
	ox := (w - cw*sc) * panX // de 0 (pegada a la izquierda) a w-cw·sc
	oy := (h - ch*sc) * panY
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(sc, sc)
	op.GeoM.Translate(ox, oy)
	op.Filter = ebiten.FilterLinear
	// Teñida hacia el color del tema y tenue.
	th := a.theme
	tint := func(c uint8) float32 { return float32(0.55 + 0.45*float64(c)/255) }
	al2 := float32(gmBackdropAlpha * k * float64(ready))
	op.ColorScale.Scale(tint(th.Accent.R)*al2, tint(th.Accent.G)*al2, tint(th.Accent.B)*al2, al2)
	dst.DrawImage(img.SubImage(r).(*ebiten.Image), op)
}
