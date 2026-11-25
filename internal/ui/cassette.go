package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// The player's cassette (Settings → Cassette): instead of the cover with the
// vinyl peeking out, a neon cassette with the cover as its label, or with
// the song written on it by hand when there is no cover. The reels turn
// while playing and the tape moves from the left reel to the right one as
// the song goes on, like a real tape: the tape runs at a fixed speed, so the
// reel with less tape turns faster. Under it, its reflection on a glossy
// floor. Audiobooks with the cassette off keep their usual one (bookart.go).
//
// What never moves is painted once per size and look on the CPU (tapeCanvas:
// shapes as signed distances, anti-aliased, with gradients and a blurred
// glow); each frame only puts those layers together with the tape (tapeSrc),
// the reels, the specks of light and the label, then dithers it all like the
// covers and adds the reflection (tapeMixSrc).

// The cassette's parts, in fractions of the body's width (u across, v
// down), measured on the reference picture; the body is 1 × tapeH.
const (
	tapeH       = 0.603 // the body's height
	tapeCorner  = 0.044 // its corners' radius
	tapeEar     = 0.016 // the lower half is this much wider on each side,
	tapeEarV    = 0.395 // from here down, after a 45° step
	tapeScrewR  = 0.022
	tapeChamfer = 0.026 // the label's cut corners
	tapeReelV   = 0.2937
	tapeRingR   = 0.0754
	tapeHubR    = 0.062
	tapeHoleR   = 0.045
	tapeViewR   = 0.012
	tapeFull    = 0.25 // the radius of a full roll of tape
	tapeLedge   = 0.452
	tapeFloor   = 0.13 // the reflection's height
)

var (
	tapeScrews = [4][2]float32{{0.0437, 0.0397}, {0.952, 0.0397}, {0.0437, 0.563}, {0.952, 0.563}}
	tapeBar    = [4]float32{0.234, 0.028, 0.766, 0.06}
	tapeSlots  = [2][4]float32{{0.103, 0.016, 0.167, 0.04}, {0.829, 0.016, 0.893, 0.04}}
	tapeLabel  = [4]float32{0.0516, 0.0675, 0.944, 0.44}
	tapeLines  = [2]float32{0.128, 0.178}
	tapeLinesU = [2]float32{0.2, 0.795} // centered on the label
	tapePill   = [4]float32{0.1825, 0.2024, 0.8056, 0.385}
	tapeReelU  = [2]float32{0.2738, 0.7143}
	tapeView   = [4]float32{0.3532, 0.2222, 0.631, 0.3611}
	// The trapezoid at the bottom, narrower at the top: its top and bottom,
	// and where each of them starts and ends.
	tapeTrap  = struct{ top, bot, tl, tr, bl, br float32 }{0.448, 0.6, 0.1786, 0.8135, 0.1349, 0.8611}
	tapeHoles = [4][3]float32{{0.25, 0.567, 0.022}, {0.3452, 0.5437, 0.019}, {0.6429, 0.5437, 0.019}, {0.742, 0.567, 0.022}}
	tapeBlock = [4]float32{0.4246, 0.5159, 0.5675, 0.603}
	tapeSlot  = [4]float32{0.4706, 0.4722, 0.5214, 0.5016}
	// The light leaking out at the edges (u, v, radius): the static glow.
	tapeLeaks = [9][3]float32{{0.55, 0, 0.08}, {0.4, 0.005, 0.05}, {0.97, 0.12, 0.05}, {1, 0.33, 0.045}, {0, 0.3, 0.05}, {0.03, 0.5, 0.04}, {0.22, tapeH, 0.06}, {0.62, tapeH, 0.05}, {0.9, tapeH - 0.01, 0.05}}
)

// cassetteRatio is the body's height over its width (screens.go centers it
// in the cover's place).
const cassetteRatio = tapeH

// reelCore is the radius of an empty reel's core, as a fraction of a full
// reel's.
const reelCore = 0.3

// cassetteReels holds each reel's rotation: each turns at its own speed,
// depending on how much tape it holds, so the vinyl's angle is not enough.
type cassetteReels struct {
	left, right float64
}

// reelRadii returns the radius of the tape wound on each reel (1 is full,
// reelCore empty) after progress (0..1) has played. The tape is an area:
// what moves from one side to the other goes with the square of the radius.
func reelRadii(progress float64) (l, r float64) {
	p := math.Max(0, math.Min(1, progress))
	c := reelCore * reelCore
	return math.Sqrt(c + (1-c)*(1-p)), math.Sqrt(c + (1-c)*p)
}

// turn rotates the reels by what the vinyl turned this frame (d radians),
// each inversely to its radius: the one with less tape turns faster.
func (c *cassetteReels) turn(d, progress float64) {
	l, r := reelRadii(progress)
	c.left += d * cassetteSpin * reelCore / l
	c.right += d * cassetteSpin * reelCore / r
}

// rgbHex is the opaque color 0xRRGGBB.
func rgbHex(v uint32) color.RGBA {
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

var (
	tapeWhite = color.RGBA{0xff, 0xff, 0xff, 0xff}
	tapeBlack = color.RGBA{0, 0, 0, 0xff}
)

// tapeStop is a color at a point t (0..1) of a gradient.
type tapeStop struct {
	t float32
	c color.RGBA
}

// gradAt is the gradient's color at t (0..1).
func gradAt(stops []tapeStop, t float32) color.RGBA {
	t = max(0, min(1, t))
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].t {
			a, b := stops[i-1], stops[i]
			return mix(a.c, b.c, float64((t-a.t)/max(b.t-a.t, 1e-6)))
		}
	}
	return stops[len(stops)-1].c
}

// tapePalette is a skin's colors: the gradient across the shell (its edges
// and its light), the label's print, a glow on the print (none if spot is
// transparent) and how strong the glow is. A solid skin has an opaque body,
// from solid[0] at the top to solid[1] at the bottom.
type tapePalette struct {
	shell, label []tapeStop
	spot         color.RGBA
	glow         float32
	solid        *[2]color.RGBA
}

// tapeNeon is the reference picture's neon, with finer gradients than four
// colors give.
var tapeNeon = tapePalette{
	shell: []tapeStop{{0, rgbHex(0x4f3bff)}, {0.22, rgbHex(0x8f3dff)}, {0.42, rgbHex(0xff3fb0)}, {0.6, rgbHex(0xff7a2a)}, {0.82, rgbHex(0xffb61f)}, {1, rgbHex(0xff8c14)}},
	label: []tapeStop{{0, rgbHex(0x9440ff)}, {0.17, rgbHex(0x6f5bff)}, {0.25, rgbHex(0x63a2ff)}, {0.33, rgbHex(0xa65dff)}, {0.43, rgbHex(0xff7fae)}, {0.52, rgbHex(0xffb347)}, {0.8, rgbHex(0xffc42a)}, {1, rgbHex(0xffa01e)}},
	spot:  rgbHex(0xff3fd0),
	glow:  1,
}

// tapeSkins are the skins in Settings order (config.TapeUI.Skin), with
// their four colors from left to right: "" is the neon (tapeNeon), "black"
// solid graphite plastic and "cover" the cover's own colors.
var tapeSkins = []struct {
	id   string
	cols [4]uint32
}{
	{"", [4]uint32{0x5b3cff, 0xff3fb0, 0xff7a2a, 0xffb61f}},
	{"black", [4]uint32{0x4d5058, 0x5b5f68, 0x5b5f68, 0x4d5058}},
	{"ocean", [4]uint32{0x19f5ff, 0x2f7bff, 0x6a4dff, 0xc04dff}},
	{"lava", [4]uint32{0xff1744, 0xff5722, 0xff9100, 0xffd600}},
	{"toxic", [4]uint32{0xc6ff00, 0x39ff14, 0x00e676, 0x00bfa5}},
	{"cyber", [4]uint32{0x00f0ff, 0x7a5cff, 0xff2a6d, 0xfcee0a}},
	{"vapor", [4]uint32{0x01cdfe, 0xb967ff, 0xff71ce, 0xfffb96}},
	{"chrome", [4]uint32{0x9fb4ff, 0xe8ecff, 0xb9bfd6, 0xffffff}},
	{"cover", [4]uint32{}},
}

