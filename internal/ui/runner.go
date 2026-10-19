package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
)

// Carrera: un corredor al ritmo, a la manera del dinosaurio de Chromium. Un
// choque y se acaba; A (o ↑) salta, y mantenerlo salta más alto (soltarlo
// pronto, un saltito); ↓ agacha, y en el aire hace caer rápido. El mundo
// avanza un tanto fijo por golpe, cada vez algo más rápido, y lo que viene
// llega justo en los golpes de la batería: el bombo trae bafles (uno, dos o
// uno grande), la caja vinilos que vuelan a tres alturas (bajo: se salta;
// medio: se agacha; alto: pasa por encima). Las notas solo van donde se pueden
// coger: sobre el arco de un salto o a ras de suelo, en los charles. Saltar en
// la corchea de antes del obstáculo (el "y" del compás) es PERFECTO y sube el
// multiplicador. Detrás, el plasma de los visualizadores en la paleta de la
// portada, y un horizonte que vibra con el espectro. Con la música en pausa,
// todo se queda quieto.

const (
	ruJumpH  = 30.0 // la altura de un salto entero (a media resolución)
	ruHeroX  = 30.0 // dónde corre el héroe
	ruMinHop = 10.0 // lo mínimo que sube aunque se suelte enseguida
)

// Lo que hay en el camino.
const (
	ruSpeaker = iota // un bafle (n: cuántos juntos; big: el grande)
	ruVinyl          // un vinilo que vuela (alt: 0 bajo, 1 medio, 2 alto)
	ruNote           // una nota que se coge (h: su altura)
	ruTick           // la marca de un golpe en el suelo (big: la del compás)
)

// ruVinylY es la altura sobre el suelo de un vinilo en cada altura.
var ruVinylY = [3]float64{2, 10, 21}

// ruThing es algo del camino. Se planea para llegar al héroe en la hora at
// (reloj del ritmo), pero se dibuja y choca por su sitio en el mundo, d (en
// píxeles, como run): así, si cambia el tempo detectado o la velocidad, el
// escenario no pega saltos.
type ruThing struct {
	kind int
	at   float64
	d    float64
	n    int
	big  bool
	alt  int
	h    float64
	done bool // ya pasó (o se cogió): no vuelve a contar
}

// Los dibujos de Carrera ('#' el color, 'o' su tono oscuro, '*' el brillo):
// el corredor con sus cascos, los bafles y el vinilo.
func init() {
	head := []string{
		"...#####....",
		"..#######...",
		".o##*####...",
		".o#######...",
		"..######....",
		"...###......",
	}
	body := []string{
		"..######.#..",
		".#.#####.#..",
		"...#####....",
	}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	crArt["ru_run1"] = join(head, body, []string{
		"...##.##....",
		"..##...##...",
		"..#.....#...",
	})
	crArt["ru_run2"] = join(head, body, []string{
		"....###.....",
		"....##......",
		"....#.#.....",
	})
	crArt["ru_jump"] = join(head[:5], []string{
		"#..###...#..",
		".#######.#..",
		"...#####.#..",
		"...#####....",
		"..##..##....",
		".##....##...",
		"............",
	})
	crArt["ru_dead"] = join([]string{
		"...#####....",
		"..#######...",
		".o#*.*###...",
		".o##*####...",
		"..#*.*##....",
		"...###......",
	}, body, []string{
		"...##.##....",
		"..##...##...",
		"..#.....#...",
	})
	crArt["ru_duck1"] = []string{
		".........#####..",
		"........o##*###.",
		"..#####.o######.",
		".##############.",
		"#.############..",
		"..##.##...##.#..",
		".##...##.##...#.",
	}
	crArt["ru_duck2"] = []string{
		".........#####..",
		"........o##*###.",
		"..#####.o######.",
		".##############.",
		"#.############..",
		"...###.....###..",
		"...#.#.....#.#..",
	}
	crArt["ru_speaker"] = []string{
		"########",
		"#oooooo#",
		"#oo##oo#",
		"#o#**#o#",
		"#oo##oo#",
		"#oooooo#",
		"#o####o#",
		"#o#oo#o#",
		"#o#**#o#",
		"#o#oo#o#",
		"#o####o#",
		"########",
	}
	crArt["ru_amp"] = []string{
		"##########",
		"#*o*o*o*o#",
		"##########",
		"#oooooooo#",
		"#ooo##ooo#",
		"#oo#**#oo#",
		"#ooo##ooo#",
		"#oooooooo#",
		"#oo####oo#",
		"#o#oooo#o#",
		"#o#o**o#o#",
		"#o#o**o#o#",
		"#o#oooo#o#",
		"#oo####oo#",
		"#oooooooo#",
		"##########",
		".#......#.",
		".#......#.",
	}
	crArt["ru_vinyl"] = []string{
		"...####...",
		".##oooo##.",
		".#oo##oo#.",
		"#oo#oo#oo#",
		"#o#o**o#o#",
		"#o#o**o#o#",
		"#oo#oo#oo#",
		".#oo##oo#.",
		".##oooo##.",
		"...####...",
	}
}

