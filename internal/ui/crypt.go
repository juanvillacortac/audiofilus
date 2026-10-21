package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Cripta: un roguelike de ritmo. Una mazmorra por pisos donde solo se puede
// dar un paso (o un golpe) en el golpe de la música; los enemigos se mueven
// también al compás, cada uno con su patrón. Los pasos a tiempo seguidos
// suben el multiplicador (hasta FEVER, ×4) y un paso a destiempo o un golpe
// sin moverse lo rompen. El reloj es el ritmo que el medidor saca de la
// canción (meter.go); sin tempo todavía, un metrónomo de 110 BPM, y con la
// música en pausa el juego se queda quieto.

const (
	crW, crH      = 44, 30 // el mapa, en baldosas
	crTile        = 12     // píxeles por baldosa
	crLight       = 5      // radio de luz alrededor del jugador
	crStartHP     = 3
	crMaxHP       = 5
	crFallbackBPM = 110
	crAnim        = 0.09 // segundos de un paso (con su saltito)
)

// Las baldosas.
const (
	crVoid = iota
	crFloor
	crWall
	crSecret // una pared agrietada (cada cinco pisos): un golpe a tiempo la rompe
)

// Los tipos de cosas del mapa.
const (
	crSlime = iota
	crBat
	crSkeleton
	crArmored // esqueleto con armadura: dos golpes
	crCoin
	crHeart
)

// crEnt es un enemigo o un objeto.
type crEnt struct {
	kind         int
	x, y         int
	fx, fy       int     // de dónde venía (para animar el paso)
	movedAt      float64 // cuándo se movió
	hp           int
	step         int     // su cuenta de golpes (patrones)
	dir          int     // el slime: arriba o abajo
	hurtAt       float64 // cuándo recibió un golpe
	dead         bool
	telegraphing bool // el esqueleto levanta los brazos: el próximo golpe se mueve
}

// crFloat es un texto que sube y se apaga (+25, MISS...).
type crFloat struct {
	x, y int // en píxeles del mapa
	text string
	c    color.RGBA
	at   float64
}

// crypt es una partida.
type crypt struct {
	tiles      [crH][crW]uint8
	seen       [crH][crW]bool
	px, py     int // el jugador
	pfx, pfy   int
	pMovedAt   float64
	hp         int
	hurtAt     float64
	invulUntil float64 // hasta cuándo no recibe daño (reloj del ritmo)
	ents       []*crEnt
	disco      [crH][crW]bool // la sala secreta: su suelo cambia de color en cada golpe
	stairX     int
	stairY     int
	depth      int
	score      int
	best       int
	combo      int // pasos a tiempo seguidos
	over       bool
	overAt     float64
	floats     []crFloat
	camX, camY float64 // la cámara (centro, en píxeles del mapa), suavizada
	// El ritmo.
	// La rejilla propia del juego: sigue a la del medidor, pero suave, para
	// que los golpes no salten cuando el medidor corrige la fase o el tempo.
	per, anchor float64
	// Los golpes se recuerdan por su hora, no por su número: la numeración
	// del medidor vuelve a empezar al recolocarse, y con números un golpe
	// nuevo podía parecer ya usado (MISS sin fin).
	lastPlayerT   float64 // el golpe en que se movió
	lastResolvedT float64 // el último golpe resuelto (los enemigos ya actuaron)
	goodAt        float64 // el último paso a tiempo (el aviso de la barra)
	missAt        float64
	frozen        bool    // la música está en pausa
	bannerAt      float64 // el cartel del piso
	shakeAt       float64
}

func (a *App) cryptBestPath() string { return filepath.Join(a.paths.Data, "crypt.json") }

// newCrypt empieza una partida en el piso 1.
func (a *App) newCrypt() *crypt {
	g := &crypt{hp: crStartHP, depth: 1}
	if b, err := os.ReadFile(a.cryptBestPath()); err == nil {
		var v struct{ Best int }
		if json.Unmarshal(b, &v) == nil {
			g.best = v.Best
		}
	}
	seed := rand.Uint64()
	if a.offline {
		seed = 7 // las capturas: siempre la misma mazmorra
	}
	g.generate(rand.New(rand.NewPCG(seed, seed)))
	g.bannerAt = a.t
	a.syncCryptGrid(g)
	g.lastResolvedT = a.cryptClosed(g)
	g.lastPlayerT = g.lastResolvedT
	g.camX, g.camY = float64(g.px*crTile+crTile/2), float64(g.py*crTile+crTile/2)
	return g
}

// saveCryptBest guarda el récord si se superó.
func (a *App) saveCryptBest() {
	g := a.crypt
	if g == nil || g.score <= g.best || a.offline {
		return
	}
	g.best = g.score
	b, _ := json.Marshal(struct{ Best int }{g.best})
	_ = os.MkdirAll(a.paths.Data, 0o755)
	_ = os.WriteFile(a.cryptBestPath(), b, 0o644)
}

// crRoom es una sala rectangular.
type crRoom struct{ x, y, w, h int }

func (r crRoom) center() (int, int) { return r.x + r.w/2, r.y + r.h/2 }

