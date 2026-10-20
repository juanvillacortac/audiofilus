package tracker

import "math"

// Las ondas de PICO-8, como ondas de 32 muestras de 4 bits (las primeras de
// cada canción): en el modo Game Boy suenan por el canal de onda, y en el
// Libre son también las formas del synth.
const (
	WaveTriangle = iota
	WaveTiltedSaw
	WaveSaw
	WaveSquare
	WavePulse
	WaveOrgan
	WaveNoise
	WavePhantom
	NumPresetWaves
)

// WaveNames son sus nombres, para la interfaz.
var WaveNames = [NumPresetWaves]string{"triangle", "tilted saw", "saw", "square", "pulse", "organ", "noise", "phantom"}

// PresetWaves son las ocho ondas de PICO-8.
func PresetWaves() [NumPresetWaves]Wave {
	var out [NumPresetWaves]Wave
	for i := 0; i < NumPresetWaves; i++ {
		for k := 0; k < WaveLen; k++ {
			out[i][k] = quantize(shape(i, float64(k)/WaveLen))
		}
	}
	return out
}

// shape es la forma i en la fase x (0..1), de -1 a 1.
func shape(i int, x float64) float64 {
	switch i {
	case WaveTriangle:
		return 1 - 4*math.Abs(x-0.5)
	case WaveTiltedSaw:
		// Sube despacio y baja rápido (el 80 % arriba).
		if x < 0.8 {
			return -1 + 2*x/0.8
		}
		return 1 - 2*(x-0.8)/0.2
	case WaveSaw:
		return 2*x - 1
	case WaveSquare:
		if x < 0.5 {
			return 1
		}
		return -1
	case WavePulse:
		if x < 0.25 {
			return 1
		}
		return -1
	case WaveOrgan:
		// Dos triangulares, una al doble: el "órgano" de PICO-8.
		t := 1 - 4*math.Abs(x-0.5)
		x2 := math.Mod(2*x, 1)
		t2 := 1 - 4*math.Abs(x2-0.5)
		return (t + 0.5*t2) / 1.5
	case WaveNoise:
		// Ruido fijo (la misma onda siempre): suena áspero, como en PICO-8.
		return noiseTable[int(x*WaveLen)%WaveLen]
	case WavePhantom:
		// Dos senos desafinados: el "fantasma".
		return (math.Sin(2*math.Pi*x) + 0.6*math.Sin(2*math.Pi*3*x+1)) / 1.6
	}
	return 0
}

// noiseTable es una secuencia pseudoaleatoria fija para la onda de ruido.
var noiseTable = func() [WaveLen]float64 {
	var t [WaveLen]float64
	seed := uint32(0x9e3779b9)
	for i := range t {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		t[i] = float64(seed%1000)/500 - 1
	}
	return t
}()

// quantize pasa -1..1 a los 16 niveles de la wave RAM.
func quantize(v float64) uint8 {
	return uint8(math.Max(0, math.Min(15, math.Round((v+1)*7.5))))
}
