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
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Ritmo: un juego de carriles con la canción que suena. Las notas caen por
// cuatro carriles y llegan a la línea justo en los golpes: salen de la rejilla
// de tempo (meter.go), así que van a tiempo aunque el medidor solo vea medio
// segundo por delante. Cada carril es un instrumento de la batería (bombo,
// caja, charles, platillos): las notas de cada corchea son lo que tocó la
// batería en esa corchea en los últimos compases (beat.drums), así siguen el
// patrón de la canción y cambian con él. Una nota a la vez. Se juega a dos
// manos y sin gatillos: ← ↓ con la cruceta y B A con los botones (o D F J K).

const (
	rhLanes   = 4
	rhTravel  = 1.3  // segundos que tarda una nota en bajar hasta la línea
	rhPerfect = 0.05 // ventanas de acierto, en segundos a cada lado
	rhGreat   = 0.10
	rhGood    = 0.15
	rhFever   = 50 // combo que enciende FEVER
)

// rhNote es una nota: cuándo hay que tocarla y en qué carril.
type rhNote struct {
	t    float64 // en beatClock
	lane int
}

// rhythmGame es una partida de Ritmo.
type rhythmGame struct {
	notes      []rhNote
	nextBeat   float64 // desde dónde se busca la próxima corchea sin nota
	lastLane   int
	sameLane   int
	score      int
	combo, max int
	judged     [4]int // perfect, great, good, miss
	best       int
	judge      string
	judgeAt    float64
	laneHit    [rhLanes]float64 // cuándo se pulsó cada carril (el destello)
	laneGood   [rhLanes]float64 // cuándo se acertó una nota en cada carril
	laneGrade  [rhLanes]int     // cómo fue ese acierto (0 perfect, 1 great, 2 good)
	laneMiss   [rhLanes]float64 // cuándo se escapó una nota de cada carril
	laneAvg    [rhLanes]float64 // la media reciente del espectro de cada carril
}

func (a *App) rhythmBestPath() string { return filepath.Join(a.paths.Data, "ritmo.json") }

func (a *App) newRhythm() *rhythmGame {
	g := &rhythmGame{lastLane: -1}
	if b, err := os.ReadFile(a.rhythmBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	return g
}

func (a *App) saveRhythmBest() {
	g := a.rhythm
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.rhythmBestPath(), b, 0o644)
}

// accuracy es la precisión (0..100).
func (g *rhythmGame) accuracy() float64 {
	n := g.judged[0] + g.judged[1] + g.judged[2] + g.judged[3]
	if n == 0 {
		return 100
	}
	return 100 * float64(300*g.judged[0]+200*g.judged[1]+100*g.judged[2]) / float64(300*n)
}

func (g *rhythmGame) fever() bool { return g.combo >= rhFever }

// laneEnergy es cuánto suena cada carril ahora: el espectro en cuatro grupos.
func laneEnergy(bands [engine.MeterBands]float64) [rhLanes]float64 {
	var e [rhLanes]float64
	for b, v := range bands {
		e[b*rhLanes/engine.MeterBands] += v
	}
	return e
}

// pickLane elige el carril de una nota: al azar, pero con más peso donde más
// suena (determinista por la hora del golpe, para que no tiemble), y sin
// repetir carril más de dos veces seguidas.
func (g *rhythmGame) pickLane(t float64, e [rhLanes]float64) int {
	r := rand.New(rand.NewPCG(uint64(t*1000), 7))
	var w [rhLanes]float64
	total := 0.0
	for i := range w {
		w[i] = 0.3 + e[i]
		if i == g.lastLane && g.sameLane >= 2 {
			w[i] = 0
		}
		total += w[i]
	}
	x := r.Float64() * total
	lane := 0
	for i := range w {
		if x < w[i] {
			lane = i
			break
		}
		x -= w[i]
	}
	if lane == g.lastLane {
		g.sameLane++
	} else {
		g.lastLane, g.sameLane = lane, 1
	}
	return lane
}

