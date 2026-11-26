package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestReelRadiiKeepTape comprueba que la cinta del casete ni se gana ni se
// pierde: empieza toda a la izquierda, acaba toda a la derecha y, entre
// medias, el total de los dos carretes (el área) no cambia.
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

// TestReelsEmptyTurnsFaster comprueba que el carrete con menos cinta gira
// más rápido (la cinta corre a velocidad fija).
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

// TestCoverColors4 comprueba que los cuatro colores más vivos de una portada
// salen uno por tono y en orden alrededor de la rueda de color (para que el
// degradado del casete fluya), y que una portada gris se queda con el aspecto
// cromo.
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

// TestTapeEdgeOutward comprueba el contorno por el que viajan las motas de
// luz: cada punto está en el borde del cuerpo y su normal apunta hacia fuera.
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

// TestDustStaysOnEdge comprueba que las motas de luz no se salen del borde
// mientras flotan durante dos minutos, y que cada una, al acabar su vida,
// vuelve en otro sitio.
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

// TestFitHand comprueba cómo se escribe a mano un título largo: primero más
// apretado, hasta el 78% de su ancho, y luego cortado con puntos suspensivos;
// sin apretar (las fuentes de píxeles de la app) solo se corta.
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

// TestTapeSettings comprueba la sección del casete en Ajustes: con el casete
// apagado solo se ve su interruptor; sus opciones se recorren y acaban en la
// configuración; la animación del resplandor se oculta sin resplandor, y el
// tamaño de las motas sin motas.
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

// TestTapeStringsFilled comprueba que ningún texto del casete está vacío en
// ningún idioma: su tabla tiene un hueco por idioma, y uno olvidado no
// mostraría nada.
func TestTapeStringsFilled(t *testing.T) {
	for l, m := range strs {
		for k, v := range m {
			if (strings.HasPrefix(k, "tape_") || strings.HasPrefix(k, "set_tape_") || k == "sec_cassette") && v == "" {
				t.Errorf("%q is empty in %s", k, l)
			}
		}
	}
}

// TestTapeShellPainted comprueba el cuerpo pintado en la CPU: opaco en el
// centro, transparente fuera de su esquina redondeada, y con su luz a lo
// largo del borde.
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

// TestTapeLookPaper comprueba el papel de la etiqueta con "Del aspecto": un
// aspecto de un solo color (el de un papel, o el de la pantalla) le da su
// color, así que una etiqueta azul marino lleva la tinta clara; los demás
// conservan su estampado, y un papel elegido manda. Con el color de pantalla
// Negro (su acento es claro), "screen" es el aspecto negro con la etiqueta
// negra.
func TestTapeLookPaper(t *testing.T) {
	accent := rgbHex(0x123456)
	for _, c := range []struct {
		skin, paper string
		want        *color.RGBA
	}{
		{"navy", "", tapePaper("navy")},
		{"screen", "", &accent},
		{"", "", nil},
		{"black", "", nil},
		{"navy", "white", tapePaper("white")},
	} {
		_, got := tapeLook(c.skin, c.paper, nil, accent, false)
		if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
			t.Errorf("skin %q, paper %q: got %v, want %v", c.skin, c.paper, got, c.want)
		}
	}
	if _, p := tapeLook("navy", "", nil, accent, false); !tapeDarkPaper(p) {
		t.Errorf("navy from the skin: want a dark paper (light ink)")
	}
	pal, p := tapeLook("screen", "", nil, rgbHex(0xe8e8ec), true)
	if !tapeDarkPaper(p) || pal.solid == nil || *pal.solid != *tapeSkinPalette("black", nil).solid {
		t.Errorf("screen on Black: got solid %v and paper %v, want the black skin with a black label", pal.solid, p)
	}
	if _, p := tapeLook("screen", "white", nil, rgbHex(0xe8e8ec), true); *p != *tapePaper("white") {
		t.Errorf("screen on Black with white paper: got %v, want the white paper", p)
	}
}

// TestTapeTooSmall comprueba que un casete demasiado estrecho para dibujarse
// (el calentamiento dibuja todas las pantallas en un lienzo de 16×16) no
// dibuja nada en vez de pedir una imagen sin tamaño, que tumbaba la app al
// arrancar.
func TestTapeTooSmall(t *testing.T) {
	a := &App{offline: true}
	if r := a.drawTape(nil, nil, 0, 0, -4, 0.5, 0); !r.Empty() {
		t.Errorf("got %v, want nothing drawn", r)
	}
}
