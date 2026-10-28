package sfx

import (
	"math"
	"math/rand/v2"
	"strings"
)

// La voz del locutor de Bloques ("DOUBLE", "TETRA", "GAME OVER"...),
// sintetizada aquí como los demás sonidos, sin archivos: un sintetizador de
// formantes al estilo de Klatt, en pequeño. Una fuente glotal (pulsos de
// Rosenberg) pasa por tres resonadores en cascada, uno por formante, que
// cambian de una vocal a otra; las consonantes son ruido filtrado (S, F,
// las explosiones de P, T, K) o silencios (el cierre de las oclusivas). Sale
// robótica, como una máquina de arcade, y eso se busca.

// Voice es el prefijo de los sonidos del locutor: "voice:double", o una
// frase, con las palabras unidas por "+" ("voice:b2b+tetra").
const Voice = "voice:"

// seg es un tramo de la voz: los formantes a los que va (F1-F3 y sus anchos
// de banda) y cuánto suena cada fuente: la voz (av), el soplo por los
// formantes (ah, la aspiración tras una P o una T) y el ruido de fricción
// por su propio filtro (af, centrado en fc con ancho fbw).
type seg struct {
	dur        float64 // segundos
	f, bw      [3]float64
	av, ah, af float64
	fc, fbw    float64
	stress     bool // la sílaba fuerte: ahí sube el tono
}

// vowel es un tramo de vocal (o de consonante sonora sin ruido).
func vowel(f1, f2, f3, av float64) seg {
	return seg{f: [3]float64{f1, f2, f3}, bw: [3]float64{70, 100, 160}, av: av}
}

// phones son los fonemas que usan las palabras, con los formantes de una voz
// grave de hombre (de las tablas de Peterson y Barney). Las nasales y las
// líquidas suenan más bajo y con los formantes más anchos.
var phones = map[string]seg{
	"IY": vowel(270, 2290, 3010, 1),
	"IH": vowel(390, 1990, 2550, 1),
	"EH": vowel(530, 1840, 2480, 1),
	"AE": vowel(660, 1720, 2410, 1),
	"AH": vowel(640, 1190, 2390, 1),
	"AX": vowel(500, 1400, 2450, 0.8), // la vocal neutra, sin acento
	"AA": vowel(730, 1090, 2440, 1),
	"AO": vowel(570, 840, 2410, 1),
	"UW": vowel(300, 870, 2240, 1),
	"ER": vowel(490, 1350, 1690, 1),
	"L":  {f: [3]float64{380, 1000, 2650}, bw: [3]float64{80, 150, 250}, av: 0.6},
	"R":  {f: [3]float64{310, 1060, 1380}, bw: [3]float64{80, 120, 180}, av: 0.7},
	"M":  {f: [3]float64{280, 1000, 2200}, bw: [3]float64{100, 250, 350}, av: 0.4},
	"N":  {f: [3]float64{280, 1700, 2600}, bw: [3]float64{100, 250, 350}, av: 0.4},
	"NG": {f: [3]float64{280, 2200, 2700}, bw: [3]float64{100, 250, 350}, av: 0.4},
	"V":  {f: [3]float64{300, 1100, 2400}, bw: [3]float64{90, 140, 220}, av: 0.45, af: 0.15, fc: 4000, fbw: 3500},
	"F":  {f: [3]float64{400, 1400, 2500}, bw: [3]float64{90, 140, 220}, af: 0.3, fc: 4500, fbw: 4500},
	"S":  {f: [3]float64{400, 1600, 2600}, bw: [3]float64{90, 140, 220}, af: 1, fc: 5500, fbw: 1800},
}

// stop es una oclusiva (B D G P T K): el cierre (con un murmullo de voz si es
// sonora), la explosión de ruido y, en las sordas, el soplo antes de la
// vocal. Los formantes del cierre son el punto del que parten las
// transiciones (el "locus"): de ahí se distingue una B de una D.
func stop(p string, aspirate bool) []seg {
	var locus [3]float64
	var fc, fbw, burst float64
	switch p {
	case "B", "P":
		locus, fc, fbw, burst = [3]float64{200, 900, 2100}, 1200, 1500, 0.5
	case "D", "T":
		locus, fc, fbw, burst = [3]float64{200, 1700, 2600}, 4200, 2500, 1
	case "G", "K":
		locus, fc, fbw, burst = [3]float64{200, 1900, 2400}, 2200, 1200, 0.8
	}
	bw := [3]float64{90, 140, 220}
	voiced := p == "B" || p == "D" || p == "G"
	closure := seg{dur: 0.055, f: locus, bw: bw}
	if voiced {
		closure.av, burst = 0.12, burst*0.6
	}
	out := []seg{closure, {dur: 0.012, f: locus, bw: bw, af: burst, fc: fc, fbw: fbw}}
	if !voiced && aspirate {
		out = append(out, seg{dur: 0.045, f: locus, bw: bw, ah: 0.5})
	}
	return out
}

