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

// Naves: un matamarcianos al ritmo. La nave dispara sola en cada corchea de
// la canción (tres balas en abanico si el golpe trae graves); los enemigos
// llegan con los golpes, cada vez más, y unos disparan cada dos tiempos.
// Tres vidas; con la música en pausa, todo se queda quieto.

// shBody es algo que se mueve: una bala, un enemigo, una chispa.
type shBody struct {
	x, y, vx, vy float64
	kind         int // enemigos: 0 baja recto, 1 en zigzag, 2 dispara
	hp           int
	born         float64
	hitAt        float64
	c            int // chispas: qué color
}

// shooterGame es una partida.
type shooterGame struct {
	x, y       float64
	lives      int
	invulUntil float64
	shots      []shBody
	foes       []shBody
	foeShots   []shBody
	sparks     []shBody
	score      int
	best       int
	over       bool
	overAt     float64
	frozen     bool
	lastShot   float64 // reloj del ritmo
	lastBeat   float64
	beats      int
	fallback   float64
}

func (a *App) shooterBestPath() string { return filepath.Join(a.paths.Data, "naves.json") }

func (a *App) newShooter() *shooterGame {
	g := &shooterGame{x: float64(max(a.w, 240)) / 2, y: float64(max(a.h, ScreenH) - bottomH - 26), lives: 3, fallback: a.beatClock()}
	if b, err := os.ReadFile(a.shooterBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	g.lastShot, g.lastBeat = a.beatClock(), a.beatClock()
	return g
}

func (a *App) saveShooterBest() {
	g := a.shooter
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.shooterBestPath(), b, 0o644)
}

// updateShooter juega un fotograma.
func (a *App) updateShooter() {
	g, in := a.shooter, &a.input
	w := float64(a.w)
	top, bottom := float64(topH+2), float64(max(a.h, ScreenH)-bottomH-12) // la nave no pisa la ayuda
	switch {
	case in.Pressed(ActMenu):
		a.openShooterPause()
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
			a.saveShooterBest()
			a.shooter = a.newShooter()
		}
		return
	}
	if !a.hearing() && !a.offline {
		g.frozen = true
		return
	}
	// La nave.
	const speed = 2.2
	if in.duration(ActLeft) > 0 {
		g.x -= speed
	}
	if in.duration(ActRight) > 0 {
		g.x += speed
	}
	if in.duration(ActUp) > 0 {
		g.y -= speed
	}
	if in.duration(ActDown) > 0 {
		g.y += speed
	}
	g.x = math.Max(6, math.Min(w-6, g.x))
	g.y = math.Max(top+60, math.Min(bottom-6, g.y))
	// El ritmo: disparo en cada corchea; enemigos en cada golpe.
	per, anchor := a.steadyBeat()
	if per == 0 {
		per, anchor = 0.5, g.fallback
	}
	now := a.beatClock()
	if g.frozen || now-g.lastBeat > 4*per {
		g.frozen = false
		g.lastShot, g.lastBeat = now, now
	}
	half := anchor + math.Floor((now-anchor)/(per/2))*(per/2)
	if half > g.lastShot+per/4 {
		g.lastShot = half
		_, bass, _, _ := a.meterNow()
		g.shots = append(g.shots, shBody{x: g.x, y: g.y - 6, vy: -5})
		if bass > 0.55 { // con graves, en abanico
			g.shots = append(g.shots, shBody{x: g.x, y: g.y - 6, vx: -1.2, vy: -4.6}, shBody{x: g.x, y: g.y - 6, vx: 1.2, vy: -4.6})
		}
	}
	beat := anchor + math.Floor((now-anchor)/per)*per
	if beat > g.lastBeat+per/2 {
		g.lastBeat = beat
		g.beats++
		n := 1 + min(3, g.score/600) // más enemigos según los puntos
		for i := 0; i < n; i++ {
			kind := rand.IntN(3)
			if g.score < 150 {
				kind = rand.IntN(2)
			}
			e := shBody{x: 12 + rand.Float64()*(w-24), y: top - 8, vy: 0.6 + rand.Float64()*0.5, kind: kind, hp: 1, born: a.t}
			if kind == 2 {
				e.hp, e.vy = 2, 0.45
			}
			g.foes = append(g.foes, e)
		}
		// Los que disparan, cada dos tiempos.
		if g.beats%2 == 0 {
			for _, e := range g.foes {
				if e.kind == 2 && e.y > top {
					dx, dy := g.x-e.x, g.y-e.y
					d := math.Max(1, math.Hypot(dx, dy))
					g.foeShots = append(g.foeShots, shBody{x: e.x, y: e.y + 5, vx: dx / d * 1.8, vy: dy / d * 1.8})
				}
			}
		}
	}
	// Todo se mueve.
	move := func(list []shBody, keep func(*shBody) bool) []shBody {
		out := list[:0]
		for i := range list {
			b := &list[i]
			b.x += b.vx
			b.y += b.vy
			if keep(b) {
				out = append(out, *b)
			}
		}
		return out
	}
	g.shots = move(g.shots, func(b *shBody) bool { return b.y > top-10 })
	g.foeShots = move(g.foeShots, func(b *shBody) bool { return b.y < bottom+10 && b.x > -10 && b.x < w+10 })
	g.foes = move(g.foes, func(b *shBody) bool { return b.y < bottom+12 })
	for i := range g.foes {
		if g.foes[i].kind == 1 { // zigzag
			g.foes[i].x += 1.2 * math.Sin((a.t-g.foes[i].born)*4)
		}
	}
	g.sparks = move(g.sparks, func(b *shBody) bool { b.vy += 0.05; return a.t-b.born < 0.5 })
	// Choques: balas contra enemigos.
	for si := range g.shots {
		s := &g.shots[si]
		for fi := range g.foes {
			f := &g.foes[fi]
			if f.hp > 0 && math.Abs(s.x-f.x) < 6 && math.Abs(s.y-f.y) < 6 {
				f.hp--
				f.hitAt = a.t
				s.y = -100
				if f.hp == 0 {
					g.score += 10 * (1 + f.kind)
					a.sparkAt(f.x, f.y)
					a.sound("cr_kill", 2)
				}
			}
		}
	}
	alive := g.foes[:0]
	for _, f := range g.foes {
		if f.hp > 0 {
			alive = append(alive, f)
		}
	}
	g.foes = alive
	// Y contra la nave.
	if a.t >= g.invulUntil {
		hit := false
		for i := range g.foes {
			if math.Abs(g.foes[i].x-g.x) < 7 && math.Abs(g.foes[i].y-g.y) < 7 {
				g.foes[i].hp, hit = 0, true
			}
		}
		for i := range g.foeShots {
			if math.Abs(g.foeShots[i].x-g.x) < 4 && math.Abs(g.foeShots[i].y-g.y) < 5 {
				g.foeShots[i].y, hit = 1e9, true
			}
		}
		if hit {
			g.lives--
			g.invulUntil = a.t + 1.5
			a.sparkAt(g.x, g.y)
			a.sound("cr_hurt", 3)
			if g.lives <= 0 {
				g.over, g.overAt = true, a.t
				a.saveShooterBest()
				a.sound("cr_dead", 4)
			}
		}
	}
}

