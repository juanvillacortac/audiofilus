package ui

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// iconArt son los iconos, dibujados a mano ('#' encendido). Casi todos son de
// 7×7; el de GitHub (para la firma del splash) es de 15×15.
var iconArt = map[string]string{
	"play": `
.#.....
.##....
.###...
.####..
.###...
.##....
.#.....`,
	"pause": `
.##.##.
.##.##.
.##.##.
.##.##.
.##.##.
.##.##.
.##.##.`,
	"next": `
#...#..
##..#..
###.#..
#####..
###.#..
##..#..
#...#..`,
	"prev": `
..#...#
..#..##
..#.###
..#####
..#.###
..#..##
..#...#`,
	"shuffle": `
.....#.
##..###
..##.#.
...#...
..##.#.
##..###
.....#.`,
	"repeat": `
.####..
#....#.
#...###
#....#.
.#....#
###...#
.#....#`,
	"speaker": `
...#..#
..##...
####.#.
####.#.
####.#.
..##...
...#..#`,
	"github": `
.....#####.....
...#########...
..###########..
.###.#####.###.
.###..###..###.
###.........###
##...........##
##...........##
##...........##
###.........###
.###.......###.
.#.###...#####.
..#......####..
...###...###...
.....##.##.....`,
	"folder": `
.......
###....
#..####
#.....#
#.....#
#######
.......`,
	"note": `
...###.
...#.#.
...#.#.
...#.#.
.###.#.
####...
.##....`,
	"search": `
.###...
#...#..
#...#..
#...#..
.###...
....##.
.....##`,
	"moon": `
..###..
.##....
##.....
##.....
##.....
.##...#
..####.`,
	"screen": `
#######
#.....#
#.###.#
#.###.#
#.....#
#######
..###..`,
	"clock": `
.#####.
#..#..#
#..#..#
#..###.
#.....#
#.....#
.#####.`,
	"globe": `
.#####.
#..#..#
#######
#..#..#
#######
#..#..#
.#####.`,
	"quiz": `
.#####.
##...##
.....##
...###.
...#...
.......
...#...`,
	"snake": `
.......
.####..
.#..#..
.#..###
.#....#
###.###
.......`,
	"ship": `
...#...
..###..
..#.#..
.#####.
##.#.##
#..#..#
.......`,
	"tracker": `
.#.#.#.
#######
#######
.######
#######
#######
.#.#.#.`,
	"cup": `
.......
#####..
#...###
#...#.#
#...###
.###...
#####..`,
	"sword": `
......#
.....##
....##.
#..##..
.###...
..#....
.#.#...`,
	"pad": `
.......
.#####.
#.#...#
###.#.#
#.#...#
.#####.
.......`,
	"bg": `
###....
#......
#.####.
#.#..#.
..#..#.
..####.
.......`,
	"power": `
...#...
.#.#.#.
#..#..#
#..#..#
#.....#
.#...#.
..###..`,
	"headphones": `
..###..
.#...#.
#.....#
#.....#
##...##
##...##
##...##`,
	"bluetooth": `
..#....
..##...
#.#.#..
.###...
#.#.#..
..##...
..#....`,
	"disc": `
..###..
.#...#.
#..#..#
#.#.#.#
#..#..#
.#...#.
..###..`,
	"user": `
..###..
..###..
..###..
.......
.#####.
#######
#######`,
	"list": `
#.#####
.......
#.#####
.......
#.#####
.......
#.#####`,
	"plus": `
...#...
...#...
...#...
#######
...#...
...#...
...#...`,
	"playnext": `
#......
##.....
###.###
####...
###.###
##.....
#..####`,
	// blocks es Bloques: el pozo, con una pieza cayendo y otras apiladas.
	"blocks": `
#..##..#
#..##..#
#......#
#......#
#.##...#
#.##.###
#.######
########`,
	// visual es el visualizador: un ecualizador de cuatro barras.
	"visual": `
....#..
..#.#..
..#.#.#
#.#.#.#
#.#.#.#
#.#.#.#
#.#.#.#`,
	"gear": `
..#.#..
.#####.
##...##
.#...#.
##...##
.#####.
..#.#..`,
	"arrow_l": `
...#
..##
.###
####
.###
..##
...#`,
	"arrow_d": `
#######
.#####.
..###..
...#...`,
	"car": `
.......
..###..
.#...#.
#######
#######
.#...#.
.......`,
	// bolt es el rayo de "cargando" junto a la batería.
	"bolt": `
..##
.##.
####
.##.
##..
#...
`,
	// gh es el gato de GitHub en pequeño, para la baldosa de Ajustes.
	"gh": `
.#...#.
.#####.
#.#.#.#
#######
.#####.
.#.#...
#..#...
`,
	// Flechas pequeñas para las pastillas de botones de la ayuda (en la
	// fuente, "<", ">" y "v" no se leían como flechas).
	"hint_l": `
..#
.##
###
.##
..#
`,
	"hint_r": `
#..
##.
###
##.
#..
`,
	"hint_u": `
..#..
.###.
#####
`,
	"hint_d": `
#####
.###.
..#..
`,
	"arrow_u": `
...#...
..###..
.#####.
#######`,
	"pencil": `
.....##
....###
...###.
..###..
.###...
##.....
#......`,
	"trash": `
..###..
#######
.#####.
.#.#.#.
.#.#.#.
.#.#.#.
.#####.`,
	// ytm marca los resultados de YouTube Music: un botón de play.
	"ytm": `
.#######.
###.#####
###..####
###...###
###..####
###.#####
.#######.`,
	"download": `
...#...
...#...
.#####.
..###..
...#...
#.....#
#######`,
	"check": `
.....#
....##
#..##.
####..
.##...`,
	// server es una biblioteca remota (Subsonic): un servidor de dos bandejas.
	"server": `
#######
#.#...#
#######
.......
#######
#.#...#
#######`,
	// radio es la radio por internet: un aparato con su antena y su altavoz.
	"radio": `
.....#.
....#..
#######
#.#...#
#.#.#.#
#.#...#
#######`,
	// mic son los podcasts: un micrófono.
	"mic": `
..###..
..###..
..###..
#.###.#
.#####.
...#...
.#####.`,
}

