package ui

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// supporters son quienes apoyaron Audiofilus en Ko-fi, en el orden en que
// llegaron: salen en Ajustes > Invítame un café, bajo el QR, con corazones.
// pnga fue la primera donación (noviembre de 2026).
var supporters = []string{"pnga"}

// supportersLine es "Gracias a pnga, ..." para el panel de las donaciones.
func supportersLine() string {
	return T("supp_thanks") + " " + strings.Join(supporters, ", ")
}

// donateValue es lo que se ve en la baldosa de las donaciones: el
// agradecimiento a quien apoyó (el último, si son varios) y, si no hay,
// la dirección.
func donateValue() string {
	if len(supporters) == 0 {
		return donateURL[len("https://"):]
	}
	return T("supp_thanks") + " " + supporters[len(supporters)-1] + " ♥"
}

// drawSupporters pinta, bajo el QR de las donaciones, el agradecimiento con
// un corazón que late a cada lado y otros pequeños que salen de ellos
// (centrado en cx, con la línea en y).
func (a *App) drawSupporters(dst *ebiten.Image, cx, y int) {
	if len(supporters) == 0 {
		return
	}
	th := a.theme
	line := supportersLine()
	tw := textWidth(line, SmallFace)
	x := cx - tw/2
	beat := 1 + 0.15*math.Max(0, math.Sin(a.t*6)) // late como un corazón
	drawText(dst, line, SmallFace, x, y, th.Text)
	for _, hx := range []int{x - 14, x + tw + 4} {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(-5, -5)
		op.GeoM.Scale(beat, beat)
		op.GeoM.Translate(float64(hx+5), float64(y+5))
		op.ColorScale.ScaleWithColor(th.Accent)
		dst.DrawImage(icon("heart"), op)
	}
	// Los que suben: de cada corazón de los lados, dos a la vez, hacia
	// afuera (lejos del texto), balanceándose y apagándose.
	for side, hx0 := range []float64{float64(x - 14), float64(x + tw + 4)} {
		dir := float64(side*2 - 1) // -1 a la izquierda, 1 a la derecha
		for i := range 2 {
			p := math.Mod(a.t*0.45+float64(i)/2+float64(side)/4, 1) // 0 al salir, 1 al apagarse
			hx := math.Round(hx0 + dir*p*14 + 1.5*math.Sin(a.t*3+float64(i)))
			hy := math.Round(float64(y+1) - p*16)
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(hx, hy)
			op.ColorScale.ScaleWithColor(th.Accent)
			op.ColorScale.ScaleAlpha(float32(0.7 * (1 - p)))
			dst.DrawImage(icon("heart"), op)
		}
	}
}