// runnerGame es una partida.
type runnerGame struct {
	things    []ruThing
	planned   float64 // hasta qué corchea se puso el camino
	eighths   int     // corcheas planeadas, para empezar suave
	y, vy     float64 // la altura sobre el suelo y su velocidad (px/s)
	jumpAt    float64 // cuándo empezó el salto (0 si está en el suelo)
	held      bool    // A sigue pulsado: el salto sube entero
	duck      bool    // ↓: agachado (o, en el aire, cayendo rápido)
	score     int
	bonus     int
	best      int
	mult      int
	perfectAt float64 // reloj de la interfaz, para el cartel
	over      bool
	overAt    float64
	frozen    bool
	fallback  float64
	run       float64 // lo recorrido, en píxeles (los puntos y el suelo)
	speed     float64 // a cuántos píxeles por segundo avanza el mundo
	lastNow   float64
	canvas    *ebiten.Image // el mundo, a media resolución
	sky       *ebiten.Image // el cielo en intensidad
	skyOut    *ebiten.Image // y ya en la paleta
}

func (a *App) runnerBestPath() string { return filepath.Join(a.paths.Data, "carrera.json") }

func (a *App) newRunner() *runnerGame {
	g := &runnerGame{mult: 1, fallback: a.beatClock()}
	if b, err := os.ReadFile(a.runnerBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	return g
}

func (a *App) saveRunnerBest() {
	g := a.runner
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.runnerBestPath(), b, 0o644)
}

// runnerGrid es la rejilla del ritmo (o un metrónomo de 120 si no hay tempo).
func (a *App) runnerGrid(g *runnerGame) (per, anchor float64) {
	per, anchor = a.steadyBeat()
	if per == 0 {
		per, anchor = 0.5, g.fallback
	}
	return per, anchor
}

// runnerJump salta, si está en el suelo. Un salto entero (con A pulsado) sube
// ruJumpH y dura un golpe.
func (a *App) runnerJump(g *runnerGame) {
	if g.y > 0 || g.jumpAt != 0 {
		return
	}
	per, _ := a.runnerGrid(g)
	g.vy = 4 * ruJumpH / per
	g.jumpAt = a.beatClock()
	g.held = true
}

// updateRunner juega un fotograma.
func (a *App) updateRunner() {
	g, in := a.runner, &a.input
	switch {
	case in.Pressed(ActMenu):
		a.openRunnerPause()
		return
	case in.Pressed(ActPause):
		a.send("toggle")
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	}
	if g.over {
		if (in.Pressed(ActConfirm) || in.Pressed(ActUp)) && a.t-g.overAt > 0.6 {
			a.saveRunnerBest()
			a.runner = a.newRunner()
		}
		return
	}
	if !a.isPlaying && !a.offline {
		g.frozen = true
		return
	}
	if in.Pressed(ActConfirm) || in.Pressed(ActUp) {
		a.runnerJump(g)
	}
	g.held = in.duration(ActConfirm) > 0 || in.duration(ActUp) > 0
	g.duck = in.duration(ActDown) > 0
	a.stepRunner(g)
}

