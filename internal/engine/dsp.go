package engine

import (
	"math"
	"slices"

	"github.com/juanvillacortac/audiofilus/internal/library"
)

// DSP es lo que el engine le hace al sonido además del volumen: ReplayGain
// (igualar lo fuerte que suena cada canción), el ecualizador y el fundido
// entre canciones. Lo guarda el servicio en data/state.json.
type DSP struct {
	// ReplayGain: "off", "track" (cada canción a su nivel), "album" (cada
	// disco a su nivel, respetando lo que suena más o menos fuerte dentro de
	// él) o "auto" (album en orden, track en aleatorio). Vacío es "auto".
	ReplayGain string `json:"replaygain,omitempty"`
	// Preamp se suma a la ganancia de ReplayGain (dB); las canciones sin
	// etiquetas no la llevan.
	Preamp float64 `json:"preamp_db,omitempty"`
	// EQ son las ganancias (dB, de -12 a 12) de las bandas de EQBands; vacío
	// o todo 0 es sin ecualizar. EQPreset es el nombre del preajuste elegido
	// ("custom" si se tocó a mano).
	EQ       []float64 `json:"eq,omitempty"`
	EQPreset string    `json:"eq_preset,omitempty"`
	// Crossfade son los segundos que se mezclan el final de una canción y el
	// principio de la siguiente (0: nada, como siempre, sin silencio). Entre
	// canciones seguidas del mismo disco, en orden, no se mezcla: los discos
	// en vivo o conceptuales van pegados.
	Crossfade float64 `json:"crossfade,omitempty"`
}

