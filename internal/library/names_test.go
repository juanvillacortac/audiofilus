package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// tone copia el FLAC de prueba de internal/decode a path.
func tone(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "decode", "testdata", "tone.flac"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestM3UWindows1252 comprueba que una lista .m3u guardada en Windows-1252
// (la codificación de las .m3u sin el 8, hechas en Windows) encuentre las
// canciones con tildes de un disco en UTF-8.
func TestM3UWindows1252(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "Canción", "01 Pequeña.flac")
	tone(t, song)
	list := filepath.Join(dir, "list.m3u")
	os.WriteFile(list, []byte("#EXTM3U\r\nCanci\xf3n\\01 Peque\xf1a.flac\r\n"), 0o644)
	got, err := Expand([]string{list})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{song}) {
		t.Errorf("Expand = %q, want %q", got, []string{song})
	}
}

// TestM3UNormalization comprueba que una lista que nombra en NFC una canción
// guardada en NFD (copiada desde macOS) dé la ruta que hay en el disco.
func TestM3UNormalization(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, norm.NFD.String("Álbum"), norm.NFD.String("01 Pequeña.flac"))
	tone(t, song)
	if _, err := os.Lstat(filepath.Join(dir, norm.NFC.String("Álbum"))); err == nil {
		t.Skip("this file system ignores the normalization (APFS)")
	}
	list := filepath.Join(dir, "list.m3u8")
	os.WriteFile(list, []byte(norm.NFC.String("Álbum/01 Pequeña.flac")+"\n"), 0o644)
	got, err := Expand([]string{list})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{song}) {
		t.Errorf("Expand = %q, want %q", got, []string{song})
	}
}

// TestCueNormalization comprueba que una hoja CUE que nombra en NFC su
// archivo, guardado en NFD, lo encuentre.
func TestCueNormalization(t *testing.T) {
	dir := t.TempDir()
	disk := filepath.Join(dir, norm.NFD.String("Canción.flac"))
	tone(t, disk)
	got, ok := ResolveCueFile(dir, norm.NFC.String("Canción.wav"))
	if !ok || got != disk {
		t.Errorf("ResolveCueFile = %q, %v; want %q", got, ok, disk)
	}
}

// TestExpandMangledPath comprueba que una ruta con bytes que no son UTF-8,
// estropeada al pasar por JSON (el socket), se pueda seguir añadiendo a la
// cola y leer sus etiquetas.
func TestExpandMangledPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux file systems take names that are not UTF-8")
	}
	dir := t.TempDir()
	song := filepath.Join(dir, "Caf\xe9", "01.flac")
	tone(t, song)
	var back string
	b, _ := json.Marshal(song)
	_ = json.Unmarshal(b, &back)
	got, err := Expand([]string{back})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{song}) {
		t.Errorf("Expand = %q, want %q", got, []string{song})
	}
	if tr := ReadTrack(back); tr.Artist != "audiofilus" {
		t.Errorf("ReadTrack artist = %q, want the file's tag", tr.Artist)
	}
}
