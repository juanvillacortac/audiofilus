package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestReelRadiiKeepTape checks that the cassette's tape is neither gained nor
// lost: it starts all on the left, ends all on the right and, in between,
// the total on both reels (the area) stays the same.
func TestReelRadiiKeepTape(t *testing.T) {
	area := func(r float64) float64 { return r*r - reelCore*reelCore }
	if l, r := reelRadii(0); l != 1 || math.Abs(r-reelCore) > 1e-9 {
		t.Errorf("start: got %v, %v; want 1, %v", l, r, reelCore)
	}
	if l, r := reelRadii(1); math.Abs(l-reelCore) > 1e-9 || r != 1 {
		t.Errorf("end: got %v, %v; want %v, 1", l, r, reelCore)
	}
	for _, p := range []float64{0.1, 0.25, 0.5, 0.9} {
		l, r := reelRadii(p)
		if got := area(l) + area(r); math.Abs(got-area(1)) > 1e-9 {
			t.Errorf("progress %v: tape area %v, want %v", p, got, area(1))
		}
	}
}

// TestReelsEmptyTurnsFaster checks that the reel with less tape turns faster
// (the tape runs at a fixed speed).
func TestReelsEmptyTurnsFaster(t *testing.T) {
	var c cassetteReels
	c.turn(0.1, 0.1)
	if c.right <= c.left {
		t.Errorf("near the start the right reel should turn faster: left %v, right %v", c.left, c.right)
	}
	c = cassetteReels{}
	c.turn(0.1, 0.9)
	if c.left <= c.right {
		t.Errorf("near the end the left reel should turn faster: left %v, right %v", c.left, c.right)
	}
}

// TestCoverColors4 checks that a cover's four most vivid colors come out one
// per hue and in order around the color wheel (so the cassette's gradient
// flows), and that a gray cover gets the chrome skin.
func TestCoverColors4(t *testing.T) {
	stripes := []color.RGBA{{255, 40, 40, 255}, {40, 255, 40, 255}, {40, 40, 255, 255}, {255, 220, 40, 255}}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for x := range 64 {
		for y := range 64 {
			img.SetRGBA(x, y, stripes[x/16])
		}
	}
	cols := coverColors4(img)
	var hues []float64
	for _, c := range cols {
		h, _, _ := hsv(c)
		hues = append(hues, h)
	}
	for _, s := range stripes {
		want, _, _ := hsv(s)
		found := false
		for _, h := range hues {
			if d := math.Abs(h - want); d < 0.03 || d > 0.97 {
				found = true
			}
		}
		if !found {
			t.Errorf("hue %.3f of %v missing from %v", want, s, hues)
		}
	}
	wraps := 0
	for i := 1; i < len(hues); i++ {
		if hues[i] < hues[i-1] {
			wraps++
		}
	}
	if wraps > 1 {
		t.Errorf("hues %v go back and forth around the wheel", hues)
	}

	gray := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range gray.Pix {
		gray.Pix[i] = 128
	}
	chrome := tapeSkinPalette("chrome", nil).shell
	for i, c := range coverColors4(gray) {
		if c != chrome[i].c {
			t.Errorf("gray cover: color %d is %v, want chrome's %v", i, c, chrome[i].c)
		}
	}
}

// TestTapeEdgeOutward checks the outline the specks of light travel on:
// each point lies on the body's edge and its normal points out of it.
func TestTapeEdgeOutward(t *testing.T) {
	const w = 1000
	body := tapeBody(tapeMap{0, w})
	for i := range 500 {
		u, v, nx, ny := tapeEdge.at(float64(i) / 500)
		x, y := float32(u*w), float32(v*w)
		if d := body.sd(x, y); d < -1.5 || d > 1.5 {
			t.Errorf("point %d (%.3f, %.3f) is %.2f px off the edge", i, u, v, d)
		}
		if d := body.sd(x+float32(nx)*4, y+float32(ny)*4); d <= 0 {
			t.Errorf("point %d (%.3f, %.3f): the normal (%.2f, %.2f) points inside", i, u, v, nx, ny)
		}
	}
}

// TestDustStaysOnEdge checks that the specks of light keep to the edge as
// they drift for two minutes, and that each one, at the end of its life,
// comes back somewhere else.
func TestDustStaysOnEdge(t *testing.T) {
	d := newTapeDust()
	reborn := 0
	for range 60 * 120 {
		before := d.specks[0].life
		d.step()
		if d.specks[0].life < before {
			reborn++
		}
		for i, s := range d.specks {
			if s.p < 0 || s.p >= 1 || math.Abs(s.off) > 0.012 || s.life > s.end {
				t.Fatalf("speck %d left its place: p %.3f, off %.4f, life %.2f of %.2f", i, s.p, s.off, s.life, s.end)
			}
		}
	}
	if reborn == 0 {
		t.Errorf("a speck never came back after its life ended")
	}
}

