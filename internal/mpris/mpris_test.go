package mpris

import "testing"

// TestFileURL comprueba que xesam:url sea una URI válida aunque la ruta
// lleve caracteres que en una URL significan otra cosa (#, %, ?, espacios):
// sin escaparlos, "Disc #1" cortaba la URL en un fragmento.
func TestFileURL(t *testing.T) {
	got := fileURL("/music/Disc #1/100% ¿Qué?.flac")
	want := "file:///music/Disc%20%231/100%25%20%C2%BFQu%C3%A9%3F.flac"
	if got != want {
		t.Errorf("fileURL = %q, want %q", got, want)
	}
}

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
