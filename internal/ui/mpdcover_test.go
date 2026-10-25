package ui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/mpd"
)

// stillPlayer es un motor quieto para un servidor MPD de prueba: lo que no
// se usa (todo menos el estado y la cola) no hace falta.
type stillPlayer struct{ mpd.Player }

func (stillPlayer) Status() mpd.Status { return mpd.Status{} }
func (stillPlayer) Queue() []string    { return nil }

// La portada de un álbum de la biblioteca de un servidor MPD se pide por la
// primera de sus canciones (find, y albumart: aquí, su cover.png suelto);
// la de una canción, por su URI.
func TestMPDCover(t *testing.T) {
	root := t.TempDir()
	data, dir := filepath.Join(root, "data"), filepath.Join(root, "Music", "Band", "First")
	os.MkdirAll(data, 0o755)
	os.MkdirAll(dir, 0o755)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	os.WriteFile(filepath.Join(dir, "cover.png"), b.Bytes(), 0o644)
	lib, _ := json.Marshal(map[string]any{"dirs": []string{filepath.Join(root, "Music")}, "entries": []map[string]any{
		{"path": filepath.Join(dir, "01.mp3"), "title": "Uno", "artist": "Band", "album": "First"},
	}})
	os.WriteFile(filepath.Join(data, "library.json"), lib, 0o644)
	s, err := mpd.Start(mpd.Options{Addr: "127.0.0.1:0", DataDir: data, Player: stillPlayer{}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host := s.Addr().String()
	for _, p := range []string{mpdAlbumCover(host, mpdAlbum{artist: "Band", title: "First"}), mpdLibPath(host, "Band/First/01.mp3")} {
		got := decodeMPDCover(p)
		if got == nil || got.Bounds().Dx() != 8 {
			t.Errorf("%q: %v", p, got)
		}
	}
	if decodeMPDCover(mpdAlbumCover(host, mpdAlbum{artist: "Nobody", title: "None"})) != nil {
		t.Error("cover for a missing album")
	}
}