// tapePaletteFrom turns four colors into a palette: the shell's gradient
// and, lighter, the print's.
func tapePaletteFrom(c [4]color.RGBA) tapePalette {
	t := [4]float32{0, 0.35, 0.65, 1}
	p := tapePalette{spot: mix(c[1], tapeWhite, 0.2), glow: 1}
	for i := range c {
		p.shell = append(p.shell, tapeStop{t[i], c[i]})
		p.label = append(p.label, tapeStop{t[i], mix(c[i], tapeWhite, 0.3)})
	}
	return p
}

// tapeSkinPalette is skin's palette; cover is the album's four colors for
// the "cover" skin (nil while there are none: then the neon).
func tapeSkinPalette(skin string, cover *[4]color.RGBA) tapePalette {
	switch skin {
	case "":
		return tapeNeon
	case "cover":
		if cover == nil {
			return tapeNeon
		}
		return tapePaletteFrom(*cover)
	case "black":
		// The app's old graphite, a paper label and only a little glow.
		p := tapePaletteFrom([4]color.RGBA{rgbHex(0x4d5058), rgbHex(0x5b5f68), rgbHex(0x5b5f68), rgbHex(0x4d5058)})
		p.solid = &[2]color.RGBA{rgbHex(0x3b3e44), rgbHex(0x0e0f11)}
		p.label = []tapeStop{{0, rgbHex(0xf2f2f5)}, {0.35, rgbHex(0xe7e8ec)}, {0.65, rgbHex(0xdddee3)}, {1, rgbHex(0xd3d5db)}}
		p.spot, p.glow = color.RGBA{}, 0.3
		return p
	}
	for _, s := range tapeSkins {
		if s.id == skin {
			var c [4]color.RGBA
			for i, v := range s.cols {
				c[i] = rgbHex(v)
			}
			return tapePaletteFrom(c)
		}
	}
	return tapeNeon
}

// tapePapers are the label's colors behind the handwriting, in Settings
// order (config.TapeUI.Paper); "" is the skin's print.
var tapePapers = []struct {
	id string
	c  uint32
}{{"", 0}, {"white", 0xffffff}, {"yellow", 0xffc800}, {"navy", 0x0300a3}, {"pink", 0xd100a4}, {"purple", 0x8d3db9}, {"green", 0x85d228}, {"orange", 0xd28b28}, {"red", 0xdd2222}}

// The cassette's defaults where the config leaves them at zero: the size of
// the handwriting and of the specks, in percent. (Its glow breathes by
// default: config.TapeUI.GlowAnim "" is "breathe", "none" is still.)
const (
	tapeHandDefault = 100
	tapeDustDefault = 200
)

// tapeAnim is the glow's animation, with "" (the default) as "breathe".
func tapeAnim(id string) string { return orDefault(id, "breathe") }

// tapePaper is the paper's color, or nil for the skin's print.
func tapePaper(id string) *color.RGBA {
	for _, p := range tapePapers {
		if p.id == id && id != "" {
			c := rgbHex(p.c)
			return &c
		}
	}
	return nil
}

// tapeDarkPaper says whether c is dark enough to need a light ink.
func tapeDarkPaper(c *color.RGBA) bool {
	return c != nil && (0.2126*float64(c.R)+0.7152*float64(c.G)+0.0722*float64(c.B))/255 < 0.45
}

// coverColors4 picks a cover's four most vivid colors for the "cover" skin,
// as accentColors picks two: the heaviest hues (vivid, bright pixels weigh
// most) at least 45° apart, brightened for the neon and put in order around
// the color wheel, so the gradient across the cassette flows. A gray cover
// gets the chrome.
func coverColors4(img image.Image) [4]color.RGBA {
	small := image.NewRGBA(image.Rect(0, 0, 48, 48))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	type bin struct {
		i          int
		w, h, s, v float64
	}
	bins := make([]bin, 24)
	for i := range bins {
		bins[i].i = i
	}
	for i := 0; i < len(small.Pix); i += 4 {
		h, s, v := hsv(color.RGBA{small.Pix[i], small.Pix[i+1], small.Pix[i+2], 0xff})
		if s < 0.3 || v < 0.3 {
			continue
		}
		w, b := s*s*v, &bins[int(h*24)%24]
		b.w += w
		b.h += h * w
		b.s += s * w
		b.v += v * w
	}
	found := slices.DeleteFunc(slices.Clone(bins), func(b bin) bool { return b.w == 0 })
	slices.SortStableFunc(found, func(a, b bin) int {
		if a.w > b.w {
			return -1
		}
		if a.w < b.w {
			return 1
		}
		return 0
	})
	var picked []bin
	for _, gap := range []int{3, 2, 1} {
		for _, b := range found {
			far := len(picked) < 4
			for _, p := range picked {
				if d := abs(p.i - b.i); min(d, 24-d) < gap {
					far = false
				}
			}
			if far {
				picked = append(picked, b)
			}
		}
	}
	if len(picked) == 0 {
		var c [4]color.RGBA
		for i, v := range tapeSkins[7].cols { // chrome
			c[i] = rgbHex(v)
		}
		return c
	}
	type hsvc struct{ h, s, v float64 }
	cols := make([]hsvc, 0, 4)
	for _, b := range picked {
		cols = append(cols, hsvc{b.h / b.w, max(0.55, b.s/b.w), max(0.9, b.v/b.w)})
	}
	for len(cols) < 4 {
		last := cols[len(cols)-1]
		cols = append(cols, hsvc{math.Mod(last.h+28.0/360, 1), last.s, last.v})
	}
	slices.SortFunc(cols, func(a, b hsvc) int {
		if a.h < b.h {
			return -1
		}
		if a.h > b.h {
			return 1
		}
		return 0
	})
	start, widest := 0, -1.0
	for i := range cols {
		if gap := math.Mod(cols[(i+1)%4].h-cols[i].h+1, 1); gap > widest {
			start, widest = (i+1)%4, gap
		}
	}
	var out [4]color.RGBA
	for i := range out {
		c := cols[(start+i)%4]
		out[i] = fromHSV(c.h, c.s, c.v)
	}
	return out
}

// TapeColors are the cover's four colors for the cassette's "cover" skin;
// ok is false while the cover has not been read.
func (a *Album) TapeColors() (c [4]color.RGBA, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tapeCols, a.hasColor
}

// tapeCanvas is a layer painted on the CPU: premultiplied RGBA, 0..1.
type tapeCanvas struct {
	w, h int
	px   []float32
}

func newTapeCanvas(w, h int) *tapeCanvas {
	return &tapeCanvas{w, h, make([]float32, w*h*4)}
}

// form is a shape, as its signed distance in pixels from a point to its
// edge (negative inside), and the box it lies in (x0, y0, x1, y1).
type form struct {
	sd  func(x, y float32) float32
	box [4]float32
}

// ink is the straight color and the alpha laid at a point.
type ink func(x, y float32) (color.RGBA, float32)

// flat is one color everywhere.
func flat(c color.RGBA, a float32) ink {
	return func(float32, float32) (color.RGBA, float32) { return c, a }
}

func hypot32(x, y float32) float32 { return float32(math.Sqrt(float64(x*x + y*y))) }

// circle is a disk.
func circle(cx, cy, r float32) form {
	return form{func(x, y float32) float32 { return hypot32(x-cx, y-cy) - r }, [4]float32{cx - r, cy - r, cx + r, cy + r}}
}

