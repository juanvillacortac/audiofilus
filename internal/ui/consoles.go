package ui

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// Las consolas del modo juego: cada sistema tiene una maqueta low poly de su
// aparato (genérica, sin marcas), hecha con el motor 3D de los visualizadores
// (drift.go): caras planas en intensidad, pasadas a la paleta del tema con
// trama. Donde va la imagen (la pantalla de una portátil o de un monitor, la
// etiqueta de un cartucho, la cara de un disco) se pinta la caja del juego
// elegido encima, después de la paleta, a color y pixelada como las portadas.

// consoleKind es la forma de una maqueta.
type consoleKind int

const (
	ckHandheld  consoleKind = iota // portátil vertical (Game Boy)
	ckWide                         // portátil apaisada (GBA, Game Gear)
	ckSlim                         // portátil ancha y fina (PSP)
	ckClamshell                    // de tapa, dos pantallas (DS)
	ckCart                         // de sobremesa, con cartucho
	ckDisc                         // de sobremesa, con disco
	ckComputer                     // monitor y teclado (los ports)
	ckArcade                       // mueble de recreativa
	ckVisor                        // gafas sobre un soporte (Virtual Boy)
	ckStar                         // una estrella (Favoritos)
	ckClock                        // un reloj (Jugados recientemente)
	ckTrophy                       // un trofeo (Más jugados)
	ckConsoleCnt
)

// consoleLook es cómo es la maqueta de un sistema: su forma, el tono del
// cuerpo (intensidad: 0,3 oscuro, 0,9 claro) y unas medidas que la varían.
type consoleLook struct {
	kind consoleKind
	tone float64
	w, h float64 // ancho y alto (o largo, en las de sobremesa) del cuerpo
	ar   float64 // proporción ancho/alto de la imagen (pantalla o etiqueta)
}

// consoleLooks son las maquetas de los sistemas conocidos.
var consoleLooks = map[string]consoleLook{
	"gb":            {ckHandheld, 0.75, 0.9, 1.48, 1.1},
	"gbc":           {ckHandheld, 0.5, 0.8, 1.32, 1.1},
	"pokemini":      {ckHandheld, 0.6, 0.62, 0.9, 1.25},
	"ngp":           {ckWide, 0.45, 1.2, 0.8, 1.2},
	"ngpc":          {ckWide, 0.55, 1.2, 0.8, 1.2},
	"gba":           {ckWide, 0.45, 1.45, 0.82, 1.5},
	"gamegear":      {ckWide, 0.28, 1.6, 0.9, 1.33},
	"lynx":          {ckWide, 0.3, 1.8, 0.8, 1.6},
	"wswan":         {ckWide, 0.6, 1.4, 0.75, 1.5},
	"wswanc":        {ckWide, 0.5, 1.4, 0.75, 1.5},
	"psp":           {ckSlim, 0.28, 1.7, 0.75, 1.76},
	"psvita":        {ckSlim, 0.3, 1.7, 0.8, 1.76},
	"nds":           {ckClamshell, 0.7, 1.3, 0.75, 1.33},
	"3ds":           {ckClamshell, 0.5, 1.35, 0.8, 1.66},
	"virtualboy":    {ckVisor, 0.3, 1.5, 0.6, 1.33},
	"favorites":     {ckStar, 1.0, 1, 1, 1},
	"recent":        {ckClock, 0.85, 1, 1, 1},
	"mostplayed":    {ckTrophy, 1.0, 1, 1, 1},
	"nes":           {ckCart, 0.7, 1.7, 1.4, 1.2},
	"fds":           {ckCart, 0.7, 1.7, 1.4, 1.2},
	"snes":          {ckCart, 0.75, 1.6, 1.3, 1.6},
	"n64":           {ckCart, 0.3, 1.65, 1.35, 1.4},
	"megadrive":     {ckCart, 0.25, 1.65, 1.2, 1.3},
	"genesis":       {ckCart, 0.25, 1.65, 1.2, 1.3},
	"sega32x":       {ckCart, 0.25, 1.65, 1.2, 1.3},
	"mastersystem":  {ckCart, 0.28, 1.7, 1.1, 1.1},
	"sg1000":        {ckCart, 0.35, 1.5, 1.1, 1.1},
	"pcengine":      {ckCart, 0.85, 1.1, 1.1, 1.0},
	"supergrafx":    {ckCart, 0.85, 1.4, 1.2, 1.0},
	"atari2600":     {ckCart, 0.25, 1.8, 1.2, 1.2},
	"atari5200":     {ckCart, 0.28, 1.9, 1.2, 1.2},
	"atari7800":     {ckCart, 0.28, 1.7, 1.2, 1.2},
	"colecovision":  {ckCart, 0.28, 1.8, 1.3, 1.2},
	"intellivision": {ckCart, 0.45, 1.9, 1.1, 1.2},
	"neogeo":        {ckArcade, 0.3, 1, 1, 1.33},
	"mame":          {ckArcade, 0.28, 1, 1, 1.33},
	"fbneo":         {ckArcade, 0.28, 1, 1, 1.33},
	"cps1":          {ckArcade, 0.28, 1, 1, 1.33},
	"cps2":          {ckArcade, 0.28, 1, 1, 1.33},
	"cps3":          {ckArcade, 0.28, 1, 1, 1.33},
	"naomi":         {ckArcade, 0.28, 1, 1, 1.33},
	"atomiswave":    {ckArcade, 0.28, 1, 1, 1.33},
	"psx":           {ckDisc, 0.75, 1.6, 1.2, 1},
	"ps2":           {ckDisc, 0.25, 1.7, 1.2, 1},
	"dreamcast":     {ckDisc, 0.9, 1.5, 1.45, 1},
	"saturn":        {ckDisc, 0.3, 1.7, 1.3, 1},
	"segacd":        {ckDisc, 0.25, 1.6, 1.3, 1},
	"pcenginecd":    {ckDisc, 0.85, 1.5, 1.2, 1},
	"3do":           {ckDisc, 0.25, 1.7, 1.2, 1},
	"pico8":         {ckWide, 0.85, 1.2, 0.8, 1},
	"tic80":         {ckWide, 0.4, 1.2, 0.8, 1.6},
	"arduboy":       {ckWide, 0.4, 1.0, 0.65, 2},
}

