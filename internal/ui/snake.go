package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
)

// Serpiente al ritmo: la serpiente da un paso en cada subdivisión del compás
// (negra, corchea o semicorchea: la que quede más cerca de un octavo de
// segundo), así que cada canción juega a su velocidad y siempre a tiempo.
// Las monedas la alargan; en los golpes fuertes puede salir un corazón que
// vale más y se va a los tres golpes. Chocar con la pared o consigo misma
// acaba la partida. Con la música en pausa, se queda quieta.

const snCell = 10 // píxeles por casilla

type snPt struct{ x, y int }

// snakeGame es una partida.
type snakeGame struct {
	cols, rows int
	body       []snPt // la cabeza primero
	prev       []snPt // dónde estaba cada segmento antes del último paso (para deslizarlo)
	dir, next  snPt
	grow       int
	food       snPt
	bonus      snPt
	bonusUntil float64 // reloj del ritmo; 0 si no hay
	score      int
	best       int
	over       bool
	overAt     float64
	frozen     bool
	lastStep   float64 // el último paso (reloj del ritmo)
	stepLen    float64 // lo que dura un paso ahora
	eatAt      float64
	fallback   float64 // el ancla del metrónomo si no hay tempo
}

func (a *App) snakeBestPath() string { return filepath.Join(a.paths.Data, "snake.json") }

// newSnake empieza una partida del tamaño de la pantalla.
func (a *App) newSnake() *snakeGame {
	w := max(a.w, 240)
	g := &snakeGame{cols: (w - 2*margin) / snCell, rows: (max(a.h, ScreenH) - topH - bottomH - 16) / snCell, fallback: a.beatClock()}
	if b, err := os.ReadFile(a.snakeBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	cx, cy := g.cols/2, g.rows/2
	g.body = []snPt{{cx, cy}, {cx - 1, cy}, {cx - 2, cy}}
	g.dir, g.next = snPt{1, 0}, snPt{1, 0}
	g.placeFood()
	g.lastStep = a.beatClock()
	return g
}

// saveSnakeBest guarda el récord.
func (a *App) saveSnakeBest() {
	g := a.snake
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.snakeBestPath(), b, 0o644)
}

// free dice si una casilla está libre (dentro y sin serpiente).
func (g *snakeGame) free(p snPt) bool {
	if p.x < 0 || p.y < 0 || p.x >= g.cols || p.y >= g.rows {
		return false
	}
	for _, b := range g.body {
		if b == p {
			return false
		}
	}
	return true
}

// placeFood pone la moneda en una casilla libre.
func (g *snakeGame) placeFood() {
	for tries := 0; tries < 500; tries++ {
		p := snPt{rand.IntN(g.cols), rand.IntN(g.rows)}
		if g.free(p) && p != g.bonus {
			g.food = p
			return
		}
	}
}

// snakeStep es lo que dura un paso: la negra, la corchea o la semicorchea de
// la canción, la que quede más cerca de snakePace (a 120 BPM, semicorcheas).
func (a *App) snakeStep(g *snakeGame) (step, anchor float64) {
	per, anchor := a.steadyBeat()
	if per == 0 {
		per, anchor = 0.5, g.fallback
	}
	step = per
	for _, n := range []float64{2, 4} {
		if math.Abs(per/n-snakePace) < math.Abs(step-snakePace) {
			step = per / n
		}
	}
	return step, anchor
}

// snakePace es el paso que se busca: ocho casillas por segundo.
const snakePace = 0.125