// generate hace el piso: salas que no se pisan, unidas en orden por pasillos
// en L (así todas quedan conectadas), las paredes alrededor, el jugador en la
// primera, la escalera en la más lejana y enemigos y monedas en las demás.
// Cuanto más hondo, más enemigos y más duros.
func (g *crypt) generate(r *rand.Rand) {
	g.tiles = [crH][crW]uint8{}
	g.seen = [crH][crW]bool{}
	g.ents = nil
	var rooms []crRoom
	for tries := 0; tries < 200 && len(rooms) < 8; tries++ {
		w, h := 4+r.IntN(5), 4+r.IntN(4)
		rm := crRoom{1 + r.IntN(crW-w-2), 1 + r.IntN(crH-h-2), w, h}
		ok := true
		for _, o := range rooms {
			if rm.x <= o.x+o.w+1 && o.x <= rm.x+rm.w+1 && rm.y <= o.y+o.h+1 && o.y <= rm.y+rm.h+1 {
				ok = false
				break
			}
		}
		if ok {
			rooms = append(rooms, rm)
		}
	}
	for _, rm := range rooms {
		for y := rm.y; y < rm.y+rm.h; y++ {
			for x := rm.x; x < rm.x+rm.w; x++ {
				g.tiles[y][x] = crFloor
			}
		}
	}
	for i := 1; i < len(rooms); i++ {
		x0, y0 := rooms[i-1].center()
		x1, y1 := rooms[i].center()
		if r.IntN(2) == 0 {
			g.hline(x0, x1, y0)
			g.vline(y0, y1, x1)
		} else {
			g.vline(y0, y1, x0)
			g.hline(x0, x1, y1)
		}
	}
	for y := 0; y < crH; y++ {
		for x := 0; x < crW; x++ {
			if g.tiles[y][x] != crVoid {
				continue
			}
			for _, d := range [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}} {
				nx, ny := x+d[0], y+d[1]
				if nx >= 0 && ny >= 0 && nx < crW && ny < crH && g.tiles[ny][nx] == crFloor {
					g.tiles[y][x] = crWall
					break
				}
			}
		}
	}
	if g.depth%5 == 0 {
		g.hideRoom(r)
	}
	g.px, g.py = rooms[0].center()
	g.pfx, g.pfy = g.px, g.py
	// La escalera, en la sala más lejana.
	far, fd := len(rooms)-1, -1
	for i, rm := range rooms {
		cx, cy := rm.center()
		if d := abs(cx-g.px) + abs(cy-g.py); d > fd {
			far, fd = i, d
		}
	}
	g.stairX, g.stairY = rooms[far].x+r.IntN(rooms[far].w), rooms[far].y+r.IntN(rooms[far].h)
	// Enemigos: más y peores en cada piso.
	n := min(4+g.depth*2, 18)
	for i := 0; i < n; i++ {
		rm := rooms[1+r.IntN(len(rooms)-1)]
		x, y := rm.x+r.IntN(rm.w), rm.y+r.IntN(rm.h)
		if g.occupied(x, y) || (x == g.stairX && y == g.stairY) {
			continue
		}
		kind := crSlime
		switch roll := r.IntN(10); {
		case roll < 3:
			kind = crSlime
		case roll < 6:
			kind = crBat
		case g.depth >= 3 && roll >= 8:
			kind = crArmored
		default:
			kind = crSkeleton
		}
		hp := 1
		if kind == crArmored {
			hp = 2
		}
		g.ents = append(g.ents, &crEnt{kind: kind, x: x, y: y, fx: x, fy: y, hp: hp, dir: 1 - 2*r.IntN(2), step: r.IntN(2)})
	}
	// Unas monedas sueltas y, a veces, un corazón.
	for i := 0; i < 5; i++ {
		rm := rooms[r.IntN(len(rooms))]
		x, y := rm.x+r.IntN(rm.w), rm.y+r.IntN(rm.h)
		if !g.occupied(x, y) && !(x == g.px && y == g.py) {
			g.ents = append(g.ents, &crEnt{kind: crCoin, x: x, y: y, fx: x, fy: y})
		}
	}
	if r.IntN(2) == 0 {
		rm := rooms[1+r.IntN(len(rooms)-1)]
		x, y := rm.center()
		if !g.occupied(x, y) {
			g.ents = append(g.ents, &crEnt{kind: crHeart, x: x, y: y, fx: x, fy: y})
		}
	}
	g.reveal()
}

// hideRoom esconde una sala de 3×3 detrás de una pared, con una pared
// agrietada (crSecret) como puerta y un paso de pasillo: busca una pared con
// suelo a un lado y sitio libre detrás. Dentro, monedas y un corazón, y el suelo de discoteca.
func (g *crypt) hideRoom(r *rand.Rand) {
	g.disco = [crH][crW]bool{}
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for tries := 0; tries < 400; tries++ {
		x, y := 1+r.IntN(crW-2), 1+r.IntN(crH-2)
		if g.tiles[y][x] != crWall {
			continue
		}
		for _, d := range dirs {
			if !g.walkable(x-d[0], y-d[1]) {
				continue // del otro lado tiene que haber suelo
			}
			// Tras la pared, un paso de pasillo y la sala (centrada a tres
			// pasos), con su marco de paredes: todo eso tiene que estar vacío.
			cx, cy := x+3*d[0], y+3*d[1]
			ok := true
			for yy := cy - 2; yy <= cy+2 && ok; yy++ {
				for xx := cx - 2; xx <= cx+2 && ok; xx++ {
					ok = xx > 0 && yy > 0 && xx < crW-1 && yy < crH-1 && g.tiles[yy][xx] == crVoid
				}
			}
			if !ok {
				continue
			}
			for yy := cy - 2; yy <= cy+2; yy++ {
				for xx := cx - 2; xx <= cx+2; xx++ {
					g.tiles[yy][xx] = crWall
				}
			}
			for yy := cy - 1; yy <= cy+1; yy++ {
				for xx := cx - 1; xx <= cx+1; xx++ {
					g.tiles[yy][xx] = crFloor
					g.disco[yy][xx] = true
					if xx != cx || yy != cy {
						g.ents = append(g.ents, &crEnt{kind: crCoin, x: xx, y: yy, fx: xx, fy: yy})
					}
				}
			}
			px, py := x+d[0], y+d[1] // el pasillo
			g.tiles[py][px], g.disco[py][px] = crFloor, true
			g.ents = append(g.ents, &crEnt{kind: crHeart, x: cx, y: cy, fx: cx, fy: cy})
			g.tiles[y][x] = crSecret
			return
		}
	}
}

func (g *crypt) hline(x0, x1, y int) {
	for x := min(x0, x1); x <= max(x0, x1); x++ {
		g.tiles[y][x] = crFloor
	}
}

func (g *crypt) vline(y0, y1, x int) {
	for y := min(y0, y1); y <= max(y0, y1); y++ {
		g.tiles[y][x] = crFloor
	}
}

// at es lo que hay en una baldosa (nil si nada).
func (g *crypt) at(x, y int) *crEnt {
	for _, e := range g.ents {
		if !e.dead && e.x == x && e.y == y {
			return e
		}
	}
	return nil
}

func (g *crypt) occupied(x, y int) bool { return g.at(x, y) != nil }