// lookFor es la maqueta del sistema name, o una que va con su tipo de aparato
// (hardware, de es_systems.cfg): los ports, motores y programas son un
// ordenador.
func lookFor(name, hardware string) consoleLook {
	if l, ok := consoleLooks[name]; ok {
		return l
	}
	switch strings.ToLower(hardware) {
	case "portable":
		return consoleLook{ckWide, 0.45, 1.45, 0.82, 1.5}
	case "console":
		return consoleLook{ckCart, 0.45, 1.7, 1.3, 1.2}
	case "arcade":
		return consoleLook{ckArcade, 0.28, 1, 1, 1.33}
	}
	return consoleLook{ckComputer, 0.7, 1.3, 1, 1.33}
}

// texPoly es un polígono donde se pinta la imagen del juego, con la posición
// de cada vértice en ella (0..1).
type texPoly struct {
	pts []v3
	uv  [][2]float64
	n   v3 // hacia dónde mira (para no pintarla de espaldas)
}

// consoleModel es una maqueta armada: sus caras, dónde va la imagen y su
// radio (para encuadrarla).
type consoleModel struct {
	faces []face
	// layer es la capa de cada cara: 1 el cuerpo, 2 lo que va encima (botones,
	// pantallas, el cartucho). El motor ordena por profundidad media, y un
	// botón pequeño junto a una cara grande del cuerpo a veces quedaba
	// detrás: los de la capa 2 se ordenan como si estuvieran un poco más
	// cerca (drawModel).
	layer []int
	tex   []texPoly
	spin  []int // caras que giran solas (el disco), por índice en faces
	spinT []int // y las de la imagen que giran con ellas
	// spinCenter es el punto alrededor del que giran (el centro del disco).
	spinCenter v3
	spinTilt   float64 // y lo que se inclina (hacia la cámara)
	// spinFace y spinTex marcan, por índice, lo que gira (sin buscar en cada
	// fotograma).
	spinFace, spinTex []bool
	radius            float64
	center            v3 // el centro de la maqueta: alrededor de él gira toda
	// Las maquetas leídas de un modelo (consoles_glb.go): sus texturas, en
	// una imagen (la lee el rasterizador, en la CPU) y, por cara, dónde cae
	// en ella (nil sin textura).
	atlasSrc *image.RGBA
	uv       []*[3][2]float32
	// twoSided: se pintan también las caras de espaldas.
	twoSided bool
	// col es, en las maquetas leídas de un modelo, el color de cada cara
	// (se pintan a color, con su textura); accent, los dos colores de la
	// maqueta para el tema.
	col       [][3]float32
	accent    [2]color.RGBA
	hasAccent bool
}

// add suma caras del cuerpo al modelo.
func (m *consoleModel) add(fs ...face) {
	for range fs {
		m.layer = append(m.layer, 1)
	}
	m.faces = append(m.faces, fs...)
}

// detail suma caras que van sobre el cuerpo (se pintan después).
func (m *consoleModel) detail(fs ...face) {
	for range fs {
		m.layer = append(m.layer, 2)
	}
	m.faces = append(m.faces, fs...)
}

// prismZ es un prisma de frente: el contorno convexo outline (en x, y,
// alrededor de c) extruido de c.z-hd a c.z+hd. Una sola cara delante y otra
// detrás: sin caras que coincidan (como una caja con dos cilindros a los
// lados, que se pisaban).
func prismZ(c v3, outline [][2]float64, hd, tone float64) []face {
	n := len(outline)
	front, back := make([]v3, n), make([]v3, n)
	for i, p := range outline {
		front[i] = v3{c.x + p[0], c.y + p[1], c.z + hd}
		back[i] = v3{c.x + p[0], c.y + p[1], c.z - hd}
	}
	out := []face{{pts: outward(front, c), tone: tone}, {pts: outward(back, c), tone: tone * 0.6}}
	for i := range n {
		j := (i + 1) % n
		q := []v3{front[i], front[j], back[j], back[i]}
		out = append(out, face{pts: outward(q, c), tone: tone * 0.8})
	}
	return out
}

// stadium es el contorno de un rectángulo de w×h con los lados cortos
// redondeados (como una GBA), en k pasos por semicírculo.
func stadium(w, h float64, k int) [][2]float64 {
	r := h / 2
	cx := w/2 - r
	var out [][2]float64
	for i := 0; i <= k; i++ {
		a := -math.Pi/2 + math.Pi*float64(i)/float64(k)
		out = append(out, [2]float64{cx + r*math.Cos(a), r * math.Sin(a)})
	}
	for i := 0; i <= k; i++ {
		a := math.Pi/2 + math.Pi*float64(i)/float64(k)
		out = append(out, [2]float64{-cx + r*math.Cos(a), r * math.Sin(a)})
	}
	return out
}