// rhLaneActs son los botones de cada carril, a dos manos y siempre vecinos:
// ← y ↓ con la izquierda, B y A con la derecha (con ↑ → Y X de alternativa).
// Con el rombo entero (Y B X A) había que saltar entre opuestos.
var rhLaneActs = [rhLanes][2]Action{{ActLeft, ActUp}, {ActDown, ActRight}, {ActBack, ActPause}, {ActConfirm, ActShuffle}}

// rhLaneKeys son las teclas de cada carril con teclado: D F J K, como en los
// juegos de ritmo de PC.
var rhLaneKeys = [rhLanes]ebiten.Key{ebiten.KeyD, ebiten.KeyF, ebiten.KeyJ, ebiten.KeyK}

// updateRhythm juega un fotograma.
func (a *App) updateRhythm() {
	g, in := a.rhythm, &a.input
	switch {
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	case in.Pressed(ActMenu):
		a.openRhythmPause()
		return
	}
	now := a.beatClock()
	per, anchor := a.steadyBeat()
	if per > 0 && a.isPlaying {
		// Notas para las corcheas que van a llegar en rhTravel: los
		// instrumentos que sonaron en esa corchea en los últimos compases,
		// cada uno en su carril (bombo, caja, charles, platillo). Hasta
		// tener el patrón, una nota por golpe.
		// Cada nota cae en la corchea de la rejilla suave que sigue a la
		// anterior: si la rejilla se corrige, las que vienen la siguen (antes
		// se sumaba media corchea tras otra y un desfase quedaba para siempre).
		half := per / 2
		if g.nextBeat < now {
			g.nextBeat = now
		}
		_, _, bands, _ := a.meterNow()
		e := laneEnergy(bands)
		for {
			t := anchor + math.Ceil((g.nextBeat-anchor)/half)*half
			if t > now+rhTravel {
				break
			}
			slot := a.beat.slot(t)
			if a.beat.drums.any {
				on := a.beat.drums.at(slot, t, per)
				n := 0
				// Una nota a la vez (dos juntas era demasiado): manda el
				// bombo, luego la caja, el platillo y el charles.
				for _, k := range []int{drumKick, drumSnare, drumCrash, drumHat} {
					if on[k] && n < 1 {
						g.notes = append(g.notes, rhNote{t, k})
						n++
					}
				}
			} else if slot%2 == 0 {
				g.notes = append(g.notes, rhNote{t, g.pickLane(t, e)})
			}
			g.nextBeat = t + half/2 // la siguiente busca después de esta
		}
	}
	// Lo tocado: la nota más cercana de ese carril, si cae en la ventana.
	for lane, acts := range rhLaneActs {
		if !in.Pressed(acts[0]) && !in.Pressed(acts[1]) && !inpututil.IsKeyJustPressed(rhLaneKeys[lane]) {
			continue
		}
		g.laneHit[lane] = a.t
		best, bestD := -1, rhGood
		for i, n := range g.notes {
			if d := math.Abs(n.t - now); n.lane == lane && d <= bestD {
				best, bestD = i, d
			}
		}
		if best < 0 {
			continue
		}
		g.notes = append(g.notes[:best], g.notes[best+1:]...)
		k, pts := 2, 100
		switch {
		case bestD <= rhPerfect:
			k, pts = 0, 300
		case bestD <= rhGreat:
			k, pts = 1, 200
		}
		g.judged[k]++
		g.combo++
		g.max = max(g.max, g.combo)
		mult := 1 + g.combo/10
		if g.fever() {
			mult *= 2
		}
		g.score += pts * mult
		g.judge, g.judgeAt = []string{"PERFECT", "GREAT", "GOOD"}[k], a.t
		g.laneGood[lane], g.laneGrade[lane] = a.t, k
	}
	// Las que pasaron de largo: fallo, y el combo se rompe.
	kept := g.notes[:0]
	for _, n := range g.notes {
		if now-n.t > rhGood {
			g.judged[3]++
			g.combo = 0
			g.judge, g.judgeAt = "MISS", a.t
			g.laneMiss[n.lane] = a.t
			continue
		}
		kept = append(kept, n)
	}
	g.notes = kept
}

