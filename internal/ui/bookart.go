package ui

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Lo que se ve en el reproductor con un audiolibro: un casete en vez del
// vinilo que asoma tras la portada (con los carretes girando mientras suena
// y la cinta pasando de uno a otro según lo escuchado) y, si el libro no
// tiene portada, el icono del libro en su lugar.

// cassetteSpin es cuántas veces más rápido que el vinilo giran los carretes.
const cassetteSpin = 4

// drawCassette pinta un casete centrado en cx, cy, de ancho 2r (como el
// vinilo de radio r), con los carretes girados angle y la cinta repartida
// según progress (0..1). Lo que lo hace un casete (y no un disquete): los dos
// carretes grandes en una ventana ancha, la cinta entre ellos, la parte de
// abajo en trapecio con sus agujeros y los tornillos de las esquinas.
func drawCassette(dst *ebiten.Image, cx, cy, r int, angle, progress float64, th Theme) {
	w, h := 2*r, int(float64(2*r)*0.63)
	x, y := cx-w/2, cy-h/2
	body := color.RGBA{0x2c, 0x2a, 0x32, 0xff}
	edge := color.RGBA{0x46, 0x43, 0x4e, 0xff}
	// El cuerpo, con las esquinas redondeadas a un pixel.
	rect(dst, x+2, y, w-4, h, body)
	rect(dst, x, y+2, w, h-4, body)
	rect(dst, x+1, y+1, w-2, h-2, body)
	rect(dst, x+2, y, w-4, 1, edge)
	// La etiqueta: clara, con una franja del color del disco arriba.
	lx, ly, lw, lh := x+w*7/100, y+h*8/100, w-w*14/100, h*62/100
	paper := color.RGBA{0xe8, 0xe2, 0xd4, 0xff}
	rect(dst, lx, ly, lw, lh, paper)
	rect(dst, lx, ly+lh/9, lw, max(2, lh/7), th.Accent)
	// La ventana, ancha y oscura, en el centro de la etiqueta.
	wx, wy, ww, wh := x+w*20/100, ly+lh*34/100, w*60/100, lh*60/100
	rect(dst, wx+1, wy, ww-2, wh, body)
	rect(dst, wx, wy+1, ww, wh-2, body)
	// Los carretes: a los lados de la ventana, grandes, con la cinta
	// enrollada (la de la izquierda se gasta y la otra crece).
	reel := float32(wh) * 0.46
	mid := float32(wy) + float32(wh)/2
	tape := color.RGBA{0x6b, 0x45, 0x2c, 0xff}
	hub := color.RGBA{0xf2, 0xf0, 0xea, 0xff}
	left := float32(wx) + reel + 1
	right := float32(wx+ww) - reel - 1
	// La cinta entre los dos carretes, abajo.
	vector.FillRect(dst, left, mid+reel*0.55, right-left, 1.5, tape, false)
	for i, c := range []float32{left, right} {
		share := 1 - progress
		if i == 1 {
			share = progress
		}
		vector.FillCircle(dst, c, mid, reel*float32(0.62+0.38*share), tape, false)
		vector.FillCircle(dst, c, mid, reel*0.58, hub, false)
		vector.FillCircle(dst, c, mid, reel*0.34, body, false)
		// Los tres dientes del carrete, grandes para que se vea girar. Los
		// carretes de un casete van más rápido que un vinilo.
		for k := 0; k < 3; k++ {
			a := cassetteSpin*angle + float64(k)*2*math.Pi/3
			cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
			for d := float32(0.08); d <= 0.34; d += 0.06 {
				vector.FillRect(dst, c+cs*reel*d-1, mid+sn*reel*d-1, 2, 2, hub, false)
			}
		}
	}
	// Abajo, el trapecio con sus dos agujeros grandes y dos chicos.
	tw, th2 := w*64/100, h*20/100
	tx, ty := x+(w-tw)/2, y+h-th2
	for row := 0; row < th2; row++ {
		in := (th2 - row) * w / 100 / 2 // se estrecha hacia arriba
		rect(dst, tx+in, ty+row, tw-2*in, 1, edge)
	}
	for _, f := range []float64{0.30, 0.70} {
		vector.FillCircle(dst, float32(x)+float32(w)*float32(f), float32(ty+th2/2+1), float32(max(1, th2/4)), body, false)
	}
	for _, f := range []float64{0.43, 0.57} {
		rect(dst, x+int(float64(w)*f)-1, ty+th2/2, 2, 2, body)
	}
	// Los tornillos de las esquinas.
	for _, p := range [][2]int{{x + 3, y + 3}, {x + w - 5, y + 3}, {x + 3, y + h - 5}, {x + w - 5, y + h - 5}} {
		rect(dst, p[0], p[1], 2, 2, edge)
	}
}

// drawBookArt pinta el icono del libro en el cuadro de la portada
// (size×size), para los audiolibros sin portada: igual que la nota de las
// canciones sin portada (drawCover), en el centro y en el color tenue.
func drawBookArt(dst *ebiten.Image, x, y, size int, th Theme) {
	panel(dst, x, y, size, size, th.Bg2)
	if size >= 20 {
		drawIcon(dst, "book", x+size/2-4, y+size/2-4, th.Dim)
	}
}