// quadZ es la imagen en un rectángulo de frente (mirando a +z) de centro c y
// medias medidas hw, hh.
func quadZ(c v3, hw, hh float64) texPoly {
	return texPoly{
		pts: []v3{{c.x - hw, c.y + hh, c.z}, {c.x + hw, c.y + hh, c.z}, {c.x + hw, c.y - hh, c.z}, {c.x - hw, c.y - hh, c.z}},
		uv:  [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}},
		n:   v3{0, 0, 1},
	}
}

// cylZ es un cilindro de n lados con el eje a lo largo de z (un botón que
// sale del frente): centro c, radio r, media profundidad hd.
func cylZ(c v3, r, hd float64, n int, tone float64) []face {
	var out []face
	front, back := make([]v3, n), make([]v3, n)
	for i := range n {
		s, co := math.Sincos(2 * math.Pi * float64(i) / float64(n))
		front[i] = v3{c.x + r*co, c.y + r*s, c.z + hd}
		back[n-1-i] = v3{c.x + r*co, c.y + r*s, c.z - hd}
	}
	out = append(out, face{pts: outward(front, c), tone: tone}, face{pts: outward(back, c), tone: tone * 0.6})
	for i := range n {
		j := (i + 1) % n
		q := []v3{front[i], front[j], {front[j].x, front[j].y, c.z - hd}, {front[i].x, front[i].y, c.z - hd}}
		out = append(out, face{pts: outward(q, c), tone: tone * 0.8})
	}
	return out
}

// cylY es un cilindro de n lados de eje vertical (un disco, una tapa).
func cylY(c v3, r, hh float64, n int, tone float64) []face {
	var out []face
	top, bot := make([]v3, n), make([]v3, n)
	for i := range n {
		s, co := math.Sincos(2 * math.Pi * float64(i) / float64(n))
		top[i] = v3{c.x + r*co, c.y + hh, c.z + r*s}
		bot[i] = v3{c.x + r*co, c.y - hh, c.z + r*s}
	}
	out = append(out, face{pts: outward(top, c), tone: tone}, face{pts: outward(bot, c), tone: tone * 0.5})
	for i := range n {
		j := (i + 1) % n
		q := []v3{top[i], top[j], bot[j], bot[i]}
		out = append(out, face{pts: outward(q, c), tone: tone * 0.75})
	}
	return out
}

// tilt gira las caras fs alrededor del eje x (por el punto o) un ángulo a.
func tilt(fs []face, o v3, a float64) []face {
	for i := range fs {
		pts := make([]v3, len(fs[i].pts))
		for k, p := range fs[i].pts {
			pts[k] = rotX(p.sub(o), a).add(o)
		}
		fs[i].pts = pts
	}
	return fs
}

// tiltTex es tilt para una imagen.
func tiltTex(t texPoly, o v3, a float64) texPoly {
	pts := make([]v3, len(t.pts))
	for k, p := range t.pts {
		pts[k] = rotX(p.sub(o), a).add(o)
	}
	t.pts, t.n = pts, rotX(t.n, a)
	return t
}

// dpad es una cruceta de frente en c, de tamaño s.
func dpad(c v3, s, tone float64) []face {
	return append(box(c, v3{s, s * 0.33, 0.025}, tone), box(c, v3{s * 0.33, s, 0.025}, tone)...)
}

// screenImage pone en el modelo el marco oscuro de una pantalla de frente y
// la imagen encima, con la proporción ar (ancho/alto) dentro de hw×hh.
func (m *consoleModel) screenImage(c v3, hw, hh, ar float64) {
	m.detail(box(c, v3{hw + 0.05, hh + 0.05, 0.01}, 0.1)...)
	iw, ih := hw, hw/ar
	if ih > hh {
		iw, ih = hh*ar, hh
	}
	m.tex = append(m.tex, quadZ(v3{c.x, c.y, c.z + 0.012}, iw, ih))
}