// words son las palabras del locutor, fonema a fonema: "AH:140!" es la vocal
// AH durante 140 ms y con el acento (!). Las oclusivas van sin duración; "P-"
// es una P sin soplo (detrás de una S, como en "spin"). Los diptongos son dos
// vocales seguidas.
var words = map[string]string{
	"single": "S:110 IH:130! NG:70 G AX:55 L:120",
	"double": "D AH:140! B AX:60 L:120",
	"triple": "T R:70 IH:120! P AX:55 L:120",
	"tetra":  "T EH:130! T R:70 AX:120",
	"tspin":  "T IY:140 S:110 P- IH:130! N:120",
	"b2b":    "B AE:140! K T UW:90 B AE:160! K",
	"level":  "L:70 EH:120! V:60 AX:50 L:80 AH:150! P",
	"fever":  "F:100 IY:160! V:60 ER:150",
	"over":   "G EY:170! M:90 OW:150! V:60 ER:160",
}

// diphthongs son las vocales que se mueven: dos tramos, el primero más largo.
var diphthongs = map[string][2]string{"EY": {"EH", "IY"}, "OW": {"AO", "UW"}}

// parse convierte una palabra de words en sus tramos.
func parse(w string) []seg {
	var out []seg
	for _, tok := range strings.Fields(w) {
		name, ms, _ := strings.Cut(tok, ":")
		stress := strings.HasSuffix(ms, "!")
		ms = strings.TrimSuffix(ms, "!")
		switch {
		case strings.HasSuffix(name, "-"):
			out = append(out, stop(strings.TrimSuffix(name, "-"), false)...)
			continue
		case ms == "":
			out = append(out, stop(name, true)...)
			continue
		}
		d := 0.0
		for _, c := range ms {
			d = d*10 + float64(c-'0')
		}
		d /= 1000
		if dp, ok := diphthongs[name]; ok {
			a, b := phones[dp[0]], phones[dp[1]]
			a.dur, a.stress = d*0.6, stress
			b.dur = d * 0.4
			out = append(out, a, b)
			continue
		}
		s := phones[name]
		s.dur, s.stress = d, stress
		out = append(out, s)
	}
	return out
}

// resonator es un resonador de dos polos (el de Klatt), de ganancia 1 en
// continua.
type resonator struct{ y1, y2 float64 }

// step filtra una muestra con el formante f de ancho bw.
func (r *resonator) step(x, f, bw float64) float64 {
	const T = 1.0 / Rate
	c := -math.Exp(-2 * math.Pi * bw * T)
	b := 2 * math.Exp(-math.Pi*bw*T) * math.Cos(2*math.Pi*f*T)
	a := 1 - b - c
	y := a*x + b*r.y1 + c*r.y2
	r.y2, r.y1 = r.y1, y
	return y
}

// bandpass es un pasabanda de dos polos con ganancia 1 en el centro, para el
// ruido de las consonantes.
type bandpass struct{ x1, x2, y1, y2 float64 }

func (p *bandpass) step(x, fc, bw float64) float64 {
	w := 2 * math.Pi * fc / Rate
	q := fc / bw
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	y := (alpha*x - alpha*p.x2 + 2*math.Cos(w)*p.y1 - (1-alpha)*p.y2) / a0
	p.x2, p.x1 = p.x1, x
	p.y2, p.y1 = p.y1, y
	return y
}

// fricGain es el volumen del ruido de las consonantes frente a la voz.
const fricGain = 0.12

// tract escala los formantes: menos de 1 es una voz más grave.
const tract = 0.9