// stepRunner avanza el mundo: el salto, el camino que entra por la derecha y
// lo que le pasa al héroe.
func (a *App) stepRunner(g *runnerGame) {
	per, anchor := a.runnerGrid(g)
	now := a.beatClock()
	if g.frozen || g.planned == 0 || now-g.lastNow > 1 {
		// Al empezar o al volver de una pausa: el camino de antes ya no
		// cuadra con el reloj, se empieza de nuevo un poco más allá.
		g.frozen = false
		g.things = g.things[:0]
		g.y, g.vy, g.jumpAt = 0, 0, 0
		g.planned = anchor + math.Ceil((now-anchor)/per)*per + 2*per
		g.lastNow = now
	}
	dt := math.Min(0.05, now-g.lastNow)
	g.lastNow = now
	// Cada vez más rápido (como el dinosaurio): de 36 a 56 píxeles por
	// golpe según lo recorrido. La velocidad va hacia la de ahora poco a
	// poco: ni un cambio de tempo ni de ritmo la hacen saltar.
	beatPx := math.Min(56, 36+g.run/650)
	if want := beatPx / per; g.speed == 0 {
		g.speed = want
	} else {
		g.speed += (want - g.speed) * math.Min(1, 1.5*dt)
	}
	g.run += g.speed * dt
	g.score = int(g.run/10) + g.bonus
	// El salto: gravedad para que uno entero dure un golpe; al soltar A
	// pronto, deja de subir (tras lo mínimo); ↓ en el aire, cae rápido.
	if g.jumpAt != 0 {
		grav := 8 * ruJumpH / (per * per)
		if !g.held && g.vy > 0 && g.y >= ruMinHop {
			g.vy = math.Min(g.vy, ruJumpH/per)
		}
		if g.duck {
			grav *= 3
		}
		g.vy -= grav * dt
		g.y += g.vy * dt
		if g.y <= 0 {
			g.y, g.vy, g.jumpAt = 0, 0, 0
		}
	}
	// El camino, hasta lo que se ve más un poco.
	// Cada corchea, en la rejilla de ahora (si el tempo cambió, la
	// siguiente se ajusta a él).
	cw := float64(max(a.w, 240)) / 2
	for (g.planned-now)*g.speed < cw-ruHeroX+24+per*g.speed {
		t := anchor + math.Round((g.planned-anchor)/(per/2))*(per/2)
		a.runnerPlan(g, t, per, anchor)
		g.planned = t + per/2
		g.eighths++
	}
	// Lo que llega al héroe: su caja, algo menor que el dibujo (como en el
	// dinosaurio, rozar un borde no mata).
	hx0, hx1, hy1 := ruHeroBox(g.y, g.duck)
	keep := g.things[:0]
	for _, th := range g.things {
		x := ruHeroX + th.d - g.run
		if x < -30 {
			continue
		}
		if th.kind == ruTick {
			keep = append(keep, th)
			continue
		}
		if !th.done {
			x0, x1, y0, y1 := th.box(x)
			switch {
			case x1 > hx0 && x0 < hx1 && y1 > g.y && y0 < hy1:
				th.done = true
				if th.kind == ruNote {
					g.bonus += 5 * g.mult
					a.sound("cr_coin", 2)
				} else {
					a.runnerCrash(g)
				}
			case th.kind != ruNote && x1 < hx0:
				// Lo pasó. Si era de saltar y lo pasó en lo alto del salto
				// (saltó en la corchea de antes), perfecto.
				th.done = true
				if th.kind == ruSpeaker || th.alt == 0 {
					crossed := now - (ruHeroX-x)/g.speed // cuando su centro pasó por el héroe
					if g.jumpAt != 0 && math.Abs(g.jumpAt+per/2-crossed) < per*0.15 {
						g.mult = min(8, g.mult+1)
						g.perfectAt = a.t
						g.bonus += 10 * g.mult
					} else {
						g.mult = 1
					}
				}
			}
		}
		keep = append(keep, th)
	}
	g.things = keep
}

