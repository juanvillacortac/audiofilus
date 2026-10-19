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
	invulBeat  int // hasta qué golpe no recibe daño
	ents       []*crEnt
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
	lastPlayerBeat int     // el último golpe en que se movió
	lastBeat       int     // el último golpe resuelto (los enemigos ya actuaron)
	moved          bool    // ya dio algún paso (antes, no se pierde el combo)
	pulseAt        float64 // el último golpe (para el suelo)
	beatN          int     // cuántos golpes van (el suelo alterna)
	missAt         float64
	fallback       float64 // el ancla del metrónomo propio
	frozen         bool    // la música está en pausa
	bannerAt       float64 // el cartel del piso
	shakeAt        float64
}

func (a *App) cryptBestPath() string { return filepath.Join(a.paths.Data, "cripta.json") }

// newCrypt empieza una partida en el piso 1.
func (a *App) newCrypt() *crypt {
	g := &crypt{hp: crStartHP, depth: 1, fallback: a.beatClock()}
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
	_, g.lastBeat = a.cryptBeat(g)
	g.lastPlayerBeat = g.lastBeat
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

// cryptBeat da la rejilla del ritmo: el periodo y el último golpe cuya
// ventana ya cerró (los enemigos actúan al cerrarse, después del jugador).
func (a *App) cryptBeat(g *crypt) (per float64, closed int) {
	per, anchor := a.beat.period(), a.beat.anchor
	if per == 0 {
		per, anchor = 60.0/crFallbackBPM, g.fallback
	}
	tol := cryptTol(per)
	return per, int(math.Floor((a.beatClock() - anchor - tol) / per))
}

// cryptTol es cuánto antes o después del golpe vale un paso.
func cryptTol(per float64) float64 { return math.Min(0.14, per*0.3) }

// cryptNearest es el golpe más cercano a ahora y a cuánto queda (con signo).
func (a *App) cryptNearest(g *crypt) (k int, off float64) {
	per, anchor := a.beat.period(), a.beat.anchor
	if per == 0 {
		per, anchor = 60.0/crFallbackBPM, g.fallback
	}
	t := a.beatClock()
	k = int(math.Round((t - anchor) / per))
	return k, t - (anchor + float64(k)*per)
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
	if !a.isPlaying && !a.offline {
		g.frozen = true
		return
	}
	per, closed := a.cryptBeat(g)
	if g.frozen || closed-g.lastBeat > 3 { // vuelve la música, o la rejilla saltó
		g.frozen = false
		g.lastBeat, g.lastPlayerBeat = closed, closed
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
		k, off := a.cryptNearest(g)
		if math.Abs(off) <= cryptTol(per) && k > g.lastPlayerBeat && k > g.lastBeat {
			g.lastPlayerBeat = k
			a.cryptAct(dx, dy)
		} else {
			a.cryptMiss()
		}
	}
	// Los golpes que cerraron: los enemigos actúan.
	for g.lastBeat < closed && !g.over {
		g.lastBeat++
		if g.moved && g.lastPlayerBeat < g.lastBeat {
			g.combo = 0 // un golpe sin moverse rompe la racha
		}
		g.pulseAt, g.beatN = a.t, g.beatN+1
		a.cryptEnemies(g.lastBeat)
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
	g.moved = true
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
func (a *App) cryptEnemies(k int) {
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
			a.cryptHurt(k, e)
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
func (a *App) cryptHurt(k int, e *crEnt) {
	g := a.crypt
	if k <= g.invulBeat {
		return
	}
	g.hp--
	g.combo = 0
	g.hurtAt, g.shakeAt, g.invulBeat = a.t, a.t, k+1
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
		".##...##..",
		"####.####.",
		"#*#######.",
		"#########.",
		".#######..",
		"..#####...",
		"...###....",
		"....#.....",
		"..........",
	},
}

// crSprites guarda los dibujos ya hechos, por dibujo y colores.
var crSprites = map[string]*ebiten.Image{}

// crSprite es el dibujo name con el color c (y su tono oscuro y su brillo).
func crSprite(name string, c, eye color.RGBA) *ebiten.Image {
	key := fmt.Sprintf("%s%v%v", name, c, eye)
	if img, ok := crSprites[key]; ok {
		return img
	}
	art := crArt[name]
	img := ebiten.NewImage(10, 10)
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
	rect(dst, 0, top, w, bottom-top, th.Shadow)
	// El temblor de un golpe.
	sx, sy := 0, 0
	if d := a.t - g.shakeAt; d >= 0 && d < 0.18 {
		k := 1 - d/0.18
		sx, sy = int(math.Round(2*k*math.Sin(a.t*90))), int(math.Round(2*k*math.Cos(a.t*77)))
	}
	ox := w/2 - int(math.Round(g.camX)) + sx
	oy := (top+bottom)/2 - int(math.Round(g.camY)) + sy
	pulse := math.Max(0, 1-(a.t-g.pulseAt)/0.22)
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
			case crWall:
				// Un bloque de piedra: cara, borde de arriba claro y sombra.
				c := mix(th.Bg2, th.Text, 0.16)
				rect(clip, px, py, crTile, crTile, scaleRGB(c, light))
				rect(clip, px, py, crTile, 2, scaleRGB(mix(th.Bg2, th.Text, 0.35), light))
				rect(clip, px, py+crTile-2, crTile, 2, scaleRGB(mix(c, th.Shadow, 0.5), light))
				if (x+y)%3 == 0 {
					rect(clip, px+3, py+5, 4, 1, scaleRGB(mix(c, th.Shadow, 0.35), light))
				}
			case crFloor:
				// El suelo de discoteca: las baldosas se encienden por
				// turnos en cada golpe, con más color en FEVER.
				c := mix(th.Bg2, th.Text, 0.05)
				if (x+y+g.beatN)%2 == 0 {
					lc := lit1
					if (x/2+y/2+g.beatN)%2 == 0 {
						lc = lit2
					}
					amt := 0.16 + 0.34*pulse
					if fever {
						amt += 0.15
					}
					c = mix(c, lc, amt)
				}
				rect(clip, px+1, py+1, crTile-1, crTile-1, scaleRGB(c, light))
			}
		}
	}
	// La escalera.
	if g.seen[g.stairY][g.stairX] {
		px, py := ox+g.stairX*crTile, oy+g.stairY*crTile
		rect(clip, px+1, py+1, crTile-1, crTile-1, th.Shadow)
		for i := 0; i < 4; i++ {
			rect(clip, px+2+i*2, py+3+i*2, crTile-3-i*2, 1, mix(th.Text, th.Bg, float64(i)*0.2))
		}
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
	// La barra de ritmo.
	by := bottom + 4
	rect(dst, 0, bottom, w, 22, th.Bg)
	rect(dst, 0, bottom, w, 1, withAlpha(th.Accent, 0x50))
	cx := w / 2
	per, anchor := a.beat.period(), a.beat.anchor
	if per == 0 {
		per, anchor = 60.0/crFallbackBPM, g.fallback
	}
	now := a.beatClock()
	span := float64(min(w/2-20, 150))
	for k := 0; k < 4; k++ {
		next := anchor + (math.Floor((now-anchor)/per)+float64(k)+1)*per
		d := (next - now) / (per * 4) // 0 en el golpe, 1 a cuatro golpes
		if d < 0 || d > 1 || g.frozen {
			continue
		}
		off := int(d * span)
		c := withAlpha(th.Accent, uint8(255*(1-d*0.7)))
		rect(dst, cx-off-2, by+3, 2, 9, c)
		rect(dst, cx+off, by+3, 2, 9, c)
	}
	pulse := math.Max(0, 1-(a.t-g.pulseAt)/0.2)
	hc := th.Accent2
	if a.t-g.missAt < 0.25 {
		hc = th.Dim
	}
	op := &ebiten.DrawImageOptions{}
	s := 1 + 0.35*pulse
	op.GeoM.Translate(-5, -5)
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(float64(cx), float64(by+7))
	dst.DrawImage(crSprite("heart", hc, th.Text), op)
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
