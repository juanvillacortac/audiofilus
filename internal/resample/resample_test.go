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
