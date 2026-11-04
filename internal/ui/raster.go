package ui

import (
	"image"
	"math"
)

// Un rasterizador por software con búfer de profundidad, para las maquetas
// del modo juego. El motor de los visualizadores (drift.go) pinta los
// triángulos de lejos a cerca, sin profundidad por píxel, y en modelos con
// piezas que se tocan o se cruzan (los gatillos de la PSP, la pantalla de
// la computadora) siempre había algo delante que debía ir detrás. Aquí
// cada píxel se pinta solo si está más cerca que lo que ya hay, y las
// texturas van con corrección de perspectiva (no se doblan al girar). Las
// maquetas son pequeñas (unos 200×140 píxeles de la pantalla lógica), así
// que en la CPU sale barato.

// swRaster es el lienzo: los colores (RGBA, para WritePixels) y la
// profundidad de cada píxel (1/z: más es más cerca; 0, vacío).
type swRaster struct {
	w, h int
	pix  []byte
	iz   []float32
}

// reset deja el lienzo de w×h, vacío y transparente.
func (r *swRaster) reset(w, h int) {
	r.w, r.h = w, h
	n := w * h
	if cap(r.iz) < n {
		r.iz = make([]float32, n)
		r.pix = make([]byte, 4*n)
	}
	r.iz, r.pix = r.iz[:n], r.pix[:4*n]
	clear(r.iz)
	clear(r.pix)
}

// rTri es un triángulo para el lienzo: sus puntos en el espacio de la
// cámara, su color (0..1, ya iluminado) y, si lleva textura, dónde cae
// cada punto en ella (en píxeles).
type rTri struct {
	p   [3]v3
	col [3]float32
	uv  *[3][2]float32
	tex *image.RGBA
	// lift: aclarar lo oscuro (las maquetas a color; en las de intensidad
	// para la paleta, no).
	lift bool
}

// rasterNear es lo más cerca de la cámara que se pinta.
const rasterNear = 0.05

// draw pinta t visto con la cámara cam.
func (r *swRaster) draw(t *rTri, cam *drCam) {
	var sx, sy, iz [3]float32
	for k, p := range t.p {
		if p.z < rasterNear {
			return // las maquetas nunca llegan a la cámara
		}
		inv := 1 / p.z
		sx[k] = float32(cam.w/2 + cam.focal*p.x*inv)
		sy[k] = float32(cam.h/2 - cam.focal*p.y*inv)
		iz[k] = float32(inv)
	}
	area := (sx[1]-sx[0])*(sy[2]-sy[0]) - (sx[2]-sx[0])*(sy[1]-sy[0])
	if area == 0 {
		return
	}
	minX := int(math.Floor(float64(min(sx[0], sx[1], sx[2]))))
	maxX := int(math.Ceil(float64(max(sx[0], sx[1], sx[2]))))
	minY := int(math.Floor(float64(min(sy[0], sy[1], sy[2]))))
	maxY := int(math.Ceil(float64(max(sy[0], sy[1], sy[2]))))
	minX, minY = max(minX, 0), max(minY, 0)
	maxX, maxY = min(maxX, r.w-1), min(maxY, r.h-1)
	if minX > maxX || minY > maxY {
		return
	}
	inv := 1 / area
	// Lo que se interpola en perspectiva: u/z y v/z.
	var uz, vz [3]float32
	var tw, th int
	textured := t.uv != nil && t.tex != nil
	if textured {
		for k := range 3 {
			uz[k], vz[k] = t.uv[k][0]*iz[k], t.uv[k][1]*iz[k]
		}
		tw, th = t.tex.Rect.Dx(), t.tex.Rect.Dy()
	}
	cr, cg, cb := t.col[0], t.col[1], t.col[2]
	if t.lift {
		cr, cg, cb = lift(cr), lift(cg), lift(cb)
	}
	flat := [4]byte{toByte(cr), toByte(cg), toByte(cb), 255}
	for y := minY; y <= maxY; y++ {
		py := float32(y) + 0.5
		row := y * r.w
		for x := minX; x <= maxX; x++ {
			px := float32(x) + 0.5
			// Las coordenadas baricéntricas del centro del píxel.
			b0 := ((sx[1]-px)*(sy[2]-py) - (sx[2]-px)*(sy[1]-py)) * inv
			b1 := ((sx[2]-px)*(sy[0]-py) - (sx[0]-px)*(sy[2]-py)) * inv
			b2 := 1 - b0 - b1
			if b0 < 0 || b1 < 0 || b2 < 0 {
				continue
			}
			z := b0*iz[0] + b1*iz[1] + b2*iz[2]
			i := row + x
			if z <= r.iz[i] {
				continue // algo más cerca ya está pintado aquí
			}
			r.iz[i] = z
			o := 4 * i
			if !textured {
				copy(r.pix[o:o+4], flat[:])
				continue
			}
			u := int((b0*uz[0] + b1*uz[1] + b2*uz[2]) / z)
			v := int((b0*vz[0] + b1*vz[1] + b2*vz[2]) / z)
			u = min(max(u, 0), tw-1)
			v = min(max(v, 0), th-1)
			s := t.tex.PixOffset(t.tex.Rect.Min.X+u, t.tex.Rect.Min.Y+v)
			r.pix[o] = toByte(lift(float32(t.tex.Pix[s]) / 255 * t.col[0]))
			r.pix[o+1] = toByte(lift(float32(t.tex.Pix[s+1]) / 255 * t.col[1]))
			r.pix[o+2] = toByte(lift(float32(t.tex.Pix[s+2]) / 255 * t.col[2]))
			r.pix[o+3] = 255
		}
	}
}

// lift aclara lo oscuro: las consolas negras (la Mega Drive, la N64) se
// perdían en el fondo oscuro de la interfaz.
func lift(c float32) float32 { return 0.09 + 0.91*c }

// toByte pasa 0..1 a un byte.
func toByte(f float32) byte {
	return byte(min(max(f, 0), 1)*255 + 0.5)
}
