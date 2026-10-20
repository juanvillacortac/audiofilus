package loudness

import (
	"context"
	"errors"
	"io"
	"math"
	"math/cmplx"
	"testing"
)

// tone es un seno estéreo de f Hz y amplitud de pico amp (por canal), de
// secs segundos; con mono los dos canales iguales, si no el derecho con otra
// fase (para que no parezca un mono duplicado).
func tone(rate int, f, amp, secs float64, mono bool) []float32 {
	n := int(secs * float64(rate))
	out := make([]float32, 2*n)
	for i := range n {
		t := float64(i) / float64(rate)
		out[2*i] = float32(amp * math.Sin(2*math.Pi*f*t))
		if mono {
			out[2*i+1] = out[2*i]
		} else {
			out[2*i+1] = float32(amp * math.Sin(2*math.Pi*f*t+1))
		}
	}
	return out
}

// dbfs es la amplitud de pico de -db dBFS.
func dbfs(db float64) float64 { return math.Pow(10, db/20) }

// measure mide samples a rate en trozos irregulares (como llegan del
// decodificador).
func measure(rate int, samples []float32, opts Options) *Analyzer {
	a := NewAnalyzer(rate, opts)
	for i := 0; i < len(samples); {
		n := min(len(samples)-i, 2*(997+i%3))
		a.Add(samples[i : i+n])
		i += n
	}
	return a
}

// Los coeficientes a 48 kHz son los de la norma (BS.1770-4, tablas 1 y 2).
func TestKWeighting48k(t *testing.T) {
	shelf, hp := kWeighting(48000)
	want := []struct {
		name      string
		got, want float64
	}{
		{"shelf b0", shelf.b0, 1.53512485958697}, {"shelf b1", shelf.b1, -2.69169618940638},
		{"shelf b2", shelf.b2, 1.19839281085285}, {"shelf a1", shelf.a1, -1.69065929318241},
		{"shelf a2", shelf.a2, 0.73248077421585},
		{"hp b0", hp.b0, 1}, {"hp b1", hp.b1, -2}, {"hp b2", hp.b2, 1},
		{"hp a1", hp.a1, -1.99004745483398}, {"hp a2", hp.a2, 0.99007225036621},
	}
	for _, w := range want {
		if math.Abs(w.got-w.want) > 1e-8 {
			t.Errorf("%s = %.14f, want %.14f", w.name, w.got, w.want)
		}
	}
}

// gainAt es la ganancia en dB de los dos filtros en f Hz.
func gainAt(shelf, hp biquad, f, rate float64) float64 {
	z := cmplx.Exp(complex(0, -2*math.Pi*f/rate))
	h := func(b biquad) complex128 {
		num := complex(b.b0, 0) + complex(b.b1, 0)*z + complex(b.b2, 0)*z*z
		den := 1 + complex(b.a1, 0)*z + complex(b.a2, 0)*z*z
		return num / den
	}
	return 20 * math.Log10(cmplx.Abs(h(shelf)*h(hp)))
}

// A otras frecuencias los coeficientes se recalculan y la respuesta es la
// misma: +0,69 dB a 1 kHz (por eso el -0,691 de la fórmula; a 22 kHz se
// desvía unas centésimas por la transformación bilineal, como en
// libebur128), unos +4 dB en los agudos y casi nada en los graves profundos.
func TestKWeightingOtherRates(t *testing.T) {
	for _, rate := range []float64{44100, 48000, 96000, 22050} {
		s, h := kWeighting(rate)
		if g := gainAt(s, h, 1000, rate); math.Abs(g-0.691) > 0.05 {
			t.Errorf("%v Hz: gain at 1 kHz %.3f dB, want 0.691", rate, g)
		}
		if g := gainAt(s, h, 8000, rate); g < 3.5 || g > 4.5 {
			t.Errorf("%v Hz: gain at 8 kHz %.3f dB, want about 4", rate, g)
		}
		if g := gainAt(s, h, 10, rate); g > -10 {
			t.Errorf("%v Hz: gain at 10 Hz %.3f dB, want strongly cut", rate, g)
		}
	}
}

// EBU Tech 3341, caso 1: un seno estéreo de 1 kHz a -23 dBFS mide -23 LUFS
// (a cualquier frecuencia de muestreo).
func TestReferenceTone(t *testing.T) {
	for _, rate := range []int{48000, 44100, 11025} {
		r := measure(rate, tone(rate, 1000, dbfs(-23), 20, false), Options{}).Result()
		if math.Abs(r.LUFS-(-23)) > 0.1 {
			t.Errorf("%d Hz: %.2f LUFS, want -23.0", rate, r.LUFS)
		}
		if math.Abs(r.Gain-5) > 0.1 {
			t.Errorf("%d Hz: gain %.2f dB, want +5 (to -18 LUFS)", rate, r.Gain)
		}
		if math.Abs(r.Seconds-20) > 0.01 {
			t.Errorf("%d Hz: %.3f s measured", rate, r.Seconds)
		}
	}
}

// Un mono (los dos canales iguales) se mide como un canal, como manda
// BS.1770: 3 dB menos que el mismo seno en dos canales distintos.
func TestMonoCountsOnce(t *testing.T) {
	r := measure(48000, tone(48000, 1000, dbfs(-23), 10, true), Options{}).Result()
	if math.Abs(r.LUFS-(-26.01)) > 0.1 {
		t.Errorf("mono: %.2f LUFS, want -26.0", r.LUFS)
	}
}

