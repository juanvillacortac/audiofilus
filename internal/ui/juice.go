package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// El "juice" de la pantalla de reproducción: pequeñas animaciones que hacen
// que cada acción se sienta. Todo en píxeles enteros y por escalones, como el
// resto: nada de suavizados de alta resolución.
//
//   - Canción nueva: la portada entra desde el lado (siguiente: desde la
//     derecha; anterior: desde la izquierda) con un rebote, un destello que se
//     apaga por escalones y un acelerón del vinilo (como un scratch).
//   - Pausa o reanudar: el botón da un "pop" y suelta un anillo que crece; al
//     cambiar de canción, igual el botón de anterior o siguiente.
//   - Adelantar o retrasar: la barra da un empujoncito hacia el lado del salto.
//
// (Hubo confeti de píxeles al cambiar de canción; al autor no le gustó.)

// juiceState es lo que lleva el juice: cuándo pasó cada cosa.
type juiceState struct {
	trackAt  float64 // cuándo cambió la canción (0: nunca)
	trackDir int     // 1 siguiente, -1 anterior
	toggleAt float64 // cuándo se pausó o reanudó
	seekAt   float64 // cuándo se adelantó o retrasó
	seekDir  int
}

const (
	juiceTrackLen  = 0.45 // lo que dura la entrada de la portada
	juiceToggleLen = 0.3
)

// juiceTrack anima el cambio de canción; dir es 1 (siguiente) o -1 (anterior).
func (a *App) juiceTrack(dir int) {
	a.juice.trackAt, a.juice.trackDir = a.t, dir
	a.spinVel += 0.22 * float64(dir) // el scratch
}

// juiceToggle anima pausar o reanudar.
func (a *App) juiceToggle() { a.juice.toggleAt = a.t }

// juiceSeek anima un salto; dir es el signo del salto.
func (a *App) juiceSeek(dir int) { a.juice.seekAt, a.juice.seekDir = a.t, dir }

// coverOffset es cuánto se desplaza la portada al entrar una canción nueva.
func (a *App) coverOffset(size int) int {
	j := &a.juice
	p := (a.t - j.trackAt) / juiceTrackLen
	if j.trackAt == 0 || p >= 1 || p < 0 {
		return 0
	}
	return int(math.Round(float64(j.trackDir*size/2) * (1 - easeOutBack(p))))
}

// easeOutBack llega a 1 pasándose un poco (el rebote).
func easeOutBack(x float64) float64 {
	const c1 = 1.70158
	const c3 = c1 + 1
	return 1 + c3*math.Pow(x-1, 3) + c1*math.Pow(x-1, 2)
}

// coverFlash es la intensidad del destello sobre la portada (por escalones).
func (a *App) coverFlash() float32 {
	j := &a.juice
	if j.trackAt == 0 {
		return 0
	}
	return steps(1 - (a.t-j.trackAt)/0.3)
}

// popAt es cuántos píxeles crece un botón pulsado en el instante at (0 si ya
// pasó).
func (a *App) popAt(at float64) int {
	p := (a.t - at) / juiceToggleLen
	if at == 0 || p >= 1 || p < 0 {
		return 0
	}
	return int(math.Round(3 * math.Sin(p*math.Pi)))
}

// playPop es cuánto crece el botón de play al pausar o reanudar.
func (a *App) playPop() int { return a.popAt(a.juice.toggleAt) }

// trackPressed dice si el botón de anterior (dir -1) o siguiente (dir 1) se
// ve pulsado: al cambiar de canción en esa dirección, como el de play.
func (a *App) trackPressed(dir int) bool {
	return a.juice.trackDir == dir && a.juice.trackAt > 0 && a.t-a.juice.trackAt < juiceToggleLen
}

// drawRing pinta el anillo que crece desde un botón de 15×15 pulsado en at.
func (a *App) drawRing(dst *ebiten.Image, x, y int, at float64) {
	p := (a.t - at) / juiceToggleLen
	if at == 0 || p >= 1 || p < 0 {
		return
	}
	grow := int(p * 14)
	k := uint8(255 * steps(1-p))
	frame(dst, x-grow, y-grow, 15+2*grow, 15+2*grow, withAlpha(a.theme.Accent, k))
}

// seekNudge es el empujoncito de la barra hacia el lado del salto.
func (a *App) seekNudge() int {
	p := (a.t - a.juice.seekAt) / 0.2
	if a.juice.seekAt == 0 || p >= 1 || p < 0 {
		return 0
	}
	return int(math.Round(2 * float64(a.juice.seekDir) * math.Sin(p*math.Pi)))
}
