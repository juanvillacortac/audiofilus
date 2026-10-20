package engine

import (
	"io"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/queue"
)

// TestReplayGainModes comprueba los modos: por canción, por disco, auto
// (disco en orden, canción en aleatorio), apagado, sin etiquetas, el
// preamp y que nunca suba más de lo que el pico permite.
func TestReplayGainModes(t *testing.T) {
	g := library.Gain{Track: -6, Album: -3, HasTrack: true, HasAlbum: true}
	db := func(v float32) float64 { return math.Round(20 * math.Log10(float64(v))) }
	cases := []struct {
		d       DSP
		shuffle bool
		want    float64
	}{
		{DSP{ReplayGain: "track"}, false, -6},
		{DSP{ReplayGain: "album"}, true, -3},
		{DSP{ReplayGain: "auto"}, false, -3},
		{DSP{ReplayGain: "auto"}, true, -6},
		{DSP{ReplayGain: "off"}, false, 0},
		{DSP{ReplayGain: "track", Preamp: 4}, false, -2},
	}
	for _, c := range cases {
		if got := db(replayGain(c.d.Normalize(), g, true, c.shuffle)); got != c.want {
			t.Errorf("%+v shuffle=%v: %v dB, want %v", c.d, c.shuffle, got, c.want)
		}
	}
	if got := replayGain(DSP{ReplayGain: "track"}, library.Gain{}, false, false); got != 1 {
		t.Errorf("without tags the gain is %v, want 1", got)
	}
	loud := library.Gain{Track: 10, TrackPeak: 0.5, HasTrack: true}
	if got := replayGain(DSP{ReplayGain: "track"}, loud, true, false); math.Abs(float64(got)-2) > 1e-6 {
		t.Errorf("+10 dB with a 0.5 peak gives %v, want 2 (the peak reaches 1)", got)
	}
}

// stereoSine es un seno estéreo de f Hz, n cuadros a rate.
func stereoSine(f float64, n, rate int) []float32 {
	buf := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		v := float32(0.25 * math.Sin(2*math.Pi*f*float64(i)/float64(rate)))
		buf[2*i], buf[2*i+1] = v, v
	}
	return buf
}

func rms(buf []float32) float64 {
	s := 0.0
	for _, v := range buf {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(buf)))
}

// TestEQ comprueba que sin bandas no toque nada, que +6 dB en 1 kHz suba un
// seno de 1 kHz unos 6 dB (menos la bajada previa para no recortar) y que
// casi no toque uno de 100 Hz.
func TestEQ(t *testing.T) {
	const rate = 48000
	var q eqFilter
	q.set(nil, rate)
	buf := stereoSine(1000, rate/4, rate)
	before := rms(buf)
	q.process(buf)
	if rms(buf) != before {
		t.Error("a flat EQ changed the signal")
	}
	gains := make([]float64, len(EQBands))
	gains[5] = 6 // 1 kHz
	gain := func(f float64) float64 {
		q.set(gains, rate)
		buf := stereoSine(f, rate/2, rate)
		in := rms(buf[rate/2:])
		q.process(buf)
		return 20 * math.Log10(rms(buf[rate/2:])/in)
	}
	pre := -6 * 0.8
	if g := gain(1000); math.Abs(g-(6+pre)) > 0.5 {
		t.Errorf("1 kHz with +6 dB: %.2f dB, want about %.1f", g, 6+pre)
	}
	if g := gain(100); math.Abs(g-pre) > 0.6 {
		t.Errorf("100 Hz with +6 dB at 1 kHz: %.2f dB, want about %.1f (only the headroom)", g, pre)
	}
}

// testEngine es un engine sin salida de audio, para leer de él a mano.
func testEngine(rate int, d DSP) *Engine {
	e := &Engine{rate: rate, q: queue.New(), events: make(chan func(), 64), boost: 1, vol: 1, gain: 1,
		limit: newLimiter(rate), meter: newMeter(rate, 100*time.Millisecond)}
	e.dsp = d.Normalize()
	e.eq.set(e.dsp.EQ, rate)
	return e
}

// readAll lee del engine hasta que se acaba la cola y devuelve los cuadros.
func readAll(t *testing.T, e *Engine) int {
	t.Helper()
	p := make([]byte, 4096*bytesPerFrame)
	frames := 0
	for i := 0; i < 10000; i++ {
		n, err := e.Read(p)
		frames += n / bytesPerFrame
		if err == io.EOF {
			return frames
		}
	}
	t.Fatal("the engine never ended")
	return 0
}

// TestCrossfade comprueba que, con fundido de 0,5 s, dos canciones de 2 s
// duren 3,5 s en total (se solapan medio segundo), y sin fundido 4.
func TestCrossfade(t *testing.T) {
	tone, _ := filepath.Abs("../decode/testdata/tone.wav")
	for _, c := range []struct {
		fade float64
		want float64
	}{{0.5, 3.5}, {0, 4}} {
		e := testEngine(44100, DSP{Crossfade: c.fade})
		e.mu.Lock()
		e.q.Set([]string{tone, tone}, 0)
		e.openLocked()
		e.playing = true
		e.mu.Unlock()
		frames := readAll(t, e)
		if got := float64(frames) / 44100; math.Abs(got-c.want) > 0.05 {
			t.Errorf("crossfade %v s: %.2f s of audio, want %v", c.fade, got, c.want)
		}
	}
}