// buildConsole arma la maqueta de un sistema.
func buildConsole(l consoleLook) *consoleModel {
	m := &consoleModel{}
	t := l.tone
	// Los botones contrastan con el cuerpo: claros en los oscuros y oscuros
	// en los claros.
	hi := 1.35
	if t > 0.6 {
		hi = 0.22
	}
	dark := 0.1 // los marcos de las pantallas
	switch l.kind {
	case ckHandheld:
		w, h, d := l.w, l.h, 0.3
		m.add(box(v3{0, 0, 0}, v3{w / 2, h / 2, d / 2}, t)...)
		top := h/2 - 0.08
		by := -h * 0.2
		// La pantalla, en lo que queda libre sobre los botones (en una
		// portátil baja, como la Pokémon Mini, llegaba hasta la cruceta).
		free := top - (by + w*0.12 + 0.06)
		sh := min(h*0.24, free/2-0.06)
		m.screenImage(v3{0, top - sh - 0.04, d/2 + 0.01}, w*0.36, sh, l.ar)
		m.detail(dpad(v3{-w * 0.24, by, d/2 + 0.02}, w*0.12, hi)...)
		m.detail(cylZ(v3{w * 0.3, by + 0.06, d/2 + 0.02}, w*0.075, 0.025, 8, hi)...)
		m.detail(cylZ(v3{w * 0.13, by - 0.02, d/2 + 0.02}, w*0.075, 0.025, 8, hi)...)
		m.detail(box(v3{-w * 0.08, -h * 0.37, d/2 + 0.01}, v3{0.06, 0.018, 0.01}, hi)...)
		m.detail(box(v3{w * 0.08, -h * 0.37, d/2 + 0.01}, v3{0.06, 0.018, 0.01}, hi)...)
	case ckWide, ckSlim:
		w, h, d := l.w, l.h, 0.26
		if l.kind == ckSlim {
			d = 0.18
		}
		core := w/2 - h/2
		m.add(prismZ(v3{}, stadium(w, h, 5), d/2, t)...)
		m.screenImage(v3{0, h * 0.04, d/2 + 0.01}, core*0.95, h*0.34, l.ar)
		m.detail(dpad(v3{-core - h*0.08, h * 0.02, d/2 + 0.02}, h*0.13, hi)...)
		if l.kind == ckSlim {
			for _, p := range [][2]float64{{0, 0.13}, {0.12, 0}, {0, -0.13}, {-0.12, 0}} {
				m.detail(cylZ(v3{core + h*0.08 + p[0]*h, h*0.04 + p[1]*h, d/2 + 0.02}, h*0.05, 0.02, 6, hi)...)
			}
		} else {
			m.detail(cylZ(v3{core + h*0.15, h * 0.08, d/2 + 0.02}, h*0.08, 0.025, 8, hi)...)
			m.detail(cylZ(v3{core - h*0.02, -h * 0.04, d/2 + 0.02}, h*0.08, 0.025, 8, hi)...)
		}
		// Los gatillos, arriba a los lados.
		m.detail(box(v3{-core, h/2 - 0.02, -0.02}, v3{h * 0.3, 0.04, d * 0.35}, t*0.7)...)
		m.detail(box(v3{core, h/2 - 0.02, -0.02}, v3{h * 0.3, 0.04, d * 0.35}, t*0.7)...)
	case ckClamshell:
		w, h, d := l.w, l.h, 0.09
		// La mitad de abajo, inclinada hacia la cámara; la de arriba, de pie.
		hinge := v3{0, 0, 0}
		m.add(tilt(box(v3{0, -h / 2, 0}, v3{w / 2, h / 2, d / 2}, t), hinge, -1.0)...)
		low := box(v3{0, -h / 2, d/2 + 0.005}, v3{w * 0.26, h * 0.32, 0.01}, dark)
		low = append(low, dpad(v3{-w * 0.38, -h * 0.4, d/2 + 0.02}, 0.07, hi)...)
		low = append(low, cylZ(v3{w * 0.38, -h * 0.4, d/2 + 0.02}, 0.05, 0.02, 6, hi)...)
		m.detail(tilt(low, hinge, -1.0)...)
		m.add(tilt(box(v3{0, h / 2, -d / 2}, v3{w / 2, h / 2, d / 2}, t), hinge, 0.12)...)
		m.detail(tilt(box(v3{0, h / 2, 0.005}, v3{w*0.3 + 0.04, h*0.36 + 0.04, 0.01}, dark), hinge, 0.12)...)
		iw, ih := w*0.3, w*0.3/l.ar
		m.tex = append(m.tex, tiltTex(quadZ(v3{0, h / 2, 0.018}, iw, ih), hinge, 0.12))
	case ckCart:
		w, ln, hb := l.w, l.h, 0.32
		m.add(box(v3{0, 0, 0}, v3{w / 2, hb / 2, ln / 2}, t)...)
		// Una franja de otro tono al frente y los botones.
		m.detail(box(v3{0, -hb * 0.15, ln/2 + 0.005}, v3{w * 0.45, hb * 0.2, 0.01}, t*0.55)...)
		m.detail(box(v3{-w * 0.3, hb/2 + 0.02, ln * 0.3}, v3{0.09, 0.03, 0.06}, hi)...)
		m.detail(box(v3{-w * 0.12, hb/2 + 0.02, ln * 0.3}, v3{0.09, 0.03, 0.06}, hi)...)
		// La ranura y el cartucho de pie, con la etiqueta de frente.
		cw, ch := w*0.3, w*0.3/l.ar
		cz := -ln * 0.08
		m.detail(box(v3{0, hb/2 + 0.02, cz}, v3{cw + 0.1, 0.03, 0.13}, t*0.5)...)
		cy := hb/2 + ch + 0.08
		m.detail(box(v3{0, cy, cz}, v3{cw + 0.05, ch + 0.06, 0.07}, 0.45)...)
		m.tex = append(m.tex, quadZ(v3{0, cy - 0.02, cz + 0.072}, cw, ch))
	case ckDisc:
		w, ln, hb := l.w, l.h, 0.34
		m.add(box(v3{0, 0, 0}, v3{w / 2, hb / 2, ln / 2}, t)...)
		m.detail(cylY(v3{0, hb/2 + 0.02, -0.05}, ln*0.3, 0.02, 12, t*0.8)...)
		m.detail(box(v3{-w * 0.32, hb/2 + 0.02, ln * 0.35}, v3{0.08, 0.025, 0.05}, hi)...)
		m.detail(box(v3{w * 0.32, hb/2 + 0.02, ln * 0.35}, v3{0.08, 0.025, 0.05}, hi)...)
		// El disco, flotando encima e inclinado hacia la cámara; gira solo.
		r := ln * 0.32
		dc := v3{0, hb/2 + 0.5, 0.1}
		const n = 16
		first := len(m.faces)
		disc := cylY(v3{}, r, 0.012, n, 1.1)
		disc = append(disc, cylY(v3{0, 0.014, 0}, r*0.18, 0.004, 8, dark)...)
		// Las caras del disco quedan en su sitio de origen, sin inclinar:
		// drawConsole las gira sobre su eje y luego las inclina y las lleva a
		// dc (spinTilt, spinCenter).
		m.detail(disc...)
		for i := first; i < len(m.faces); i++ {
			m.spin = append(m.spin, i)
		}
		// La imagen en la cara de arriba: un abanico con la caja recortada en
		// círculo.
		tp := texPoly{n: v3{0, 1, 0}}
		for i := range n {
			s, co := math.Sincos(2 * math.Pi * float64(i) / n)
			tp.pts = append(tp.pts, v3{r * 0.96 * co, 0.0135, r * 0.96 * s})
			tp.uv = append(tp.uv, [2]float64{0.5 + 0.5*co, 0.5 + 0.5*s})
		}
		m.spinT = append(m.spinT, len(m.tex))
		m.tex = append(m.tex, tp)
		m.spinCenter, m.spinTilt = dc, 1.15
	case ckComputer:
		mw, mh, md := l.w*0.5, l.h*0.42, 0.45
		my := 0.3
		m.add(box(v3{0, my, -0.1}, v3{mw, mh, md / 2}, t)...)
		m.screenImage(v3{0, my + 0.02, -0.1 + md/2 + 0.01}, mw*0.8, mh*0.78, l.ar)
		m.add(box(v3{0, my - mh - 0.08, -0.1}, v3{0.12, 0.08, 0.1}, t*0.8)...)
		m.add(box(v3{0, my - mh - 0.17, -0.1}, v3{0.35, 0.02, 0.25}, t*0.8)...)
		// El teclado, con las filas de teclas.
		kc := v3{0, my - mh - 0.15, 0.55}
		m.add(tilt(box(kc, v3{mw * 1.05, 0.04, 0.22}, t), kc, 0.12)...)
		var keys []face
		for i := range 4 {
			keys = append(keys, box(v3{0, kc.y + 0.05, 0.42 + float64(i)*0.08}, v3{mw * 0.95, 0.01, 0.025}, hi)...)
		}
		m.detail(tilt(keys, kc, 0.12)...)
	case ckArcade:
		bw, bh, bd := 0.75, 1.9, 0.7
		m.add(box(v3{0, 0, 0}, v3{bw / 2, bh / 2, bd / 2}, t)...)
		// La marquesina (brilla), la pantalla inclinada y el panel de mandos.
		m.detail(box(v3{0, bh/2 - 0.12, bd/2 + 0.03}, v3{bw/2 - 0.04, 0.1, 0.04}, 1.4)...)
		scr := v3{0, bh * 0.15, bd/2 + 0.005}
		m.detail(tilt(box(scr, v3{bw/2 - 0.06, 0.32, 0.01}, dark), scr, -0.18)...)
		iw, ih := bw/2-0.1, (bw/2-0.1)/l.ar
		m.tex = append(m.tex, tiltTex(quadZ(v3{scr.x, scr.y, scr.z + 0.012}, iw, ih), scr, -0.18))
		panelC := v3{0, -bh * 0.1, bd/2 + 0.12}
		m.add(tilt(box(panelC, v3{bw/2 + 0.02, 0.05, 0.16}, t*0.8), panelC, 0.35)...)
		var pn []face
		pn = append(pn, cylY(v3{-0.15, 0.08, 0}.add(panelC), 0.02, 0.05, 6, 0.5)...)
		pn = append(pn, cylY(v3{-0.15, 0.14, 0}.add(panelC), 0.045, 0.03, 6, 1.3)...)
		for i := range 3 {
			pn = append(pn, cylY(v3{0.05 + float64(i)*0.1, 0.06, 0}.add(panelC), 0.035, 0.015, 6, 1.2)...)
		}
		m.detail(tilt(pn, panelC, 0.35)...)
	case ckStar:
		// Cinco puntas, con el centro de cada cara levantado (facetada).
		const outer, inner, d, bulge = 0.75, 0.32, 0.16, 0.16
		var ring []v3
		for k := range 10 {
			r := outer
			if k%2 == 1 {
				r = inner
			}
			a := math.Pi/2 + float64(k)*math.Pi/5
			ring = append(ring, v3{r * math.Cos(a), r * math.Sin(a), 0})
		}
		front, back := v3{0, 0, d + bulge}, v3{0, 0, -d - bulge}
		for k := range ring {
			p, q := ring[k], ring[(k+1)%len(ring)]
			pf, qf := v3{p.x, p.y, d}, v3{q.x, q.y, d}
			pb, qb := v3{p.x, p.y, -d}, v3{q.x, q.y, -d}
			m.add(face{pts: []v3{front, pf, qf}, tone: t}, face{pts: []v3{back, qb, pb}, tone: t * 0.8})
			m.add(face{pts: []v3{pf, pb, qb, qf}, tone: t * 0.7})
		}
	case ckClock:
		m.add(cylZ(v3{}, 0.7, 0.12, 16, t)...)
		m.detail(cylZ(v3{0, 0, 0.13}, 0.6, 0.01, 16, 0.12)...) // la esfera
		for k := range 12 {
			a := float64(k) * math.Pi / 6
			m.detail(box(v3{0.5 * math.Sin(a), 0.5 * math.Cos(a), 0.15}, v3{0.025, 0.025, 0.01}, 1.2)...)
		}
		m.detail(box(v3{0, 0.17, 0.16}, v3{0.025, 0.17, 0.01}, 1.3)...) // el minutero
		m.detail(box(v3{0.12, 0, 0.17}, v3{0.12, 0.025, 0.01}, 1.3)...) // la horaria
		m.add(cylY(v3{0, 0.78, 0}, 0.09, 0.06, 8, t*0.8)...)            // la corona
		m.add(box(v3{-0.42, -0.72, 0}, v3{0.08, 0.06, 0.06}, t*0.7)...)
		m.add(box(v3{0.42, -0.72, 0}, v3{0.08, 0.06, 0.06}, t*0.7)...) // las patas
	case ckTrophy:
		m.add(cylY(v3{0, 0.25, 0}, 0.45, 0.35, 12, t)...)    // la copa
		m.add(cylY(v3{0, -0.2, 0}, 0.08, 0.15, 8, t*0.8)...) // el tallo
		m.add(cylY(v3{0, -0.4, 0}, 0.25, 0.05, 10, t)...)    // el pie
		m.add(box(v3{0, -0.6, 0}, v3{0.42, 0.15, 0.3}, t*0.6)...)
		m.detail(box(v3{0, -0.6, 0.31}, v3{0.25, 0.07, 0.01}, 1.3)...) // la placa
		for _, sx := range []float64{-1, 1} {
			m.add(box(v3{sx * 0.55, 0.38, 0}, v3{0.06, 0.16, 0.05}, t*0.8)...) // las asas
			m.add(box(v3{sx * 0.5, 0.52, 0}, v3{0.08, 0.04, 0.05}, t*0.8)...)
			m.add(box(v3{sx * 0.5, 0.24, 0}, v3{0.08, 0.04, 0.05}, t*0.8)...)
		}
	case ckVisor:
		m.add(box(v3{0, 0.35, 0}, v3{l.w / 2, l.h / 2 * 0.6, 0.35}, t)...)
		m.detail(box(v3{0, 0.35, 0.36}, v3{l.w * 0.4, 0.12, 0.02}, dark)...)
		m.add(box(v3{-0.3, -0.25, 0}, v3{0.04, 0.4, 0.04}, t*0.8)...)
		m.add(box(v3{0.3, -0.25, 0}, v3{0.04, 0.4, 0.04}, t*0.8)...)
		m.add(box(v3{0, -0.67, 0}, v3{0.5, 0.03, 0.35}, t*0.8)...)
		// Sin pantalla a la vista: la imagen va en un cartel delante.
		m.screenImage(v3{0, -0.2, 0.45}, 0.3, 0.22, l.ar)
	}
	m.spinFace, m.spinTex = make([]bool, len(m.faces)), make([]bool, len(m.tex))
	for _, i := range m.spin {
		m.spinFace[i] = true
	}
	for _, i := range m.spinT {
		m.spinTex[i] = true
	}
	// El radio, para encuadrar (el disco, ya en su sitio).
	var lo, hiP v3
	first := true
	for i, f := range m.faces {
		for _, p := range f.pts {
			if m.spinFace[i] {
				p = rotX(p, m.spinTilt).add(m.spinCenter)
			}
			if first {
				lo, hiP, first = p, p, false
				continue
			}
			lo = v3{math.Min(lo.x, p.x), math.Min(lo.y, p.y), math.Min(lo.z, p.z)}
			hiP = v3{math.Max(hiP.x, p.x), math.Max(hiP.y, p.y), math.Max(hiP.z, p.z)}
		}
	}
	m.center = lo.add(hiP).mul(0.5)
	half := hiP.sub(lo).mul(0.5)
	m.radius = math.Sqrt(half.dot(half))
	return m
}

