package milkdrop

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// El visualizador: cada fotograma,
//
//  1. Update (en la CPU): el sonido (bass/mid/treb de las 16 bandas), las
//     ecuaciones por fotograma, la malla (cada vértice con la fórmula de
//     MilkDrop: zoom, estirado, warp, giro y desplazamiento, o lo que diga
//     el código por vértice) y los triángulos de ondas, formas y bordes.
//  2. Draw (en la GPU): el fotograma anterior pasa por la malla a uno nuevo
//     (shader warpSrc: interpolación bilineal y el apagado, decay), encima se
//     pinta lo de antes, y el resultado pasa por la composición (compSrc:
//     gamma, eco, brillo, oscurecer, solarizar, invertir) a la imagen que se
//     ve. Son dos imágenes que se turnan, más la de salida: no se crea nada
//     por fotograma.
//
// Al cambiar de preset, los dos se evalúan a la vez unos segundos: la malla,
// los valores de la composición y el alfa de lo que pinta cada uno se mezclan.

// Options son el tamaño interno y la malla.
type Options struct {
	// Width y Height son la resolución interna (por defecto 320×240).
	Width, Height int
	// MeshX y MeshY son las celdas de la malla (por defecto 32×24, la de
	// MilkDrop).
	MeshX, MeshY int
	// WavePoints son los puntos de la onda principal (por defecto 256).
	WavePoints int
}

// Input es lo que entra cada fotograma.
type Input struct {
	// Dt son los segundos desde el fotograma anterior.
	Dt float64
	// Bands es el espectro, de graves a agudos, de 0 a 1 (cualquier número
	// de bandas; el de audiofilus tiene 16).
	Bands []float64
	// Samples, si los hay, son la forma de onda de verdad (de -1 a 1,
	// izquierdo y derecho intercalados). Si no, se sintetiza del espectro.
	Samples []float32
	// Progress es cuánto lleva el preset de su tiempo (0..1).
	Progress float64
}

// nBands son las bandas con que se trabaja por dentro.
const nBands = 16

// Renderer es un visualizador de MilkDrop.
type Renderer struct {
	opt  Options
	w, h int
	g    *Globals

	cur, old        *instance
	blend, blendLen float64 // segundos de mezcla que llevan y que duran
	time, fps       float64
	frame           int
	warnings        []string

	// El sonido.
	bands         [nBands]float64
	imm, avg, att [3]float64
	wave          [2][wavePoints]float64
	phase         [2][nBands]uint32
	wavePrev      [2][2]float32 // el punto anterior de cada línea (modo 7)
	waveFirst     [2]float32    // el primero (para cerrar el círculo)
	e             env

	// La malla: por vértice, su posición (0..1) y su rad y ang.
	gx, gy             int
	mx, my, mrad, mang []float64
	uvCur, uvOld       []float64
	warpVerts          []ebiten.Vertex
	warpIdx            []uint16
	motion             [len(motionOuts)]float64

	geo  batch
	comp frameVals // los valores de la composición (mezclados)
	hue  [4][3]float32

	palette    [4][3]float32 // el degradado de SetPalette
	paletteMix float32

	gpu gpuState
}

// NewRenderer crea un visualizador vacío (negro hasta que tenga preset).
func NewRenderer(opt Options) *Renderer {
	if opt.Width <= 0 || opt.Height <= 0 {
		opt.Width, opt.Height = 320, 240
	}
	if opt.MeshX <= 0 || opt.MeshY <= 0 {
		opt.MeshX, opt.MeshY = 32, 24
	}
	if opt.WavePoints <= 0 || opt.WavePoints > wavePoints {
		opt.WavePoints = 256
	}
	r := &Renderer{opt: opt, g: NewGlobals(0x9e3779b97f4a7c15), fps: 30}
	r.geo.whiteX, r.geo.whiteY = 1.5, 1.5 // el píxel del centro de gpuState.white
	for c := range r.phase {
		for b := range r.phase[c] {
			r.phase[c][b] = uint32(b*2654435761 + c*40503)
		}
	}
	for i := range r.avg {
		r.avg[i] = 0.3
	}
	r.Resize(opt.Width, opt.Height)
	return r
}

