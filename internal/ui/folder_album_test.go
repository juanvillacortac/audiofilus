package ui

import (
	"path/filepath"
	"testing"
)

// TestFolderAlbum comprueba que una canción sin álbum toma el de su carpeta
// (el de arriba si es la carpeta de un disco), y que las sueltas en una
// carpeta de la música y las remotas siguen sin álbum.
func TestFolderAlbum(t *testing.T) {
	root := musicRoot(t)
	cases := []struct{ path, want string }{
		{"Artist/Album One/01.wma", "Album One"},
		{"Album Two/CD2/01.flac", "Album Two"},
		{"Album Two/Disc 1/01.flac", "Album Two"},
		{"CD1/01.flac", "CD1"}, // la de arriba es la raíz: se queda con la suya
		{"loose.mp3", ""},
	}
	for _, c := range cases {
		p := filepath.Join(root, filepath.FromSlash(c.path))
		if got := folderAlbum(p, []string{root}); got != c.want {
			t.Errorf("folderAlbum(%q) = %q, want %q", p, got, c.want)
		}
	}
	for _, p := range []string{"subsonic://srv/song/1", "relative.mp3"} {
		if got := folderAlbum(p, []string{root}); got != "" {
			t.Errorf("folderAlbum(%q) = %q, want none", p, got)
		}
	}
}

// musicRoot es una carpeta de música absoluta en cualquier sistema ("/music"
// no lo es en Windows, le falta la unidad).
func musicRoot(t *testing.T) string {
	root, err := filepath.Abs(filepath.FromSlash("/music"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestBuildLibraryFolderAlbum comprueba que las canciones sin etiqueta de
// álbum de una misma carpeta quedan juntas en un álbum con su nombre, y no en
// "Sin álbum".
func TestBuildLibraryFolderAlbum(t *testing.T) {
	root := musicRoot(t)
	rip1, rip2, loose := filepath.Join(root, "Rip", "01.wma"), filepath.Join(root, "Rip", "02.wma"), filepath.Join(root, "suelta.wma")
	lib := buildLibrary([]entry{
		{Path: rip1, Title: "Uno", Artist: "A"},
		{Path: rip2, Title: "Dos", Artist: "A", Album: "Rip"},
		{Path: loose, Title: "Tres"},
	}, []string{root})
	if al := lib.byPath[rip1]; al == nil || al.Title != "Rip" || al.noAlbum || len(al.Songs) != 2 {
		t.Errorf("untagged song not grouped with its folder: %+v", al)
	}
	if al := lib.byPath[loose]; al == nil || !al.noAlbum {
		t.Errorf("loose song in the root should have no album: %+v", al)
	}
}
