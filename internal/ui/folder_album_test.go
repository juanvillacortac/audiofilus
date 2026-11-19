package ui

import (
	"path/filepath"
	"testing"
)

// TestFolderAlbum comprueba que una canción sin álbum toma el de su carpeta
// (el de arriba si es la carpeta de un disco), y que las sueltas en una
// carpeta de la música y las remotas siguen sin álbum.
func TestFolderAlbum(t *testing.T) {
	root := filepath.FromSlash("/music")
	cases := []struct{ path, want string }{
		{"/music/Artist/Album One/01.wma", "Album One"},
		{"/music/Album Two/CD2/01.flac", "Album Two"},
		{"/music/Album Two/Disc 1/01.flac", "Album Two"},
		{"/music/CD1/01.flac", "CD1"}, // la de arriba es la raíz: se queda con la suya
		{"/music/loose.mp3", ""},
		{"subsonic://srv/song/1", ""},
		{"relative.mp3", ""},
	}
	for _, c := range cases {
		if got := folderAlbum(filepath.FromSlash(c.path), []string{root}); got != c.want {
			t.Errorf("folderAlbum(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestBuildLibraryFolderAlbum comprueba que las canciones sin etiqueta de
// álbum de una misma carpeta quedan juntas en un álbum con su nombre, y no en
// "Sin álbum".
func TestBuildLibraryFolderAlbum(t *testing.T) {
	lib := buildLibrary([]entry{
		{Path: "/music/Rip/01.wma", Title: "Uno", Artist: "A"},
		{Path: "/music/Rip/02.wma", Title: "Dos", Artist: "A", Album: "Rip"},
		{Path: "/music/suelta.wma", Title: "Tres"},
	}, []string{"/music"})
	if al := lib.byPath["/music/Rip/01.wma"]; al == nil || al.Title != "Rip" || al.noAlbum || len(al.Songs) != 2 {
		t.Errorf("untagged song not grouped with its folder: %+v", al)
	}
	if al := lib.byPath["/music/suelta.wma"]; al == nil || !al.noAlbum {
		t.Errorf("loose song in the root should have no album: %+v", al)
	}
}
