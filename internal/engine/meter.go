package engine

import (
	"math"
	"sync"
	"time"
)

// El medidor: cuánto suena (nivel), cuántos graves hay y, si se pide, el
// espectro en 16 bandas, en ventanas de 1024 frames (~21 ms a 48 kHz), con la
// hora a la que esa ventana llega a los oídos. Read mide lo que entrega a oto, que no suena hasta que pasa el búfer,
// así que cada punto lleva la hora de ahora más el búfer: la interfaz, que lo
// pide cuatro veces por segundo, lo dibuja a su hora y queda a tiempo con lo
// que se oye. El retraso son dos búferes: el del player de oto (que llena Read)
// y el de su contexto (que va a PulseAudio). Lo usan el visualizador y el tetris oculto.

const (
	meterWindow = 1024 // frames por punto
	meterPoints = 64   // los últimos ~1,4 s
	// meterBassHz es el corte del filtro de graves (un polo): el bombo y el bajo.
	meterBassHz = 150
	// MeterBands son las bandas del espectro: logarítmicas, de 40 Hz a 16 kHz.
	MeterBands = 16
	bandLowHz  = 40
	bandHighHz = 16000
)

// MeterPoint es un punto del medidor: la hora en que se oye (ms Unix) y el
// nivel y los graves, de 0 a 255.
type MeterPoint struct {
	At    int64 `json:"t"`
	Level uint8 `json:"l"`
	Bass  uint8 `json:"b"`
	// Bands es el espectro (0..255 por banda, de graves a agudos). Como todo
	// el medidor, solo se calcula si está activado (SetSpectrum).
	Bands []byte `json:"s,omitempty"`
}

// meter acumula las ventanas; lo usa Read con e.mu tomado.
type meter struct {
	latency time.Duration
	alpha   float64 // coeficiente del filtro de graves
	low     float64 // estado del filtro
	sum     float64 // energía acumulada de la ventana
	sumBass float64
	n       int
	points  [meterPoints]MeterPoint
	next    int

	spectrum bool // medir (nivel, graves y espectro): solo con la app abierta y despierta
	mono     [meterWindow]float64
	fft      fft
	edges    [MeterBands + 1]int // la primera casilla de la FFT de cada banda
	rate     int
	key      *keyFinder // la tonalidad (key.go); se crea al medir por primera vez
}

func newMeter(rate int, latency time.Duration) meter {
	m := meter{latency: latency, alpha: 1 - math.Exp(-2*math.Pi*meterBassHz/float64(rate)), rate: rate}
	m.fft = newFFT(meterWindow)
	for b := 0; b <= MeterBands; b++ {
		hz := bandLowHz * math.Pow(float64(bandHighHz)/bandLowHz, float64(b)/MeterBands)
		m.edges[b] = max(1, min(meterWindow/2, int(hz*meterWindow/float64(rate)+0.5)))
	}
	for b := 1; b <= MeterBands; b++ { // cada banda, al menos una casilla
		m.edges[b] = max(m.edges[b], m.edges[b-1]+1)
	}
	return m
}

// SetSpectrum activa o desactiva el medidor entero (nivel, graves y
// espectro). Solo lo mira la interfaz abierta y despierta; en sleep y en
// segundo plano medir era un 16 % de la CPU del servicio. Con él va la
// historia del scratch (scratch.go), que solo pide la interfaz: al volver,
// empieza vacía, porque lo de antes ya no es lo que se oía.
func (e *Engine) SetSpectrum(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if on && !e.meter.spectrum {
		e.histFrom = e.histN
	}
	if !on {
		e.scr = nil
	}
	e.meter.spectrum = on
}

