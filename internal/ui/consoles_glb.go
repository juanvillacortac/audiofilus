package ui

import (
	"embed"
	"image"
	"image/color"
	"log"
	"math"
	"sync"
)

// Las maquetas de verdad: modelos low poly de cada consola (glTF binario,
// de Sketchfab y poly.pizza, con licencia CC-BY; los créditos están en
// models/CREDITS.md), metidos en el binario. Se leen la primera vez que se
// muestran y pasan por el mismo motor y la misma paleta que las maquetas
// hechas a mano (consoles.go), que quedan para los sistemas sin modelo.

//go:embed models/*.glb
var modelFiles embed.FS

// glbSpec es cómo usar el modelo de un sistema: su archivo y cuánto girarlo
// para que mire a la cámara.
type glbSpec struct {
	file string
	yaw  float64 // giro alrededor de y (radianes) para que el frente mire a +z
	// pitch y roll lo enderezan antes (alrededor de x y de z): hay modelos
	// tumbados sobre la mesa.
	pitch, roll float64
	// twoSided pinta también las caras de espaldas (modelos con caras de un
	// solo lado mal orientadas).
	twoSided bool
}

// glbSpecs son los modelos de cada sistema.
var glbSpecs = map[string]glbSpec{
	"gb":           {file: "gb.glb"},
	"gbc":          {file: "gbc.glb", yaw: math.Pi, roll: -0.6},
	"gba":          {file: "gba.glb"},
	"nds":          {file: "nds.glb", yaw: math.Pi / 2},
	"nes":          {file: "nes.glb"},
	"fds":          {file: "nes.glb"},
	"snes":         {file: "snes.glb"},
	"n64":          {file: "n64.glb"},
	"virtualboy":   {file: "virtualboy.glb"},
	"psp":          {file: "psp.glb", pitch: 1.35},
	"psx":          {file: "psx.glb", yaw: math.Pi / 2, twoSided: true},
	"megadrive":    {file: "megadrive.glb", yaw: math.Pi},
	"genesis":      {file: "megadrive.glb", yaw: math.Pi},
	"dreamcast":    {file: "dreamcast.glb"},
	"mastersystem": {file: "mastersystem.glb"},
	"atari2600":    {file: "atari2600.glb"},
	"pico8":        {file: "pico8.glb"},
	"ports":        {file: "pc.glb", yaw: math.Pi},
	"mame":         {file: "arcade.glb", yaw: math.Pi},
	"fbneo":        {file: "arcade.glb", yaw: math.Pi},
	"arcade":       {file: "arcade.glb", yaw: math.Pi},
}

// glbReady son las maquetas ya armadas desde su modelo, por sistema (nil si
// no tiene o no se pudo leer). Se arman en segundo plano al leer los juegos
// (preloadModels): leer el glTF y sus texturas en el hilo de la interfaz
// daba un tirón la primera vez que salía cada consola.
var glbReady sync.Map

// preloadModels arma las maquetas de los sistemas names (en segundo plano).
func preloadModels(names []string) {
	for _, n := range names {
		glbModel(n)
	}
}

// glbModel es la maqueta de un sistema desde su modelo, o nil si no tiene
// (o no se pudo leer: entonces va la hecha a mano). La textura queda en
// atlasSrc: se sube a la GPU al dibujarla (drawModel).
func glbModel(name string) *consoleModel {
	if m, ok := glbReady.Load(name); ok {
		return m.(*consoleModel)
	}
	m := buildGLB(name)
	glbReady.Store(name, m)
	return m
}

