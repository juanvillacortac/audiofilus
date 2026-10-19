package tracker

import (
	"bytes"
	"math"
	"path/filepath"
	"testing"
)

// TestNoteName comprueba los nombres de las notas.
func TestNoteName(t *testing.T) {
	for n, want := range map[int8]string{1: "C-0", 49: "C-4", 54: "F-4", 96: "B-7", NoteEmpty: "---", NoteOff: "OFF"} {
		if got := NoteName(n); got != want {
			t.Errorf("NoteName(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestRender comprueba que la canción de ejemplo suena (no es silencio, no
// satura) y dura lo que debe: 16 semicorcheas a 120 BPM son 2 s, más 1 s de
// cola.
func TestRender(t *testing.T) {
	s := NewSong()
	out := Render(s, 48000)
	if got, want := len(out)/2, 3*48000; math.Abs(float64(got-want)) > 48 {
		t.Errorf("rendered %d frames, want ~%d", got, want)
	}
	var peak, energy float64
	for _, x := range out {
		peak = math.Max(peak, math.Abs(float64(x)))
		energy += float64(x * x)
	}
	if energy/float64(len(out)) < 1e-4 {
		t.Error("the example song is silent")
	}
	if peak > 1 {
		t.Errorf("clips: peak %.2f", peak)
	}
}

// TestNoteOffSilences comprueba que OFF apaga el canal.
func TestNoteOffSilences(t *testing.T) {
	s := &Song{BPM: 120, Patterns: []Pattern{NewPattern()}, Order: []int{0}}
	s.Patterns[0][0][Pulse1] = Cell{Note: 49, Vol: 15}
	s.Patterns[0][4][Pulse1] = Cell{Note: NoteOff, Vol: -1}
	out := Render(s, 48000)
	step := 48000 * 60 / 120 / 4
	loud := func(from, to int) bool {
		for i := from * 2; i < to*2; i++ {
			if math.Abs(float64(out[i])) > 0.01 {
				return true
			}
		}
		return false
	}
	if !loud(step, 2*step) {
		t.Error("the note doesn't sound")
	}
	if loud(5*step, 6*step) {
		t.Error("OFF didn't silence the channel")
	}
}

// TestSaveLoad comprueba que una canción vuelve igual de su archivo.
func TestSaveLoad(t *testing.T) {
	s := NewSong()
	s.BPM = 140
	s.Patterns = append(s.Patterns, NewPattern())
	s.Order = []int{0, 1, 0}
	p := filepath.Join(t.TempDir(), "s.json")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.BPM != 140 || len(got.Patterns) != 2 || len(got.Order) != 3 || got.Patterns[0] != s.Patterns[0] {
		t.Errorf("round trip changed the song: %+v", got)
	}
}

// TestWAV comprueba la cabecera del WAV.
func TestWAV(t *testing.T) {
	var b bytes.Buffer
	if err := WriteWAV(&b, make([]float32, 200), 48000); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 44+400 || string(b.Bytes()[:4]) != "RIFF" || string(b.Bytes()[8:12]) != "WAVE" {
		t.Errorf("bad WAV: %d bytes", b.Len())
	}
}

// TestNoteParse comprueba que note y NoteName son inversas.
func TestNoteParse(t *testing.T) {
	for n := int8(1); n <= NoteMax; n++ {
		if got := note(NoteName(n)); got != n {
			t.Fatalf("note(%q) = %d, want %d", NoteName(n), got, n)
		}
	}
	if note("OFF") != NoteOff || note("H-4") != NoteEmpty {
		t.Error("special notes")
	}
}

// TestExamples comprueba que las canciones de ejemplo son válidas y suenan.
func TestExamples(t *testing.T) {
	for _, s := range Examples() {
		c := s.Clone()
		c.Fix()
		if len(c.Order) != len(s.Order) || c.BPM != s.BPM {
			t.Errorf("%s: out of bounds", s.Name)
		}
		notes := 0
		for _, p := range s.Patterns {
			for _, row := range p {
				for _, cell := range row {
					if cell.Note > 0 {
						notes++
					}
				}
			}
		}
		if notes < 20*len(s.Patterns) {
			t.Errorf("%s: only %d notes", s.Name, notes)
		}
		out := Render(s, 8000)
		var e float64
		for _, x := range out {
			e += float64(x * x)
		}
		if e/float64(len(out)) < 1e-4 {
			t.Errorf("%s: silent", s.Name)
		}
	}
}
