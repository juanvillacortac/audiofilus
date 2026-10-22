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
	// ScreenH es el alto lógico en horizontal (y el ancho en vertical: el lado
	// corto de la pantalla siempre mide 240).
	ScreenH = 240
	// maxH es lo más alto que llega el lienzo en vertical: 9:21 (lo más
	// alargado se queda en eso con bandas).
	maxH = 560
	// El ancho sale de la proporción de la ventana (ViewW), cualquiera entre
	// minW, 1:1 (las portátiles cuadradas), y maxW, 21:9 (los móviles y
	// pantallas más alargados; lo más ancho, como 32:9, se queda en eso con
	// bandas), como en Trancazo.
	minW = 240 // 1:1
	maxW = 560 // 21:9
)

// ViewSize es el lienzo lógico para una ventana de w×h, siempre en pixel art
// (se amplía sin suavizar): en horizontal (o cuadrada), 240 de alto y el ancho
// de la proporción (ViewW); en vertical (un móvil, una ventana estrecha), 240
// de ancho y el alto de la proporción, hasta maxH.
func ViewSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 || w >= h {
		return ViewW(w, h), ScreenH
	}
	return minW, min(maxH, int(math.Round(float64(h)*minW/float64(w))))
}

// ViewW es el ancho lógico para una ventana horizontal de w×h.
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
	W, H int
}

// Aspects son los formatos de las capturas de las maquetas: los horizontales
// de las portátiles y el escritorio, y los verticales de un móvil.
var Aspects = []Aspect{{"1x1", 240, 240}, {"4x3", 320, 240}, {"3x2", 360, 240}, {"16x10", 384, 240}, {"16x9", 427, 240},
	{"2x1", 480, 240}, {"21x9", 560, 240}, {"3x4", 240, 320}, {"9x16", 240, 427},
	{"iphone", 240, 520}} // un iPhone 15 (393×852 puntos)

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

// themeColor es un color fijo para la interfaz (Ajustes → Color): su nombre
// (la clave "tc_<nombre>" de lang) y sus dos tonos.
type themeColor struct {
	name            string
	accent, accent2 color.RGBA
	plain           bool // el fondo sin teñir y más negro (el de la carcasa negra)
}

// themeColors son los colores fijos que se pueden elegir, pensados para ir con
// las carcasas de las consolas (las de la RG34XX-SP: índigo, blanco con
// botones lila, amarillo con botones rojos, negro con los de colores, azul
// cobalto; las metálicas platino, grafito, azul y rosa perla; y el celeste de
// la GBA SP); el segundo tono es el de los botones.
// El primero ("album") es el de cada disco.
var themeColors = []themeColor{
	{name: "album"},
	{"red", color.RGBA{0xe8, 0x45, 0x3c, 0xff}, color.RGBA{0xf2, 0x99, 0x4a, 0xff}, false},
	{"orange", color.RGBA{0xf2, 0x8a, 0x2e, 0xff}, color.RGBA{0xf2, 0xc9, 0x4c, 0xff}, false},
	{"yellow", color.RGBA{0xf2, 0xc4, 0x18, 0xff}, color.RGBA{0xe0, 0x3a, 0x48, 0xff}, false},
	{"green", color.RGBA{0x4c, 0xc9, 0x6a, 0xff}, color.RGBA{0x2e, 0xc4, 0xb6, 0xff}, false},
	{"ice", color.RGBA{0x9c, 0xc8, 0xe0, 0xff}, color.RGBA{0xdc, 0xe9, 0xf2, 0xff}, false},
	{"blue", color.RGBA{0x34, 0x52, 0xd0, 0xff}, color.RGBA{0x8a, 0xa0, 0xf2, 0xff}, false},
	{"purple", color.RGBA{0x6a, 0x5a, 0xe0, 0xff}, color.RGBA{0xc8, 0xc6, 0xe0, 0xff}, false},
	{"pink", color.RGBA{0xf0, 0x60, 0x9e, 0xff}, color.RGBA{0xf2, 0x9a, 0x8a, 0xff}, false},
	{"pearlpink", color.RGBA{0xf0, 0xb8, 0xd0, 0xff}, color.RGBA{0xd4, 0xd2, 0xd8, 0xff}, false},
	{"white", color.RGBA{0xf4, 0xf4, 0xf6, 0xff}, color.RGBA{0x9a, 0x86, 0xec, 0xff}, false},
	{"platinum", color.RGBA{0xb4, 0xc0, 0xcc, 0xff}, color.RGBA{0x7a, 0x86, 0x96, 0xff}, false},
	{"graphite", color.RGBA{0x84, 0x88, 0x92, 0xff}, color.RGBA{0x5c, 0x5f, 0x68, 0xff}, false},
	{"black", color.RGBA{0xe8, 0xe8, 0xec, 0xff}, color.RGBA{0x4a, 0x4c, 0x54, 0xff}, true},
}

// fixedTheme es el tema del color fijo con ese nombre, si lo hay.
func fixedTheme(name string) (Theme, bool) {
	for _, c := range themeColors[1:] {
		if c.name != name {
			continue
		}
		t := ThemeFor(c.accent, c.accent2)
		if c.plain {
			t.Bg = color.RGBA{0x06, 0x06, 0x08, 0xff}
			t.Bg2 = color.RGBA{0x14, 0x14, 0x18, 0xff}
		}
		return t, true
	}
	return Theme{}, false
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
