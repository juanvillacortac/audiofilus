package ui

import "math"

// Los jugadores automáticos de Serpiente y Naves, solo para las capturas y
// las animaciones de la web (-shots, -frames). Sencillos: lo justo para que
// la partida se vea de verdad.

// botSnake gira hacia la moneda (o el corazón) por el lado libre: de las
// tres direcciones posibles, la que acerca sin chocar; si ninguna acerca,
// cualquiera libre.
func (a *App) botSnake() {
	g := a.snake
	if g == nil || g.over {
		if g != nil && g.over {
			a.snake = a.newSnake()
		}
		return
	}
	target := g.food
	if g.bonusUntil > 0 {
		target = g.bonus
	}
	head := g.body[0]
	best, bestD := g.next, math.MaxFloat64
	for _, d := range []snPt{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if d == (snPt{-g.dir.x, -g.dir.y}) {
			continue
		}
		n := snPt{head.x + d.x, head.y + d.y}
		if !g.free(n) {
			continue
		}
		dist := math.Abs(float64(n.x-target.x)) + math.Abs(float64(n.y-target.y))
		if dist < bestD {
			best, bestD = d, dist
		}
	}
	g.next = best
}

// botShooter se pone debajo del enemigo más bajo y se aparta de las balas que
// le llegan.
func (a *App) botShooter() {
	g := a.shooter
	if g == nil {
		return
	}
	if g.over {
		a.shooter = a.newShooter()
		return
	}
	tx := g.x
	low := -1.0
	for _, f := range g.foes {
		if f.y > low {
			low, tx = f.y, f.x
		}
	}
	for _, s := range g.foeShots {
		if math.Abs(s.x-g.x) < 10 && s.y < g.y && g.y-s.y < 40 {
			tx = g.x + 30*math.Copysign(1, g.x-s.x)
		}
	}
	switch {
	case tx < g.x-2:
		g.x -= 2
	case tx > g.x+2:
		g.x += 2
	}
}