// BenchmarkEQ mide el ecualizador con las diez bandas activas (un segundo
// de audio a 48 kHz por iteración).
func BenchmarkEQ(b *testing.B) {
	var q eqFilter
	q.set(PresetGains("rock"), 48000)
	buf := stereoSine(1000, 48000, 48000)
	b.ResetTimer()
	for range b.N {
		q.process(buf)
	}
}

// response es la ganancia (dB) de un filtro a la frecuencia f.
func response(b biquad, f float64, rate int) float64 {
	w := 2 * math.Pi * f / float64(rate)
	z1 := complex(math.Cos(-w), math.Sin(-w))
	z2 := z1 * z1
	num := complex(float64(b.b0), 0) + complex(float64(b.b1), 0)*z1 + complex(float64(b.b2), 0)*z2
	den := 1 + complex(float64(b.a1), 0)*z1 + complex(float64(b.a2), 0)*z2
	h := num / den
	return 20 * math.Log10(math.Hypot(real(h), imag(h)))
}

// Cada forma del paramétrico hace lo que dice, a las frecuencias de prueba.
func TestPEQShapes(t *testing.T) {
	const rate = 48000
	for _, c := range []struct {
		band PEQBand
		f    float64
		want float64 // dB
		tol  float64
		less bool // want es un máximo (el corte de los filtros de paso)
	}{
		{PEQBand{"peak", 1000, 6, 1}, 1000, 6, 0.05, false},
		{PEQBand{"peak", 1000, 6, 1}, 100, 0, 0.3, false},
		{PEQBand{"peak", 1000, -9, 4}, 1000, -9, 0.05, false},
		{PEQBand{"peak", 1000, -9, 4}, 2000, 0, 0.5, false},
		{PEQBand{"lowshelf", 100, 6, 0.707}, 20, 6, 0.3, false},
		{PEQBand{"lowshelf", 100, 6, 0.707}, 2000, 0, 0.1, false},
		{PEQBand{"highshelf", 8000, -6, 0.707}, 20000, -6, 0.5, false},
		{PEQBand{"highshelf", 8000, -6, 0.707}, 500, 0, 0.1, false},
		{PEQBand{"lowpass", 1000, 0, 0.707}, 1000, -3, 0.1, false},
		{PEQBand{"lowpass", 1000, 0, 0.707}, 10000, -35, 0, true},
		{PEQBand{"highpass", 1000, 0, 0.707}, 1000, -3, 0.1, false},
		{PEQBand{"highpass", 1000, 0, 0.707}, 100, -35, 0, true},
	} {
		got := response(rbj(c.band, rate), c.f, rate)
		if c.less && got > c.want || !c.less && math.Abs(got-c.want) > c.tol {
			t.Errorf("%+v at %.0f Hz: %.2f dB, want %.2f", c.band, c.f, got, c.want)
		}
	}
}

// Normalize deja las bandas en sus límites, descarta las de forma
// desconocida y se queda con ocho como mucho.
func TestPEQNormalize(t *testing.T) {
	bands := []PEQBand{{"peak", 5, 40, 50}, {"wobble", 1000, 3, 1}, {"lowpass", 30000, 9, 0}}
	for len(bands) < 12 {
		bands = append(bands, PEQBand{"peak", 1000, 1, 1})
	}
	d := DSP{EQMode: "parametric", PEQ: bands, PEQPreamp: -99}.Normalize()
	if len(d.PEQ) != PEQMax {
		t.Fatalf("%d bands, want %d", len(d.PEQ), PEQMax)
	}
	if b := d.PEQ[0]; b.Freq != 20 || b.Gain != 15 || b.Q != 10 {
		t.Errorf("first band = %+v", b)
	}
	if b := d.PEQ[1]; b.Type != "lowpass" || b.Freq != 20000 || b.Gain != 0 || b.Q != 0.707 {
		t.Errorf("lowpass band = %+v", b)
	}
	if d.PEQPreamp != -24 || d.Resampler != "auto" {
		t.Errorf("preamp %v resampler %q", d.PEQPreamp, d.Resampler)
	}
	if (DSP{EQMode: "weird"}).Normalize().EQMode != "graphic" {
		t.Errorf("unknown mode not graphic")
	}
}

// En modo paramétrico suenan las bandas del paramétrico (no las del
// gráfico), y las que no hacen nada no se calculan.
func TestPEQInEngineFilter(t *testing.T) {
	var q eqFilter
	d := DSP{EQMode: "parametric", EQ: []float64{6, 6, 6, 6, 6, 6, 6, 6, 6, 6},
		PEQ: []PEQBand{{"peak", 1000, 4, 1}, {"peak", 2000, 0, 1}, {"highpass", 30, 0, 0.707}}}.Normalize()
	q.setDSP(d, 48000)
	if len(q.bands) != 2 {
		t.Fatalf("%d bands, want 2 (the 0 dB peak is skipped)", len(q.bands))
	}
	if want := float32(math.Pow(10, -4*0.8/20)); math.Abs(float64(q.pre-want)) > 1e-6 {
		t.Errorf("pre = %v, want %v", q.pre, want)
	}
	// Solo un preamp: también se aplica.
	q.setDSP(DSP{EQMode: "parametric", PEQPreamp: -6}.Normalize(), 48000)
	if len(q.bands) != 1 || math.Abs(float64(q.pre)-0.501) > 0.001 {
		t.Errorf("preamp only: %d bands, pre %v", len(q.bands), q.pre)
	}
}
