package ui

import (
	"bytes"
	"compress/gzip"
	"image"
	"image/color"
	"io"
	"log"
	"sync"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/go-text/typesetting/di"
	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/juanvillacortac/audiofilus/internal/ui/assets"
)

// El hindi (devanagari) y el árabe no se pueden escribir letra por letra
// como los demás alfabetos: el devanagari junta consonantes (स + ् + ट = स्ट)
// y pone algunas vocales delante de su consonante (ि), y el árabe une cada
// letra con sus vecinas (cambia de forma según dónde cae en la palabra) y se
// escribe de derecha a izquierda. Ninguna fuente pixel art lo hace, así que
// esos tramos se forman con HarfBuzz (go-text, el mismo que usa Ebitengine)
// sobre Noto Sans Devanagari y Noto Kufi Arabic (la cúfica, geométrica, es la
// que mejor queda en píxeles), y cada glifo se dibuja con píxeles duros como
// las demás fuentes. El resto del texto sigue con las fuentes de siempre.
//
// La dirección: el texto se parte en tramos de izquierda a derecha y de
// derecha a izquierda (una versión mínima del algoritmo bidi de Unicode: los
// signos neutros, como los espacios, van con sus vecinos si los dos van
// igual, y si no con el párrafo), y los tramos se ordenan según el párrafo,
// que es de derecha a izquierda si su primera letra lo es o si la interfaz
// está en árabe.

// complexFont es una de las fuentes de estos alfabetos, a un tamaño.
type complexFont struct {
	gf *gofont.Face // para formar el texto
	sf *sfnt.Font   // para dibujar sus glifos
	px int
	sc language.Script

	mu     sync.Mutex
	glyphs map[gofont.GID]*complexGlyph
}

// complexGlyph es un glifo ya dibujado: su máscara y dónde va respecto al
// punto de la línea base.
type complexGlyph struct {
	mask *image.Alpha
	off  image.Point
}

// Los tamaños: en el texto chico, lo justo para que se lea (el cuerpo de las
// letras queda como el de Tiny5 con sus marcas encima y debajo); en los
// títulos, en el peso medio, como el bloque de Jersey.
const (
	devaSmallPx, devaBigPx = 13, 17
	arabSmallPx, arabBigPx = 12, 16
)

// complexFonts son las cuatro fuentes ([0] texto chico, [1] títulos), que se
// cargan la primera vez que hacen falta.
var (
	complexOnce      sync.Once
	devaFonts, arabs [2]*complexFont
)

func loadComplexFonts() {
	load := func(name string, px int, sc language.Script) *complexFont {
		data, err := assets.FS.ReadFile(name)
		if err == nil && bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
			var zr *gzip.Reader
			if zr, err = gzip.NewReader(bytes.NewReader(data)); err == nil {
				data, err = io.ReadAll(zr)
			}
		}
		if err != nil {
			log.Printf("fonts: %s: %v", name, err)
			return nil
		}
		gf, err := gofont.ParseTTF(bytes.NewReader(data))
		if err != nil {
			log.Printf("fonts: %s: %v", name, err)
			return nil
		}
		sf, err := sfnt.Parse(data)
		if err != nil {
			log.Printf("fonts: %s: %v", name, err)
			return nil
		}
		return &complexFont{gf: gf, sf: sf, px: px, sc: sc, glyphs: map[gofont.GID]*complexGlyph{}}
	}
	devaFonts = [2]*complexFont{
		load("fonts/NotoSansDevanagari-Regular.ttf.gz", devaSmallPx, language.Devanagari),
		load("fonts/NotoSansDevanagari-Medium.ttf.gz", devaBigPx, language.Devanagari),
	}
	arabs = [2]*complexFont{
		load("fonts/NotoKufiArabic-Regular.ttf.gz", arabSmallPx, language.Arabic),
		load("fonts/NotoKufiArabic-Medium.ttf.gz", arabBigPx, language.Arabic),
	}
}

