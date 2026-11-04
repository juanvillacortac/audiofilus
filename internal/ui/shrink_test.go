package ui

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// TestShrinkFor comprueba que la reducción previa promedia bloques enteros
// (con la transparencia premultiplicada) y que no toca una imagen que ya es
// pequeña.
func TestShrinkFor(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			c := color.NRGBA{255, 0, 0, 255}
			if x%2 == 1 {
				c = color.NRGBA{0, 0, 255, 0} // transparente: no debe teñir
			}
			src.SetNRGBA(x, y, c)
		}
	}
	out, r := shrinkFor(src, src.Bounds(), 50, 50)
	if r.Dx() != 100 || r.Dy() != 100 {
		t.Fatalf("shrunk to %v, want 100×100 (factor 4)", r)
	}
	got := out.(*image.RGBA).RGBAAt(10, 10)
	if got.R < 125 || got.R > 130 || got.B != 0 || got.A < 125 || got.A > 130 {
		t.Errorf("averaged pixel %v, want half-transparent pure red", got)
	}
	small := image.NewRGBA(image.Rect(0, 0, 120, 120))
	if o, r := shrinkFor(small, small.Bounds(), 96, 96); o != image.Image(small) || r != small.Bounds() {
		t.Errorf("image under twice the size was shrunk")
	}
}

// TestCoverStale comprueba que una imagen que se pide solo mientras está a
// la vista (passing) no se prepara si nadie la pidió hace rato, y que las
// demás sí.
func TestCoverStale(t *testing.T) {
	c := &covers{asked: map[coverJob]time.Time{}}
	passing := coverJob{&Album{passing: true}, 96}
	other := coverJob{&Album{}, 96}
	c.asked[passing] = time.Now().Add(-time.Second)
	if !c.stale(passing) {
		t.Errorf("passing image asked a second ago is not stale")
	}
	c.asked[passing] = time.Now()
	if c.stale(passing) {
		t.Errorf("passing image asked just now is stale")
	}
	if c.stale(other) {
		t.Errorf("a regular cover was skipped")
	}
}