// consoleView es el estado del dibujo de las maquetas: el lienzo, las
// imágenes de paso y las maquetas ya armadas.
type consoleView struct {
	imgFull *ebiten.Image // lo pintado (se usa su esquina, ver drawModel)
	raster  swRaster      // el lienzo con profundidad (raster.go)
	pts     []v3          // los vértices de una cara, de paso
	last    modelKey      // lo que tiene dibujado outFull (para no repetirlo)
	outFull *ebiten.Image // en la paleta
	models  map[string]*consoleModel
	verts   []ebiten.Vertex
	idx     []uint16
}

// model es la maqueta del sistema (armada la primera vez).
func (cv *consoleView) model(name, hardware string) *consoleModel {
	if cv.models == nil {
		cv.models = map[string]*consoleModel{}
	}
	if m, ok := cv.models[name]; ok {
		return m
	}
	m := glbModel(name)
	if m == nil {
		m = buildConsole(lookFor(name, hardware))
	}
	cv.models[name] = m
	return m
}

// drawConsole pinta la maqueta del sistema name en dst, en (x, y) y de w×h,
// girando según t; spin (0..1) es una vuelta entera que hace al cambiar de
// sistema o de juego. cover, si está, es la imagen del juego.
func (a *App) drawConsole(dst *ebiten.Image, cv *consoleView, name, hardware string, x, y, w, h int, spin float64, cover *ebiten.Image, coverAlpha float32) {
	a.drawConsoleA(dst, cv, name, hardware, x, y, w, h, spin, cover, coverAlpha, 1)
}

