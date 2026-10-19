package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
)

// Duelo: batallas por turnos al ritmo. En tu turno, un cursor cruza la barra
// de ataque en dos golpes, empezando en uno, así su centro cae justo en el
// siguiente: A cerca del centro hace más daño, y en el golpe es crítico. En el del enemigo, durante ocho golpes, hay que esquivar con el
// corazón, dentro de la caja, las notas que lanza en cada golpe: el vinilo,
// anillos que giran; el casete, líneas de cinta con un hueco; el metrónomo,
// columnas que caen de un lado y del otro, como su péndulo. Al vencer a uno
// llega el siguiente, más duro. Las notas no vuelan a velocidad fija: dan un
// empujón en cada golpe y se frenan hasta el siguiente, y la batería de la
// canción manda refuerzos (la caja, notas que te buscan; los platillos, un
// anillo); el bombo sacude la caja. Con la música en pausa, todo se queda
// quieto.

const (
	duBoxW, duBoxH = 160, 80
	duTurnBeats    = 8   // lo que dura el turno del enemigo
	duHeartSpeed   = 1.6 // píxeles por fotograma
	duMaxHP        = 20
)

// Las fases de la batalla.
const (
	duIntro  = iota // el enemigo aparece y dice algo
	duAttack        // tu turno: la barra de ataque
	duResult        // el daño hecho, un momento
	duDodge         // su turno: esquivar
	duWin           // lo venciste
)

// duFoe es un tipo de enemigo.
type duFoe struct {
	name  string // clave de lang.go
	art   string // su dibujo (crArt)
	lines []string
	hp    int
}

var duFoes = []duFoe{
	{"du_vinyl", "du_vinyl", []string{"du_vinyl_1", "du_vinyl_2"}, 24},
	{"du_tape", "du_tape", []string{"du_tape_1", "du_tape_2"}, 32},
	{"du_metro", "du_metro", []string{"du_metro_1", "du_metro_2"}, 40},
}

// duBullet es una nota que vuela.
type duBullet struct{ x, y, vx, vy float64 }

// duelGame es una partida.
type duelGame struct {
	phase      int
	phaseAt    float64
	level      int // cuántos enemigos venciste
	foe        int
	foeHP      int
	foeMax     int
	hp         int
	hx, hy     float64 // el corazón, dentro de la caja
	bullets    []duBullet
	turnBeats  int
	lastBeat   float64
	cursor     float64 // la barra de ataque: 0..1
	lastDamage int
	crit       bool
	hurtAt     float64
	hitAt      float64 // cuándo recibió un golpe el enemigo
	invul      float64
	score      int
	best       int
	over       bool
	overAt     float64
	frozen     bool
	line       string
	fallback   float64
	swing      int                // el metrónomo: de qué lado cae ahora
	atkStart   float64            // el golpe en que el cursor sale (reloj del ritmo)
	seen       [drumCount]float64 // el último golpe de batería ya atendido
	kickAt     float64            // el último bombo, para sacudir la caja (reloj de la interfaz)
	snareAt    float64            // la última caja que lanzó notas (reloj del ritmo)
}

func (a *App) duelBestPath() string { return filepath.Join(a.paths.Data, "duelo.json") }

func (a *App) newDuel() *duelGame {
	g := &duelGame{hp: duMaxHP, fallback: a.beatClock()}
	if b, err := os.ReadFile(a.duelBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	a.duelFoe(g)
	return g
}

// duelFoe trae al siguiente enemigo, más duro en cada vuelta.
func (a *App) duelFoe(g *duelGame) {
	g.foe = g.level % len(duFoes)
	f := duFoes[g.foe]
	g.foeMax = f.hp + 12*(g.level/len(duFoes))
	g.foeHP = g.foeMax
	g.line = T(f.lines[0])
	g.phase, g.phaseAt = duIntro, a.t
	g.hx, g.hy = 0, 0
	g.bullets = nil
}

func (a *App) saveDuelBest() {
	g := a.duel
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.duelBestPath(), b, 0o644)
}

// duelGrid es la rejilla del ritmo (o un metrónomo de 120 si no hay tempo).
func (a *App) duelGrid(g *duelGame) (per, anchor float64) {
	per, anchor = a.steadyBeat()
	if per == 0 {
		per, anchor = 0.5, g.fallback
	}
	return per, anchor
}