// walkable dice si se puede pisar.
func (g *crypt) walkable(x, y int) bool {
	return x >= 0 && y >= 0 && x < crW && y < crH && g.tiles[y][x] == crFloor
}

// reveal descubre lo que alumbra el jugador.
func (g *crypt) reveal() {
	for y := g.py - crLight; y <= g.py+crLight; y++ {
		for x := g.px - crLight; x <= g.px+crLight; x++ {
			if x >= 0 && y >= 0 && x < crW && y < crH && (x-g.px)*(x-g.px)+(y-g.py)*(y-g.py) <= crLight*crLight {
				g.seen[y][x] = true
			}
		}
	}
}

// isEnemy dice si un tipo es un enemigo (no un objeto).
func isEnemy(kind int) bool { return kind <= crArmored }

// syncCryptGrid acerca la rejilla del juego a la del medidor: el tempo y la
// fase poco a poco (sin saltos que se vean ni se sientan) y de golpe solo si
// el tempo es otro de verdad (otra canción). Sin tempo, la que tenía (o, al
// empezar, un metrónomo de crFallbackBPM).
func (a *App) syncCryptGrid(g *crypt) {
	now := a.beatClock()
	tp, ta := a.beat.period(), a.beat.anchor
	if g.per == 0 {
		g.per, g.anchor = 60.0/crFallbackBPM, now
		if tp > 0 {
			g.per, g.anchor = tp, ta
		}
		return
	}
	followGrid(&g.per, &g.anchor, tp, ta, now)
}

// cryptTol es cuánto antes o después del golpe vale un paso.
func cryptTol(per float64) float64 { return math.Min(0.17, per*0.35) }

// cryptNearest es la hora del golpe más cercano a ahora y a cuánto queda
// (con signo: negativo si el golpe aún no llegó).
func (a *App) cryptNearest(g *crypt) (beat, off float64) {
	now := a.beatClock()
	beat = g.anchor + math.Round((now-g.anchor)/g.per)*g.per
	return beat, now - beat
}

// cryptCanStep dice si un paso ahora cae a tiempo en un golpe que aún no se
// usó ni se resolvió, y cuál.
func (a *App) cryptCanStep(g *crypt) (float64, bool) {
	beat, off := a.cryptNearest(g)
	half := g.per / 2
	return beat, math.Abs(off) <= cryptTol(g.per) && beat > g.lastPlayerT+half && beat > g.lastResolvedT+half
}

// cryptClosed es la hora del último golpe cuya ventana ya cerró (los
// enemigos actúan al cerrarse, después del jugador).
func (a *App) cryptClosed(g *crypt) float64 {
	now := a.beatClock()
	return g.anchor + math.Floor((now-g.anchor-cryptTol(g.per))/g.per)*g.per
}

// updateCrypt juega un fotograma.
func (a *App) updateCrypt() {
	g, in := a.crypt, &a.input
	// La música se controla sin salir, como en Bloques.
	switch {
	case in.Pressed(ActPause):
		a.send("toggle")
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	}
	if in.Pressed(ActMenu) {
		a.openCryptPause()
		return
	}
	if g.over {
		if in.Pressed(ActConfirm) && a.t-g.overAt > 0.6 {
			a.saveCryptBest()
			a.crypt = a.newCrypt()
		}
		return
	}
	// Sin música no hay ritmo: todo se queda quieto hasta que vuelva.
	if !a.hearing() && !a.offline {
		g.frozen = true
		return
	}
	a.syncCryptGrid(g)
	closed := a.cryptClosed(g)
	half := g.per / 2
	if g.frozen || closed < g.lastResolvedT-half { // vuelve la música, o la fase retrocedió
		g.frozen = false
		g.lastResolvedT, g.lastPlayerT = closed, math.Min(g.lastPlayerT, closed)
	}
	// El paso del jugador.
	dx, dy := 0, 0
	switch {
	case in.Pressed(ActUp):
		dy = -1
	case in.Pressed(ActDown):
		dy = 1
	case in.Pressed(ActLeft):
		dx = -1
	case in.Pressed(ActRight):
		dx = 1
	}
	if dx != 0 || dy != 0 {
		if beat, ok := a.cryptCanStep(g); ok {
			g.lastPlayerT, g.goodAt = beat, a.t
			a.cryptAct(dx, dy)
		} else {
			a.cryptMiss()
		}
	}
	// El golpe que cerró: los enemigos actúan (uno por fotograma; si se
	// saltaron varios, no se ponen al día de golpe).
	if closed > g.lastResolvedT+half && !g.over {
		g.lastResolvedT = closed
		a.cryptEnemies(closed)
	}
	// La cámara sigue al jugador.
	tx, ty := float64(g.px*crTile+crTile/2), float64(g.py*crTile+crTile/2)
	g.camX += (tx - g.camX) * 0.2
	g.camY += (ty - g.camY) * 0.2
}

// cryptMult es el multiplicador por la racha: ×1 a ×4 (FEVER).
func (g *crypt) mult() int { return 1 + min(3, g.combo/8) }

// cryptMiss es un paso a destiempo: no se mueve y se pierde la racha.
func (a *App) cryptMiss() {
	g := a.crypt
	g.combo, g.missAt = 0, a.t
	g.floats = append(g.floats, crFloat{g.px * crTile, g.py*crTile - 4, "MISS", a.theme.Dim, a.t})
	a.sound("cr_miss", 2)
}