// openRhythmPause es la pausa de Ritmo (Start).
func (a *App) openRhythmPause() {
	g := a.rhythm
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d · %.1f%%", T("tet_score"), g.score, g.accuracy()),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("rh_music"), icon: "pause", do: func() { a.send("toggle") }},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveRhythmBest(); a.rhythm = a.newRhythm() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveRhythmBest(); a.screen = Games }},
		},
	}
}

// rhLaneColor es el color de un carril: del primer acento al segundo, suave.
func (a *App) rhLaneColor(lane int) color.RGBA {
	return mix(a.theme.Accent, a.theme.Accent2, float64(lane)/(rhLanes-1))
}

// rhLaneLabels son el botón y la tecla de cada carril, para el receptor.
var rhLaneLabels = [rhLanes][2]string{{"<", "D"}, {"v", "F"}, {"B", "J"}, {"A", "K"}}

// rhArrowIcons son los iconos de las flechas de los receptores.
var rhArrowIcons = map[string]string{"<": "arrow_l", "v": "arrow_d"}

// drawRhythmGame pinta la partida, minimalista: líneas finas, notas planas y
// en cada receptor su botón.
func (a *App) drawRhythmGame(dst *ebiten.Image) {
	g := a.rhythm
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	now := a.beatClock()
	pulse := a.beatPulse()

	laneW := 22
	fw := rhLanes * laneW
	side := max(80, min(120, w-fw-3*margin))
	fx := (w - fw - side - margin) / 2
	top, bottom := topH+6, h-bottomH-4
	hitY := bottom - 26
	// Con el dedo: tocar un carril es pulsar su botón, en el momento de tocar.
	for lane := range rhLanes {
		a.pressZone(fx+lane*laneW, top, laneW, bottom-top, func(int, int) { a.input.inject(rhLaneActs[lane][0]) })
	}

	// Los carriles: solo sus bordes, tenues.
	for lane := 0; lane <= rhLanes; lane++ {
		rect(dst, fx+lane*laneW, top, 1, hitY-top+8, withAlpha(th.Dim, 0x30))
	}
	// La línea de toque, que late con los golpes.
	rect(dst, fx, hitY, fw+1, 1, mix(withAlpha(th.Dim, 0xff), th.Text, pulse*0.7))

	// El acierto sube por el carril como un haz que se apaga por escalones
	// (más fuerte si fue PERFECT).
	for lane := 0; lane < rhLanes; lane++ {
		p := (a.t - g.laneGood[lane]) / 0.3
		if g.laneGood[lane] == 0 || p >= 1 {
			continue
		}
		c := a.rhLaneColor(lane)
		strength := []float64{1, 0.75, 0.5}[g.laneGrade[lane]] * float64(steps(1-p))
		x := fx + lane*laneW + 1
		const bands = 6
		span := float64(hitY-top) * (0.5 + 0.5*strength)
		for b := 0; b < bands; b++ {
			y0 := hitY - int(span*float64(b+1)/bands)
			y1 := hitY - int(span*float64(b)/bands)
			al := uint8(150 * strength * float64(bands-b) / bands)
			rect(dst, x, y0, laneW-1, y1-y0, withAlpha(c, al))
		}
	}
	// La línea se enciende un momento con cada acierto.
	if p := (a.t - maxOf(g.laneGood[:])) / 0.15; p < 1 && maxOf(g.laneGood[:]) > 0 {
		rect(dst, fx, hitY-1, fw+1, 3, withAlpha(th.Text, uint8(200*steps(1-p))))
	}

	// Las notas: rayas planas del color de su carril.
	for _, n := range g.notes {
		dt := n.t - now
		y := hitY - int(dt/rhTravel*float64(hitY-top))
		if y < top || y > hitY+6 {
			continue
		}
		x := fx + n.lane*laneW
		c := a.rhLaneColor(n.lane)
		if g.fever() && a.beat.count%2 == 0 {
			c = mix(c, th.Text, 0.35)
		}
		panel(dst, x+3, y-2, laneW-5, 4, c)
	}

	// Los receptores: el botón de cada carril; se encienden al pulsar.
	usePad := a.input.usingPad || a.offline
	for lane := 0; lane < rhLanes; lane++ {
		x := fx + lane*laneW
		cx := x + laneW/2
		c := a.rhLaneColor(lane)
		const bs = 13
		bx, by := cx-bs/2, hitY+4
		good := g.laneGood[lane] > 0 && a.t-g.laneGood[lane] < 0.18
		pressed := a.t-g.laneHit[lane] < 0.1
		missed := g.laneMiss[lane] > 0 && a.t-g.laneMiss[lane] < 0.25
		bg, fg := th.Bg2, c
		switch {
		case good:
			// Acierto: blanco un instante y luego lleno del color del carril,
			// un pelín más grande.
			bg, fg = c, th.Bg
			if a.t-g.laneGood[lane] < 0.06 {
				bg = th.Text
			}
			bx, by = bx-1, by-1
		case missed:
			// Fallo: se apaga y tiembla.
			bg, fg = withAlpha(th.Bg2, 0x80), withAlpha(th.Dim, 0x90)
			bx += []int{-1, 1, -1, 1, 0}[min(4, int((a.t-g.laneMiss[lane])*20))]
		}
		if good {
			panel(dst, bx, by, bs+2, bs+2, bg)
		} else {
			panel(dst, bx, by, bs, bs, bg)
		}
		if pressed && !good {
			// Pulsar sin nota: solo el borde, para no confundirlo con acertar.
			frame(dst, bx, by, bs, bs, c)
		}
		if good {
			bx, by = bx+1, by+1
		}
		a.drawHitBurst(dst, g, lane, cx, by+bs/2, c)
		label := rhLaneLabels[lane][0]
		if !usePad {
			label = rhLaneLabels[lane][1]
		}
		// Las flechas, dibujadas (en la fuente, "<" y "v" no se leían como
		// flechas: la "v" parecía una Y).
		if ic, ok := rhArrowIcons[label]; ok {
			b := icon(ic).Bounds()
			drawIcon(dst, ic, bx+bs/2-b.Dx()/2, by+(bs-b.Dy())/2, fg)
		} else {
			drawText(dst, label, SmallFace, bx+bs/2-textWidth(label, SmallFace)/2, by+(bs-smallCap)/2, fg)
		}
	}

	// El juicio, grande y con rebote sobre la línea; debajo, el combo.
	if p := (a.t - g.judgeAt) / 0.55; g.judgeAt > 0 && p < 1 {
		k := uint8(255 * steps(1-p))
		face, c := BigFace, th.Text
		switch g.judge {
		case "PERFECT":
			c = th.Accent2
			if int(a.t*20)%2 == 0 {
				c = th.Text
			}
		case "GREAT":
			c = th.Accent
		case "MISS":
			face, c = SmallFace, th.Dim
		}
		jy := hitY - 44 - int(6*easeOutBack(math.Min(1, p*4)))
		jx := fx + (fw-textWidth(g.judge, face))/2
		if face == BigFace {
			drawText(dst, g.judge, face, jx+1, jy+1, withAlpha(th.Shadow, k))
		}
		drawText(dst, g.judge, face, jx, jy, withAlpha(c, k))
		if g.judge != "MISS" && g.combo >= 2 {
			cs := fmt.Sprintf("%d", g.combo)
			drawText(dst, cs, SmallFace, fx+(fw-textWidth(cs, SmallFace))/2, jy+18, withAlpha(th.Dim, k))
		}
	}
	if a.beat.period() <= 0 {
		msg := T("rh_listening")
		if !a.isPlaying {
			msg = T("rh_play")
		}
		drawTextW(dst, msg, SmallFace, fx+max(0, (fw-textWidth(msg, SmallFace))/2), top+(hitY-top)/2, fw, th.Dim)
	}

	// A la derecha, lo justo: puntos, combo, precisión y lo que suena.
	sx := fx + fw + margin + 4
	y := top
	drawText(dst, T("tet_score"), SmallFace, sx, y, th.Dim)
	drawText(dst, fmt.Sprint(g.score), BigFace, sx, y+10, th.Text)
	y += 30
	combo := fmt.Sprintf("%s %d", "COMBO", g.combo)
	cc := th.Accent
	if g.fever() {
		combo = fmt.Sprintf("FEVER %d", g.combo)
		if int(a.t*8)%2 == 0 {
			cc = th.Accent2
		}
	}
	drawText(dst, combo, SmallFace, sx, y, cc)
	drawText(dst, fmt.Sprintf("%.1f%%", g.accuracy()), SmallFace, sx, y+11, th.Dim)
	drawText(dst, fmt.Sprintf("%s %d", T("tet_best"), max(g.best, g.score)), SmallFace, sx, y+22, th.Dim)
	if a.playing != nil {
		a.drawCover(dst, a.playing, sx, bottom-26, 26, 1)
		drawTextW(dst, a.playingSong.Title, SmallFace, sx+30, bottom-24, w-sx-30-4, th.Text)
		drawTextW(dst, a.playingSong.Artist, SmallFace, sx+30, bottom-14, w-sx-30-4, th.Accent)
	}
	a.drawHints(dst, []hint{{"START", T("h_pause")}, {"SEL", T("h_menu")}, {"FN", T("h_guide")}})
}