// Resize cambia la resolución interna (y rehace la malla).
func (r *Renderer) Resize(w, h int) {
	w, h = max(w, 8), max(h, 8)
	if w == r.w && h == r.h {
		return
	}
	r.w, r.h = w, h
	r.gx, r.gy = r.opt.MeshX, r.opt.MeshY
	n := (r.gx + 1) * (r.gy + 1)
	r.mx, r.my, r.mrad, r.mang = make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	r.uvCur, r.uvOld = make([]float64, 2*n), make([]float64, 2*n)
	r.warpVerts = make([]ebiten.Vertex, n)
	r.warpIdx = r.warpIdx[:0]
	corner := math.Hypot(float64(w), float64(h)) / 2
	for j := 0; j <= r.gy; j++ {
		for i := 0; i <= r.gx; i++ {
			k := j*(r.gx+1) + i
			x, y := float64(i)/float64(r.gx), float64(j)/float64(r.gy)
			dx, dy := (x-0.5)*float64(w), (y-0.5)*float64(h)
			r.mx[k], r.my[k] = x, y
			r.mrad[k] = math.Hypot(dx, dy) / corner
			r.mang[k] = math.Atan2(-dy, dx)
			r.warpVerts[k] = ebiten.Vertex{DstX: float32(x * float64(w)), DstY: float32(y * float64(h)), ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1}
			if i < r.gx && j < r.gy {
				a, b, c, d := uint16(k), uint16(k+1), uint16(k+r.gx+1), uint16(k+r.gx+2)
				r.warpIdx = append(r.warpIdx, a, b, c, b, d, c)
			}
		}
	}
	r.gpu.resized = true
}

// Size es la resolución interna.
func (r *Renderer) Size() (int, int) { return r.w, r.h }

// SetPreset cambia al preset p, mezclando durante blend segundos (0: de
// golpe). Devuelve los errores de compilación de sus ecuaciones (las que
// fallan no se evalúan; el resto, sí).
func (r *Renderer) SetPreset(p *Preset, blend float64) []string {
	r.fillEnv(0)
	in := newInstance(p, r.g, &r.e)
	if r.cur != nil && blend > 0 {
		r.old, r.blend, r.blendLen = r.cur, 0, blend
	} else {
		r.old = nil
	}
	r.cur = in
	r.frame = 0
	r.warnings = in.warnings
	return in.warnings
}

// Preset es el preset que se ve (nil si ninguno).
func (r *Renderer) Preset() *Preset {
	if r.cur == nil {
		return nil
	}
	return r.cur.p
}

// Blending dice si se está mezclando con el preset anterior.
func (r *Renderer) Blending() bool { return r.old != nil }

// listen saca bass/mid/treb (relativos a su media: ~1 con música normal) y la
// onda.
func (r *Renderer) listen(in *Input, dt float64) {
	// Las bandas que lleguen, a 16.
	switch {
	case len(in.Bands) == nBands:
		copy(r.bands[:], in.Bands)
	case len(in.Bands) > 1:
		for b := range r.bands {
			pos := float64(b) / (nBands - 1) * float64(len(in.Bands)-1)
			k := int(pos)
			v := in.Bands[k]
			if k+1 < len(in.Bands) {
				v += (in.Bands[k+1] - v) * (pos - float64(k))
			}
			r.bands[b] = v
		}
	default:
		r.bands = [nBands]float64{}
	}
	// Graves (40-180 Hz), medios (hasta 1,7 kHz) y agudos, cada uno respecto
	// a su media de los últimos segundos, como en MilkDrop.
	ranges := [3][2]int{{0, 4}, {4, 10}, {10, 16}}
	slow, fast := 1-math.Exp(-dt/4), 1-math.Exp(-dt/0.35)
	for i, rg := range ranges {
		s := 0.0
		for b := rg[0]; b < rg[1]; b++ {
			s += r.bands[b]
		}
		s /= float64(rg[1] - rg[0])
		r.avg[i] += (s - r.avg[i]) * slow
		ratio := 0.0
		if s > 0.002 {
			ratio = math.Min(4, s/math.Max(r.avg[i], 0.03))
		}
		r.imm[i] = ratio
		r.att[i] += (ratio - r.att[i]) * fast
	}
	r.synthWave(in)
}

// sinTab es un seno en tabla (la onda sintetizada lo usa mucho).
var sinTab = func() (t [4096]float64) {
	for i := range t {
		t[i] = math.Sin(float64(i) / 4096 * 2 * math.Pi)
	}
	return
}()

