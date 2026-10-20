package milkdrop

import "math"

// Las variables de cada contexto: qué entra (el tiempo, el sonido...), qué
// sale (el zoom, el color de la onda...) y con qué valor empieza cada
// fotograma (el del preset o, si no lo trae, el de MilkDrop).

// Las salidas de las ecuaciones por fotograma, por su índice en frameVals.
const (
	oZoom = iota
	oZoomExp
	oRot
	oWarp
	oCX
	oCY
	oDX
	oDY
	oSX
	oSY
	oDecay
	oGamma
	oEchoZoom
	oEchoAlpha
	oEchoOrient
	oWaveMode
	oWaveR
	oWaveG
	oWaveB
	oWaveA
	oWaveX
	oWaveY
	oWaveMystery
	oWaveThick
	oWaveDots
	oWaveAdd
	oWaveBright
	oWaveScale
	oWaveSmooth
	oModAlpha
	oModAlphaStart
	oModAlphaEnd
	oOBSize
	oOBR
	oOBG
	oOBB
	oOBA
	oIBSize
	oIBR
	oIBG
	oIBB
	oIBA
	oMVX
	oMVY
	oMVDX
	oMVDY
	oMVL
	oMVR
	oMVG
	oMVB
	oMVA
	oDarkenCenter
	oInvert
	oBrighten
	oDarken
	oSolarize
	oWrap
	oWarpSpeed
	oWarpScale
	oShader
	nOut
)

// outVar es una salida: su nombre en las ecuaciones, su clave en el archivo
// (vacía si es la misma) y su valor por defecto.
type outVar struct {
	name, key string
	def       float64
}

// outVars son las salidas, con los valores por defecto de MilkDrop.
var outVars = [nOut]outVar{
	oZoom:          {"zoom", "", 1},
	oZoomExp:       {"zoomexp", "fzoomexponent", 1},
	oRot:           {"rot", "", 0},
	oWarp:          {"warp", "", 1},
	oCX:            {"cx", "", 0.5},
	oCY:            {"cy", "", 0.5},
	oDX:            {"dx", "", 0},
	oDY:            {"dy", "", 0},
	oSX:            {"sx", "", 1},
	oSY:            {"sy", "", 1},
	oDecay:         {"decay", "fdecay", 0.98},
	oGamma:         {"gamma", "fgammaadj", 2},
	oEchoZoom:      {"echo_zoom", "fvideoechozoom", 2},
	oEchoAlpha:     {"echo_alpha", "fvideoechoalpha", 0},
	oEchoOrient:    {"echo_orient", "nvideoechoorientation", 0},
	oWaveMode:      {"wave_mode", "nwavemode", 0},
	oWaveR:         {"wave_r", "", 1},
	oWaveG:         {"wave_g", "", 1},
	oWaveB:         {"wave_b", "", 1},
	oWaveA:         {"wave_a", "fwavealpha", 0.8},
	oWaveX:         {"wave_x", "", 0.5},
	oWaveY:         {"wave_y", "", 0.5},
	oWaveMystery:   {"wave_mystery", "fwaveparam", 0},
	oWaveThick:     {"wave_thick", "bwavethick", 0},
	oWaveDots:      {"wave_usedots", "bwavedots", 0},
	oWaveAdd:       {"additivewave", "badditivewaves", 0},
	oWaveBright:    {"wave_brighten", "bmaximizewavecolor", 1},
	oWaveScale:     {"wave_scale", "fwavescale", 1},
	oWaveSmooth:    {"wave_smoothing", "fwavesmoothing", 0.75},
	oModAlpha:      {"modwavealphabyvolume", "bmodwavealphabyvolume", 0},
	oModAlphaStart: {"modwavealphastart", "fmodwavealphastart", 0.75},
	oModAlphaEnd:   {"modwavealphaend", "fmodwavealphaend", 0.95},
	oOBSize:        {"ob_size", "", 0.01},
	oOBR:           {"ob_r", "", 0},
	oOBG:           {"ob_g", "", 0},
	oOBB:           {"ob_b", "", 0},
	oOBA:           {"ob_a", "", 0},
	oIBSize:        {"ib_size", "", 0.01},
	oIBR:           {"ib_r", "", 0.25},
	oIBG:           {"ib_g", "", 0.25},
	oIBB:           {"ib_b", "", 0.25},
	oIBA:           {"ib_a", "", 0},
	oMVX:           {"mv_x", "nmotionvectorsx", 12},
	oMVY:           {"mv_y", "nmotionvectorsy", 9},
	oMVDX:          {"mv_dx", "", 0},
	oMVDY:          {"mv_dy", "", 0},
	oMVL:           {"mv_l", "", 0.9},
	oMVR:           {"mv_r", "", 1},
	oMVG:           {"mv_g", "", 1},
	oMVB:           {"mv_b", "", 1},
	oMVA:           {"mv_a", "", 0},
	oDarkenCenter:  {"darken_center", "bdarkencenter", 0},
	oInvert:        {"invert", "binvert", 0},
	oBrighten:      {"brighten", "bbrighten", 0},
	oDarken:        {"darken", "bdarken", 0},
	oSolarize:      {"solarize", "bsolarize", 0},
	oWrap:          {"wrap", "btexwrap", 1},
	oWarpSpeed:     {"warpanimspeed", "fwarpanimspeed", 1},
	oWarpScale:     {"warpscale", "fwarpscale", 1},
	oShader:        {"fshader", "fshader", 0},
}