// isArabic e isDevanagari dicen de qué alfabeto es r.
func isArabic(r rune) bool {
	return r >= 0x0600 && r <= 0x06FF || r >= 0x0750 && r <= 0x077F || r >= 0x08A0 && r <= 0x08FF ||
		r >= 0xFB50 && r <= 0xFDFF || r >= 0xFE70 && r <= 0xFEFF
}

func isDevanagari(r rune) bool {
	return r >= 0x0900 && r <= 0x097F || r >= 0xA8E0 && r <= 0xA8FF || r >= 0x1CD0 && r <= 0x1CFF
}

// isRTL dice si r se escribe de derecha a izquierda (el hebreo también,
// aunque sus letras salgan de la fuente de base, si las tiene).
func isRTL(r rune) bool { return isArabic(r) || r >= 0x0590 && r <= 0x05FF }

// needsShaping dice si s tiene algo que formar o de derecha a izquierda.
func needsShaping(s string) bool {
	for _, r := range s {
		if r >= 0x0590 && (isRTL(r) || isDevanagari(r)) {
			return true
		}
	}
	return false
}

// dirClass es la dirección de una letra: 'L', 'R' o neutra ('N': espacios,
// signos...). Las cifras van de izquierda a derecha.
func dirClass(r rune) byte {
	switch {
	case isRTL(r):
		return 'R'
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return 'L'
	}
	return 'N'
}

// bidiRun es un tramo de una sola dirección, en el orden del texto.
type bidiRun struct {
	s   []rune
	rtl bool
}

// bidiRuns parte s en tramos y los devuelve en el orden en que se ven, de
// izquierda a derecha.
func bidiRuns(s string) []bidiRun {
	rs := []rune(s)
	cls := make([]byte, len(rs))
	paraRTL := lang == "ar"
	first := true
	for i, r := range rs {
		c := dirClass(r)
		// Las marcas (vocales, virama...) van con su letra.
		if unicode.Is(unicode.Mn, r) || r == 0x200C || r == 0x200D {
			if i > 0 {
				c = cls[i-1]
			}
		}
		cls[i] = c
		if first && c != 'N' {
			paraRTL = paraRTL || c == 'R'
			first = false
		}
	}
	para := byte('L')
	if paraRTL {
		para = 'R'
	}
	// Los neutros: con sus vecinos si van igual, si no con el párrafo.
	for i := 0; i < len(cls); {
		if cls[i] != 'N' {
			i++
			continue
		}
		j := i
		for j < len(cls) && cls[j] == 'N' {
			j++
		}
		prev, next := para, para
		if i > 0 {
			prev = cls[i-1]
		}
		if j < len(cls) {
			next = cls[j]
		}
		d := para
		if prev == next {
			d = prev
		}
		for k := i; k < j; k++ {
			cls[k] = d
		}
		i = j
	}
	var runs []bidiRun
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && cls[j] == cls[i] {
			j++
		}
		runs = append(runs, bidiRun{s: rs[i:j], rtl: cls[i] == 'R'})
		i = j
	}
	if paraRTL {
		for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
			runs[i], runs[j] = runs[j], runs[i]
		}
	}
	return runs
}

// mirror son los signos que se ven al revés de derecha a izquierda.
var mirror = map[rune]rune{'(': ')', ')': '(', '[': ']', ']': '[', '{': '}', '}': '{', '<': '>', '>': '<', '«': '»', '»': '«'}

// piece es un trozo de una línea ya formada: texto de la fuente de base, o
// un glifo de las de estos alfabetos. x es dónde empieza, desde la izquierda.
type piece struct {
	x     int
	text  string
	glyph *complexGlyph
}

// shapedLine es una línea con hindi o árabe, ya formada.
type shapedLine struct {
	w      int
	pieces []piece
	img    *ebiten.Image // la línea en blanco, hecha al dibujarla
	pad    int           // margen arriba de img para las marcas altas
}

// shapedCache guarda las líneas formadas (cada texto de la interfaz se pinta
// en cada frame). Se vacía entera cuando se llena.
var (
	shapedMu    sync.Mutex
	shapedCache = map[shapedKey]*shapedLine{}
)

type shapedKey struct {
	s   string
	big bool
}

const shapedMax = 256

