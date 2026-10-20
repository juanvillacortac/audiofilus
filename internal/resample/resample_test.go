package resample

import (
	"io"
	"math"
	"testing"
)

// sine es un decode.Source en memoria: un seno de 440 Hz.
type sine struct {
	rate, frames int
	pos          int
}

func (s *sine) Read(dst []float32) (int, error) {
	n := 0
	for ; n+1 < len(dst) && s.pos < s.frames; n += 2 {
		v := float32(math.Sin(2 * math.Pi * 440 * float64(s.pos) / float64(s.rate)))
		dst[n], dst[n+1] = v, v
		s.pos++
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
func (s *sine) SampleRate() int         { return s.rate }
func (s *sine) Length() int64           { return int64(s.frames) }
func (s *sine) Position() int64         { return int64(s.pos) }
func (s *sine) SeekFrame(f int64) error { s.pos = int(f); return nil }
func (s *sine) Close() error            { return nil }

// drain lee r hasta el final.
func drain(r *Reader) []float32 {
	var out []float32
	buf := make([]float32, 777*2)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out
		}
	}
}

// TestLengthAfterResampling comprueba que un segundo de audio siga durando un
// segundo a 48 kHz, venga de la frecuencia que venga.
func TestLengthAfterResampling(t *testing.T) {
	for _, in := range []int{44100, 32000, 48000, 96000} {
		r := New(&sine{rate: in, frames: in}, 48000) // 1 s
		got := len(drain(r)) / 2
		if math.Abs(float64(got-48000)) > 2 {
			t.Errorf("%d Hz -> 48000: %d frames, want ~48000", in, got)
		}
	}
}

// TestSignalIsPreserved comprueba que la señal remuestreada siga siendo el
// mismo seno, con un error pequeño.
func TestSignalIsPreserved(t *testing.T) {
	out := drain(New(&sine{rate: 44100, frames: 44100}, 48000))
	// Se compara con el seno ideal de 440 Hz a 48 kHz.
	var maxErr float64
	for i := 0; i < len(out)/2; i++ {
		want := math.Sin(2 * math.Pi * 440 * float64(i) / 48000)
		maxErr = math.Max(maxErr, math.Abs(float64(out[i*2])-want))
	}
	if maxErr > 0.01 {
		t.Fatalf("max error %.4f: interpolation is off", maxErr)
	}
}

// Pending cuenta lo leído que aún no salió: la posición de src menos Pending
// avanza parejo con lo que sale, sin saltos del tamaño del búfer.
func TestPendingSmooth(t *testing.T) {
	src := &sine{rate: 22050, frames: 22050 * 4}
	r := New(src, 48000)
	out := make([]float32, 2*480) // 10 ms por lectura
	prev := 0.0
	for i := 0; i < 200; i++ {
		if _, err := r.Read(out); err != nil {
			t.Fatal(err)
		}
		pos := (float64(src.Position()) - r.Pending()) / 22050
		want := float64(i+1) * 0.01
		if math.Abs(pos-want) > 0.001 || pos < prev {
			t.Fatalf("read %d: position %.4f s, want %.4f s", i, pos, want)
		}
		prev = pos
	}
}

// tone es un decode.Source en memoria: un seno de freq Hz a rate, de
// amplitud 0,5.
type tone struct {
	freq         float64
	rate, frames int
	pos          int
}