// motionOuts son las salidas que también se pueden cambiar en cada vértice.
var motionOuts = [...]int{oZoom, oZoomExp, oRot, oWarp, oCX, oCY, oDX, oDY, oSX, oSY}

// frameVals son los valores de las salidas de un fotograma.
type frameVals [nOut]float64

// baseOf es el valor inicial de la salida i en el preset p.
func baseOf(p *Preset, i int) float64 {
	o := outVars[i]
	for _, k := range []string{o.key, o.name} {
		if k == "" {
			continue
		}
		if v, ok := p.Vals[k]; ok {
			return v
		}
	}
	return o.def
}

// env es lo que entra a todos los contextos en un fotograma.
type env struct {
	time, fps, frame, progress float64
	bass, mid, treb            float64
	bassAtt, midAtt, trebAtt   float64
	meshX, meshY               float64
	aspectX, aspectY           float64
	pixelsX, pixelsY           float64
}

// envVars son los punteros de las entradas en un contexto.
type envVars struct {
	time, fps, frame, progress *float64
	bass, mid, treb            *float64
	bassAtt, midAtt, trebAtt   *float64
	meshX, meshY               *float64
	aspectX, aspectY           *float64
	pixelsX, pixelsY           *float64
}

// bindEnv busca las entradas en s.
func bindEnv(s *Scope) envVars {
	return envVars{
		time: s.Var("time"), fps: s.Var("fps"), frame: s.Var("frame"), progress: s.Var("progress"),
		bass: s.Var("bass"), mid: s.Var("mid"), treb: s.Var("treb"),
		bassAtt: s.Var("bass_att"), midAtt: s.Var("mid_att"), trebAtt: s.Var("treb_att"),
		meshX: s.Var("meshx"), meshY: s.Var("meshy"),
		aspectX: s.Var("aspectx"), aspectY: s.Var("aspecty"),
		pixelsX: s.Var("pixelsx"), pixelsY: s.Var("pixelsy"),
	}
}

// set pone las entradas.
func (v *envVars) set(e *env) {
	*v.time, *v.fps, *v.frame, *v.progress = e.time, e.fps, e.frame, e.progress
	*v.bass, *v.mid, *v.treb = e.bass, e.mid, e.treb
	*v.bassAtt, *v.midAtt, *v.trebAtt = e.bassAtt, e.midAtt, e.trebAtt
	*v.meshX, *v.meshY = e.meshX, e.meshY
	*v.aspectX, *v.aspectY = e.aspectX, e.aspectY
	*v.pixelsX, *v.pixelsY = e.pixelsX, e.pixelsY
}

// Las q (q1..q32) pasan de las ecuaciones por fotograma a las de los
// vértices, las ondas y las formas; las t (t1..t8), de las ecuaciones por
// fotograma de una onda o forma a las de cada punto.
const (
	nQ = 32
	nT = 8
)

// qNames y tNames son sus nombres.
var qNames, tNames = func() (q [nQ]string, t [nT]string) {
	for i := range q {
		q[i] = "q" + itoa(i+1)
	}
	for i := range t {
		t[i] = "t" + itoa(i+1)
	}
	return
}()

// itoa es strconv.Itoa para números pequeños.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// bindQ busca las q en s.
func bindQ(s *Scope) (q [nQ]*float64) {
	for i, n := range qNames {
		q[i] = s.Var(n)
	}
	return
}

// bindT busca las t en s.
func bindT(s *Scope) (t [nT]*float64) {
	for i, n := range tNames {
		t[i] = s.Var(n)
	}
	return
}

// clean deja un valor usable: sin NaN ni infinitos (si no, def).
func clean(v, def float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return def
	}
	return v
}
