package modsrc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// TestLive prueba los cuatro archivos de verdad, por internet: buscar, mirar
// sus listas, bajar algo pequeño y que se pueda decodificar. Solo con
// AUDIOFILUS_LIVE=1 (pide a servidores ajenos; no va en la CI).
// AUDIOFILUS_LIVE_DIR, si está, guarda allí el índice de Modland y lo
// bajado (para no repetir descargas entre pruebas).
func TestLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_LIVE") != "1" {
		t.Skip("set AUDIOFILUS_LIVE=1 to hit the real sites")
	}
	politeGap = time.Second
	base := os.Getenv("AUDIOFILUS_LIVE_DIR")
	if base == "" {
		base = t.TempDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	queries := map[string]string{"modarchive": "space debris", "modland": "purple motion", "vgmrips": "sonic", "amp": "space"}
	artists := map[string]string{"modarchive": "Purple Motion", "modland": "Purple Motion", "vgmrips": "Masato Nakamura", "amp": "Jester"}
	ids := []string{"modarchive", "modland", "vgmrips", "amp"}
	for i, src := range Sources(filepath.Join(base, "cache")) {
		id := ids[i]
		t.Run(id, func(t *testing.T) {
			items, err := src.Search(ctx, queries[id])
			if err != nil || len(items) == 0 {
				t.Fatalf("search: %v (%d results)", err, len(items))
			}
			t.Logf("search %q: %d results, first %+v", queries[id], len(items), items[0])
			for _, what := range []string{"random", "top", "new"} {
				got, err := src.Browse(ctx, what)
				t.Logf("browse %s: %d results, err %v", what, len(got), err)
				if err != nil && err != ErrUnsupported {
					t.Errorf("browse %s: %v", what, err)
				}
			}
			art, err := src.ByArtist(ctx, artists[id])
			t.Logf("by artist %q: %d results, err %v", artists[id], len(art), err)
			if err != nil || len(art) == 0 {
				t.Errorf("by artist: %v (%d)", err, len(art))
			}
			// Lo más pequeño de los resultados, para bajar poco.
			pick := items[0]
			for _, it := range items {
				if it.Size > 0 && (pick.Size == 0 || it.Size < pick.Size) {
					pick = it
				}
			}
			files, err := Fetch(ctx, pick, filepath.Join(base, "dl"), nil)
			if err != nil {
				t.Fatalf("fetch %+v: %v", pick, err)
			}
			src, err := decode.Open(files[0])
			if err != nil {
				t.Fatalf("decode %s: %v", files[0], err)
			}
			defer src.Close()
			buf := make([]float32, 4096)
			n, err := src.Read(buf)
			if n == 0 {
				t.Fatalf("read %s: %v", files[0], err)
			}
			t.Logf("fetched %s (%d files), decoded %d samples at %d Hz", files[0], len(files), n, src.SampleRate())
		})
	}
}
