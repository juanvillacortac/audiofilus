package engine

import (
	"math"
	"testing"
	"time"
)

// TestFFTPeak comprueba que una senoidal dé su pico en su casilla y con su
// amplitud.
func TestFFTPeak(t *testing.T) {
	f := newFFT(1024)
	x := make([]float64, 1024)
	for i := range x {
		x[i] = 0.5 * math.Sin(2*math.Pi*64*float64(i)/1024) // casilla 64
	}
	m := f.magnitudes(x)
	best := 0
	for k := range m {
		if m[k] > m[best] {
			best = k
		}
	}
	if best != 64 || math.Abs(m[64]-0.5) > 0.05 {
		t.Errorf("peak at bin %d with %.3f, want bin 64 with ~0.5", best, m[best])
	}
}

// TestMeterBands comprueba que un tono grave y uno agudo enciendan bandas
// distintas, y que el nivel y los graves sigan al grave.
func TestMeterBands(t *testing.T) {
	for _, c := range []struct {
		hz  float64
		low bool
	}{{100, true}, {5000, false}} {
		m := newMeter(48000, 0)
		m.spectrum = true
		buf := make([]float32, 2*meterWindow)
		for i := 0; i < meterWindow; i++ {
			v := float32(0.5 * math.Sin(2*math.Pi*c.hz*float64(i)/48000))
			buf[2*i], buf[2*i+1] = v, v
		}
		m.add(buf, time.Now(), 48000)
		p := m.points[0]
		if len(p.Bands) != MeterBands {
			t.Fatalf("%v Hz: %d bands, want %d", c.hz, len(p.Bands), MeterBands)
		}
		loud := 0
		for b := range p.Bands {
			if p.Bands[b] > p.Bands[loud] {
				loud = b
			}
		}
		if c.low != (loud < MeterBands/2) {
			t.Errorf("%v Hz: loudest band %d (%v)", c.hz, loud, p.Bands)
		}
		if p.Level < 100 {
			t.Errorf("%v Hz: level %d, want a loud level", c.hz, p.Level)
		}
		if c.low && p.Bass < 100 || !c.low && p.Bass > 40 {
			t.Errorf("%v Hz: bass %d", c.hz, p.Bass)
		}
	}
}