// updateSnake juega un fotograma.
func (a *App) updateSnake() {
	g, in := a.snake, &a.input
	switch {
	case in.Pressed(ActMenu):
		a.openSnakePause()
		return
	case in.Pressed(ActPause):
		a.send("toggle")
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	}
	if g.over {
		if in.Pressed(ActConfirm) && a.t-g.overAt > 0.6 {
			a.saveSnakeBest()
			a.snake = a.newSnake()
		}
		return
	}
	for _, d := range []struct {
		act Action
		p   snPt
	}{{ActUp, snPt{0, -1}}, {ActDown, snPt{0, 1}}, {ActLeft, snPt{-1, 0}}, {ActRight, snPt{1, 0}}} {
		if in.Pressed(d.act) && d.p != (snPt{-g.dir.x, -g.dir.y}) {
			g.next = d.p
		}
	}
	if !a.hearing() && !a.offline {
		g.frozen = true
		return
	}
	step, anchor := a.snakeStep(g)
	now := a.beatClock()
	tick := anchor + math.Floor((now-anchor)/step)*step
	if g.frozen || tick-g.lastStep > 4*step {
		g.frozen, g.lastStep = false, tick
	}
	if tick <= g.lastStep+step/2 {
		return
	}
	g.lastStep, g.stepLen = tick, step
	g.prev = append(g.prev[:0], g.body...)
	// Un paso.
	g.dir = g.next
	head := snPt{g.body[0].x + g.dir.x, g.body[0].y + g.dir.y}
	if g.grow == 0 {
		g.body = g.body[:len(g.body)-1]
	} else {
		g.grow--
	}
	if !g.free(head) {
		g.over, g.overAt = true, a.t
		a.saveSnakeBest()
		a.sound("cr_dead", 4)
		return
	}
	g.body = append([]snPt{head}, g.body...)
	switch {
	case head == g.food:
		g.grow++
		g.score += 10 * (1 + len(g.body)/10)
		g.eatAt = a.t
		g.placeFood()
		a.sound("cr_coin", 2)
	case g.bonusUntil > 0 && head == g.bonus:
		g.grow += 2
		g.score += 50
		g.eatAt, g.bonusUntil = a.t, 0
		a.sound("cr_kill", 3)
	}
	// En cada golpe fuerte (cada cuatro tiempos), quizá un corazón.
	if per, _ := a.steadyBeat(); per > 0 && g.bonusUntil == 0 && rand.IntN(6) == 0 {
		if int(math.Round((tick-anchor)/per))%4 == 0 {
			for tries := 0; tries < 50; tries++ {
				p := snPt{rand.IntN(g.cols), rand.IntN(g.rows)}
				if g.free(p) && p != g.food {
					g.bonus, g.bonusUntil = p, now+3*per
					break
				}
			}
		}
	}
	if g.bonusUntil > 0 && now > g.bonusUntil {
		g.bonusUntil = 0
	}
}

// openSnakePause es el menú de pausa.
func (a *App) openSnakePause() {
	g := a.snake
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("sn_length"), len(g.body)),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveSnakeBest(); a.snake = a.newSnake() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveSnakeBest(); a.screen = Games }},
		},
	}
}