// speak sintetiza una palabra: mono, a Rate, normalizada.
func speak(segs []seg) []float64 {
	total := 0.0
	var stresses []float64 // los centros de las sílabas fuertes, en segundos
	for _, s := range segs {
		if s.stress {
			stresses = append(stresses, total+s.dur/2)
		}
		total += s.dur
	}
	n := int(total * Rate)
	out := make([]float64, n)
	rng := rand.New(rand.NewPCG(7, 11))
	var r1, r2, r3, r4 resonator
	var fric bandpass
	var phase, prevPulse, prevV float64
	si, segStart := 0, 0.0
	prev := segs[0]
	// Los valores de ahora, que van hacia los del tramo: los formantes en
	// 40 ms (las transiciones que distinguen las consonantes), el volumen de
	// cada fuente en 8 ms.
	cur := segs[0]
	for i := 0; i < n; i++ {
		t := float64(i) / Rate
		for si < len(segs)-1 && t >= segStart+segs[si].dur {
			segStart += segs[si].dur
			prev, si = cur, si+1
		}
		s := segs[si]
		in := t - segStart
		k := math.Min(1, in/math.Min(0.04, s.dur*0.6))
		ka := math.Min(1, in/0.008)
		for j := 0; j < 3; j++ {
			cur.f[j] = prev.f[j] + (s.f[j]-prev.f[j])*k
			cur.bw[j] = prev.bw[j] + (s.bw[j]-prev.bw[j])*k
		}
		cur.av = prev.av + (s.av-prev.av)*ka
		cur.ah = prev.ah + (s.ah-prev.ah)*ka
		cur.af = prev.af + (s.af-prev.af)*ka
		cur.fc, cur.fbw = s.fc, s.fbw
		// El tono, de locutor grave (de tráiler): baja a lo largo de la
		// palabra y sube un poco en cada sílaba fuerte.
		f0 := 88 - 24*t/total
		for _, c := range stresses {
			f0 += 14 * math.Exp(-(t-c)*(t-c)/(2*0.07*0.07))
		}
		// El pulso glotal de Rosenberg (abre el 40 % del periodo, cierra en
		// el 16 %) y su derivada, que es lo que radian los labios.
		phase += f0 / Rate
		phase -= math.Floor(phase)
		var pulse float64
		switch {
		case phase < 0.4:
			pulse = 0.5 * (1 - math.Cos(math.Pi*phase/0.4))
		case phase < 0.56:
			pulse = math.Cos(math.Pi * (phase - 0.4) / 0.32)
		}
		dp := (pulse - prevPulse) * Rate / 400 // en torno a ±1
		prevPulse = pulse
		noise := rng.Float64()*2 - 1
		src := cur.av*dp + cur.ah*noise*0.25
		// Los formantes, un 10 % más bajos que los de las tablas: un tracto
		// vocal más largo, una voz más grande.
		v := r1.step(src, cur.f[0]*tract, cur.bw[0])
		v = r2.step(v, cur.f[1]*tract, cur.bw[1])
		v = r3.step(v, cur.f[2]*tract, cur.bw[2])
		v = r4.step(v, 3500*tract, 250)
		// Pre-énfasis: sube los agudos de la voz, que si no queda tapada
		// (todo el peso en F1).
		v, prevV = v-0.9*prevV, v
		if cur.af > 0 && cur.fc > 0 {
			v += cur.af * fric.step(noise, cur.fc, cur.fbw) * fricGain
		}
		out[i] = v
	}
	// Normalizada al pico, con 5 ms de entrada y de salida.
	peak := 0.0
	for _, v := range out {
		peak = math.Max(peak, math.Abs(v))
	}
	fade := int(0.005 * Rate)
	for i := range out {
		if peak > 0 {
			out[i] /= peak
		}
		out[i] *= math.Min(1, math.Min(float64(i), float64(n-1-i))/float64(fade))
	}
	return out
}

// retro le da el color de las máquinas de voz de antes: las muestras se
// sostienen a 24 kHz y se redondean a 8 bits (con menos, el siseo tapaba
// las consonantes).
func retro(x []float64) []float64 {
	const hold, levels = 2, 128
	out := make([]float64, len(x))
	for i := range x {
		v := x[i-i%hold]
		out[i] = math.Round(v*levels) / levels
	}
	return out
}

