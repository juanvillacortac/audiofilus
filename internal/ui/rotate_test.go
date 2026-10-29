package ui

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

// Con la interfaz girada, cada botón de la cruceta y de la derecha se lee del
// que queda en esa posición en la mano: con la consola girada a la izquierda
// arriba es el de la derecha y aceptar (abajo) el de la izquierda; a la
// derecha, al revés. Los gatillos y Start no giran.
func TestRotatedButtons(t *testing.T) {
	const (
		up, right, down, left     = ebiten.StandardGamepadButtonLeftTop, ebiten.StandardGamepadButtonLeftRight, ebiten.StandardGamepadButtonLeftBottom, ebiten.StandardGamepadButtonLeftLeft
		fTop, fRight, fDown, fLft = ebiten.StandardGamepadButtonRightTop, ebiten.StandardGamepadButtonRightRight, ebiten.StandardGamepadButtonRightBottom, ebiten.StandardGamepadButtonRightLeft
		l1, start                 = ebiten.StandardGamepadButtonFrontTopLeft, ebiten.StandardGamepadButtonCenterRight
	)
	for _, c := range []struct {
		rot  int
		want map[ebiten.StandardGamepadButton]ebiten.StandardGamepadButton
	}{
		{0, map[ebiten.StandardGamepadButton]ebiten.StandardGamepadButton{up: up, fDown: fDown, l1: l1}},
		{rotLeft, map[ebiten.StandardGamepadButton]ebiten.StandardGamepadButton{
			up: right, right: down, down: left, left: up, fTop: fRight, fRight: fDown, fDown: fLft, fLft: fTop, l1: l1, start: start}},
		{rotRight, map[ebiten.StandardGamepadButton]ebiten.StandardGamepadButton{
			up: left, right: up, down: right, left: down, fTop: fLft, fRight: fTop, fDown: fRight, fLft: fDown, l1: l1, start: start}},
	} {
		in := &Input{rot: c.rot}
		for b, want := range c.want {
			if got := in.physical(b); got != want {
				t.Errorf("rot %d: button %d reads %d, want %d", c.rot, b, got, want)
			}
		}
	}
}

// El lienzo vertical girado llena el alto de la pantalla, centrado, con su
// parte de arriba a la derecha (consola girada a la izquierda) o a la
// izquierda (a la derecha).
func TestRotatedGeoM(t *testing.T) {
	const sw, sh, ow, oh = 1280, 720, 240, 427
	for _, c := range []struct {
		rot        int
		topX, topY float64 // dónde cae la esquina de arriba a la izquierda del lienzo
	}{
		{rotLeft, 1280, 0},
		{rotRight, 0, 720},
	} {
		g := rotatedGeoM(c.rot, sw, sh, ow, oh)
		x, y := g.Apply(0, 0)
		if math.Abs(x-c.topX) > 1 || math.Abs(y-c.topY) > 1 {
			t.Errorf("rot %d: canvas corner at (%.1f, %.1f), want (%.1f, %.1f)", c.rot, x, y, c.topX, c.topY)
		}
		// La otra esquina: el lienzo entero cabe en la pantalla.
		x2, y2 := g.Apply(ow, oh)
		for _, v := range []float64{x, x2} {
			if v < -1e-6 || v > sw+1e-6 {
				t.Errorf("rot %d: x %.1f off screen", c.rot, v)
			}
		}
		for _, v := range []float64{y, y2} {
			if v < -1e-6 || v > sh+1e-6 {
				t.Errorf("rot %d: y %.1f off screen", c.rot, v)
			}
		}
	}
}
