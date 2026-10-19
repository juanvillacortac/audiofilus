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

// botDuel ataca cuando el cursor pasa cerca del centro y, en el turno del
// enemigo, aparta el corazón de la nota más cercana (sin salirse de la caja).
func (a *App) botDuel() {
	g := a.duel
	if g == nil {
		return
	}
	if g.over {
		a.duel = a.newDuel()
		return
	}
	switch g.phase {
	case duAttack:
		if math.Abs(g.cursor-0.5) < 0.03 {
			a.duelStrike(g)
		}
	case duDodge:
		var nx, ny, nd float64 = 0, 0, 1e9
		for _, b := range g.bullets {
			if d := math.Hypot(b.x-g.hx, b.y-g.hy); d < nd {
				nx, ny, nd = b.x, b.y, d
			}
		}
		if nd < 22 {
			dx, dy := g.hx-nx, g.hy-ny
			l := math.Max(1, math.Hypot(dx, dy))
			g.hx += dx / l * duHeartSpeed
			g.hy += dy / l * duHeartSpeed
		} else {
			g.hx *= 0.97 // de vuelta al centro
			g.hy *= 0.97
		}
	}
}

// botRunner salta en la corchea de antes de cada bafle o vinilo bajo (salto
// entero, así coge la nota de encima) y se agacha bajo los de media altura.
func (a *App) botRunner() {
	g := a.runner
	if g == nil {
		return
	}
	if g.over {
		a.runner = a.newRunner()
		return
	}
	per, _ := a.runnerGrid(g)
	now := a.beatClock()
	g.duck, g.held = false, g.jumpAt != 0
	for _, it := range g.things {
		dt := it.at - now
		if it.done || dt < -per/2 || dt > per {
			continue
		}
		switch {
		case (it.kind == ruSpeaker || (it.kind == ruVinyl && it.alt == 0)) && dt <= per/2:
			a.runnerJump(g)
		case it.kind == ruVinyl && it.alt == 1 && dt < per*0.4 && g.jumpAt == 0:
			g.duck = true
		}
	}
}
