package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// El casete del reproductor (Ajustes → Casete): en vez de la portada con el
// vinilo asomando, un casete de neón con la portada de etiqueta, o con la
// canción escrita a mano encima cuando no hay portada. Los carretes giran
// mientras suena y la cinta pasa del carrete izquierdo al derecho según
// avanza la canción, como en una cinta de verdad: la cinta corre a velocidad
// fija, así que el carrete con menos cinta gira más rápido. Debajo, su
// reflejo en un suelo brillante. Los audiolibros van siempre en casete,
// esté o no puesto.
//
// Lo que nunca se mueve se pinta una vez por tamaño y aspecto en la CPU
// (tapeCanvas: formas como distancias con signo, suavizadas, con degradados
// y un brillo difuminado); cada frame solo junta esas capas con la cinta
// (tapeSrc), los carretes, las motas de luz y la etiqueta, y luego le pone
// trama a todo como a las portadas y suma el reflejo (tapeMixSrc).

// Las piezas del casete, en fracciones del ancho del cuerpo (u a lo ancho,
// v hacia abajo), medidas sobre la imagen de referencia; el cuerpo mide
// 1 × tapeH.
const (
	tapeH       = 0.603 // el alto del cuerpo
	tapeCorner  = 0.044 // el radio de sus esquinas
	tapeEar     = 0.016 // la mitad de abajo es esto más ancha por cada lado,
	tapeEarV    = 0.395 // de aquí para abajo, tras un escalón a 45°
	tapeScrewR  = 0.022
	tapeChamfer = 0.026 // las esquinas cortadas de la etiqueta
	tapeReelV   = 0.2937
	tapeRingR   = 0.0754
	tapeHubR    = 0.062
	tapeHoleR   = 0.045
	tapeViewR   = 0.012
	tapeFull    = 0.25 // el radio de un rollo de cinta lleno
	tapeLedge   = 0.452
	tapeFloor   = 0.13 // el alto del reflejo
)

var (
	tapeScrews = [4][2]float32{{0.0437, 0.0397}, {0.952, 0.0397}, {0.0437, 0.563}, {0.952, 0.563}}
	tapeBar    = [4]float32{0.234, 0.028, 0.766, 0.06}
	tapeSlots  = [2][4]float32{{0.103, 0.016, 0.167, 0.04}, {0.829, 0.016, 0.893, 0.04}}
	tapeLabel  = [4]float32{0.0516, 0.0675, 0.944, 0.44}
	tapeLines  = [2]float32{0.128, 0.178}
	tapeLinesU = [2]float32{0.2, 0.795} // centradas en la etiqueta
	tapePill   = [4]float32{0.1825, 0.2024, 0.8056, 0.385}
	tapeReelU  = [2]float32{0.2738, 0.7143}
	tapeView   = [4]float32{0.3532, 0.2222, 0.631, 0.3611}
	// El trapecio de abajo, más estrecho arriba: su borde de arriba y el de
	// abajo, y dónde empieza y acaba cada uno.
	tapeTrap  = struct{ top, bot, tl, tr, bl, br float32 }{0.448, 0.6, 0.1786, 0.8135, 0.1349, 0.8611}
	tapeHoles = [4][3]float32{{0.25, 0.567, 0.022}, {0.3452, 0.5437, 0.019}, {0.6429, 0.5437, 0.019}, {0.742, 0.567, 0.022}}
	tapeBlock = [4]float32{0.4246, 0.5159, 0.5675, 0.603}
	tapeSlot  = [4]float32{0.4706, 0.4722, 0.5214, 0.5016}
	// La luz que se escapa por los bordes (u, v, radio): el resplandor quieto.
	tapeLeaks = [9][3]float32{{0.55, 0, 0.08}, {0.4, 0.005, 0.05}, {0.97, 0.12, 0.05}, {1, 0.33, 0.045}, {0, 0.3, 0.05}, {0.03, 0.5, 0.04}, {0.22, tapeH, 0.06}, {0.62, tapeH, 0.05}, {0.9, tapeH - 0.01, 0.05}}
)

// cassetteSpin es cuántas veces más rápido que el vinilo giran los carretes.
const cassetteSpin = 4

// cassetteRatio es el alto del cuerpo entre su ancho (screens.go lo centra
// en el sitio de la portada).
const cassetteRatio = tapeH

// reelCore es el radio del núcleo de un carrete vacío, como fracción del de
// uno lleno.
const reelCore = 0.3

// cassetteReels guarda el giro de cada carrete: cada uno gira a su propia
// velocidad, según cuánta cinta tenga, así que el ángulo del vinilo no basta.
type cassetteReels struct {
	left, right float64
}

// reelRadii devuelve el radio de la cinta enrollada en cada carrete (1 es
// lleno, reelCore vacío) cuando ha sonado progress (0..1). La cinta es un
// área: lo que pasa de un lado al otro va con el cuadrado del radio.
func reelRadii(progress float64) (l, r float64) {
	p := math.Max(0, math.Min(1, progress))
	c := reelCore * reelCore
	return math.Sqrt(c + (1-c)*(1-p)), math.Sqrt(c + (1-c)*p)
}

// turn gira los carretes lo que giró el vinilo en este frame (d radianes),
// cada uno en proporción inversa a su radio: el que tiene menos cinta gira
// más rápido.
func (c *cassetteReels) turn(d, progress float64) {
	l, r := reelRadii(progress)
	c.left += d * cassetteSpin * reelCore / l
	c.right += d * cassetteSpin * reelCore / r
}

// rgbHex es el color opaco 0xRRGGBB.
func rgbHex(v uint32) color.RGBA {
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

var (
	tapeWhite = color.RGBA{0xff, 0xff, 0xff, 0xff}
	tapeBlack = color.RGBA{0, 0, 0, 0xff}
)

// tapeStop es un color en un punto t (0..1) de un degradado.
type tapeStop struct {
	t float32
	c color.RGBA
}

// gradAt es el color del degradado en t (0..1).
func gradAt(stops []tapeStop, t float32) color.RGBA {
	t = max(0, min(1, t))
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].t {
			a, b := stops[i-1], stops[i]
			return mix(a.c, b.c, float64((t-a.t)/max(b.t-a.t, 1e-6)))
		}
	}
	return stops[len(stops)-1].c
}

// tapePalette son los colores de un aspecto: el degradado a lo ancho de la
// carcasa (sus bordes y su luz), el estampado de la etiqueta, un brillo sobre
// el estampado (ninguno si spot es transparente) y la fuerza del brillo. Un
// aspecto liso tiene el cuerpo opaco, de solid[0] arriba a solid[1] abajo.
type tapePalette struct {
	shell, label []tapeStop
	spot         color.RGBA
	glow         float32
	solid        *[2]color.RGBA
}

// tapeNeon es el neón de la imagen de referencia, con degradados más finos
// de los que dan cuatro colores.
var tapeNeon = tapePalette{
	shell: []tapeStop{{0, rgbHex(0x4f3bff)}, {0.22, rgbHex(0x8f3dff)}, {0.42, rgbHex(0xff3fb0)}, {0.6, rgbHex(0xff7a2a)}, {0.82, rgbHex(0xffb61f)}, {1, rgbHex(0xff8c14)}},
	label: []tapeStop{{0, rgbHex(0x9440ff)}, {0.17, rgbHex(0x6f5bff)}, {0.25, rgbHex(0x63a2ff)}, {0.33, rgbHex(0xa65dff)}, {0.43, rgbHex(0xff7fae)}, {0.52, rgbHex(0xffb347)}, {0.8, rgbHex(0xffc42a)}, {1, rgbHex(0xffa01e)}},
	spot:  rgbHex(0xff3fd0),
	glow:  1,
}

// tapeSkin es el id de un aspecto (config.TapeUI.Skin) y sus cuatro colores
// de izquierda a derecha; los lisos y "cover" no tienen aquí, los calcula
// tapeSkinPalette.
type tapeSkin struct {
	id   string
	cols [4]uint32
}

// tapeChrome son los colores del aspecto cromo, que es también lo que le
// toca a una portada gris (coverColors4).
var tapeChrome = [4]uint32{0x9fb4ff, 0xe8ecff, 0xb9bfd6, 0xffffff}

