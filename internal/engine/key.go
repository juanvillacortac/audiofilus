package engine

import (
	"math"
	"sync"
)

// La tonalidad de lo que suena, para que los sonidos de los juegos (las
// campanitas de Bloques) toquen en ella. Con cada 8192 cuadros (~0,17 s) se
// hace una FFT larga (casillas de ~6 Hz: separa los semitonos desde unos
// 100 Hz), se suma cuánto suena cada nota (el cromagrama, de 100 a 2500 Hz)
// y se compara con los perfiles de Krumhansl y Kessler de las 24 tonalidades
// (lo que pesa cada grado en una tonalidad mayor o menor). Se acumula con
// memoria de ~30 s y empieza de cero en cada canción. Como el resto del
// medidor, solo se calcula con la interfaz abierta y despierta.

const (
	keyWindow = 8192
	keyLowHz  = 100
	keyHighHz = 2500
	keyMemory = 30.0 // segundos de memoria del cromagrama
	keyMinFFT = 15   // FFT hechas antes de decir una tonalidad (~2,5 s)
)

// Los perfiles de Krumhansl y Kessler, desde la tónica.
var (
	keyMajor = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	keyMinor = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// KeyNames son los nombres de las notas, desde do.
var KeyNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// keyBin es una casilla de la FFT que cuenta, con su nota (0 do ... 11 si).
type keyBin struct{ k, pc int }

// keyFinder acumula el cromagrama; lo usa el medidor con su lock tomado.
type keyFinder struct {
	ring   []float64 // los últimos keyWindow cuadros, en mono
	lin    []float64 // los mismos, en orden, para la FFT
	pos, n int       // dónde va el siguiente y cuántos desde la última FFT
	fft    fft
	bins   []keyBin
	decay  float64
	chroma [12]float64
	count  int // FFT acumuladas desde el reinicio
}

// keyTables son las tablas de cada frecuencia de muestreo (la FFT y sus
// casillas), hechas una vez: la ventana y los giros de 8192 pesan.
var keyTables = struct {
	sync.Mutex
	m map[int]keyBinsFFT
}{m: map[int]keyBinsFFT{}}

type keyBinsFFT struct {
	f    fft
	bins []keyBin
}

func newKeyFinder(rate int) keyFinder {
	keyTables.Lock()
	t, ok := keyTables.m[rate]
	if !ok {
		t.f = newFFT(keyWindow)
		for k := 1; k < keyWindow/2; k++ {
			hz := float64(k) * float64(rate) / keyWindow
			if hz < keyLowHz || hz > keyHighHz {
				continue
			}
			pc := int(math.Round(12*math.Log2(hz/261.6256))) % 12
			t.bins = append(t.bins, keyBin{k, (pc + 12) % 12})
		}
		keyTables.m[rate] = t
	}
	keyTables.Unlock()
	return keyFinder{
		ring: make([]float64, keyWindow), lin: make([]float64, keyWindow),
		fft: t.f.clone(), bins: t.bins,
		decay: math.Exp(-float64(keyWindow) / float64(rate) / keyMemory),
	}
}

// add suma un cuadro (mono) y, cada keyWindow, mide.
func (k *keyFinder) add(x float64) {
	k.ring[k.pos] = x
	k.pos = (k.pos + 1) % keyWindow
	k.n++
	if k.n < keyWindow {
		return
	}
	k.n = 0
	copy(k.lin, k.ring[k.pos:])
	copy(k.lin[keyWindow-k.pos:], k.ring[:k.pos])
	mags := k.fft.magnitudes(k.lin)
	var frame [12]float64
	total := 0.0
	for _, b := range k.bins {
		frame[b.pc] += mags[b.k]
		total += mags[b.k]
	}
	if total < 1e-3 { // silencio: no cuenta
		return
	}
	for i := range k.chroma {
		k.chroma[i] = k.chroma[i]*k.decay + frame[i]/total
	}
	k.count++
}

// reset empieza de cero (otra canción).
func (k *keyFinder) reset() {
	k.chroma, k.count, k.n = [12]float64{}, 0, 0
	clear(k.ring)
}

// key es la tonalidad más probable ("C", "F#m"), o "" si aún no se sabe.
func (k *keyFinder) key() string {
	if k.count < keyMinFFT {
		return ""
	}
	best, name := math.Inf(-1), ""
	for t := 0; t < 12; t++ {
		for minor, prof := range [2][12]float64{keyMajor, keyMinor} {
			var rot [12]float64
			for i := range rot {
				rot[i] = prof[(i-t+12)%12]
			}
			if c := correlation(k.chroma[:], rot[:]); c > best {
				best, name = c, KeyNames[t]
				if minor == 1 {
					name += "m"
				}
			}
		}
	}
	return name
}

// correlation es la correlación de Pearson de dos series iguales de largo.
func correlation(a, b []float64) float64 {
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(len(a))
	mb /= float64(len(b))
	var num, da, db float64
	for i := range a {
		num += (a[i] - ma) * (b[i] - mb)
		da += (a[i] - ma) * (a[i] - ma)
		db += (b[i] - mb) * (b[i] - mb)
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}
