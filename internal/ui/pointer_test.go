package ui

import "testing"

// TestPointerTapAndDrag comprueba los gestos sin ratón de verdad: un toque
// sin moverse hace lo de la zona de encima (la última registrada), un
// arrastre hacia arriba de dos pasos son dos ↓ (el contenido sigue al dedo)
// y uno que se suelta rápido sigue un poco por inercia.
func TestPointerTapAndDrag(t *testing.T) {
	a := &App{offline: true, t: 1}
	var hit string
	a.beginZones()
	a.tapZone(0, 0, 100, 100, func() { hit = "back" })
	a.tapZone(10, 10, 20, 20, func() { hit = "row" })
	a.endZones()

	a.pointerDown(15, 15, true, 0)
	a.pointerUp()
	if hit != "row" {
		t.Errorf("tap hit %q, want the topmost zone %q", hit, "row")
	}

	// Arrastre lento hacia arriba: 2 pasos de 14 px.
	a.pointerDown(50, 80, true, 0)
	for y := 80; y >= 80-2*defaultStep; y -= 2 {
		a.pointerMove(50, y, 1.0/60)
	}
	if a.ptr.pending != 2 {
		t.Errorf("dragging up %d px gave %d steps, want 2", 2*defaultStep, a.ptr.pending)
	}
	a.ptr.vel = 0
	a.pointerUp()
	if a.ptr.flingAx != 0 {
		t.Error("a slow drag kept moving after release")
	}

	// Un arrastre rápido sigue por inercia.
	a.ptr.pending = 0
	a.pointerDown(50, 80, true, 0)
	for y := 80; y >= 40; y -= 10 {
		a.pointerMove(50, y, 1.0/60)
	}
	a.pointerUp()
	if a.ptr.flingAx != 2 || a.ptr.flingVel <= 0 {
		t.Errorf("a fast flick has no fling (axis %d, vel %.0f)", a.ptr.flingAx, a.ptr.flingVel)
	}
}

// TestScrollerFollowsFinger comprueba el desplazamiento libre de una lista:
// el dedo la mueve píxel a píxel sin cambiar la elegida, no se pasa de los
// bordes, y en cuanto la elegida cambia por la cruceta la vista la sigue.
func TestScrollerFollowsFinger(t *testing.T) {
	var s scroller
	sel := 0
	first, off := s.place(sel, 10, 100, 13)
	if first != 0 || off != 0 {
		t.Fatalf("start at %d+%d", first, off)
	}
	s.by(13*5 + 4) // cinco filas y un poco
	if first, off = s.place(sel, 10, 100, 13); first != 5 || off != 4 {
		t.Errorf("after dragging: first %d off %d, want 5 and 4", first, off)
	}
	s.by(1e6)
	if first, _ = s.place(sel, 10, 100, 13); first != 90 {
		t.Errorf("dragged past the end: first %d, want 90", first)
	}
	sel = 50 // la cruceta
	if first, off = s.place(sel, 10, 100, 13); first != 45 || off != 0 {
		t.Errorf("after the D-pad: first %d off %d, want 45 and 0 (centered on 50)", first, off)
	}
}

// TestSwipeInGames comprueba que en un juego deslizar es una sola pulsación
// hacia donde va el dedo (no una ola de pasos, que en Cripta eran MISS).
func TestSwipeInGames(t *testing.T) {
	a := &App{offline: true, t: 1, screen: Crypt}
	a.pointerDown(100, 100, true, 0)
	for x := 100; x <= 200; x += 5 {
		a.pointerMove(x, 100, 1.0/60)
	}
	if !a.input.virt[ActRight] || a.ptr.pending != 0 {
		t.Errorf("swipe right: right %v, pending %d", a.input.virt[ActRight], a.ptr.pending)
	}
	a.pointerUp()
	if a.ptr.flingAx != 0 {
		t.Error("a swipe in a game kept moving after release")
	}
}
