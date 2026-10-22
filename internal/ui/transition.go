package ui

import (
	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Las pantallas cambian con un empuje: la nueva entra desde un lado y empuja
// a la anterior, rápido (0,16 s) y con frenada, en píxeles enteros. Si la
// nueva va después en la barra de pantallas entra por la derecha; si va
// antes, por la izquierda.

// transLen es lo que dura la transición.
const transLen = 0.16

// watchScreen arranca la transición cuando cambia la pantalla.
func (a *App) watchScreen() {
	if a.screen == a.lastScreen {
		return
	}
	a.transFrom, a.transAt, a.transReady = a.lastScreen, a.t, false
	a.sound(sfx.Screen, 2)
	// Hacia la derecha si la nueva va después en la barra de pantallas.
	a.transDir = 1
	if dockIndex(a.screen) < dockIndex(a.lastScreen) {
		a.transDir = -1
	}
	a.prevScreen = a.lastScreen
	a.lastScreen = a.screen
}

// drawTransition empuja: la pantalla nueva (ya dibujada en dst) se copia y
// se pinta desplazada, con la anterior al lado.
func (a *App) drawTransition(dst *ebiten.Image) {
	p := (a.t - a.transAt) / transLen
	if a.transAt == 0 || p >= 1 || a.mode != modeMain {
		return
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	fit := func(img **ebiten.Image) {
		if *img == nil || (*img).Bounds().Dx() != w || (*img).Bounds().Dy() != h {
			if *img != nil {
				(*img).Deallocate()
			}
			*img = newTarget(w, h)
		}
	}
	fit(&a.transImg)
	fit(&a.transNew)
	if !a.transReady {
		// La foto de la anterior, una vez al empezar.
		a.transImg.Clear()
		a.drawScreen(a.transImg, a.transFrom)
		a.transReady = true
	}
	a.transNew.Clear()
	a.transNew.DrawImage(dst, nil)
	// Con frenada (cúbica) y en píxeles enteros.
	e := 1 - math.Pow(1-p, 3)
	off := int(math.Round(e * float64(w)))
	d := a.transDir
	dst.Clear()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(-d*off), 0)
	dst.DrawImage(a.transImg, op)
	op = &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(d*(w-off)), 0)
	dst.DrawImage(a.transNew, op)
	// Una raya de acento en el borde de la que entra.
	edge := w - off
	if d < 0 {
		edge = off - 1
	}
	rect(dst, edge, 0, 1, h, a.theme.Accent)
}