var icons = map[string]*ebiten.Image{}

func icon(name string) *ebiten.Image {
	if img, ok := icons[name]; ok {
		return img
	}
	rows := strings.Split(strings.TrimSpace(iconArt[name]), "\n")
	img := image.NewRGBA(image.Rect(0, 0, len(strings.TrimSpace(rows[0])), len(rows)))
	for y, row := range rows {
		for x, c := range strings.TrimSpace(row) {
			if c == '#' {
				img.Set(x, y, color.White)
			}
		}
	}
	e := ebiten.NewImageFromImage(img)
	icons[name] = e
	return e
}

// drawIcon dibuja un icono en (x, y) con el color c.
func drawIcon(dst *ebiten.Image, name string, x, y int, c color.Color) {
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(c)
	dst.DrawImage(icon(name), op)
}

// rect rellena un rectángulo sin suavizado.
func rect(dst *ebiten.Image, x, y, w, h int, c color.Color) {
	vector.FillRect(dst, float32(x), float32(y), float32(w), float32(h), c, false)
}

// frame dibuja el contorno de un rectángulo de un píxel.
func frame(dst *ebiten.Image, x, y, w, h int, c color.Color) {
	rect(dst, x, y, w, 1, c)
	rect(dst, x, y+h-1, w, 1, c)
	rect(dst, x, y, 1, h, c)
	rect(dst, x+w-1, y, 1, h, c)
}

// panel es un rectángulo con las esquinas recortadas un píxel: el "redondeo"
// del pixel art, que da el aire moderno de Songo sin perder los píxeles.
func panel(dst *ebiten.Image, x, y, w, h int, c color.Color) {
	rect(dst, x+1, y, w-2, h, c)
	rect(dst, x, y+1, 1, h-2, c)
	rect(dst, x+w-1, y+1, 1, h-2, c)
}

// marching dibuja un borde de trazos que avanzan con t (la selección).
func marching(dst *ebiten.Image, x, y, w, h int, t float64, c color.Color) {
	off := int(t*12) % 4
	n := 2*(w+h) - 4
	for i := 0; i < n; i++ {
		if (i+off)%4 >= 2 {
			continue
		}
		var px, py int
		switch {
		case i < w:
			px, py = x+i, y
		case i < w+h-1:
			px, py = x+w-1, y+i-w+1
		case i < 2*w+h-2:
			px, py = x+w-1-(i-w-h+2), y+h-1
		default:
			px, py = x, y+h-1-(i-2*w-h+3)
		}
		rect(dst, px, py, 1, 1, c)
	}
}

func sincos(a float64) (float64, float64) { return math.Sincos(a) }

// rectBatch junta muchos rectángulos en un solo DrawTriangles: cada rect()
// suelto es un dibujo para Ebitengine y el driver, y el visualizador de LEDs
// pinta cientos por fotograma (se llevaba la mitad de la CPU en la consola).
type rectBatch struct {
	v []ebiten.Vertex
	i []uint16
}

// batchWhite es la imagen blanca de la que salen los rectángulos del lote.
var batchWhite *ebiten.Image

// add añade un rectángulo de color c.
func (b *rectBatch) add(x, y, w, h int, c color.Color) {
	if w <= 0 || h <= 0 {
		return
	}
	r, g, bl, a := c.RGBA() // premultiplicado, de 0 a 0xffff
	cr, cg, cb, ca := float32(r)/0xffff, float32(g)/0xffff, float32(bl)/0xffff, float32(a)/0xffff
	n := uint16(len(b.v))
	for _, p := range [4][2]int{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
		b.v = append(b.v, ebiten.Vertex{DstX: float32(p[0]), DstY: float32(p[1]), SrcX: 1, SrcY: 1, ColorR: cr, ColorG: cg, ColorB: cb, ColorA: ca})
	}
	b.i = append(b.i, n, n+1, n+2, n, n+2, n+3)
	if len(b.v) > 60000 {
		panic("rectBatch: too many rectangles") // nunca deberían ser tantos
	}
}

// draw pinta el lote en dst y lo vacía.
func (b *rectBatch) draw(dst *ebiten.Image) {
	if len(b.i) == 0 {
		return
	}
	if batchWhite == nil {
		batchWhite = newWhite()
	}
	op := &ebiten.DrawTrianglesOptions{ColorScaleMode: ebiten.ColorScaleModePremultipliedAlpha}
	dst.DrawTriangles(b.v, b.i, batchWhite, op)
	b.v, b.i = b.v[:0], b.i[:0]
}

// panel es panel() en el lote.
func (b *rectBatch) panel(x, y, w, h int, c color.Color) {
	b.add(x+1, y, w-2, h, c)
	b.add(x, y+1, 1, h-2, c)
	b.add(x+w-1, y+1, 1, h-2, c)
}
