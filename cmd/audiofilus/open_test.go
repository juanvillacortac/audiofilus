package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenArgs comprueba qué se toma por archivos abiertos desde el
// explorador: rutas que existen (también como URI file://), no comandos,
// opciones ni rutas que no están.
func TestOpenArgs(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "a song.flac")
	if err := os.WriteFile(song, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := openArgs([]string{song, dir}); !ok || len(got) != 2 || got[0] != song {
		t.Errorf("paths = %q, %v", got, ok)
	}
	if got, ok := openArgs([]string{"file://" + filepath.ToSlash(dir) + "/a%20song.flac"}); !ok || got[0] != song {
		t.Errorf("file URI = %q, %v", got, ok)
	}
	for _, args := range [][]string{nil, {"status"}, {"-shots", dir}, {filepath.Join(dir, "missing.mp3")}, {song, "next"}} {
		if _, ok := openArgs(args); ok {
			t.Errorf("%q taken as files to open", args)
		}
	}
}