// sparkAt hace estallar unas chispas en x, y.
func (a *App) sparkAt(x, y float64) {
	g := a.shooter
	for i := 0; i < 10; i++ {
		an := float64(i) / 10 * 2 * math.Pi
		sp := 1 + rand.Float64()*1.5
		g.sparks = append(g.sparks, shBody{x: x, y: y, vx: math.Cos(an) * sp, vy: math.Sin(an) * sp, born: a.t, c: i % 3})
	}
}

func (a *App) openShooterPause() {
	g := a.shooter
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("sh_lives"), g.lives),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveShooterBest(); a.shooter = a.newShooter() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveShooterBest(); a.screen = Games }},
		},
	}
}

// drawShooter pinta la partida: estrellas que corren más con los graves, la
// nave, los enemigos, las balas y las chispas.
func (a *App) drawShooter(dst *ebiten.Image) {
	g, th := a.shooter, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, topH, w, h-topH-bottomH, withAlpha(th.Shadow, 0xb0))
	// Con el dedo: arrastrar mueve la nave tal cual (dispara sola); al
	// acabar, tocar empieza otra.
	if g.over {
		a.pressZone(0, topH, w, h-topH-bottomH, func(int, int) { a.input.inject(ActConfirm) })
	} else {
		a.dragArea(0, topH, w, h-topH-bottomH, func(dx, dy int) { g.x += float64(dx); g.y += float64(dy) })
	}
	_, bass, _, _ := a.meterNow()
	for i := 0; i < 50; i++ {
		f := float64(i)
		sp := 20 + math.Mod(f*17, 60)
		y := math.Mod(a.t*sp*(1+bass*1.5)+f*41, float64(h))
		x := math.Mod(f*83.7, float64(w))
		c := withAlpha(th.Text, uint8(40+math.Mod(f*29, 120)))
		rect(dst, int(x), int(y), 1, 1+int(bass*2), c)
	}
	for _, s := range g.shots {
		rect(dst, int(s.x)-1, int(s.y)-3, 2, 5, th.Text)
	}
	for _, s := range g.foeShots {
		rect(dst, int(s.x)-1, int(s.y)-1, 3, 3, th.Accent2)
	}
	for _, f := range g.foes {
		name := []string{"bug", "ufo", "ufo"}[f.kind]
		c := []color.RGBA{th.Accent2, th.Accent, mix(th.Accent, th.Text, 0.4)}[f.kind]
		if a.t-f.hitAt < 0.1 {
			c = th.Text
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(math.Round(f.x-5), math.Round(f.y-5))
		dst.DrawImage(crSprite(name, c, th.Shadow), op)
	}
	for _, s := range g.sparks {
		c := []color.RGBA{th.Accent, th.Accent2, th.Text}[s.c]
		rect(dst, int(s.x), int(s.y), 2, 2, c)
	}
	if !g.over && (a.t >= g.invulUntil || int(a.t*12)%2 == 0) {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(math.Round(g.x-5), math.Round(g.y-5))
		dst.DrawImage(crSprite("ship", th.Text, th.Accent), op)
	}
	// El marcador.
	for i := 0; i < g.lives; i++ {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(margin+i*11), float64(topH+3))
		dst.DrawImage(crSprite("heart", th.Accent2, th.Text), op)
	}
	sc := fmt.Sprintf("%d", g.score)
	drawTextShadow(dst, sc, SmallFace, w-margin-textWidth(sc, SmallFace), topH+5, th.Text, th.Shadow)
	if g.frozen && !g.over {
		a.drawCenterNote(dst, T("cr_play_music"))
	}
	if g.over {
		a.drawGameOver(dst, T("sh_over"), fmt.Sprintf("%s %d", T("tet_score"), g.score), max(g.best, g.score), g.overAt)
	}
	a.drawHints(dst, []hint{{"<>^v", T("sh_h_move")}, {"START", T("h_pause")}, {"SEL", T("h_menu")}})
}
