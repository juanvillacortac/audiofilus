package milkdrop

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Lo que se pinta encima de la realimentación (ondas, formas, bordes) se
// junta cada fotograma en una lista de triángulos: vértices e índices que se
// reusan (no se crea memoria una vez que tienen su tamaño) y unas órdenes de
// dibujo, una por cada cambio de mezcla (normal o aditiva) o de textura (las
// formas con textura toman el fotograma anterior). Así se arma en la CPU, y
// la GPU lo pinta con pocas llamadas.

// rgba es un color con alfa (sin premultiplicar), de 0 a 1.
type rgba struct{ r, g, b, a float32 }

// drawCmd es una orden: los vértices [v0, v1) y los índices [i0, i1), que
// cuentan desde v0.
type drawCmd struct {
	textured, additive bool
	v0, v1, i0, i1     int
}

// batch son los triángulos de un fotograma.
type batch struct {
	verts []ebiten.Vertex
	idx   []uint16
	cmds  []drawCmd
	// whiteX/Y es dónde está el píxel blanco en la imagen de las formas sin
	// textura.
	whiteX, whiteY float32
}

// reset vacía la lista (sin soltar la memoria).
func (b *batch) reset() {
	b.verts, b.idx, b.cmds = b.verts[:0], b.idx[:0], b.cmds[:0]
}

// maxCmdVerts es el tope de vértices de una orden (los índices son de 16 bits).
const maxCmdVerts = 65000

// begin prepara la orden para añadir n vértices con esa mezcla y textura, y
// devuelve el índice del primero (relativo a la orden).
func (b *batch) begin(textured, additive bool, n int) uint16 {
	if k := len(b.cmds); k > 0 {
		c := &b.cmds[k-1]
		if c.textured == textured && c.additive == additive && c.v1-c.v0+n <= maxCmdVerts {
			return uint16(c.v1 - c.v0)
		}
	}
	b.cmds = append(b.cmds, drawCmd{textured: textured, additive: additive, v0: len(b.verts), v1: len(b.verts), i0: len(b.idx), i1: len(b.idx)})
	return 0
}

// vert añade un vértice sin textura.
func (b *batch) vert(x, y float32, c rgba) {
	b.verts = append(b.verts, ebiten.Vertex{DstX: x, DstY: y, SrcX: b.whiteX, SrcY: b.whiteY, ColorR: c.r, ColorG: c.g, ColorB: c.b, ColorA: c.a})
	b.cmds[len(b.cmds)-1].v1++
}

// tvert añade un vértice con textura (sx, sy en píxeles de la imagen).
func (b *batch) tvert(x, y, sx, sy float32, c rgba) {
	b.verts = append(b.verts, ebiten.Vertex{DstX: x, DstY: y, SrcX: sx, SrcY: sy, ColorR: c.r, ColorG: c.g, ColorB: c.b, ColorA: c.a})
	b.cmds[len(b.cmds)-1].v1++
}

// tri añade un triángulo con los vértices base+i, base+j, base+k.
func (b *batch) tri(base, i, j, k uint16) {
	b.idx = append(b.idx, base+i, base+j, base+k)
	b.cmds[len(b.cmds)-1].i1 += 3
}

// line añade un segmento de grosor w, con el color c0 en un extremo y c1 en
// el otro.
func (b *batch) line(x0, y0, x1, y1, w float32, c0, c1 rgba, additive bool) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if l < 1e-4 {
		dx, dy, l = 1, 0, 1
	}
	// La normal, y medio píxel más a lo largo para que los segmentos
	// seguidos no dejen huecos.
	nx, ny := -dy/l*w/2, dx/l*w/2
	ex, ey := dx/l*0.5, dy/l*0.5
	base := b.begin(false, additive, 4)
	b.vert(x0-ex+nx, y0-ey+ny, c0)
	b.vert(x0-ex-nx, y0-ey-ny, c0)
	b.vert(x1+ex+nx, y1+ey+ny, c1)
	b.vert(x1+ex-nx, y1+ey-ny, c1)
	b.tri(base, 0, 1, 2)
	b.tri(base, 1, 3, 2)
}

// rect añade un rectángulo de un color.
func (b *batch) rect(x, y, w, h float32, c rgba, additive bool) {
	if w <= 0 || h <= 0 || c.a <= 0 {
		return
	}
	base := b.begin(false, additive, 4)
	b.vert(x, y, c)
	b.vert(x+w, y, c)
	b.vert(x, y+h, c)
	b.vert(x+w, y+h, c)
	b.tri(base, 0, 1, 2)
	b.tri(base, 1, 3, 2)
}

// dot añade un punto de lado s centrado en (x, y).
func (b *batch) dot(x, y, s float32, c rgba, additive bool) {
	b.rect(x-s/2, y-s/2, s, s, c, additive)
}
