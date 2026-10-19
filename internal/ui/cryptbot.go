package ui

// Un jugador automático de Cripta, solo para las capturas y la animación de la
// web (-shots, -frames): en cada golpe da un paso hacia lo más cercano que
// valga la pena (un enemigo, una moneda o un corazón; si no queda nada, la
// escalera), buscando el camino por las baldosas.

// botDir es el paso hacia el objetivo más cercano (0, 0 si no hay camino).
func (g *crypt) botDir() (int, int) {
	type pt struct{ x, y int }
	var prev [crH][crW]pt
	var seen [crH][crW]bool
	q := []pt{{g.px, g.py}}
	seen[g.py][g.px] = true
	var goal *pt
	for len(q) > 0 && goal == nil {
		p := q[0]
		q = q[1:]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := pt{p.x + d[0], p.y + d[1]}
			if !g.walkable(n.x, n.y) || seen[n.y][n.x] {
				continue
			}
			seen[n.y][n.x], prev[n.y][n.x] = true, p
			if e := g.at(n.x, n.y); e != nil || (n.x == g.stairX && n.y == g.stairY && g.cleared()) {
				goal = &n
				break
			}
			q = append(q, n)
		}
	}
	if goal == nil {
		return 0, 0
	}
	p := *goal
	for prev[p.y][p.x] != (pt{g.px, g.py}) {
		p = prev[p.y][p.x]
	}
	return p.x - g.px, p.y - g.py
}

// cleared dice si ya no quedan enemigos ni monedas (el bot baja entonces).
func (g *crypt) cleared() bool {
	for _, e := range g.ents {
		if !e.dead && e.kind != crHeart {
			return false
		}
	}
	return true
}

// botStepCrypt juega un paso de 60 Hz: justo tras cada golpe, el paso del bot;
// después, el resto del juego como siempre (enemigos, cámara).
func (a *App) botStepCrypt() {
	g := a.crypt
	if g.over {
		a.crypt = a.newCrypt()
		return
	}
	k, off := a.cryptNearest(g)
	if off >= 0 && off < 0.04 && k > g.lastPlayerBeat && k > g.lastBeat {
		if dx, dy := g.botDir(); dx != 0 || dy != 0 {
			g.lastPlayerBeat = k
			a.cryptAct(dx, dy)
		}
	}
	a.updateCrypt()
}