// tapeSkins son los aspectos en el orden de Ajustes: "screen" liso con el
// acento de Ajustes > Color (tapeLayersFor), "cover" los colores de la propia
// portada, "" el neón (tapeNeon, el de por defecto), "black" plástico
// grafito liso, y tras el cromo uno liso por cada papel de etiqueta
// (tapePapers).
var tapeSkins = func() []tapeSkin {
	s := []tapeSkin{
		{"screen", [4]uint32{}},
		{"cover", [4]uint32{}},
		{"", [4]uint32{0x5b3cff, 0xff3fb0, 0xff7a2a, 0xffb61f}},
		{"black", [4]uint32{}},
		{"ocean", [4]uint32{0x19f5ff, 0x2f7bff, 0x6a4dff, 0xc04dff}},
		{"lava", [4]uint32{0xff1744, 0xff5722, 0xff9100, 0xffd600}},
		{"toxic", [4]uint32{0xc6ff00, 0x39ff14, 0x00e676, 0x00bfa5}},
		{"cyber", [4]uint32{0x00f0ff, 0x7a5cff, 0xff2a6d, 0xfcee0a}},
		{"vapor", [4]uint32{0x01cdfe, 0xb967ff, 0xff71ce, 0xfffb96}},
		{"chrome", tapeChrome},
	}
	for _, p := range tapePapers[1:] {
		s = append(s, tapeSkin{id: p.id})
	}
	return s
}()

// tapePaletteFrom convierte cuatro colores en una paleta: el degradado de la
// carcasa y, más claro, el del estampado.
func tapePaletteFrom(c [4]color.RGBA) tapePalette {
	t := [4]float32{0, 0.35, 0.65, 1}
	p := tapePalette{spot: mix(c[1], tapeWhite, 0.2), glow: 1}
	for i := range c {
		p.shell = append(p.shell, tapeStop{t[i], c[i]})
		p.label = append(p.label, tapeStop{t[i], mix(c[i], tapeWhite, 0.3)})
	}
	return p
}

// tapeSolid es un aspecto liso: edge y mid tiñen los bordes y la luz de la
// carcasa, el cuerpo va de top a bot, con etiqueta de papel y poco brillo.
func tapeSolid(edge, mid, top, bot color.RGBA) tapePalette {
	p := tapePaletteFrom([4]color.RGBA{edge, mid, mid, edge})
	p.solid = &[2]color.RGBA{top, bot}
	p.label = []tapeStop{{0, rgbHex(0xf2f2f5)}, {0.35, rgbHex(0xe7e8ec)}, {0.65, rgbHex(0xdddee3)}, {1, rgbHex(0xd3d5db)}}
	p.spot, p.glow = color.RGBA{}, 0.3
	return p
}

// tapeSolidOf es c como plástico opaco: apenas sombreado, con los bordes un
// poco más oscuros que el cuerpo (más claros parecen cristal iluminado por
// dentro).
func tapeSolidOf(c color.RGBA) tapePalette {
	return tapeSolid(mix(c, tapeBlack, 0.35), mix(c, tapeBlack, 0.25), mix(c, tapeWhite, 0.1), mix(c, tapeBlack, 0.2))
}

// tapeSkinPalette es la paleta de skin; cover son los cuatro colores del
// álbum para el aspecto "cover" (nil mientras no los haya: entonces el neón).
func tapeSkinPalette(skin string, cover *[4]color.RGBA) tapePalette {
	switch skin {
	case "":
		return tapeNeon
	case "cover":
		if cover == nil {
			return tapeNeon
		}
		return tapePaletteFrom(*cover)
	case "black":
		// El grafito que usaba antes la app.
		return tapeSolid(rgbHex(0x4d5058), rgbHex(0x5b5f68), rgbHex(0x3b3e44), rgbHex(0x0e0f11))
	}
	if c := tapePaper(skin); c != nil {
		return tapeSolidOf(*c)
	}
	for _, s := range tapeSkins {
		if s.id == skin {
			var c [4]color.RGBA
			for i, v := range s.cols {
				c[i] = rgbHex(v)
			}
			return tapePaletteFrom(c)
		}
	}
	return tapeNeon
}

// tapePapers son los colores de la etiqueta detrás de la letra, en el orden
// de Ajustes (config.TapeUI.Paper); "" es el estampado del aspecto.
var tapePapers = []struct {
	id string
	c  uint32
}{{"", 0}, {"white", 0xffffff}, {"yellow", 0xffc800}, {"navy", 0x0300a3}, {"pink", 0xd100a4}, {"purple", 0x8d3db9}, {"green", 0x85d228}, {"orange", 0xd28b28}, {"red", 0xdd2222}}

// Los valores por defecto del casete donde la configuración los deja a cero:
// el tamaño de la letra y el de las motas, en porcentaje. (Su resplandor
// respira por defecto: config.TapeUI.GlowAnim "" es "breathe", "none" es
// quieto.)
const (
	tapeHandDefault = 100
	tapeDustDefault = 200
)

// tapeAnim es la animación del resplandor, con "" (la de por defecto) como
// "breathe".
func tapeAnim(id string) string { return orDefault(id, "breathe") }

// tapePaper es el color del papel, o nil para el estampado del aspecto.
func tapePaper(id string) *color.RGBA {
	for _, p := range tapePapers {
		if p.id == id && id != "" {
			c := rgbHex(p.c)
			return &c
		}
	}
	return nil
}

// tapeDarkPaper dice si c es lo bastante oscuro para pedir tinta clara.
func tapeDarkPaper(c *color.RGBA) bool {
	return c != nil && (0.2126*float64(c.R)+0.7152*float64(c.G)+0.0722*float64(c.B))/255 < 0.45
}

// coverColors4 elige los cuatro colores más vivos de una portada para el
// aspecto "cover", como accentColors elige dos: los tonos con más peso (los
// píxeles vivos y claros pesan más) separados al menos 45°, aclarados para el
// neón y ordenados alrededor de la rueda de color, para que el degradado a
// lo ancho del casete fluya. A una portada gris le toca el cromo.
func coverColors4(img image.Image) [4]color.RGBA {
	small := image.NewRGBA(image.Rect(0, 0, 48, 48))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	type bin struct {
		i          int
		w, h, s, v float64
	}
	bins := make([]bin, 24)
	for i := range bins {
		bins[i].i = i
	}
	for i := 0; i < len(small.Pix); i += 4 {
		h, s, v := hsv(color.RGBA{small.Pix[i], small.Pix[i+1], small.Pix[i+2], 0xff})
		if s < 0.3 || v < 0.3 {
			continue
		}
		w, b := s*s*v, &bins[int(h*24)%24]
		b.w += w
		b.h += h * w
		b.s += s * w
		b.v += v * w
	}
	found := slices.DeleteFunc(slices.Clone(bins), func(b bin) bool { return b.w == 0 })
	slices.SortStableFunc(found, func(a, b bin) int {
		if a.w > b.w {
			return -1
		}
		if a.w < b.w {
			return 1
		}
		return 0
	})
	var picked []bin
	for _, gap := range []int{3, 2, 1} {
		for _, b := range found {
			far := len(picked) < 4
			for _, p := range picked {
				if d := abs(p.i - b.i); min(d, 24-d) < gap {
					far = false
				}
			}
			if far {
				picked = append(picked, b)
			}
		}
	}
	if len(picked) == 0 {
		var c [4]color.RGBA
		for i, v := range tapeChrome {
			c[i] = rgbHex(v)
		}
		return c
	}
	type hsvc struct{ h, s, v float64 }
	cols := make([]hsvc, 0, 4)
	for _, b := range picked {
		cols = append(cols, hsvc{b.h / b.w, max(0.55, b.s/b.w), max(0.9, b.v/b.w)})
	}
	for len(cols) < 4 {
		last := cols[len(cols)-1]
		cols = append(cols, hsvc{math.Mod(last.h+28.0/360, 1), last.s, last.v})
	}
	slices.SortFunc(cols, func(a, b hsvc) int {
		if a.h < b.h {
			return -1
		}
		if a.h > b.h {
			return 1
		}
		return 0
	})
	start, widest := 0, -1.0
	for i := range cols {
		if gap := math.Mod(cols[(i+1)%4].h-cols[i].h+1, 1); gap > widest {
			start, widest = (i+1)%4, gap
		}
	}
	var out [4]color.RGBA
	for i := range out {
		c := cols[(start+i)%4]
		out[i] = fromHSV(c.h, c.s, c.v)
	}
	return out
}

// TapeColors son los cuatro colores de la portada para el aspecto "cover"
// del casete; ok es false mientras no se haya leído la portada.
func (a *Album) TapeColors() (c [4]color.RGBA, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tapeCols, a.hasColor
}

// tapeCanvas es una capa pintada en la CPU: RGBA premultiplicado, de 0 a 1.
type tapeCanvas struct {
	w, h int
	px   []float32
}

