package library

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSidecarTagsWin comprueba que las etiquetas del .tags.json (las que
// escribe audiofilus para podcasts y YouTube Music) mandan sobre las del
// archivo, aunque estas no vengan vacías.
func TestSidecarTagsWin(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("..", "decode", "testdata", "tone.mp3"))
	if err != nil {
		t.Skip("no test mp3")
	}
	p := filepath.Join(dir, "ep.mp3")
	os.WriteFile(p, src, 0o644)
	os.WriteFile(p+".tags.json", []byte(`{"title":"Cementerio de los hijos de Dios","artist":"Nuestro insólito universo"}`), 0o644)
	tr := ReadTrack(p)
	if tr.Title != "Cementerio de los hijos de Dios" || tr.Artist != "Nuestro insólito universo" || tr.Path != p {
		t.Errorf("got %+v", tr)
	}
}