// drawConsoleA es drawConsole con la maqueta entera más tenue (alpha < 1:
// las del fondo del carrusel).
func (a *App) drawConsoleA(dst *ebiten.Image, cv *consoleView, name, hardware string, x, y, w, h int, spin float64, cover *ebiten.Image, coverAlpha, alpha float32) {
	if w < 8 || h < 8 {
		return
	}
	a.drawModel(dst, cv, cv.model(name, hardware), x, y, w, h, spin, cover, coverAlpha, alpha, alpha < 1)
}

// drawModel pinta la maqueta m (ver drawConsole).
func (a *App) drawModel(dst *ebiten.Image, cv *consoleView, m *consoleModel, x, y, w, h int, spin float64, cover *ebiten.Image, coverAlpha, alpha float32, still bool) {
	out := a.renderModel(cv, m, w, h, spin, cover, coverAlpha, still)
	dop := &ebiten.DrawImageOptions{}
	dop.GeoM.Translate(float64(x), float64(y))
	dop.ColorScale.ScaleAlpha(alpha)
	dst.DrawImage(out, dop)
}

// renderModel dibuja la maqueta m de w×h en el lienzo de cv y lo devuelve
// (vale hasta el siguiente dibujo con el mismo cv). Si nada cambió desde el
// anterior, devuelve el mismo sin volver a dibujarlo.
func (a *App) renderModel(cv *consoleView, m *consoleModel, w, h int, spin float64, cover *ebiten.Image, coverAlpha float32, still bool) *ebiten.Image {
	// El vaivén va a 30 cuadros por segundo (el giro al cambiar, a 60): una
	// maqueta tiene miles de triángulos, y redibujarla en cada fotograma
	// para un movimiento tan lento se llevaba media CPU. Las de los lados
	// del carrusel (still) no se mecen.
	t := math.Floor(a.t*30) / 30
	if spin > 0 && spin < 1 {
		t = a.t
	}
	yaw := 0.5*math.Sin(t*0.45) + 2*math.Pi*easeOutCubic(spin)
	bob := 0.03 * math.Sin(t*1.7)
	pitch := 0.0
	if still {
		yaw, bob, t = 0.35, 0, 0 // quietas, también el disco
	} else if a.gm.on {
		yaw += a.gm.spinYaw // el giro a mano (updateFreeSpin)
		pitch = a.gm.spinPitch
	}
	ps, pc := math.Sincos(pitch)
	// Si nada cambió desde el último dibujo, se usa el mismo.
	key := modelKey{m: m, w: w, h: h, yaw: yaw, pitch: pitch, bob: bob, cover: cover, coverAlpha: coverAlpha, disc: math.Floor(t*30) * float64(len(m.spin)),
		pal: [3]color.RGBA{a.theme.Bg2, a.theme.Accent, a.theme.Text}}
	if cv.last == key && cv.outFull != nil {
		return cv.outFull.SubImage(image.Rect(0, 0, w, h)).(*ebiten.Image)
	}
	cv.last = key
	// Las imágenes de paso solo crecen (y se usa su esquina de w×h): las
	// maquetas del carrusel cambian de tamaño en cada fotograma al
	// deslizarse, y crearlas de nuevo cada vez daba tirones.
	growImg(&cv.imgFull, w, h)
	growImg(&cv.outFull, w, h)
	area := image.Rect(0, 0, w, h)
	img := cv.imgFull.SubImage(area).(*ebiten.Image)
	out := cv.outFull.SubImage(area).(*ebiten.Image)
	img.Clear()

	dist := m.radius * 3.1
	focal := float64(h) * 1.25
	camPos := v3{0, m.radius * 0.55, dist}
	cam := consoleCam(camPos, v3{0, -m.radius * 0.05, 0}, focal, float64(w), float64(h))
	discA := t * 1.3
	// La maqueta gira alrededor de su centro (no se mueve la cámara). Los
	// senos y cosenos, una vez por dibujo (no por punto).
	ys, yc := math.Sincos(yaw)
	ds, dc := math.Sincos(discA)
	ts, tc := math.Sincos(m.spinTilt)
	xf := func(p v3, spinning bool) v3 {
		if spinning {
			p = v3{p.x*dc + p.z*ds, p.y, -p.x*ds + p.z*dc}                  // rotY(discA)
			p = v3{p.x, p.y*tc - p.z*ts, p.y*ts + p.z*tc}.add(m.spinCenter) // rotX(spinTilt)
		}
		p = p.sub(m.center)
		p.y += bob
		p = v3{p.x*yc + p.z*ys, p.y, -p.x*ys + p.z*yc}   // rotY(yaw)
		return v3{p.x, p.y*pc - p.z*ps, p.y*ps + p.z*pc} // rotX(pitch)
	}
	// Las caras, al lienzo con profundidad (raster.go): de espaldas no se
	// pintan (salvo en los modelos de dos caras); la luz, por cara.
	rs := &cv.raster
	rs.reset(w, h)
	for i, f := range m.faces {
		if len(f.pts) < 3 {
			continue
		}
		if cap(cv.pts) < len(f.pts) {
			cv.pts = make([]v3, len(f.pts))
		}
		pts := cv.pts[:len(f.pts)] // las caras redondas (discos) tienen muchos vértices
		for k, p := range f.pts {
			pts[k] = xf(p, m.spinFace[i])
		}
		n := pts[1].sub(pts[0]).cross(pts[2].sub(pts[0])).norm()
		if n.dot(pts[0].sub(cam.pos)) >= 0 {
			if !m.twoSided {
				continue
			}
			n = n.mul(-1)
		}
		lit := math.Max(0, n.dot(light))
		var t rTri
		if m.col != nil {
			// A color: el de su material y su textura, sin caras negras.
			cl := float32(0.45 + 0.55*lit)
			c := m.col[i]
			t.col = [3]float32{c[0] * cl, c[1] * cl, c[2] * cl}
			t.lift = true
			if m.uv[i] != nil {
				t.uv, t.tex = m.uv[i], m.atlasSrc
			}
		} else {
			// En intensidad, para la paleta del tema (consoleShader).
			tone := f.tone
			if tone <= 1.25 {
				tone *= 0.3 + 0.7*lit
			}
			t.col = [3]float32{float32(math.Min(1.5, tone) / 1.5), 0, 0}
		}
		for k := 1; k+1 < len(f.pts); k++ {
			t.p = [3]v3{cam.view(pts[0]), cam.view(pts[k]), cam.view(pts[k+1])}
			if t.uv != nil {
				t.uv = m.uv[i] // los modelos son de triángulos: una sola vez
			}
			rs.draw(&t, &cam)
		}
	}
	img.WritePixels(rs.pix)

	th := a.theme
	out.Clear()
	if m.col != nil {
		// A color (con sus texturas): tal cual, pixelado.
		out.DrawImage(img, nil)
	} else {
		op := &ebiten.DrawRectShaderOptions{}
		op.Images[0] = img
		op.Uniforms = map[string]any{"Bg": vec3(th.Bg2), "Mid": vec3(th.Accent), "Hi": vec3(th.Text)}
		out.DrawRectShader(w, h, consoleShader, op)
	}

	// La imagen del juego, encima, en cada sitio que mire a la cámara.
	if cover != nil && coverAlpha > 0 {
		cw, ch := float32(cover.Bounds().Dx()), float32(cover.Bounds().Dy())
		for ti, tp := range m.tex {
			s := m.spinTex[ti]
			p0 := xf(tp.pts[0], s)
			nrm := xf(tp.pts[0].add(tp.n), s).sub(p0)
			if nrm.dot(p0.sub(cam.pos)) >= 0 {
				continue // de espaldas
			}
			// En una malla de gmTess×gmTess: con dos triángulos la imagen se
			// doblaba por la diagonal al girar (sin corrección de perspectiva,
			// como en la PlayStation).
			if !cv.texMesh(&cam, tp, func(p v3) v3 { return xf(p, s) }, cw, ch, coverAlpha) {
				continue
			}
			// El brillo: una franja de luz cruza la imagen mientras gira de
			// cara a la cámara (con su ángulo de lado a lado).
			side := nrm.norm().dot(cam.r)
			sop := &ebiten.DrawTrianglesShaderOptions{Images: [4]*ebiten.Image{cover}}
			sop.Uniforms = map[string]any{"Progress": float32(0.5 - side*1.4)}
			out.DrawTrianglesShader(cv.verts, cv.idx, shineShader, sop)
		}
	}
	return out
}