func (s *tone) Read(dst []float32) (int, error) {
	n := 0
	for ; n+1 < len(dst) && s.pos < s.frames; n += 2 {
		v := float32(0.5 * math.Sin(2*math.Pi*s.freq*float64(s.pos)/float64(s.rate)))
		dst[n], dst[n+1] = v, v
		s.pos++
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
func (s *tone) SampleRate() int         { return s.rate }
func (s *tone) Length() int64           { return int64(s.frames) }
func (s *tone) Position() int64         { return int64(s.pos) }
func (s *tone) SeekFrame(f int64) error { s.pos = int(f); return nil }
func (s *tone) Close() error            { return nil }

// drainQ remuestrea con Quality de in a out, leyendo de a chunk frames.
func drainQ(src *tone, out, chunk int) []float32 {
	r := NewMode(src, out, Quality)
	var res []float32
	buf := make([]float32, chunk*2)
	for {
		n, err := r.Read(buf)
		res = append(res, buf[:n]...)
		if err != nil {
			return res
		}
	}
}

// rms es la raíz cuadrática media del canal izquierdo de buf, sin los
// bordes (skip frames de cada lado).
func rms(buf []float32, skip int) float64 {
	sum, n := 0.0, 0
	for i := skip; i < len(buf)/2-skip; i++ {
		v := float64(buf[2*i])
		sum += v * v
		n++
	}
	return math.Sqrt(sum / float64(n))
}

// Quality dura lo mismo que el origen, venga de la frecuencia que venga
// (también con razones que no son fracciones pequeñas).
func TestQualityLength(t *testing.T) {
	for _, in := range []int{44100, 32000, 96000, 22050, 47999} {
		got := len(drainQ(&tone{freq: 440, rate: in, frames: in}, 48000, 777)) / 2
		if math.Abs(float64(got-48000)) > 2 {
			t.Errorf("%d Hz -> 48000: %d frames, want ~48000", in, got)
		}
	}
}

// Quality deja pasar igual lo que está por debajo del 45 % de la frecuencia
// más baja (±0,1 dB) y es casi exacto con un seno.
func TestQualityPassband(t *testing.T) {
	for _, c := range []struct{ in, out int }{{44100, 48000}, {96000, 48000}, {48000, 44100}, {44100, 47999}} {
		low := math.Min(float64(c.in), float64(c.out))
		for _, f := range []float64{100, 1000, 0.45 * low} {
			out := drainQ(&tone{freq: f, rate: c.in, frames: c.in}, c.out, 4096)
			got := rms(out, 2000)
			want := 0.5 / math.Sqrt2
			if db := 20 * math.Log10(got/want); math.Abs(db) > 0.1 {
				t.Errorf("%d -> %d, %.0f Hz: %.3f dB", c.in, c.out, f, db)
			}
		}
	}
	// Y el seno de 1 kHz sale casi idéntico al ideal.
	out := drainQ(&tone{freq: 1000, rate: 44100, frames: 44100}, 48000, 4096)
	maxErr := 0.0
	for i := 2000; i < len(out)/2-2000; i++ {
		want := 0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000)
		maxErr = math.Max(maxErr, math.Abs(float64(out[2*i])-want))
	}
	if maxErr > 1e-4 {
		t.Errorf("1 kHz 44.1 -> 48 kHz: max error %.6f", maxErr)
	}
}

// Al bajar de frecuencia, lo que no cabe (por encima del 55 % de la nueva
// frecuencia de Nyquist) se rechaza al menos 90 dB: no vuelve como aliasing.
func TestQualityStopband(t *testing.T) {
	for _, c := range []struct {
		in, out int
		f       float64
	}{{96000, 48000, 30000}, {96000, 44100, 35000}, {88200, 44100, 26000}, {48000, 32000, 20000}} {
		out := drainQ(&tone{freq: c.f, rate: c.in, frames: c.in}, c.out, 4096)
		db := 20 * math.Log10(rms(out, 2000)/(0.5/math.Sqrt2))
		t.Logf("%d -> %d, %.0f Hz: %.1f dB", c.in, c.out, c.f, db)
		if db > -90 {
			t.Errorf("%d -> %d, %.0f Hz: %.1f dB, want below -90", c.in, c.out, c.f, db)
		}
	}
	// El lineal, en cambio, deja pasar bastante (por eso hay modo Quality).
	r := New(&tone{freq: 30000, rate: 96000, frames: 96000}, 48000)
	lin := 20 * math.Log10(rms(drain(r), 2000)/(0.5/math.Sqrt2))
	t.Logf("linear 96 -> 48 kHz, 30 kHz tone: %.1f dB", lin)
}

// Leer de a poco o de a mucho da exactamente lo mismo: no hay saltos en los
// bordes de los búferes.
func TestQualityChunking(t *testing.T) {
	a := drainQ(&tone{freq: 3000, rate: 44100, frames: 20000}, 48000, 1)
	b := drainQ(&tone{freq: 3000, rate: 44100, frames: 20000}, 48000, 9999)
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs: %v vs %v", i, a[i], b[i])
		}
	}
}

// Con Quality también, la posición (src menos Pending) avanza pareja.
func TestQualityPending(t *testing.T) {
	src := &tone{freq: 440, rate: 44100, frames: 44100 * 4}
	r := NewMode(src, 48000, Quality)
	out := make([]float32, 2*480)
	for i := 0; i < 200; i++ {
		if _, err := r.Read(out); err != nil {
			t.Fatal(err)
		}
		pos := (float64(src.Position()) - r.Pending()) / 44100
		if want := float64(i+1) * 0.01; math.Abs(pos-want) > 0.001 {
			t.Fatalf("read %d: position %.4f s, want %.4f s", i, pos, want)
		}
	}
	// Tras Reset (un salto) empieza de cero.
	r.Reset()
	if p := r.Pending(); p != 0 {
		t.Errorf("pending after reset = %v", p)
	}
}

// BenchmarkQuality mide lo que cuesta un segundo de audio 44,1 → 48 kHz con
// Quality (una operación = un segundo de salida). En arm64:
// GOARCH=arm64 go test -c -o rs.test ./internal/resample && ./rs.test -test.bench . -test.run x
func BenchmarkQuality(b *testing.B) {
	for _, c := range []struct {
		name    string
		in, out int
	}{{"44k1-48k", 44100, 48000}, {"96k-48k", 96000, 48000}, {"generic", 44000, 48000}} {
		b.Run(c.name, func(b *testing.B) {
			buf := make([]float32, 2*1024)
			for i := 0; i < b.N; i++ {
				r := NewMode(&tone{freq: 440, rate: c.in, frames: c.in}, c.out, Quality)
				for {
					if _, err := r.Read(buf); err != nil {
						break
					}
				}
			}
		})
	}
}

// BenchmarkLinear es lo mismo con el lineal, para comparar.
func BenchmarkLinear(b *testing.B) {
	buf := make([]float32, 2*1024)
	for i := 0; i < b.N; i++ {
		r := New(&tone{freq: 440, rate: 44100, frames: 44100}, 48000)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
	}
}
