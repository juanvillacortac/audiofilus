package engine

import (
	"io"
	"math"
	"testing"
)

// A 1,5× sale un tercio menos de audio y el tono no cambia (cuenta los cruces
// por cero de un seno de 440 Hz); a 0,75×, un tercio más.
func TestStretchSpeedKeepsPitch(t *testing.T) {
	const rate, secs, freq = 48000, 4, 440.0
	for _, speed := range []float64{1.5, 0.75, 2} {
		src := make([]float32, rate*secs*2)
		for i := 0; i < rate*secs; i++ {
			v := float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/rate))
			src[i*2], src[i*2+1] = v, v
		}
		read := func(dst []float32) (int, error) {
			n := copy(dst, src)
			src = src[n:]
			if len(src) == 0 {
				return n, io.EOF
			}
			return n, nil
		}
		st := newStretcher(speed)
		var out []float32
		buf := make([]float32, 1000)
		for {
			n, err := st.Read(buf, read)
			out = append(out, buf[:n]...)
			if err != nil || n == 0 {
				break
			}
		}
		frames := len(out) / 2
		want := float64(rate*secs) / speed
		if math.Abs(float64(frames)-want)/want > 0.03 {
			t.Errorf("speed %v: %d frames, want about %.0f", speed, frames, want)
		}
		// El tono: cruces por cero en el medio (lejos de los bordes).
		cross := 0
		a, b := frames/4, frames*3/4
		for i := a + 1; i < b; i++ {
			if (out[(i-1)*2] < 0) != (out[i*2] < 0) {
				cross++
			}
		}
		got := float64(cross) / 2 / (float64(b-a) / rate)
		if math.Abs(got-freq) > 10 {
			t.Errorf("speed %v: pitch %.0f Hz, want %v", speed, got, freq)
		}
	}
}

// El crossfeed manda los graves de un lado al otro y casi nada de los agudos;
// apagado no toca nada.
func TestCrossfeed(t *testing.T) {
	energy := func(freq float64, level string) (left, right float64) {
		var c crossfeed
		c.set(level, 48000)
		buf := make([]float32, 48000*2)
		for i := 0; i < 48000; i++ {
			buf[i*2] = float32(math.Sin(2 * math.Pi * freq * float64(i) / 48000)) // solo a la izquierda
		}
		c.process(buf)
		for i := 24000; i < 48000; i++ {
			left += float64(buf[i*2] * buf[i*2])
			right += float64(buf[i*2+1] * buf[i*2+1])
		}
		return
	}
	if _, r := energy(100, ""); r != 0 {
		t.Errorf("off: right channel got %v", r)
	}
	l, r := energy(100, "medium")
	bass := r / l
	l, r = energy(8000, "medium")
	treble := r / l
	if bass < 0.1 || treble > bass/20 {
		t.Errorf("medium: crossfed bass ratio %.3f, treble %.4f; want bass crossfed and treble much less", bass, treble)
	}
}
