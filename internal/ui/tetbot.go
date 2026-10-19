package ui

// Un jugador automático de Bloques, solo para el GIF de la web (-frames): elige
// dónde va cada pieza mirando todas las posiciones (como los bots clásicos:
// líneas completas, sin huecos, la pila baja y pareja), la lleva ahí girando y
// moviendo como lo haría el mando, y la suelta en el golpe de la música.

// tetPlan es dónde va la pieza: el giro y la columna.
type tetPlan struct{ rot, x int }

// botPlan busca la mejor posición para la pieza actual.
func (g *tetris) botPlan() tetPlan {
	best, bestScore := tetPlan{g.rot, g.px}, -1e9
	for r := 0; r < 4; r++ {
		for x := -2; x < tetW; x++ {
			if !g.fits(g.piece, r, x, g.py) {
				continue
			}
			t := *g // el tablero es un arreglo: se copia entero
			t.rot, t.px = r, x
			t.py = t.ghostY()
			for _, c := range tetShapes[t.piece][t.rot] {
				t.board[t.py+c[1]][t.px+c[0]] = 1
			}
			if s := t.botScore(); s > bestScore {
				best, bestScore = tetPlan{r, x}, s
			}
		}
	}
	return best
}

// botScore puntúa un tablero: premia las líneas (más si son cuatro) y castiga
// los huecos tapados, la altura y los escalones entre columnas.
func (g *tetris) botScore() float64 {
	lines := 0
	for y := 0; y < tetRows; y++ {
		full := true
		for x := 0; x < tetW; x++ {
			full = full && g.board[y][x] != 0
		}
		if full {
			lines++
		}
	}
	var heights [tetW]int
	holes := 0
	for x := 0; x < tetW; x++ {
		top := tetRows
		for y := 0; y < tetRows; y++ {
			if g.board[y][x] != 0 {
				if top == tetRows {
					top = y
				}
			} else if top < y {
				holes++
			}
		}
		heights[x] = tetRows - top
	}
	agg, bump := 0, 0
	for x := 0; x < tetW; x++ {
		agg += heights[x]
		if x > 0 {
			bump += abs(heights[x] - heights[x-1])
		}
	}
	bonus := []float64{0, 1, 3, 5, 12}[lines]
	return 8*bonus - 3.5*float64(holes) - 0.5*float64(agg) - 0.35*float64(bump)
}

// botStep juega un paso de 60 Hz: un giro o un paso de columna cada pocos
// pasos, como alguien con el mando, y con la pieza en su sitio la suelta en el
// siguiente golpe (o tras un rato si no llega). Devuelve si soltó.
func (a *App) botStep(step int, plan *tetPlan, readyAt *float64) bool {
	g := a.tetris
	if g.over || len(g.clearing) > 0 {
		return false
	}
	if g.rot != plan.rot || g.px != plan.x {
		if step%4 == 0 {
			switch {
			case g.rot != plan.rot:
				g.rotate(1)
			case g.px < plan.x && g.move(1), g.px > plan.x && g.move(-1):
			default:
				plan.x = g.px // no llega (algo estorba): se suelta donde está
			}
		}
		return false
	}
	if *readyAt == 0 {
		*readyAt = a.t
	}
	if a.beatHit || a.t-*readyAt > 0.6 {
		a.hardDrop()
		*readyAt = 0
		return true
	}
	return false
}
