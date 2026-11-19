package ui

import (
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/norm"
)

// Las letras con tilde que a Tiny5 y Jersey les faltan se arman con sus
// partes, como en las fuentes pixel art del vietnamita: la letra base que la
// fuente tenga (o, ô, ă...) y las marcas que le falten encima. El vietnamita
// es el que más las usa (ầ, ặ, ộ, ả, ơ, ư...), pero sirve para cualquier
// letra latina con marcas. Las marcas salen de la propia fuente, restando a
// una letra con la marca la letra sin ella (á - a es el acento agudo), así
// que tienen su estilo; las dos que no tiene ninguna (el gancho de ả y el
// cuerno de ơ) se dibujan aquí, a su escala.

// markSource dice de qué par de letras se saca cada marca (con ella y sin
// ella).
var markSource = map[rune][2]rune{
	0x0300: {'à', 'a'}, // grave
	0x0301: {'á', 'a'}, // agudo
	0x0302: {'â', 'a'}, // circunflejo
	0x0303: {'ã', 'a'}, // tilde
	0x0306: {'ă', 'a'}, // breve
	0x0323: {'ạ', 'a'}, // punto debajo
}

// Las marcas dibujadas a mano, a la escala 1 (Tiny5 a 8 px); en los títulos
// van al doble. Cada fila es una línea de píxeles.
var (
	hookMark = []string{ // el gancho de ả, como un "?" pequeño
		"##",
		".#",
		"#.",
	}
	hornMark = []string{ // el cuerno de ơ y ư, arriba a la derecha
		".#",
		"#.",
	}
	dotMark = []string{"#"} // el punto de ạ, si la fuente no lo tiene
)

// composeFace es una fuente que arma las letras que le faltan con sus
// partes. scale es el tamaño de un píxel de su diseño (1 en Tiny5, 2 en
// Jersey y en Tiny5 al doble).
type composeFace struct {
	font.Face
	scale int

	mu    sync.Mutex
	marks map[rune]*image.Alpha // cada marca, recortada a su tamaño
	cache map[rune]*composed
}

// composed es una letra ya armada: su máscara desde el origen, y su avance.
type composed struct {
	rect image.Rectangle // respecto al origen (el punto de la línea base)
	mask *image.Alpha
	adv  fixed.Int26_6
}

func newComposeFace(f font.Face, scale int) *composeFace {
	return &composeFace{Face: f, scale: scale, marks: map[rune]*image.Alpha{}, cache: map[rune]*composed{}}
}

// GlyphAdvance tiene que decir que sí a lo que se puede armar: el MultiFace
// de Ebitengine pregunta por ahí si la fuente tiene la letra.
func (c *composeFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	if adv, ok := c.Face.GlyphAdvance(r); ok {
		return adv, true
	}
	if g := c.compose(r); g != nil {
		return g.adv, true
	}
	return 0, false
}

func (c *composeFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	if b, adv, ok := c.Face.GlyphBounds(r); ok {
		return b, adv, true
	}
	g := c.compose(r)
	if g == nil {
		return fixed.Rectangle26_6{}, 0, false
	}
	return fixed.R(g.rect.Min.X, g.rect.Min.Y, g.rect.Max.X, g.rect.Max.Y), g.adv, true
}

