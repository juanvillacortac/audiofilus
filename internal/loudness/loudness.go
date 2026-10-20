// Package loudness mide la sonoridad de una canción según EBU R128 / ITU-R
// BS.1770-4: filtro de ponderación K a la frecuencia de la fuente, bloques
// de 400 ms que se solapan un 75 %, puerta absoluta de -70 LUFS y relativa
// de -10 LU, y el pico verdadero (sobremuestreado). De ahí sale el
// ReplayGain 2.0 (ganancia = -18 - LUFS) para las canciones que no lo
// traen en sus etiquetas.
//
// Se lee la fuente a trozos: la memoria no crece con el audio salvo por la
// energía de cada bloque de 100 ms (un float64 cada uno: una hora son 280
// KB), que hace falta para la puerta relativa y para medir un álbum entero
// como si fuera una sola pista.
package loudness

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// ReferenceLUFS es la sonoridad a la que lleva ReplayGain 2.0.
const ReferenceLUFS = -18.0

// Options son las opciones del análisis.
type Options struct {
	// SamplePeak mide el pico de las muestras tal cual, sin sobremuestrear
	// (más rápido; el pico verdadero puede ser algo mayor).
	SamplePeak bool
}

// Result es lo medido.
type Result struct {
	LUFS     float64 // sonoridad integrada (math.Inf(-1) si es silencio)
	Gain     float64 // ReplayGain 2.0 en dB: -18 - LUFS (0 si es silencio)
	Peak     float64 // el pico, lineal (1 = 0 dBFS)
	TruePeak bool    // Peak es el pico verdadero (sobremuestreado)
	Seconds  float64 // la duración medida
	Silent   bool    // ningún bloque pasó la puerta absoluta
}

// biquad es un filtro de segundo orden (forma directa I).
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// kWeighting son los dos filtros de la ponderación K a la frecuencia rate:
// el estante de agudos (la cabeza) y el paso alto RLB. Los coeficientes se
// calculan como en libebur128 a partir de los parámetros que dan los de
// BS.1770 a 48 kHz, así sirven a cualquier frecuencia.
func kWeighting(rate float64) (shelf, highpass biquad) {
	f0, g, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / rate)
	vh := math.Pow(10, g/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf = biquad{
		b0: (vh + vb*k/q + k*k) / a0,
		b1: 2 * (k*k - vh) / a0,
		b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/q + k*k) / a0,
	}
	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / rate)
	d := 1 + k/q + k*k
	highpass = biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / d, a2: (1 - k/q + k*k) / d}
	return shelf, highpass
}

// Analyzer mide un flujo estéreo a trozos (Add) y da el resultado al final.
type Analyzer struct {
	rate     int
	opts     Options
	filt     [2][2]biquad // por canal: estante y paso alto
	subSum   [2]float64   // la energía del bloque de 100 ms en curso, por canal
	subN     int          // las muestras (frames) que lleva
	subIndex int          // qué bloque de 100 ms es (para sus límites)
	subEnd   int64        // en qué frame termina el bloque en curso
	frames   int64
	subs     [][2]float64 // la energía media de cada bloque de 100 ms, por canal
	peak     float64
	mono     bool // hasta ahora los dos canales son iguales (un mono duplicado)
	tp       [2]truePeak
}

// NewAnalyzer prepara la medida de un flujo estéreo a rate muestras por
// segundo.
func NewAnalyzer(rate int, opts Options) *Analyzer {
	a := &Analyzer{rate: rate, opts: opts, mono: true}
	for c := range a.filt {
		a.filt[c][0], a.filt[c][1] = kWeighting(float64(rate))
	}
	a.subEnd = a.subBoundary(1)
	if !opts.SamplePeak {
		for c := range a.tp {
			a.tp[c] = newTruePeak(rate)
		}
	}
	return a
}

// subBoundary es el frame donde termina el bloque de 100 ms número i (con
// frecuencias que no dan un número entero de muestras, como 11025 Hz).
func (a *Analyzer) subBoundary(i int) int64 {
	return int64(math.Round(float64(i) * float64(a.rate) / 10))
}

