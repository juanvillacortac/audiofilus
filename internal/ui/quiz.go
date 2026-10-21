package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// ¿Qué canción es?: suenan unos segundos de una canción al azar de la
// biblioteca (desde la mitad, donde suele estar lo reconocible) y hay que
// elegirla entre cuatro, con la cruceta y A. Diez rondas de diez segundos:
// cuanto antes, más puntos, y la racha multiplica. Se juega con la música de
// la tarjeta (no la de los servidores ni la de YouTube): la canción suena por
// el servicio, como cualquier otra.

const (
	quizRounds = 10
	quizTime   = 10.0 // segundos por ronda
	quizSeek   = "35" // dónde empieza a sonar cada canción
)

// quizGame es una partida.
type quizGame struct {
	pool       []Song
	options    [4]Song
	answer     int
	sel        int
	round      int
	startAt    float64
	chosen     int // -1 sin contestar, -2 se acabó el tiempo, o la opción
	answeredAt float64
	score      int
	streak     int
	best       int
	hits       int
	over       bool
	overAt     float64
}

func (a *App) quizBestPath() string { return filepath.Join(a.paths.Data, "quiz.json") }

// newQuiz prepara una partida con las canciones de la tarjeta (la primera
// ronda la pide quien la guarda en a.quiz, con quizRound).
func (a *App) newQuiz() *quizGame {
	g := &quizGame{chosen: -1}
	if b, err := os.ReadFile(a.quizBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	if a.local != nil {
		for _, s := range a.local.Songs {
			if s.Title != "" && !strings.Contains(s.Path, "://") {
				g.pool = append(g.pool, s)
			}
		}
	}
	return g
}

// saveQuizBest guarda el récord.
func (a *App) saveQuizBest() {
	g := a.quiz
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.quizBestPath(), b, 0o644)
}

// quizRound empieza una ronda: la canción y tres más con otro título, en un
// orden al azar, y que suene.
func (a *App) quizRound() {
	g := a.quiz
	if g == nil || len(g.pool) < 4 {
		return
	}
	pool := g.pool
	ans := pool[rand.IntN(len(pool))]
	opts := []Song{ans}
	for tries := 0; len(opts) < 4 && tries < 200; tries++ {
		s := pool[rand.IntN(len(pool))]
		dup := false
		for _, o := range opts {
			dup = dup || strings.EqualFold(o.Title, s.Title)
		}
		if !dup {
			opts = append(opts, s)
		}
	}
	rand.Shuffle(len(opts), func(i, j int) { opts[i], opts[j] = opts[j], opts[i] })
	for i := range g.options {
		g.options[i] = opts[i%len(opts)]
		if opts[i%len(opts)].Path == ans.Path {
			g.answer = i
		}
	}
	g.chosen, g.startAt, g.sel = -1, a.t, 0
	a.send("load", "0", ans.Path)
	a.send("seek", quizSeek)
}

// updateQuiz juega un fotograma.
func (a *App) updateQuiz() {
	g, in := a.quiz, &a.input
	if in.Pressed(ActMenu) {
		a.openQuizPause()
		return
	}
	if len(g.pool) < 4 {
		if in.Pressed(ActBack) || in.Pressed(ActConfirm) {
			a.screen = Games
		}
		return
	}
	if g.over {
		if in.Pressed(ActConfirm) && a.t-g.overAt > 0.6 {
			a.quiz = a.newQuiz()
			a.quizRound()
		}
		if in.Pressed(ActBack) {
			a.screen = Games
		}
		return
	}
	if g.chosen == -1 {
		switch {
		case in.Pressed(ActLeft), in.Pressed(ActRight):
			g.sel ^= 1
		case in.Pressed(ActUp), in.Pressed(ActDown):
			g.sel ^= 2
		case in.Pressed(ActConfirm):
			g.chosen, g.answeredAt = g.sel, a.t
			if g.sel == g.answer {
				left := math.Max(0, quizTime-(a.t-g.startAt))
				g.streak++
				g.hits++
				g.score += (20 + int(left*10)) * (1 + (g.streak-1)/3)
				a.sound("cr_coin", 3)
			} else {
				g.streak = 0
				a.sound("cr_hurt", 3)
			}
		}
		if g.chosen == -1 && a.t-g.startAt > quizTime {
			g.chosen, g.answeredAt, g.streak = -2, a.t, 0
			a.sound("cr_miss", 3)
		}
		return
	}
	if a.t-g.answeredAt > 1.8 {
		g.round++
		if g.round >= quizRounds {
			g.over, g.overAt = true, a.t
			a.saveQuizBest()
			a.send("pause")
			return
		}
		a.quizRound()
	}
}

// openQuizPause es el menú de pausa.
func (a *App) openQuizPause() {
	g := a.quiz
	a.menu = &menu{
		title:    T("qz_title_menu"),
		subtitle: fmt.Sprintf("%s %d/%d · %s %d", T("qz_round"), min(g.round+1, quizRounds), quizRounds, T("tet_score"), g.score),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveQuizBest(); a.quiz = a.newQuiz(); a.quizRound() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveQuizBest(); a.screen = Games }},
		},
	}
}