// TestFitHand checks how a long title is written by hand: tighter first,
// down to 78% of its width, then cut with an ellipsis; with no squeezing
// (the app's pixel fonts) it is only cut.
func TestFitHand(t *testing.T) {
	width := func(s string) float64 { return float64(utf8.RuneCountInString(s)) }
	if s, sq := fitHand("abcdefghij", 10, 0.78, width); s != "abcdefghij" || sq != 1 {
		t.Errorf("fits: got %q at %.2f", s, sq)
	}
	if s, sq := fitHand("abcdefghijkl", 10, 0.78, width); s != "abcdefghijkl" || math.Abs(sq-10.0/12) > 1e-9 {
		t.Errorf("a little long: got %q at %.2f, want it whole and tighter", s, sq)
	}
	s, sq := fitHand(strings.Repeat("x", 20), 10, 0.78, width)
	if sq != 0.78 || !strings.HasSuffix(s, "…") || width(s)*sq > 10 {
		t.Errorf("too long: got %q at %.2f", s, sq)
	}
	s, sq = fitHand(strings.Repeat("x", 20), 10, 1, width)
	if sq != 1 || !strings.HasSuffix(s, "…") || width(s) > 10 {
		t.Errorf("too long, no squeezing: got %q at %.2f", s, sq)
	}
}

// TestTapeSettings checks the cassette's section in Settings: with the
// cassette off only its switch shows; its choices cycle and land in the
// config; the glow's animation hides with no glow, the specks' size with
// no specks.
func TestTapeSettings(t *testing.T) {
	a := &App{offline: true}
	shown := func() map[int]bool {
		on := map[int]bool{}
		for _, s := range a.sections() {
			if s.title == "sec_cassette" {
				for _, tile := range s.tiles {
					on[tile.id] = true
				}
			}
		}
		return on
	}
	if got := shown(); len(got) != 1 || !got[setCassette] {
		t.Fatalf("cassette off: tiles %v, want only its switch", got)
	}
	a.changeSetting(setCassette, 1)
	if got := shown(); !got[setTapeSkin] || !got[setTapeAnim] || !got[setTapeDust] {
		t.Errorf("cassette on: tiles %v, want its look", got)
	}
	a.changeSetting(setTapeSkin, 1)
	a.changeSetting(setTapeHand, 1)
	a.changeSetting(setTapeGlow, -1)
	if tp := a.cfg.UI.Tape; tp.Skin != "black" || tp.HandSize != 120 || tp.Glow != "off" {
		t.Errorf("after cycling: %+v, want the black skin, 120%% and no glow", tp)
	}
	if got := shown(); got[setTapeAnim] || got[setTapeDust] {
		t.Errorf("no glow: tiles %v, want no animation nor specks", got)
	}
}

// TestTapeStringsFilled checks that no text of the cassette is empty in any
// language: its table has a slot per language, and one left out would show
// nothing.
func TestTapeStringsFilled(t *testing.T) {
	for l, m := range strs {
		for k, v := range m {
			if (strings.HasPrefix(k, "tape_") || strings.HasPrefix(k, "set_tape_") || k == "sec_cassette") && v == "" {
				t.Errorf("%q is empty in %s", k, l)
			}
		}
	}
}

// TestTapeShellPainted checks the body painted on the CPU: solid in the
// middle, clear outside its rounded corner, and its light along the edge.
func TestTapeShellPainted(t *testing.T) {
	const w, p = 192, 20
	m := tapeMap{p, w}
	cw, ch := w+2*p, int(math.Round(tapeH*w))+2*p
	shell, light, leaks := newTapeCanvas(cw, ch), newTapeCanvas(cw, ch), newTapeCanvas(cw, ch)
	paintShell(shell, light, leaks, tapeInks{m, tapeNeon}, nil)
	alpha := func(c *tapeCanvas, u, v float32) float32 {
		return c.px[(int(m.y(v))*c.w+int(m.x(u)))*4+3]
	}
	if a := alpha(shell, 0.5, 0.3); a < 0.99 {
		t.Errorf("the label's middle has alpha %.2f, want solid", a)
	}
	if a := alpha(shell, 0.002, 0.002); a > 0.01 {
		t.Errorf("outside the rounded corner has alpha %.2f, want clear", a)
	}
	if a := alpha(light, 0, 0.3); a == 0 {
		t.Errorf("no light on the left edge")
	}
}
