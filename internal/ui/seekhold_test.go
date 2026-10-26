package ui

import (
	"strconv"
	"testing"
)

// Mantener una flecha: a los 0,3 s empieza a buscar, sin saltos y cada vez
// más rápido hasta el tope (50 s de canción por segundo, no más), mandando
// la posición cada 0,2 s y al soltar. Suena una vez. Soltar antes de 0,3 s
// cambia de canción, y después de buscar, no.
func TestSeekHoldAccelerates(t *testing.T) {
	a := &App{offline: true, duration: 600, position: 100}
	var at []int      // los frames en que mandó la posición
	var pos []float64 // y cuál
	frame := 0
	sendHook = func(cmd string, args []string) {
		switch cmd {
		case "seek":
			at = append(at, frame)
			v, _ := strconv.ParseFloat(args[0], 64)
			pos = append(pos, v)
		case "next":
			at = append(at, -1)
		}
	}
	defer func() { sendHook = nil }()
	var h holdState
	sounds := 0
	for frame = 1; frame <= 300; frame++ { // 5 s mantenida
		a.tapOrHold(&h, frame, "next", 1)
		if a.snd.pending != "" {
			sounds++
			a.snd.pending, a.snd.prio = "", -1
		}
		if frame < holdFrames && a.position != 100 {
			t.Fatalf("moved before holding: %v", a.position)
		}
	}
	frame = 301
	a.tapOrHold(&h, 0, "next", 1) // soltada: no cambia de canción
	if sounds != 1 {
		t.Errorf("%d sounds while holding, want 1", sounds)
	}
	if len(at) < 3 || at[0] != holdFrames || at[len(at)-1] != 301 {
		t.Fatalf("seeks at %v", at)
	}
	for i := 1; i < len(at)-1; i++ {
		if at[i]-at[i-1] != seekEvery {
			t.Fatalf("seeks at %v", at)
		}
	}
	if first := pos[1] - pos[0]; first > 1 {
		t.Errorf("first 0.2 s moved %.2f s, want 1 s or less", first)
	}
	for i := 2; i < len(pos)-1; i++ {
		step, prev := pos[i]-pos[i-1], pos[i-1]-pos[i-2]
		if step < prev-0.11 || step > scrubMax*seekEvery/60+0.11 {
			t.Fatalf("positions %v: step %.2f after %.2f", pos, step, prev)
		}
	}
	if last := pos[len(pos)-2] - pos[len(pos)-3]; last < scrubMax*seekEvery/60-0.11 {
		t.Errorf("top speed step %.2f, want %.0f", last, scrubMax*seekEvery/60)
	}
	at = nil
	for frame = 1; frame < holdFrames; frame++ { // un toque
		a.tapOrHold(&h, frame, "next", 1)
	}
	a.tapOrHold(&h, 0, "next", 1)
	if len(at) != 1 || at[0] != -1 {
		t.Errorf("a tap: %v", at)
	}
}

// Con el menú de Start o la barra de Select abiertos, L1/R1 siguen cambiando
// de canción y L2/R2 buscando (ninguno de los dos los usa).
func TestMusicKeysOverMenus(t *testing.T) {
	var sent []string
	sendHook = func(cmd string, args []string) { sent = append(sent, cmd) }
	defer func() { sendHook = nil }()
	for _, open := range []string{"menu", "dock"} {
		a := &App{offline: true, w: 320, h: 240, duration: 300, screen: LibraryScreen}
		if open == "menu" {
			a.menu = &menu{title: "x", items: []menuItem{{label: "a", do: func() {}}}}
		} else {
			a.dock.open = true
		}
		sent = nil
		for _, act := range []Action{ActNext, ActSeekFwd} {
			a.input = Input{}
			a.input.inject(act)
			if open == "menu" {
				a.overlayMusicKeys()
				a.updateMenu()
			} else {
				a.updateDock()
			}
		}
		if len(sent) != 2 || sent[0] != "next" || sent[1] != "seek" {
			t.Errorf("with the %s open: %v", open, sent)
		}
		if open == "menu" && a.menu == nil || open == "dock" && !a.dock.open {
			t.Errorf("the %s closed", open)
		}
	}
}
