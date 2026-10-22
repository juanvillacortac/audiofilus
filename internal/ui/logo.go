package ui

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// El logo, con estética de los reproductores de los 2000: a la izquierda un
// CD brillante (borde cromado, surcos, eje y un destello que gira) y a la
// derecha "audiofilus" en letras cromadas: cursiva por píxeles, contorno
// oscuro, relieve hacia abajo a la derecha y un degradado de cromo (cielo
// claro arriba, una línea de horizonte oscura y el reflejo abajo). Encima, un
// brillo que barre las letras, destellos de estrella que parpadean y un
// subrayado curvo. Todo en píxeles enteros con los colores del tema, y se
// amplía al doble (splashScale).

const (
	logoH     = 40 // alto del logo sin ampliar
	logoDisc  = 30 // ancho del hueco del CD
	logoTextY = 14 // la parte de arriba de las letras, ya en su sitio
	logoSlant = 3  // cuántos píxeles se inclinan las letras
)

var (
	logoMask   *ebiten.Image // las letras en blanco, ya inclinadas
	logoCanvas *ebiten.Image // el logo entero, antes de ampliarlo
	logoDiscIm *ebiten.Image // el CD, que se pinta píxel a píxel
	logoDiscPx []byte
)

// splashLogoSize es el tamaño del logo sin ampliar.
func splashLogoSize() (int, int) {
	return logoDisc + 2 + logoWordW() + logoSlant + 5, logoH
}

// logoWordW es el ancho de las letras del logo: en negrita (un píxel más
// cada una) y separadas un píxel más, para que no se toquen.
func logoWordW() int {
	return textWidth(splashWord, BigFace) + 2*len([]rune(splashWord))
}

// chromeColor es el color del cromo en la fila o de las letras (0 es la
// parte de arriba de las mayúsculas, 9 la de abajo).
func chromeColor(o int, th Theme) color.RGBA {
	hi := th.Text
	switch {
	case o <= 3:
		return hi // las minúsculas empiezan en la fila 3: arriba, blanco
	case o == 4:
		return mix(hi, th.Accent2, 0.45)
	case o == 5:
		return th.Accent2
	case o == 6:
		return mix(th.Accent2, th.Accent, 0.6) // el horizonte, suave
	case o == 7:
		return th.Accent
	case o == 8:
		return mix(th.Accent, hi, 0.3)
	default:
		return mix(th.Accent, hi, 0.6) // el reflejo de abajo
	}
}

// drawLogo pinta el logo al doble en (x, y) en el instante t del splash:
// antes de 1,1 s las letras caen con rebote; después ondulan, y hacia 1,3 s
// un brillo las cruza.
func drawLogo(dst *ebiten.Image, x, y int, t float64) { drawLogoAt(dst, x, y, t, splashScale) }

