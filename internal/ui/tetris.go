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

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Bloques: un juego de piezas que caen, una pantalla más de las que se
// recorren con Select. Al cambiar de pantalla la partida se queda y se sigue
// al volver. Reacciona a la música que suena:
//
//   - en cada golpe de bajo la pieza baja una fila (las partes fuertes
//     aprietan), y el tablero da un saltito;
//   - el borde del pozo late con los graves;
//   - las piezas ya apiladas se iluminan por columnas con el espectro, como un
//     ecualizador hecho de bloques, y detrás de ellas suben columnas tenues
//     con el espectro;
//   - en cada golpe el borde destella y la pieza da un rebote; se calcula el
//     tempo (BPM) y soltar una pieza a tiempo (±0,1 s del golpe) vale el doble;
//   - completar líneas hace bombear el fondo.
//
// Controles: flechas para mover, ↓ para bajar rápido, ↑ para soltar, A gira a
// la derecha y B (o X) a la izquierda, Start abre la pausa (seguir, nueva
// partida o salir), Select vuelve al reproductor. La música sigue a mano: Y
// pausa o reanuda, L1/R1 cambian de canción, L2/R2 retroceden o adelantan.
//
// Se llama "Bloques" y no por el nombre del juego original, que es una marca.

const (
	tetW, tetH   = 10, 20
	tetHidden    = 2 // filas ocultas arriba, donde aparecen las piezas
	tetRows      = tetH + tetHidden
	tetLockDelay = 0.5 // segundos que una pieza apoyada tarda en fijarse
	tetLockMoves = 15  // movimientos o giros que reinician esa espera
	tetDAS       = 10  // fotogramas antes de que una flecha mantenida repita
	tetARR       = 2   // fotogramas entre repeticiones
	tetSoftDrop  = 2   // fotogramas por fila con ↓
	tetPreview   = 3   // piezas siguientes a la vista
)

// tetShapes son las siete piezas en los cuatro estados de SRS (0, R, 2, L),
// como celdas x, y en su cuadro de 3×3 (4×4 la I y la O). Los giros salen de
// girar el cuadro, como en SRS.
var tetShapes = func() [7][4][4][2]int {
	base := [7][4][2]int{
		{{0, 1}, {1, 1}, {2, 1}, {3, 1}}, // I
		{{0, 0}, {0, 1}, {1, 1}, {2, 1}}, // J
		{{2, 0}, {0, 1}, {1, 1}, {2, 1}}, // L
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}}, // O
		{{1, 0}, {2, 0}, {0, 1}, {1, 1}}, // S
		{{1, 0}, {0, 1}, {1, 1}, {2, 1}}, // T
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}}, // Z
	}
	var out [7][4][4][2]int
	for p, cells := range base {
		size := 3
		if p == 0 {
			size = 4
		}
		cur := cells
		for r := 0; r < 4; r++ {
			out[p][r] = cur
			if p == 3 {
				continue // la O no gira
			}
			var next [4][2]int
			for i, c := range cur {
				next[i] = [2]int{size - 1 - c[1], c[0]} // 90° a la derecha
			}
			cur = next
		}
	}
	return out
}()

const pieceI, pieceO, pieceT = 0, 3, 5

