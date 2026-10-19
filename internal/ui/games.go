package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
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
	{Quiz, "quiz", "scr_quiz", "gm_quiz_d", "adivina.json"},
	{Snake, "snake", "scr_snake", "gm_snake_d", "serpiente.json"},
	{Shooter, "ship", "scr_shooter", "gm_shooter_d", "naves.json"},
	{Tracker, "tracker", "scr_tracker", "gm_tracker_d", ""},
}

// isGame dice si una pantalla es un juego (o la de Juegos): en la barra de
// pantallas todas cuentan como el hueco de Juegos.
func isGame(s Screen) bool {
	return s == Games || s == Blocks || s == Rhythm || s == Crypt || s == Tracker || s == Quiz || s == Snake || s == Shooter
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
	case in.Repeat(ActLeft), in.Repeat(ActUp):
		a.gamesSel--
		if a.gamesSel < 0 { // da la vuelta: el carrusel viene desde la otra punta
			a.gamesSel, a.gamesPos = n-1, a.gamesPos+float64(n)
		}
	case in.Repeat(ActRight), in.Repeat(ActDown):
		a.gamesSel++
		if a.gamesSel >= n {
			a.gamesSel, a.gamesPos = 0, a.gamesPos-float64(n)
		}
	case in.Pressed(ActConfirm):
		a.screen = gameCards[a.gamesSel].screen
	case in.Pressed(ActBack):
		a.goBack()
	}
}

// drawGames es la sala de arcade: un suelo de cuadrícula en perspectiva que
// corre hacia el horizonte y late con la música; el carrusel, con el juego
// elegido grande al centro y los vecinos más chicos y apagados a los lados
// (se desliza al cambiar); la tarjeta elegida, con bombillas de marquesina
// que se encienden por turnos y su icono saltando con el ritmo; y abajo el
// nombre, de qué va, el récord, "PULSA A" parpadeando y los puntos de página.
func (a *App) drawGames(dst *ebiten.Image) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	n := len(gameCards)
	pulse := a.beatPulse()
	a.gamesPos += (float64(a.gamesSel) - a.gamesPos) * 0.22
	if a.offline {
		a.gamesPos = float64(a.gamesSel)
	}
	a.drawArcadeFloor(dst, pulse)
	// El carrusel: de lejos a cerca, para que el centro quede encima.
	cy := topH + 70
	spacing := float64(min(118, w/3))
	type slot struct {
		i int
		d float64
	}
	var slots []slot
	for i := range gameCards {
		if d := float64(i) - a.gamesPos; math.Abs(d) < 2.6 {
			slots = append(slots, slot{i, d})
		}
	}
	sort.Slice(slots, func(p, q int) bool { return math.Abs(slots[p].d) > math.Abs(slots[q].d) })
	for _, sl := range slots {
		ad := math.Abs(sl.d)
		near := 1 - math.Min(1, ad) // 1 en el centro
		cw := int(64 + 36*near)
		ch := int(74 + 40*near)
		x := w/2 + int(math.Round(sl.d*spacing)) - cw/2
		y := cy - ch/2 + int(10*math.Min(1, ad))
		gc := gameCards[sl.i]
		sel := sl.i == a.gamesSel && ad < 0.5
		rect(dst, x+3, y+4, cw, ch, withAlpha(th.Shadow, 0xc0))
		bg := mix(th.Bg2, th.Shadow, 0.35*math.Min(1, ad))
		if sel {
			bg = mix(th.Bg2, th.Accent, 0.16+0.08*pulse)
		}
		panel(dst, x, y, cw, ch, bg)
		if sel {
			a.drawBulbs(dst, x, y, cw, ch)
		}
		// El icono, a escala entera: más grande cuanto más al centro.
		k := 3.0
		switch {
		case ad < 0.5:
			k = 6
		case ad < 1.5:
			k = 4
		}
		img := icon(gc.icon)
		iw, ih := img.Bounds().Dx(), img.Bounds().Dy()
		bounce := 0.0
		if sel {
			bounce = 3 * pulse
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(k, k)
		dy := -6
		if sel {
			dy = 0 // sin nombre debajo: el icono, al centro
		}
		op.GeoM.Translate(float64(x+(cw-int(k)*iw)/2), float64(y+(ch-int(k)*ih)/2+dy)-bounce)
		ic := mix(th.Dim, th.Shadow, 0.3*math.Min(1, ad))
		if sel {
			ic = th.Accent
		}
		op.ColorScale.ScaleWithColor(ic)
		dst.DrawImage(img, op)
		if ad < 1.5 && !sel { // el nombre de los vecinos (el del elegido va abajo, grande)
			name := fit(T(gc.name), SmallFace, cw-8)
			drawText(dst, name, SmallFace, x+(cw-textWidth(name, SmallFace))/2, y+ch-13, th.Dim)
		}
	}
	// Abajo, lo del elegido.
	gc := gameCards[a.gamesSel]
	ty := cy + 68
	name := T(gc.name)
	drawTextShadow(dst, name, BigFace, (w-textWidth(name, BigFace))/2, ty, th.Text, th.Shadow)
	ty += 17
	for _, l := range wrapLines(T(gc.desc), SmallFace, min(w-2*margin, 260)) {
		drawText(dst, l, SmallFace, (w-textWidth(l, SmallFace))/2, ty, th.Dim)
		ty += 10
	}
	info := ""
	if gc.best != "" {
		if b := a.readBestCached(gc.best); b > 0 {
			info = fmt.Sprintf("%s %d", T("tet_best"), b)
		}
	}
	if int(a.t*2.5)%2 == 0 {
		if info != "" {
			info += "   "
		}
		info += T("gm_press_a")
	}
	drawText(dst, info, SmallFace, (w-textWidth(info, SmallFace))/2, ty+3, th.Accent2)
	// Los puntos de página.
	dw := n*6 - 2
	for i := 0; i < n; i++ {
		c := withAlpha(th.Dim, 0x80)
		if i == a.gamesSel {
			c = th.Accent
		}
		rect(dst, (w-dw)/2+i*6, h-bottomH-7, 4, 2, c)
	}
	a.drawHints(dst, []hint{{"<>", T("gm_choose")}, {"A", T("gm_play")}, {"B", T("h_back")}, {"SEL", T("h_menu")}})
}