func newTapeCanvas(w, h int) *tapeCanvas {
	return &tapeCanvas{w, h, make([]float32, w*h*4)}
}

// form es una figura, como su distancia con signo en píxeles de un punto a
// su borde (negativa dentro), y la caja que la contiene (x0, y0, x1, y1).
type form struct {
	sd  func(x, y float32) float32
	box [4]float32
}

// ink es el color sin premultiplicar y el alfa que se ponen en un punto.
type ink func(x, y float32) (color.RGBA, float32)

// flat es un mismo color en todas partes.
func flat(c color.RGBA, a float32) ink {
	return func(float32, float32) (color.RGBA, float32) { return c, a }
}

func hypot32(x, y float32) float32 { return float32(math.Sqrt(float64(x*x + y*y))) }

// circle es un disco.
func circle(cx, cy, r float32) form {
	return form{func(x, y float32) float32 { return hypot32(x-cx, y-cy) - r }, [4]float32{cx - r, cy - r, cx + r, cy + r}}
}

// roundBox es un rectángulo con las esquinas redondeadas a r.
func roundBox(x0, y0, x1, y1, r float32) form { return roundBox4(x0, y0, x1, y1, r, r, r, r) }

// roundBox4 es un rectángulo con cada esquina redondeada a su propio radio:
// arriba a la izquierda, arriba a la derecha, abajo a la derecha, abajo a la
// izquierda.
func roundBox4(x0, y0, x1, y1, tl, tr, br, bl float32) form {
	cx, cy, hw, hh := (x0+x1)/2, (y0+y1)/2, (x1-x0)/2, (y1-y0)/2
	return form{func(x, y float32) float32 {
		r := tl
		switch {
		case x >= cx && y < cy:
			r = tr
		case x >= cx:
			r = br
		case y >= cy:
			r = bl
		}
		r = min(r, hw, hh)
		qx, qy := abs32(x-cx)-hw+r, abs32(y-cy)-hh+r
		return hypot32(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
	}, [4]float32{x0, y0, x1, y1}}
}

// polygon es un polígono convexo, con las esquinas en el sentido de las
// agujas del reloj en pantalla.
func polygon(pts ...[2]float32) form {
	box := [4]float32{pts[0][0], pts[0][1], pts[0][0], pts[0][1]}
	type edge struct{ ax, ay, nx, ny float32 }
	edges := make([]edge, len(pts))
	for i, a := range pts {
		b := pts[(i+1)%len(pts)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := hypot32(dx, dy)
		edges[i] = edge{a[0], a[1], dy / l, -dx / l}
		box = [4]float32{min(box[0], a[0]), min(box[1], a[1]), max(box[2], a[0]), max(box[3], a[1])}
	}
	return form{func(x, y float32) float32 {
		d := float32(math.Inf(-1))
		for _, e := range edges {
			d = max(d, (x-e.ax)*e.nx+(y-e.ay)*e.ny)
		}
		return d
	}, box}
}

// segment es una línea de w píxeles de ancho con los extremos redondos.
func segment(ax, ay, bx, by, w float32) form {
	dx, dy := bx-ax, by-ay
	l2 := max(dx*dx+dy*dy, 1e-6)
	return form{func(x, y float32) float32 {
		h := max(0, min(1, ((x-ax)*dx+(y-ay)*dy)/l2))
		return hypot32(x-ax-dx*h, y-ay-dy*h) - w/2
	}, [4]float32{min(ax, bx) - w, min(ay, by) - w, max(ax, bx) + w, max(ay, by) + w}}
}

// stroke es el contorno de f, de w píxeles de ancho.
func (f form) stroke(w float32) form {
	sd := f.sd
	return form{func(x, y float32) float32 { return abs32(sd(x, y)) - w/2 }, [4]float32{f.box[0] - w, f.box[1] - w, f.box[2] + w, f.box[3] + w}}
}

// and es donde f y g se solapan.
func (f form) and(g form) form {
	a, b := f.sd, g.sd
	return form{func(x, y float32) float32 { return max(a(x, y), b(x, y)) }, f.box}
}

// or es f y g juntas.
func (f form) or(g form) form {
	a, b := f.sd, g.sd
	return form{func(x, y float32) float32 { return min(a(x, y), b(x, y)) },
		[4]float32{min(f.box[0], g.box[0]), min(f.box[1], g.box[1]), max(f.box[2], g.box[2]), max(f.box[3], g.box[3])}}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// paint pone in sobre f, suavizado a lo largo de un píxel.
func (c *tapeCanvas) paint(f form, in ink) {
	for y := max(0, int(f.box[1])-2); y < min(c.h, int(f.box[3])+2); y++ {
		for x := max(0, int(f.box[0])-2); x < min(c.w, int(f.box[2])+2); x++ {
			fx, fy := float32(x)+0.5, float32(y)+0.5
			cov := 0.5 - f.sd(fx, fy)
			if cov <= 0 {
				continue
			}
			k, a := in(fx, fy)
			if a *= min(cov, 1); a <= 0 {
				continue
			}
			p := c.px[(y*c.w+x)*4:][:4]
			p[0] = float32(k.R)/255*a + p[0]*(1-a)
			p[1] = float32(k.G)/255*a + p[1]*(1-a)
			p[2] = float32(k.B)/255*a + p[2]*(1-a)
			p[3] = a + p[3]*(1-a)
		}
	}
}

// bloom difumina c (tres desenfoques de caja, casi un gaussiano, de r
// píxeles cada uno) y lo suma, por k, sobre dst: el brillo del neón.
func (c *tapeCanvas) bloom(dst *tapeCanvas, r int, k float32) {
	a, b := slices.Clone(c.px), make([]float32, len(c.px))
	for range 3 {
		boxBlur(a, b, c.w, c.h, r, true)
		boxBlur(b, a, c.w, c.h, r, false)
	}
	for i, v := range a {
		dst.px[i] = min(1, dst.px[i]+v*k)
	}
}

// boxBlur promedia en dst cada píxel de src con sus r vecinos a cada lado,
// a lo ancho o a lo alto; fuera de la capa cuenta como transparente.
func boxBlur(src, dst []float32, w, h, r int, across bool) {
	lines, length, stride, step := h, w, w*4, 4
	if !across {
		lines, length, stride, step = w, h, 4, w*4
	}
	n := float32(2*r + 1)
	for l := range lines {
		base := l * stride
		for ch := range 4 {
			var s float32
			for i := 0; i <= r && i < length; i++ {
				s += src[base+i*step+ch]
			}
			for i := range length {
				dst[base+i*step+ch] = s / n
				if j := i + r + 1; j < length {
					s += src[base+j*step+ch]
				}
				if j := i - r; j >= 0 {
					s -= src[base+j*step+ch]
				}
			}
		}
	}
}

// image sube la capa a la GPU.
func (c *tapeCanvas) image() *ebiten.Image {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	for i, v := range c.px {
		img.Pix[i] = uint8(max(0, min(1, v))*255 + 0.5)
	}
	return ebiten.NewImageFromImage(img)
}

// tapeMap pasa las unidades del cuerpo a una capa, con p píxeles de margen
// alrededor del cuerpo para el brillo.
type tapeMap struct{ p, w float32 }

func (m tapeMap) x(u float32) float32 { return m.p + u*m.w }
func (m tapeMap) y(v float32) float32 { return m.p + v*m.w }
func (m tapeMap) d(d float32) float32 { return d * m.w }

// across es a qué altura del ancho del cuerpo cae el píxel x (0..1), para
// los degradados.
func (m tapeMap) across(x float32) float32 { return (x - m.p) / m.w }

// tapeBody es la forma del cuerpo: esquinas redondeadas, y la mitad de abajo
// un poco más ancha, con un escalón a 45° por cada lado.
func tapeBody(m tapeMap) form {
	R, e := m.d(tapeCorner), m.d(tapeEar)
	x0, x1, y0, y1, a := m.x(0), m.x(1), m.y(0), m.y(tapeH), m.y(tapeEarV)
	return roundBox(x0, y0, x1, y1, R).
		or(roundBox4(x0-e, a+e, x1+e, y1, 0, 0, R, R)).
		or(polygon([2]float32{x0, a}, [2]float32{x0, a + e}, [2]float32{x0 - e, a + e})).
		or(polygon([2]float32{x1, a}, [2]float32{x1 + e, a + e}, [2]float32{x1, a + e}))
}

// tapeLabelForm es la forma de la etiqueta, con las esquinas cortadas a 45°.
func tapeLabelForm(m tapeMap) form {
	x0, y0, x1, y1, k := m.x(tapeLabel[0]), m.y(tapeLabel[1]), m.x(tapeLabel[2]), m.y(tapeLabel[3]), m.d(tapeChamfer)
	return polygon([2]float32{x0 + k, y0}, [2]float32{x1 - k, y0}, [2]float32{x1, y0 + k}, [2]float32{x1, y1 - k},
		[2]float32{x1 - k, y1}, [2]float32{x0 + k, y1}, [2]float32{x0, y1 - k}, [2]float32{x0, y0 + k})
}

// tapeInks es lo que comparten las funciones que pintan: el mapa, la paleta
// y el degradado de la carcasa mezclado con blanco en k, con alfa a.
type tapeInks struct {
	m   tapeMap
	pal tapePalette
}

func (t tapeInks) shell(k, a float32) ink {
	return func(x, _ float32) (color.RGBA, float32) {
		return mix(gradAt(t.pal.shell, t.m.across(x)), tapeWhite, float64(k)), a
	}
}

// paintShell pinta lo que queda debajo del contenido de la etiqueta: el
// cuerpo, sus bordes, el riel, la repisa, el trapecio, los agujeros y los
// tornillos, y el papel de la etiqueta (el estampado del aspecto o el color
// del papel) con sus dos líneas. En light pinta lo que brilla, y en leaks la
// luz que se escapa por los bordes.
func paintShell(c, light, leaks *tapeCanvas, t tapeInks, paper *color.RGBA) {
	m, pal := t.m, t.pal
	lw := max(1, m.d(0.006))
	body := tapeBody(m)
	y0, y1 := m.y(0), m.y(tapeH)

	// La carcasa: plástico tintado (o grafito liso), más oscuro hacia abajo,
	// con el borde iluminado y un bisel justo por dentro.
	if pal.solid != nil {
		top, bot := pal.solid[0], pal.solid[1]
		c.paint(body, func(_, y float32) (color.RGBA, float32) { return mix(top, bot, float64((y-y0)/(y1-y0))), 1 })
	} else {
		c.paint(body, func(x, _ float32) (color.RGBA, float32) {
			return mix(rgbHex(0x080512), gradAt(pal.shell, m.across(x)), 0.24), 1
		})
	}
	shade := float32(0.3)
	if pal.solid != nil {
		shade = 0.1 // su propio degradado ya lo oscurece
	}
	c.paint(body, func(_, y float32) (color.RGBA, float32) {
		if f := (y - y0) / (y1 - y0); f < 0.55 {
			return tapeWhite, 0.06 * (1 - f/0.55)
		} else {
			return tapeBlack, shade * (f - 0.55) / 0.45
		}
	})
	c.paint(body.stroke(lw), t.shell(0.35, 1))
	light.paint(body.stroke(lw*1.6), t.shell(0.2, 1))
	c.paint(roundBox(m.x(0.02), m.y(0.02), m.x(0.98), m.y(tapeH-0.02), m.d(0.03)).stroke(1), t.shell(0.1, 0.35))

	// El riel de arriba y sus dos ranuras, y la repisa donde empieza la mitad
	// de abajo.
	for i, r := range [][4]float32{tapeBar, tapeSlots[0], tapeSlots[1]} {
		round := m.d(0.008)
		if i == 0 {
			round = m.d(0.01)
		}
		f := roundBox(m.x(r[0]), m.y(r[1]), m.x(r[2]), m.y(r[3]), round)
		c.paint(f, flat(tapeBlack, 0.35))
		c.paint(f.stroke(1), t.shell(0.3, 0.8))
	}
	ly := m.y(tapeLedge) + 0.5
	c.paint(segment(m.x(0.012), ly, m.x(tapeTrap.tl-0.015), ly, 1), t.shell(0.3, 0.7))
	c.paint(segment(m.x(tapeTrap.tr+0.015), ly, m.x(0.988), ly, 1), t.shell(0.3, 0.7))

	// Luz que se escapa por los bordes del plástico, y una raya caliente
	// arriba.
	for _, l := range tapeLeaks {
		x, y, r := m.x(l[0]), m.y(l[1]), m.d(l[2])
		col := mix(gradAt(pal.shell, l[0]), tapeWhite, 0.4)
		leaks.paint(circle(x, y, r), func(px, py float32) (color.RGBA, float32) {
			return col, 0.55 * pal.glow * max(0, 1-hypot32(px-x, py-y)/r)
		})
	}
	light.paint(segment(m.x(0.56), y0+0.5, m.x(0.68), y0+0.5, 1.5), flat(color.RGBA{255, 240, 220, 255}, 0.95*pal.glow))

	// El trapecio de abajo, más estrecho arriba, con su borde doble.
	tr := tapeTrap
	trap := func(k float32) form {
		return polygon([2]float32{m.x(tr.tl) + k, m.y(tr.top) + k}, [2]float32{m.x(tr.tr) - k, m.y(tr.top) + k},
			[2]float32{m.x(tr.br) - k, m.y(tr.bot)}, [2]float32{m.x(tr.bl) + k, m.y(tr.bot)})
	}
	if pal.solid != nil {
		top, bot := mix(pal.solid[0], pal.solid[1], 0.45), mix(pal.solid[0], pal.solid[1], 0.8)
		t0, t1 := m.y(tr.top), m.y(tr.bot)
		c.paint(trap(0), func(_, y float32) (color.RGBA, float32) { return mix(top, bot, float64((y-t0)/(t1-t0))), 1 })
	} else {
		c.paint(trap(0), func(x, _ float32) (color.RGBA, float32) {
			return mix(rgbHex(0x0a0616), gradAt(pal.shell, m.across(x)), 0.32), 1
		})
	}
	c.paint(trap(0).stroke(lw), t.shell(0.35, 1))
	c.paint(trap(m.d(0.012)).stroke(1), t.shell(0.2, 0.55))
	light.paint(trap(0).stroke(lw*1.4), t.shell(0.2, 1))
	block := roundBox(m.x(tapeBlock[0]), m.y(tapeBlock[1]), m.x(tapeBlock[2]), m.y(tapeBlock[3]), 0)
	c.paint(block, flat(tapeBlack, 0.25))
	c.paint(block.stroke(1), t.shell(0.3, 0.85))
	c.paint(roundBox(m.x(tapeSlot[0]), m.y(tapeSlot[1]), m.x(tapeSlot[2]), m.y(tapeSlot[3]), m.d(0.008)).stroke(1), t.shell(0.3, 0.85))
	for _, h := range tapeHoles {
		hole := circle(m.x(h[0]), m.y(h[1]), m.d(h[2]))
		c.paint(hole, flat(rgbHex(0x05020b), 1))
		c.paint(hole.stroke(lw), flat(mix(gradAt(pal.shell, h[0]), tapeWhite, 0.55), 1))
		light.paint(hole.stroke(lw), flat(gradAt(pal.shell, h[0]), 1))
	}
	// Luz que roza sus laderas.
	for _, s := range [][2]float32{{0.79, 0.745}, {0.81, 0.765}, {0.27, 0.23}} {
		c.paint(segment(m.x(s[0]), m.y(0.462), m.x(s[1]), m.y(0.53), max(1, m.d(0.004))), flat(tapeWhite, 0.35))
	}

	// Los tornillos de las esquinas: plateados, con una cruz y un anillo de
	// luz.
	for _, s := range tapeScrews {
		x, y, r := m.x(s[0]), m.y(s[1]), m.d(tapeScrewR)
		c.paint(circle(x, y, r+1), flat(rgbHex(0x120a26), 1))
		hx, hy := x-r*0.35, y-r*0.4
		c.paint(circle(x, y, r), func(px, py float32) (color.RGBA, float32) {
			return mix(tapeWhite, rgbHex(0x8c8cb4), float64(min(1, hypot32(px-hx, py-hy)/(r*1.3)))), 1
		})
		th, l := float32(max(1, int(math.Round(float64(r*0.3))))|1), float32(max(3, int(math.Round(float64(r*1.15))))|1)
		cx, cy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
		c.paint(roundBox(cx-(l-1)/2, cy-(th-1)/2, cx+(l+1)/2, cy+(th+1)/2, 0), flat(rgbHex(0x1d1442), 1))
		c.paint(roundBox(cx-(th-1)/2, cy-(l-1)/2, cx+(th+1)/2, cy+(l+1)/2, 0), flat(rgbHex(0x1d1442), 1))
		light.paint(circle(x, y, r+1.5).stroke(1.2), flat(mix(gradAt(pal.shell, s[0]), tapeWhite, 0.3), 1))
	}

	// El papel de la etiqueta: el estampado del aspecto, con un brillo abajo a
	// la izquierda y luz desde arriba, o un solo color; y sus dos líneas para
	// escribir.
	label := tapeLabelForm(m)
	lx0, lx1, ly0, ly1 := m.x(tapeLabel[0]), m.x(tapeLabel[2]), m.y(tapeLabel[1]), m.y(tapeLabel[3])
	printAt := func(x, _ float32) (color.RGBA, float32) { return gradAt(pal.label, (x-lx0)/(lx1-lx0)), 1 }
	light.paint(label.stroke(lw*2), printAt)
	if paper != nil {
		c.paint(label, flat(*paper, 1))
	} else {
		c.paint(label, printAt)
		if pal.spot.A > 0 {
			sx, sy, sr := m.x(0.13), m.y(0.43), m.d(0.24)
			c.paint(label.and(circle(sx, sy, sr)), func(x, y float32) (color.RGBA, float32) {
				return pal.spot, 0.55 * max(0, 1-hypot32(x-sx, y-sy)/sr)
			})
		}
	}
	c.paint(label, func(_, y float32) (color.RGBA, float32) {
		if f := (y - ly0) / (ly1 - ly0); f < 0.6 {
			return tapeWhite, 0.12 * (1 - f/0.6)
		} else {
			return tapeBlack, 0.12 * (f - 0.6) / 0.4
		}
	})
	rule, ruleA := rgbHex(0x24157a), float32(1)
	if tapeDarkPaper(paper) {
		rule, ruleA = color.RGBA{242, 238, 255, 255}, 0.7
	}
	th := max(1, float32(math.Round(float64(m.d(0.0032)))))
	for _, v := range tapeLines {
		y := float32(math.Round(float64(m.y(v))))
		c.paint(roundBox(float32(math.Round(float64(m.x(tapeLinesU[0])))), y, float32(math.Round(float64(m.x(tapeLinesU[1])))), y+th, 0), flat(rule, ruleA))
	}
}

// paintWindow pinta lo que queda encima del contenido de la etiqueta: un
// brillo suave y el borde oscuro de la etiqueta, la ventana (una píldora con
// el borde iluminado, oscura por dentro), los anillos alrededor de los
// carretes y la ventanilla de la cinta, oscura detrás de los rollos.
func paintWindow(c, light *tapeCanvas, t tapeInks) {
	m, pal := t.m, t.pal
	lw := max(1, m.d(0.006))
	label := tapeLabelForm(m)
	c.paint(segment(m.x(0.68), m.y(tapeLabel[1])-m.d(0.03), m.x(0.6), m.y(tapeLabel[3])+m.d(0.03), m.d(0.06)).and(label), flat(tapeWhite, 0.08))
	edge := rgbHex(0x1c0e52)
	if pal.solid != nil {
		edge = mix(pal.solid[0], tapeBlack, 0.45) // la sombra del propio plástico: gris sobre blanco
	}
	c.paint(label.stroke(max(1, m.d(0.007))), flat(edge, 1))

	p0, q0, p1, q1 := m.x(tapePill[0]), m.y(tapePill[1]), m.x(tapePill[2]), m.y(tapePill[3])
	pr, pw := (q1-q0)/2, max(1.5, m.d(0.008))
	pill := roundBox(p0, q0, p1, q1, pr)
	inside := []tapeStop{{0, rgbHex(0x170a3d)}, {0.5, rgbHex(0x1d0b30)}, {1, rgbHex(0x2c0f24)}}
	if pal.solid != nil {
		inside = []tapeStop{{0, rgbHex(0x121216)}, {0.5, rgbHex(0x18181d)}, {1, rgbHex(0x121216)}}
	}
	c.paint(pill, func(x, _ float32) (color.RGBA, float32) { return gradAt(inside, (x-p0)/(p1-p0)), 1 })
	c.paint(pill.stroke(pw), t.shell(0.45, 1))
	light.paint(pill.stroke(pw*1.5), t.shell(0.3, 1))
	in := pw + 0.5
	c.paint(roundBox(p0+in, q0+in, p1-in, q1-in, pr-in).stroke(1), flat(rgbHex(0x04020c), 0.9))
	for _, u := range tapeReelU {
		ring := circle(m.x(u), m.y(tapeReelV), m.d(tapeRingR)).stroke(lw)
		c.paint(ring, flat(rgbHex(0xcfc9ff), 1))
		light.paint(ring, flat(mix(gradAt(pal.shell, u), tapeWhite, 0.5), 1))
	}
	bg := rgbHex(0x120828)
	if pal.solid != nil {
		bg = rgbHex(0x0f0f12)
	}
	c.paint(roundBox(m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3]), m.d(tapeViewR)), flat(bg, 1))
}

// paintOver pinta lo que queda encima de la cinta: el sombreado y los
// reflejos de la ventanilla, y su marco.
func paintOver(c *tapeCanvas, t tapeInks) {
	m, pal := t.m, t.pal
	a0, b0, a1, b1, r := m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3]), m.d(tapeViewR)
	glass := roundBox(a0, b0, a1, b1, r)
	c.paint(glass, func(_, y float32) (color.RGBA, float32) {
		f := (y - b0) / (b1 - b0)
		return mix(tapeWhite, tapeBlack, float64(f)), 0.10 + 0.15*f
	})
	for _, s := range [][4]float32{{0.47, 0.4, 0.016, 0.2}, {0.505, 0.435, 0.005, 0.45}, {0.6, 0.54, 0.008, 0.18}} {
		c.paint(segment(m.x(s[0]), b0-2, m.x(s[1]), b1+2, max(1, m.d(s[2]))).and(glass), flat(tapeWhite, s[3]))
	}
	c.paint(roundBox(a0-0.5, b0-0.5, a1+0.5, b1+0.5, r+0.5).stroke(1), flat(rgbHex(0x05020f), 1))
	l, rr := mix(gradAt(pal.shell, tapeView[0]), tapeWhite, 0.4), mix(gradAt(pal.shell, tapeView[2]), tapeWhite, 0.4)
	c.paint(roundBox(a0-1.5, b0-1.5, a1+1.5, b1+1.5, r+1.5).stroke(1), func(x, _ float32) (color.RGBA, float32) {
		return mix(l, rr, float64((x-a0)/(a1-a0))), 0.7
	})
}