// Add mide más muestras estéreo intercaladas (I, D, I, D...).
func (a *Analyzer) Add(samples []float32) {
	for i := 0; i+1 < len(samples); i += 2 {
		l, r := float64(samples[i]), float64(samples[i+1])
		if l != r {
			a.mono = false
		}
		if a.opts.SamplePeak {
			a.peak = math.Max(a.peak, math.Max(math.Abs(l), math.Abs(r)))
		} else {
			a.peak = math.Max(a.peak, math.Max(a.tp[0].push(l), a.tp[1].push(r)))
		}
		yl := a.filt[0][1].process(a.filt[0][0].process(l))
		yr := a.filt[1][1].process(a.filt[1][0].process(r))
		a.subSum[0] += yl * yl
		a.subSum[1] += yr * yr
		a.subN++
		a.frames++
		if a.frames >= a.subEnd {
			n := float64(a.subN)
			a.subs = append(a.subs, [2]float64{a.subSum[0] / n, a.subSum[1] / n})
			a.subSum, a.subN = [2]float64{}, 0
			a.subIndex++
			a.subEnd = a.subBoundary(a.subIndex + 1)
		}
	}
}

// blocks son las energías de los bloques de 400 ms (cada 100 ms), ya
// sumados los canales (o solo uno, si era un mono duplicado: BS.1770 mide
// un mono como un canal).
func (a *Analyzer) blocks() []float64 {
	if len(a.subs) < 4 {
		return nil
	}
	out := make([]float64, 0, len(a.subs)-3)
	for i := 3; i < len(a.subs); i++ {
		var z [2]float64
		for _, s := range a.subs[i-3 : i+1] {
			z[0] += s[0] / 4
			z[1] += s[1] / 4
		}
		e := z[0] + z[1]
		if a.mono {
			e = z[0]
		}
		out = append(out, e)
	}
	return out
}

// lufs es la sonoridad de una energía.
func lufs(e float64) float64 { return -0.691 + 10*math.Log10(e) }

// gated es la sonoridad integrada de unos bloques, con las dos puertas.
func gated(blocks []float64) (float64, bool) {
	var sum float64
	n := 0
	for _, e := range blocks {
		if e > 0 && lufs(e) > -70 {
			sum += e
			n++
		}
	}
	if n == 0 {
		return math.Inf(-1), false
	}
	rel := lufs(sum/float64(n)) - 10
	sum, n = 0, 0
	for _, e := range blocks {
		if e > 0 && lufs(e) > -70 && lufs(e) > rel {
			sum += e
			n++
		}
	}
	if n == 0 {
		return math.Inf(-1), false
	}
	return lufs(sum / float64(n)), true
}

// Result es lo medido hasta ahora.
func (a *Analyzer) Result() Result {
	l, ok := gated(a.blocks())
	return result(l, ok, a.peak, !a.opts.SamplePeak, float64(a.frames)/float64(a.rate))
}

func result(l float64, ok bool, peak float64, tp bool, secs float64) Result {
	r := Result{LUFS: l, Peak: peak, TruePeak: tp, Seconds: secs, Silent: !ok}
	if ok {
		r.Gain = ReferenceLUFS - l
	}
	return r
}

// Album mide varias pistas como un álbum: la puerta sobre todos sus bloques
// juntos (como si sonaran seguidas) y el pico mayor.
func Album(tracks ...*Analyzer) Result {
	var all []float64
	peak, secs, tp := 0.0, 0.0, len(tracks) > 0
	for _, t := range tracks {
		all = append(all, t.blocks()...)
		peak = math.Max(peak, t.peak)
		secs += float64(t.frames) / float64(t.rate)
		tp = tp && !t.opts.SamplePeak
	}
	l, ok := gated(all)
	return result(l, ok, peak, tp, secs)
}

// Progress recibe cuántos frames se llevan medidos y cuántos hay (total -1
// si la fuente no lo sabe).
type Progress func(done, total int64)

// Measure mide una fuente entera (y la deja leída; no la cierra).
func Measure(ctx context.Context, src decode.Source, opts Options, progress Progress) (*Analyzer, error) {
	a := NewAnalyzer(src.SampleRate(), opts)
	buf := make([]float32, 8192)
	total := src.Length()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := src.Read(buf)
		a.Add(buf[:n])
		if progress != nil {
			progress(a.frames, total)
		}
		if errors.Is(err, io.EOF) {
			return a, nil
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return a, nil
		}
	}
}

// File mide un archivo (lo que decode sabe abrir).
func File(ctx context.Context, path string, opts Options, progress Progress) (*Analyzer, error) {
	src, err := decode.Open(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return Measure(ctx, src, opts, progress)
}
