package history

import (
	"testing"
	"time"
)

// TestHistory comprueba guardar y leer escuchas, lo reciente (sin repetir)
// y las estadísticas (lo más escuchado primero, y desde una fecha).
func TestHistory(t *testing.T) {
	dir := t.TempDir()
	ls := []Listen{
		{At: 100, Path: "/a", Title: "A", Artist: "X", Album: "One", Duration: 1800},
		{At: 200, Path: "/b", Title: "B", Artist: "Y", Album: "Two", Duration: 1800},
		{At: 300, Path: "/a", Title: "A", Artist: "X", Album: "One", Duration: 1800},
		{At: 400, Path: "/c", Title: "C", Artist: "X", Album: "One", Duration: 1800},
	}
	for _, l := range ls {
		if err := Append(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	got := Load(dir)
	if len(got) != 4 || got[3].Path != "/c" {
		t.Fatalf("Load = %+v", got)
	}
	var recent []string
	for _, l := range Recent(got, 10) {
		recent = append(recent, l.Path)
	}
	if len(recent) != 3 || recent[0] != "/c" || recent[1] != "/a" || recent[2] != "/b" {
		t.Errorf("Recent = %v, want [/c /a /b]", recent)
	}
	s := Compute(got, time.Time{}, 5)
	if s.Plays != 4 || s.Hours != 2 || s.Songs != 3 || s.Artists != 2 {
		t.Errorf("stats = %+v", s)
	}
	if s.TopSongs[0].Path != "/a" || s.TopSongs[0].Plays != 2 || s.TopArtists[0].Name != "X" || s.TopArtists[0].Plays != 3 || s.TopAlbums[0].Name != "One" {
		t.Errorf("tops = %+v %+v %+v", s.TopSongs, s.TopArtists, s.TopAlbums)
	}
	if s := Compute(got, time.Unix(250, 0), 5); s.Plays != 2 {
		t.Errorf("since 250: %d plays, want 2", s.Plays)
	}
	if Load(t.TempDir()) != nil {
		t.Error("no history should be empty")
	}
}

// TestMarks comprueba las favoritas: la última marcada va primero, quitar
// la favorita y la valoración la borra, se guardan en disco, y lo del
// servidor se suma sin pisar lo de aquí.
func TestMarks(t *testing.T) {
	dir := t.TempDir()
	ms := LoadMarks(dir)
	_ = ms.Set("/a", Mark{Fav: true, Title: "A"})
	time.Sleep(1100 * time.Millisecond)
	_ = ms.Set("/b", Mark{Fav: true, Rating: 4})
	_ = ms.Set("/c", Mark{Rating: 2})
	if f := ms.Favorites(); len(f) != 2 || f[0] != "/b" || f[1] != "/a" {
		t.Errorf("Favorites = %v, want [/b /a]", f)
	}
	_ = ms.Set("/c", Mark{})
	again := LoadMarks(dir)
	if again.Get("/b").Rating != 4 || again.Get("/c") != (Mark{}) || !again.Get("/a").Fav {
		t.Errorf("reloaded marks are wrong: %+v %+v %+v", again.Get("/a"), again.Get("/b"), again.Get("/c"))
	}
	again.Merge([]Remote{{Path: "/b", Rating: 1, Title: "B"}, {Path: "/d", Fav: true, Rating: 5, Title: "D", Artist: "Y", Album: "Z"}})
	if again.Get("/b").Rating != 4 || !again.Get("/d").Fav || again.Get("/d").Rating != 5 {
		t.Errorf("merge: %+v %+v", again.Get("/b"), again.Get("/d"))
	}
}