// roundBox is a rectangle with its corners rounded to r.
func roundBox(x0, y0, x1, y1, r float32) form { return roundBox4(x0, y0, x1, y1, r, r, r, r) }

// roundBox4 is a rectangle with each corner rounded to its own radius: top
// left, top right, bottom right, bottom left.
func roundBox4(x0, y0, x1, y1, tl, tr, br, bl float32) form {
	cx, cy, hw, hh := (x0+x1)/2, (y0+y1)/2, (x1-x0)/2, (y1-y0)/2
	return form{func(x, y float32) float32 {
		r := tl
		switch {
		case x >= cx && y < cy:
			r = tr
		case x >= cx:
			r = br
		case y >= cy:
			r = bl
		}
		r = min(r, hw, hh)
		qx, qy := abs32(x-cx)-hw+r, abs32(y-cy)-hh+r
		return hypot32(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
	}, [4]float32{x0, y0, x1, y1}}
}

// polygon is a convex polygon, its corners clockwise on the screen.
func polygon(pts ...[2]float32) form {
	box := [4]float32{pts[0][0], pts[0][1], pts[0][0], pts[0][1]}
	type edge struct{ ax, ay, nx, ny float32 }
	edges := make([]edge, len(pts))
	for i, a := range pts {
		b := pts[(i+1)%len(pts)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := hypot32(dx, dy)
		edges[i] = edge{a[0], a[1], dy / l, -dx / l}
		box = [4]float32{min(box[0], a[0]), min(box[1], a[1]), max(box[2], a[0]), max(box[3], a[1])}
	}
	return form{func(x, y float32) float32 {
		d := float32(math.Inf(-1))
		for _, e := range edges {
			d = max(d, (x-e.ax)*e.nx+(y-e.ay)*e.ny)
		}
		return d
	}, box}
}

// segment is a line w pixels wide with round ends.
func segment(ax, ay, bx, by, w float32) form {
	dx, dy := bx-ax, by-ay
	l2 := max(dx*dx+dy*dy, 1e-6)
	return form{func(x, y float32) float32 {
		h := max(0, min(1, ((x-ax)*dx+(y-ay)*dy)/l2))
		return hypot32(x-ax-dx*h, y-ay-dy*h) - w/2
	}, [4]float32{min(ax, bx) - w, min(ay, by) - w, max(ax, bx) + w, max(ay, by) + w}}
}

// stroke is the outline of f, w pixels wide.
func (f form) stroke(w float32) form {
	sd := f.sd
	return form{func(x, y float32) float32 { return abs32(sd(x, y)) - w/2 }, [4]float32{f.box[0] - w, f.box[1] - w, f.box[2] + w, f.box[3] + w}}
}

// and is where f and g overlap.
func (f form) and(g form) form {
	a, b := f.sd, g.sd
	return form{func(x, y float32) float32 { return max(a(x, y), b(x, y)) }, f.box}
}

// or is f and g together.
func (f form) or(g form) form {
	a, b := f.sd, g.sd
	return form{func(x, y float32) float32 { return min(a(x, y), b(x, y)) },
		[4]float32{min(f.box[0], g.box[0]), min(f.box[1], g.box[1]), max(f.box[2], g.box[2]), max(f.box[3], g.box[3])}}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// paint lays in over f, anti-aliased over a pixel.
func (c *tapeCanvas) paint(f form, in ink) {
	for y := max(0, int(f.box[1])-2); y < min(c.h, int(f.box[3])+2); y++ {
		for x := max(0, int(f.box[0])-2); x < min(c.w, int(f.box[2])+2); x++ {
			fx, fy := float32(x)+0.5, float32(y)+0.5
			cov := 0.5 - f.sd(fx, fy)
			if cov <= 0 {
				continue
			}
			k, a := in(fx, fy)
			if a *= min(cov, 1); a <= 0 {
				continue
			}
			p := c.px[(y*c.w+x)*4:][:4]
			p[0] = float32(k.R)/255*a + p[0]*(1-a)
			p[1] = float32(k.G)/255*a + p[1]*(1-a)
			p[2] = float32(k.B)/255*a + p[2]*(1-a)
			p[3] = a + p[3]*(1-a)
		}
	}
}

// bloom blurs c (three box blurs, close to a gaussian, r pixels each) and
// adds it, times k, onto dst: the neon glow.
func (c *tapeCanvas) bloom(dst *tapeCanvas, r int, k float32) {
	a, b := slices.Clone(c.px), make([]float32, len(c.px))
	for range 3 {
		boxBlur(a, b, c.w, c.h, r, true)
		boxBlur(b, a, c.w, c.h, r, false)
	}
	for i, v := range a {
		dst.px[i] = min(1, dst.px[i]+v*k)
	}
}

// boxBlur averages each pixel of src with its r neighbors on each side,
// across or down, into dst; outside the layer counts as clear.
func boxBlur(src, dst []float32, w, h, r int, across bool) {
	lines, length, stride, step := h, w, w*4, 4
	if !across {
		lines, length, stride, step = w, h, 4, w*4
	}
	n := float32(2*r + 1)
	for l := range lines {
		base := l * stride
		for ch := range 4 {
			var s float32
			for i := 0; i <= r && i < length; i++ {
				s += src[base+i*step+ch]
			}
			for i := range length {
				dst[base+i*step+ch] = s / n
				if j := i + r + 1; j < length {
					s += src[base+j*step+ch]
				}
				if j := i - r; j >= 0 {
					s -= src[base+j*step+ch]
				}
			}
		}
	}
}

// image uploads the layer.
func (c *tapeCanvas) image() *ebiten.Image {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	for i, v := range c.px {
		img.Pix[i] = uint8(max(0, min(1, v))*255 + 0.5)
	}
	return ebiten.NewImageFromImage(img)
}

// tapeMap places the body's units on a layer, with p pixels of room around
// the body for the glow.
type tapeMap struct{ p, w float32 }

func (m tapeMap) x(u float32) float32 { return m.p + u*m.w }
func (m tapeMap) y(v float32) float32 { return m.p + v*m.w }
func (m tapeMap) d(d float32) float32 { return d * m.w }

// across is how far across the body pixel x is (0..1), for the gradients.
func (m tapeMap) across(x float32) float32 { return (x - m.p) / m.w }

// tapeBody is the body's shape: rounded corners, and the lower half a
// little wider, with a 45° step on each side.
func tapeBody(m tapeMap) form {
	R, e := m.d(tapeCorner), m.d(tapeEar)
	x0, x1, y0, y1, a := m.x(0), m.x(1), m.y(0), m.y(tapeH), m.y(tapeEarV)
	return roundBox(x0, y0, x1, y1, R).
		or(roundBox4(x0-e, a+e, x1+e, y1, 0, 0, R, R)).
		or(polygon([2]float32{x0, a}, [2]float32{x0, a + e}, [2]float32{x0 - e, a + e})).
		or(polygon([2]float32{x1, a}, [2]float32{x1 + e, a + e}, [2]float32{x1, a + e}))
}

// tapeLabelForm is the label's shape, with its corners cut at 45°.
func tapeLabelForm(m tapeMap) form {
	x0, y0, x1, y1, k := m.x(tapeLabel[0]), m.y(tapeLabel[1]), m.x(tapeLabel[2]), m.y(tapeLabel[3]), m.d(tapeChamfer)
	return polygon([2]float32{x0 + k, y0}, [2]float32{x1 - k, y0}, [2]float32{x1, y0 + k}, [2]float32{x1, y1 - k},
		[2]float32{x1 - k, y1}, [2]float32{x0 + k, y1}, [2]float32{x0, y1 - k}, [2]float32{x0, y0 + k})
}

// tapeInks are what the painters share: the map, the palette, and the
// shell's gradient mixed with white by k at alpha a.
type tapeInks struct {
	m   tapeMap
	pal tapePalette
}

func (t tapeInks) shell(k, a float32) ink {
	return func(x, _ float32) (color.RGBA, float32) {
		return mix(gradAt(t.pal.shell, t.m.across(x)), tapeWhite, float64(k)), a
	}
}

// paintShell paints what lies under the label's contents: the body, its
// edges, rail, ledge, trapezoid, holes and screws, and the label's paper
// (the skin's print or the paper color) with its two lines. On light it
// paints what glows, and on leaks the light leaking out at the edges.
func paintShell(c, light, leaks *tapeCanvas, t tapeInks, paper *color.RGBA) {
	m, pal := t.m, t.pal
	lw := max(1, m.d(0.006))
	body := tapeBody(m)
	y0, y1 := m.y(0), m.y(tapeH)

	// The shell: tinted plastic (or solid graphite), darker towards the
	// bottom, with a lit edge and a bevel just inside it.
	if pal.solid != nil {
		top, bot := pal.solid[0], pal.solid[1]
		c.paint(body, func(_, y float32) (color.RGBA, float32) { return mix(top, bot, float64((y-y0)/(y1-y0))), 1 })
	} else {
		c.paint(body, func(x, _ float32) (color.RGBA, float32) {
			return mix(rgbHex(0x080512), gradAt(pal.shell, m.across(x)), 0.24), 1
		})
	}
	c.paint(body, func(_, y float32) (color.RGBA, float32) {
		if f := (y - y0) / (y1 - y0); f < 0.55 {
			return tapeWhite, 0.06 * (1 - f/0.55)
		} else {
			return tapeBlack, 0.3 * (f - 0.55) / 0.45
		}
	})
	c.paint(body.stroke(lw), t.shell(0.35, 1))
	light.paint(body.stroke(lw*1.6), t.shell(0.2, 1))
	c.paint(roundBox(m.x(0.02), m.y(0.02), m.x(0.98), m.y(tapeH-0.02), m.d(0.03)).stroke(1), t.shell(0.1, 0.35))

	// The top rail and its two slots, the ledge where the lower half starts.
	for i, r := range [][4]float32{tapeBar, tapeSlots[0], tapeSlots[1]} {
		round := m.d(0.008)
		if i == 0 {
			round = m.d(0.01)
		}
		f := roundBox(m.x(r[0]), m.y(r[1]), m.x(r[2]), m.y(r[3]), round)
		c.paint(f, flat(tapeBlack, 0.35))
		c.paint(f.stroke(1), t.shell(0.3, 0.8))
	}
	ly := m.y(tapeLedge) + 0.5
	c.paint(segment(m.x(0.012), ly, m.x(tapeTrap.tl-0.015), ly, 1), t.shell(0.3, 0.7))
	c.paint(segment(m.x(tapeTrap.tr+0.015), ly, m.x(0.988), ly, 1), t.shell(0.3, 0.7))

	// Light leaking out at the edges of the plastic, and a hot streak on top.
	for _, l := range tapeLeaks {
		x, y, r := m.x(l[0]), m.y(l[1]), m.d(l[2])
		col := mix(gradAt(pal.shell, l[0]), tapeWhite, 0.4)
		leaks.paint(circle(x, y, r), func(px, py float32) (color.RGBA, float32) {
			return col, 0.55 * pal.glow * max(0, 1-hypot32(px-x, py-y)/r)
		})
	}
	light.paint(segment(m.x(0.56), y0+0.5, m.x(0.68), y0+0.5, 1.5), flat(color.RGBA{255, 240, 220, 255}, 0.95*pal.glow))

	// The trapezoid at the bottom, narrower at the top, with its double edge.
	tr := tapeTrap
	trap := func(k float32) form {
		return polygon([2]float32{m.x(tr.tl) + k, m.y(tr.top) + k}, [2]float32{m.x(tr.tr) - k, m.y(tr.top) + k},
			[2]float32{m.x(tr.br) - k, m.y(tr.bot)}, [2]float32{m.x(tr.bl) + k, m.y(tr.bot)})
	}
	if pal.solid != nil {
		top, bot := mix(pal.solid[0], pal.solid[1], 0.45), mix(pal.solid[0], pal.solid[1], 0.8)
		t0, t1 := m.y(tr.top), m.y(tr.bot)
		c.paint(trap(0), func(_, y float32) (color.RGBA, float32) { return mix(top, bot, float64((y-t0)/(t1-t0))), 1 })
	} else {
		c.paint(trap(0), func(x, _ float32) (color.RGBA, float32) {
			return mix(rgbHex(0x0a0616), gradAt(pal.shell, m.across(x)), 0.32), 1
		})
	}
	c.paint(trap(0).stroke(lw), t.shell(0.35, 1))
	c.paint(trap(m.d(0.012)).stroke(1), t.shell(0.2, 0.55))
	light.paint(trap(0).stroke(lw*1.4), t.shell(0.2, 1))
	block := roundBox(m.x(tapeBlock[0]), m.y(tapeBlock[1]), m.x(tapeBlock[2]), m.y(tapeBlock[3]), 0)
	c.paint(block, flat(tapeBlack, 0.25))
	c.paint(block.stroke(1), t.shell(0.3, 0.85))
	c.paint(roundBox(m.x(tapeSlot[0]), m.y(tapeSlot[1]), m.x(tapeSlot[2]), m.y(tapeSlot[3]), m.d(0.008)).stroke(1), t.shell(0.3, 0.85))
	for _, h := range tapeHoles {
		hole := circle(m.x(h[0]), m.y(h[1]), m.d(h[2]))
		c.paint(hole, flat(rgbHex(0x05020b), 1))
		c.paint(hole.stroke(lw), flat(mix(gradAt(pal.shell, h[0]), tapeWhite, 0.55), 1))
		light.paint(hole.stroke(lw), flat(gradAt(pal.shell, h[0]), 1))
	}
	// Light glancing off its slopes.
	for _, s := range [][2]float32{{0.79, 0.745}, {0.81, 0.765}, {0.27, 0.23}} {
		c.paint(segment(m.x(s[0]), m.y(0.462), m.x(s[1]), m.y(0.53), max(1, m.d(0.004))), flat(tapeWhite, 0.35))
	}

	// The corner screws: silver, with a cross, and a ring of light.
	for _, s := range tapeScrews {
		x, y, r := m.x(s[0]), m.y(s[1]), m.d(tapeScrewR)
		c.paint(circle(x, y, r+1), flat(rgbHex(0x120a26), 1))
		hx, hy := x-r*0.35, y-r*0.4
		c.paint(circle(x, y, r), func(px, py float32) (color.RGBA, float32) {
			return mix(tapeWhite, rgbHex(0x8c8cb4), float64(min(1, hypot32(px-hx, py-hy)/(r*1.3)))), 1
		})
		th, l := float32(max(1, int(math.Round(float64(r*0.3))))|1), float32(max(3, int(math.Round(float64(r*1.15))))|1)
		cx, cy := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
		c.paint(roundBox(cx-(l-1)/2, cy-(th-1)/2, cx+(l+1)/2, cy+(th+1)/2, 0), flat(rgbHex(0x1d1442), 1))
		c.paint(roundBox(cx-(th-1)/2, cy-(l-1)/2, cx+(th+1)/2, cy+(l+1)/2, 0), flat(rgbHex(0x1d1442), 1))
		light.paint(circle(x, y, r+1.5).stroke(1.2), flat(mix(gradAt(pal.shell, s[0]), tapeWhite, 0.3), 1))
	}

	// The label's paper: the skin's print, with a glow at the bottom left and
	// light from the top, or one color; and its two lines to write on.
	label := tapeLabelForm(m)
	lx0, lx1, ly0, ly1 := m.x(tapeLabel[0]), m.x(tapeLabel[2]), m.y(tapeLabel[1]), m.y(tapeLabel[3])
	printAt := func(x, _ float32) (color.RGBA, float32) { return gradAt(pal.label, (x-lx0)/(lx1-lx0)), 1 }
	light.paint(label.stroke(lw*2), printAt)
	if paper != nil {
		c.paint(label, flat(*paper, 1))
	} else {
		c.paint(label, printAt)
		if pal.spot.A > 0 {
			sx, sy, sr := m.x(0.13), m.y(0.43), m.d(0.24)
			c.paint(label.and(circle(sx, sy, sr)), func(x, y float32) (color.RGBA, float32) {
				return pal.spot, 0.55 * max(0, 1-hypot32(x-sx, y-sy)/sr)
			})
		}
	}
	c.paint(label, func(_, y float32) (color.RGBA, float32) {
		if f := (y - ly0) / (ly1 - ly0); f < 0.6 {
			return tapeWhite, 0.12 * (1 - f/0.6)
		} else {
			return tapeBlack, 0.12 * (f - 0.6) / 0.4
		}
	})
	rule, ruleA := rgbHex(0x24157a), float32(1)
	if tapeDarkPaper(paper) {
		rule, ruleA = color.RGBA{242, 238, 255, 255}, 0.7
	}
	th := max(1, float32(math.Round(float64(m.d(0.0032)))))
	for _, v := range tapeLines {
		y := float32(math.Round(float64(m.y(v))))
		c.paint(roundBox(float32(math.Round(float64(m.x(tapeLinesU[0])))), y, float32(math.Round(float64(m.x(tapeLinesU[1])))), y+th, 0), flat(rule, ruleA))
	}
}

// paintWindow paints what lies over the label's contents: a soft sheen and
// the label's dark edge, the window (a pill with a lit edge, dark inside),
// the rings around the reels and the tape glass, dark behind the rolls.
func paintWindow(c, light *tapeCanvas, t tapeInks) {
	m, pal := t.m, t.pal
	lw := max(1, m.d(0.006))
	label := tapeLabelForm(m)
	c.paint(segment(m.x(0.68), m.y(tapeLabel[1])-m.d(0.03), m.x(0.6), m.y(tapeLabel[3])+m.d(0.03), m.d(0.06)).and(label), flat(tapeWhite, 0.08))
	c.paint(label.stroke(max(1, m.d(0.007))), flat(rgbHex(0x1c0e52), 1))

	p0, q0, p1, q1 := m.x(tapePill[0]), m.y(tapePill[1]), m.x(tapePill[2]), m.y(tapePill[3])
	pr, pw := (q1-q0)/2, max(1.5, m.d(0.008))
	pill := roundBox(p0, q0, p1, q1, pr)
	inside := []tapeStop{{0, rgbHex(0x170a3d)}, {0.5, rgbHex(0x1d0b30)}, {1, rgbHex(0x2c0f24)}}
	if pal.solid != nil {
		inside = []tapeStop{{0, rgbHex(0x121216)}, {0.5, rgbHex(0x18181d)}, {1, rgbHex(0x121216)}}
	}
	c.paint(pill, func(x, _ float32) (color.RGBA, float32) { return gradAt(inside, (x-p0)/(p1-p0)), 1 })
	c.paint(pill.stroke(pw), t.shell(0.45, 1))
	light.paint(pill.stroke(pw*1.5), t.shell(0.3, 1))
	in := pw + 0.5
	c.paint(roundBox(p0+in, q0+in, p1-in, q1-in, pr-in).stroke(1), flat(rgbHex(0x04020c), 0.9))
	for _, u := range tapeReelU {
		ring := circle(m.x(u), m.y(tapeReelV), m.d(tapeRingR)).stroke(lw)
		c.paint(ring, flat(rgbHex(0xcfc9ff), 1))
		light.paint(ring, flat(mix(gradAt(pal.shell, u), tapeWhite, 0.5), 1))
	}
	bg := rgbHex(0x120828)
	if pal.solid != nil {
		bg = rgbHex(0x0f0f12)
	}
	c.paint(roundBox(m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3]), m.d(tapeViewR)), flat(bg, 1))
}

// paintOver paints what lies over the tape: the glass's shading and
// reflections, and its frame.
func paintOver(c *tapeCanvas, t tapeInks) {
	m, pal := t.m, t.pal
	a0, b0, a1, b1, r := m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3]), m.d(tapeViewR)
	glass := roundBox(a0, b0, a1, b1, r)
	c.paint(glass, func(_, y float32) (color.RGBA, float32) {
		f := (y - b0) / (b1 - b0)
		return mix(tapeWhite, tapeBlack, float64(f)), 0.10 + 0.15*f
	})
	for _, s := range [][4]float32{{0.47, 0.4, 0.016, 0.2}, {0.505, 0.435, 0.005, 0.45}, {0.6, 0.54, 0.008, 0.18}} {
		c.paint(segment(m.x(s[0]), b0-2, m.x(s[1]), b1+2, max(1, m.d(s[2]))).and(glass), flat(tapeWhite, s[3]))
	}
	c.paint(roundBox(a0-0.5, b0-0.5, a1+0.5, b1+0.5, r+0.5).stroke(1), flat(rgbHex(0x05020f), 1))
	l, rr := mix(gradAt(pal.shell, tapeView[0]), tapeWhite, 0.4), mix(gradAt(pal.shell, tapeView[2]), tapeWhite, 0.4)
	c.paint(roundBox(a0-1.5, b0-1.5, a1+1.5, b1+1.5, r+1.5).stroke(1), func(x, _ float32) (color.RGBA, float32) {
		return mix(l, rr, float64((x-a0)/(a1-a0))), 0.7
	})
}