// El silencio no cuenta (puerta absoluta), y los pasajes mucho más bajos
// tampoco (puerta relativa).
func TestGating(t *testing.T) {
	rate := 48000
	loud := tone(rate, 1000, dbfs(-23), 10, false)
	withSilence := append(append([]float32{}, loud...), make([]float32, 2*rate*10)...)
	if r := measure(rate, withSilence, Options{}).Result(); math.Abs(r.LUFS-(-23)) > 0.1 {
		t.Errorf("with silence: %.2f LUFS, want -23", r.LUFS)
	}
	withQuiet := append(append([]float32{}, loud...), tone(rate, 1000, dbfs(-50), 10, false)...)
	if r := measure(rate, withQuiet, Options{}).Result(); math.Abs(r.LUFS-(-23)) > 0.1 {
		t.Errorf("with a -50 dB passage: %.2f LUFS, want -23 (relative gate)", r.LUFS)
	}
	if r := measure(rate, make([]float32, 2*rate*5), Options{}).Result(); !r.Silent || r.Gain != 0 || !math.IsInf(r.LUFS, -1) {
		t.Errorf("silence: %+v", r)
	}
	if r := measure(rate, tone(rate, 1000, 0.5, 0.3, false), Options{}).Result(); !r.Silent {
		t.Errorf("shorter than one 400 ms block should have no loudness: %+v", r)
	}
}

// El álbum se mide como si sonara seguido: entre sus pistas, con el pico
// mayor.
func TestAlbum(t *testing.T) {
	rate := 48000
	a := measure(rate, tone(rate, 1000, dbfs(-20), 10, false), Options{})
	b := measure(rate, tone(rate, 1000, dbfs(-26), 10, false), Options{})
	al := Album(a, b)
	ra, rb := a.Result(), b.Result()
	if !(al.LUFS < ra.LUFS && al.LUFS > rb.LUFS) {
		t.Errorf("album %.2f not between %.2f and %.2f", al.LUFS, ra.LUFS, rb.LUFS)
	}
	// La media de energías de -20 y -26 dB: unos -22,03.
	if math.Abs(al.LUFS-(-22.03)) > 0.15 {
		t.Errorf("album %.2f LUFS, want about -22.0", al.LUFS)
	}
	if al.Peak != math.Max(ra.Peak, rb.Peak) || math.Abs(al.Seconds-20) > 0.01 {
		t.Errorf("album peak %v seconds %v", al.Peak, al.Seconds)
	}
}

// El pico verdadero ve los picos entre muestras: un seno de 12 kHz a 48 kHz
// con la fase a 45° tiene sus muestras en ±0,707 pero llega a 1.
func TestTruePeak(t *testing.T) {
	rate, n := 48000, 48000
	s := make([]float32, 2*n)
	for i := range n {
		v := float32(math.Sin(2*math.Pi*12000*float64(i)/float64(rate) + math.Pi/4))
		s[2*i], s[2*i+1] = v, v
	}
	sp := measure(rate, s, Options{SamplePeak: true}).Result()
	tp := measure(rate, s, Options{}).Result()
	if math.Abs(sp.Peak-0.7071) > 0.001 || sp.TruePeak {
		t.Errorf("sample peak %.4f", sp.Peak)
	}
	if tp.Peak < 0.95 || tp.Peak > 1.08 || !tp.TruePeak {
		t.Errorf("true peak %.4f, want about 1.0", tp.Peak)
	}
	// Un seno grave no sube por sobremuestrear.
	low := measure(rate, tone(rate, 100, 0.5, 2, true), Options{}).Result()
	if math.Abs(low.Peak-0.5) > 0.005 {
		t.Errorf("true peak of a 100 Hz sine at 0.5: %.4f", low.Peak)
	}
}

// src es una decode.Source en memoria.
type src struct {
	rate int
	data []float32
	pos  int
}

func (s *src) Read(dst []float32) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(dst, s.data[s.pos:])
	s.pos += n
	return n, nil
}
func (s *src) SampleRate() int         { return s.rate }
func (s *src) Length() int64           { return int64(len(s.data) / 2) }
func (s *src) Position() int64         { return int64(s.pos / 2) }
func (s *src) SeekFrame(f int64) error { s.pos = int(2 * f); return nil }
func (s *src) Close() error            { return nil }

// Measure lee la fuente entera, avisa del avance y se puede cancelar.
func TestMeasure(t *testing.T) {
	data := tone(44100, 1000, dbfs(-23), 5, false)
	var last, total int64
	a, err := Measure(context.Background(), &src{rate: 44100, data: data}, Options{}, func(d, tt int64) { last, total = d, tt })
	if err != nil {
		t.Fatal(err)
	}
	if r := a.Result(); math.Abs(r.LUFS-(-23)) > 0.1 || last != total || total != 5*44100 {
		t.Errorf("result %+v, progress %d/%d", r, last, total)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Measure(ctx, &src{rate: 44100, data: data}, Options{}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled measure: %v", err)
	}
}

// BenchmarkMinute mide un minuto de audio estéreo a 44,1 kHz (con el pico
// verdadero, lo más caro).
func BenchmarkMinute(b *testing.B) {
	data := tone(44100, 440, 0.5, 60, false)
	b.SetBytes(int64(len(data) * 4))
	for b.Loop() {
		measure(44100, data, Options{}).Result()
	}
}

// BenchmarkMinuteSamplePeak es lo mismo con el pico de las muestras.
func BenchmarkMinuteSamplePeak(b *testing.B) {
	data := tone(44100, 440, 0.5, 60, false)
	for b.Loop() {
		measure(44100, data, Options{SamplePeak: true}).Result()
	}
}