func (c *composeFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	if dr, m, mp, adv, ok := c.Face.Glyph(dot, r); ok {
		return dr, m, mp, adv, true
	}
	g := c.compose(r)
	if g == nil {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	o := image.Pt(dot.X.Round(), dot.Y.Round())
	return g.rect.Add(o), g.mask, image.Point{}, g.adv, true
}

// glyphMask es la letra r de la fuente de base como máscara de 0 y 255,
// con su rectángulo respecto al origen.
func (c *composeFace) glyphMask(r rune) (image.Rectangle, *image.Alpha, fixed.Int26_6, bool) {
	dr, m, mp, adv, ok := c.Face.Glyph(fixed.Point26_6{}, r)
	if !ok {
		return image.Rectangle{}, nil, 0, false
	}
	out := image.NewAlpha(dr)
	for y := dr.Min.Y; y < dr.Max.Y; y++ {
		for x := dr.Min.X; x < dr.Max.X; x++ {
			if _, _, _, a := m.At(mp.X+x-dr.Min.X, mp.Y+y-dr.Min.Y).RGBA(); a >= 0x8000 {
				out.SetAlpha(x, y, opaque)
			}
		}
	}
	return dr, out, adv, true
}

// mark es la marca m recortada a su tamaño, o nil si no hay de dónde sacarla.
func (c *composeFace) mark(m rune) *image.Alpha {
	if a, ok := c.marks[m]; ok {
		return a
	}
	var a *image.Alpha
	switch m {
	case 0x0309:
		a = c.drawn(hookMark)
	case 0x031B:
		a = c.drawn(hornMark)
	default:
		if src, ok := markSource[m]; ok {
			a = c.diff(src[0], src[1])
		}
		if a == nil && m == 0x0323 {
			a = c.drawn(dotMark)
		}
	}
	c.marks[m] = a
	return a
}

// drawn pasa una marca dibujada a mano a la escala de la fuente.
func (c *composeFace) drawn(rows []string) *image.Alpha {
	s := c.scale
	a := image.NewAlpha(image.Rect(0, 0, len(rows[0])*s, len(rows)*s))
	for y, row := range rows {
		for x, p := range row {
			if p == '#' {
				for dy := range s {
					for dx := range s {
						a.SetAlpha(x*s+dx, y*s+dy, opaque)
					}
				}
			}
		}
	}
	return a
}

// diff es lo que tiene with y no tiene without (la marca), recortado.
func (c *composeFace) diff(with, without rune) *image.Alpha {
	wr, wm, _, ok1 := c.glyphMask(with)
	br, bm, _, ok2 := c.glyphMask(without)
	if !ok1 || !ok2 {
		return nil
	}
	var pts []image.Point
	for y := wr.Min.Y; y < wr.Max.Y; y++ {
		for x := wr.Min.X; x < wr.Max.X; x++ {
			if wm.AlphaAt(x, y).A == 0 {
				continue
			}
			if image.Pt(x, y).In(br) && bm.AlphaAt(x, y).A != 0 {
				continue
			}
			pts = append(pts, image.Pt(x, y))
		}
	}
	if len(pts) == 0 {
		return nil
	}
	b := image.Rectangle{Min: pts[0], Max: pts[0].Add(image.Pt(1, 1))}
	for _, p := range pts {
		b = b.Union(image.Rectangle{Min: p, Max: p.Add(image.Pt(1, 1))})
	}
	a := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
	for _, p := range pts {
		a.SetAlpha(p.X-b.Min.X, p.Y-b.Min.Y, opaque)
	}
	return a
}

// compose arma r, o nil si no se puede (no es una letra con marcas, o
// falta su base o alguna marca).
func (c *composeFace) compose(r rune) *composed {
	c.mu.Lock()
	defer c.mu.Unlock()
	if g, ok := c.cache[r]; ok {
		return g
	}
	g := c.build(r)
	c.cache[r] = g
	return g
}

// isBelow dice si la marca va debajo de la letra (las demás van encima, y el
// cuerno al costado).
func isBelow(m rune) bool { return m == 0x0323 }

func (c *composeFace) build(r rune) *composed {
	d := []rune(norm.NFD.String(string(r)))
	if len(d) < 2 {
		return nil
	}
	// La base más completa que tenga la fuente: con todas las marcas que
	// pueda (ô para ố), probando a quitar cada una.
	base, rest := rune(0), []rune(nil)
	for k := len(d) - 1; k >= 1 && base == 0; k-- {
		for _, skip := range subsets(d[1:], k-1) {
			cand := []rune(norm.NFC.String(string(append([]rune{d[0]}, skip.keep...))))
			if len(cand) != 1 {
				continue
			}
			if _, ok := c.Face.GlyphAdvance(cand[0]); ok {
				base, rest = cand[0], skip.drop
				break
			}
		}
	}
	if base == 0 {
		return nil
	}
	// La i sin punto, para ponerle una marca encima (ỉ).
	if (base == 'i' || base == 'j') && hasAbove(rest) {
		if _, ok := c.Face.GlyphAdvance(base - 'i' + 'ı'); ok && base == 'i' {
			base = 'ı'
		}
	}
	rect, mask, adv, ok := c.glyphMask(base)
	if !ok {
		return nil
	}
	// El cuerno se pega al cuerpo de la letra sin marcas (o, u), no a lo
	// que ya tenga encima la base (ò en ờ).
	body := rect
	if _, bm, _, ok := c.glyphMask(d[0]); ok {
		body = inkBounds(bm)
	}
	// El lienzo, con margen para las marcas.
	pad := 8 * c.scale
	canvas := image.NewAlpha(rect.Inset(-pad))
	copyMask(canvas, mask, rect.Min)
	ink := inkBounds(canvas)
	hadAbove := hasAbove(decomposeMarks(base))
	s := c.scale
	for _, m := range rest {
		mk := c.mark(m)
		if mk == nil {
			return nil
		}
		w, h := mk.Bounds().Dx(), mk.Bounds().Dy()
		cx := (ink.Min.X + ink.Max.X - w) / 2
		var at image.Point
		switch {
		case m == 0x031B: // cuerno: arriba a la derecha del cuerpo, pegado
			at = image.Pt(body.Max.X-s, body.Min.Y-h+s)
		case isBelow(m):
			at = image.Pt(cx, ink.Max.Y+s)
		case hadAbove: // segunda marca encima (ố): sobre la primera, sin
			// aire, corrida hacia su lado (el agudo y el gancho a la
			// derecha, el grave a la izquierda), como en las fuentes
			// vietnamitas; así no ensancha la letra.
			at = image.Pt(cx, ink.Min.Y-h)
			switch m {
			case 0x0301, 0x0309:
				at.X = ink.Max.X - w + s
			case 0x0300:
				at.X = ink.Min.X - s
			}
		default:
			at = image.Pt(cx, ink.Min.Y-h-s)
		}
		copyMask(canvas, mk, at)
		if !isBelow(m) && m != 0x031B {
			hadAbove = true
		}
		ink = inkBounds(canvas)
	}
	// Si una marca sobresale del avance (la que va al lado de ố), el avance
	// crece lo justo para dejar un píxel de aire antes de la siguiente.
	if need := ink.Max.X + s; need > adv.Round() {
		adv = fixed.I(need)
	}
	out := image.NewAlpha(image.Rect(0, 0, ink.Dx(), ink.Dy()))
	for y := ink.Min.Y; y < ink.Max.Y; y++ {
		for x := ink.Min.X; x < ink.Max.X; x++ {
			out.SetAlpha(x-ink.Min.X, y-ink.Min.Y, canvas.AlphaAt(x, y))
		}
	}
	return &composed{rect: ink, mask: out, adv: adv}
}

// split es una forma de repartir las marcas: las que trae la base y las que
// se ponen aparte.
type split struct{ keep, drop []rune }

// subsets son las formas de quedarse con n de las marcas ms (en su orden).
func subsets(ms []rune, n int) []split {
	var out []split
	var rec func(i int, keep, drop []rune)
	rec = func(i int, keep, drop []rune) {
		if i == len(ms) {
			if len(keep) == n {
				out = append(out, split{append([]rune(nil), keep...), append([]rune(nil), drop...)})
			}
			return
		}
		rec(i+1, append(keep, ms[i]), drop)
		rec(i+1, keep, append(drop, ms[i]))
	}
	rec(0, nil, nil)
	return out
}

// decomposeMarks son las marcas de r.
func decomposeMarks(r rune) []rune {
	d := []rune(norm.NFD.String(string(r)))
	return d[1:]
}

// hasAbove dice si alguna de las marcas va encima.
func hasAbove(ms []rune) bool {
	for _, m := range ms {
		if !isBelow(m) && m != 0x031B {
			return true
		}
	}
	return false
}

// opaque es un píxel encendido.
var opaque = color.Alpha{A: 0xff}

// copyMask enciende en dst los píxeles de src, con su esquina en at.
func copyMask(dst, src *image.Alpha, at image.Point) {
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if src.AlphaAt(x, y).A != 0 {
				dst.SetAlpha(at.X+x-b.Min.X, at.Y+y-b.Min.Y, opaque)
			}
		}
	}
}

// inkBounds es el rectángulo de los píxeles encendidos de a.
func inkBounds(a *image.Alpha) image.Rectangle {
	var r image.Rectangle
	first := true
	b := a.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if a.AlphaAt(x, y).A == 0 {
				continue
			}
			p := image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+1, y+1)}
			if first {
				r, first = p, false
			} else {
				r = r.Union(p)
			}
		}
	}
	return r
}
