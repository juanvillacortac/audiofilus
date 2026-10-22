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
// tiene portada, un librito en su lugar.

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

// drawBookArt pinta un libro de tapa dura en el cuadro de la portada
// (size×size), para los audiolibros sin portada: la tapa del color del disco
// con su marco y la placa del título, el lomo con sus nervios, el canto de
// las hojas, la cinta de marcapáginas y su sombra; todo con contorno, como
// pixel art.
func drawBookArt(dst *ebiten.Image, x, y, size int, th Theme) {
	panel(dst, x, y, size, size, th.Bg2)
	u := max(1, size/48) // un "pixel" del dibujo
	bw, bh := size*50/100, size*68/100
	bx, by := x+(size-bw)/2+u*2, y+(size-bh)/2-u*2
	spine, pages := size*9/100, max(2, size*4/100)
	outline := mix(th.Shadow, th.Bg, 0.3)
	cover := th.Accent
	dark := mix(th.Accent, th.Shadow, 0.45)
	light := mix(th.Accent, th.Text, 0.35)
	paper := color.RGBA{0xee, 0xe6, 0xd2, 0xff}
	paperLine := mix(paper, th.Shadow, 0.25)

	// La sombra, abajo a la derecha.
	rect(dst, bx-spine+u*2, by+bh+pages, bw+spine+pages, u*2, withAlpha(th.Shadow, 0x90))
	// El canto de las hojas: a la derecha y abajo, con rayas.
	rect(dst, bx+u, by+pages, bw+pages-u, bh, paper)
	for k := by + pages + u*2; k < by+bh+pages-u; k += u * 2 {
		rect(dst, bx+bw, k, pages-u, max(1, u/2), paperLine)
	}
	for k := bx + u*2; k < bx+bw; k += u * 3 {
		rect(dst, k, by+bh, max(1, u/2), pages-u, paperLine)
	}
	// La tapa, con su contorno, el brillo arriba y a la izquierda.
	rect(dst, bx-u, by-u, bw+u*2, bh+u*2, outline)
	rect(dst, bx, by, bw, bh, cover)
	rect(dst, bx, by, bw, u, light)
	rect(dst, bx, by, u, bh, light)
	// El marco grabado y la placa clara del título, con dos líneas.
	fm := size * 6 / 100
	frame := mix(cover, th.Shadow, 0.3)
	rect(dst, bx+fm, by+fm, bw-2*fm, u, frame)
	rect(dst, bx+fm, by+bh-fm-u, bw-2*fm, u, frame)
	rect(dst, bx+fm, by+fm, u, bh-2*fm, frame)
	rect(dst, bx+bw-fm-u, by+fm, u, bh-2*fm, frame)
	px, py, pw, ph := bx+fm*2, by+bh*22/100, bw-fm*4, bh*22/100
	rect(dst, px-u, py-u, pw+u*2, ph+u*2, outline)
	rect(dst, px, py, pw, ph, paper)
	rect(dst, px+pw/8, py+ph/3, pw*3/4, max(1, u), mix(th.Shadow, paper, 0.2))
	rect(dst, px+pw/4, py+ph*2/3, pw/2, max(1, u), mix(th.Shadow, paper, 0.4))
	// Un adorno debajo: un rombo.
	cx, cy, d := bx+bw/2, by+bh*66/100, max(2, size/18)
	for k := 0; k <= d; k++ {
		rect(dst, cx-k, cy-d+k, 2*k+1, 1, light)
		rect(dst, cx-k, cy+d-k, 2*k+1, 1, light)
	}
	// El lomo, más oscuro, con tres nervios.
	rect(dst, bx-spine-u, by-u, spine+u, bh+u*2, outline)
	rect(dst, bx-spine, by, spine, bh, dark)
	for _, f := range []int{18, 50, 82} {
		ry := by + bh*f/100
		rect(dst, bx-spine, ry, spine, u*2, mix(dark, th.Text, 0.25))
		rect(dst, bx-spine, ry+u*2, spine, u, outline)
	}
	// La cinta de marcapáginas, colgando por abajo.
	rw := max(2, size/22)
	rx := bx + bw*70/100
	rect(dst, rx, by+bh, rw, size*9/100, th.Accent2)
	rect(dst, rx, by+bh+size*9/100, rw/2, u, th.Accent2) // la punta en V
}