// paintReel paints a reel at angle 0, centered: a white disk with a ring,
// a dark center and six teeth.
func paintReel(hub, hole float32) *tapeCanvas {
	s := int(math.Ceil(float64(2*hub+4))) + 2
	c, o := newTapeCanvas(s, s), float32(s)/2
	c.paint(circle(o, o, hub+0.8), flat(rgbHex(0x1b1238), 1))
	hx, hy := o-hub*0.3, o-hub*0.35
	c.paint(circle(o, o, hub), func(x, y float32) (color.RGBA, float32) {
		return mix(tapeWhite, rgbHex(0xa9a6dc), float64(min(1, hypot32(x-hx, y-hy)/(hub*1.25)))), 1
	})
	c.paint(circle(o, o, hub*0.84).stroke(max(1, hub*0.07)), flat(color.RGBA{90, 84, 150, 255}, 0.7))
	c.paint(circle(o, o, hole), flat(rgbHex(0x0b0720), 1))
	for k := range 6 {
		sn, cs := math.Sincos(float64(k) * math.Pi / 3)
		at := func(x, y float32) [2]float32 {
			return [2]float32{o + x*float32(cs) - y*float32(sn), o + x*float32(sn) + y*float32(cs)}
		}
		a, b, w := hole*0.6, hole*1.06, hole*0.17
		c.paint(polygon(at(a, -w), at(b, -w), at(b, w), at(a, w)), flat(rgbHex(0xecebff), 1))
	}
	return c
}