// cryptAct es un paso a tiempo: moverse, o atacar si hay un enemigo.
func (a *App) cryptAct(dx, dy int) {
	g := a.crypt
	g.combo++
	if g.combo == 24 {
		g.floats = append(g.floats, crFloat{g.px * crTile, g.py*crTile - 10, "FEVER!", a.theme.Accent2, a.t})
	}
	nx, ny := g.px+dx, g.py+dy
	if e := g.at(nx, ny); e != nil && isEnemy(e.kind) {
		e.hp--
		e.hurtAt = a.t
		g.shakeAt = a.t
		if e.hp <= 0 {
			e.dead = true
			pts := 25 * g.mult() * g.depth
			g.score += pts
			g.floats = append(g.floats, crFloat{nx * crTile, ny * crTile, fmt.Sprintf("+%d", pts), a.theme.Accent, a.t})
			// Deja una moneda.
			g.ents = append(g.ents, &crEnt{kind: crCoin, x: nx, y: ny, fx: nx, fy: ny, movedAt: a.t})
			a.sound("cr_kill", 2)
		} else {
			a.sound("cr_hit", 2)
		}
		return
	}
	if nx >= 0 && ny >= 0 && nx < crW && ny < crH && g.tiles[ny][nx] == crSecret {
		g.tiles[ny][nx] = crFloor
		g.disco[ny][nx] = true
		g.shakeAt = a.t
		g.floats = append(g.floats, crFloat{nx * crTile, ny * crTile, "SECRET!", a.theme.Accent2, a.t})
		a.sound("cr_stairs", 3)
		return
	}
	if !g.walkable(nx, ny) {
		a.sound("cr_bump", 1)
		return // contra la pared: el paso cuenta para la racha, pero no se mueve
	}
	g.pfx, g.pfy, g.px, g.py, g.pMovedAt = g.px, g.py, nx, ny, a.t
	g.reveal()
	a.sound("cr_step", 1)
	if e := g.at(nx, ny); e != nil {
		switch e.kind {
		case crCoin:
			e.dead = true
			pts := 10 * g.mult()
			g.score += pts
			g.floats = append(g.floats, crFloat{nx * crTile, ny * crTile, fmt.Sprintf("+%d", pts), a.theme.Text, a.t})
			a.sound("cr_coin", 2)
		case crHeart:
			e.dead = true
			g.hp = min(crMaxHP, g.hp+1)
			g.floats = append(g.floats, crFloat{nx * crTile, ny * crTile, "+1", a.theme.Accent2, a.t})
			a.sound("cr_coin", 2)
		}
	}
	if nx == g.stairX && ny == g.stairY {
		a.cryptDescend()
	}
}

// cryptDescend baja al piso siguiente.
func (a *App) cryptDescend() {
	g := a.crypt
	g.depth++
	g.score += 100 * g.depth
	g.generate(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	g.camX, g.camY = float64(g.px*crTile+crTile/2), float64(g.py*crTile+crTile/2)
	g.bannerAt, g.floats = a.t, nil
	a.sound("cr_stairs", 3)
}

// cryptEnemies mueve a los enemigos en el golpe k, cada uno con su patrón.
func (a *App) cryptEnemies(beat float64) {
	g := a.crypt
	for _, e := range g.ents {
		if e.dead || !isEnemy(e.kind) {
			continue
		}
		e.step++
		dx, dy := 0, 0
		switch e.kind {
		case crSlime: // rebota arriba y abajo cada dos golpes
			if e.step%2 == 0 {
				dy = e.dir
				e.dir = -e.dir
			}
		case crBat: // revolotea al azar en cada golpe
			d := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}[rand.IntN(4)]
			dx, dy = d[0], d[1]
		case crSkeleton, crArmored: // avisa un golpe y después persigue
			near := abs(e.x-g.px)+abs(e.y-g.py) <= 9
			if !near {
				break
			}
			if !e.telegraphing {
				e.telegraphing = true
				break
			}
			e.telegraphing = false
			ddx, ddy := g.px-e.x, g.py-e.y
			if abs(ddx) >= abs(ddy) {
				dx = isign(ddx)
			} else {
				dy = isign(ddy)
			}
			if !g.walkable(e.x+dx, e.y+dy) { // probar por el otro eje
				dx, dy = 0, 0
				if ddy != 0 && abs(ddx) >= abs(ddy) {
					dy = isign(ddy)
				} else if ddx != 0 {
					dx = isign(ddx)
				}
			}
		}
		if dx == 0 && dy == 0 {
			continue
		}
		nx, ny := e.x+dx, e.y+dy
		if nx == g.px && ny == g.py {
			a.cryptHurt(beat, e)
			continue
		}
		if !g.walkable(nx, ny) || g.occupied(nx, ny) || (nx == g.stairX && ny == g.stairY) {
			if e.kind == crSlime {
				e.dir = -e.dir
			}
			continue
		}
		e.fx, e.fy, e.x, e.y, e.movedAt = e.x, e.y, nx, ny, a.t
	}
}

// cryptHurt es un golpe de un enemigo al jugador.
func (a *App) cryptHurt(beat float64, e *crEnt) {
	g := a.crypt
	if beat < g.invulUntil {
		return
	}
	g.hp--
	g.combo = 0
	g.hurtAt, g.shakeAt, g.invulUntil = a.t, a.t, beat+g.per*1.5
	e.movedAt = a.t // un amago hacia el jugador
	a.sound("cr_hurt", 3)
	if g.hp <= 0 {
		g.over, g.overAt = true, a.t
		a.saveCryptBest()
		a.sound("cr_dead", 4)
	}
}

// openCryptPause es el menú de pausa (Start).
func (a *App) openCryptPause() {
	g := a.crypt
	a.menu = &menu{
		title:    T("cr_paused"),
		subtitle: fmt.Sprintf("%s %d · %s %d", T("cr_depth"), g.depth, T("tet_score"), g.score),
		items: []menuItem{
			{label: T("tet_resume"), icon: "play", do: func() {}},
			{label: T("tet_new"), icon: "repeat", do: func() { a.saveCryptBest(); a.crypt = a.newCrypt() }},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveCryptBest(); a.screen = Games }},
		},
	}
}

