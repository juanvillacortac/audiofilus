package ui

import (
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/musicbrainz"
)

// TestGuessArtistTitle comprueba lo que se busca de una canción: sus
// etiquetas, o lo que dice el nombre del archivo sin el número de pista.
func TestGuessArtistTitle(t *testing.T) {
	for _, c := range []struct {
		s             Song
		artist, title string
	}{
		{Song{Title: "Bohemian Rhapsody", Artist: "Queen"}, "Queen", "Bohemian Rhapsody"},
		{Song{Title: "01 - Queen - Bohemian Rhapsody"}, "Queen", "Bohemian Rhapsody"},
		{Song{Title: "03. Some_Song"}, "", "Some Song"},
		{Song{Title: "1-02 Artist - Track"}, "Artist", "Track"},
		{Song{Title: "1999", Artist: "Prince"}, "Prince", "1999"}, // un número que es el título
	} {
		a, ti := guessArtistTitle(c.s)
		if a != c.artist || ti != c.title {
			t.Errorf("%+v: got %q, %q; want %q, %q", c.s, a, ti, c.artist, c.title)
		}
	}
}

// TestBestRelease comprueba qué edición se elige: un álbum antes que un
// sencillo, y de los álbumes el más antiguo.
func TestBestRelease(t *testing.T) {
	rec := musicbrainz.Recording{Releases: []musicbrainz.ReleaseRef{
		{Title: "Single", Type: "Single", Date: "1990"},
		{Title: "Hits", Type: "Album", Date: "2005"},
		{Title: "Original", Type: "Album", Date: "1991-03"},
	}}
	if r, ok := bestRelease(rec); !ok || r.Title != "Original" {
		t.Errorf("got %+v", r)
	}
	if _, ok := bestRelease(musicbrainz.Recording{}); ok {
		t.Error("no releases should give none")
	}
	if releaseYear("1997-05-21") != 1997 || releaseYear("") != 0 {
		t.Error("releaseYear")
	}
}
