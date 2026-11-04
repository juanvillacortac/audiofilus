package ui

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/sfx"
)

// Las transiciones del modo juego son las del reproductor (transition.go):
// la pantalla nueva entra empujando a la anterior, por la derecha si se
// entra más adentro (sistemas → juegos → partidas u opciones) y por la
// izquierda al volver. Entrar al modo juego y salir de él también empuja.
// Como las pantallas del modo juego no siempre se pueden volver a dibujar
// tal como estaban (las opciones ya cerradas), se guarda una foto de cada
// fotograma y esa es la que sale empujada.

// gmTrans es el estado de las transiciones del modo juego.
type gmTrans struct {
	shown   gmScreen      // la pantalla del último fotograma
	valid   bool          // si frame tiene un fotograma del modo juego
	frame   *ebiten.Image // el último fotograma
	from    *ebiten.Image // la foto de la pantalla que sale
	fresh   *ebiten.Image // el fotograma nuevo, para empujarlo
	at      float64       // cuándo empezó (0: ninguna)
	dir     int           // 1 entra por la derecha, -1 por la izquierda
	fromApp bool          // se acaba de entrar desde el reproductor
}

// gmDepth es lo adentro que está cada pantalla, para el lado del empuje.
func gmDepth(s gmScreen) int {
	switch s {
	case gmSystems:
		return 0
	case gmGames, gmSearch, gmSysMenu:
		return 1
	case gmViewer, gmInfo:
		return 3
	case gmZoom2, gmVideoFull:
		return 4
	}
	return 2 // las opciones y las partidas
}

// fitTarget deja *img del tamaño w×h.
func fitTarget(img **ebiten.Image, w, h int) {
	if *img == nil || (*img).Bounds().Dx() != w || (*img).Bounds().Dy() != h {
		if *img != nil {
			(*img).Deallocate()
		}
		*img = newTarget(w, h)
	}
}

// drawGameModeFrame pinta un fotograma del modo juego (con el fondo) y, si
// cambió la pantalla, el empuje.
func (a *App) drawGameModeFrame(dst *ebiten.Image) {
	g := &a.gm
	tr := &g.trans
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	fitTarget(&tr.frame, w, h)
	fitTarget(&tr.from, w, h)
	fitTarget(&tr.fresh, w, h)
	switch {
	case tr.fromApp:
		// Desde el reproductor: sale su pantalla.
		tr.from.Clear()
		a.drawScreen(tr.from, a.screen)
		tr.at, tr.dir, tr.fromApp = a.t, 1, false
	case tr.valid && g.screen != tr.shown && !g.loading:
		tr.frame, tr.from = tr.from, tr.frame
		tr.at, tr.dir = a.t, 1
		if gmDepth(g.screen) < gmDepth(tr.shown) {
			tr.dir = -1
		}
		a.sound(sfx.Screen, 2)
	}
	tr.shown = g.screen

	if !a.ExternalBackdrop && !a.gmVideoNative() { // el vídeo, debajo (DrawFinalScreen)
		drawBackground(dst, a.theme, a.t)
	}
	a.drawGameMode(dst)
	if !pushFrame(dst, tr.from, tr.fresh, tr.at, tr.dir, a.t, a.theme.Accent) {
		tr.at = 0
		tr.frame.Clear()
		tr.frame.DrawImage(dst, nil)
		tr.valid = true
	}
}

// gmLeaveTransition prepara el empuje al volver al reproductor: la foto del
// modo juego sale por la izquierda (con la transición del reproductor).
func (a *App) gmLeaveTransition() {
	tr := &a.gm.trans
	if !tr.valid || tr.frame == nil {
		return
	}
	b := tr.frame.Bounds()
	fitTarget(&a.transImg, b.Dx(), b.Dy())
	a.transImg.Clear()
	a.transImg.DrawImage(tr.frame, nil)
	a.transAt, a.transReady, a.transDir = a.t, true, -1
	a.transFrom = a.screen
	a.lastScreen = a.screen
}

// pushFrame hace el empuje en dst (que ya tiene la pantalla nueva): from
// sale y la nueva (copiada en fresh) entra por el lado dir, rápido y con
// frenada, en píxeles enteros, con una raya de acento en su borde. Devuelve
// si la transición sigue en curso.
func pushFrame(dst, from, fresh *ebiten.Image, at float64, dir int, t float64, edge color.Color) bool {
	p := (t - at) / transLen
	if at == 0 || p >= 1 {
		return false
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	fresh.Clear()
	fresh.DrawImage(dst, nil)
	e := 1 - math.Pow(1-p, 3)
	off := int(math.Round(e * float64(w)))
	dst.Clear()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(-dir*off), 0)
	dst.DrawImage(from, op)
	op = &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(dir*(w-off)), 0)
	dst.DrawImage(fresh, op)
	x := w - off
	if dir < 0 {
		x = off - 1
	}
	rect(dst, x, 0, 1, h, edge)
	return true
}
