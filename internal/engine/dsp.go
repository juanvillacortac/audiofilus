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
		// Los coeficientes y el estado en variables locales: el compilador
		// los deja en registros (en el struct, cada muestra los lee y
		// escribe en memoria; así va casi el doble de rápido: en la consola, las
		// diez bandas cuestan un 5 % de un núcleo a 480 MHz).
		b0, b1, b2, a1, a2 := f.b0, f.b1, f.b2, f.a1, f.a2
		lx1, lx2, ly1, ly2 := f.x1[0], f.x2[0], f.y1[0], f.y2[0]
		rx1, rx2, ry1, ry2 := f.x1[1], f.x2[1], f.y1[1], f.y2[1]
		for i := 0; i+1 < len(buf); i += 2 {
			l, r := buf[i], buf[i+1]
			ly := b0*l + b1*lx1 + b2*lx2 - a1*ly1 - a2*ly2
			ry := b0*r + b1*rx1 + b2*rx2 - a1*ry1 - a2*ry2
			lx2, lx1, ly2, ly1 = lx1, l, ly1, ly
			rx2, rx1, ry2, ry1 = rx1, r, ry1, ry
			buf[i], buf[i+1] = ly, ry
		}
		f.x1[0], f.x2[0], f.y1[0], f.y2[0] = lx1, lx2, ly1, ly2
		f.x1[1], f.x2[1], f.y1[1], f.y2[1] = rx1, rx2, ry1, ry2
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
