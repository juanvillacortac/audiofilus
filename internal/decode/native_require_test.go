package decode

import (
	"math"
	"os"
	"testing"
)

// Con AUDIOFILUS_REQUIRE_NATIVE=1 (el CI de Windows con las DLL del paquete
// en el PATH), las bibliotecas nativas tienen que cargar y decodificar: así
// se comprueba que las DLL que va a llevar el zip de Windows sirven.
func TestRequireNative(t *testing.T) {
	if os.Getenv("AUDIOFILUS_REQUIRE_NATIVE") == "" {
		t.Skip("set AUDIOFILUS_REQUIRE_NATIVE=1 to require the native libraries")
	}
	for name, load := range map[string]func() error{
		"mpg123": loadMPG123, "fdk-aac": loadFDK, "opusfile": loadOpusfile, "libxmp": loadXMP, "libgme": loadGME,
	} {
		if err := load(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, f := range []string{"testdata/tone.mp3", "testdata/tone.m4a", "testdata/tone.opus"} {
		src, err := Open(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		buf := make([]float32, 8192)
		peak := 0.0
		for i := 0; i < 20; i++ {
			n, err := src.Read(buf)
			for _, v := range buf[:n] {
				peak = math.Max(peak, math.Abs(float64(v)))
			}
			if err != nil {
				break
			}
		}
		src.Close()
		if peak < 0.05 {
			t.Errorf("%s: peak %.3f, want the tone", f, peak)
		}
	}
	// Las radios: AAC en ADTS por el decodificador de flujos de fdk-aac.
	if d, err := NewStreamDecoder("aac"); err != nil {
		t.Errorf("aac stream: %v", err)
	} else {
		d.Close()
	}
}