// add mide buf (estéreo intercalado), que empezará a oírse en start.
func (m *meter) add(buf []float32, start time.Time, rate int) {
	if !m.spectrum {
		return // nadie mira el medidor (app cerrada o en sleep): ni se calcula
	}
	if m.key == nil {
		k := newKeyFinder(m.rate)
		m.key = &k
	}
	for i := 0; i+1 < len(buf); i += 2 {
		x := float64(buf[i]+buf[i+1]) / 2
		m.key.add(x)
		m.low += m.alpha * (x - m.low)
		m.sum += x * x
		m.sumBass += m.low * m.low
		m.mono[m.n] = x
		m.n++
		if m.n == meterWindow {
			at := start.Add(time.Duration(i/2) * time.Second / time.Duration(rate))
			p := MeterPoint{
				At:    at.UnixMilli(),
				Level: meterByte(math.Sqrt(m.sum/meterWindow), 2.5),
				Bass:  meterByte(math.Sqrt(m.sumBass/meterWindow), 4),
			}
			p.Bands = m.bands()
			m.points[m.next] = p
			m.next = (m.next + 1) % meterPoints
			m.sum, m.sumBass, m.n = 0, 0, 0
		}
	}
}

// meterByte pasa un valor eficaz a 0..255 con una ganancia (la música rara vez
// pasa de 0,3 de valor eficaz).
func meterByte(rms, gain float64) uint8 {
	return uint8(math.Min(255, rms*gain*255))
}

// Meter devuelve los puntos del medidor, del más viejo al más nuevo.
func (e *Engine) Meter() []MeterPoint {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.meter.list()
}

// Key es la tonalidad de lo que suena ("C", "F#m"; key.go), o "" si aún no
// se sabe.
func (e *Engine) Key() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.meter.keyName()
}

// keyName es la tonalidad medida, o "" si no se midió aún.
func (m *meter) keyName() string {
	if m.key == nil {
		return ""
	}
	return m.key.key()
}

// list son los puntos guardados, del más viejo al más nuevo.
func (m *meter) list() []MeterPoint {
	out := make([]MeterPoint, 0, meterPoints)
	for i := 0; i < meterPoints; i++ {
		p := m.points[(m.next+i)%meterPoints]
		if p.At != 0 {
			out = append(out, p)
		}
	}
	return out
}

// Analyzer es el mismo medidor (nivel, graves y espectro) para otro audio
// que el del engine: el tracker de la interfaz, que suena por su propia
// salida. Con sus puntos reaccionan a él los visualizadores y los juegos.
type Analyzer struct {
	mu   sync.Mutex
	m    meter
	rate int
}

// NewAnalyzer mide audio a rate, que se oye latency después de medirlo (lo
// que tarda en salir por el búfer).
func NewAnalyzer(rate int, latency time.Duration) *Analyzer {
	a := &Analyzer{m: newMeter(rate, latency), rate: rate}
	a.m.spectrum = true
	return a
}

// Add mide buf (estéreo intercalado), que se acaba de mandar a la salida.
func (a *Analyzer) Add(buf []float32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.m.add(buf, time.Now().Add(a.m.latency), a.rate)
}

// Points son los puntos medidos, del más viejo al más nuevo.
func (a *Analyzer) Points() []MeterPoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m.list()
}

// Key es la tonalidad de lo medido, como Engine.Key.
func (a *Analyzer) Key() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.m.keyName()
}

// bands calcula el espectro de la ventana: FFT con ventana de Hann y, por
// banda, la potencia media en dB, de -66 dB (0) a -6 dB (255).
func (m *meter) bands() []byte {
	mags := m.fft.magnitudes(m.mono[:])
	out := make([]byte, MeterBands)
	for b := 0; b < MeterBands; b++ {
		var p float64
		for k := m.edges[b]; k < m.edges[b+1]; k++ {
			p += mags[k] * mags[k]
		}
		p /= float64(m.edges[b+1] - m.edges[b])
		db := 10 * math.Log10(p+1e-12)
		out[b] = uint8(math.Max(0, math.Min(255, (db+66)/60*255)))
	}
	return out
}
