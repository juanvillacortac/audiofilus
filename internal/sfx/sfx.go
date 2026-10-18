// Package sfx son los sonidos de la interfaz y de los gestos: bips cortos al
// estilo chiptune, sintetizados aquí (sin archivos de audio ni licencias), en
// estéreo float32 a 48 kHz, listos para oto.
package sfx

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"sync"
)

// Rate es la frecuencia de los sonidos.
const Rate = 48000

// Los sonidos.
const (
	Move      = "move"    // mover el cursor
	Type      = "type"    // una tecla del teclado en pantalla
	Confirm   = "confirm" // aceptar
	Back      = "back"    // volver
	Open      = "open"    // abrir un menú o la barra
	Close     = "close"   // cerrarlo
	ToggleOn  = "on"      // encender una opción
	ToggleOff = "off"     // apagarla
	Play      = "play"    // seguir
	Pause     = "pause"   // pausar
	Next      = "next"    // canción siguiente / adelantar
	Prev      = "prev"    // canción anterior / retroceder
	ShuffleOn = "shuffle" // aleatorio encendido
	Ordered   = "ordered" // aleatorio apagado
	Random    = "random"  // una canción al azar
	Error     = "error"   // algo no se pudo
	Screen    = "screen"  // cambiar de pantalla
	Splash    = "splash"  // el del splash, a tiempo con la animación del logo
)

// Gain es el volumen de cada nivel de Sonidos ("off", "low", "", "high").
func Gain(level string) float64 {
	switch level {
	case "off":
		return 0
	case "low":
		return 0.12
	case "high":
		return 0.45
	}
	return 0.25
}

// note es un trozo: una onda de f0 a f1 Hz durante d segundos.
type note struct {
	f0, f1 float64
	d      float64
	wave   int // 0 cuadrada al 50 %, 1 cuadrada al 25 %, 2 triangular, 3 ruido
	amp    float64
}

// recipes son los sonidos, nota a nota.
var recipes = map[string][]note{
	Move:      {{1800, 1800, 0.014, 1, 0.35}},
	Type:      {{0, 0, 0.010, 3, 0.35}},
	Confirm:   {{988, 988, 0.030, 0, 0.6}, {1319, 1319, 0.060, 0, 0.6}},
	Back:      {{784, 784, 0.030, 0, 0.6}, {523, 523, 0.050, 0, 0.6}},
	Open:      {{523, 523, 0.025, 2, 1}, {659, 659, 0.025, 2, 1}, {784, 784, 0.025, 2, 1}, {1047, 1047, 0.045, 2, 1}},
	Close:     {{1047, 1047, 0.025, 2, 1}, {784, 784, 0.025, 2, 1}, {523, 523, 0.045, 2, 1}},
	ToggleOn:  {{600, 1200, 0.060, 1, 0.6}},
	ToggleOff: {{1200, 600, 0.060, 1, 0.6}},
	Play:      {{523, 523, 0.045, 2, 1}, {784, 784, 0.080, 2, 1}},
	Pause:     {{784, 784, 0.045, 2, 1}, {523, 523, 0.080, 2, 1}},
	Next:      {{400, 1600, 0.070, 1, 0.55}, {1600, 1600, 0.020, 1, 0.4}},
	Prev:      {{1600, 400, 0.070, 1, 0.55}, {400, 400, 0.020, 1, 0.4}},
	ShuffleOn: {{880, 880, 0.030, 0, 0.5}, {1175, 1175, 0.030, 0, 0.5}, {988, 988, 0.030, 0, 0.5}, {1319, 1319, 0.060, 0, 0.5}},
	Ordered:   {{1319, 1319, 0.030, 0, 0.5}, {988, 988, 0.030, 0, 0.5}, {880, 880, 0.060, 0, 0.5}},
	Random: {{0, 0, 0.012, 3, 0.4}, {0, 0, 0.020, 3, 0}, {0, 0, 0.012, 3, 0.4}, {0, 0, 0.020, 3, 0},
		{0, 0, 0.012, 3, 0.4}, {0, 0, 0.030, 3, 0}, {1047, 1047, 0.040, 0, 0.5}, {1568, 1568, 0.080, 0, 0.5}},
	Error:  {{110, 110, 0.120, 0, 0.6}},
	Screen: {{300, 900, 0.050, 2, 0.8}},
}

