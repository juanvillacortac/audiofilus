package decode

import (
	"math"
	"os"
	"testing"
)

// Los capítulos de un M4B (los de Nero y, aparte, los de la pista de texto
// de QuickTime, que ffmpeg escribe las dos) y de un MP3 (CHAP de ID3) salen
// con su comienzo y su título, en orden.
func TestChapters(t *testing.T) {
	want := []Chapter{{0, "One"}, {1, "Two"}, {2, "Three"}}
	check := func(name string, got []Chapter) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %d chapters %+v, want %d", name, len(got), got, len(want))
		}
		for i := range want {
			if got[i].Title != want[i].Title || math.Abs(got[i].Start-want[i].Start) > 0.05 {
				t.Errorf("%s: chapter %d = %+v, want %+v", name, i, got[i], want[i])
			}
		}
	}
	check("m4b", Chapters("testdata/chapters.m4b"))
	check("mp3", Chapters("testdata/chapters.mp3"))

	// La pista de QuickTime sola (sin chpl).
	f, err := os.Open("testdata/chapters.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	moov, err := readMoov(f)
	if err != nil {
		t.Fatal(err)
	}
	check("quicktime", quicktimeChapters(f, moov))

	if ch := Chapters("testdata/tone.mp3"); ch != nil {
		t.Errorf("a file without chapters gave %+v", ch)
	}
}
