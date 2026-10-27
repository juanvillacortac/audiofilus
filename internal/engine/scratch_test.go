package engine

import (
	"math"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// TestScratchResumes comprueba que el scratch suena (con la canción, ida y
// vuelta) y que al acabar sigue justo desde donde estaba la aguja, sin saltar
// ni repetir: lo que viene después es la historia desde ese punto.
func TestScratchResumes(t *testing.T) {
	e := &Engine{rate: 8000}
	song := make([]float32, 2*8000) // un segundo: una rampa, para saber de dónde sale cada cuadro
	for i := 0; i < len(song)/2; i++ {
		song[2*i], song[2*i+1] = float32(i)/8000, -float32(i)/8000
	}
	e.histWrite(song)
	start := int64(6000) // la aguja: lo que se oía
	e.scr = &scratch{moves: scratchMoves[1], start: float64(start), replay: -1}
	out := make([]float32, 2*8000)
	n := e.scratchRead(out)
	if e.scr != nil {
		t.Fatalf("scratch not finished after %d samples", n)
	}
	moveFrames := int(math.Round(scratchMoves[1][0].dur * 8000))
	if want := 2 * (moveFrames + int(e.histN-start)); n != want {
		t.Errorf("filled %d samples, want %d (the move plus the unheard rest)", n, want)
	}
	loud := 0
	for i := 0; i < 2*moveFrames; i += 2 {
		if out[i] != 0 {
			loud++
		}
	}
	if loud < moveFrames/2 {
		t.Errorf("the scratch is mostly silent (%d of %d frames)", loud, moveFrames)
	}
	for k := 0; k < int(e.histN-start); k++ {
		if got, want := out[2*(moveFrames+k)], float32(start+int64(k))/8000; got != want {
			t.Fatalf("after the scratch, frame %d is %v, want %v", k, got, want)
		}
	}
}

// Con la app cerrada o en sleep (sin medidor) la historia del scratch no se
// graba, y al volver empieza vacía: un scratch nunca suena a lo de antes.
func TestScratchHistoryOnlyWhenWatched(t *testing.T) {
	e := testEngine(48000, DSP{})
	e.OpenRemote = func(string) (decode.Source, bool, error) { return &liveSource{}, true, nil }
	e.q.Set([]string{"radio://a"}, 0)
	e.openLocked()
	e.playing = true
	p := make([]byte, 4096*bytesPerFrame)
	e.Read(p)
	if e.histN != 0 {
		t.Fatalf("history recorded with the app closed: %d frames", e.histN)
	}
	e.SetSpectrum(true)
	e.Read(p)
	if e.histN == 0 {
		t.Fatal("no history with the app open")
	}
	e.SetSpectrum(false)
	n := e.histN
	e.Read(p)
	e.SetSpectrum(true)
	if e.histN != n || e.histFrom != n {
		t.Errorf("after sleep: histN %d, histFrom %d, want both %d", e.histN, e.histFrom, n)
	}
}
