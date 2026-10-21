package decode

import (
	"bytes"
	"math"
	"os"
	"testing"
)

// El respaldo de go-mp3 para radios decodifica un MP3 que llega a trozos
// pequeños (como de la red), empezando a mitad de un frame, sin esperar
// nunca: devuelve 0 mientras falten bytes y luego el tono.
func TestGoMP3Stream(t *testing.T) {
	b, err := os.ReadFile("testdata/tone.mp3")
	if err != nil {
		t.Fatal(err)
	}
	// Seis veces seguido, como una emisora que no para, y empezando a mitad
	// de la cabecera: se tiene que resincronizar.
	b = bytes.Repeat(b, 6)[7:]
	s := newGoMP3Stream()
	dst := make([]float32, 2048)
	samples, peak := 0, 0.0
	for off := 0; off < len(b); off += 700 {
		s.Feed(b[off:min(off+700, len(b))])
		for { // lo que se pueda, como hace la radio
			n, err := s.Decode(dst)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			for _, v := range dst[:n] {
				peak = math.Max(peak, math.Abs(float64(v)))
			}
			samples += n
		}
	}
	// Al menos 8 de los 12 s (lo último se queda esperando más bytes).
	if s.SampleRate() == 0 || samples < 8*44100*2 || peak < 0.05 { // el tono va a 1/8
		t.Errorf("rate %d, %d samples, peak %.3f; want the tone decoded", s.SampleRate(), samples, peak)
	}
}