// tapeDotSize is the size of the soft dot of light the specks and the
// orbiting light are drawn with.
const tapeDotSize = 32

// paintDot paints that dot: bright in the middle, fading out.
func paintDot() *tapeCanvas {
	const h = tapeDotSize / 2
	c := newTapeCanvas(tapeDotSize, tapeDotSize)
	c.paint(circle(h, h, h), func(x, y float32) (color.RGBA, float32) {
		if d := hypot32(x-h, y-h) / h; d < 0.3 {
			return tapeWhite, 1 - 0.55*d/0.3
		} else {
			return tapeWhite, max(0, 0.45*(1-(d-0.3)/0.7))
		}
	})
	return c
}

// tapeLayers are the cassette's still layers for one size and look.
type tapeLayers struct {
	key         string
	m           tapeMap
	cw, ch      int // the layers' size: the body and room for the glow around it
	floor, refl int // the floor line (the body's bottom) and the reflection's height
	pal         tapePalette
	dark        bool // light ink, on a dark paper
	shell       *ebiten.Image
	window      *ebiten.Image
	over        *ebiten.Image
	glow        *ebiten.Image // the edges' glow; nil without glow
	spots       *ebiten.Image // the static light leaks
	mask        *ebiten.Image // the label's shape, white
	reel        *ebiten.Image
	dot         *ebiten.Image
}

