package library

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// writeNSF escribe un NSF de songs canciones mudas (su programa solo
// vuelve: RTS): basta para que libgme lo liste.
func writeNSF(t *testing.T, path string, songs int) {
	t.Helper()
	h := make([]byte, 0x80)
	copy(h, "NESM\x1a")
	h[5], h[6], h[7] = 1, byte(songs), 1
	binary.LittleEndian.PutUint16(h[0x08:], 0x8000) // carga
	binary.LittleEndian.PutUint16(h[0x0A:], 0x8000) // init
	binary.LittleEndian.PutUint16(h[0x0C:], 0x8001) // play
	copy(h[0x0E:], "Test Game")
	copy(h[0x2E:], "Tester")
	binary.LittleEndian.PutUint16(h[0x6E:], 16639) // NTSC, 60 Hz
	if err := os.WriteFile(path, append(h, 0x60, 0x60), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScanGMESongs comprueba que un NSF con tres canciones salga en la
// biblioteca como tres pistas, con el juego como disco, el autor como
// artista y "Juego #n" como título (el NSF no los nombra).
func TestScanGMESongs(t *testing.T) {
	if !decode.GMEAvailable() {
		t.Skip("libgme not installed")
	}
	dir := t.TempDir()
	writeNSF(t, filepath.Join(dir, "game.nsf"), 3)
	files, err := Expand([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, f := range files {
		tr := ReadTrack(f)
		if tr.Album != "Test Game" || tr.Artist != "Tester" {
			t.Errorf("%s: %+v", f, tr)
		}
		titles = append(titles, tr.Title)
	}
	if strings.Join(titles, "|") != "Test Game #1|Test Game #2|Test Game #3" {
		t.Errorf("titles = %q", titles)
	}
}
