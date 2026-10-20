package ui

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

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

// TestTrackerFeedsMeter comprueba que lo que toca el tracker llegue al
// medidor (meterTap) y que, mientras toca, sea lo que miran los
// visualizadores y los juegos (meterPts, hearing).
func TestTrackerFeedsMeter(t *testing.T) {
	an := engine.NewAnalyzer(44100, 0)
	src := make([]byte, 4*2*4096) // 4096 cuadros estéreo de un tono
	for i := 0; i < 2*4096; i++ {
		v := float32(0.5 * math.Sin(float64(i/2)*2*math.Pi*110/44100))
		binary.LittleEndian.PutUint32(src[i*4:], math.Float32bits(v))
	}
	tap := &meterTap{r: bytes.NewReader(src), an: an}
	if _, err := io.ReadAll(tap); err != nil {
		t.Fatal(err)
	}
	pts := an.Points()
	if len(pts) < 3 || pts[0].Level == 0 || pts[0].Bass == 0 {
		t.Fatalf("meter points = %+v", pts)
	}
	a := &App{tracker: &trackerState{playing: true, an: an}}
	if !a.hearing() || len(a.meterPts()) != len(pts) {
		t.Error("while the tracker plays, the meter should be the tracker's")
	}
	a.tracker.playing = false
	if a.hearing() || len(a.meterPts()) != 0 {
		t.Error("with the tracker stopped and no music, nothing should be heard")
	}
}