// paintReel pinta un carrete en el ángulo 0, centrado: un disco blanco con
// un anillo, el centro oscuro y seis dientes.
func paintReel(hub, hole float32) *tapeCanvas {
	s := int(math.Ceil(float64(2*hub+4))) + 2
	c, o := newTapeCanvas(s, s), float32(s)/2
	c.paint(circle(o, o, hub+0.8), flat(rgbHex(0x1b1238), 1))
	hx, hy := o-hub*0.3, o-hub*0.35
	c.paint(circle(o, o, hub), func(x, y float32) (color.RGBA, float32) {
		return mix(tapeWhite, rgbHex(0xa9a6dc), float64(min(1, hypot32(x-hx, y-hy)/(hub*1.25)))), 1
	})
	c.paint(circle(o, o, hub*0.84).stroke(max(1, hub*0.07)), flat(color.RGBA{90, 84, 150, 255}, 0.7))
	c.paint(circle(o, o, hole), flat(rgbHex(0x0b0720), 1))
	for k := range 6 {
		sn, cs := math.Sincos(float64(k) * math.Pi / 3)
		at := func(x, y float32) [2]float32 {
			return [2]float32{o + x*float32(cs) - y*float32(sn), o + x*float32(sn) + y*float32(cs)}
		}
		a, b, w := hole*0.6, hole*1.06, hole*0.17
		c.paint(polygon(at(a, -w), at(b, -w), at(b, w), at(a, w)), flat(rgbHex(0xecebff), 1))
	}
	return c
}

