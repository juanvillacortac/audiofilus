package ui

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Los kanji, kana y hanzi que no tienen Tiny5 ni Jersey salen de Fusion
// Pixel, sin cambiar las medidas de la línea (el texto latino no se mueve).
func TestCJKFallback(t *testing.T) {
	start := time.Now()
	if err := LoadFonts(); err != nil {
		t.Fatal(err)
	}
	if w := text.Advance("初恋", SmallFace); w != text.Advance("初恋", smallBase) {
		t.Errorf("Fusion Pixel was in use before loading in the background")
	}
	pollFonts(true)
	t.Logf("fonts loaded in %v", time.Since(start))
	for _, f := range []struct {
		name       string
		face, base text.Face
	}{{"small", SmallFace, smallBase}, {"big", BigFace, bigBase}} {
		if f.face.Metrics() != f.base.Metrics() {
			t.Errorf("%s: metrics %+v, want the base font's %+v", f.name, f.face.Metrics(), f.base.Metrics())
		}
		for _, s := range []string{"初恋", "ヒカル", "设置"} {
			if w := text.Advance(s, f.face); w <= 0 {
				t.Errorf("%s: %q has no width", f.name, s)
			}
		}
	}
}
