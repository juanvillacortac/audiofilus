package ui

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/juanvillacortac/audiofilus/internal/ui/assets"
)

// Las dos fuentes, como en Trancazo: Jersey 10 para títulos y Tiny5 para el
// resto. Las dos son pixel art y se usan a su tamaño nativo, para que cada
// píxel del diseño de la letra sea uno del lienzo.
var (
	// BigFace es Jersey 10: rejilla de 75 unidades sobre 1400, así que a
	// 1400/75 px cada píxel de la letra cae en uno del lienzo. Mayúsculas de
	// 10 px, empiezan 5 px bajo la parte de arriba de la línea, y la línea mide 20.
	BigFace text.Face
	// SmallFace es Tiny5 a 8 px: mayúsculas de 5 px, empiezan 2 px bajo la parte
	// de arriba, y la línea mide 9.
	SmallFace text.Face
)

const (
	bigCapTop   = 5
	bigCap      = 10
	smallCapTop = 2
	smallCap    = 5
)

// LoadFonts carga las dos fuentes.
func LoadFonts() error {
	var err error
	if BigFace, err = loadFace("fonts/Jersey10-Regular.ttf", 1400.0/75); err != nil {
		return err
	}
	SmallFace, err = loadFace("fonts/Tiny5-Regular.ttf", 8)
	return err
}

func loadFace(name string, px float64) (text.Face, error) {
	data, err := assets.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return text.NewGoXFace(crispFace{face}), nil
}

// crispFace deja cada píxel de cada letra encendido o apagado: el rasterizador
// suaviza los bordes aunque la rejilla caiga justa, y a 240 px de alto eso se
// ve borroso. Es la misma idea que en Trancazo.
type crispFace struct{ font.Face }

func (c crispFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	dot.X, dot.Y = fixed.I(dot.X.Round()), fixed.I(dot.Y.Round())
	dr, mask, mp, adv, ok := c.Face.Glyph(dot, r)
	if !ok {
		return dr, mask, mp, adv, ok
	}
	out := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	for y := range dr.Dy() {
		for x := range dr.Dx() {
			if _, _, _, a := mask.At(mp.X+x, mp.Y+y).RGBA(); a >= 0x8000 {
				out.Pix[y*out.Stride+x] = 0xff
			}
		}
	}
	return dr, out, image.Point{}, adv, true
}

// drawText escribe s con la parte de arriba de sus mayúsculas en y.
func drawText(dst *ebiten.Image, s string, face text.Face, x, y int, c color.Color) {
	top := smallCapTop
	if face == BigFace {
		top = bigCapTop
	}
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y-top))
	op.ColorScale.ScaleWithColor(c)
	text.Draw(dst, s, face, op)
}

// drawTextShadow escribe s con una sombra de un píxel abajo a la derecha.
func drawTextShadow(dst *ebiten.Image, s string, face text.Face, x, y int, c, shadow color.Color) {
	drawText(dst, s, face, x+1, y+1, shadow)
	drawText(dst, s, face, x, y, c)
}

// textWidth es el ancho de s en píxeles.
func textWidth(s string, face text.Face) int {
	return int(text.Advance(s, face) + 0.5)
}

// fadeW es el ancho del fundido de los textos que no caben (ocho escalones
// de 3 píxeles).
const fadeW = 24

// scratch es un lienzo de paso para pintar un texto y luego desvanecer sus
// bordes. Se reutiliza en cada texto: Ebitengine ejecuta los dibujos en orden.
var scratch *ebiten.Image

// drawTextW escribe s en un ancho w. Si no cabe, en vez de cortarlo con "..."
// se desvanece hacia la derecha con trama.
func drawTextW(dst *ebiten.Image, s string, face text.Face, x, y, w int, c color.Color) {
	if w <= 0 {
		return
	}
	if textWidth(s, face) <= w {
		drawText(dst, s, face, x, y, c)
		return
	}
	drawFaded(dst, s, face, x, y, w, 0, false, true, c, nil)
}

// drawFaded escribe s desplazado off píxeles a la izquierda dentro de un hueco
// de w, con los bordes que se pidan desvanecidos y, si shadow no es nil, con
// sombra.
func drawFaded(dst *ebiten.Image, s string, face text.Face, x, y, w, off int, left, right bool, c, shadow color.Color) {
	if w <= 0 {
		return
	}
	h := 24
	if scratch == nil {
		scratch = ebiten.NewImage(640, h)
	}
	w = min(w, scratch.Bounds().Dx())
	top := smallCapTop
	if face == BigFace {
		top = bigCapTop
	}
	region := scratch.SubImage(image.Rect(0, 0, w, h)).(*ebiten.Image)
	region.Clear()
	// El texto se pinta con su parte de arriba en y=top+1 del lienzo de paso.
	if shadow != nil {
		drawText(region, s, face, -off+1, top+2, shadow)
	}
	drawText(region, s, face, -off, top+1, c)
	op := &ebiten.DrawRectShaderOptions{}
	op.GeoM.Translate(float64(x), float64(y-top-1))
	op.Images[0] = region
	fl, fr := float32(0), float32(0)
	if left {
		fl = fadeW
	}
	if right {
		fr = fadeW
	}
	op.Uniforms = map[string]any{"Width": float32(w), "FadeL": fl, "FadeR": fr}
	dst.DrawRectShader(w, h, fadeShader, op)
}

// fitKey identifica un recorte ya calculado.
type fitKey struct {
	s    string
	face text.Face
	w    int
}

// fitCache guarda los recortes: medir letra a letra cada título en cada frame
// daba tirones al desplazarse por las listas.
var fitCache = map[fitKey]string{}

// fit recorta s con "..." para que quepa en w píxeles.
func fit(s string, face text.Face, w int) string {
	k := fitKey{s, face, w}
	if r, ok := fitCache[k]; ok {
		return r
	}
	out := s
	if textWidth(s, face) > w {
		r := []rune(s)
		// Búsqueda binaria del largo que cabe.
		lo, hi := 0, len(r)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if textWidth(string(r[:mid])+"...", face) <= w {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		out = string(r[:lo]) + "..."
	}
	if len(fitCache) > 4096 {
		clear(fitCache)
	}
	fitCache[k] = out
	return out
}