// tapeDotSize es el tamaño del punto de luz suave con el que se dibujan las
// motas y la luz que da vueltas.
const tapeDotSize = 32

// paintDot pinta ese punto: brillante en el centro y apagándose hacia fuera.
func paintDot() *tapeCanvas {
	const h = tapeDotSize / 2
	c := newTapeCanvas(tapeDotSize, tapeDotSize)
	c.paint(circle(h, h, h), func(x, y float32) (color.RGBA, float32) {
		if d := hypot32(x-h, y-h) / h; d < 0.3 {
			return tapeWhite, 1 - 0.55*d/0.3
		} else {
			return tapeWhite, max(0, 0.45*(1-(d-0.3)/0.7))
		}
	})
	return c
}

// tapeLayers son las capas quietas del casete para un tamaño y un aspecto.
type tapeLayers struct {
	key         string
	m           tapeMap
	cw, ch      int // el tamaño de las capas: el cuerpo y margen para el brillo alrededor
	floor, refl int // la línea del suelo (la base del cuerpo) y el alto del reflejo
	pal         tapePalette
	dark        bool // tinta clara, sobre papel oscuro
	shell       *ebiten.Image
	window      *ebiten.Image
	over        *ebiten.Image
	glow        *ebiten.Image // el brillo de los bordes; nil sin resplandor
	spots       *ebiten.Image // las fugas de luz quietas
	mask        *ebiten.Image // la forma de la etiqueta, en blanco
	reel        *ebiten.Image
	dot         *ebiten.Image
}