// drawLogoAt es drawLogo ampliado a la escala entera scale (1 donde el logo
// al doble no cabe: una pantalla en vertical).
func drawLogoAt(dst *ebiten.Image, x, y int, t float64, scale int) {
	th := DefaultTheme
	lw, lh := splashLogoSize()
	if logoCanvas == nil || logoCanvas.Bounds().Dx() != lw {
		logoCanvas = newTarget(lw, lh)
		logoMask = newTarget(lw, lh)
	}
	logoCanvas.Clear()
	logoMask.Clear()

	// Las letras, en blanco, en la máscara: cada fila de cada letra se
	// desplaza para inclinarla (cursiva sin suavizado).
	x0 := logoDisc + 2
	letters := []rune(splashWord)
	for i, r := range letters {
		start := 0.08 + float64(i)*0.07
		p := (t - start) / 0.5
		if p <= 0 {
			continue
		}
		ly := float64(logoTextY) - (1-easeOutBounce(math.Min(p, 1)))*30
		if t > 1.1 {
			ly += math.Round(math.Sin(t*4-float64(i)*0.7) * 1.2)
		}
		lx := x0 + textWidth(string(letters[:i]), BigFace) + 2*i
		top := int(ly)
		for row := 0; row < bigCap; row++ {
			yy := top + row
			if yy < 0 || yy >= lh {
				continue
			}
			sub := logoMask.SubImage(image.Rect(0, yy, lw, yy+1)).(*ebiten.Image)
			sx := lx + (bigCap-1-row)*logoSlant/(bigCap-1)
			drawText(sub, string(r), BigFace, sx, top, color.White)
			drawText(sub, string(r), BigFace, sx+1, top, color.White) // negrita
		}
	}

	// Relieve, contorno y cromo, todos a partir de la máscara.
	stamp := func(dx, dy int, c color.Color) {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(dx), float64(dy))
		op.ColorScale.ScaleWithColor(c)
		logoCanvas.DrawImage(logoMask, op)
	}
	deep := mix(th.Shadow, th.Accent, 0.3)
	stamp(3, 3, th.Shadow)
	stamp(2, 2, deep)
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		stamp(d[0], d[1], th.Shadow)
	}
	for yy := 0; yy < lh; yy++ {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(0, float64(yy))
		op.ColorScale.ScaleWithColor(chromeColor(yy-logoTextY, th))
		logoCanvas.DrawImage(logoMask.SubImage(image.Rect(0, yy, lw, yy+1)).(*ebiten.Image), op)
	}
	// El brillo: una franja diagonal blanca que cruza las letras.
	if p := (t - 1.25) / 0.55; p > 0 && p < 1 {
		sx := x0 - 8 + int(p*float64(lw-x0+16))
		for yy := logoTextY - 2; yy < logoTextY+bigCap+2; yy++ {
			bx := sx + (logoTextY+bigCap-yy)/2
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(bx), float64(yy))
			logoCanvas.DrawImage(logoMask.SubImage(image.Rect(bx, yy, bx+3, yy+1)).(*ebiten.Image), op)
		}
	}

	// El subrayado curvo, que aparece con las letras.
	if t > 0.9 {
		ww := logoWordW()
		n := int(float64(ww) * math.Min(1, (t-0.9)/0.35))
		for i := 0; i < n; i++ {
			f := float64(i) / float64(ww)
			yy := logoTextY + bigCap + 6 + int(math.Round(-2*math.Sin(f*math.Pi)))
			c := withAlpha(mix(th.Accent, th.Accent2, f), uint8(255*math.Min(1, 4*math.Min(f, 1-f)+0.2)))
			rect(logoCanvas, x0+i, yy, 1, 1, c)
		}
	}

	// El CD.
	if p := t / 0.45; p > 0 {
		drawLogoDisc(logoCanvas, logoDisc/2, logoTextY+bigCap/2, easeOutBack(math.Min(1, p)), t, th)
	}

	// Los destellos: estrellas de cuatro puntas que crecen y se apagan.
	if t > 1.0 {
		ww := logoWordW()
		for i, s := range [][3]float64{{0.97, -5, 0}, {0.2, -4, 1.3}, {0.62, 13, 2.1}, {-0.05, 16, 0.7}} {
			ph := math.Mod(t*1.6+s[2], 2.2)
			size := []int{0, 1, 2, 1, 0}[min(4, int(ph*3))]
			if size == 0 {
				continue
			}
			sx := x0 + int(s[0]*float64(ww))
			sy := logoTextY + int(s[1])
			if i == 3 {
				sx = 2 // junto al CD
			}
			c := th.Text
			rect(logoCanvas, sx-size, sy, 2*size+1, 1, c)
			rect(logoCanvas, sx, sy-size, 1, 2*size+1, c)
		}
	}

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(scale), float64(scale))
	op.GeoM.Translate(float64(x), float64(y))
	dst.DrawImage(logoCanvas, op)
}

// drawLogoDisc pinta el CD centrado en (cx, cy), a la escala k (0..1, con el
// rebote de la entrada), con el destello girando según t.
func drawLogoDisc(dst *ebiten.Image, cx, cy int, k, t float64, th Theme) {
	const size = logoDisc
	if logoDiscIm == nil {
		logoDiscIm = ebiten.NewImage(size, size)
		logoDiscPx = make([]byte, 4*size*size)
	}
	r := 13.0 * k
	shine := t * 2.4
	set := func(i int, c color.RGBA) {
		logoDiscPx[i], logoDiscPx[i+1], logoDiscPx[i+2], logoDiscPx[i+3] = c.R, c.G, c.B, c.A
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			i := 4 * (py*size + px)
			set(i, color.RGBA{})
			dx, dy := float64(px)-size/2+0.5, float64(py)-size/2+0.5
			d := math.Hypot(dx, dy)
			switch {
			case d > r+0.5:
				// La sombra, abajo a la derecha.
				if math.Hypot(dx-2, dy-2) <= r+0.5 {
					set(i, th.Shadow)
				}
				continue
			case d > r-0.6:
				set(i, th.Shadow) // el contorno
				continue
			case d > r-2:
				// El borde cromado: claro arriba, oscuro abajo.
				set(i, mix(th.Text, th.Dim, (dy/r+1)/2))
				continue
			case d < 2:
				set(i, th.Shadow) // el agujero
				continue
			case d < 4.5:
				set(i, mix(th.Bg2, th.Text, 0.35)) // el eje
				if d > 3.6 {
					set(i, mix(th.Bg2, th.Text, 0.15))
				}
				continue
			}
			// La cara: del segundo acento al primero, con surcos.
			c := mix(th.Accent2, th.Accent, (dy/r+1)/2)
			if int(d)%3 == 0 {
				c = mix(c, th.Shadow, 0.25)
			}
			// El brillo de arriba a la izquierda.
			if dx+dy < -r*0.55 && d > r*0.55 {
				c = mix(c, th.Text, 0.45)
			}
			// El destello que gira: dos cuñas opuestas.
			an := math.Atan2(dy, dx)
			if w := math.Abs(math.Remainder(an-shine, math.Pi)); w < 0.22 {
				c = mix(c, th.Text, 0.6*(1-w/0.22))
			}
			set(i, c)
		}
	}
	logoDiscIm.WritePixels(logoDiscPx)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(cx-size/2), float64(cy-size/2))
	dst.DrawImage(logoDiscIm, op)
}
