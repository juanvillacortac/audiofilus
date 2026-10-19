package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Juegos: los minijuegos van juntos en un solo hueco de la barra de
// pantallas. Esta pantalla los muestra como tarjetas (icono, nombre, de qué va
// y el récord); ←/→ eligen, A juega y B vuelve. "Salir" en la pausa de cada
// juego trae de vuelta aquí.

// gameCard es un juego de la pantalla Juegos.
type gameCard struct {
	screen Screen
	icon   string
	name   string // clave de lang.go
	desc   string // clave de lang.go
	best   string // el archivo del récord, en data/
}

var gameCards = []gameCard{
	{Crypt, "sword", "scr_crypt", "gm_crypt_d", "cripta.json"},
	{Blocks, "blocks", "scr_blocks", "gm_blocks_d", "bloques.json"},
	{Rhythm, "note", "scr_rhythm", "gm_rhythm_d", "ritmo.json"},
}

// isGame dice si una pantalla es un juego (o la de Juegos): en la barra de
// pantallas todas cuentan como el hueco de Juegos.
func isGame(s Screen) bool { return s == Games || s == Blocks || s == Rhythm || s == Crypt }

// readBest lee un récord guardado (0 si no hay).
func (a *App) readBest(file string) int {
	b, err := os.ReadFile(filepath.Join(a.paths.Data, file))
	if err != nil {
		return 0
	}
	var v struct{ Best int }
	_ = json.Unmarshal(b, &v)
	return v.Best
}

// updateGames atiende la pantalla de Juegos.
func (a *App) updateGames() {
	in := &a.input
	n := len(gameCards)
	switch {
	case in.Repeat(ActLeft), in.Repeat(ActUp):
		a.gamesSel = (a.gamesSel + n - 1) % n
	case in.Repeat(ActRight), in.Repeat(ActDown):
		a.gamesSel = (a.gamesSel + 1) % n
	case in.Pressed(ActConfirm):
		a.screen = gameCards[a.gamesSel].screen
	case in.Pressed(ActBack):
		a.goBack()
	}
}

// drawGames pinta las tarjetas: en fila si caben, si no la elegida grande
// con sus vecinas asomando.
func (a *App) drawGames(dst *ebiten.Image) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	n := len(gameCards)
	cw, ch := 104, 150
	gap := 8
	total := n*cw + (n-1)*gap
	x0 := (w - total) / 2
	if total > w-2*margin { // no caben: la elegida al centro
		x0 = w/2 - cw/2 - a.gamesSel*(cw+gap)
	}
	y := topH + (h-topH-bottomH-ch)/2
	for i, gc := range gameCards {
		x := x0 + i*(cw+gap)
		sel := i == a.gamesSel
		yy := y
		if sel {
			yy -= 4 + int(2*a.beatPulse())
		}
		rect(dst, x+3, yy+3, cw, ch, th.Shadow)
		bg := th.Bg2
		if sel {
			bg = mix(th.Bg2, th.Accent, 0.18)
		}
		panel(dst, x, yy, cw, ch, bg)
		if sel {
			frame(dst, x-1, yy-1, cw+2, ch+2, th.Accent)
		}
		// El icono, grande.
		img := icon(gc.icon)
		iw := img.Bounds().Dx()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(5, 5)
		op.GeoM.Translate(float64(x+(cw-5*iw)/2), float64(yy+14))
		c := th.Dim
		if sel {
			c = th.Accent
		}
		op.ColorScale.ScaleWithColor(c)
		dst.DrawImage(img, op)
		name := T(gc.name)
		drawTextShadow(dst, name, BigFace, x+(cw-textWidth(name, BigFace))/2, yy+62, th.Text, th.Shadow)
		drawTextWrap(dst, T(gc.desc), SmallFace, x+8, yy+84, cw-16, 10, th.Dim)
		if b := a.readBestCached(gc.best); b > 0 {
			s := fmt.Sprintf("%s %d", T("tet_best"), b)
			drawText(dst, s, SmallFace, x+(cw-textWidth(s, SmallFace))/2, yy+ch-14, th.Accent2)
		}
	}
	a.drawHints(dst, []hint{{"<>", T("gm_choose")}, {"A", T("gm_play")}, {"B", T("h_back")}, {"SEL", T("h_menu")}})
}

// readBestCached lee los récords como mucho una vez por segundo (no en cada
// fotograma).
func (a *App) readBestCached(file string) int {
	if a.gamesBest == nil || a.t-a.gamesBestAt > 1 {
		a.gamesBest, a.gamesBestAt = map[string]int{}, a.t
	}
	if v, ok := a.gamesBest[file]; ok {
		return v
	}
	v := a.readBest(file)
	a.gamesBest[file] = v
	return v
}

// drawTextWrap pinta s partido por palabras en líneas de como mucho w
// píxeles, separadas lh.
func drawTextWrap(dst *ebiten.Image, s string, face text.Face, x, y, w, lh int, c color.Color) {
	line := ""
	for _, word := range strings.Fields(s) {
		next := word
		if line != "" {
			next = line + " " + word
		}
		if line != "" && textWidth(next, face) > w {
			drawText(dst, line, face, x, y, c)
			y += lh
			next = word
		}
		line = next
	}
	if line != "" {
		drawText(dst, line, face, x, y, c)
	}
}