// Los dibujos de la cripta, de 10×10: '#' el color de la cosa, 'o' su tono
// oscuro, '*' los ojos o el brillo (el color del fondo o el texto).
var crArt = map[string][]string{
	"hero": {
		"...####...",
		"..######..",
		"..#*##*#..",
		"..######..",
		"...#oo#...",
		".########.",
		"#.######.#",
		"..######..",
		"..##..##..",
		"..oo..oo..",
	},
	"slime": {
		"..........",
		"..........",
		"...####...",
		"..######..",
		".##*##*##.",
		".########.",
		"##########",
		"##########",
		".oooooooo.",
		"..........",
	},
	"bat1": {
		"#........#",
		"##......##",
		"###.##.###",
		".########.",
		"..#*##*#..",
		"...####...",
		"....oo....",
		"..........",
		"..........",
		"..........",
	},
	"bat2": {
		"..........",
		"..........",
		"....##....",
		"...####...",
		".##*##*##.",
		"##########",
		"#.######.#",
		"#..o..o..#",
		"..........",
		"..........",
	},
	"skel": {
		"...####...",
		"..######..",
		"..#o##o#..",
		"..######..",
		"...#oo#...",
		"....##....",
		".########.",
		"...####...",
		"...#..#...",
		"..##..##..",
	},
	"skelup": {
		"#..####..#",
		"#.######.#",
		"#.#o##o#.#",
		"##.####.##",
		"...#oo#...",
		"....##....",
		"...####...",
		"...####...",
		"...#..#...",
		"..##..##..",
	},
	"stairs": {
		"##########",
		"#********#",
		"#oooooooo#",
		"#.******.#",
		"#.oooooo.#",
		"#..****..#",
		"#..oooo..#",
		"#...**...#",
		"#........#",
		"##########",
	},
	// Los enemigos de Duelo (duel.go), en 1 bit: un solo color y los
	// detalles hechos con huecos.
	"du_vinyl": {
		"...........####...........",
		"........##########........",
		"......###........###......",
		".....##..########..##.....",
		"....##.###########..##....",
		"...##.###...##...##..##...",
		"..##.###.########.#.#.##..",
		"..#.###.###....###.#.#.#..",
		".##.##.##.######.##..#.##.",
		".#.##.##.########.##..#.#.",
		".#.##.###..####..###.##.#.",
		"##.##.#.##..##..##.#.##.##",
		"##.####.####..####.####.##",
		"##.####.##.#.#.#.#.####.##",
		"##.##.#.#........#.#.##.##",
		".#.##.###.#.#.#.####.##.#.",
		".#.##.##.########.##.##.#.",
		".##.##.##.######.##.##.##.",
		"..#.###.###....###.###.#..",
		"..##.###.########.###.##..",
		"...##.###...##...###.##...",
		"....##.############.##....",
		".....##..########..##.....",
		"......###........###......",
		"........##########........",
		"...........####...........",
	},
	"du_tape": {
		"##########################",
		"#........................#",
		"#.######################.#",
		"#.###..#################.#",
		"#.##...##........#....##.#",
		"#.##..#####....#####..##.#",
		"#.##..#...#....#...#..##.#",
		"#.##.##.#.##..##.#.##.##.#",
		"#.##..#...#....#...#..##.#",
		"#.##..#####....#####..##.#",
		"#.##....#........#....##.#",
		"#.######################.#",
		"#.######################.#",
		"#........................#",
		"#####................#####",
		"......#.....#...#..#......",
		".......#....#..#..#.......",
		"........#...#.#...#.......",
		"........#..#..#..#........",
		"........#..#.#...#........",
		"........#.#.#....#........",
		".......#.#..#.....#.......",
		"......#.#...#......#......",
		".....#..#....#......#.....",
	},
	"du_metro": {
		"........######........",
		"........######........",
		"........##..##........",
		".......###..###.......",
		".......###..###.......",
		".......###..###.......",
		"......####..####......",
		"......####..####......",
		"......####..####......",
		".....#####..#####.....",
		".....#####..#####.....",
		".....#####..#####.....",
		"....######..######....",
		"....######..######....",
		"....######..######....",
		"...###..######..###...",
		"...####..####..####...",
		"...################...",
		"..##################..",
		"..#####........#####..",
		"..######.#.#.#.#####..",
		".####################.",
		".####################.",
		"######################",
		"######################",
		"######################",
	},
	// Las de Naves (shooter.go).
	"ship": {
		"....##....",
		"....##....",
		"...####...",
		"...#**#...",
		"..######..",
		".##.##.##.",
		"##########",
		"##.####.##",
		"#...##...#",
		"....oo....",
	},
	"ufo": {
		"..........",
		"...####...",
		"..#*##*#..",
		".########.",
		"##o#oo#o##",
		".########.",
		"..#....#..",
		".#......#.",
		"..........",
		"..........",
	},
	"bug": {
		".#......#.",
		"..#....#..",
		"..######..",
		".##*##*##.",
		"##########",
		"#.######.#",
		"#.#....#.#",
		"...#..#...",
		"..........",
		"..........",
	},
	"coin": {
		"..........",
		"..........",
		"...####...",
		"..##**##..",
		"..#*##*#..",
		"..#*##*#..",
		"..##**##..",
		"...####...",
		"..........",
		"..........",
	},
	"heart": {
		"..........",
		"..........",
		"..##..##..",
		".#*######.",
		".########.",
		"..######..",
		"...####...",
		"....##....",
		"..........",
		"..........",
	},
}

// crSprites guarda los dibujos ya hechos, por dibujo y colores.
var crSprites = map[string]*ebiten.Image{}

// crSprite es el dibujo name con el color c (y su tono oscuro y su brillo).
// Los dibujos son de cualquier tamaño (los de Cripta, de 10×10).
func crSprite(name string, c, eye color.RGBA) *ebiten.Image {
	key := fmt.Sprintf("%s%v%v", name, c, eye)
	if img, ok := crSprites[key]; ok {
		return img
	}
	art := crArt[name]
	img := ebiten.NewImage(len(art[0]), len(art))
	dark := mix(c, color.RGBA{0, 0, 0, 0xff}, 0.45)
	for y, row := range art {
		for x, ch := range row {
			switch ch {
			case '#':
				img.Set(x, y, c)
			case 'o':
				img.Set(x, y, dark)
			case '*':
				img.Set(x, y, eye)
			}
		}
	}
	crSprites[key] = img
	return img
}