// ruHeroBox es la caja de choque del héroe a la altura y: sus bordes y su
// techo (de pie, 12; agachado en el suelo, 6 y más largo).
func ruHeroBox(y float64, duck bool) (x0, x1, top float64) {
	if duck && y == 0 {
		return ruHeroX - 5, ruHeroX + 8, 6
	}
	return ruHeroX - 4, ruHeroX + 3, y + 12
}

// box es la caja de choque de algo del camino cuando está en x (desde el
// suelo: y0 abajo, y1 arriba).
func (th ruThing) box(x float64) (x0, x1, y0, y1 float64) {
	switch th.kind {
	case ruSpeaker:
		if th.big {
			return x - 4, x + 4, 0, 16
		}
		half := float64(th.n*8) / 2
		return x - half + 1, x + half - 1, 0, 11
	case ruVinyl:
		y := ruVinylY[th.alt]
		return x - 4, x + 4, y + 1, y + 9
	default:
		return x - 3, x + 3, th.h - 3, th.h + 4
	}
}

// runnerPlan pone lo que toca en la corchea t: lo que tocó la batería en esa
// corchea del compás en los últimos compases (como Ritmo); sin batería
// reconocida, al azar en los golpes.
func (a *App) runnerPlan(g *runnerGame, t, per, anchor float64) {
	n := int(math.Round((t - anchor) / (per / 2)))
	onBeat := n%2 == 0
	// Todo lo que se pone aquí, en su sitio del mundo.
	from := len(g.things)
	defer func() {
		for i := from; i < len(g.things); i++ {
			g.things[i].d = g.run + (t-g.lastNow)*g.speed
		}
	}()
	if onBeat {
		g.things = append(g.things, ruThing{kind: ruTick, at: t, big: (n%8+8)%8 == 0})
	}
	if g.eighths < 8 { // el primer compás, libre: para entrar en ritmo
		return
	}
	var hits [drumCount]bool
	if dr := &a.beat.drums; dr.any && a.beat.per > 0 {
		hits = dr.at(a.beat.slot(t), a.beatClock(), per)
	} else if onBeat {
		r := rand.Float64()
		hits[drumKick] = r < 0.45
		hits[drumSnare] = r >= 0.45 && r < 0.65
	} else {
		hits[drumHat] = rand.Float64() < 0.4
	}
	// Lo último del camino: tras un obstáculo, golpe y medio libre (un salto
	// entero y la caída), y las notas del suelo lejos de todo.
	lastObs, lastAny := -1e9, -1e9
	for _, th := range g.things {
		switch th.kind {
		case ruTick:
			continue
		case ruSpeaker, ruVinyl:
			lastObs = math.Max(lastObs, th.at)
		}
		lastAny = math.Max(lastAny, th.at)
	}
	chance := math.Min(0.9, 0.45+float64(g.score)/1500)
	free := t-lastObs >= per*1.49
	switch {
	case onBeat && free && hits[drumSnare] && g.eighths > 24 && rand.Float64() < chance:
		// Un vinilo: casi siempre a media altura (agacharse); a veces bajo
		// (saltarlo) o alto (pasa por encima: solo asusta).
		alt := 1
		switch r := rand.Float64(); {
		case r < 0.3:
			alt = 0
		case r < 0.42:
			alt = 2
		}
		g.things = append(g.things, ruThing{kind: ruVinyl, at: t, alt: alt})
	case onBeat && free && (hits[drumKick] || hits[drumCrash]) && rand.Float64() < chance:
		th := ruThing{kind: ruSpeaker, at: t, n: 1}
		switch r := rand.Float64(); {
		case hits[drumCrash] || (g.score > 250 && r < 0.2):
			th.big = true
		case g.score > 120 && r < 0.45:
			th.n = 2
		}
		g.things = append(g.things, th)
		// Encima, en lo alto del salto entero, una nota: premio por saltar
		// en el "y" y mantener A.
		if rand.Float64() < 0.5 {
			g.things = append(g.things, ruThing{kind: ruNote, at: t, h: ruJumpH + 5})
		}
	case !onBeat && hits[drumHat] && t-lastAny >= per && t-lastObs >= per:
		// A ras de suelo, sin obstáculo cerca: se coge corriendo.
		g.things = append(g.things, ruThing{kind: ruNote, at: t, h: 4})
	}
}

