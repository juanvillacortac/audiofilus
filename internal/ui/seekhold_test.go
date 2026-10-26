package ui

import "testing"

// Mantener una flecha: el primer salto a los 0,3 s, el segundo 0,6 s después,
// y luego cada vez más seguido hasta el tope de 0,2 s (no más rápido). Soltar
// antes de 0,3 s cambia de canción, y después de saltar, no.
func TestSeekHoldAccelerates(t *testing.T) {
	a := &App{offline: true, duration: 600}
	var at []int // los frames en que saltó
	frame := 0
	sendHook = func(cmd string, args []string) {
		switch cmd {
		case "seek":
			at = append(at, frame)
		case "next":
			at = append(at, -1)
		}
	}
	defer func() { sendHook = nil }()
	var h holdState
	for frame = 1; frame <= 300; frame++ { // 5 s mantenida
		a.tapOrHold(&h, frame, "next", 10)
	}
	a.tapOrHold(&h, 0, "next", 10) // soltada: no cambia de canción
	if len(at) < 3 || at[0] != holdFrames || at[1]-at[0] != seekFirst {
		t.Fatalf("seeks at %v", at)
	}
	for i := 1; i < len(at); i++ {
		gap := at[i] - at[i-1]
		if gap < seekEvery || i > 1 && gap > at[i-1]-at[i-2] || at[i] < 0 {
			t.Fatalf("seeks at %v: gap %d", at, gap)
		}
	}
	if last := at[len(at)-1] - at[len(at)-2]; last != seekEvery {
		t.Errorf("top speed gap %d, want %d", last, seekEvery)
	}
	at = nil
	for frame = 1; frame < holdFrames; frame++ { // un toque
		a.tapOrHold(&h, frame, "next", 10)
	}
	a.tapOrHold(&h, 0, "next", 10)
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
