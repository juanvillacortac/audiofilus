package ui

import "testing"

// TestRadioSong comprueba cómo se parte el título de una radio para buscar
// su letra: "Artista - Canción", con guiones en la canción, y lo que no es
// una canción.
func TestRadioSong(t *testing.T) {
	for _, c := range []struct{ in, artist, title string }{
		{"Radiohead - A Wolf at the Door", "Radiohead", "A Wolf at the Door"},
		{"AC/DC - Back In Black - Live", "AC/DC", "Back In Black - Live"},
		{"Classic FM UK", "", ""},
		{" - Untitled", "", ""},
	} {
		artist, title, ok := radioSong(c.in)
		if ok != (c.artist != "") || (ok && (artist != c.artist || title != c.title)) {
			t.Errorf("radioSong(%q) = %q, %q, %v", c.in, artist, title, ok)
		}
	}
}
