package ui

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// Los huevos de pascua:
//
//   - El código Konami (↑↑↓↓←→←→ B A, en cualquier pantalla) enciende o apaga
//     el modo Game Boy: toda la imagen pasa a los cuatro verdes de la
//     pantalla de la Game Boy, con trama, y suena su "ding" de encendido. Se
//     guarda en audiofilus.json.
//   - En fechas especiales, al abrir la app, confeti (o corazones) y un
//     cartel: Año Nuevo, el cumpleaños de audiofilus (18 de octubre, su
//     primer commit) y el 26 de octubre.
//   - En Reproduciendo, ↑ y ↓ hacen scratch con el vinilo.

// konamiCode es la secuencia.
var konamiCode = []Action{ActUp, ActUp, ActDown, ActDown, ActLeft, ActRight, ActLeft, ActRight, ActBack, ActConfirm}

// konamiActs son las acciones que se apuntan para reconocerla.
var konamiActs = []Action{ActUp, ActDown, ActLeft, ActRight, ActBack, ActConfirm}

// watchKonami apunta las pulsaciones y, si forman el código, cambia el modo
// Game Boy.
func (a *App) watchKonami() {
	in := &a.input
	for _, act := range konamiActs {
		if !in.Pressed(act) {
			continue
		}
		a.konami = append(a.konami, act)
		if len(a.konami) > len(konamiCode) {
			a.konami = a.konami[1:]
		}
		if len(a.konami) == len(konamiCode) {
			match := true
			for i := range konamiCode {
				match = match && a.konami[i] == konamiCode[i]
			}
			if match {
				a.konami = nil
				a.cfg.UI.GameBoy = !a.cfg.UI.GameBoy
				a.saveConfig()
				if a.cfg.UI.GameBoy {
					a.flash(T("egg_gb_on"))
					a.sound("gb_ding", 4)
				} else {
					a.flash(T("egg_gb_off"))
				}
			}
		}
	}
}

// gbImg es el lienzo del paso Game Boy.
var gbImg *ebiten.Image

// applyGameBoy pasa la imagen a los cuatro verdes (con trama de Bayer).
func applyGameBoy(screen *ebiten.Image) {
	w, h := screen.Bounds().Dx(), screen.Bounds().Dy()
	if gbImg == nil || gbImg.Bounds().Dx() != w || gbImg.Bounds().Dy() != h {
		if gbImg != nil {
			gbImg.Deallocate()
		}
		gbImg = newTarget(w, h)
	}
	op := &ebiten.DrawRectShaderOptions{}
	op.Images[0] = screen
	gbImg.DrawRectShader(w, h, gbShader, op)
	screen.DrawImage(gbImg, nil)
}

// Las fechas especiales.
const (
	dayNone = iota
	dayNewYear
	dayAppBirthday
	dayWifeBirthday
)

// specialDay dice si hoy es un día especial.
func specialDay(now time.Time) int {
	switch _, m, d := now.Date(); {
	case m == time.January && d == 1:
		return dayNewYear
	case m == time.October && d == 18:
		return dayAppBirthday
	case m == time.October && d == 26:
		return dayWifeBirthday
	}
	return dayNone
}

// celebrateFor es lo que dura la celebración al abrir la app.
const celebrateFor = 9.0

// drawCelebration pinta el confeti (o los corazones) y el cartel durante los
// primeros segundos del día especial.
func (a *App) drawCelebration(dst *ebiten.Image) {
	kind := specialDay(a.now)
	if a.offline {
		kind = a.shotDay // las capturas eligen el día
	}
	if kind == dayNone {
		return
	}
	if a.celebrateAt == 0 {
		a.celebrateAt = a.t
	}
	d := a.t - a.celebrateAt
	if d > celebrateFor {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	fade := math.Min(1, (celebrateFor-d)/1.5)
	cols := []color.RGBA{th.Accent, th.Accent2, th.Text, mix(th.Accent, th.Text, 0.5)}
	for i := 0; i < 46; i++ {
		f := float64(i)
		speed := 28 + math.Mod(f*13.7, 40)
		y := math.Mod(d*speed+f*53, float64(h+20)) - 10
		x := math.Mod(f*71.3, float64(w)) + 6*math.Sin(d*1.7+f)
		c := withAlpha(cols[i%len(cols)], uint8(230*fade))
		if kind == dayWifeBirthday {
			// Corazones rosas y rojos, sea cual sea la portada.
			hearts := []color.RGBA{{0xff, 0x5f, 0xa2, 0xff}, {0xe8, 0x2a, 0x4a, 0xff}, {0xff, 0xa8, 0xc8, 0xff}}
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(math.Round(x), math.Round(y))
			op.ColorScale.ScaleAlpha(float32(fade))
			dst.DrawImage(crSprite("heart", hearts[i%3], color.RGBA{0xff, 0xff, 0xff, 0xff}), op)
			continue
		}
		wd, hd := 2, 3
		if int(d*6+f)%2 == 0 { // el confeti gira
			wd, hd = 3, 2
		}
		rect(dst, int(x), int(y), wd, hd, c)
	}
	msg := map[int]string{dayNewYear: T("egg_newyear"), dayAppBirthday: T("egg_app_birthday"), dayWifeBirthday: T("egg_wife_birthday")}[kind]
	bw := textWidth(msg, BigFace) + 24
	bx, by := (w-bw)/2, topH+16
	al := uint8(240 * fade)
	rect(dst, bx+3, by+3, bw, 28, withAlpha(th.Shadow, al))
	panel(dst, bx, by, bw, 28, withAlpha(th.Bg2, al))
	rect(dst, bx, by, bw, 1, withAlpha(th.Accent2, al))
	drawTextShadow(dst, msg, BigFace, bx+12, by+7, withAlpha(th.Text, al), withAlpha(th.Shadow, al))
}

// scratch es ↑ o ↓ en Reproduciendo: el vinilo da un tirón hacia un lado y,
// con música sonando, el servicio raya la propia canción (engine/scratch.go:
// ↑ un vaivén largo, ↓ dos cortos); en pausa, suena el efecto sintetizado.
func (a *App) scratch(dir int) {
	a.spinVel += 0.9 * float64(dir)
	if a.isPlaying && !a.offline {
		a.send("scratch", fmt.Sprint(dir))
		a.sound(soundNone, 2) // ni el bip de mover: suena la canción
		return
	}
	a.sound("scratch", 2)
}