// shapeLine forma s para face (los títulos si es BigFace).
func shapeLine(s string, face text.Face) *shapedLine {
	big := face == BigFace
	shapedMu.Lock()
	defer shapedMu.Unlock()
	if l, ok := shapedCache[shapedKey{s, big}]; ok {
		return l
	}
	if len(shapedCache) >= shapedMax {
		for _, l := range shapedCache {
			if l.img != nil {
				l.img.Deallocate()
			}
		}
		clear(shapedCache)
	}
	complexOnce.Do(loadComplexFonts)
	size := 0
	if big {
		size = 1
	}
	l := &shapedLine{}
	pen := 0
	addText := func(rs []rune) {
		if len(rs) == 0 {
			return
		}
		t := string(rs)
		l.pieces = append(l.pieces, piece{x: pen, text: t})
		pen += textAdvance(t, face)
	}
	addShaped := func(rs []rune, f *complexFont, rtl bool) {
		if f == nil {
			addText(rs)
			return
		}
		pen += f.shape(rs, rtl, pen, &l.pieces)
	}
	for _, run := range bidiRuns(s) {
		if !run.rtl {
			// De izquierda a derecha: los tramos de devanagari se forman,
			// lo demás va con la fuente de base.
			for i := 0; i < len(run.s); {
				j := i
				deva := isDevanagari(run.s[i])
				for j < len(run.s) && (isDevanagari(run.s[j]) || j > i && deva && isJoiner(run.s[j])) == deva {
					j++
				}
				if deva {
					addShaped(run.s[i:j], devaFonts[size], false)
				} else {
					addText(run.s[i:j])
				}
				i = j
			}
			continue
		}
		// De derecha a izquierda: los trozos van al revés; las palabras
		// árabes se forman enteras (HarfBuzz ya las da en orden visual), y
		// los signos sueltos, de uno en uno y en espejo.
		var segs [][]rune
		for i := 0; i < len(run.s); {
			j := i + 1
			if isArabic(run.s[i]) {
				for j < len(run.s) && (isArabic(run.s[j]) || isJoiner(run.s[j]) || unicode.Is(unicode.Mn, run.s[j])) {
					j++
				}
			}
			segs = append(segs, run.s[i:j])
			i = j
		}
		for k := len(segs) - 1; k >= 0; k-- {
			seg := segs[k]
			switch {
			case isArabic(seg[0]):
				addShaped(seg, arabs[size], true)
			case len(seg) == 1 && mirror[seg[0]] != 0:
				addText([]rune{mirror[seg[0]]})
			default:
				addText(seg)
			}
		}
	}
	l.w = pen
	shapedCache[shapedKey{s, big}] = l
	return l
}

// isJoiner son los que unen o separan letras sin verse (ZWNJ, ZWJ).
func isJoiner(r rune) bool { return r == 0x200C || r == 0x200D }

// textAdvance es el ancho de s en face, en píxeles enteros.
func textAdvance(s string, face text.Face) int { return int(text.Advance(s, face) + 0.5) }

// shape forma rs con f, apunta sus glifos en pieces desde x y devuelve el
// ancho.
func (f *complexFont) shape(rs []rune, rtl bool, x int, pieces *[]piece) int {
	dir := di.DirectionLTR
	if rtl {
		dir = di.DirectionRTL
	}
	out := (&shaping.HarfbuzzShaper{}).Shape(shaping.Input{
		Text: rs, RunEnd: len(rs), Direction: dir, Face: f.gf, Size: fixed.I(f.px), Script: f.sc,
	})
	pen := fixed.I(0)
	for _, g := range out.Glyphs {
		if cg := f.glyph(g.GlyphID); cg != nil {
			gx := (pen + g.XOffset).Round()
			gy := -g.YOffset.Round()
			*pieces = append(*pieces, piece{x: x + gx, glyph: &complexGlyph{mask: cg.mask, off: cg.off.Add(image.Pt(0, gy))}})
		}
		pen += g.Advance
	}
	return pen.Round()
}

