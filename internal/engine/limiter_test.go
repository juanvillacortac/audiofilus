package engine

import (
	"math"
	"testing"
)

// sine es un segundo de seno estéreo de 440 Hz con amplitud amp a 48 kHz.
func sine(amp float64) []float32 {
	buf := make([]float32, 2*48000)
	for i := 0; i < len(buf)/2; i++ {
		v := float32(amp * math.Sin(2*math.Pi*440*float64(i)/48000))
		buf[2*i], buf[2*i+1] = v, v
	}
	return buf
}

func peak(buf []float32) float32 {
	var p float32
	for _, v := range buf {
		p = max(p, abs32(v))
	}
	return p
}

// TestLimiterKeepsPeaks comprueba que con refuerzo una señal a tope nunca pasa
// del techo (no se recorta en seco).
func TestLimiterKeepsPeaks(t *testing.T) {
	l := newLimiter(48000)
	buf := sine(1)
	l.process(buf, 2, false)
	if p := peak(buf); p > limitCeiling+1e-6 {
		t.Errorf("peak %v over the ceiling %v", p, limitCeiling)
	}
}

// TestLimiterQuietIsLinear comprueba que una señal baja solo se amplifica: el
// limitador no la toca si no llega al techo.
func TestLimiterQuietIsLinear(t *testing.T) {
	l := newLimiter(48000)
	buf := sine(0.3)
	l.process(buf, 2, false)
	if p := peak(buf); math.Abs(float64(p)-0.6) > 0.001 {
		t.Errorf("peak %v, want 0.6 (quiet signal x2)", p)
	}
}

// TestLimiterNoBoost comprueba que sin refuerzo la señal pasa intacta.
func TestLimiterNoBoost(t *testing.T) {
	l := newLimiter(48000)
	buf := sine(1)
	want := sine(1)
	l.process(buf, 1, false)
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d changed without boost", i)
		}
	}
}
