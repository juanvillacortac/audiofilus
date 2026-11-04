package decode

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Los tests usan la libavshim que compila tools/ffmpeg/build.sh en build/
// (si está), además de la que haya junto al binario.
func init() {
	arch := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	lib, err := filepath.Abs(filepath.Join("..", "..", "build", "ffmpeg", "libs."+arch, "libavshim.so"))
	if _, serr := os.Stat(lib); err == nil && serr == nil {
		avLibNames = append([]string{lib}, avLibNames...)
	}
}

// TestWMA comprueba un WMA (con libavshim): la frecuencia, la duración, el
// nivel, y que tras un salto salga justo el audio de esa posición, al cuadro
// (no el del principio del paquete al que salta FFmpeg, ni uno corrido por
// el retraso del decodificador), también en el último bloque.
func TestWMA(t *testing.T) {
	needLib(t, loadAV)
	s, err := Open("testdata/tone.wma")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SampleRate() != 44100 {
		t.Errorf("SampleRate = %d, want 44100", s.SampleRate())
	}
	if got := float64(s.Length()) / 44100; math.Abs(got-2) > 0.1 {
		t.Errorf("Length %.3f s, want ~2", got)
	}
	all := readAll(t, s)
	if got := float64(len(all)/2) / 44100; math.Abs(got-2) > 0.1 {
		t.Errorf("decoded %.3f s, want ~2", got)
	}
	if p := peak(all); p < 0.05 || p > 1.01 {
		t.Errorf("peak %.3f out of range", p)
	}
	for _, at := range []int{5, 12345, 30000, 44100 + 777, 60001, len(all)/2 - 1064} {
		if err := s.SeekFrame(int64(at)); err != nil {
			t.Fatal(err)
		}
		if s.Position() != int64(at) {
			t.Errorf("Position after seek = %d, want %d", s.Position(), at)
		}
		rest := readAll(t, s)
		if len(rest) != len(all)-at*2 {
			t.Errorf("after seeking to %d, %d frames remain, want %d", at, len(rest)/2, len(all)/2-at)
		}
		var diff float64
		for i := 0; i < len(rest) && at*2+i < len(all); i++ {
			diff = math.Max(diff, math.Abs(float64(rest[i]-all[at*2+i])))
		}
		if diff > 1e-4 {
			t.Errorf("after seeking to %d the audio differs by %.4f from reading straight", at, diff)
		}
	}
}

// TestWMAWithoutLibrary comprueba que, sin libavshim, un WMA falle con
// ErrUnsupported (y se salte) en vez de romper.
func TestWMAWithoutLibrary(t *testing.T) {
	t.Setenv("AUDIOFILUS_DECODER", "go")
	_, err := Open("testdata/tone.wma")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