// glyph dibuja el glifo gid de f con píxeles duros (o lo da de la caché).
func (f *complexFont) glyph(gid gofont.GID) *complexGlyph {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.glyphs[gid]; ok {
		return g
	}
	var buf sfnt.Buffer
	segs, err := f.sf.LoadGlyph(&buf, sfnt.GlyphIndex(gid), fixed.I(f.px), nil)
	var g *complexGlyph
	if err == nil && len(segs) > 0 {
		b := segs.Bounds()
		x0, y0 := b.Min.X.Floor()-1, b.Min.Y.Floor()-1
		w, h := b.Max.X.Ceil()-x0+1, b.Max.Y.Ceil()-y0+1
		ras := vector.NewRasterizer(w, h)
		pt := func(p fixed.Point26_6) (float32, float32) {
			return float32(p.X)/64 - float32(x0), float32(p.Y)/64 - float32(y0)
		}
		for _, s := range segs {
			switch s.Op {
			case sfnt.SegmentOpMoveTo:
				ras.MoveTo(pt(s.Args[0]))
			case sfnt.SegmentOpLineTo:
				ras.LineTo(pt(s.Args[0]))
			case sfnt.SegmentOpQuadTo:
				ax, ay := pt(s.Args[0])
				bx, by := pt(s.Args[1])
				ras.QuadTo(ax, ay, bx, by)
			case sfnt.SegmentOpCubeTo:
				ax, ay := pt(s.Args[0])
				bx, by := pt(s.Args[1])
				cx, cy := pt(s.Args[2])
				ras.CubeTo(ax, ay, bx, by, cx, cy)
			}
		}
		soft := image.NewAlpha(image.Rect(0, 0, w, h))
		ras.Draw(soft, soft.Bounds(), image.Opaque, image.Point{})
		// Píxeles duros, como crispFace.
		for i, a := range soft.Pix {
			if a >= 0x80 {
				soft.Pix[i] = 0xff
			} else {
				soft.Pix[i] = 0
			}
		}
		g = &complexGlyph{mask: soft, off: image.Pt(x0, y0)}
	}
	f.glyphs[gid] = g
	return g
}

// image dibuja la línea en blanco (una vez): los trozos de texto con la
// fuente de base y los glifos en su sitio. La parte de arriba de la línea
// (donde text.Draw empieza) queda a pad píxeles del borde.
func (l *shapedLine) image(face text.Face) *ebiten.Image {
	if l.img != nil {
		return l.img
	}
	m := face.Metrics()
	ascent := int(m.HAscent + 0.5)
	l.pad = 10
	if face == BigFace {
		l.pad = 14
	}
	h := int(m.HAscent+m.HDescent+0.5) + 2*l.pad
	w := max(l.w+2, 1)
	glyphs := image.NewRGBA(image.Rect(0, 0, w, h))
	white := image.NewUniform(color.White)
	any := false
	for _, p := range l.pieces {
		if p.glyph == nil {
			continue
		}
		at := image.Pt(p.x, l.pad+ascent).Add(p.glyph.off)
		r := p.glyph.mask.Bounds().Add(at)
		drawMask(glyphs, r, white, p.glyph.mask)
		any = true
	}
	img := ebiten.NewImage(w, h)
	if any {
		img.DrawImage(ebiten.NewImageFromImage(glyphs), nil)
	}
	for _, p := range l.pieces {
		if p.glyph != nil {
			continue
		}
		op := &text.DrawOptions{}
		op.GeoM.Translate(float64(p.x), float64(l.pad))
		text.Draw(img, p.text, face, op)
	}
	l.img = img
	return img
}

// drawMask enciende en dst, dentro de r, los píxeles de mask con src.
func drawMask(dst *image.RGBA, r image.Rectangle, src image.Image, mask *image.Alpha) {
	mb := mask.Bounds()
	for y := 0; y < mb.Dy(); y++ {
		for x := 0; x < mb.Dx(); x++ {
			if mask.AlphaAt(mb.Min.X+x, mb.Min.Y+y).A == 0 {
				continue
			}
			px, py := r.Min.X+x, r.Min.Y+y
			if image.Pt(px, py).In(dst.Bounds()) {
				dst.Set(px, py, src.At(0, 0))
			}
		}
	}
}