// gmTess es en cuántas filas y columnas se parte la imagen de una cara.
const gmTess = 8

// texMesh arma en cv.verts y cv.idx la malla de la imagen tp (un
// cuadrilátero: arriba a la izquierda, arriba a la derecha, abajo a la
// derecha, abajo a la izquierda) ya en la cámara; xf la coloca. Devuelve
// false si algún punto queda detrás de la cámara.
func (cv *consoleView) texMesh(cam *drCam, tp texPoly, xf func(v3) v3, cw, ch, alpha float32) bool {
	cv.verts, cv.idx = cv.verts[:0], cv.idx[:0]
	if len(tp.pts) != 4 {
		return false
	}
	lerpUV := func(a, b [2]float64, t float64) [2]float64 {
		return [2]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}
	}
	for r := 0; r <= gmTess; r++ {
		fv := float64(r) / gmTess
		pl, pr := lerp3(tp.pts[0], tp.pts[3], fv), lerp3(tp.pts[1], tp.pts[2], fv)
		ul, ur := lerpUV(tp.uv[0], tp.uv[3], fv), lerpUV(tp.uv[1], tp.uv[2], fv)
		for c := 0; c <= gmTess; c++ {
			fu := float64(c) / gmTess
			v := cam.view(xf(lerp3(pl, pr, fu)))
			if v.z < 0.3 {
				return false
			}
			uv := lerpUV(ul, ur, fu)
			cv.verts = append(cv.verts, ebiten.Vertex{
				DstX: float32(cam.w/2 + cam.focal*v.x/v.z), DstY: float32(cam.h/2 - cam.focal*v.y/v.z),
				SrcX: float32(uv[0]) * cw, SrcY: float32(uv[1]) * ch,
				ColorR: alpha, ColorG: alpha, ColorB: alpha, ColorA: alpha,
			})
		}
	}
	const n = gmTess + 1
	for r := 0; r < gmTess; r++ {
		for c := 0; c < gmTess; c++ {
			i := uint16(r*n + c)
			cv.idx = append(cv.idx, i, i+1, i+n+1, i, i+n+1, i+n)
		}
	}
	return true
}

