package ui

import (
	"testing"
)

// TestRunnerReachable comprueba que un salto entero en la corchea de antes
// pasa por encima de cada bafle y del vinilo bajo, coge la nota de encima, y
// que agachado se pasa bajo el vinilo de media altura (y de pie no).
func TestRunnerReachable(t *testing.T) {
	const per = 0.5
	for _, beatPx := range []float64{36, 56} {
		speed := beatPx / per
		at := 10.0
		hits := func(th ruThing, jumpAt float64, duck bool) (crash, note bool) {
			for now := at - per; now < at+per; now += 1.0 / 240 {
				y := 0.0
				if u := (now - jumpAt) / per; jumpAt > 0 && u > 0 && u < 1 {
					y = 4 * ruJumpH * u * (1 - u)
				}
				hx0, hx1, hy1 := ruHeroBox(y, duck)
				x0, x1, y0, y1 := th.box(ruHeroX + (th.at-now)*speed)
				if x1 > hx0 && x0 < hx1 && y1 > y && y0 < hy1 {
					return th.kind != ruNote, th.kind == ruNote
				}
			}
			return false, false
		}
		jump := at - per/2
		for _, th := range []ruThing{
			{kind: ruSpeaker, at: at, n: 1}, {kind: ruSpeaker, at: at, n: 2},
			{kind: ruSpeaker, at: at, big: true}, {kind: ruVinyl, at: at, alt: 0},
		} {
			if c, _ := hits(th, jump, false); c {
				t.Errorf("beatPx %v: a full jump hits %+v", beatPx, th)
			}
		}
		if _, n := hits(ruThing{kind: ruNote, at: at, h: ruJumpH + 5}, jump, false); !n {
			t.Errorf("beatPx %v: a full jump misses the note above", beatPx)
		}
		if _, n := hits(ruThing{kind: ruNote, at: at, h: 4}, 0, false); !n {
			t.Errorf("beatPx %v: running misses the ground note", beatPx)
		}
		mid := ruThing{kind: ruVinyl, at: at, alt: 1}
		if c, _ := hits(mid, 0, true); c {
			t.Errorf("beatPx %v: ducking hits the middle vinyl", beatPx)
		}
		if c, _ := hits(mid, 0, false); !c {
			t.Errorf("beatPx %v: standing passes the middle vinyl", beatPx)
		}
	}
}
