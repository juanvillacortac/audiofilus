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
