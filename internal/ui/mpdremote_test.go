package ui

import (
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/mpd"
)

// Una canción de la biblioteca de otro audiofilus va en la cola del envío
// como mpdlib://<host>/<uri>, y se manda por su URI con los nombres que se
// vieron al navegar (o el del archivo si no).
func TestMPDLibItem(t *testing.T) {
	a := &App{mpdSongs: mpdRemoteSongs{}}
	p := mpdLibPath("192.168.1.5:6600", "Band/First/01 Uno.flac")
	if host, uri, ok := splitMPDLib(p); !ok || host != "192.168.1.5:6600" || uri != "Band/First/01 Uno.flac" {
		t.Fatalf("split %q: %q %q %v", p, host, uri, ok)
	}
	if it := a.mpdCastItem(p); it.RemoteURI != "Band/First/01 Uno.flac" || it.Title != "01 Uno" || it.Path != "" {
		t.Errorf("item without tags: %+v", it)
	}
	a.mpdSongs[p] = mpd.Song{File: "Band/First/01 Uno.flac", Tags: mpd.Tags{Title: "Uno", Artist: "Band"}, Duration: 61}
	if it := a.mpdCastItem(p); it.Title != "Uno" || it.Artist != "Band" || it.Duration != 61 {
		t.Errorf("item with tags: %+v", it)
	}
	if got := filterQuote(`It's \ fine`); got != `'It\'s \\ fine'` {
		t.Errorf("filterQuote = %s", got)
	}
}