// newTapeLayers pinta las capas de un casete de w píxeles de ancho. Los
// desenfoques la hacen la parte lenta: solo pasa cuando cambian el tamaño o
// el aspecto.
func newTapeLayers(key string, w int, pal tapePalette, paper *color.RGBA, glow bool) *tapeLayers {
	p := int(math.Ceil(float64(w)*0.08)) + 2
	bodyH := int(math.Round(tapeH * float64(w)))
	L := &tapeLayers{key: key, m: tapeMap{float32(p), float32(w)}, cw: w + 2*p, ch: bodyH + 2*p,
		floor: p + bodyH, refl: int(math.Round(tapeFloor * float64(w))), pal: pal, dark: tapeDarkPaper(paper)}
	t := tapeInks{L.m, pal}
	shell, window, over := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
	light, leaks := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
	paintShell(shell, light, leaks, t, paper)
	paintWindow(window, light, t)
	paintOver(over, t)
	L.shell, L.window, L.over = shell.image(), window.image(), over.image()
	if glow {
		g, s := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
		for _, b := range []struct {
			r int
			k float32
		}{{max(1, int(math.Round(float64(w)*0.008))), 0.9}, {max(2, int(math.Round(float64(w)*0.02))), 0.55}} {
			light.bloom(g, b.r, b.k*pal.glow)
			leaks.bloom(s, b.r, b.k*pal.glow)
		}
		L.glow, L.spots = g.image(), s.image()
	}
	mask := newTapeCanvas(L.cw, L.ch)
	mask.paint(tapeLabelForm(L.m), flat(tapeWhite, 1))
	L.mask = mask.image()
	L.reel = paintReel(L.m.d(tapeHubR), L.m.d(tapeHoleR)).image()
	L.dot = paintDot().image()
	return L
}

// free devuelve la memoria de las capas.
func (L *tapeLayers) free() {
	for _, img := range []*ebiten.Image{L.shell, L.window, L.over, L.glow, L.spots, L.mask, L.reel, L.dot} {
		if img != nil {
			img.Deallocate()
		}
	}
}

// tapeEdgePath es el contorno del cuerpo como una polilínea cerrada en
// unidades del cuerpo, en el sentido de las agujas del reloj desde arriba a
// la izquierda (esquinas redondeadas, la mitad de abajo más ancha), con la
// distancia recorrida en cada punto: por donde viajan el polvo y la luz que
// da vueltas.
type tapeEdgePath struct {
	pts [][2]float64
	cum []float64
}

var tapeEdge = func() tapeEdgePath {
	const R, e, H, a = tapeCorner, tapeEar, tapeH, tapeEarV
	var pts [][2]float64
	line := func(x0, y0, x1, y1 float64) {
		n := max(1, int(math.Ceil(math.Hypot(x1-x0, y1-y0)/0.005)))
		for i := range n {
			f := float64(i) / float64(n)
			pts = append(pts, [2]float64{x0 + (x1-x0)*f, y0 + (y1-y0)*f})
		}
	}
	arc := func(cx, cy, a0, a1 float64) {
		for i := range 12 {
			t := a0 + (a1-a0)*float64(i)/12
			pts = append(pts, [2]float64{cx + R*math.Cos(t), cy + R*math.Sin(t)})
		}
	}
	line(R, 0, 1-R, 0)
	arc(1-R, R, -math.Pi/2, 0)
	line(1, R, 1, a)
	line(1, a, 1+e, a+e)
	line(1+e, a+e, 1+e, H-R)
	arc(1+e-R, H-R, 0, math.Pi/2)
	line(1+e-R, H, -e+R, H)
	arc(-e+R, H-R, math.Pi/2, math.Pi)
	line(-e, H-R, -e, a+e)
	line(-e, a+e, 0, a)
	line(0, a, 0, R)
	arc(R, R, math.Pi, math.Pi*1.5)
	pts = append(pts, pts[0])
	cum := []float64{0}
	for i := 1; i < len(pts); i++ {
		cum = append(cum, cum[i-1]+math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1]))
	}
	return tapeEdgePath{pts, cum}
}()

// at es el punto a una fracción p de la vuelta al borde (en unidades del
// cuerpo) y la normal hacia fuera en ese punto.
func (e tapeEdgePath) at(p float64) (u, v, nx, ny float64) {
	s := (math.Mod(p, 1) + 1)
	s = math.Mod(s, 1) * e.cum[len(e.cum)-1]
	hi := max(1, min(len(e.cum)-1, sortSearch(e.cum, s)))
	lo := hi - 1
	a, b := e.pts[lo], e.pts[hi]
	l := max(e.cum[hi]-e.cum[lo], 1e-9)
	f := (s - e.cum[lo]) / l
	tx, ty := (b[0]-a[0])/l, (b[1]-a[1])/l
	return a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f, ty, -tx
}

// sortSearch es el índice del primer valor de cum mayor que s.
func sortSearch(cum []float64, s float64) int {
	i, _ := slices.BinarySearch(cum, s)
	for i < len(cum) && cum[i] <= s {
		i++
	}
	return i
}

// tapeSpeck es una mota de polvo de luz que flota a lo largo del borde:
// dónde está (p, una fracción de la vuelta, y off, hacia fuera del borde),
// su velocidad, lo grande y lo blanca que es, su parpadeo y cuánto vive.
type tapeSpeck struct {
	big                     bool
	p, v, off, ph, life     float64
	r, vmax, white, tw, end float64
}

// tapeDust son las motas de luz, siempre las mismas (una semilla fija): seis
// grandes (las fugas de luz, que flotan más despacio) y doce pequeñas.
type tapeDust struct {
	specks []tapeSpeck
	rnd    *rand.Rand
	t      float64 // el tiempo de la app hasta el que se movió
}

func newTapeDust() *tapeDust {
	d := &tapeDust{rnd: rand.New(rand.NewPCG(12345, 1))}
	for i := range 18 {
		f := d.rnd.Float64
		s := tapeSpeck{big: i < 6, p: f(), ph: f() * 2 * math.Pi}
		if s.big {
			s.end, s.r, s.vmax, s.white, s.tw = 6+f()*8, 0.035+f()*0.025, 0.006, 0.35, 0.5+f()*0.7
		} else {
			s.end, s.r, s.vmax, s.white, s.tw = 3+f()*5, 0.012+f()*0.01, 0.014, 0.55+f()*0.3, 1.5+f()*2.5
		}
		s.life = f() * s.end
		d.specks = append(d.specks, s)
	}
	return d
}

// step mueve el polvo un frame de la app (1/60 s): la velocidad de cada mota
// recibe un empujón al azar, así que no para de cambiar de dirección; al
// acabar su vida se apaga y vuelve en otro sitio.
func (d *tapeDust) step() {
	const dt = 1.0 / 60
	for i := range d.specks {
		s := &d.specks[i]
		s.v = max(-s.vmax, min(s.vmax, (s.v+(d.rnd.Float64()-0.5)*s.vmax*0.3)*0.99))
		s.p = math.Mod(s.p+s.v*dt+1, 1)
		lim := 0.012
		if s.big {
			lim = 0.006
		}
		s.off = max(-lim, min(lim, (s.off+(d.rnd.Float64()-0.5)*lim*0.2)*0.98))
		s.ph += s.tw * dt
		if s.life += dt; s.life > s.end {
			s.life, s.p, s.v = 0, d.rnd.Float64(), 0
		}
	}
}

// moveTo lleva el polvo hasta el tiempo t de la app, de frame en frame (como
// mucho un segundo, tras una pausa).
func (d *tapeDust) moveTo(t float64) {
	for n := 0; d.t < t && n < 60; n++ {
		d.step()
		d.t += 1.0 / 60
	}
	d.t = max(d.t, t-1.0/60)
}