// newTapeLayers paints the layers of a cassette w pixels wide. The blurs
// make it the slow part: it only happens when the size or the look change.
func newTapeLayers(key string, w int, pal tapePalette, paper *color.RGBA, glow bool) *tapeLayers {
	p := int(math.Ceil(float64(w)*0.08)) + 2
	bodyH := int(math.Round(tapeH * float64(w)))
	L := &tapeLayers{key: key, m: tapeMap{float32(p), float32(w)}, cw: w + 2*p, ch: bodyH + 2*p,
		floor: p + bodyH, refl: int(math.Round(tapeFloor * float64(w))), pal: pal, dark: tapeDarkPaper(paper)}
	t := tapeInks{L.m, pal}
	shell, window, over := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
	light, leaks := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
	paintShell(shell, light, leaks, t, paper)
	paintWindow(window, light, t)
	paintOver(over, t)
	L.shell, L.window, L.over = shell.image(), window.image(), over.image()
	if glow {
		g, s := newTapeCanvas(L.cw, L.ch), newTapeCanvas(L.cw, L.ch)
		for _, b := range []struct {
			r int
			k float32
		}{{max(1, int(math.Round(float64(w)*0.008))), 0.9}, {max(2, int(math.Round(float64(w)*0.02))), 0.55}} {
			light.bloom(g, b.r, b.k*pal.glow)
			leaks.bloom(s, b.r, b.k*pal.glow)
		}
		L.glow, L.spots = g.image(), s.image()
	}
	mask := newTapeCanvas(L.cw, L.ch)
	mask.paint(tapeLabelForm(L.m), flat(tapeWhite, 1))
	L.mask = mask.image()
	L.reel = paintReel(L.m.d(tapeHubR), L.m.d(tapeHoleR)).image()
	L.dot = paintDot().image()
	return L
}

// free gives the layers' memory back.
func (L *tapeLayers) free() {
	for _, img := range []*ebiten.Image{L.shell, L.window, L.over, L.glow, L.spots, L.mask, L.reel, L.dot} {
		if img != nil {
			img.Deallocate()
		}
	}
}

// tapeEdgePath is the body's outline as a closed polyline in body units,
// clockwise from the top left (rounded corners, the wider lower half), with
// the distance along it at each point: what the dust and the orbiting
// light travel on.
type tapeEdgePath struct {
	pts [][2]float64
	cum []float64
}

var tapeEdge = func() tapeEdgePath {
	const R, e, H, a = tapeCorner, tapeEar, tapeH, tapeEarV
	var pts [][2]float64
	line := func(x0, y0, x1, y1 float64) {
		n := max(1, int(math.Ceil(math.Hypot(x1-x0, y1-y0)/0.005)))
		for i := range n {
			f := float64(i) / float64(n)
			pts = append(pts, [2]float64{x0 + (x1-x0)*f, y0 + (y1-y0)*f})
		}
	}
	arc := func(cx, cy, a0, a1 float64) {
		for i := range 12 {
			t := a0 + (a1-a0)*float64(i)/12
			pts = append(pts, [2]float64{cx + R*math.Cos(t), cy + R*math.Sin(t)})
		}
	}
	line(R, 0, 1-R, 0)
	arc(1-R, R, -math.Pi/2, 0)
	line(1, R, 1, a)
	line(1, a, 1+e, a+e)
	line(1+e, a+e, 1+e, H-R)
	arc(1+e-R, H-R, 0, math.Pi/2)
	line(1+e-R, H, -e+R, H)
	arc(-e+R, H-R, math.Pi/2, math.Pi)
	line(-e, H-R, -e, a+e)
	line(-e, a+e, 0, a)
	line(0, a, 0, R)
	arc(R, R, math.Pi, math.Pi*1.5)
	pts = append(pts, pts[0])
	cum := []float64{0}
	for i := 1; i < len(pts); i++ {
		cum = append(cum, cum[i-1]+math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1]))
	}
	return tapeEdgePath{pts, cum}
}()

// at is the point a fraction p of the way around the edge (in body units)
// and the outward normal there.
func (e tapeEdgePath) at(p float64) (u, v, nx, ny float64) {
	s := (math.Mod(p, 1) + 1)
	s = math.Mod(s, 1) * e.cum[len(e.cum)-1]
	hi := max(1, min(len(e.cum)-1, sortSearch(e.cum, s)))
	lo := hi - 1
	a, b := e.pts[lo], e.pts[hi]
	l := max(e.cum[hi]-e.cum[lo], 1e-9)
	f := (s - e.cum[lo]) / l
	tx, ty := (b[0]-a[0])/l, (b[1]-a[1])/l
	return a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f, ty, -tx
}

// sortSearch is the index of the first value in cum greater than s.
func sortSearch(cum []float64, s float64) int {
	i, _ := slices.BinarySearch(cum, s)
	for i < len(cum) && cum[i] <= s {
		i++
	}
	return i
}

// tapeSpeck is one speck of dust of light drifting along the edge: where it
// is (p, a fraction of the way around, and off, out of the edge), its
// speed, how big and how white it is, its twinkle and how long it lives.
type tapeSpeck struct {
	big                     bool
	p, v, off, ph, life     float64
	r, vmax, white, tw, end float64
}

// tapeDust is the specks of light, always the same (a fixed seed): six big
// ones (the light leaks, drifting slower) and twelve small ones.
type tapeDust struct {
	specks []tapeSpeck
	rnd    *rand.Rand
	t      float64 // the app's time it was moved to
}

func newTapeDust() *tapeDust {
	d := &tapeDust{rnd: rand.New(rand.NewPCG(12345, 1))}
	for i := range 18 {
		f := d.rnd.Float64
		s := tapeSpeck{big: i < 6, p: f(), ph: f() * 2 * math.Pi}
		if s.big {
			s.end, s.r, s.vmax, s.white, s.tw = 6+f()*8, 0.035+f()*0.025, 0.006, 0.35, 0.5+f()*0.7
		} else {
			s.end, s.r, s.vmax, s.white, s.tw = 3+f()*5, 0.012+f()*0.01, 0.014, 0.55+f()*0.3, 1.5+f()*2.5
		}
		s.life = f() * s.end
		d.specks = append(d.specks, s)
	}
	return d
}

// step moves the dust by one app frame (1/60 s): each speck's speed takes a
// random kick, so it keeps changing direction; it fades out at the end of
// its life and comes back somewhere else.
func (d *tapeDust) step() {
	const dt = 1.0 / 60
	for i := range d.specks {
		s := &d.specks[i]
		s.v = max(-s.vmax, min(s.vmax, (s.v+(d.rnd.Float64()-0.5)*s.vmax*0.3)*0.99))
		s.p = math.Mod(s.p+s.v*dt+1, 1)
		lim := 0.012
		if s.big {
			lim = 0.006
		}
		s.off = max(-lim, min(lim, (s.off+(d.rnd.Float64()-0.5)*lim*0.2)*0.98))
		s.ph += s.tw * dt
		if s.life += dt; s.life > s.end {
			s.life, s.p, s.v = 0, d.rnd.Float64(), 0
		}
	}
}