// drawCrypt pinta la partida.
func (a *App) drawCrypt(dst *ebiten.Image) {
	g, th := a.crypt, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	top, bottom := topH, h-bottomH-22 // la barra de ritmo va abajo
	// Detrás de la mazmorra, la música: el fondo animado de la app (ya está
	// pintado) algo oscurecido, y un sello que la escucha (drawCryptViz).
	rect(dst, 0, top, w, bottom-top, withAlpha(th.Shadow, 0xa8))
	a.drawCryptViz(dst, top, bottom)
	// El temblor de un golpe.
	sx, sy := 0, 0
	if d := a.t - g.shakeAt; d >= 0 && d < 0.18 {
		k := 1 - d/0.18
		sx, sy = int(math.Round(2*k*math.Sin(a.t*90))), int(math.Round(2*k*math.Cos(a.t*77)))
	}
	ox := w/2 - int(math.Round(g.camX)) + sx
	oy := (top+bottom)/2 - int(math.Round(g.camY)) + sy
	// Con el dedo: tocar a un lado del héroe es dar un paso hacia ahí, en el
	// momento de tocar (no al soltar: el paso va al golpe).
	heroX, heroY := ox+g.px*crTile+crTile/2, oy+g.py*crTile+crTile/2
	a.pressZone(0, top, w, bottom-top, func(px, py int) {
		dx, dy := px-heroX, py+top-heroY
		switch {
		case abs(dx) >= abs(dy) && dx > 0:
			a.input.inject(ActRight)
		case abs(dx) >= abs(dy):
			a.input.inject(ActLeft)
		case dy > 0:
			a.input.inject(ActDown)
		default:
			a.input.inject(ActUp)
		}
	})
	pulse, beatN := a.cryptPulse(g)
	fever := g.mult() == 4
	lit1, lit2 := th.Accent, th.Accent2
	if !fever {
		lit2 = th.Accent
	}
	clip := dst.SubImage(image.Rect(0, top, w, bottom)).(*ebiten.Image)
	x0, x1 := max(0, -ox/crTile-1), min(crW, (w-ox)/crTile+1)
	y0, y1 := max(0, (top-oy)/crTile-1), min(crH, (bottom-oy)/crTile+1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !g.seen[y][x] {
				continue
			}
			t := g.tiles[y][x]
			if t == crVoid {
				continue
			}
			px, py := ox+x*crTile, oy+y*crTile
			d := math.Hypot(float64(x-g.px), float64(y-g.py))
			light := math.Max(0.45, 1-d/float64(crLight+4))
			switch t {
			case crWall, crSecret:
				// Un bloque de piedra: cara, borde de arriba claro y sombra.
				c := mix(th.Bg2, th.Text, 0.16)
				rect(clip, px, py, crTile, crTile, scaleRGB(c, light))
				rect(clip, px, py, crTile, 2, scaleRGB(mix(th.Bg2, th.Text, 0.35), light))
				rect(clip, px, py+crTile-2, crTile, 2, scaleRGB(mix(c, th.Shadow, 0.5), light))
				if (x+y)%3 == 0 && t == crWall {
					rect(clip, px+3, py+5, 4, 1, scaleRGB(mix(c, th.Shadow, 0.35), light))
				}
				if t == crSecret { // la grieta, en zigzag
					crack := scaleRGB(mix(c, th.Shadow, 0.7), light)
					for i, dx := range []int{5, 6, 5, 4, 5, 6, 7, 6} {
						rect(clip, px+dx, py+2+i, 1, 1, crack)
					}
				}
			case crFloor:
				// El suelo de discoteca: las baldosas se encienden por
				// turnos en cada golpe, con más color en FEVER.
				c := mix(th.Bg2, th.Text, 0.05)
				if (x+y+beatN)%2 == 0 {
					lc := lit1
					if (x/2+y/2+beatN)%2 == 0 {
						lc = lit2
					}
					amt := 0.16 + 0.34*pulse
					if fever {
						amt += 0.15
					}
					c = mix(c, lc, amt)
				}
				if g.disco[y][x] {
					// La sala secreta: cada baldosa de un color, que gira en
					// cada golpe.
					c = hsvRGB(float64(x*3+y*5+beatN)/9, 0.7, 0.55+0.4*pulse)
				}
				rect(clip, px+1, py+1, crTile-1, crTile-1, scaleRGB(c, light))
			}
		}
	}
	// La escalera.
	if g.seen[g.stairY][g.stairX] {
		px, py := ox+g.stairX*crTile, oy+g.stairY*crTile
		// Un hueco con peldaños que bajan hacia lo oscuro; brilla en el golpe.
		rect(clip, px+1, py+1, crTile-1, crTile-1, color.RGBA{0, 0, 0, 0xff})
		edge := mix(th.Bg2, th.Text, 0.3+0.3*pulse)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(px+1), float64(py+1))
		clip.DrawImage(crSprite("stairs", edge, mix(th.Text, th.Accent, 0.3*pulse)), op)
	}
	// Las cosas, con su paso animado (un saltito).
	for _, e := range g.ents {
		if e.dead || !g.seen[e.y][e.x] {
			continue
		}
		f := math.Min(1, (a.t-e.movedAt)/crAnim)
		ex := float64(ox) + (float64(e.fx)+(float64(e.x-e.fx))*f)*crTile + 1
		ey := float64(oy) + (float64(e.fy)+(float64(e.y-e.fy))*f)*crTile + 1 - 3*math.Sin(f*math.Pi)
		var name string
		c := th.Text
		switch e.kind {
		case crSlime:
			name, c = "slime", th.Accent
			ey += pulse * 1 // se aplasta en el golpe
		case crBat:
			name, c = "bat1", th.Accent2
			if int(a.t*8)%2 == 1 {
				name = "bat2"
			}
		case crSkeleton, crArmored:
			name = "skel"
			if e.telegraphing {
				name = "skelup"
			}
			if e.kind == crArmored {
				c = mix(th.Text, th.Accent2, 0.5)
			}
		case crCoin:
			name, c = "coin", mix(th.Accent, th.Text, 0.4)
			ey -= math.Abs(math.Sin(a.t*4)) * 1.5
		case crHeart:
			name, c = "heart", th.Accent2
		}
		if a.t-e.hurtAt < 0.12 {
			c = th.Text
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(math.Round(ex), math.Round(ey))
		clip.DrawImage(crSprite(name, c, th.Shadow), op)
	}
	// El jugador.
	f := math.Min(1, (a.t-g.pMovedAt)/crAnim)
	hx := float64(ox) + (float64(g.pfx)+(float64(g.px-g.pfx))*f)*crTile + 1
	hy := float64(oy) + (float64(g.pfy)+(float64(g.py-g.pfy))*f)*crTile + 1 - 3*math.Sin(f*math.Pi)
	hc := th.Text
	if a.t-g.hurtAt < 0.3 && int(a.t*20)%2 == 0 {
		hc = th.Accent2
	}
	if !g.over {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(math.Round(hx), math.Round(hy))
		clip.DrawImage(crSprite("hero", hc, th.Accent), op)
	}
	// Los textos que suben.
	keep := g.floats[:0]
	for _, fl := range g.floats {
		d := a.t - fl.at
		if d > 0.8 {
			continue
		}
		keep = append(keep, fl)
		al := uint8(255 * math.Min(1, 2*(0.8-d)))
		drawTextShadow(clip, fl.text, SmallFace, ox+fl.x+crTile/2-textWidth(fl.text, SmallFace)/2, oy+fl.y-int(d*18), withAlpha(fl.c, al), withAlpha(th.Shadow, al))
	}
	g.floats = keep
	// El rojo de un golpe recibido.
	if d := a.t - g.hurtAt; d < 0.25 {
		rect(dst, 0, top, w, bottom-top, withAlpha(th.Accent2, uint8(70*(1-d/0.25))))
	}
	a.drawCryptHUD(dst, top, bottom)
	if g.frozen {
		msg := T("cr_play_music")
		bw := textWidth(msg, SmallFace) + 16
		panel(dst, (w-bw)/2, (top+bottom)/2-10, bw, 20, withAlpha(th.Shadow, 0xe0))
		drawText(dst, msg, SmallFace, (w-bw)/2+8, (top+bottom)/2-3, th.Text)
	}
	if d := a.t - g.bannerAt; d < 1.4 && !g.over {
		s := fmt.Sprintf(T("cr_floor"), g.depth)
		al := uint8(255 * math.Min(1, (1.4-d)*3))
		y := (top+bottom)/2 - 34 - int(math.Max(0, 0.15-d)*80)
		drawTextShadow(dst, s, BigFace, (w-textWidth(s, BigFace))/2, y, withAlpha(th.Text, al), withAlpha(th.Shadow, al))
	}
	if g.over {
		a.drawCryptOver(dst, top, bottom)
	}
	a.drawHints(dst, []hint{{"<>^v", T("cr_h_move")}, {"START", T("h_pause")}, {"SEL", T("h_menu")}})
}