// EQBands son las frecuencias centrales del ecualizador (Hz), por octavas
// como los ecualizadores gráficos de siempre.
var EQBands = []float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// EQPresets son los preajustes del ecualizador, en el orden en que se
// ofrecen.
var EQPresets = []struct {
	Name  string
	Gains []float64
}{
	{"flat", []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"bass", []float64{6, 5, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"treble", []float64{0, 0, 0, 0, 0, 0, 2, 4, 5, 6}},
	{"vocal", []float64{-2, -2, -1, 1, 3, 4, 3, 1, 0, -1}},
	{"rock", []float64{5, 4, 2, -1, -2, -1, 1, 3, 4, 5}},
	{"metal", []float64{4, 3, 1, 0, -2, -1, 1, 3, 4, 3}},
	{"pop", []float64{-1, 1, 3, 4, 3, 0, -1, -1, 1, 2}},
	{"electronic", []float64{5, 4, 1, 0, -2, 1, 0, 1, 4, 5}},
	{"jazz", []float64{3, 2, 1, 2, -1, -1, 0, 1, 2, 3}},
	{"classical", []float64{4, 3, 2, 1, -1, -1, 0, 2, 3, 4}},
	{"loudness", []float64{6, 4, 0, 0, -2, 0, -1, -2, 3, 5}},
}

// PresetGains son las ganancias del preajuste name (nil si no existe).
func PresetGains(name string) []float64 {
	for _, p := range EQPresets {
		if p.Name == name {
			return slices.Clone(p.Gains)
		}
	}
	return nil
}

// Normalize deja la configuración en valores válidos.
func (d DSP) Normalize() DSP {
	switch d.ReplayGain {
	case "off", "track", "album", "auto":
	default:
		d.ReplayGain = "auto"
	}
	d.Preamp = math.Max(-12, math.Min(12, d.Preamp))
	if len(d.EQ) != len(EQBands) {
		d.EQ = nil
	}
	flat := true
	for i := range d.EQ {
		d.EQ[i] = math.Max(-12, math.Min(12, d.EQ[i]))
		if d.EQ[i] != 0 {
			flat = false
		}
	}
	if flat {
		d.EQ = nil
		if d.EQPreset == "" || d.EQPreset == "custom" {
			d.EQPreset = "flat"
		}
	}
	d.Crossfade = math.Max(0, math.Min(12, d.Crossfade))
	return d
}

// replayGain es la ganancia lineal de ReplayGain para una canción con
// etiquetas g (ok: las tiene), en el modo mode y con shuffle o no. Nunca
// sube más de lo que su pico permite (así no recorta).
func replayGain(d DSP, g library.Gain, ok, shuffle bool) float32 {
	mode := d.ReplayGain
	if mode == "auto" || mode == "" {
		mode = "album"
		if shuffle {
			mode = "track"
		}
	}
	if mode == "off" || !ok {
		return 1
	}
	db, peak := g.Track, g.TrackPeak
	if (mode == "album" && g.HasAlbum) || !g.HasTrack {
		db, peak = g.Album, g.AlbumPeak
	}
	lin := math.Pow(10, (db+d.Preamp)/20)
	if peak > 0 && lin*peak > 1 {
		lin = 1 / peak
	}
	return float32(lin)
}

// biquad es un filtro de segundo orden (forma directa I) por canal: un
// "peaking" del ecualizador.
type biquad struct {
	b0, b1, b2, a1, a2 float32
	x1, x2, y1, y2     [2]float32
}

// peaking prepara un filtro que sube o baja gain dB alrededor de f (Hz),
// con ancho de una octava (Q = 1,41), a la frecuencia rate (las fórmulas
// del "Audio EQ Cookbook" de Robert Bristow-Johnson).
func peaking(f, gain float64, rate int) biquad {
	a := math.Pow(10, gain/40)
	w := 2 * math.Pi * f / float64(rate)
	alpha := math.Sin(w) / (2 * 1.41)
	a0 := 1 + alpha/a
	return biquad{
		b0: float32((1 + alpha*a) / a0),
		b1: float32(-2 * math.Cos(w) / a0),
		b2: float32((1 - alpha*a) / a0),
		a1: float32(-2 * math.Cos(w) / a0),
		a2: float32((1 - alpha/a) / a0),
	}
}

// eqFilter es el ecualizador: solo las bandas que no están a 0 (en una
// portátil cada filtro cuesta), y una bajada previa para que lo que se
// sube no recorte.
type eqFilter struct {
	bands []biquad
	pre   float32
}

// set prepara el ecualizador para gains (nil: apagado) a la frecuencia rate.
func (q *eqFilter) set(gains []float64, rate int) {
	q.bands, q.pre = nil, 1
	top := 0.0
	for i, g := range gains {
		if g == 0 || i >= len(EQBands) {
			continue
		}
		// Por encima de la frecuencia de Nyquist la banda no existe.
		if EQBands[i] >= float64(rate)*0.45 {
			continue
		}
		q.bands = append(q.bands, peaking(EQBands[i], g, rate))
		top = math.Max(top, g)
	}
	// Lo que más sube, se baja antes (una parte: las bandas vecinas se
	// suman, pero el limitador de después recoge lo que se pase).
	q.pre = float32(math.Pow(10, -top*0.8/20))
}

// process ecualiza buf (estéreo intercalado).
func (q *eqFilter) process(buf []float32) {
	if len(q.bands) == 0 {
		return
	}
	for i := range buf {
		buf[i] *= q.pre
	}
	for b := range q.bands {
		f := &q.bands[b]
		for i := 0; i+1 < len(buf); i += 2 {
			for c := 0; c < 2; c++ {
				x := buf[i+c]
				y := f.b0*x + f.b1*f.x1[c] + f.b2*f.x2[c] - f.a1*f.y1[c] - f.a2*f.y2[c]
				f.x2[c], f.x1[c] = f.x1[c], x
				f.y2[c], f.y1[c] = f.y1[c], y
				buf[i+c] = y
			}
		}
	}
}

// gainBuf multiplica buf por g.
func gainBuf(buf []float32, g float32) {
	if g == 1 {
		return
	}
	for i := range buf {
		buf[i] *= g
	}
}