// drawArcadeFloor pinta el suelo de la sala: líneas que se van hacia el punto
// de fuga del horizonte y travesaños que se acercan, más deprisa con los
// graves, en el color del acento; arriba, un resplandor que late.
func (a *App) drawArcadeFloor(dst *ebiten.Image, pulse float64) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	hy := topH + 46 // el horizonte
	bottom := h - bottomH
	rect(dst, 0, hy, w, bottom-hy, withAlpha(th.Shadow, 0x70))
	rect(dst, 0, hy, w, 1, withAlpha(th.Accent, uint8(80+90*pulse)))
	vx := float32(w) / 2
	lc := withAlpha(th.Accent, uint8(36+40*pulse))
	for i := -9; i <= 9; i++ {
		x1 := vx + float32(i)*float32(w)/7
		vector.StrokeLine(dst, vx+float32(i)*6, float32(hy), x1, float32(bottom), 1, lc, false)
	}
	_, bass, _, _ := a.meterNow()
	off := math.Mod(a.t*(0.5+bass), 1)
	for k := 0; k < 8; k++ {
		f := (float64(k) + off) / 8 // 0 en el horizonte, 1 abajo
		y := hy + int(float64(bottom-hy)*f*f)
		rect(dst, 0, y, w, 1, withAlpha(th.Accent, uint8(20+60*f)))
	}
}

// drawBulbs pinta las bombillas alrededor de la tarjeta elegida: una cada
// 6 px, encendidas de dos en dos y corriendo, más en el golpe.
func (a *App) drawBulbs(dst *ebiten.Image, x, y, w, h int) {
	th := a.theme
	step := int(a.t*8) + int(a.beatPulse()*2)
	k := 0
	bulb := func(bx, by int) {
		c := withAlpha(th.Accent2, 0x50)
		if (k+step)%3 == 0 {
			c = th.Text
		}
		rect(dst, bx, by, 2, 2, c)
		k++
	}
	for bx := x + 2; bx < x+w-2; bx += 6 {
		bulb(bx, y-4)
	}
	for by := y + 2; by < y+h-2; by += 6 {
		bulb(x+w+2, by)
	}
	for bx := x + w - 4; bx > x; bx -= 6 {
		bulb(bx, y+h+2)
	}
	for by := y + h - 4; by > y; by -= 6 {
		bulb(x-4, by)
	}
	frame(dst, x-1, y-1, w+2, h+2, th.Accent)
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
