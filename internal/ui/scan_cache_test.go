package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// Al arrancar con la caché de esas carpetas, la biblioteca sale de ella sin
// escanear, igual a la que da el escaneo (que entonces no la reemplaza); con
// otras carpetas, o sin caché, no hay biblioteca de la caché.
func TestCachedLibrary(t *testing.T) {
	music := t.TempDir()
	for _, f := range []string{"tone.mp3", "tone.flac", "tone.ogg"} {
		b, err := os.ReadFile(filepath.Join("..", "decode", "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(music, f), b, 0o644)
	}
	paths := config.Paths{Data: t.TempDir()}
	dirs := []string{music}
	if loadCachedLibrary(paths, dirs) != nil {
		t.Fatal("a library with no cache")
	}
	s := StartScan(paths, dirs)
	var scanned *Library
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if lib, err, ok := s.Result(); ok {
			if err != nil {
				t.Fatal(err)
			}
			scanned = lib
			break
		}
	}
	if scanned == nil || len(scanned.Songs) != 3 {
		t.Fatalf("scan: %v", scanned)
	}
	cached := loadCachedLibrary(paths, dirs)
	if cached == nil || !sameLibrary(cached, scanned) {
		t.Fatalf("the cached library differs from the scanned one")
	}
	if loadCachedLibrary(paths, []string{t.TempDir()}) != nil {
		t.Error("a cached library for other folders")
	}
	scanned.Songs[0].Title = "changed"
	if sameLibrary(cached, scanned) {
		t.Error("a changed title wasn't noticed")
	}
}
