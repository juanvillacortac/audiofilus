package ui

import (
	"image"
	"image/color"
	"testing"
)

// Un logo apaisado se encaja entero (no se recorta como una portada): su
// borde izquierdo se ve, y arriba y abajo queda transparente.
func TestScaleLogoFits(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.NRGBA{200, 200, 200, 255}
			if x < 4 {
				c = color.NRGBA{255, 0, 0, 255} // el borde izquierdo, rojo
			}
			src.SetNRGBA(x, y, c)
		}
	}
	img := scaleLogo(src, 20)
	if r, g, _, a := img.At(0, 10).RGBA(); a != 0xffff || r < 0x8000 || g > 0x8000 {
		t.Errorf("left edge %v, want the logo's red (not cropped)", img.At(0, 10))
	}
	if _, _, _, a := img.At(10, 1).RGBA(); a != 0 {
		t.Errorf("above a wide logo %v, want transparent", img.At(10, 1))
	}
}

// Un logo oscuro con transparencia (letras negras) va sobre un fondo claro;
// uno claro se queda transparente.
func TestScaleLogoDarkBackdrop(t *testing.T) {
	logo := func(c color.NRGBA) *image.RGBA {
		src := image.NewNRGBA(image.Rect(0, 0, 20, 20))
		for y := 5; y < 15; y++ {
			for x := 5; x < 15; x++ {
				src.SetNRGBA(x, y, c)
			}
		}
		return scaleLogo(src, 20)
	}
	if r, _, _, a := logo(color.NRGBA{10, 10, 10, 255}).At(1, 1).RGBA(); a != 0xffff || r < 0xc000 {
		t.Error("a dark logo has no light backdrop")
	}
	if _, _, _, a := logo(color.NRGBA{250, 250, 250, 255}).At(1, 1).RGBA(); a != 0 {
		t.Error("a light logo got a backdrop")
	}
}