// updateRhythmFrame avanza Ritmo sin botones (para la maqueta).
func (a *App) updateRhythmFrame() {
	a.input = Input{}
	a.beatHit = a.updateBeat()
	a.updateRhythm()
}

// autoPlay toca las notas que llegan a la línea (para la maqueta).
func (a *App) autoPlay() {
	g := a.rhythm
	now := a.beatClock()
	for i := 0; i < len(g.notes); i++ {
		if n := g.notes[i]; math.Abs(n.t-now) <= rhPerfect {
			g.notes = append(g.notes[:i], g.notes[i+1:]...)
			g.judged[0]++
			g.combo++
			g.max = max(g.max, g.combo)
			g.score += 300 * (1 + g.combo/10)
			g.judge, g.judgeAt = "PERFECT", a.t
			g.laneHit[n.lane] = a.t
			g.laneGood[n.lane], g.laneGrade[n.lane] = a.t, 0
			i--
		}
	}
}

// drawHitBurst pinta, al acertar en un carril, un anillo cuadrado que crece
// desde el receptor (centrado en cx, cy) y, si fue PERFECT, chispas que
// saltan hacia arriba. Todo por escalones, como el resto.
func (a *App) drawHitBurst(dst *ebiten.Image, g *rhythmGame, lane, cx, cy int, c color.RGBA) {
	at := g.laneGood[lane]
	p := (a.t - at) / 0.3
	if at == 0 || p >= 1 {
		return
	}
	k := uint8(255 * steps(1-p))
	r := 8 + int(14*easeOutBack(p))
	col := withAlpha(mix(c, a.theme.Text, 0.4), k)
	frame(dst, cx-r, cy-r, 2*r+1, 2*r+1, col)
	if g.laneGrade[lane] == 0 {
		frame(dst, cx-r+1, cy-r+1, 2*r-1, 2*r-1, col)
		// Chispas: seis, en abanico hacia arriba, que caen un poco.
		for i := 0; i < 6; i++ {
			an := math.Pi * (1.1 + 0.8*float64(i)/5) // de izquierda a derecha, por arriba
			d := 26 * p
			x := cx + int(math.Cos(an)*d)
			y := cy + int(math.Sin(an)*d+40*p*p)
			rect(dst, x, y, 2, 2, withAlpha(a.theme.Text, k))
		}
	}
}

// maxOf es el mayor de vs (0 si está vacío).
func maxOf(vs []float64) float64 {
	m := 0.0
	for _, v := range vs {
		m = max(m, v)
	}
	return m
}
