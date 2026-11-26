package ui

import (
	"math"
	"testing"
)

// BenchmarkTapePaint mide lo que se pinta en la CPU al cambiar el aspecto
// del casete (newTapeLayers sin subir a la GPU), al ancho de 3:2.
func BenchmarkTapePaint(b *testing.B) {
	const w = 183
	p := int(math.Ceil(w*0.08)) + 2
	m := tapeMap{float32(p), w}
	cw, ch := w+2*p, int(math.Round(tapeH*w))+2*p
	t := tapeInks{m, tapeNeon}
	for b.Loop() {
		shell, window, over := newTapeCanvas(cw, ch), newTapeCanvas(cw, ch), newTapeCanvas(cw, ch)
		light, leaks := newTapeCanvas(cw, ch), newTapeCanvas(cw, ch)
		paintShell(shell, light, leaks, t, nil)
		paintWindow(window, light, t)
		paintOver(over, t)
		g, s := newTapeCanvas(cw, ch), newTapeCanvas(cw, ch)
		for _, r := range []int{max(1, int(math.Round(w*0.008))), max(2, int(math.Round(w*0.02)))} {
			light.bloom(g, r, 0.9)
			leaks.bloom(s, r, 0.9)
		}
		mask := newTapeCanvas(cw, ch)
		mask.paint(tapeLabelForm(m), flat(tapeWhite, 1))
		paintReel(m.d(tapeHubR), m.d(tapeHoleR))
		paintDot()
	}
}