// Las tablas de wall kicks de SRS: para cada giro (de, a), los
// desplazamientos que se prueban en orden. En SRS la y va hacia arriba; aquí,
// hacia abajo, así que ya están con la y cambiada de signo.
var (
	kicksJLSTZ = map[[2]int][5][2]int{
		{0, 1}: {{0, 0}, {-1, 0}, {-1, -1}, {0, 2}, {-1, 2}},
		{1, 0}: {{0, 0}, {1, 0}, {1, 1}, {0, -2}, {1, -2}},
		{1, 2}: {{0, 0}, {1, 0}, {1, 1}, {0, -2}, {1, -2}},
		{2, 1}: {{0, 0}, {-1, 0}, {-1, -1}, {0, 2}, {-1, 2}},
		{2, 3}: {{0, 0}, {1, 0}, {1, -1}, {0, 2}, {1, 2}},
		{3, 2}: {{0, 0}, {-1, 0}, {-1, 1}, {0, -2}, {-1, -2}},
		{3, 0}: {{0, 0}, {-1, 0}, {-1, 1}, {0, -2}, {-1, -2}},
		{0, 3}: {{0, 0}, {1, 0}, {1, -1}, {0, 2}, {1, 2}},
	}
	kicksI = map[[2]int][5][2]int{
		{0, 1}: {{0, 0}, {-2, 0}, {1, 0}, {-2, 1}, {1, -2}},
		{1, 0}: {{0, 0}, {2, 0}, {-1, 0}, {2, -1}, {-1, 2}},
		{1, 2}: {{0, 0}, {-1, 0}, {2, 0}, {-1, -2}, {2, 1}},
		{2, 1}: {{0, 0}, {1, 0}, {-2, 0}, {1, 2}, {-2, -1}},
		{2, 3}: {{0, 0}, {2, 0}, {-1, 0}, {2, -1}, {-1, 2}},
		{3, 2}: {{0, 0}, {-2, 0}, {1, 0}, {-2, 1}, {1, -2}},
		{3, 0}: {{0, 0}, {1, 0}, {-2, 0}, {1, 2}, {-2, -1}},
		{0, 3}: {{0, 0}, {-1, 0}, {2, 0}, {-1, -2}, {2, 1}},
	}
)

// tetris es una partida, con las mecánicas de los juegos modernos (como
// Apotris): SRS, DAS/ARR, lock delay con move reset, hold, varias siguientes,
// T-spins y back-to-back.
type tetris struct {
	board        [tetRows][tetW]int8 // 0 vacío; 1..7 la pieza que lo ocupa
	piece, rot   int
	px, py       int
	queue        []int // las siguientes (siempre al menos tetPreview)
	bag          []int
	hold         int  // -1 si no hay
	held         bool // ya se usó el hold con esta pieza
	fall         float64
	lockT        float64 // tiempo apoyada
	lockMoves    int     // movimientos que aún pueden reiniciar el lock delay
	lowest       int     // la fila más baja a la que llegó la pieza
	rotated      bool    // lo último fue un giro (para los T-spins)
	b2b          bool    // el último borrado fue difícil (tetris o T-spin)
	soft         int     // fotogramas de ↓ mantenida
	score, lines int
	best         int
	over         bool
	clearing     []int   // filas completas que parpadean antes de quitarse
	clearAt      float64 // cuándo empezaron
	clearWait    float64 // cuánto esperan (hasta el golpe, si está cerca)
	shakeAt      float64
	bounceAt     float64 // el rebote de la pieza en un golpe
	onBeatAt     float64 // cuándo se soltó una pieza a tiempo (el aviso)
	onBeatBonus  int
	combo        int     // piezas seguidas soltadas a tiempo
	comboAt      float64 // cuándo cambió el combo
	laserAt      float64 // el destello al soltar una pieza
	laserY       int     // fila donde aterrizó
	clearY       int     // fila del medio de las líneas quitadas (para la onda)
	clearDoneAt  float64 // cuándo se quitaron
	callAt       float64 // el aviso de jugada (T-SPIN, TETRIS, B2B)
	call         string
	colAvg       [tetW]float64 // la media reciente de cada columna del ecualizador
}

// tetBest es el récord, en data/bloques.json.
func (a *App) tetBestPath() string { return filepath.Join(a.paths.Data, "bloques.json") }