// fitHand encaja s en maxW como lo haría una mano: más apretado según se
// queda sin sitio, hasta minSq de su ancho, y luego cortado con puntos
// suspensivos. width mide un texto; devuelve qué escribir y cuánto
// apretarlo.
func fitHand(s string, maxW, minSq float64, width func(string) float64) (string, float64) {
	s = strings.TrimSpace(s)
	w := width(s)
	if w <= 0 || w <= maxW {
		return s, 1
	}
	sq := max(minSq, maxW/w)
	if w*sq <= maxW {
		return s, sq
	}
	r := []rune(s)
	for len(r) > 1 && width(string(r)+"…")*sq > maxW {
		r = r[:len(r)-1]
	}
	return strings.TrimRight(string(r), " ") + "…", sq
}

// tapeFonts son las fuentes de letra a mano (Caveat, OFL), que se leen la
// primera vez que se escribe una etiqueta: normal y negrita, y sus caras por
// tamaño.
var tapeFonts struct {
	read  bool
	fonts [2]*opentype.Font
	faces map[[2]int]text.Face
}

// handFace es Caveat a px píxeles, suavizada (al contrario que las fuentes
// de píxeles); nil si no se pudo leer.
func handFace(bold bool, px int) (text.Face, *opentype.Font) {
	tf := &tapeFonts
	if !tf.read {
		tf.read, tf.faces = true, map[[2]int]text.Face{}
		for i, name := range []string{"fonts/Caveat-Regular.ttf.gz", "fonts/Caveat-Bold.ttf.gz"} {
			tf.fonts[i], _ = parseFont(name)
		}
	}
	w := 0
	if bold {
		w = 1
	}
	f := tf.fonts[w]
	if f == nil {
		return nil, nil
	}
	if face, ok := tf.faces[[2]int{w, px}]; ok {
		return face, f
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, nil
	}
	tf.faces[[2]int{w, px}] = text.NewGoXFace(face)
	return tf.faces[[2]int{w, px}], f
}

// handCovers dice si f tiene todas las letras de s: Caveat tiene latino y
// cirílico, no chino, japonés, coreano, árabe, hindi ni tailandés.
func handCovers(f *opentype.Font, s string) bool {
	var buf sfnt.Buffer
	for _, r := range s {
		if r == ' ' {
			continue
		}
		if i, err := f.GlyphIndex(&buf, r); err != nil || i == 0 {
			return false
		}
	}
	return true
}

// writeLabel escribe a mano el título y el artista de la canción en las dos
// líneas de la etiqueta, centrados y un poco inclinados. Lo que Caveat no
// puede escribir va con la fuente de la app, derecho.
func writeLabel(dst *ebiten.Image, L *tapeLayers, title, artist string, bold bool, scale float64) {
	m := L.m
	ink := rgbHex(0x1a1170)
	if L.dark {
		ink = rgbHex(0xf2eeff)
	}
	mid := float64(m.x((tapeLinesU[0] + tapeLinesU[1]) / 2))
	span := float64(m.d(tapeLinesU[1] - tapeLinesU[0]))
	for i, l := range []struct {
		s                 string
		size, tilt, inset float64
		app               text.Face
		cap               int
	}{{title, 0.07, -0.025, 0.02, BigFace, bigCap}, {artist, 0.058, -0.012, 0.04, SmallFace, smallCap}} {
		if strings.TrimSpace(l.s) == "" {
			continue
		}
		base := math.Round(float64(m.y(tapeLines[i]))) - 0.5
		maxW := span - float64(m.d(float32(l.inset)))
		face, f := handFace(bold, max(4, int(math.Round(float64(m.w)*l.size*scale))))
		if face == nil || !handCovers(f, l.s) {
			s, _ := fitHand(l.s, maxW, 1, func(t string) float64 { return float64(textWidth(t, l.app)) })
			drawText(dst, s, l.app, int(mid)-textWidth(s, l.app)/2, int(base)-l.cap, ink)
			continue
		}
		s, sq := fitHand(l.s, maxW, 0.78, func(t string) float64 { return text.Advance(t, face) })
		op := &text.DrawOptions{}
		op.PrimaryAlign = text.AlignCenter
		op.GeoM.Translate(0, -face.Metrics().HAscent)
		op.GeoM.Scale(sq, 1)
		op.GeoM.Rotate(l.tilt)
		op.GeoM.Translate(mid, base)
		op.ColorScale.ScaleWithColor(ink)
		op.Filter = ebiten.FilterLinear
		text.Draw(dst, s, face, op)
	}
}

// tapeState es lo que el casete guarda de un frame a otro. Solo lo toca el
// hilo de la interfaz.
var tapeState struct {
	layers   *tapeLayers
	textKey  string
	text     *ebiten.Image // la etiqueta escrita a mano
	coverKey string
	cover    *ebiten.Image // la portada recortada con la forma de la etiqueta
	frame    *ebiten.Image // el casete montado en este frame
	refl     *ebiten.Image // su parte de abajo boca abajo, para el reflejo
	dust     *tapeDust
}

// tapeImage mantiene img a w×h, y la vuelve a crear si cambió el tamaño.
func tapeImage(img **ebiten.Image, w, h int) *ebiten.Image {
	if *img == nil || (*img).Bounds().Dx() != w || (*img).Bounds().Dy() != h {
		if *img != nil {
			(*img).Deallocate()
		}
		*img = ebiten.NewImage(w, h)
	}
	return *img
}

// tapeLook es la paleta y el papel de la etiqueta (nil para el estampado del
// aspecto) según los ajustes de aspecto y papel; accent es el de Ajustes →
// Color, para el aspecto "screen", y black dice que ese color es Negro, cuyo
// acento es claro (para verse sobre negro): ahí "screen" es el aspecto negro
// con la etiqueta negra. Con "Del aspecto", un aspecto de un solo color le
// da ese color a la etiqueta, como si se hubiera elegido de papel.
func tapeLook(skin, paperID string, cover *[4]color.RGBA, accent color.RGBA, black bool) (tapePalette, *color.RGBA) {
	pal, paper, own := tapeSkinPalette(skin, cover), tapePaper(paperID), tapePaper(skin)
	if skin == "screen" {
		pal, own = tapeSolidOf(accent), &accent
		if black {
			k := rgbHex(0x1a1b1f) // un poco más oscuro que el plástico de alrededor
			pal, own = tapeSkinPalette("black", nil), &k
		}
	}
	if paper == nil {
		paper = own
	}
	return pal, paper
}

// tapeLayersFor devuelve las capas de un casete de w píxeles de ancho con el
// aspecto actual, y las vuelve a pintar si algo cambió.
func (a *App) tapeLayersFor(w int, cover *[4]color.RGBA) *tapeLayers {
	cfg, black := a.cfg.UI.Tape, a.themeColor() == "black"
	key := fmt.Sprint(w, cfg.Skin, cfg.Paper, cfg.Glow != "off")
	if cfg.Skin == "cover" && cover != nil {
		key += fmt.Sprint(*cover)
	}
	if cfg.Skin == "screen" {
		key += fmt.Sprint(a.theme.Accent, black) // sigue al disco cuando Color es "album"
	}
	st := &tapeState
	if st.layers == nil || st.layers.key != key {
		if st.layers != nil {
			st.layers.free()
		}
		pal, paper := tapeLook(cfg.Skin, cfg.Paper, cover, a.theme.Accent, black)
		st.layers = newTapeLayers(key, w, pal, paper, cfg.Glow != "off")
		st.textKey, st.coverKey = "", ""
	}
	return st.layers
}

// toggleCassette cambia el reproductor entre la portada con el vinilo y el
// casete (tocando cualquiera de los dos, o en Ajustes → Casete).
func (a *App) toggleCassette() {
	a.cfg.UI.Cassette = !a.cfg.UI.Cassette
	a.saveConfig()
}

// tapeGlowLevel es con cuánta fuerza se suma ahora el resplandor: fijo,
// respirando cada 4 s, o con los graves (como el fondo).
func (a *App) tapeGlowLevel() float32 {
	switch tapeAnim(a.cfg.UI.Tape.GlowAnim) {
	case "breathe":
		return 0.72 + 0.38*float32(math.Sin(a.t*2*math.Pi/4))
	case "beat":
		return 0.65 + 0.75*bgPump
	}
	return 1
}