// updateDuel juega un fotograma.
func (a *App) updateDuel() {
	g, in := a.duel, &a.input
	switch {
	case in.Pressed(ActMenu):
		a.openDuelPause()
		return
	case in.Pressed(ActPause):
		a.send("toggle")
	}
	if g.over {
		if in.Pressed(ActConfirm) && a.t-g.overAt > 0.8 {
			a.saveDuelBest()
			a.duel = a.newDuel()
		}
		return
	}
	if !a.isPlaying && !a.offline {
		g.frozen = true
		return
	}
	per, anchor := a.duelGrid(g)
	now := a.beatClock()
	if g.frozen {
		g.frozen = false
		g.lastBeat = now
	}
	beat := anchor + math.Floor((now-anchor)/per)*per
	newBeat := beat > g.lastBeat+per/2
	if newBeat {
		g.lastBeat = beat
	}
	switch g.phase {
	case duIntro:
		if a.t-g.phaseAt > 1.6 || (in.Pressed(ActConfirm) && a.t-g.phaseAt > 0.3) {
			a.duelAttack(g)
		}
	case duAttack:
		// El cursor cruza la barra en dos golpes, contados con el reloj del
		// ritmo: sale en un golpe y pasa por el centro en el siguiente.
		g.cursor = math.Max(0, math.Min(1, (now-g.atkStart)/(2*per)))
		if (in.Pressed(ActConfirm) && g.cursor > 0) || g.cursor >= 1 {
			a.duelStrike(g)
		}
	case duResult:
		if a.t-g.phaseAt > 0.9 {
			if g.foeHP == 0 {
				g.phase, g.phaseAt = duWin, a.t
				g.score += 500 * (g.level + 1)
				g.hp = min(duMaxHP, g.hp+6)
				a.sound("cr_stairs", 3)
				break
			}
			g.phase, g.phaseAt, g.turnBeats = duDodge, a.t, 0
			g.line = T(duFoes[g.foe].lines[rand.IntN(2)])
			g.bullets = nil
			g.hx, g.hy = 0, 0
		}
	case duDodge:
		a.duelDodge(g, newBeat, per, anchor, now)
	case duWin:
		if a.t-g.phaseAt > 1.8 {
			g.level++
			a.duelFoe(g)
		}
	}
}

// duelAttack empieza tu turno: el cursor saldrá en el próximo golpe (o en el
// otro, si el próximo está demasiado cerca para verlo venir).
func (a *App) duelAttack(g *duelGame) {
	per, anchor := a.duelGrid(g)
	now := a.beatClock()
	next := anchor + math.Ceil((now-anchor)/per)*per
	if next-now < per*0.4 {
		next += per
	}
	g.phase, g.phaseAt, g.cursor, g.atkStart = duAttack, a.t, 0, next
}

// duelStrike es el golpe de tu turno: cuanto más cerca del centro de la
// barra, más daño (nada si el cursor ya la cruzó entera), y en el golpe de
// la música, crítico.
func (a *App) duelStrike(g *duelGame) {
	off := math.Abs(g.cursor - 0.5) // 0 en el centro
	dmg := 0
	if g.cursor < 1 {
		dmg = max(1, int(math.Round(8*(1-off*2))))
	}
	g.crit = dmg > 0 && off < 0.2 && a.steadyOnBeat(0.1)
	if g.crit {
		dmg = dmg * 3 / 2
	}
	g.lastDamage = dmg
	g.foeHP = max(0, g.foeHP-dmg)
	g.score += dmg * 10
	if dmg > 0 {
		g.hitAt = a.t
		a.sound("cr_kill", 3)
	} else {
		a.sound("cr_miss", 2)
	}
	g.phase, g.phaseAt = duResult, a.t
}