func (a *App) newTetris() *tetris {
	g := &tetris{hold: -1}
	if b, err := os.ReadFile(a.tetBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	g.spawn(g.take())
	return g
}

// take saca la siguiente pieza de la cola, rellenándola con la bolsa de siete
// (todas salen antes de repetir).
func (g *tetris) take() int {
	for len(g.queue) <= tetPreview {
		if len(g.bag) == 0 {
			g.bag = rand.Perm(7)
		}
		g.queue = append(g.queue, g.bag[0])
		g.bag = g.bag[1:]
	}
	p := g.queue[0]
	g.queue = g.queue[1:]
	return p
}

// spawn pone la pieza p arriba, en las filas ocultas.
func (g *tetris) spawn(p int) {
	g.piece, g.rot, g.px, g.py = p, 0, 3, 0
	g.fall, g.lockT, g.lockMoves, g.lowest, g.rotated = 0, 0, tetLockMoves, 0, false
	if !g.fits(g.piece, g.rot, g.px, g.py) {
		g.over = true
		return
	}
	if g.fits(g.piece, g.rot, g.px, g.py+1) { // como el estándar: aparece y baja una
		g.py++
	}
}

func (g *tetris) level() int { return g.lines / 10 }

// gravity son los segundos por fila del nivel (la curva del estándar).
func (g *tetris) gravity() float64 {
	l := float64(g.level())
	return math.Max(1.0/60, math.Pow(0.8-l*0.007, l))
}

// fits dice si la pieza cabe ahí.
func (g *tetris) fits(p, r, x, y int) bool {
	for _, c := range tetShapes[p][r] {
		cx, cy := x+c[0], y+c[1]
		if cx < 0 || cx >= tetW || cy < 0 || cy >= tetRows || g.board[cy][cx] != 0 {
			return false
		}
	}
	return true
}

// grounded dice si la pieza está apoyada.
func (g *tetris) grounded() bool { return !g.fits(g.piece, g.rot, g.px, g.py+1) }

// moved reinicia el lock delay tras un movimiento o giro con la pieza apoyada,
// las veces que quedan (move reset).
func (g *tetris) moved() {
	if g.grounded() && g.lockMoves > 0 {
		g.lockMoves--
		g.lockT = 0
	}
}

func (g *tetris) move(dx int) bool {
	if g.fits(g.piece, g.rot, g.px+dx, g.py) {
		g.px += dx
		g.rotated = false
		g.moved()
		return true
	}
	return false
}

// rotate gira (dir 1 derecha, -1 izquierda) con los wall kicks de SRS.
func (g *tetris) rotate(dir int) {
	if g.piece == pieceO {
		return
	}
	r := (g.rot + dir + 4) % 4
	table := kicksJLSTZ
	if g.piece == pieceI {
		table = kicksI
	}
	for _, k := range table[[2]int{g.rot, r}] {
		if g.fits(g.piece, r, g.px+k[0], g.py+k[1]) {
			g.rot, g.px, g.py = r, g.px+k[0], g.py+k[1]
			g.rotated = true
			g.moved()
			return
		}
	}
}

// down baja una fila; false si no puede.
func (g *tetris) down() bool {
	if !g.fits(g.piece, g.rot, g.px, g.py+1) {
		return false
	}
	g.py++
	g.rotated = false
	if g.py > g.lowest { // llegar más abajo devuelve los move resets
		g.lowest, g.lockMoves, g.lockT = g.py, tetLockMoves, 0
	}
	return true
}

// swapHold guarda la pieza (una vez por pieza) y saca la guardada o la
// siguiente.
func (g *tetris) swapHold() {
	if g.held {
		return
	}
	cur := g.piece
	if g.hold < 0 {
		g.spawn(g.take())
	} else {
		g.spawn(g.hold)
	}
	g.hold, g.held = cur, true
}

// ghostY es dónde caería la pieza.
func (g *tetris) ghostY() int {
	y := g.py
	for g.fits(g.piece, g.rot, g.px, y+1) {
		y++
	}
	return y
}

// isTSpin aplica la regla de las tres esquinas: una T que se fija justo
// después de girar, con tres de las cuatro esquinas de su cuadro ocupadas.
func (g *tetris) isTSpin() bool {
	if g.piece != pieceT || !g.rotated {
		return false
	}
	n := 0
	for _, c := range [4][2]int{{0, 0}, {2, 0}, {0, 2}, {2, 2}} {
		x, y := g.px+c[0], g.py+c[1]
		if x < 0 || x >= tetW || y >= tetRows || (y >= 0 && g.board[y][x] != 0) {
			n++
		}
	}
	return n >= 3
}

// lock fija la pieza, puntúa y busca filas completas. beatSoon es cuánto
// falta para el próximo golpe (o un número grande si no se sabe).
func (g *tetris) lock(t, beatSoon float64) {
	tspin := g.isTSpin()
	above := true
	for _, c := range tetShapes[g.piece][g.rot] {
		g.board[g.py+c[1]][g.px+c[0]] = int8(g.piece + 1)
		above = above && g.py+c[1] < tetHidden
	}
	g.held = false
	g.clearing = g.clearing[:0]
	for y := 0; y < tetRows; y++ {
		full := true
		for x := 0; x < tetW; x++ {
			full = full && g.board[y][x] != 0
		}
		if full {
			g.clearing = append(g.clearing, y)
		}
	}
	n := len(g.clearing)
	// Puntos del estándar (por nivel); los difíciles seguidos, ×1,5.
	pts := []int{0, 100, 300, 500, 800}[n]
	g.call = ""
	switch {
	case tspin:
		pts = []int{400, 800, 1200, 1600, 1600}[n]
		g.call = "T-SPIN"
	case n == 4:
		g.call = "TETRA"
	}
	hard := tspin && n > 0 || n == 4
	if hard && g.b2b {
		pts = pts * 3 / 2
		g.call = "B2B " + g.call
	}
	if n > 0 {
		g.b2b = hard
	}
	if g.fever() {
		pts *= 2
	}
	g.score += pts * (g.level() + 1)
	if g.call != "" {
		g.callAt = t
	}
	if n > 0 {
		// Las líneas explotan en el golpe si viene enseguida; si no, ya.
		g.clearAt, g.clearWait = t, 0.12
		if beatSoon < 0.25 {
			g.clearWait = math.Max(0.12, beatSoon)
		}
		return
	}
	if above {
		g.over = true // se fijó entera en las filas ocultas
		return
	}
	g.spawn(g.take())
}

// finishClear quita las filas completas cuando acaba su parpadeo.
func (g *tetris) finishClear() {
	for _, row := range g.clearing {
		for y := row; y > 0; y-- {
			g.board[y] = g.board[y-1]
		}
		g.board[0] = [tetW]int8{}
	}
	g.lines += len(g.clearing)
	g.clearing = g.clearing[:0]
	g.spawn(g.take())
}

// shift atiende las flechas con DAS/ARR: un toque mueve una celda; mantenida,
// tras tetDAS fotogramas, repite cada tetARR. Si están las dos, manda la
// última que se pulsó.
func (g *tetris) shift(left, right int) {
	d, dir := left, -1
	if right > 0 && (left == 0 || right < left) {
		d, dir = right, 1
	}
	if d == 1 || (d > tetDAS && (d-tetDAS)%tetARR == 0) {
		g.move(dir)
	}
}

// updateTetris juega un fotograma.
func (a *App) updateTetris() {
	g, in := a.tetris, &a.input
	beat := a.beatHit // el ritmo se sigue una vez por fotograma (Update)
	if beat {
		g.bounceAt = a.t
	}
	// La música se controla sin salir: Y pausa o reanuda, L1/R1 cambian de
	// canción y L2/R2 retroceden o adelantan (los gatillos no los usa el juego).
	switch {
	case in.Pressed(ActPause):
		a.send("toggle")
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	case in.Repeat(ActSeekBack):
		a.seekBy(-10)
	case in.Repeat(ActSeekFwd):
		a.seekBy(+10)
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	}
	switch {
	case in.Pressed(ActMenu):
		a.openTetrisPause()
		return
	case g.over:
		if in.Pressed(ActConfirm) {
			a.saveTetBest()
			a.tetris = a.newTetris()
		}
		return
	}
	if len(g.clearing) > 0 {
		if a.t-g.clearAt >= g.clearWait {
			g.clearY = g.clearing[len(g.clearing)/2]
			g.clearDoneAt = a.t
			g.finishClear()
			bgPump = 1
			g.shakeAt = a.t
		}
		return
	}
	dt := 1.0 / 60
	// Giros y hold.
	switch {
	case in.Pressed(ActConfirm):
		g.rotate(1)
	case in.Pressed(ActBack):
		g.rotate(-1)
	case in.Pressed(ActShuffle):
		g.swapHold()
	}
	g.shift(in.duration(ActLeft), in.duration(ActRight))
	// Soltar: ↑.
	if in.Pressed(ActUp) {
		a.hardDrop()
		return
	}
	// Caída: la gravedad del nivel o, con ↓, una fila cada tetSoftDrop.
	if in.duration(ActDown) > 0 {
		g.soft++
		if g.soft%tetSoftDrop == 1 && g.down() {
			g.score++
		}
	} else {
		g.soft = 0
		g.fall += dt
		for g.fall >= g.gravity() {
			g.fall -= g.gravity()
			if !g.down() {
				g.fall = 0
				break
			}
		}
	}
	if g.grounded() {
		g.lockT += dt
		if g.lockT >= tetLockDelay {
			g.lock(a.t, a.beatSoon())
			if g.over {
				a.saveTetBest()
			}
		}
	} else {
		g.lockT = 0
	}
}

// hardDrop suelta la pieza de golpe y la fija: a tiempo con la música sube el
// combo, que multiplica (y en FEVER, el doble); a destiempo se rompe.
func (a *App) hardDrop() {
	g := a.tetris
	n := 0
	for g.down() {
		n++
	}
	pts := 2 * n
	if a.beat.onBeat(a.beatClock(), 0.1) {
		g.combo++
		pts = (2*pts + 50*(g.level()+1)) * g.combo
		if g.fever() {
			pts *= 2
		}
		g.onBeatAt, g.onBeatBonus = a.t, pts
	} else {
		g.combo = 0
	}
	g.comboAt = a.t
	g.score += pts
	g.laserAt, g.laserY = a.t, g.py+tetBottom(g.piece, g.rot)
	g.shakeAt = a.t
	g.lock(a.t, a.beatSoon())
	if g.over {
		a.saveTetBest()
	}
}

// beatSoon es cuánto falta para el próximo golpe previsto (o 9 si no se sabe).
func (a *App) beatSoon() float64 {
	p := a.beat.period()
	if p <= 0 || a.beat.at == 0 {
		return 9
	}
	now := a.beatClock()
	next := a.beat.at + math.Ceil((now-a.beat.at)/p)*p
	return next - now
}

// saveTetBest guarda el récord si se superó.
func (a *App) saveTetBest() {
	g := a.tetris
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.tetBestPath(), b, 0o644)
}

