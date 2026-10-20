package milkdrop

import "fmt"

// Un preset compilado: sus contextos (por fotograma, por vértice, cada onda y
// cada forma), con los punteros a sus variables ya buscados para que cada
// fotograma solo lea y escriba números.

// instance es un preset compilado y su estado (las variables persisten entre
// fotogramas, como en MilkDrop).
type instance struct {
	p *Preset

	frame          *Scope
	init, perFrame *Code
	fEnv           envVars
	fOut           [nOut]*float64
	base           frameVals
	fQ             [nQ]*float64
	qInit          [nQ]float64
	vals           frameVals // las salidas del último fotograma
	q              [nQ]float64

	perPixel *Code
	pix      *Scope
	pEnv     envVars
	pQ       [nQ]*float64
	pMotion  [len(motionOuts)]*float64
	pX, pY   *float64
	pRad     *float64
	pAng     *float64

	waves  [MaxWaves]*waveInst
	shapes [MaxShapes]*shapeInst

	// warnings son los errores de compilación (ese trozo no se evalúa).
	warnings []string
}

// compileOr compila src o, si falla, apunta el error y deja el código vacío
// (MilkDrop hace lo mismo: un preset con un error se ve, a medias).
func (in *instance) compileOr(what, src string, s *Scope) *Code {
	c, err := Compile(src, s)
	if err != nil {
		in.warnings = append(in.warnings, fmt.Sprintf("%s: %v", what, err))
		return &Code{}
	}
	return c
}

// newInstance compila p con las variables globales g. e son las entradas del
// primer fotograma (para el código de inicio).
func newInstance(p *Preset, g *Globals, e *env) *instance {
	in := &instance{p: p, frame: NewScope(g), pix: NewScope(g)}
	s := in.frame
	in.fEnv = bindEnv(s)
	for i, o := range outVars {
		in.fOut[i] = s.Var(o.name)
		in.base[i] = baseOf(p, i)
	}
	in.fQ = bindQ(s)
	in.init = in.compileOr("per_frame_init", p.Init, s)
	in.perFrame = in.compileOr("per_frame", p.PerFrame, s)

	ps := in.pix
	in.pEnv = bindEnv(ps)
	in.pQ = bindQ(ps)
	for i, o := range motionOuts {
		in.pMotion[i] = ps.Var(outVars[o].name)
	}
	in.pX, in.pY, in.pRad, in.pAng = ps.Var("x"), ps.Var("y"), ps.Var("rad"), ps.Var("ang")
	in.perPixel = in.compileOr("per_pixel", p.PerPixel, ps)

	// El inicio: los valores del preset, el código de inicio y lo que deja
	// en las q (que vuelven a ese valor al empezar cada fotograma).
	in.fEnv.set(e)
	in.resetOuts()
	in.init.Run()
	for i, q := range in.fQ {
		in.qInit[i] = *q
	}
	for i := range in.waves {
		in.waves[i] = newWave(in, i, g, e)
	}
	for i := range in.shapes {
		in.shapes[i] = newShape(in, i, g, e)
	}
	return in
}

// resetOuts pone las salidas a los valores del preset.
func (in *instance) resetOuts() {
	for i, p := range in.fOut {
		*p = in.base[i]
	}
}

// runFrame evalúa las ecuaciones por fotograma y deja sus salidas en vals y q.
func (in *instance) runFrame(e *env) {
	in.resetOuts()
	for i, q := range in.fQ {
		*q = in.qInit[i]
	}
	in.fEnv.set(e)
	in.perFrame.Run()
	for i, p := range in.fOut {
		in.vals[i] = clean(*p, in.base[i])
	}
	for i, q := range in.fQ {
		in.q[i] = clean(*q, 0)
	}
	// Lo que no tiene sentido fuera de rango.
	v := &in.vals
	v[oDecay] = clamp(v[oDecay], 0, 1)
	v[oWarpScale] = nonZero(v[oWarpScale], 1)
	v[oSX], v[oSY] = nonZero(v[oSX], 1), nonZero(v[oSY], 1)
	v[oEchoZoom] = nonZero(v[oEchoZoom], 1)
	// El contexto de los vértices recibe las entradas y las q una vez.
	if !in.perPixel.Empty() {
		in.pEnv.set(e)
		for i, q := range in.pQ {
			*q = in.q[i]
		}
	}
}

