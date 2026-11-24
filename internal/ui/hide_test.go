package ui

import (
	"slices"
	"testing"
)

// TestAlbumsByFolder comprueba que, con Álbumes por carpeta, cada carpeta es
// un álbum con su nombre aunque sus etiquetas digan otra cosa, que la canción
// conserva su álbum de las etiquetas y que una carpeta con varios artistas
// queda como "Varios artistas".
func TestAlbumsByFolder(t *testing.T) {
	entries := []entry{
		{Path: "/m/Mix/a.mp3", Title: "A", Artist: "X", Album: "One"},
		{Path: "/m/Mix/b.mp3", Title: "B", Artist: "Y", Album: "Two"},
		{Path: "/m/Solo/c.flac", Title: "C", Artist: "Z", Album: "One"},
	}
	albumsByFolder.Store(true)
	defer albumsByFolder.Store(false)
	lib := buildLibrary(entries, []string{"/m"})
	if len(lib.Albums) != 2 {
		t.Fatalf("albums = %d, want 2 (one per folder)", len(lib.Albums))
	}
	byTitle := map[string]*Album{}
	for _, al := range lib.Albums {
		byTitle[al.Title] = al
	}
	mix, solo := byTitle["Mix"], byTitle["Solo"]
	if mix == nil || solo == nil {
		t.Fatalf("album titles = %v, want Mix and Solo", lib.Albums)
	}
	if mix.Artist != T("various") || solo.Artist != "Z" {
		t.Errorf("artists = %q, %q", mix.Artist, solo.Artist)
	}
	if mix.Songs[0].Album != "One" {
		t.Errorf("song album = %q, want the tag's", mix.Songs[0].Album)
	}
	albumsByFolder.Store(false)
	if n := len(buildLibrary(entries, []string{"/m"}).Albums); n != 3 {
		t.Errorf("by tags: albums = %d, want 3", n)
	}
}

// TestHiddenFormats comprueba que los formatos quitados de una sección no
// salen en ella (ni los álbumes y artistas que solo los tienen), y sí en las
// demás.
func TestHiddenFormats(t *testing.T) {
	lib := buildLibrary([]entry{
		{Path: "/m/A/1.mp3", Artist: "A", Album: "A"},
		{Path: "/m/B/1.flac", Artist: "B", Album: "B"},
		{Path: "/m/B/2.mp3", Artist: "B", Album: "B"},
	}, []string{"/m"})
	a := &App{lib: lib, local: lib}
	a.cfg.UI.HiddenFormats = map[string][]string{"albums": {"mp3"}, "songs": {"flac"}}
	if v := a.rootView(tabAlbums); len(v.albums) != 1 || v.albums[0].Title != "B" {
		t.Fatalf("albums = %v, want only B", v.albums)
	}
	a.libTab = tabAlbums
	if v := a.albumView(lib.Albums[1]); len(v.songs) != 1 || songFormat(v.songs[0].Path) != "flac" {
		t.Errorf("album songs = %v, want only the flac", v.songs)
	}
	if v := a.rootView(tabSongs); len(v.songs) != 2 {
		t.Errorf("songs = %d, want the two mp3s", len(v.songs))
	}
	if v := a.rootView(tabArtists); len(v.artists) != 2 {
		t.Errorf("artists = %d, want both (no filter there)", len(v.artists))
	}
	if got := libraryFormats(lib); !slices.Equal(got, []string{"flac", "mp3"}) {
		t.Errorf("formats = %v", got)
	}
}

// TestHiddenTabsAndScreens comprueba que las secciones y pantallas quitadas
// se saltan, y que se entra por la primera sección que queda.
func TestHiddenTabsAndScreens(t *testing.T) {
	a := &App{}
	a.cfg.UI.HiddenTabs = []string{"songs", "artists"}
	if got := a.visibleTabs(); !slices.Equal(got, []int{tabSearch, tabAlbums, tabLists, tabFolders}) {
		t.Errorf("visible tabs = %v", got)
	}
	if a.homeTab() != tabAlbums {
		t.Errorf("home tab = %d, want albums", a.homeTab())
	}
	a.libTab = tabAlbums
	if a.stepTab(1) != tabLists || a.stepTab(-1) != tabSearch {
		t.Errorf("step = %d, %d", a.stepTab(1), a.stepTab(-1))
	}
	a.libTab = tabFolders
	if a.stepTab(1) != tabSearch {
		t.Errorf("step from the last = %d, want search", a.stepTab(1))
	}
	a.cfg.UI.HiddenScreens = []string{"radio", "games"}
	for _, e := range a.docks() {
		if e.screen == Radio || e.screen == Games {
			t.Errorf("hidden screen %v is in the bar", e.screen)
		}
	}
	if !slices.ContainsFunc(a.docks(), func(e dockEntry) bool { return e.screen == Settings }) {
		t.Errorf("settings left the bar")
	}
}
