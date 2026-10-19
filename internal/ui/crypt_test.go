package ui

import (
	"math/rand/v2"
	"testing"
)

// TestCryptStepsAfterRegrid simula una partida a 120 BPM pulsando justo en
// cada golpe del medidor, y a la mitad el medidor se recoloca (otra fase y la
// numeración desde cero): los pasos deben volver a valer enseguida, no quedar
// en MISS para siempre.
func TestCryptStepsAfterRegrid(t *testing.T) {
	a := &App{offline: true, t: 10}
	a.beat.per, a.beat.anchor = 0.5, 0
	g := &crypt{}
	a.syncCryptGrid(g)
	g.lastResolvedT = a.cryptClosed(g)
	g.lastPlayerT = g.lastResolvedT
	hits, misses, missesLate := 0, 0, 0
	nextPress := 10.5
	for f := 0; f < 60*20; f++ {
		a.t = 10 + float64(f)/60
		if a.t >= 20 && a.beat.anchor == 0 {
			a.beat.anchor = 20.1 // se recoloca: 0,1 s de desfase
			nextPress = 20.1
		}
		a.syncCryptGrid(g)
		if a.t >= nextPress {
			if beat, ok := a.cryptCanStep(g); ok {
				g.lastPlayerT = beat
				hits++
			} else {
				misses++
				if a.t > 23 {
					missesLate++
				}
			}
			nextPress += 0.5
		}
		if closed := a.cryptClosed(g); closed > g.lastResolvedT+g.per/2 {
			g.lastResolvedT = closed
		}
	}
	if missesLate > 0 {
		t.Errorf("%d misses after the grid settled (hits %d, misses %d)", missesLate, hits, misses)
	}
	if hits < 30 {
		t.Errorf("only %d hits", hits)
	}
}

// TestCryptSecretRoom comprueba que cada cinco pisos hay una pared agrietada
// con una sala escondida detrás (en casi todas las mazmorras cabe).
func TestCryptSecretRoom(t *testing.T) {
	found := 0
	for seed := uint64(1); seed <= 20; seed++ {
		g := &crypt{depth: 5}
		g.generate(rand.New(rand.NewPCG(seed, seed)))
		for y := range g.tiles {
			for x := range g.tiles[y] {
				if g.tiles[y][x] == crSecret {
					found++
				}
			}
		}
	}
	if found < 15 {
		t.Errorf("secret wall in only %d of 20 dungeons", found)
	}
}