// moveTo moves the dust to the app's time t, a frame at a time (at most a
// second's worth, after a pause).
func (d *tapeDust) moveTo(t float64) {
	for n := 0; d.t < t && n < 60; n++ {
		d.step()
		d.t += 1.0 / 60
	}
	d.t = max(d.t, t-1.0/60)
}

// fitHand fits s in maxW the way a hand does: written tighter as it runs
// out of room, down to minSq of its width, then cut with an ellipsis. width
// measures a string; it returns what to write and how much to squeeze it.
func fitHand(s string, maxW, minSq float64, width func(string) float64) (string, float64) {
	s = strings.TrimSpace(s)
	w := width(s)
	if w <= 0 || w <= maxW {
		return s, 1
	}
	sq := max(minSq, maxW/w)
	if w*sq <= maxW {
		return s, sq
	}
	r := []rune(s)
	for len(r) > 1 && width(string(r)+"…")*sq > maxW {
		r = r[:len(r)-1]
	}
	return strings.TrimRight(string(r), " ") + "…", sq
}

// tapeFonts are the handwriting fonts (Caveat, OFL), read the first time a
// label is written: regular and bold, and their faces by size.
var tapeFonts struct {
	read  bool
	fonts [2]*opentype.Font
	faces map[[2]int]text.Face
}

// handFace is Caveat at px pixels, smooth (unlike the pixel fonts); nil if
// it could not be read.
func handFace(bold bool, px int) (text.Face, *opentype.Font) {
	tf := &tapeFonts
	if !tf.read {
		tf.read, tf.faces = true, map[[2]int]text.Face{}
		for i, name := range []string{"fonts/Caveat-Regular.ttf.gz", "fonts/Caveat-Bold.ttf.gz"} {
			tf.fonts[i], _ = parseFont(name)
		}
	}
	w := 0
	if bold {
		w = 1
	}
	f := tf.fonts[w]
	if f == nil {
		return nil, nil
	}
	if face, ok := tf.faces[[2]int{w, px}]; ok {
		return face, f
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, nil
	}
	tf.faces[[2]int{w, px}] = text.NewGoXFace(face)
	return tf.faces[[2]int{w, px}], f
}

// handCovers says whether f has every letter of s: Caveat has Latin and
// Cyrillic, not Chinese, Japanese, Korean, Arabic, Hindi or Thai.
func handCovers(f *opentype.Font, s string) bool {
	var buf sfnt.Buffer
	for _, r := range s {
		if r == ' ' {
			continue
		}
		if i, err := f.GlyphIndex(&buf, r); err != nil || i == 0 {
			return false
		}
	}
	return true
}

// writeLabel writes the song's title and artist by hand on the label's two
// lines, centered and a little tilted. What Caveat cannot write goes in the
// app's own font, upright.
func writeLabel(dst *ebiten.Image, L *tapeLayers, title, artist string, bold bool, scale float64) {
	m := L.m
	ink := rgbHex(0x1a1170)
	if L.dark {
		ink = rgbHex(0xf2eeff)
	}
	mid := float64(m.x((tapeLinesU[0] + tapeLinesU[1]) / 2))
	span := float64(m.d(tapeLinesU[1] - tapeLinesU[0]))
	for i, l := range []struct {
		s                 string
		size, tilt, inset float64
		app               text.Face
		cap               int
	}{{title, 0.07, -0.025, 0.02, BigFace, bigCap}, {artist, 0.058, -0.012, 0.04, SmallFace, smallCap}} {
		if strings.TrimSpace(l.s) == "" {
			continue
		}
		base := math.Round(float64(m.y(tapeLines[i]))) - 0.5
		maxW := span - float64(m.d(float32(l.inset)))
		face, f := handFace(bold, max(4, int(math.Round(float64(m.w)*l.size*scale))))
		if face == nil || !handCovers(f, l.s) {
			s, _ := fitHand(l.s, maxW, 1, func(t string) float64 { return float64(textWidth(t, l.app)) })
			drawText(dst, s, l.app, int(mid)-textWidth(s, l.app)/2, int(base)-l.cap, ink)
			continue
		}
		s, sq := fitHand(l.s, maxW, 0.78, func(t string) float64 { return text.Advance(t, face) })
		op := &text.DrawOptions{}
		op.PrimaryAlign = text.AlignCenter
		op.GeoM.Translate(0, -face.Metrics().HAscent)
		op.GeoM.Scale(sq, 1)
		op.GeoM.Rotate(l.tilt)
		op.GeoM.Translate(mid, base)
		op.ColorScale.ScaleWithColor(ink)
		op.Filter = ebiten.FilterLinear
		text.Draw(dst, s, face, op)
	}
}

// tapeState is what the cassette keeps between frames. Only the UI thread
// touches it.
var tapeState struct {
	layers   *tapeLayers
	textKey  string
	text     *ebiten.Image // the label written by hand
	coverKey string
	cover    *ebiten.Image // the cover cut to the label's shape
	frame    *ebiten.Image // the cassette put together this frame
	refl     *ebiten.Image // its lower part upside down, for the reflection
	dust     *tapeDust
}

// tapeImage keeps img at w×h, making it again if the size changed.
func tapeImage(img **ebiten.Image, w, h int) *ebiten.Image {
	if *img == nil || (*img).Bounds().Dx() != w || (*img).Bounds().Dy() != h {
		if *img != nil {
			(*img).Deallocate()
		}
		*img = ebiten.NewImage(w, h)
	}
	return *img
}

// tapeLayersFor returns the layers for a cassette w pixels wide in the
// current look, painting them again if something changed.
func (a *App) tapeLayersFor(w int, cover *[4]color.RGBA) *tapeLayers {
	cfg := a.cfg.UI.Tape
	key := fmt.Sprint(w, cfg.Skin, cfg.Paper, cfg.Glow != "off")
	if cfg.Skin == "cover" && cover != nil {
		key += fmt.Sprint(*cover)
	}
	st := &tapeState
	if st.layers == nil || st.layers.key != key {
		if st.layers != nil {
			st.layers.free()
		}
		st.layers = newTapeLayers(key, w, tapeSkinPalette(cfg.Skin, cover), tapePaper(cfg.Paper), cfg.Glow != "off")
		st.textKey, st.coverKey = "", ""
	}
	return st.layers
}

// toggleCassette switches the player between the cover with the vinyl and
// the cassette (a tap on either, or Settings → Cassette).
func (a *App) toggleCassette() {
	a.cfg.UI.Cassette = !a.cfg.UI.Cassette
	a.saveConfig()
}

// tapeGlowLevel is how brightly the glow is added now: steady, breathing
// every 4 s, or with the bass (like the background).
func (a *App) tapeGlowLevel() float32 {
	switch tapeAnim(a.cfg.UI.Tape.GlowAnim) {
	case "breathe":
		return 0.72 + 0.38*float32(math.Sin(a.t*2*math.Pi/4))
	case "beat":
		return 0.65 + 0.75*bgPump
	}
	return 1
}

