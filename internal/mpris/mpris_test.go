package mpris

import "testing"

// Un Play o Pause de los audífonos que pide lo que ya pasa alterna (están
// desincronizados: los FreeBuds mandan Pause con la música en pausa); el que
// pide lo contrario se hace tal cual.
func TestHeadsetToggles(t *testing.T) {
	for _, c := range []struct {
		cmd     string
		playing bool
		want    bool
	}{
		{"Pause", false, true},
		{"Play", true, true},
		{"Pause", true, false},
		{"Play", false, false},
		{"Next", false, false},
	} {
		if got := HeadsetToggles(c.cmd, c.playing); got != c.want {
			t.Errorf("%s while playing=%v: toggles %v, want %v", c.cmd, c.playing, got, c.want)
		}
	}
}
