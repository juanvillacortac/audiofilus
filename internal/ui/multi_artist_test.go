package ui

import (
	"path/filepath"
	"testing"
)

// TestBuildLibraryMultiArtist comprueba que una canción con varios
// artistas ("Alpha, Beta", como los une internal/tags) los muestre todos,
// mientras su álbum se archiva bajo el primero (sin artista del álbum) o
// bajo el primero del artista del álbum.
func TestBuildLibraryMultiArtist(t *testing.T) {
	root := musicRoot(t)
	lib := buildLibrary([]entry{
		{Path: filepath.Join(root, "a.flac"), Title: "A", Artist: "Alpha, Beta", Album: "One"},
		{Path: filepath.Join(root, "b.flac"), Title: "B", Artist: "Gamma, Delta", AlbumArtist: "Gamma, Epsilon", Album: "Two"},
	}, []string{root})
	s, ok := lib.song[filepath.Join(root, "a.flac")]
	if !ok || s.Artist != "Alpha, Beta" {
		t.Errorf("song artist %q, want all of them", s.Artist)
	}
	if s := lib.song[filepath.Join(root, "b.flac")]; s.Artist != "Gamma, Delta" {
		t.Errorf("song artist %q, want all of them", s.Artist)
	}
	var names []string
	for _, a := range lib.Artists {
		names = append(names, a.Name)
	}
	if len(names) != 2 || names[0] != "Alpha" || names[1] != "Gamma" {
		t.Errorf("library artists %q, want [Alpha Gamma]", names)
	}
}
