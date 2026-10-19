package ui

import "testing"

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