// drawSnake pinta la partida.
func (a *App) drawSnake(dst *ebiten.Image) {
	g, th := a.snake, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	bw, bh := g.cols*snCell, g.rows*snCell
	// Centrado en el alto (si la partida empezó con la pantalla en otra
	// orientación, el tablero es más bajo que lo que hay).
	top := topH + max(0, (h-topH-bottomH-16-bh)/2)
	ox, oy := (w-bw)/2, top+14
	pulse := a.beatPulse()
	head := fmt.Sprintf("%s %d", T("tet_score"), g.score)
	drawText(dst, head, SmallFace, ox, top+4, th.Text)
	bs := fmt.Sprintf("%s %d", T("tet_best"), max(g.best, g.score))
	drawText(dst, bs, SmallFace, ox+bw-textWidth(bs, SmallFace), top+4, th.Dim)
	panel(dst, ox-2, oy-2, bw+4, bh+4, withAlpha(th.Shadow, 0xc0))
	// La cuadrícula de puntos, que late con el golpe: una imagen hecha una
	// vez (antes eran ~600 rectángulos por cuadro: en la consola se notaba).
	if snDots == nil || snDots.Bounds().Dx() != bw || snDots.Bounds().Dy() != bh {
		if snDots != nil {
			snDots.Deallocate()
		}
		snDots = ebiten.NewImage(bw, bh)
		for y := 0; y < g.rows; y++ {
			for x := 0; x < g.cols; x++ {
				snDots.Set(x*snCell+snCell/2, y*snCell+snCell/2, color.White)
			}
		}
	}
	dop := &ebiten.DrawImageOptions{}
	dop.GeoM.Translate(float64(ox), float64(oy))
	dop.ColorScale.ScaleWithColor(withAlpha(th.Accent, uint8(30+50*pulse)))
	dst.DrawImage(snDots, dop)
	// La moneda y el corazón (que parpadea al acabarse).
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(ox+g.food.x*snCell), float64(oy+g.food.y*snCell-int(pulse*1.5)))
	dst.DrawImage(crSprite("coin", mix(th.Accent, th.Text, 0.4), th.Shadow), op)
	if g.bonusUntil > 0 && (g.bonusUntil-a.beatClock() > 0.8 || int(a.t*10)%2 == 0) {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(ox+g.bonus.x*snCell), float64(oy+g.bonus.y*snCell))
		dst.DrawImage(crSprite("heart", th.Accent2, th.Text), op)
	}
	// La serpiente: de un acento al otro, la cabeza más clara y con ojos.
	// Cada segmento se desliza de su casilla de antes a la nueva durante el
	// paso (antes saltaba de golpe y, cuanto más larga, más a tirones).
	f := 1.0
	if g.stepLen > 0 && !g.frozen && !g.over {
		f = math.Max(0, math.Min(1, (a.beatClock()-g.lastStep)/g.stepLen))
	}
	n := len(g.body)
	for i := n - 1; i >= 0; i-- {
		px, py := float64(g.body[i].x), float64(g.body[i].y)
		if i < len(g.prev) {
			px += (float64(g.prev[i].x) - px) * (1 - f)
			py += (float64(g.prev[i].y) - py) * (1 - f)
		}
		sx, sy := ox+int(math.Round(px*snCell)), oy+int(math.Round(py*snCell))
		c := mix(th.Accent, th.Accent2, float64(i)/float64(max(1, n-1)))
		if i == 0 {
			c = mix(th.Accent, th.Text, 0.35)
		}
		if a.t-g.eatAt < 0.2 {
			c = mix(c, th.Text, 0.4)
		}
		inset := 1
		if i > 0 && pulse > 0.5 && i%2 == 0 {
			inset = 0 // el cuerpo late con el golpe
		}
		rect(dst, sx+inset, sy+inset, snCell-2*inset, snCell-2*inset, c)
		if i == 0 {
			ex, ey := 3*g.dir.y, 3*g.dir.x
			eye := th.Shadow
			rect(dst, sx+4+2*g.dir.x+ex, sy+4+2*g.dir.y+ey, 2, 2, eye)
			rect(dst, sx+4+2*g.dir.x-ex, sy+4+2*g.dir.y-ey, 2, 2, eye)
		}
	}
	if g.frozen && !g.over {
		a.drawCenterNote(dst, T("cr_play_music"))
	}
	if g.over {
		a.drawGameOver(dst, T("sn_over"), fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("sn_length"), len(g.body)), max(g.best, g.score), g.overAt)
	}
	a.drawHints(dst, []hint{{"<>^v", T("sn_h_turn")}, {"START", T("h_pause")}, {"SEL", T("h_menu")}})
}

// snDots es la cuadrícula de puntos del tablero, hecha una vez.
var snDots *ebiten.Image

// drawCenterNote es un aviso en el centro de la pantalla.
func (a *App) drawCenterNote(dst *ebiten.Image, msg string) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	bw := textWidth(msg, SmallFace) + 16
	panel(dst, (w-bw)/2, h/2-10, bw, 20, withAlpha(th.Shadow, 0xe0))
	drawText(dst, msg, SmallFace, (w-bw)/2+8, h/2-3, th.Text)
}

// drawGameOver es el cartel de fin de partida de los juegos: título, una
// línea con lo hecho, el récord y "A: otra vez".
func (a *App) drawGameOver(dst *ebiten.Image, title, line string, best int, overAt float64) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	pw, ph := min(210, w-20), 78
	x, y := (w-pw)/2, (h-ph)/2
	rect(dst, x+3, y+3, pw, ph, th.Shadow)
	panel(dst, x, y, pw, ph, th.Bg2)
	rect(dst, x, y, pw, 1, th.Accent2)
	drawTextShadow(dst, title, BigFace, x+(pw-textWidth(title, BigFace))/2, y+8, th.Text, th.Shadow)
	drawText(dst, line, SmallFace, x+(pw-textWidth(line, SmallFace))/2, y+32, th.Accent)
	l2 := fmt.Sprintf("%s %d", T("tet_best"), best)
	drawText(dst, l2, SmallFace, x+(pw-textWidth(l2, SmallFace))/2, y+44, th.Dim)
	if a.t-overAt > 0.6 {
		l3 := T("cr_again")
		drawText(dst, l3, SmallFace, x+(pw-textWidth(l3, SmallFace))/2, y+60, th.Text)
	}
}