// duelDodge es el turno del enemigo: el corazón se mueve, las notas salen en
// cada golpe y avanzan a empujones, uno por golpe; la batería manda más. Un
// roce quita vida.
func (a *App) duelDodge(g *duelGame, newBeat bool, per, anchor, now float64) {
	in := &a.input
	hw, hh := float64(duBoxW/2-5), float64(duBoxH/2-5)
	if in.duration(ActLeft) > 0 {
		g.hx -= duHeartSpeed
	}
	if in.duration(ActRight) > 0 {
		g.hx += duHeartSpeed
	}
	if in.duration(ActUp) > 0 {
		g.hy -= duHeartSpeed
	}
	if in.duration(ActDown) > 0 {
		g.hy += duHeartSpeed
	}
	g.hx = math.Max(-hw, math.Min(hw, g.hx))
	g.hy = math.Max(-hh, math.Min(hh, g.hy))
	if newBeat {
		g.turnBeats++
		if g.turnBeats > duTurnBeats {
			a.duelAttack(g)
			g.bullets = nil
			return
		}
		a.duelPattern(g, per)
	}
	a.duelDrums(g, per, now)
	// El empujón: rápido justo en el golpe, casi quietas al final. La media
	// por golpe es 1, así la dificultad no cambia.
	ph := math.Mod((now-anchor)/per, 1)
	if ph < 0 {
		ph++
	}
	surge := 0.3 + 3.5*math.Exp(-5*ph)
	speed := (1 + 0.15*float64(g.level)) * surge
	keep := g.bullets[:0]
	for _, b := range g.bullets {
		b.x += b.vx * speed
		b.y += b.vy * speed
		if math.Abs(b.x) > float64(duBoxW) || math.Abs(b.y) > float64(duBoxH) {
			continue
		}
		if a.t >= g.invul && math.Abs(b.x-g.hx) < 5 && math.Abs(b.y-g.hy) < 5 {
			g.hp -= 2 + g.level/3
			g.hurtAt, g.invul = a.t, a.t+0.8
			a.sound("cr_hurt", 3)
			if g.hp <= 0 {
				g.hp = 0
				g.over, g.overAt = true, a.t
				a.saveDuelBest()
				a.sound("cr_dead", 4)
			}
			continue
		}
		keep = append(keep, b)
	}
	g.bullets = keep
	if a.t-g.hurtAt > 0.8 && newBeat {
		g.score += 5 // sobrevivir un golpe también suma
	}
}

// duelDrums atiende los golpes de batería que reconoció el medidor: el bombo
// sacude la caja, la caja lanza notas que buscan el corazón desde las
// esquinas de arriba y los platillos, un abanico desde donde está el enemigo.
func (a *App) duelDrums(g *duelGame, per, now float64) {
	dr := &a.beat.drums
	var hit [drumCount]bool
	for k := range hit {
		if dr.last[k] != g.seen[k] {
			g.seen[k] = dr.last[k]
			hit[k] = now-dr.last[k] < 0.15 // uno viejo (de otra fase) no cuenta
		}
	}
	w, h := float64(duBoxW/2), float64(duBoxH/2)
	if hit[drumKick] || (a.beatHit && !dr.any) {
		g.kickAt = a.t
	}
	if hit[drumSnare] && now-g.snareAt > per/2 {
		g.snareAt = now
		corners := []float64{-w + 4, w - 4}
		if g.level == 0 {
			corners = corners[rand.IntN(2):][:1]
		}
		for _, x := range corners {
			dx, dy := g.hx-x, g.hy+h-4
			d := math.Max(1, math.Hypot(dx, dy))
			g.bullets = append(g.bullets, duBullet{x, -h + 4, dx / d * 1.1, dy / d * 1.1})
		}
	}
	if hit[drumCrash] {
		const n = 10
		for i := 0; i < n; i++ {
			an := math.Pi * (0.1 + 0.8*float64(i)/float64(n-1)) // hacia abajo, en abanico
			g.bullets = append(g.bullets, duBullet{0, -h - 2, math.Cos(an) * 0.8, math.Sin(an) * 0.8})
		}
	}
}

// duelPattern lanza las notas de un golpe, a la manera del enemigo.
func (a *App) duelPattern(g *duelGame, per float64) {
	w, h := float64(duBoxW/2), float64(duBoxH/2)
	_, bass, _, _ := a.meterNow()
	switch g.foe {
	case 0: // el vinilo: un anillo que sale del centro, girado cada vez
		n := 6 + g.level
		rot := float64(g.turnBeats) * 0.4
		for i := 0; i < n; i++ {
			an := rot + float64(i)*2*math.Pi/float64(n)
			g.bullets = append(g.bullets, duBullet{0, 0, math.Cos(an) * 0.9, math.Sin(an) * 0.9})
		}
	case 1: // el casete: una línea de cinta que barre, con un hueco
		gap := rand.Float64()*(2*h-24) - h + 12
		fromLeft := g.turnBeats%2 == 0
		for y := -h + 4; y < h; y += 8 {
			if math.Abs(y-gap) < 10 {
				continue
			}
			x, vx := -w-6, 1.3
			if !fromLeft {
				x, vx = w+6, -1.3
			}
			g.bullets = append(g.bullets, duBullet{x, y, vx, 0})
		}
	default: // el metrónomo: columnas que caen de un lado y del otro
		g.swing ^= 1
		cols := 3 + g.level/2
		for i := 0; i < cols; i++ {
			x := -w + 8 + float64(i)*(w-8)/float64(cols)
			if g.swing == 1 {
				x = -x
			}
			g.bullets = append(g.bullets, duBullet{x, -h - 4, 0, 1.1 + 0.3*float64(i%2)})
		}
	}
	if bass > 0.6 { // con graves, una nota que va directa al corazón
		dx, dy := g.hx, g.hy+h
		d := math.Max(1, math.Hypot(dx, dy))
		g.bullets = append(g.bullets, duBullet{0, -h, dx / d * 1.4, dy / d * 1.4})
	}
}