var (
	mu    sync.Mutex
	cache = map[string][]float32{}
)

// Samples es el sonido name (estéreo intercalado, sin volumen aplicado); nil
// si no existe.
func Samples(name string) []float32 {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := cache[name]; ok {
		return s
	}
	if name == Splash {
		cache[name] = jingle()
		return cache[name]
	}
	r, ok := recipes[name]
	if !ok {
		return nil
	}
	rng := rand.New(rand.NewPCG(1, 2))
	var out []float32
	phase := 0.0
	for _, n := range r {
		frames := int(n.d * Rate)
		for i := 0; i < frames; i++ {
			t := float64(i) / float64(frames)
			f := n.f0 + (n.f1-n.f0)*t
			phase += f / Rate
			phase -= math.Floor(phase)
			var v float64
			switch n.wave {
			case 0:
				v = sq(phase, 0.5)
			case 1:
				v = sq(phase, 0.25)
			case 2:
				v = 4*math.Abs(phase-0.5) - 1
			case 3:
				v = rng.Float64()*2 - 1
			}
			// Ataque de 2 ms y caída hasta el final del trozo: sin chasquidos.
			env := math.Min(1, float64(i)/(0.002*Rate)) * (1 - t*t)
			x := float32(v * env * n.amp)
			out = append(out, x, x)
		}
	}
	cache[name] = out
	return out
}

func sq(p, duty float64) float64 {
	if p < duty {
		return 1
	}
	return -1
}

// Bytes es el sonido con volumen gain, en bytes float32 little-endian para
// un player de oto.
func Bytes(name string, gain float64) []byte {
	s := Samples(name)
	b := make([]byte, 4*len(s))
	for i, x := range s {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x*float32(gain)))
	}
	return b
}

// jingle es el sonido del splash, sincronizado con el logo (ui/logo.go): un
// barrido que sube al aparecer el CD, una nota por letra al caer cada una
// (la escala pentatónica hacia arriba, cuando toca el suelo en su primer
// rebote), un brillo agudo cuando lo cruza y un acorde de do mayor al final.
func jingle() []float32 {
	const dur = 2.6
	out := make([]float32, int(dur*Rate)*2)
	add := func(at float64, n note) {
		frames := int(n.d * Rate)
		phase := 0.0
		start := int(at * Rate)
		for i := 0; i < frames && start+i < len(out)/2; i++ {
			t := float64(i) / float64(frames)
			f := n.f0 + (n.f1-n.f0)*t
			phase += f / Rate
			phase -= math.Floor(phase)
			var v float64
			switch n.wave {
			case 0:
				v = sq(phase, 0.5)
			case 1:
				v = sq(phase, 0.25)
			case 2:
				v = 4*math.Abs(phase-0.5) - 1
			}
			env := math.Min(1, float64(i)/(0.003*Rate)) * math.Pow(1-t, 2)
			x := float32(v * env * n.amp)
			out[2*(start+i)] += x
			out[2*(start+i)+1] += x
		}
	}
	add(0, note{180, 900, 0.30, 2, 0.5}) // el CD
	// Do, re, mi, sol, la... (la escala pentatónica), una por letra.
	scale := []float64{523, 587, 659, 784, 880, 1047, 1175, 1319, 1568, 1760}
	for i, f := range scale {
		land := 0.08 + float64(i)*0.07 + 0.5/2.75
		add(land, note{f, f, 0.09, 1, 0.32})
	}
	for k, f := range []float64{2093, 2637, 3136, 4186} { // el brillo
		add(1.25+float64(k)*0.06, note{f, f, 0.12, 2, 0.18})
	}
	for _, f := range []float64{523, 659, 784, 1047} { // el acorde
		add(1.62, note{f, f, 0.9, 2, 0.22})
	}
	for i := range out {
		out[i] = float32(math.Max(-1, math.Min(1, float64(out[i]))))
	}
	return out
}
