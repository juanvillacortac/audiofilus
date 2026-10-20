package engine

import "math"

// El refuerzo de volumen (por encima del 100 %) multiplica la señal, y en las
// canciones grabadas muy altas los picos pasarían del máximo y se recortarían
// en seco (distorsión). El limitador baja la ganancia justo en esos picos y la
// recupera despacio: suena más fuerte sin romperse. Solo actúa con refuerzo;
// a 100 % o menos la señal pasa sin tocar.

const (
	// limitCeiling es el pico máximo que deja pasar (-0,2 dB).
	limitCeiling = 0.977
	// limitRelease es lo que tarda en recuperar la ganancia tras un pico.
	limitRelease = 0.08 // segundos
)

// limiter es un limitador de picos sin anticipación: ataque instantáneo (la
// ganancia baja en la misma muestra que el pico, así que nunca se pasa) y
// relajación exponencial.
type limiter struct {
	gain    float32 // la reducción actual (1 = nada)
	release float32 // cuánto se acerca a 1 por muestra
}

// newLimiter prepara un limitador para la frecuencia rate.
func newLimiter(rate int) limiter {
	return limiter{gain: 1, release: float32(1 - math.Exp(-1/(limitRelease*float64(rate))))}
}

// process multiplica buf (estéreo intercalado) por boost y limita los picos.
// Sin refuerzo no hace nada, salvo con always: el ecualizador o ReplayGain
// pueden subir la señal por encima del máximo.
func (l *limiter) process(buf []float32, boost float32, always bool) {
	if boost <= 1 && !always {
		l.gain = 1
		return
	}
	boost = max(boost, 1)
	for i := 0; i+1 < len(buf); i += 2 {
		left, right := buf[i]*boost, buf[i+1]*boost
		peak := max(abs32(left), abs32(right))
		want := float32(1)
		if peak > limitCeiling {
			want = limitCeiling / peak
		}
		if want < l.gain {
			l.gain = want // ataque instantáneo
		} else {
			l.gain += (want - l.gain) * l.release
		}
		buf[i], buf[i+1] = left*l.gain, right*l.gain
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