// runnerCrash es un choque: se acabó.
func (a *App) runnerCrash(g *runnerGame) {
	g.over, g.overAt = true, a.t
	a.saveRunnerBest()
	a.sound("cr_dead", 4)
}

func (a *App) openRunnerPause() {
	g := a.runner
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d", T("tet_score"), g.score),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveRunnerBest(); a.runner = a.newRunner() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveRunnerBest(); a.screen = Games }},
		},
	}
}

// drawRunner pinta la carrera a media resolución, ampliada al doble: el
// cielo de plasma, el horizonte que vibra con el espectro, el suelo con sus
// piedritas y sus marcas de golpe, lo que viene y el héroe; el marcador va
// encima, a resolución normal.
func (a *App) drawRunner(dst *ebiten.Image) {
	g, th := a.runner, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	top := topH + 1
	cw, ch := w/2, (h-bottomH-top)/2
	if cw < 16 || ch < 24 {
		return
	}
	fitImg(&g.canvas, cw, ch)
	cv := g.canvas
	cv.Fill(th.Shadow)
	ground := ch - 12
	per, anchor := a.runnerGrid(g)
	now := a.beatClock()
	pulse := a.beatPulse()
	_, _, bands, _ := a.meterNow()
	// El cielo: el plasma de los visualizadores en la paleta de la portada,
	// tenue para que lo de delante se lea, y más vivo en cada golpe.
	fitImg(&g.sky, cw, ground)
	fitImg(&g.skyOut, cw, ground)
	a.plasmaInto(g.sky, 1.6)
	pop := &ebiten.DrawRectShaderOptions{}
	pop.Images[0] = g.sky
	pop.Uniforms = map[string]any{"Bg": vec3(th.Shadow), "Mid": vec3(th.Accent), "Hi": vec3(th.Accent2)}
	g.skyOut.DrawRectShader(cw, ground, paletteShader, pop)
	sop := &ebiten.DrawImageOptions{}
	sop.ColorScale.ScaleAlpha(float32(0.32 + 0.2*pulse))
	cv.DrawImage(g.skyOut, sop)
	// El horizonte: una línea que vibra con el espectro (de graves a agudos,
	// de izquierda a derecha), como un osciloscopio tendido en el suelo.
	line := mix(th.Text, th.Accent2, pulse*0.5)
	prev := ground
	for x := 0; x < cw; x++ {
		f := float64(x) / float64(cw)
		y := ground - int(math.Round(2.5*bandAt(bands, f)*math.Abs(math.Sin(float64(x)*0.7+a.t*9))))
		lo, hi := min(prev, y), max(prev, y)
		rect(cv, x, lo, 1, hi-lo+1, line)
		prev = y
	}
	// Las piedritas del suelo, que pasan con el mundo.
	for x := 0; x < cw; x++ {
		k := int(math.Floor(float64(x) + g.run))
		switch {
		case k%37 == 0:
			rect(cv, x, ground+3, 2, 1, withAlpha(th.Text, 0x90))
		case k%23 == 0:
			rect(cv, x, ground+6, 1, 1, withAlpha(th.Text, 0x70))
		case k%53 == 0:
			rect(cv, x, ground+4, 3, 1, withAlpha(th.Text, 0x60))
		}
	}
	// Lo que viene.
	for _, it := range g.things {
		x := ruHeroX + it.d - g.run
		if x < -20 || x > float64(cw)+20 {
			continue
		}
		switch it.kind {
		case ruTick: // una marca por golpe (más viva la del compás): los obstáculos caen en ellas
			c := withAlpha(th.Accent, 0x70)
			if it.big {
				c = th.Accent
			}
			rect(cv, int(math.Round(x)), ground+9, 1, 2, c)
		case ruSpeaker:
			name, iw, ih := "ru_speaker", 8, 12
			if it.big {
				name, iw, ih = "ru_amp", 10, 18
			}
			img := crSprite(name, th.Text, th.Accent2)
			for i := 0; i < it.n; i++ {
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Translate(math.Round(x-float64(it.n*iw)/2)+float64(i*iw), float64(ground-ih+1))
				cv.DrawImage(img, op)
			}
		case ruVinyl:
			y := float64(ground) - ruVinylY[it.alt]
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(math.Round(x-5), y-9)
			cv.DrawImage(crSprite("ru_vinyl", th.Accent2, th.Text), op)
			an := a.t * 10 // el brillo que gira
			rect(cv, int(math.Round(x+3*math.Cos(an))), int(math.Round(y-4+3*math.Sin(an))), 1, 1, th.Text)
		case ruNote:
			if it.done {
				continue
			}
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(math.Round(x-3), float64(ground)-it.h-4+math.Round(math.Sin(a.t*6+it.at)))
			op.ColorScale.ScaleWithColor(th.Accent)
			cv.DrawImage(icon("note"), op)
		}
	}
	// El héroe: corre a dos patas, una por corchea; agachado, largo y bajo;
	// en el aire, encogido; al chocar, con los ojos en cruz.
	step := int(math.Floor((now-anchor)/(per/2)))&1 == 1
	name := "ru_run1"
	switch {
	case g.over:
		name = "ru_dead"
	case g.y > 0:
		name = "ru_jump"
	case g.duck && step:
		name = "ru_duck2"
	case g.duck:
		name = "ru_duck1"
	case step:
		name = "ru_run2"
	}
	img := crSprite(name, th.Text, th.Shadow)
	hop := &ebiten.DrawImageOptions{}
	hop.GeoM.Translate(ruHeroX-6, math.Round(float64(ground)-float64(img.Bounds().Dy())+1-g.y))
	cv.DrawImage(img, hop)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(2, 2)
	op.GeoM.Translate(0, float64(top))
	dst.DrawImage(cv, op)
	// El marcador, como el del dinosaurio: el récord y los puntos en cinco
	// cifras, que parpadean al pasar cada centena.
	sc := fmt.Sprintf("%05d", g.score)
	if g.score >= 100 && g.score%100 < 12 && int(a.t*6)%2 == 0 && !g.over {
		sc = ""
	}
	hi := fmt.Sprintf("HI %05d", max(g.best, g.score))
	x := w - margin - textWidth("00000", SmallFace)
	drawTextShadow(dst, sc, SmallFace, x, top+6, th.Text, th.Shadow)
	drawTextShadow(dst, hi, SmallFace, x-10-textWidth(hi, SmallFace), top+6, th.Dim, th.Shadow)
	if g.mult > 1 {
		m := fmt.Sprintf("x%d", g.mult)
		drawText(dst, m, SmallFace, w-margin-textWidth(m, SmallFace), top+17, th.Accent2)
	}
	if p := a.t - g.perfectAt; g.perfectAt > 0 && p < 0.5 {
		msg := T("ru_perfect")
		y := top + 2*(ground-int(ruJumpH)-16) - int(p*16)
		drawTextShadow(dst, msg, SmallFace, 2*int(ruHeroX)-textWidth(msg, SmallFace)/2+8, y, th.Accent2, th.Shadow)
	}
	if g.frozen && !g.over {
		a.drawCenterNote(dst, T("cr_play_music"))
	}
	if g.over {
		a.drawGameOver(dst, T("ru_over"), fmt.Sprintf("%s %d", T("tet_score"), g.score), max(g.best, g.score), g.overAt)
	}
	a.drawHints(dst, []hint{{"A", T("ru_h_jump")}, {"v", T("ru_h_duck")}, {"START", T("h_pause")}, {"SEL", T("h_menu")}})
}
