package ui

import (
	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// El splash: el logo (logo.go) entra —el CD aparece con un rebote y las
// letras cromadas caen una a una—, luego las letras ondulan, un brillo las
// cruza, un ecualizador de LEDs late debajo y aparece la firma (el logo de
// GitHub y juanvillacortac). Al final se funde con el fondo por escalones.
// Mientras tanto la biblioteca se carga en segundo plano. Cualquier tecla o
// botón lo salta.

const (
	splashLen   = 2.8  // segundos que dura
	splashOut   = 0.32 // lo que dura el fundido final
	splashSkip  = 0.35 // a partir de cuándo se puede saltar
	splashWord  = "audiofilus"
	splashScale = 2 // el logo va al doble: es lo único con píxeles gordos
	splashBy    = "juanvillacortac"
)

// updateSplash avanza el splash; devuelve false cuando terminó.
func (a *App) updateSplash() bool {
	if a.splashT == 0 {
		a.playSound(sfx.Splash) // el jingle, a tiempo con el logo
	}
	a.splashT += 1.0 / 60
	if a.splashT > splashSkip && a.input.any && a.splashT < splashLen-splashOut {
		a.splashT = splashLen - splashOut // se salta: solo queda el fundido
		a.stopSplashSound()               // y el jingle se corta
	}
	if a.splashT >= splashLen {
		a.splashing = false
	}
	return a.splashing
}

// easeOutBounce es la caída con rebote (de 0 a 1).
func easeOutBounce(x float64) float64 {
	const n, d = 7.5625, 2.75
	switch {
	case x < 1/d:
		return n * x * x
	case x < 2/d:
		x -= 1.5 / d
		return n*x*x + 0.75
	case x < 2.5/d:
		x -= 2.25 / d
		return n*x*x + 0.9375
	default:
		x -= 2.625 / d
		return n*x*x + 0.984375
	}
}

// steps redondea t (0..1) a cuatro escalones: los fundidos del splash son
// por escalones, como el resto de la interfaz.
func steps(t float64) float32 {
	return float32(math.Floor(math.Max(0, math.Min(1, t))*4) / 4)
}

// drawSplash pinta el splash en el instante a.splashT.
func (a *App) drawSplash(dst *ebiten.Image) { a.drawSplashLayers(dst, true, true) }

// drawSplashLayers pinta el fondo (bg), lo de encima (fg: logo, ecualizador y
// firma, sobre transparente) o los dos. Por separado sirven a la web, que
// estira el fondo a cualquier pantalla y centra lo de encima sin recortarlo.
func (a *App) drawSplashLayers(dst *ebiten.Image, bg, fg bool) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	t := a.splashT
	th := DefaultTheme
	if bg {
		drawBackground(dst, th, t)
	}
	if !fg {
		return
	}

	lw, lh := splashLogoSize()
	scale := splashScale
	if lw*scale > w-8 { // en vertical no cabe al doble
		scale = 1
	}
	lx := (w - lw*scale) / 2
	ly := h/2 - 62
	if scale == 1 {
		ly = h/2 - 40
	}
	drawLogoAt(dst, lx, ly, t, scale)
	drawLEDs(dst, w/2, ly+lh*scale+2, t, math.Min(1, math.Max(0, (t-0.7)/0.4)))

	// La firma: logo de GitHub y el usuario, apareciendo por escalones.
	if alpha := steps((t - 0.9) / 0.4); alpha > 0 {
		drawSignature(dst, (w-signatureWidth())/2, h-36, alpha)
	}

	// Salida: el fondo tapa todo por escalones.
	if out := (t - (splashLen - splashOut)) / splashOut; out > 0 && bg {
		bg := th.Bg
		k := steps(out)
		bg.R, bg.G, bg.B, bg.A = uint8(float32(bg.R)*k), uint8(float32(bg.G)*k), uint8(float32(bg.B)*k), uint8(255*k)
		rect(dst, 0, 0, w, h, bg)
	}
}

// drawLEDs pinta el ecualizador de LEDs centrado en cx, con el borde de
// arriba en y; grow (0..1) es cuánto ha crecido.
func drawLEDs(dst *ebiten.Image, cx, y int, t, grow float64) {
	th := DefaultTheme
	const bars, bw, gap, blocks = 17, 4, 2, 6
	ex := cx - (bars*(bw+gap)-gap)/2
	for i := 0; i < bars; i++ {
		f := float64(i) / bars
		v := 0.5 + 0.5*math.Sin(t*5.3+f*7)
		v *= 0.55 + 0.45*(0.5+0.5*math.Sin(t*2.9+f*17))
		lit := int(v*blocks*grow + 0.5)
		for b := 0; b < blocks; b++ {
			c := mix(th.Accent, th.Accent2, float64(b)/blocks)
			if b >= lit {
				c = th.Bg2
			}
			rect(dst, ex+i*(bw+gap), y+(blocks-1-b)*3, bw, 2, c)
		}
	}
}

// drawSignature pinta el logo de GitHub y juanvillacortac a partir de (x, y),
// con opacidad alpha.
func drawSignature(dst *ebiten.Image, x, y int, alpha float32) {
	th := DefaultTheme
	iop := &ebiten.DrawImageOptions{}
	iop.GeoM.Translate(float64(x), float64(y))
	iop.ColorScale.ScaleWithColor(th.Text)
	iop.ColorScale.ScaleAlpha(alpha)
	dst.DrawImage(icon("github"), iop)
	c := th.Dim
	c.R, c.G, c.B, c.A = uint8(float32(c.R)*alpha), uint8(float32(c.G)*alpha), uint8(float32(c.B)*alpha), uint8(255*alpha)
	drawText(dst, splashBy, SmallFace, x+20, y+5, c)
}

// signatureWidth es el ancho de drawSignature.
func signatureWidth() int { return 20 + textWidth(splashBy, SmallFace) }