// pixel evalúa las ecuaciones de un vértice (x, y de 0 a 1 con y hacia abajo,
// rad y ang) y devuelve sus salidas de movimiento en m.
func (in *instance) pixel(x, y, rad, ang float64, m *[len(motionOuts)]float64) {
	for i, o := range motionOuts {
		*in.pMotion[i] = in.vals[o]
	}
	*in.pX, *in.pY, *in.pRad, *in.pAng = x, y, rad, ang
	in.perPixel.Run()
	for i, o := range motionOuts {
		m[i] = clean(*in.pMotion[i], in.vals[o])
	}
}

// waveInst es una onda propia compilada.
type waveInst struct {
	enabled                      bool
	spectrum, dots, thin, add    bool
	scaling, smoothing, sep      float64
	samples                      int
	s                            *Scope
	init, perFrame, perPoint     *Code
	env                          envVars
	q                            [nQ]*float64
	t                            [nT]*float64
	tInit                        [nT]float64
	r, g, b, a, nSamples         *float64
	baseR, baseG, baseB, baseA   float64
	sample, value1, value2, x, y *float64
	fr, fg, fb, fa               float64 // los colores tras el código por fotograma
}

// wavePoints es el tope de puntos de una onda propia (como en MilkDrop).
const wavePoints = 512

// newWave compila la onda i de in.
func newWave(in *instance, i int, g *Globals, e *env) *waveInst {
	d := in.p.Waves[i]
	val := func(k string, def float64) float64 {
		if v, ok := d.Vals[k]; ok {
			return v
		}
		return def
	}
	w := &waveInst{
		enabled: val("enabled", 0) != 0, spectrum: val("bspectrum", 0) != 0, dots: val("busedots", 0) != 0,
		thin: val("bdrawthin", 0) != 0, add: val("badditive", 0) != 0,
		scaling: val("scaling", 1), smoothing: val("smoothing", 0.5), sep: val("sep", 0),
		samples: int(clamp(val("samples", wavePoints), 2, wavePoints)),
		baseR:   val("r", 1), baseG: val("g", 1), baseB: val("b", 1), baseA: val("a", 1),
	}
	if !w.enabled {
		return w
	}
	s := NewScope(g)
	w.s = s
	w.env = bindEnv(s)
	w.q, w.t = bindQ(s), bindT(s)
	w.r, w.g, w.b, w.a, w.nSamples = s.Var("r"), s.Var("g"), s.Var("b"), s.Var("a"), s.Var("samples")
	w.sample, w.value1, w.value2, w.x, w.y = s.Var("sample"), s.Var("value1"), s.Var("value2"), s.Var("x"), s.Var("y")
	name := fmt.Sprintf("wave %d ", i)
	w.init = in.compileOr(name+"init", d.Init, s)
	w.perFrame = in.compileOr(name+"per_frame", d.PerFrame, s)
	w.perPoint = in.compileOr(name+"per_point", d.PerPoint, s)
	w.env.set(e)
	for k, q := range w.q {
		*q = in.qInit[k]
	}
	w.reset()
	w.init.Run()
	for k, t := range w.t {
		w.tInit[k] = *t
	}
	return w
}

// reset pone los colores y los puntos del preset.
func (w *waveInst) reset() {
	*w.r, *w.g, *w.b, *w.a = w.baseR, w.baseG, w.baseB, w.baseA
	*w.nSamples = float64(w.samples)
}

// runFrame evalúa el código por fotograma de la onda; devuelve cuántos puntos
// tiene este fotograma.
func (w *waveInst) runFrame(in *instance, e *env) int {
	w.reset()
	w.env.set(e)
	for k, q := range w.q {
		*q = in.q[k]
	}
	for k, t := range w.t {
		*t = w.tInit[k]
	}
	w.perFrame.Run()
	w.fr, w.fg, w.fb, w.fa = clean(*w.r, 1), clean(*w.g, 1), clean(*w.b, 1), clean(*w.a, 1)
	return int(clamp(clean(*w.nSamples, float64(w.samples)), 0, wavePoints))
}

// point evalúa un punto: sample de 0 a 1, v1 y v2 los dos canales (o el
// espectro). Devuelve la posición (y hacia arriba) y el color.
func (w *waveInst) point(sample, v1, v2 float64) (x, y, r, g, b, a float64) {
	*w.sample, *w.value1, *w.value2 = sample, v1, v2
	*w.x, *w.y = 0.5+v1, 0.5+v2
	*w.r, *w.g, *w.b, *w.a = w.fr, w.fg, w.fb, w.fa
	w.perPoint.Run()
	return clean(*w.x, 0.5), clean(*w.y, 0.5), clean(*w.r, 1), clean(*w.g, 1), clean(*w.b, 1), clean(*w.a, 1)
}