// drawCryptHUD pinta los corazones, los puntos, el multiplicador y la barra de
// ritmo: marcas que llegan al corazón del centro justo en cada golpe.
func (a *App) drawCryptHUD(dst *ebiten.Image, top, bottom int) {
	g, th := a.crypt, a.theme
	w := dst.Bounds().Dx()
	for i := 0; i < crMaxHP; i++ {
		c := withAlpha(th.Dim, 0x60)
		if i < g.hp {
			c = th.Accent2
		}
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(margin+i*11), float64(top+4))
		dst.DrawImage(crSprite("heart", c, mix(c, th.Text, 0.5)), op)
	}
	right := fmt.Sprintf("%s %d   %d", T("cr_depth"), g.depth, g.score)
	drawTextShadow(dst, right, SmallFace, w-margin-textWidth(right, SmallFace), top+6, th.Text, th.Shadow)
	if m := g.mult(); m > 1 {
		s := fmt.Sprintf("x%d", m)
		c := th.Accent
		if m == 4 {
			s, c = "FEVER x4", th.Accent2
		}
		drawTextShadow(dst, s, SmallFace, w-margin-textWidth(s, SmallFace), top+16, c, th.Shadow)
	}
	// La barra de ritmo: las marcas de los próximos golpes llegan al corazón
	// justo en cada golpe; la zona clara alrededor es la ventana en la que un
	// paso vale, y una marca se enciende al entrar en ella. La del golpe que
	// ya se usó desaparece. Todo con el mismo reloj que el juego.
	by := bottom + 4
	rect(dst, 0, bottom, w, 22, th.Bg)
	rect(dst, 0, bottom, w, 1, withAlpha(th.Accent, 0x50))
	cx := w / 2
	per, anchor := g.per, g.anchor
	now := a.beatClock()
	tol := cryptTol(per)
	span := float64(min(w/2-20, 150))
	pxs := span / (per * 4) // píxeles por segundo
	win := int(tol * pxs)
	rect(dst, cx-win, by+1, 2*win, 13, withAlpha(th.Accent, 0x22))
	rect(dst, cx-win, by+1, 1, 13, withAlpha(th.Accent, 0x90))
	rect(dst, cx+win-1, by+1, 1, 13, withAlpha(th.Accent, 0x90))
	base := math.Floor((now - anchor) / per)
	for k := 0; k <= 4 && !g.frozen; k++ {
		bt := anchor + (base+float64(k))*per
		d := bt - now // segundos que faltan (negativo: ya pasó)
		if d < -tol || d > per*4 || math.Abs(bt-g.lastPlayerT) < per/2 {
			continue
		}
		off := int(math.Round(d * pxs))
		c := withAlpha(th.Accent, uint8(255*(1-math.Max(0, d)/(per*4)*0.7)))
		if math.Abs(d) <= tol {
			c = th.Text
		}
		rect(dst, cx-off-3, by+3, 2, 9, c)
		rect(dst, cx+off+1, by+3, 2, 9, c)
	}
	pulse, _ := a.cryptPulse(g)
	hc := th.Accent2
	if a.t-g.missAt < 0.25 {
		hc = th.Dim
	}
	// Late en el golpe: a escala entera (×2, y ×3 justo en el golpe), para
	// que los píxeles no se deformen.
	op := &ebiten.DrawImageOptions{}
	k := 2.0
	if pulse > 0.45 {
		k = 3
	}
	op.GeoM.Translate(-5, -5)
	op.GeoM.Scale(k, k)
	op.GeoM.Translate(float64(cx), float64(by+7))
	dst.DrawImage(crSprite("heart", hc, th.Text), op)
	// El aviso del último paso, sobre el corazón.
	label, lc, at := "", th.Text, math.Max(g.goodAt, g.missAt)
	switch {
	case a.t-at > 0.35:
	case g.missAt > g.goodAt:
		label, lc = "MISS", th.Dim
	default:
		label, lc = "GOOD", th.Accent
	}
	if label != "" {
		drawTextShadow(dst, label, SmallFace, cx-textWidth(label, SmallFace)/2, bottom-10, lc, th.Shadow)
	}
}