// synthWave rellena la onda: la de verdad si llega, o una suma de senos, uno
// por banda con su fuerza, que avanzan cada uno a su paso (reacciona a la
// música como un osciloscopio, aunque no sea la onda).
func (r *Renderer) synthWave(in *Input) {
	if n := len(in.Samples) / 2; n > 0 {
		for i := 0; i < wavePoints; i++ {
			k := i * n / wavePoints
			r.wave[0][i] = float64(in.Samples[2*k])
			r.wave[1][i] = float64(in.Samples[2*k+1])
		}
		return
	}
	var amp [nBands]float64
	var step [nBands]uint32
	for b := range amp {
		amp[b] = r.bands[b] / (1 + float64(b)*0.3) * 1.6
		// Las vueltas de cada banda a lo largo de la onda (de 1 a ~9).
		step[b] = uint32((1 + float64(b)*0.55) * float64(1<<32) / wavePoints)
	}
	for c := range r.wave {
		for b := range r.phase[c] {
			r.phase[c][b] += uint32((0.011 + 0.004*float64(b) + 0.003*float64(c)) * float64(1<<32))
		}
		w := &r.wave[c]
		for i := range w {
			s := 0.0
			for b := 0; b < nBands; b++ {
				if amp[b] == 0 {
					continue
				}
				s += amp[b] * sinTab[(r.phase[c][b]+step[b]*uint32(i))>>20]
			}
			w[i] = math.Max(-1, math.Min(1, s))
		}
	}
}

// fillEnv pone las entradas del fotograma.
func (r *Renderer) fillEnv(progress float64) {
	e := &r.e
	e.time, e.fps, e.frame, e.progress = r.time, r.fps, float64(r.frame), progress
	e.bass, e.mid, e.treb = r.imm[0], r.imm[1], r.imm[2]
	e.bassAtt, e.midAtt, e.trebAtt = r.att[0], r.att[1], r.att[2]
	e.meshX, e.meshY = float64(r.gx), float64(r.gy)
	e.aspectX, e.aspectY = 1, 1
	if r.h > r.w {
		e.aspectX = float64(r.w) / float64(r.h)
	}
	if r.w > r.h {
		e.aspectY = float64(r.h) / float64(r.w)
	}
	e.pixelsX, e.pixelsY = float64(r.w), float64(r.h)
}

// Update calcula el fotograma siguiente (en la CPU; no toca la GPU).
func (r *Renderer) Update(in Input) {
	dt := clamp(in.Dt, 1.0/240, 0.25)
	r.time += dt
	r.frame++
	r.fps += (1/dt - r.fps) * 0.1
	r.listen(&in, dt)
	r.fillEnv(in.Progress)
	r.geo.reset()
	if r.cur == nil {
		r.comp = frameVals{}
		return
	}
	mix := 1.0
	if r.old != nil {
		r.blend += dt
		if r.blend >= r.blendLen {
			r.old = nil
		} else {
			mix = smooth(r.blend / r.blendLen)
		}
	}
	r.cur.runFrame(&r.e)
	r.meshFor(r.cur, r.uvCur)
	r.comp = r.cur.vals
	if r.old != nil {
		r.old.runFrame(&r.e)
		r.meshFor(r.old, r.uvOld)
		for k := range r.uvCur {
			r.uvCur[k] = r.uvOld[k] + (r.uvCur[k]-r.uvOld[k])*mix
		}
		for k := range r.comp {
			r.comp[k] = r.old.vals[k] + (r.cur.vals[k]-r.old.vals[k])*mix
		}
	}
	w, h := float32(r.w), float32(r.h)
	for k := range r.warpVerts {
		r.warpVerts[k].SrcX = float32(r.uvCur[2*k]) * w
		r.warpVerts[k].SrcY = float32(r.uvCur[2*k+1]) * h
	}
	r.hueCorners()
	if r.old != nil {
		r.paint(r.old, 1-mix)
	}
	r.paint(r.cur, mix)
}

// smooth es la curva de la mezcla (suave al empezar y al acabar).
func smooth(t float64) float64 {
	t = clamp(t, 0, 1)
	return t * t * (3 - 2*t)
}