// buildGLB arma la maqueta de un sistema desde su modelo.
func buildGLB(name string) *consoleModel {
	spec, ok := glbSpecs[name]
	if !ok {
		return nil
	}
	b, err := modelFiles.ReadFile("models/" + spec.file)
	if err != nil {
		return nil
	}
	g, err := parseGLB(b)
	if err != nil {
		log.Printf("console model %s: %v", spec.file, err)
		return nil
	}
	m := &consoleModel{}

	// Enderezado y girado para que mire a la cámara, centrado y a radio 1.
	for i := range g.tris {
		for k, p := range g.tris[i].p {
			g.tris[i].p[k] = rotY(rotX(rotZ(p, spec.roll), spec.pitch), spec.yaw)
		}
	}
	lo, hi := g.tris[0].p[0], g.tris[0].p[0]
	for _, t := range g.tris {
		for _, p := range t.p {
			lo = v3{math.Min(lo.x, p.x), math.Min(lo.y, p.y), math.Min(lo.z, p.z)}
			hi = v3{math.Max(hi.x, p.x), math.Max(hi.y, p.y), math.Max(hi.z, p.z)}
		}
	}
	c := lo.add(hi).mul(0.5)
	half := hi.sub(lo).mul(0.5)
	scale := 1 / math.Max(1e-6, math.Sqrt(half.dot(half)))

	// El atlas: las texturas una debajo de otra, a color y opacas, y abajo
	// un poco de blanco para lo que no lleva textura (se tiñe con el color
	// de su material).
	aw, ah := 4, 0
	offs := make([]int, len(g.texs))
	for i, t := range g.texs {
		offs[i] = ah
		aw = max(aw, t.Bounds().Dx())
		ah += t.Bounds().Dy()
	}
	whiteY := ah
	ah += 4
	atlas := image.NewRGBA(image.Rect(0, 0, aw, ah))
	for i, t := range g.texs {
		r := t.Bounds()
		for y := 0; y < r.Dy(); y++ {
			for x := 0; x < r.Dx(); x++ {
				c := color.NRGBAModel.Convert(t.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
				atlas.SetRGBA(x, offs[i]+y, color.RGBA{c.R, c.G, c.B, 255})
			}
		}
	}
	for y := whiteY; y < ah; y++ {
		for x := 0; x < aw; x++ {
			atlas.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	m.atlasSrc = atlas

	// Los colores de la maqueta, por superficie, para teñir el tema con la
	// consola (como con la portada de un disco).
	var swatch []color.RGBA
	var weights []float64
	for _, t := range g.tris {
		var pts []v3
		for _, p := range t.p {
			pts = append(pts, p.sub(c).mul(scale))
		}
		col := [3]float32{float32(t.col[0]), float32(t.col[1]), float32(t.col[2])}
		m.col = append(m.col, col)
		sample := color.RGBA{uint8(col[0] * 255), uint8(col[1] * 255), uint8(col[2] * 255), 255}
		f := face{pts: pts}
		var uv *[3][2]float32
		if t.tex >= 0 {
			f.tone = 1.25 * t.tone
			tw, th := float32(g.texs[t.tex].Bounds().Dx()), float32(g.texs[t.tex].Bounds().Dy())
			// Las coordenadas que se salen de 0..1 (textura repetida) se
			// traen dentro, el triángulo entero a la vez.
			fu := float32(math.Floor(float64(min(t.uv[0][0], t.uv[1][0], t.uv[2][0]))))
			fv := float32(math.Floor(float64(min(t.uv[0][1], t.uv[1][1], t.uv[2][1]))))
			var q [3][2]float32
			for k := range 3 {
				u := min(max(t.uv[k][0]-fu, 0), 1)
				v := min(max(t.uv[k][1]-fv, 0), 1)
				q[k] = [2]float32{u * tw, float32(offs[t.tex]) + v*th}
			}
			uv = &q
			// El color de la textura en el centro del triángulo.
			px := atlas.RGBAAt(int((q[0][0]+q[1][0]+q[2][0])/3), int((q[0][1]+q[1][1]+q[2][1])/3))
			sample = color.RGBA{uint8(float32(px.R) * col[0]), uint8(float32(px.G) * col[1]), uint8(float32(px.B) * col[2]), 255}
		} else {
			f.tone = 1.25 * (0.3 + 0.7*t.tone)
		}
		m.faces = append(m.faces, f)
		m.layer = append(m.layer, 1)
		m.uv = append(m.uv, uv)
		swatch = append(swatch, sample)
		cr := pts[1].sub(pts[0]).cross(pts[2].sub(pts[0]))
		weights = append(weights, cr.dot(cr))
	}
	m.accent, m.hasAccent = modelAccent(swatch, weights)
	m.twoSided = spec.twoSided
	m.spinFace, m.spinTex = make([]bool, len(m.faces)), make([]bool, len(m.tex))
	m.radius = 1
	return m
}

// modelAccent son los dos colores de una maqueta para el tema: los de sus
// superficies, cada una tanto como su área, en una imagen que se mira como
// una portada (accentColors).
func modelAccent(sw []color.RGBA, weights []float64) ([2]color.RGBA, bool) {
	var total float64
	for _, w := range weights {
		total += math.Sqrt(w)
	}
	if total == 0 {
		return [2]color.RGBA{}, false
	}
	const n = 1024
	img := image.NewRGBA(image.Rect(0, 0, n, 1))
	x := 0
	for i, c := range sw {
		k := int(math.Round(math.Sqrt(weights[i]) / total * n))
		for ; k > 0 && x < n; k-- {
			img.SetRGBA(x, 0, c)
			x++
		}
	}
	if x == 0 {
		return [2]color.RGBA{}, false
	}
	return accentColors(img.SubImage(image.Rect(0, 0, x, 1))), true
}
