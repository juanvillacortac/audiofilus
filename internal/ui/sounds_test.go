package ui

import (
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/sfx"
)

// En Reproduciendo, mantener una flecha no repite el sonido de moverse (no
// mueve nada: arriba y abajo son el scratch); en la biblioteca, sí suena.
func TestNowPlayingArrowsSilent(t *testing.T) {
	var heard []string
	soundHook = func(name string) { heard = append(heard, name) }
	defer func() { soundHook = nil }()
	for _, c := range []struct {
		screen Screen
		want   bool
	}{{NowPlaying, false}, {LibraryScreen, true}} {
		heard = nil
		a := &App{offline: true, screen: c.screen}
		for _, act := range []Action{ActUp, ActDown, ActLeft, ActRight} {
			a.input = Input{}
			a.input.inject(act)
			a.soundFromInput()
		}
		moved := false
		for _, n := range heard {
			moved = moved || n == sfx.Move
		}
		if moved != c.want {
			t.Errorf("screen %v: move sound %v, want %v (%v)", c.screen, moved, c.want, heard)
		}
	}
}