// drawTape draws the cassette with its body's corner at (x, y), w pixels
// wide: al's cover as the label (or the song written by hand), the reels
// turned by a.reels and the tape split according to progress, its glow and
// its reflection under it. flash is the song-change flash. It returns the
// label's rectangle, so the icons (favorite, timer...) can go on top.
func (a *App) drawTape(dst *ebiten.Image, al *Album, x, y, w int, progress float64, flash float32) image.Rectangle {
	if w < 16 {
		return image.Rectangle{} // nothing to draw (the warm-up's tiny canvas)
	}
	cfg, st := a.cfg.UI.Tape, &tapeState
	var cover *[4]color.RGBA
	if al != nil {
		if c, ok := al.TapeColors(); ok {
			cover = &c
		}
	}
	L := a.tapeLayersFor(w, cover)
	m := L.m
	f := tapeImage(&st.frame, L.cw, L.ch)
	f.Clear()
	f.DrawImage(L.shell, nil)

	// The label: the song written by hand, then the cover over it as it
	// loads.
	lx0, ly0 := int(math.Round(float64(m.x(tapeLabel[0])))), int(math.Round(float64(m.y(tapeLabel[1]))))
	iw, ih := int(math.Round(float64(m.d(tapeLabel[2]-tapeLabel[0])))), int(math.Round(float64(m.d(tapeLabel[3]-tapeLabel[1]))))
	art := image.Rect(x-int(m.p)+lx0, y-int(m.p)+ly0, x-int(m.p)+lx0+iw, y-int(m.p)+ly0+ih)
	var img *ebiten.Image
	alpha := float32(0)
	if al != nil && cfg.Label != "hand" {
		img, alpha = a.cover(al, iw)
	}
	if alpha < 1 {
		song := a.playingSong
		scale := float64(cmp0(cfg.HandSize, tapeHandDefault)) / 100
		if key := fmt.Sprint(L.key, song.Title, "\n", song.Artist, cfg.Thin, scale); st.textKey != key {
			t := tapeImage(&st.text, L.cw, L.ch)
			t.Clear()
			writeLabel(t, L, song.Title, song.Artist, !cfg.Thin, scale)
			st.textKey = key
		}
		f.DrawImage(st.text, nil)
	}
	if img != nil {
		if key := fmt.Sprintf("%s|%p", L.key, img); st.coverKey != key {
			c := tapeImage(&st.cover, L.cw, L.ch)
			c.Clear()
			b := img.Bounds()
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(lx0), float64(ly0-(b.Dy()-ih)/8)) // the top strip, where titles usually are
			c.DrawImage(img, op)
			c.DrawImage(L.mask, &ebiten.DrawImageOptions{Blend: ebiten.BlendDestinationIn})
			st.coverKey = key
		}
		op := &ebiten.DrawImageOptions{}
		op.ColorScale.ScaleAlpha(alpha)
		f.DrawImage(st.cover, op)
	}
	if flash > 0 {
		op := &ebiten.DrawImageOptions{}
		op.ColorScale.ScaleWithColor(withAlpha(a.theme.Text, uint8(150*flash)))
		f.DrawImage(L.mask, op)
	}
	f.DrawImage(L.window, nil)

	// The glow, the light leaks or the dust, and the orbiting light, as
	// light added on top.
	if L.glow != nil {
		lv := a.tapeGlowLevel()
		light := &ebiten.DrawImageOptions{Blend: ebiten.BlendLighter}
		light.ColorScale.Scale(lv, lv, lv, lv)
		f.DrawImage(L.glow, light)
		// dot adds a soft dot of light of radius r (in body widths) at (u, v).
		dot := func(u, v, r float64, c color.RGBA, k float32) {
			op := &ebiten.DrawImageOptions{Blend: ebiten.BlendLighter, Filter: ebiten.FilterLinear}
			s := 2 * r * float64(m.w) / tapeDotSize
			op.GeoM.Translate(-tapeDotSize/2, -tapeDotSize/2)
			op.GeoM.Scale(s, s)
			op.GeoM.Translate(float64(m.x(float32(u))), float64(m.y(float32(v))))
			op.ColorScale.Scale(float32(c.R)/255*k, float32(c.G)/255*k, float32(c.B)/255*k, k)
			f.DrawImage(L.dot, op)
		}
		if cfg.Glow == "static" {
			f.DrawImage(L.spots, light)
		} else {
			if st.dust == nil {
				st.dust = newTapeDust()
			}
			st.dust.moveTo(a.t)
			size := float64(cmp0(cfg.DustSize, tapeDustDefault)) / 100
			for _, s := range st.dust.specks {
				bright := 0.95
				if s.big {
					bright = 0.6
				}
				fade := min(1, s.life/0.8, (s.end-s.life)/0.8)
				k := min(1, bright*fade*(0.6+0.4*math.Sin(s.ph))*float64(L.pal.glow*lv))
				if k < 0.01 {
					continue
				}
				u, v, nx, ny := tapeEdge.at(s.p)
				dot(u+nx*s.off, v+ny*s.off, s.r*size, mix(gradAt(L.pal.shell, float32(u)), tapeWhite, s.white), float32(k))
			}
		}
		if cfg.GlowAnim == "orbit" {
			u, v, _, _ := tapeEdge.at(a.t / 5)
			dot(u, v, 0.09, mix(gradAt(L.pal.shell, float32(u)), tapeWhite, 0.45), 0.85*L.pal.glow)
		}
	}

	// The tape on each reel, seen through the glass.
	full := float64(m.d(tapeFull))
	rl, rr := reelRadii(progress)
	sop := &ebiten.DrawRectShaderOptions{}
	vx0, vy0, vx1, vy1 := m.x(tapeView[0]), m.y(tapeView[1]), m.x(tapeView[2]), m.y(tapeView[3])
	sop.GeoM.Translate(math.Floor(float64(vx0)), math.Floor(float64(vy0)))
	sop.Uniforms = map[string]any{
		"Left":  []float32{m.x(tapeReelU[0]), m.y(tapeReelV)},
		"Right": []float32{m.x(tapeReelU[1]), m.y(tapeReelV)},
		"Radii": []float32{float32(full * rl), float32(full * rr)},
		"Core":  float32(full * reelCore),
		"View":  []float32{vx0, vy0, vx1, vy1},
		"ViewR": m.d(tapeViewR),
	}
	f.DrawRectShader(int(math.Ceil(float64(vx1)))-int(vx0)+1, int(math.Ceil(float64(vy1)))-int(vy0)+1, tapeShader, sop)

	// The reels, turning (both the same way, like playing side A).
	half := float64(L.reel.Bounds().Dx()) / 2
	for i, u := range tapeReelU {
		ang := a.reels.left
		if i == 1 {
			ang = a.reels.right
		}
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Translate(-half, -half)
		op.GeoM.Rotate(-ang)
		op.GeoM.Translate(float64(m.x(u)), float64(m.y(tapeReelV)))
		f.DrawImage(L.reel, op)
	}
	f.DrawImage(L.over, nil)

	// On the screen, dithered: the cassette cut at the floor (the floor
	// hides its glow below), and its reflection added under it.
	ox, oy := float64(x)-float64(m.p), float64(y)-float64(m.p)
	h := L.floor
	if cfg.NoReflection {
		h = L.ch
	}
	body := &ebiten.DrawRectShaderOptions{}
	body.GeoM.Translate(ox, oy)
	body.Images[0] = f.SubImage(image.Rect(0, 0, L.cw, h)).(*ebiten.Image)
	body.Uniforms = map[string]any{"Reflect": float32(0), "Height": float32(1)}
	dst.DrawRectShader(L.cw, h, tapeMixShader, body)
	if !cfg.NoReflection && L.refl > 0 {
		r := tapeImage(&st.refl, L.cw, L.refl)
		r.Clear()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(1, -1)
		op.GeoM.Translate(0, float64(L.refl))
		r.DrawImage(f.SubImage(image.Rect(0, L.floor-L.refl, L.cw, L.floor)).(*ebiten.Image), op)
		refl := &ebiten.DrawRectShaderOptions{Blend: ebiten.BlendLighter}
		refl.GeoM.Translate(ox, oy+float64(L.floor))
		refl.Images[0] = r
		refl.Uniforms = map[string]any{"Reflect": float32(1), "Height": float32(L.refl)}
		dst.DrawRectShader(L.cw, L.refl, tapeMixShader, refl)
	}
	return art
}

// cmp0 is v, or def when v is 0 (a setting left at its default).
func cmp0(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
