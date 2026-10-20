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