// Las salidas de una forma.
const (
	sSides = iota
	sThick
	sAdditive
	sTextured
	sX
	sY
	sRad
	sAng
	sTexAng
	sTexZoom
	sR
	sG
	sB
	sA
	sR2
	sG2
	sB2
	sA2
	sBorderR
	sBorderG
	sBorderB
	sBorderA
	sNumInst
	nShapeOut
)

// shapeOuts son sus nombres, claves en el archivo y valores por defecto.
var shapeOuts = [nShapeOut]outVar{
	sSides:    {"sides", "", 4},
	sThick:    {"thick", "thickoutline", 0},
	sAdditive: {"additive", "", 0},
	sTextured: {"textured", "", 0},
	sX:        {"x", "", 0.5},
	sY:        {"y", "", 0.5},
	sRad:      {"rad", "", 0.1},
	sAng:      {"ang", "", 0},
	sTexAng:   {"tex_ang", "", 0},
	sTexZoom:  {"tex_zoom", "", 1},
	sR:        {"r", "", 1},
	sG:        {"g", "", 0},
	sB:        {"b", "", 0},
	sA:        {"a", "", 1},
	sR2:       {"r2", "", 0},
	sG2:       {"g2", "", 1},
	sB2:       {"b2", "", 0},
	sA2:       {"a2", "", 0},
	sBorderR:  {"border_r", "", 1},
	sBorderG:  {"border_g", "", 1},
	sBorderB:  {"border_b", "", 1},
	sBorderA:  {"border_a", "", 0.1},
	sNumInst:  {"num_inst", "", 1},
}

// maxInstances es el tope de copias de una forma por fotograma.
const maxInstances = 32

// shapeInst es una forma propia compilada.
type shapeInst struct {
	enabled        bool
	s              *Scope
	init, perFrame *Code
	env            envVars
	q              [nQ]*float64
	t              [nT]*float64
	tInit          [nT]float64
	out            [nShapeOut]*float64
	base           [nShapeOut]float64
	instance       *float64
	vals           [nShapeOut]float64
}

// newShape compila la forma i de in.
func newShape(in *instance, i int, g *Globals, e *env) *shapeInst {
	d := in.p.Shapes[i]
	sh := &shapeInst{enabled: d.Vals["enabled"] != 0}
	for k, o := range shapeOuts {
		sh.base[k] = o.def
		for _, key := range []string{o.key, o.name} {
			if v, ok := d.Vals[key]; ok && key != "" {
				sh.base[k] = v
				break
			}
		}
	}
	if !sh.enabled {
		return sh
	}
	s := NewScope(g)
	sh.s = s
	sh.env = bindEnv(s)
	sh.q, sh.t = bindQ(s), bindT(s)
	for k, o := range shapeOuts {
		sh.out[k] = s.Var(o.name)
	}
	sh.instance = s.Var("instance")
	name := fmt.Sprintf("shape %d ", i)
	sh.init = in.compileOr(name+"init", d.Init, s)
	sh.perFrame = in.compileOr(name+"per_frame", d.PerFrame, s)
	sh.env.set(e)
	for k, q := range sh.q {
		*q = in.qInit[k]
	}
	sh.reset()
	sh.init.Run()
	for k, t := range sh.t {
		sh.tInit[k] = *t
	}
	return sh
}

// reset pone las salidas del preset.
func (sh *shapeInst) reset() {
	for k, p := range sh.out {
		*p = sh.base[k]
	}
}

// instances es cuántas copias dibujar este fotograma.
func (sh *shapeInst) instances() int {
	return int(clamp(sh.base[sNumInst], 1, maxInstances))
}

// runInstance evalúa la copia k y deja sus salidas en vals.
func (sh *shapeInst) runInstance(in *instance, e *env, k int) {
	sh.reset()
	sh.env.set(e)
	for i, q := range sh.q {
		*q = in.q[i]
	}
	for i, t := range sh.t {
		*t = sh.tInit[i]
	}
	*sh.instance = float64(k)
	sh.perFrame.Run()
	for i, p := range sh.out {
		sh.vals[i] = clean(*p, sh.base[i])
	}
}

// clamp deja v entre lo y hi.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// nonZero es v, o def si v es (casi) 0.
func nonZero(v, def float64) float64 {
	if v > -1e-6 && v < 1e-6 {
		return def
	}
	return v
}