// drawTape dibuja el casete con la esquina del cuerpo en (x, y), de w
// píxeles de ancho: la portada de al de etiqueta (o la canción escrita a
// mano), los carretes girados según a.reels y la cinta repartida según
// progress, su resplandor y su reflejo debajo. flash es el destello del
// cambio de canción. Devuelve el rectángulo de la etiqueta, para que los
// iconos (favorito, temporizador...) vayan encima.
func (a *App) drawTape(dst *ebiten.Image, al *Album, x, y, w int, progress float64, flash float32) image.Rectangle {
	if w < 16 {
		return image.Rectangle{} // nada que dibujar (el lienzo diminuto de warmUp)
	}
	cfg, st := a.cfg.UI.Tape, &tapeState
	var cover *[4]color.RGBA
	if al != nil {
		if c, ok := al.TapeColors(); ok {
			cover = &c
		}
	}
	L := a.tapeLayersFor(w, cover)
	m := L.m
	f := tapeImage(&st.frame, L.cw, L.ch)
	f.Clear()
	f.DrawImage(L.shell, nil)

	// La etiqueta: la canción escrita a mano, y encima la portada según va
	// cargando.
	lx0, ly0 := int(math.Round(float64(m.x(tapeLabel[0])))), int(math.Round(float64(m.y(tapeLabel[1]))))
	iw, ih := int(math.Round(float64(m.d(tapeLabel[2]-tapeLabel[0])))), int(math.Round(float64(m.d(tapeLabel[3]-tapeLabel[1]))))
	art := image.Rect(x-int(m.p)+lx0, y-int(m.p)+ly0, x-int(m.p)+lx0+iw, y-int(m.p)+ly0+ih)
	var img *ebiten.Image
	alpha := float32(0)
	if al != nil && cfg.Label != "hand" {
		img, alpha = a.cover(al, iw)
	}
	if alpha < 1 {
		song := a.playingSong
		scale := float64(cmp0(cfg.HandSize, tapeHandDefault)) / 100
		if key := fmt.Sprint(L.key, song.Title, "\n", song.Artist, cfg.Thin, scale); st.textKey != key {
			t := tapeImage(&st.text, L.cw, L.ch)
			t.Clear()
			writeLabel(t, L, song.Title, song.Artist, !cfg.Thin, scale)
			st.textKey = key
		}
		f.DrawImage(st.text, nil)
	}
	if img != nil {
		if key := fmt.Sprintf("%s|%p", L.key, img); st.coverKey != key {
			c := tapeImage(&st.cover, L.cw, L.ch)
			c.Clear()
			b := img.Bounds()
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(lx0), float64(ly0-(b.Dy()-ih)/8)) // la franja de arriba, donde suelen ir los títulos
			c.DrawImage(img, op)
			c.DrawImage(L.mask, &ebiten.DrawImageOptions{Blend: ebiten.BlendDestinationIn})
			st.coverKey = key
		}
		op := &ebiten.DrawImageOptions{}
		op.ColorScale.ScaleAlpha(alpha)
		f.DrawImage(st.cover, op)
	}
	if flash > 0 {
		op := &ebiten.DrawImageOptions{}
		op.ColorScale.ScaleWithColor(withAlpha(a.theme.Text, uint8(150*flash)))
		f.DrawImage(L.mask, op)
	}
	f.DrawImage(L.window, nil)

	// El resplandor, las fugas de luz o el polvo, y la luz que da vueltas,
	// sumados encima como luz.
	if L.glow != nil {
		lv := a.tapeGlowLevel()
		light := &ebiten.DrawImageOptions{Blend: ebiten.BlendLighter}
		light.ColorScale.Scale(lv, lv, lv, lv)
		f.DrawImage(L.glow, light)
		// dot suma un punto de luz suave de radio r (en anchos del cuerpo) en
		// (u, v).
		dot := func(u, v, r float64, c color.RGBA, k float32) {
			op := &ebiten.DrawImageOptions{Blend: ebiten.BlendLighter, Filter: ebiten.FilterLinear}
			s := 2 * r * float64(m.w) / tapeDotSize
			op.GeoM.Translate(-tapeDotSize/2, -tapeDotSize/2)
			op.GeoM.Scale(s, s)
			op.GeoM.Translate(float64(m.x(float32(u))), float64(m.y(float32(v))))
			op.ColorScale.Scale(float32(c.R)/255*k, float32(c.G)/255*k, float32(c.B)/255*k, k)
			f.DrawImage(L.dot, op)
		}
		if cfg.Glow == "static" {
			f.DrawImage(L.spots, light)
		} else {
			if st.dust == nil {
				st.dust = newTapeDust()
			}
			st.dust.moveTo(a.t)
			size := float64(cmp0(cfg.DustSize, tapeDustDefault)) / 100
			for _, s := range st.dust.specks {
				bright := 0.95
				if s.big {
					bright = 0.6
				}
				fade := min(1, s.life/0.8, (s.end-s.life)/0.8)
				k := min(1, bright*fade*(0.6+0.4*math.Sin(s.ph))*float64(L.pal.glow*lv))
				if k < 0.01 {
					continue
				}
				u, v, nx, ny := tapeEdge.at(s.p)
				dot(u+nx*s.off, v+ny*s.off, s.r*size, mix(gradAt(L.pal.shell, float32(u)), tapeWhite, s.white), float32(k))
			}
		}
		if cfg.GlowAnim == "orbit" {
			u, v, _, _ := tapeEdge.at(a.t / 5)
			dot(u, v, 0.09, mix(gradAt(L.pal.shell, float32(u)), tapeWhite, 0.45), 0.85*L.pal.glow)
		}
	}

	// La cinta de cada carrete, vista por la ventanilla.
	full := float64(m.d(tapeFull))
	rl, rr := reelRadii(progress)
	sop := &ebiten.DrawRectShaderOptions{}
	vx0, vy0, vx1, vy1 := m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3])
	sop.GeoM.Translate(math.Floor(float64(vx0)), math.Floor(float64(vy0)))
	sop.Uniforms = map[string]any{
		"Left":  []float32{m.x(tapeReelU[0]), m.y(tapeReelV)},
		"Right": []float32{m.x(tapeReelU[1]), m.y(tapeReelV)},
		"Radii": []float32{float32(full * rl), float32(full * rr)},
		"Core":  float32(full * reelCore),
		"View":  []float32{vx0, vy0, vx1, vy1},
		"ViewR": m.d(tapeViewR),
	}
	f.DrawRectShader(int(math.Ceil(float64(vx1)))-int(vx0)+1, int(math.Ceil(float64(vy1)))-int(vy0)+1, tapeShader, sop)

	// Los carretes, girando (los dos hacia el mismo lado, como sonando la
	// cara A).
	half := float64(L.reel.Bounds().Dx()) / 2
	for i, u := range tapeReelU {
		ang := a.reels.left
		if i == 1 {
			ang = a.reels.right
		}
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Translate(-half, -half)
		op.GeoM.Rotate(-ang)
		op.GeoM.Translate(float64(m.x(u)), float64(m.y(tapeReelV)))
		f.DrawImage(L.reel, op)
	}
	f.DrawImage(L.over, nil)

	// En pantalla, con trama: el casete cortado en el suelo (el suelo tapa su
	// brillo de debajo), y su reflejo sumado debajo.
	ox, oy := float64(x)-float64(m.p), float64(y)-float64(m.p)
	h := L.floor
	if cfg.NoReflection {
		h = L.ch
	}
	body := &ebiten.DrawRectShaderOptions{}
	body.GeoM.Translate(ox, oy)
	body.Images[0] = f.SubImage(image.Rect(0, 0, L.cw, h)).(*ebiten.Image)
	body.Uniforms = map[string]any{"Reflect": float32(0), "Height": float32(1)}
	dst.DrawRectShader(L.cw, h, tapeMixShader, body)
	if !cfg.NoReflection && L.refl > 0 {
		r := tapeImage(&st.refl, L.cw, L.refl)
		r.Clear()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(1, -1)
		op.GeoM.Translate(0, float64(L.refl))
		r.DrawImage(f.SubImage(image.Rect(0, L.floor-L.refl, L.cw, L.floor)).(*ebiten.Image), op)
		refl := &ebiten.DrawRectShaderOptions{Blend: ebiten.BlendLighter}
		refl.GeoM.Translate(ox, oy+float64(L.floor))
		refl.Images[0] = r
		refl.Uniforms = map[string]any{"Reflect": float32(1), "Height": float32(L.refl)}
		dst.DrawRectShader(L.cw, L.refl, tapeMixShader, refl)
	}
	return art
}

// cmp0 es v, o def cuando v es 0 (un ajuste que se dejó por defecto).
func cmp0(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
