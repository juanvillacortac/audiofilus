package decode

import (
	"io"
	"testing"
	"time"
)

// TestCuePath comprueba el formato de las rutas de pistas CUE, sin final y
// con él, y que una ruta normal (aunque tenga "#") no lo sea.
func TestCuePath(t *testing.T) {
	if p := CuePath("/m/x.flac", 1500*time.Millisecond, 0); p != "/m/x.flac#t=1.5" {
		t.Errorf("CuePath = %q", p)
	}
	f, s, e, ok := SplitCuePath("/m/x.flac#t=12.345,256.789")
	if !ok || f != "/m/x.flac" || s != 12345*time.Millisecond || e != 256789*time.Millisecond {
		t.Errorf("SplitCuePath = %q %v %v %v", f, s, e, ok)
	}
	for _, p := range []string{"/m/#1 hit.mp3", "/m/x.flac#t=5,2", "/m/x.flac"} {
		if IsCuePath(p) {
			t.Errorf("%q should not be a CUE path", p)
		}
	}
}

// TestSegment comprueba un trozo de 0,5 a 1,5 s de cada formato: dura 1 s,
// se lee entero hasta un EOF exacto, y saltar dentro cuenta desde su inicio.
func TestSegment(t *testing.T) {
	for _, name := range []string{"tone.wav", "tone.flac", "tone.ogg"} {
		t.Run(name, func(t *testing.T) {
			s, err := OpenSegment(CuePath("testdata/"+name, 500*time.Millisecond, 1500*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			rate := int64(s.SampleRate())
			if s.Length() != rate {
				t.Fatalf("Length = %d, want %d", s.Length(), rate)
			}
			buf := make([]float32, 1000) // no divide al trozo: la última lectura se recorta
			total := 0
			for {
				n, err := s.Read(buf)
				total += n / 2
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if int64(total) != rate || s.Position() != rate {
				t.Errorf("read %d frames, Position %d; want %d", total, s.Position(), rate)
			}
			if err := s.SeekFrame(rate / 4); err != nil {
				t.Fatal(err)
			}
			if s.Position() != rate/4 {
				t.Errorf("Position after seek = %d, want %d", s.Position(), rate/4)
			}
			rest := readAll(t, s)
			if got := int64(len(rest) / 2); got != rate-rate/4 {
				t.Errorf("after seeking, %d frames left, want %d", got, rate-rate/4)
			}
		})
	}
	// Sin final: hasta que acaba el archivo (2 s).
	s, err := OpenSegment(CuePath("testdata/tone.wav", time.Second, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Length() != int64(s.SampleRate()) {
		t.Errorf("open-ended Length = %d, want %d", s.Length(), s.SampleRate())
	}
}
