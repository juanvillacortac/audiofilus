// Package ui es la interfaz pixel art de audiofilus, en Ebitengine.
//
// Como en Trancazo, todo se dibuja en un lienzo de ScreenH píxeles de alto y
// un ancho que sigue la ventana (ViewW): 240 en 1:1, 320 en 4:3, 360 en 3:2,
// 384 en 16:10 y 427 en 16:9. Así las portátiles escalan por un entero
// (640×480 = 320×240 ×2, 720×480 = 360×240 ×2, 720×720 = 240×240 ×3) y lo
// esencial se diseña para que quepa en el cuadrado central de 240×240.
package ui

import (
	"image/color"
	"math"
)

const (
	// ScreenH es el alto lógico en todos los formatos.
	ScreenH = 240
	// El ancho sale de la proporción de la ventana (ViewW), cualquiera entre
	// minW, 1:1 (las portátiles cuadradas), y maxW, 21:9 (los móviles y
	// pantallas más alargados; lo más ancho, como 32:9, se queda en eso con
	// bandas), como en Trancazo.
	minW = 240 // 1:1
	maxW = 560 // 21:9
)

// ViewW es el ancho lógico para una ventana de w×h.
func ViewW(w, h int) int {
	if w <= 0 || h <= 0 {
		return 320
	}
	v := int(math.Round(float64(w) * ScreenH / float64(h)))
	return max(minW, min(maxW, v))
}

// Aspect es un formato de pantalla con nombre, para las capturas.
type Aspect struct {
	Name string
	W    int
}

// Aspects son los formatos de las capturas de las maquetas.
var Aspects = []Aspect{{"1x1", 240}, {"4x3", 320}, {"3x2", 360}, {"16x10", 384}, {"16x9", 427}, {"2x1", 480}, {"21x9", 560}}

// Theme son los colores de la interfaz. El acento se toma de la portada que
// suena, así cada disco tiñe la interfaz (como hace Songo con sus temas).
type Theme struct {
	Bg      color.RGBA // fondo, casi negro
	Bg2     color.RGBA // fondo de los paneles
	Text    color.RGBA
	Dim     color.RGBA // texto secundario
	Accent  color.RGBA // color del disco
	Accent2 color.RGBA // un segundo tono, para degradados
	Shadow  color.RGBA
}

// DefaultTheme se usa hasta que hay una portada.
var DefaultTheme = Theme{
	Bg:      color.RGBA{0x0d, 0x0b, 0x17, 0xff},
	Bg2:     color.RGBA{0x1a, 0x16, 0x2b, 0xff},
	Text:    color.RGBA{0xf1, 0xec, 0xff, 0xff},
	Dim:     color.RGBA{0x8f, 0x87, 0xad, 0xff},
	Accent:  color.RGBA{0x4f, 0xe3, 0xc1, 0xff},
	Accent2: color.RGBA{0xff, 0x5f, 0xa2, 0xff},
	Shadow:  color.RGBA{0x05, 0x04, 0x0a, 0xff},
}

// ThemeFor devuelve el tema teñido con los colores de una portada.
func ThemeFor(accent, accent2 color.RGBA) Theme {
	t := DefaultTheme
	t.Accent, t.Accent2 = accent, accent2
	// El fondo lleva un poco del color del disco.
	t.Bg = mix(DefaultTheme.Bg, accent, 0.10)
	t.Bg2 = mix(DefaultTheme.Bg2, accent, 0.14)
	return t
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + 0.5) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 0xff}
}

func withAlpha(c color.RGBA, a uint8) color.RGBA {
	f := float64(a) / 255
	return color.RGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), a}
}