// tetColors son los colores de las piezas, sacados del tema (de la portada).
// Pocos tonos, como el resto: el acento, el segundo acento y el texto, en
// mezclas suaves.
func (a *App) tetColors() [7]color.RGBA {
	th := a.theme
	return [7]color.RGBA{
		th.Accent, mix(th.Accent, th.Accent2, 0.5), th.Accent2,
		mix(th.Accent, th.Text, 0.4), mix(th.Accent2, th.Text, 0.4),
		mix(th.Accent, th.Bg2, 0.3), mix(th.Accent2, th.Bg2, 0.3),
	}
}

// drawCell pinta una celda plana, con las esquinas recortadas y un píxel de
// hueco: como los paneles de la interfaz, sin relieve.
func drawCell(dst *ebiten.Image, x, y, s int, c color.RGBA) {
	panel(dst, x, y, s-1, s-1, c)
}

// drawTetris pinta la partida.
func (a *App) drawTetris(dst *ebiten.Image) {
	g := a.tetris
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	cell := (h - topH - bottomH - 10) / tetH
	bw, bh := cell*tetW, cell*tetH
	side := max(90, min(130, w-bw-3*margin))
	bx := (w - bw - side - margin) / 2
	by := topH + 5
	// El saltito en los golpes y al soltar una pieza.
	if p := (a.t - g.shakeAt) / 0.12; g.shakeAt > 0 && p < 1 {
		by += int(math.Round(2 * math.Sin(p*math.Pi)))
	}
	level, _, bands, _ := a.meterNow()
	pulse := a.beatPulse()
	// sy es la y en pantalla de una fila del tablero (las ocultas no se ven).
	sy := func(row int) int { return by + (row-tetHidden)*cell }

	// El fondo de ritmo, centrado en el pozo.
	since, clearSince, fever := 10.0, 10.0, 0.0
	if a.beat.at > 0 {
		since = math.Max(0, a.beatClock()-a.beat.at)
	}
	if g.clearDoneAt > 0 {
		clearSince = a.t - g.clearDoneAt
	}
	if g.fever() {
		fever = 1
	}
	drawRhythm(dst, th, bx+bw/2, by+bh/2, a.t, since, a.beat.period(), level, fever, clearSince, sy(g.clearY))

	// El pozo, un panel como los de la interfaz; su filo de abajo late con
	// los graves.
	panel(dst, bx-3, by-3, bw+5, bh+5, withAlpha(th.Bg2, 0xd0))
	// En cada golpe todo el borde destella (por escalones); el filo de
	// abajo late con los graves.
	if f := steps(pulse); f > 0 {
		c := th.Accent
		if g.fever() && a.beat.count%2 == 0 { // en FEVER alterna los acentos
			c = th.Accent2
		}
		frame(dst, bx-3, by-3, bw+5, bh+5, mix(th.Bg2, c, float64(f)))
	}
	edge := mix(withAlpha(th.Accent, 0xff), th.Text, pulse*0.6)
	rect(dst, bx-2, by+bh+1, bw+3, 1, edge)
	// El ecualizador del pozo: columnas tenues con el espectro, detrás de
	// las piezas (lo que cambia respecto al último segundo, como el
	// visualizador).
	for x := 0; x < tetW; x++ {
		v := bands[x*engine.MeterBands/tetW]
		avg := &g.colAvg[x]
		*avg += (v - *avg) * 0.02
		v = math.Max(0, math.Min(1, 0.4*v+2*(v-*avg)))
		rows := int(v * tetH * 0.5)
		if rows > 0 {
			rect(dst, bx+x*cell, by+bh-rows*cell, cell-1, rows*cell, mix(th.Bg2, th.Accent, 0.08+0.08*pulse))
		}
	}
	for y := 0; y < tetH; y++ { // puntos tenues donde caben las celdas
		for x := 0; x < tetW; x++ {
			a.batch.add(bx+x*cell+cell/2-1, by+y*cell+cell/2-1, 1, 1, withAlpha(th.Dim, 0x40))
		}
	}
	a.batch.draw(dst)

	cols := a.tetColors()
	flash := len(g.clearing) > 0 && int((a.t-g.clearAt)*20)%2 == 0
	for y := tetHidden; y < tetRows; y++ {
		clearing := false
		for _, r := range g.clearing {
			clearing = clearing || r == y
		}
		for x := 0; x < tetW; x++ {
			v := g.board[y][x]
			if v == 0 {
				continue
			}
			c := cols[v-1]
			// El ecualizador: cada columna se ilumina con su banda.
			band := bands[x*engine.MeterBands/tetW]
			c = mix(c, th.Text, math.Max(0, band-0.35)*0.7)
			if clearing && flash {
				c = th.Text
			}
			a.batch.panel(bx+x*cell, sy(y), cell-1, cell-1, c)
		}
	}
	a.batch.draw(dst)
	if !g.over && len(g.clearing) == 0 {
		// La sombra de dónde caerá y la pieza.
		gy := g.ghostY()
		for _, c := range tetShapes[g.piece][g.rot] {
			if y := gy + c[1]; y >= tetHidden {
				// Opaca, apagada hacia el fondo del pozo: con transparencia se
				// mezclaba con los puntos de debajo y se veía borrosa.
				panel(dst, bx+(g.px+c[0])*cell, sy(y), cell-1, cell-1, mix(th.Bg2, cols[g.piece], 0.3))
			}
		}
		// La pieza da un rebote de un píxel en cada golpe.
		bounce := 0
		if g.bounceAt > 0 && a.t-g.bounceAt < 0.1 {
			bounce = 1
		}
		for _, c := range tetShapes[g.piece][g.rot] {
			if y := g.py + c[1]; y >= tetHidden {
				drawCell(dst, bx+(g.px+c[0])*cell, sy(y)+bounce, cell, mix(cols[g.piece], th.Text, pulse*0.25))
			}
		}
	}

	// A la derecha: las siguientes y la guardada, los puntos y lo que suena.
	sx := bx + bw + margin
	ns := max(5, cell*3/5)
	box := ns*4 + 4
	drawText(dst, T("tet_next"), SmallFace, sx, by, th.Dim)
	drawText(dst, T("tet_hold"), SmallFace, sx+box+6, by, th.Dim)
	for i, p := range g.queue[:min(tetPreview, len(g.queue))] {
		a.drawMini(dst, p, sx, by+9+i*(ns*2+5), ns, cols[p])
	}
	if g.hold >= 0 {
		c := cols[g.hold]
		if g.held {
			c = mix(th.Bg2, c, 0.4) // ya se usó con esta pieza
		}
		a.drawMini(dst, g.hold, sx+box+6, by+9, ns, c)
	} else {
		panel(dst, sx+box+6, by+9, box, ns*2+4, th.Bg2)
	}
	y := by + 9 + tetPreview*(ns*2+5) + 4
	// Los números, de dos en dos.
	colW := (min(side, w-sx-margin) - 4) / 2
	for _, row := range [][4]string{
		{T("tet_score"), fmt.Sprint(g.score), T("tet_best"), fmt.Sprint(max(g.best, g.score))},
		{T("tet_lines"), fmt.Sprint(g.lines), T("tet_level"), fmt.Sprint(g.level() + 1)},
	} {
		for k := 0; k < 2; k++ {
			drawText(dst, row[2*k], SmallFace, sx+k*colW, y, th.Dim)
			drawText(dst, row[2*k+1], SmallFace, sx+k*colW, y+9, th.Text)
		}
		y += 21
	}
	// El ritmo: tempo, carril y combo.
	drawText(dst, "BPM "+bpmLabel(a.beat.BPM()), SmallFace, sx, y, th.Dim)
	a.drawLane(dst, sx, y+8, min(side-6, w-sx-margin))
	y += 21
	switch {
	case g.fever():
		c := th.Accent
		if int(a.t*8)%2 == 0 {
			c = th.Accent2
		}
		drawText(dst, fmt.Sprintf("FEVER x%d", g.combo), SmallFace, sx, y, c)
	case g.combo > 1:
		drawText(dst, fmt.Sprintf("%s x%d", T("tet_combo"), g.combo), SmallFace, sx, y, th.Accent2)
	}
	if a.playing != nil && y+30 < by+bh {
		a.drawCover(dst, a.playing, sx, by+bh-26, 26, 1)
		drawTextW(dst, a.playingSong.Title, SmallFace, sx+30, by+bh-24, w-sx-30-4, th.Text)
		drawTextW(dst, a.playingSong.Artist, SmallFace, sx+30, by+bh-14, w-sx-30-4, th.Accent)
	}

	// El láser al soltar una pieza: una raya que cruza el pozo (y se sale un
	// poco) en la fila donde aterrizó, y se apaga por escalones.
	if p := (a.t - g.laserAt) / 0.2; g.laserAt > 0 && p < 1 {
		ly := sy(g.laserY) + cell/2
		c := mix(th.Accent2, th.Text, float64(steps(1-p)))
		rect(dst, bx-8, ly-1, bw+16, 2, withAlpha(c, uint8(255*steps(1-p))))
	}

	// El aviso de jugada (T-SPIN, TETRA, B2B), en grande y por escalones.
	if p := (a.t - g.callAt) / 1.0; g.callAt > 0 && p < 1 && g.call != "" {
		k := uint8(255 * steps(1-p))
		mx := bx + (bw-textWidth(g.call, BigFace))/2
		my := by + bh/2 - bigCap
		drawText(dst, g.call, BigFace, mx+1, my+1, withAlpha(th.Shadow, k))
		drawText(dst, g.call, BigFace, mx, my, withAlpha(th.Text, k))
	}

	// El aviso de "a tiempo": sube y se apaga por escalones.
	if p := (a.t - g.onBeatAt) / 0.8; g.onBeatAt > 0 && p < 1 {
		msg := fmt.Sprintf("%s +%d", T("tet_onbeat"), g.onBeatBonus)
		k := uint8(255 * steps(1-p))
		mx := bx + (bw-textWidth(msg, SmallFace))/2
		my := by + bh/3 - int(p*12)
		drawText(dst, msg, SmallFace, mx+1, my+1, withAlpha(th.Shadow, k))
		drawText(dst, msg, SmallFace, mx, my, withAlpha(th.Accent2, k))
	}

	// Pausa o fin, en el centro del pozo.
	msg := ""
	if g.over {
		msg = T("tet_over")
	}
	if msg != "" {
		mw := textWidth(msg, BigFace) + 12
		cy := by + bh/2
		panel(dst, bx+(bw-mw)/2, cy-bigCap/2-8, mw, bigCap+16, th.Bg2)
		drawTextShadow(dst, msg, BigFace, bx+(bw-mw)/2+6, cy-bigCap/2, th.Text, th.Shadow)
	}
	if g.over {
		a.drawHints(dst, []hint{{"A", T("h_again")}, {"SEL", T("h_menu")}})
	} else {
		a.drawHints(dst, []hint{{"A/B", T("h_rotate")}, {"X", T("h_hold")}, {"START", T("h_pause")}, {"SEL", T("h_menu")}})
	}
}

