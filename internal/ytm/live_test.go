package ytm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSearchLive busca de verdad en YouTube Music (solo con
// AUDIOFILUS_NET_TESTS=1: necesita red).
func TestSearchLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := Search(ctx, "chevelle the red")
	if err != nil || len(res) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(res))
	}
	for _, r := range res[:min(3, len(res))] {
		t.Logf("%s | %s | %s | %s | %s", r.ID, r.Title, r.Artist(), r.Album, r.Duration)
	}
	if res[0].Artist() == "" || res[0].Album == "" || res[0].Duration == "" {
		t.Errorf("incomplete first result: %+v", res[0])
	}
}

// TestAlbumLive busca un álbum y trae sus canciones (con red, como
// TestSearchLive).
func TestAlbumLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_NET_TESTS") == "" {
		t.Skip("set AUDIOFILUS_NET_TESTS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	als, err := SearchAlbums(ctx, "vespertine bjork")
	if err != nil || len(als) == 0 {
		t.Fatalf("search albums: %v (%d results)", err, len(als))
	}
	al := als[0]
	t.Logf("%s | %s | %s | %s", al.ID, al.Title, al.Artist, al.Year)
	if al.Title != "Vespertine" || al.Year == "" || al.Artist == "" {
		t.Errorf("first album: %+v", al)
	}
	tracks, err := AlbumTracks(ctx, al)
	if err != nil || len(tracks) < 10 {
		t.Fatalf("album tracks: %v (%d tracks)", err, len(tracks))
	}
	for _, tr := range tracks[:3] {
		t.Logf("%s | %s | %s | %s | %s", tr.ID, tr.Title, tr.Artist(), tr.Album, tr.Duration)
	}
	if tr := tracks[0]; tr.Title != "Hidden Place" || tr.Artist() == "" || tr.Album != "Vespertine" || tr.Duration == "" || tr.Thumb == "" || tr.Track != 1 || tr.TrackTotal != len(tracks) || tr.Year != al.Year {
		t.Errorf("first track: %+v", tr)
	}
}
