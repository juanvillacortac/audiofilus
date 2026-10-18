package sfx

import (
	"math"
	"testing"
)

// TestSounds comprueba que cada sonido existe, dura menos de medio segundo
// (no estorba a la música) y no satura.
func TestSounds(t *testing.T) {
	if d := float64(len(Samples(Splash))/2) / Rate; d < 2 || d > 3 {
		t.Errorf("splash: %.2f s", d)
	}
	for name := range recipes {
		s := Samples(name)
		if len(s) == 0 {
			t.Errorf("%s: empty", name)
			continue
		}
		if d := float64(len(s)/2) / Rate; d > 0.5 {
			t.Errorf("%s: %.2f s long", name, d)
		}
		for _, x := range s {
			if math.Abs(float64(x)) > 1 {
				t.Errorf("%s: clips (%f)", name, x)
				break
			}
		}
	}
	if Gain("off") != 0 || Gain("high") <= Gain("") || Gain("") <= Gain("low") {
		t.Error("gain levels out of order")
	}
}