// meshFor calcula las coordenadas de textura de cada vértice con in.
func (r *Renderer) meshFor(in *instance, uv []float64) {
	v := &in.vals
	wc := newWarpConst(r.time, v[oWarpSpeed], v[oWarpScale])
	ax, ay := r.e.aspectX, r.e.aspectY
	m := &r.motion
	for i, o := range motionOuts {
		m[i] = v[o]
	}
	pixel := !in.perPixel.Empty()
	for k := range r.mx {
		if pixel {
			in.pixel(r.mx[k], r.my[k], r.mrad[k], r.mang[k], m)
		}
		uv[2*k], uv[2*k+1] = meshUV(m, &wc, r.mx[k], r.my[k], r.mrad[k], ax, ay)
	}
}

// warpConst son las frecuencias del warp de un fotograma.
type warpConst struct {
	t, scaleInv float64
	f           [4]float64
}

// newWarpConst calcula las del instante t.
func newWarpConst(time, speed, scale float64) warpConst {
	t := time * speed
	return warpConst{t: t, scaleInv: 1 / nonZero(scale, 1), f: [4]float64{
		11.68 + 4.0*math.Cos(t*1.413+10),
		8.77 + 3.0*math.Cos(t*1.113+7),
		10.54 + 3.0*math.Cos(t*1.233+3),
		11.49 + 4.0*math.Cos(t*0.933+5),
	}}
}

// meshUV es la fórmula de MilkDrop para un vértice: de dónde (u, v, de 0 a 1
// con v hacia abajo) toma el fotograma anterior el punto (x, y), con el
// movimiento m (en el orden de motionOuts: zoom, zoomexp, rot, warp, cx, cy,
// dx, dy, sx, sy). Se trabaja en coordenadas corregidas por el aspecto
// (ax, ay) para que el giro no deforme.
func meshUV(m *[len(motionOuts)]float64, wc *warpConst, x, y, rad, ax, ay float64) (u, v float64) {
	zoom, zoomExp, rot, warp := m[0], m[1], m[2], m[3]
	cx, cy, dx, dy, sx, sy := m[4], m[5], m[6], m[7], nonZero(m[8], 1), nonZero(m[9], 1)
	z := zoom
	if zoomExp != 1 {
		z = pow(zoom, pow(zoomExp, rad*2-1))
	}
	zi := 1 / nonZero(z, 1)
	X, Y := (x-0.5)*ax*zi, (y-0.5)*ay*zi
	Cx, Cy := (cx-0.5)*ax, (cy-0.5)*ay
	X, Y = (X-Cx)/sx+Cx, (Y-Cy)/sy+Cy
	if warp != 0 {
		fx, fy := 2*x-1, 2*y-1
		k, t, s, f := warp*0.0035, wc.t, wc.scaleInv, &wc.f
		X += k * math.Sin(t*0.333+s*(fx*f[0]-fy*f[3]))
		Y += k * math.Cos(t*0.375-s*(fx*f[2]+fy*f[1]))
		X += k * math.Cos(t*0.753-s*(fx*f[1]-fy*f[2]))
		Y += k * math.Sin(t*0.825+s*(fx*f[0]+fy*f[3]))
	}
	if rot != 0 {
		c, sn := math.Cos(rot), math.Sin(rot)
		X2, Y2 := X-Cx, Y-Cy
		X, Y = X2*c-Y2*sn+Cx, X2*sn+Y2*c+Cy
	}
	// dx positivo lleva la imagen a la derecha y dy positivo, hacia arriba.
	X -= dx
	Y += dy
	return X/ax + 0.5, Y/ay + 0.5
}

// hueCorners calcula los colores de las esquinas del "shader" de MilkDrop 1
// (fShader: un tinte que va cambiando de color por la pantalla).
func (r *Renderer) hueCorners() {
	t := r.time
	for i := range r.hue {
		var c [3]float64
		hi := 0.0
		for k := range c {
			c[k] = 0.6 + 0.3*math.Sin(t*0.43+float64(i)*2.1+float64(k)*1.9+math.Sin(t*0.17+float64(i*k)))
			hi = math.Max(hi, c[k])
		}
		for k := range c {
			r.hue[i][k] = float32(c[k] / hi)
		}
	}
}

// Warnings son los errores del preset que se ve.
func (r *Renderer) Warnings() []string { return r.warnings }
