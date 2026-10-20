package loudness

import "math"

// truePeak estima el pico verdadero de un canal (BS.1770-4, anexo 2):
// sobremuestrea a 192 kHz o más (×4 bajo 96 kHz, ×2 bajo 192 kHz) con un
// interpolador polifásico de seno cardinal con ventana, y mira el mayor
// valor absoluto, que incluye los picos entre muestras. No es el filtro
// exacto de la norma (48 coeficientes fijos para 48 kHz) sino uno
// equivalente calculado para la frecuencia de la fuente.
//
// Para que no cueste diez veces más que el pico de las muestras, solo se
// interpola cerca de los picos (ver push).
type truePeak struct {
	factor int
	phases [][truePeakTaps]float64 // por fase, sus coeficientes (de la muestra más nueva a la más vieja)
	// hist guarda cada muestra dos veces (en pos y en pos+taps), así la
	// ventana de las últimas taps muestras es siempre contigua.
	hist [2 * truePeakTaps]float64
	pos  int
	peak float64 // el pico que ya se tiene (de este canal)
}

// truePeakTaps son los coeficientes de cada fase.
const truePeakTaps = 12

func newTruePeak(rate int) truePeak {
	factor := 1
	switch {
	case rate < 96000:
		factor = 4
	case rate < 192000:
		factor = 2
	}
	t := truePeak{factor: factor}
	if factor == 1 {
		return t
	}
	// El prototipo: un paso bajo con el corte en la frecuencia original,
	// de factor*taps coeficientes, con ventana de Hann.
	n := factor * truePeakTaps
	h := make([]float64, n)
	center := float64(n-1) / 2
	for i := range h {
		x := (float64(i) - center) / float64(factor)
		s := 1.0
		if x != 0 {
			s = math.Sin(math.Pi*x) / (math.Pi * x)
		}
		w := 0.5 - 0.5*math.Cos(2*math.Pi*(float64(i)+0.5)/float64(n))
		h[i] = s * w
	}
	t.phases = make([][truePeakTaps]float64, factor)
	for p := range t.phases {
		var ph [truePeakTaps]float64
		sum := 0.0
		for k := range ph {
			ph[k] = h[k*factor+p]
			sum += ph[k]
		}
		// Cada fase con ganancia 1 en continua: una señal fija no sube.
		for k := range ph {
			ph[k] /= sum
		}
		t.phases[p] = ph
	}
	return t
}

// push mete una muestra y devuelve el pico de este canal hasta ahora.
func (t *truePeak) push(x float64) float64 {
	if a := math.Abs(x); a > t.peak {
		t.peak = a
	}
	if t.factor == 1 {
		return t.peak
	}
	t.pos++
	if t.pos == truePeakTaps {
		t.pos = 0
	}
	t.hist[t.pos] = x
	t.hist[t.pos+truePeakTaps] = x
	// La ventana, de la más vieja a la más nueva (la de ahora, la última).
	win := t.hist[t.pos+1 : t.pos+1+truePeakTaps]
	// Los puntos que se interpolan ahora caen entre las dos muestras del
	// centro de la ventana. Entre muestras, una señal de banda limitada no
	// sube más de +3 dB sobre sus vecinas (el peor caso: un seno a fs/4
	// desfasado 45°): si ni con 1,5 veces la mayor de las dos se llega al
	// pico que ya se tiene, no hace falta interpolar. Así el pico verdadero
	// cuesta poco más que el de las muestras salvo en los pasajes más
	// fuertes.
	c := math.Max(math.Abs(win[truePeakTaps/2-1]), math.Abs(win[truePeakTaps/2]))
	if c*1.5 <= t.peak {
		return t.peak
	}
	for p := range t.phases {
		ph := &t.phases[p]
		var y float64
		for k := range truePeakTaps {
			y += ph[k] * win[truePeakTaps-1-k]
		}
		if y < 0 {
			y = -y
		}
		if y > t.peak {
			t.peak = y
		}
	}
	return t.peak
}