// openTetrisPause es la pausa de Bloques (Start): el menú de siempre, con
// la partida quieta detrás.
func (a *App) openTetrisPause() {
	g := a.tetris
	a.menu = &menu{
		title:    T("tet_paused"),
		subtitle: fmt.Sprintf("%s %d · %s %d", T("tet_score"), g.score, T("tet_level"), g.level()+1),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveTetBest(); a.tetris = a.newTetris() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveTetBest(); a.screen = Games }},
		},
	}
}

// bpmLabel es el tempo para el panel, o "--" si aún no se sabe.
func bpmLabel(bpm int) string {
	if bpm == 0 {
		return "--"
	}
	return fmt.Sprint(bpm)
}

// fever es el modo FEVER: cuatro piezas seguidas a tiempo.
func (g *tetris) fever() bool { return g.combo >= 4 }

// tetBottom es la fila más baja de la pieza en ese giro (dentro de su cuadro).
func tetBottom(p, r int) int {
	b := 0
	for _, c := range tetShapes[p][r] {
		b = max(b, c[1])
	}
	return b
}

// drawLane es el carril de ritmo: los golpes que vienen (por el tempo) se
// acercan a la línea de la izquierda; cuando llegan, es el momento de soltar.
func (a *App) drawLane(dst *ebiten.Image, x, y, w int) {
	th := a.theme
	const h = 9
	panel(dst, x, y, w, h, th.Bg2)
	hit := x + 6
	pulse := a.beatPulse()
	rect(dst, hit, y+1, 2, h-2, mix(th.Accent, th.Text, pulse)) // la línea
	p := a.beat.period()
	if p <= 0 || a.beat.at == 0 {
		return
	}
	const ahead = 1.6 // segundos que se ven
	// Los golpes que vienen, desde el último (de la rejilla).
	for k := 1; k <= 8; k++ {
		at := a.beat.at + float64(k)*p
		dt := at - a.beatClock()
		if dt < 0 || dt > ahead {
			continue
		}
		mx := hit + int(dt/ahead*float64(w-10))
		c := th.Accent2
		if dt < 0.12 {
			c = th.Text
		}
		rect(dst, mx, y+2, 2, h-4, c)
	}
}

// drawMini pinta la pieza p, pequeña, en una caja (para las siguientes y la
// guardada).
func (a *App) drawMini(dst *ebiten.Image, p, x, y, ns int, c color.RGBA) {
	panel(dst, x, y, ns*4+4, ns*2+4, a.theme.Bg2)
	minX, minY, maxX, maxY := 4, 4, 0, 0
	for _, cl := range tetShapes[p][0] {
		minX, minY = min(minX, cl[0]), min(minY, cl[1])
		maxX, maxY = max(maxX, cl[0]), max(maxY, cl[1])
	}
	ox := x + 2 + (4-(maxX-minX+1))*ns/2 - minX*ns
	oy := y + 2 + (2-(maxY-minY+1))*ns/2 - minY*ns
	for _, cl := range tetShapes[p][0] {
		// Cuadradas: a este tamaño, las esquinas recortadas las volvían puntos.
		rect(dst, ox+cl[0]*ns, oy+cl[1]*ns, ns-1, ns-1, c)
	}
}
