package ui

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/juanvillacortac/audiofilus/internal/ui/assets"
)

// Las dos fuentes, como en Trancazo: Jersey 10 para títulos y Tiny5 para el
// resto. Las dos son pixel art y se usan a su tamaño nativo, para que cada
// píxel del diseño de la letra sea uno del lienzo. No tienen japonés ni chino:
// lo que les falta sale de Fusion Pixel 8px (también pixel art, OFL), a 8 px
// en el texto y al doble en los títulos (a 12 px queda fina al lado de
// Jersey). Hay dos variantes, porque el japonés y el chino dibujan distinto
// los mismos caracteres: va primero la del idioma de la interfaz (la china en
// chino, la japonesa en los demás) y la otra detrás. Así los kanji de un
// título salen en cualquier idioma.
var (
	// BigFace es Jersey 10: rejilla de 75 unidades sobre 1400, así que a
	// 1400/75 px cada píxel de la letra cae en uno del lienzo. Mayúsculas de
	// 10 px, empiezan 5 px bajo la parte de arriba de la línea, y la línea mide 20.
	BigFace text.Face
	// SmallFace es Tiny5 a 8 px: mayúsculas de 5 px, empiezan 2 px bajo la parte
	// de arriba, y la línea mide 9.
	SmallFace text.Face

	// Las fuentes sueltas, para armar BigFace y SmallFace según el idioma
	// (setFontOrder): las de base y las de Fusion Pixel ([0] japonesa, [1]
	// china), si cargaron.
	bigBase, smallBase text.Face
	cjkBig, cjkSmall   [2]text.Face
)

// cjkFonts son los archivos de Fusion Pixel 8px, comprimidos (sin
// comprimir pesan 3,7 MB cada uno).
var cjkFonts = [2]string{"fonts/FusionPixel8-ja.ttf.gz", "fonts/FusionPixel8-zh_hans.ttf.gz"}

const (
	bigCapTop   = 5
	bigCap      = 10
	smallCapTop = 2
	smallCap    = 5
)

// cjkLoaded trae las fuentes de Fusion Pixel cuando terminan de cargar en
// segundo plano (descomprimir y leer 7 MB cuesta en las portátiles): mientras
// tanto, durante el splash, se usan las de base solas.
var cjkLoaded chan [2][2]text.Face

// LoadFonts carga las fuentes de base y empieza a cargar las de Fusion Pixel
// en segundo plano (llegan con pollFonts). Si Fusion Pixel no carga, sigue sin
// ella (el japonés y el chino no se ven).
func LoadFonts() error {
	big, err := loadFace("fonts/Jersey10-Regular.ttf", 1400.0/75, nil)
	if err != nil {
		return err
	}
	small, err := loadFace("fonts/Tiny5-Regular.ttf", 8, nil)
	if err != nil {
		return err
	}
	bigBase, smallBase = text.NewGoXFace(big), text.NewGoXFace(small)
	cjkBig, cjkSmall = [2]text.Face{}, [2]text.Face{}
	setFontOrder()
	cjkLoaded = make(chan [2][2]text.Face, 1)
	go func(ch chan [2][2]text.Face) {
		var out [2][2]text.Face // [variante][grande, pequeña]
		for i, name := range cjkFonts {
			f, err := parseFont(name)
			if err != nil {
				continue
			}
			b, err1 := newFace(f, name, 16, big)
			s, err2 := newFace(f, name, 8, small)
			if err1 == nil && err2 == nil {
				out[i] = [2]text.Face{text.NewGoXFace(b), text.NewGoXFace(s)}
			}
		}
		ch <- out
		ebiten.ScheduleFrame()
	}(cjkLoaded)
	return nil
}

// pollFonts pone las de Fusion Pixel en cuanto llegan (desde el hilo de la
// interfaz, que es el que dibuja con ellas). Con wait, las espera.
func pollFonts(wait bool) {
	if cjkLoaded == nil {
		return
	}
	var out [2][2]text.Face
	if wait {
		out = <-cjkLoaded
	} else {
		select {
		case out = <-cjkLoaded:
		default:
			return
		}
	}
	cjkLoaded = nil
	for i := range out {
		cjkBig[i], cjkSmall[i] = out[i][0], out[i][1]
	}
	setFontOrder()
}

// setFontOrder arma BigFace y SmallFace para el idioma en uso: la base y
// detrás las de Fusion Pixel, primero la del idioma.
func setFontOrder() {
	if smallBase == nil {
		return // aún no se cargaron (SetLanguage va antes que LoadFonts)
	}
	order := [2]int{0, 1}
	if lang == "zh" {
		order = [2]int{1, 0}
	}
	build := func(base text.Face, cjk [2]text.Face) text.Face {
		faces := []text.Face{base}
		for _, i := range order {
			if cjk[i] != nil {
				faces = append(faces, cjk[i])
			}
		}
		if len(faces) == 1 {
			return base
		}
		m, err := text.NewMultiFace(faces...)
		if err != nil {
			return base
		}
		return m
	}
	BigFace, SmallFace = build(bigBase, cjkBig), build(smallBase, cjkSmall)
}

// parseFont lee una fuente de los assets (descomprimiéndola si es .gz).
func parseFont(name string) (*opentype.Font, error) {
	data, err := assets.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if data, err = io.ReadAll(zr); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

// loadFace carga una fuente a px píxeles.
func loadFace(name string, px float64, like font.Face) (font.Face, error) {
	f, err := parseFont(name)
	if err != nil {
		return nil, err
	}
	return newFace(f, name, px, like)
}

// newFace es f a px píxeles, nítida. Con like, dice tener sus medidas: las
// de Fusion Pixel dicen tener las de Tiny5 o Jersey, porque la mezcla
// (MultiFace) toma la más alta de todas y el texto latino bajaría.
func newFace(f *opentype.Font, name string, px float64, like font.Face) (font.Face, error) {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if like != nil {
		return crispFace{metricsFace{face, like.Metrics()}}, nil
	}
	return crispFace{face}, nil
}

// metricsFace es una fuente con las medidas de otra.
type metricsFace struct {
	font.Face
	m font.Metrics
}

func (f metricsFace) Metrics() font.Metrics { return f.m }

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