func (a *App) openDuelPause() {
	g := a.duel
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d · HP %d/%d", T("tet_score"), g.score, g.hp, duMaxHP),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveDuelBest(); a.duel = a.newDuel() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveDuelBest(); a.screen = Games }},
		},
	}
}

// drawDuel pinta la batalla: arriba el enemigo (que se mece con la música) y
// su vida; en medio, la caja (la barra de ataque o el corazón esquivando);
// abajo, lo que dice y tu vida.
func (a *App) drawDuel(dst *ebiten.Image) {
	g, th := a.duel, a.theme
	w := dst.Bounds().Dx()
	off := max(0, (dst.Bounds().Dy()-ScreenH)/2) // en vertical, centrado en el alto
	// Con el dedo: al esquivar, arrastrar mueve el corazón; en lo demás,
	// tocar es A (atacar, seguir), en el momento de tocar (el golpe va al
	// ritmo).
	if gh := dst.Bounds().Dy(); g.phase == duDodge && !g.over {
		a.dragArea(0, topH, w, gh-topH-bottomH, func(dx, dy int) { g.hx += float64(dx); g.hy += float64(dy) })
	} else {
		a.pressZone(0, topH, w, gh-topH-bottomH, func(int, int) { a.input.inject(ActConfirm) })
	}
	pulse := a.beatPulse()
	f := duFoes[g.foe]
	// El enemigo, al doble, meciéndose; parpadea al recibir un golpe.
	img := crSprite(f.art, th.Text, th.Text) // 1 bit: un solo color
	const k = 2
	iw, ih := img.Bounds().Dx()*k, img.Bounds().Dy()*k
	ex := w/2 - iw/2 + int(3*math.Sin(a.t*2))
	kick := math.Max(0, 1-(a.t-g.kickAt)/0.2) // el bombo: 1 justo al sonar
	ey := topH + 6 + off + int(-3*pulse-3*kick)
	if g.phase == duWin {
		ey += int(math.Min(1, (a.t-g.phaseAt)/1.2) * 40) // se hunde y se va
	}
	if !(a.t-g.hitAt < 0.3 && int(a.t*20)%2 == 0) && (g.phase != duWin || a.t-g.phaseAt < 1.2) {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(k, k)
		op.GeoM.Translate(float64(ex), float64(ey))
		if g.phase == duWin {
			op.ColorScale.ScaleAlpha(float32(math.Max(0, 1-(a.t-g.phaseAt)/1.2)))
		}
		dst.DrawImage(img, op)
		if g.foe == 2 { // la aguja del metrónomo, que va y viene con el ritmo
			per, anchor := a.duelGrid(g)
			ph := math.Mod((a.beatClock()-anchor)/per, 2)
			an := math.Sin(ph*math.Pi) * 0.5
			// Desde abajo de su ranura (columnas 10-11, filas 2-14 del dibujo).
			px, py := float64(ex+11*k), float64(ey+14*k)
			for r := 0.0; r < 24; r += 2 {
				rect(dst, int(px+math.Sin(an)*r)-1, int(py-math.Cos(an)*r), 2, 2, th.Accent2)
			}
			rect(dst, int(px+math.Sin(an)*20)-2, int(py-math.Cos(an)*20)-2, 5, 4, th.Accent2) // la pesa
		}
	}
	// Su nombre y su vida.
	name := T(f.name)
	ny := ey + ih + 4
	if g.phase == duWin {
		ny = topH + 6 + off + ih + 4
	}
	drawTextShadow(dst, name, SmallFace, w/2-textWidth(name, SmallFace)-4, ny, th.Text, th.Shadow)
	bw := 60
	rect(dst, w/2+4, ny+1, bw, 5, th.Bg2)
	rect(dst, w/2+4, ny+1, bw*g.foeHP/max(1, g.foeMax), 5, th.Accent)
	// La caja.
	// La caja late con el golpe (el borde se ilumina y engorda) y el bombo la
	// sacude un poco.
	bx, by := w/2-duBoxW/2, ny+14
	if g.phase == duDodge {
		bx += int(math.Round(2 * kick * math.Sin(a.t*90)))
	}
	bw2 := 2 + int(math.Round(pulse))
	rect(dst, bx-bw2, by-bw2, duBoxW+2*bw2, duBoxH+2*bw2, mix(th.Text, th.Accent2, pulse*0.7))
	rect(dst, bx, by, duBoxW, duBoxH, th.Shadow)
	cx, cy := float64(bx+duBoxW/2), float64(by+duBoxH/2)
	switch g.phase {
	case duIntro, duWin:
		msg := g.line
		if g.phase == duWin {
			msg = fmt.Sprintf(T("du_win"), T(f.name))
		}
		for i, l := range wrapLines(msg, SmallFace, duBoxW-12) {
			drawText(dst, l, SmallFace, bx+6, by+8+i*11, th.Text)
		}
	case duAttack, duResult:
		// La barra de ataque: el centro, más claro; el cursor la cruza.
		for x := 0; x < duBoxW-16; x++ {
			d := math.Abs(float64(x)/float64(duBoxW-16) - 0.5)
			c := mix(th.Accent, th.Bg2, d*2)
			rect(dst, bx+8+x, by+duBoxH/2-8, 1, 16, c)
		}
		// Marcas de cada golpe (inicio, centro, final) y de las corcheas.
		for i := 0; i <= 4; i++ {
			mx, mh := bx+8+i*(duBoxW-16)/4, 14
			if i%2 == 0 {
				mh = 20
			}
			rect(dst, mx, by+duBoxH/2-mh/2, 1, mh, th.Text)
		}
		curX := bx + 8 + int(g.cursor*float64(duBoxW-16))
		if g.phase == duResult || g.cursor > 0 || int(a.t*8)%2 == 0 { // esperando el golpe, parpadea
			rect(dst, curX-1, by+duBoxH/2-12, 3, 24, th.Text)
		}
		if g.phase == duResult {
			msg := fmt.Sprintf("-%d", g.lastDamage)
			switch {
			case g.lastDamage == 0:
				msg = "MISS"
			case g.crit:
				msg = fmt.Sprintf("-%d  %s", g.lastDamage, T("du_crit"))
			}
			drawTextShadow(dst, msg, BigFace, w/2-textWidth(msg, BigFace)/2, by+4, th.Accent2, th.Shadow)
		}
	case duDodge:
		// Las notas, solo dentro de la caja.
		box := dst.SubImage(image.Rect(bx, by, bx+duBoxW, by+duBoxH)).(*ebiten.Image)
		for _, b := range g.bullets {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(math.Round(cx+b.x-3), math.Round(cy+b.y-3))
			op.ColorScale.ScaleWithColor(th.Text)
			box.DrawImage(icon("note"), op)
		}
		if a.t >= g.invul || int(a.t*14)%2 == 0 {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(math.Round(cx+g.hx-5), math.Round(cy+g.hy-5))
			dst.DrawImage(crSprite("heart", th.Accent2, th.Text), op)
		}
	}
	// Tu vida y los puntos.
	hy := by + duBoxH + 8
	hp := fmt.Sprintf("HP %d/%d", g.hp, duMaxHP)
	drawText(dst, hp, SmallFace, bx, hy, th.Text)
	rect(dst, bx+textWidth(hp, SmallFace)+6, hy+1, 40, 5, th.Bg2)
	rect(dst, bx+textWidth(hp, SmallFace)+6, hy+1, 40*g.hp/duMaxHP, 5, th.Accent2)
	sc := fmt.Sprintf("%d", g.score)
	drawText(dst, sc, SmallFace, bx+duBoxW-textWidth(sc, SmallFace), hy, th.Accent)
	if g.frozen && !g.over {
		a.drawCenterNote(dst, T("cr_play_music"))
	}
	if g.over {
		a.drawGameOver(dst, T("du_lost"), fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("du_beaten"), g.level), max(g.best, g.score), g.overAt)
	}
	hints := []hint{{"A", T("du_h_hit")}, {"START", T("h_pause")}}
	if g.phase == duDodge {
		hints = []hint{{"<>^v", T("du_h_dodge")}, {"START", T("h_pause")}}
	}
	a.drawHints(dst, hints)
}
