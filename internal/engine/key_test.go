package engine

import (
	"math"
	"testing"
)

// chords suena una progresión de acordes (notas MIDI) a 48 kHz, un segundo
// cada uno, con dos armónicos por nota como un instrumento.
func chords(prog [][]int, rounds int) []float64 {
	var out []float64
	for r := 0; r < rounds; r++ {
		for _, ch := range prog {
			for i := 0; i < 48000; i++ {
				t := float64(i) / 48000
				v := 0.0
				for _, n := range ch {
					f := 440 * math.Pow(2, float64(n-69)/12)
					v += math.Sin(2*math.Pi*f*t) + 0.4*math.Sin(4*math.Pi*f*t)
				}
				out = append(out, v*0.1)
			}
		}
	}
	return out
}

// La tonalidad sale de una progresión típica: en re mayor (I IV V I) es D, en
// la menor (i iv V i) es Am, y sin oír lo suficiente no se dice nada.
func TestKeyFinder(t *testing.T) {
	for _, c := range []struct {
		name string
		prog [][]int
	}{
		{"D", [][]int{{62, 66, 69}, {67, 71, 74}, {69, 73, 76}, {62, 66, 69}}},
		{"Am", [][]int{{57, 60, 64}, {62, 65, 69}, {64, 68, 71}, {57, 60, 64}}},
	} {
		k := newKeyFinder(48000)
		if k.key() != "" {
			t.Errorf("%s: a key before hearing anything", c.name)
		}
		for _, x := range chords(c.prog, 2) {
			k.add(x)
		}
		if got := k.key(); got != c.name {
			t.Errorf("key = %q, want %q", got, c.name)
		}
		k.reset()
		if k.key() != "" {
			t.Errorf("%s: still a key after reset", c.name)
		}
	}
}