// cryptPulse es el latido del golpe (1 justo en él, 0 a los 0,2 s) y el
// número del último golpe, con el reloj del juego: el suelo y el corazón
// laten a la vez que las marcas llegan al centro.
func (a *App) cryptPulse(g *crypt) (float64, int) {
	if g.frozen || g.per == 0 {
		return 0, 0
	}
	n := math.Floor((a.beatClock() - g.anchor) / g.per)
	since := a.beatClock() - (g.anchor + n*g.per)
	return math.Max(0, 1-since/0.2), int(n)
}

// drawCryptOver es el cartel de fin de partida.
func (a *App) drawCryptOver(dst *ebiten.Image, top, bottom int) {
	g, th := a.crypt, a.theme
	w := dst.Bounds().Dx()
	pw, ph := min(200, w-20), 78
	x, y := (w-pw)/2, (top+bottom)/2-ph/2
	rect(dst, x+3, y+3, pw, ph, th.Shadow)
	panel(dst, x, y, pw, ph, th.Bg2)
	rect(dst, x, y, pw, 1, th.Accent2)
	title := T("cr_dead")
	drawTextShadow(dst, title, BigFace, x+(pw-textWidth(title, BigFace))/2, y+8, th.Text, th.Shadow)
	l1 := fmt.Sprintf("%s %d · %s %d", T("cr_depth"), g.depth, T("tet_score"), g.score)
	drawText(dst, l1, SmallFace, x+(pw-textWidth(l1, SmallFace))/2, y+32, th.Accent)
	l2 := fmt.Sprintf("%s %d", T("tet_best"), max(g.best, g.score))
	drawText(dst, l2, SmallFace, x+(pw-textWidth(l2, SmallFace))/2, y+44, th.Dim)
	if a.t-g.overAt > 0.6 {
		l3 := T("cr_again")
		drawText(dst, l3, SmallFace, x+(pw-textWidth(l3, SmallFace))/2, y+60, th.Text)
	}
}

// scaleRGB oscurece un color (la luz de la antorcha).
func scaleRGB(c color.RGBA, k float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * k), uint8(float64(c.G) * k), uint8(float64(c.B) * k), c.A}
}

// isign es el signo de un entero: -1, 0 o 1.
func isign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// drawCryptViz pinta, detrás de la mazmorra y casi transparente, un sello
// que escucha la música: un círculo con una estrella de siete puntas que gira
// despacio (más deprisa con más música) y palpita con los graves, doce runas
// alrededor que se encienden con cada banda del espectro, y unas brasas que
// suben. Líneas de un píxel, sin suavizar, como el resto.
func (a *App) drawCryptViz(dst *ebiten.Image, top, bottom int) {
	g, th := a.crypt, a.theme
	w, h := dst.Bounds().Dx(), bottom-top
	_, bass, bands, ok := a.meterNow()
	if !ok || g.frozen {
		bass = 0
	}
	pulse, _ := a.cryptPulse(g)
	cx, cy := float32(w)/2, float32(top)+float32(h)/2
	r := float32(min(w, h))*0.42 + float32(bass*4+pulse*2)
	ring := withAlpha(th.Accent, uint8(28+40*pulse))
	vector.StrokeCircle(dst, cx, cy, r, 1, ring, false)
	vector.StrokeCircle(dst, cx, cy, r*0.82, 1, withAlpha(th.Accent, 22), false)
	vector.StrokeCircle(dst, cx, cy, r*0.3, 1, withAlpha(th.Accent2, uint8(18+30*bass)), false)
	// La estrella {7/3}: siete puntas unidas de tres en tres.
	rot := a.t*0.08 + float64(bass)*0.3
	pt := func(i int, rr float32) (float32, float32) {
		an := rot + float64(i)*2*math.Pi/7 - math.Pi/2
		return cx + rr*float32(math.Cos(an)), cy + rr*float32(math.Sin(an))
	}
	star := withAlpha(th.Accent2, uint8(20+36*pulse))
	for i := 0; i < 7; i++ {
		x0, y0 := pt(i, r*0.82)
		x1, y1 := pt(i+3, r*0.82)
		vector.StrokeLine(dst, x0, y0, x1, y1, 1, star, false)
	}
	// Las runas: una marca por banda en el anillo de fuera, al revés que la
	// estrella; más larga y más clara cuanto más suena su banda.
	for i := 0; i < 12; i++ {
		v := 0.0
		if ok {
			v = bands[i*len(bands)/12]
		}
		an := -rot*0.6 + float64(i)*2*math.Pi/12
		c, sn := float32(math.Cos(an)), float32(math.Sin(an))
		l := 3 + float32(v)*7
		vector.StrokeLine(dst, cx+c*(r+2), cy+sn*(r+2), cx+c*(r+2+l), cy+sn*(r+2+l), 1, withAlpha(th.Text, uint8(24+90*v)), false)
	}
	// Brasas: suben despacio, cada una a su ritmo, sin estado (su posición
	// sale del tiempo y de su número).
	for i := 0; i < 14; i++ {
		f := float64(i)
		speed := 6 + math.Mod(f*7.3, 9)
		y := float64(h) - math.Mod(a.t*speed+f*37, float64(h))
		x := math.Mod(f*97.1, float64(w)) + 4*math.Sin(a.t*0.7+f)
		al := uint8(30 + 50*math.Sin(math.Pi*y/float64(h)))
		rect(dst, int(x), top+int(y), 1, 1, withAlpha(th.Accent2, al))
	}
}

// hsvRGB es un color por tono (0..1, da vueltas), saturación y brillo.
func hsvRGB(h, sat, v float64) color.RGBA {
	h -= math.Floor(h)
	i := int(h * 6)
	f := h*6 - float64(i)
	p, q, t := v*(1-sat), v*(1-sat*f), v*(1-sat*(1-f))
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 0xff}
}