// say sintetiza el sonido "voice:..." (una palabra o varias con "+"), en
// estéreo intercalado; nil si alguna palabra no existe.
func say(name string) []float32 {
	var mono []float64
	for i, w := range strings.Split(strings.TrimPrefix(name, Voice), "+") {
		def, ok := words[w]
		if !ok {
			return nil
		}
		if i > 0 {
			mono = append(mono, make([]float64, int(0.07*Rate))...) // la pausa entre palabras
		}
		mono = append(mono, retro(hard(speak(parse(def))))...)
	}
	left, right := echo(mono)
	out := make([]float32, 2*len(left))
	for i := range left {
		out[2*i], out[2*i+1] = float32(left[i]*0.95), float32(right[i]*0.95)
	}
	return out
}

// Ajustes del eco: cada repetición llega echoDelay después, más baja
// (echoFeedback) y más apagada (un filtro de un polo con el corte en
// echoTone), alternando de lado, como en un estadio. Encima, una
// reverberación corta (reverbWet) que llena el hueco entre repeticiones.
const (
	echoDelay    = 0.17
	echoFeedback = 0.6
	echoRepeats  = 5
	echoTone     = 3500.0
	reverbWet    = 0.45
	echoDry      = 1.4 // la voz seca frente al eco (luego se normaliza todo)
)

// hardDrive es cuánto se satura la voz (hard): más, más áspera.
const hardDrive = 3.0

// hard endurece la voz como un locutor de estadio por megáfono: la satura
// con una tangente hiperbólica (le sube el cuerpo y le añade armónicos, y
// con ellos se oye por encima de la música) y la vuelve a normalizar.
func hard(x []float64) []float64 {
	out := make([]float64, len(x))
	norm := math.Tanh(hardDrive)
	for i, v := range x {
		out[i] = math.Tanh(hardDrive*v) / norm
	}
	return out
}

// reverb suma a x una reverberación de Schroeder: cuatro filtros peine en
// paralelo (la cola, ~1,2 s) y dos pasatodo en serie (la difuminan). side
// cambia un poco los retardos de un canal a otro, para que suene abierta.
func reverb(x []float64, side int) []float64 {
	combs := []float64{0.0297, 0.0371, 0.0411, 0.0437}
	const fb = 0.78
	wet := make([]float64, len(x))
	for _, c := range combs {
		d := int((c + 0.0011*float64(side)) * Rate)
		buf := make([]float64, d)
		lp := 0.0
		for i := range x {
			y := buf[i%d]
			lp += 0.4 * (y - lp) // la cola se apaga antes en los agudos
			buf[i%d] = x[i] + fb*lp
			wet[i] += y / float64(len(combs))
		}
	}
	for _, c := range []float64{0.005, 0.0017} {
		d := int(c * Rate)
		buf := make([]float64, d)
		for i := range wet {
			b := buf[i%d]
			y := -0.7*wet[i] + b
			buf[i%d] = wet[i] + 0.7*y
			wet[i] = y
		}
	}
	out := make([]float64, len(x))
	for i := range x {
		out[i] = x[i] + reverbWet*wet[i]
	}
	return out
}

// echo le pone el eco a la voz: la seca en los dos lados y las repeticiones,
// una a la izquierda y la siguiente a la derecha. Devuelve los dos canales,
// más largos que la voz para que se oiga la cola.
func echo(dry []float64) (left, right []float64) {
	d := int(echoDelay * Rate)
	n := len(dry) + echoRepeats*d + int(0.5*Rate) // y medio segundo de cola de la reverberación
	left, right = make([]float64, n), make([]float64, n)
	// La voz seca va más fuerte que su eco: que se oiga dura, no lavada.
	for i, v := range dry {
		left[i], right[i] = v*echoDry, v*echoDry
	}
	a := 1 - math.Exp(-2*math.Pi*echoTone/Rate)
	wet := append([]float64(nil), dry...)
	gain := 1.0
	for r := 1; r <= echoRepeats; r++ {
		gain *= echoFeedback
		lp := 0.0
		for i := range wet {
			lp += a * (wet[i] - lp)
			wet[i] = lp
		}
		dst := left
		if r%2 == 0 {
			dst = right
		}
		for i, v := range wet {
			dst[r*d+i] += v * gain
		}
	}
	left, right = reverb(left, 0), reverb(right, 1)
	// Que la suma no sature: la voz va normalizada a 1 y el eco suma encima.
	peak := 0.0
	for i := range left {
		peak = math.Max(peak, math.Max(math.Abs(left[i]), math.Abs(right[i])))
	}
	if peak > 1 {
		for i := range left {
			left[i] /= peak
			right[i] /= peak
		}
	}
	return left, right
}