// growImg deja *img de al menos w×h (la agranda de a 32 píxeles, nunca la
// achica).
func growImg(img **ebiten.Image, w, h int) {
	if *img != nil && (*img).Bounds().Dx() >= w && (*img).Bounds().Dy() >= h {
		return
	}
	nw, nh := (w+31)/32*32, (h+31)/32*32
	if *img != nil {
		nw, nh = max(nw, (*img).Bounds().Dx()), max(nh, (*img).Bounds().Dy())
		(*img).Deallocate()
	}
	*img = newTarget(nw, nh)
}

// modelKey es lo que decide un dibujo de una maqueta: si no cambia, se usa
// el anterior.
type modelKey struct {
	m          *consoleModel
	w, h       int
	yaw, bob   float64
	pitch      float64 // el giro a mano hacia arriba o abajo
	cover      *ebiten.Image
	coverAlpha float32
	disc       float64       // el giro del disco (las que tienen uno)
	pal        [3]color.RGBA // los colores del tema (cambian con el disco)
}

// consoleCam es lookAt para las maquetas: la de Derrape (que mira hacia +z)
// deja la derecha en -x, y aquí la cámara mira hacia -z: con sus ejes, las
// consolas salían en espejo (la cruceta a la derecha).
func consoleCam(pos, target v3, focal, w, h float64) drCam {
	f := target.sub(pos).norm()
	r := f.cross(v3{0, 1, 0}).norm()
	u := r.cross(f)
	return drCam{pos, r, u, f, focal, w, h}
}

// easeOutCubic va de 0 a 1 frenando al final (y se queda en 0 fuera de 0..1).
func easeOutCubic(k float64) float64 {
	if k <= 0 || k >= 1 {
		return 0
	}
	return 1 - (1-k)*(1-k)*(1-k)
}
