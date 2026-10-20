package radio

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// TestRadioLive busca emisoras y escucha unos segundos de una de cada códec
// (solo con AUDIOFILUS_NET_TESTS=1: necesita red).
func TestRadioLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	top, err := Top(ctx, "VE")
	if err != nil || len(top) == 0 {
		t.Fatalf("top: %v (%d)", err, len(top))
	}
	for _, s := range top[:3] {
		t.Logf("%s | %s %dk | %s", s.Name, s.Codec, s.Bitrate, s.URL)
	}
	tried := map[string]bool{}
	for _, s := range top {
		c := codecOf(s.Codec, "")
		if tried[c] {
			continue
		}
		tried[c] = true
		st := Open(s.URL, s.Codec)
		deadline := time.Now().Add(20 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			r, err := st.Ready()
			if err != nil {
				t.Logf("%s (%s): %v", s.Name, c, err)
				break
			}
			if r {
				ready = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !ready {
			st.Close()
			continue
		}
		buf := make([]float32, 4096)
		var energy float64
		for i := 0; i < 100; i++ {
			n, _ := st.Read(buf)
			for _, x := range buf[:n] {
				energy += math.Abs(float64(x))
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("%s (%s): %d Hz, title %q, energy %.0f", s.Name, c, st.SampleRate(), st.Title(), energy)
		if energy == 0 {
			t.Errorf("%s: only silence", s.Name)
		}
		st.Close()
	}
}

// TestRadioPlaylistLive conecta con una emisora de verdad por su .pls (con
// AUDIOFILUS_NET_TESTS; si no, se salta) y espera a que suene.
func TestRadioPlaylistLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	s := Open("https://somafm.com/groovesalad.pls", "")
	defer s.Close()
	for i := 0; i < 150; i++ {
		ready, err := s.Ready()
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the station never started")
}
