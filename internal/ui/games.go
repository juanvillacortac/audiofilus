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
// pantallas. Esta pantalla los muestra en una lista (icono y nombre) con el
// elegido en grande al lado (de qué va y el récord); ↑/↓ eligen, A juega y B
// vuelve. "Salir" en la pausa de cada
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
	{Quiz, "quiz", "scr_quiz", "gm_quiz_d", "adivina.json"},
	{Snake, "snake", "scr_snake", "gm_snake_d", "serpiente.json"},
	{Shooter, "ship", "scr_shooter", "gm_shooter_d", "naves.json"},
	{Duel, "duel", "scr_duel", "gm_duel_d", "duelo.json"},
	{Tracker, "tracker", "scr_tracker", "gm_tracker_d", ""},
}

// isGame dice si una pantalla es un juego (o la de Juegos): en la barra de
// pantallas todas cuentan como el hueco de Juegos.
func isGame(s Screen) bool {
	return s == Games || s == Blocks || s == Rhythm || s == Crypt || s == Tracker || s == Quiz || s == Snake || s == Shooter || s == Duel
}

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
	case in.Repeat(ActUp), in.Repeat(ActLeft):
		a.gamesSel = (a.gamesSel + n - 1) % n
	case in.Repeat(ActDown), in.Repeat(ActRight):
		a.gamesSel = (a.gamesSel + 1) % n
	case in.Pressed(ActConfirm):
		a.screen = gameCards[a.gamesSel].screen
	case in.Pressed(ActBack):
		a.goBack()
	}
}

// drawGames pinta la lista: a la izquierda, un juego por fila (icono y
// nombre), el elegido marcado como en Ajustes; a la derecha, el elegido en
// grande, con de qué va y el récord.
func (a *App) drawGames(dst *ebiten.Image) {
	th := a.theme
	w, _ := dst.Bounds().Dx(), dst.Bounds().Dy()
	lw := min(150, w/2-margin)
	y0 := topH + 8
	const rowH = 23
	for i, gc := range gameCards {
		y := y0 + i*rowH
		c, tc := th.Dim, th.Dim
		if i == a.gamesSel {
			panel(dst, margin, y, lw, rowH-3, mix(th.Bg2, th.Accent, 0.14))
			rect(dst, margin, y, 2, rowH-3, th.Accent)
			c, tc = th.Accent, th.Text
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(2, 2)
		op.GeoM.Translate(float64(margin+8), float64(y+3))
		op.ColorScale.ScaleWithColor(c)
		dst.DrawImage(icon(gc.icon), op)
		drawText(dst, fit(T(gc.name), SmallFace, lw-34), SmallFace, margin+28, y+6, tc)
	}
	gc := gameCards[a.gamesSel]
	rx := margin + lw + 16
	rw := w - rx - margin
	img := icon(gc.icon)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(5, 5)
	op.GeoM.Translate(float64(rx), float64(y0+4))
	op.ColorScale.ScaleWithColor(th.Accent)
	dst.DrawImage(img, op)
	ty := y0 + 50
	for _, l := range wrapLines(T(gc.name), BigFace, rw) {
		drawText(dst, l, BigFace, rx, ty, th.Text)
		ty += 15
	}
	ty += 4
	for _, l := range wrapLines(T(gc.desc), SmallFace, rw) {
		drawText(dst, l, SmallFace, rx, ty, th.Dim)
		ty += 10
	}
	if gc.best != "" {
		if b := a.readBestCached(gc.best); b > 0 {
			drawText(dst, fmt.Sprintf("%s %d", T("tet_best"), b), SmallFace, rx, ty+6, th.Accent2)
		}
	}
	a.drawHints(dst, []hint{{"^v", T("gm_choose")}, {"A", T("gm_play")}, {"B", T("h_back")}, {"SEL", T("h_menu")}})
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

// wrapLines parte s por palabras en líneas de como mucho w píxeles.
func wrapLines(s string, face text.Face, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		next := word
		if line != "" {
			next = line + " " + word
		}
		if line != "" && textWidth(next, face) > w {
			out = append(out, line)
			next = word
		}
		line = next
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// drawTextWrap pinta s partido por palabras en líneas de como mucho w
// píxeles, separadas lh.
func drawTextWrap(dst *ebiten.Image, s string, face text.Face, x, y, w, lh int, c color.Color) {
	for _, l := range wrapLines(s, face, w) {
		drawText(dst, l, face, x, y, c)
		y += lh
	}
}