// drawQuiz pinta la ronda.
func (a *App) drawQuiz(dst *ebiten.Image) {
	g, th := a.quiz, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	if len(g.pool) < 4 {
		msg := T("qz_need_songs")
		drawTextShadow(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2, th.Text, th.Shadow)
		a.drawHints(dst, []hint{{"B", T("h_back")}})
		return
	}
	y := topH + 6
	head := fmt.Sprintf("%s %d/%d", T("qz_round"), min(g.round+1, quizRounds), quizRounds)
	drawText(dst, head, SmallFace, margin, y, th.Dim)
	sc := fmt.Sprintf("%d", g.score)
	if m := 1 + (g.streak-1)/3; g.streak > 0 && m > 1 {
		sc = fmt.Sprintf("x%d  %d", m, g.score)
	}
	drawText(dst, sc, SmallFace, w-margin-textWidth(sc, SmallFace), y, th.Accent)
	y += 12
	// El tiempo que queda.
	left := quizTime
	if g.chosen == -1 {
		left = math.Max(0, quizTime-(a.t-g.startAt))
	}
	bw := w - 2*margin
	rect(dst, margin, y, bw, 3, th.Bg2)
	tc := th.Accent
	if left < 3 {
		tc = th.Accent2
	}
	rect(dst, margin, y, int(float64(bw)*left/quizTime), 3, tc)
	y += 9
	if g.over {
		a.drawQuizOver(dst)
		return
	}
	// La pregunta, que late con la música.
	q := T("qz_question")
	pulse := a.beatPulse()
	drawTextShadow(dst, q, BigFace, (w-textWidth(q, BigFace))/2, y+int(-2*pulse), th.Text, th.Shadow)
	y += 22
	// Las cuatro opciones, en dos por dos.
	gap := 6
	cw := (w - 2*margin - gap) / 2
	ch := (h - bottomH - 6 - y - gap) / 2
	for i, s := range g.options {
		x := margin + (i%2)*(cw+gap)
		yy := y + (i/2)*(ch+gap)
		bg := th.Bg2
		border := withAlpha(th.Text, 0x30)
		switch {
		case g.chosen != -1 && i == g.answer:
			bg, border = mix(th.Bg2, th.Accent, 0.45), th.Accent
		case g.chosen == i:
			bg, border = mix(th.Bg2, th.Shadow, 0.5), th.Accent2
		case g.chosen == -1 && i == g.sel:
			bg, border = mix(th.Bg2, th.Accent, 0.18), th.Accent
		}
		panel(dst, x, yy, cw, ch, bg)
		frame(dst, x-1, yy-1, cw+2, ch+2, border)
		// Tocar una opción es elegirla y responder.
		if g.chosen == -1 {
			idx := i
			a.tapZone(x, yy, cw, ch, func() { g.sel = idx; a.input.inject(ActConfirm) })
		}
		cs := min(ch-8, 36)
		al := a.local.byPath[s.Path]
		a.drawCover(dst, al, x+4, yy+(ch-cs)/2, cs, 1)
		tx := x + cs + 10
		tw := cw - cs - 14
		drawTextW(dst, s.Title, SmallFace, tx, yy+ch/2-9, tw, th.Text)
		drawTextW(dst, s.Artist, SmallFace, tx, yy+ch/2+2, tw, th.Dim)
	}
	if g.chosen == -2 {
		msg := T("qz_time_up")
		drawTextShadow(dst, msg, BigFace, (w-textWidth(msg, BigFace))/2, h/2, th.Accent2, th.Shadow)
	}
	a.drawHints(dst, []hint{{"<>^v", T("gm_choose")}, {"A", T("qz_answer")}, {"START", T("h_pause")}})
}

// drawQuizOver es el cartel del final.
func (a *App) drawQuizOver(dst *ebiten.Image) {
	g, th := a.quiz, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	pw, ph := min(220, w-20), 96
	x, y := (w-pw)/2, (h-ph)/2
	rect(dst, x+3, y+3, pw, ph, th.Shadow)
	panel(dst, x, y, pw, ph, th.Bg2)
	rect(dst, x, y, pw, 1, th.Accent)
	t := T("qz_done")
	drawTextShadow(dst, t, BigFace, x+(pw-textWidth(t, BigFace))/2, y+8, th.Text, th.Shadow)
	l1 := fmt.Sprintf(T("qz_hits"), g.hits, quizRounds)
	drawText(dst, l1, SmallFace, x+(pw-textWidth(l1, SmallFace))/2, y+34, th.Accent)
	l2 := fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("tet_best"), max(g.best, g.score))
	drawText(dst, l2, SmallFace, x+(pw-textWidth(l2, SmallFace))/2, y+48, th.Dim)
	if a.t-g.overAt > 0.6 {
		l3 := T("cr_again")
		drawText(dst, l3, SmallFace, x+(pw-textWidth(l3, SmallFace))/2, y+72, th.Text)
	}
	a.drawHints(dst, []hint{{"A", T("gm_play")}, {"B", T("h_back")}})
}
