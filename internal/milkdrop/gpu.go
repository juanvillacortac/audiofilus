package milkdrop

import (
	"image"
	"image/color"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
)

// La parte de la GPU: dos shaders de Kage (el warp, que pasa el fotograma
// anterior por la malla, y la composición) y las imágenes que se turnan. Los
// shaders evitan sin/cos por píxel (en la Mali de las portátiles cuestan):
// el warp hace cuatro lecturas (bilineal a mano, que la de Kage es la más
// cercana) y la composición, dos.

// warpSrc pasa el fotograma anterior por la malla: interpolación bilineal,
// textura repetida o con el borde estirado (wrap), el apagado (decay, con un
// poco más para que lo muy oscuro llegue a negro en 8 bits) y, si el preset
// lo pide, el centro un poco más oscuro (darken_center).
const warpSrc = `//kage:unit pixels
package main

// P: decay, wrap, darken_center, lo que se resta
var P vec4

func tap(p vec2) vec3 {
	size := imageSrc0Size()
	if P.y > 0.5 {
		p = mod(p, size)
	} else {
		p = clamp(p, vec2(0.5), size-0.5)
	}
	return imageSrc0UnsafeAt(p + imageSrc0Origin()).rgb
}

func Fragment(dst vec4, src vec2, color vec4) vec4 {
	q := src - imageSrc0Origin() - 0.5
	f := fract(q)
	b := floor(q) + 0.5
	c0 := mix(tap(b), tap(b+vec2(1, 0)), f.x)
	c1 := mix(tap(b+vec2(0, 1)), tap(b+vec2(1, 1)), f.x)
	c := mix(c0, c1, f.y)*P.x - P.w
	if P.z > 0 {
		d := (dst.xy - imageDstOrigin() - imageDstSize()/2) / imageDstSize().y
		k := max(0, 1-(abs(d.x)+abs(d.y))/0.12)
		c *= 1 - 0.12*k*P.z
	}
	return vec4(max(c, vec3(0)), 1)
}
`

// compSrc es la composición: el eco (la imagen otra vez, ampliada y quizá
// volteada, mezclada con echo_alpha), la gamma, el tinte de fShader y los
// efectos (brighten, darken, solarize, invert), que se mezclan con un peso
// para que el cambio de preset sea suave. Al final, si se pide (M.x > 0), los
// colores del disco: la luminancia de la imagen va a un degradado de cuatro
// colores (Q0 → Q1 → Q2 → Q3) y se guarda un poco del color del preset (M.y).
const compSrc = `//kage:unit pixels
package main

// G: gamma, alfa del eco, zoom del eco, orientación del eco
var G vec4
// F: brighten, darken, solarize, invert
var F vec4
// S: cuánto tinte
var S vec4
// Los colores del tinte en las esquinas.
var H0 vec4
var H1 vec4
var H2 vec4
var H3 vec4
// M: cuánto de los colores del disco, cuánto color del preset se guarda
var M vec4
// El degradado de los colores del disco.
var Q0 vec4
var Q1 vec4
var Q2 vec4
var Q3 vec4

func Fragment(dst vec4, src vec2, color vec4) vec4 {
	o := imageSrc0Origin()
	size := imageSrc0Size()
	rel := src - o
	c := imageSrc0UnsafeAt(src).rgb
	if G.y > 0.001 {
		uv := (rel/size - 0.5) / G.z
		if mod(G.w, 2) >= 1 {
			uv.x = -uv.x
		}
		if G.w >= 2 {
			uv.y = -uv.y
		}
		e := imageSrc0At(o + (uv+0.5)*size).rgb
		c = mix(c, e, G.y)
	}
	c = clamp(c*G.x, vec3(0), vec3(1))
	if S.x > 0.001 {
		t := rel / size
		h := mix(mix(H0.rgb, H1.rgb, t.x), mix(H2.rgb, H3.rgb, t.x), t.y)
		c *= mix(vec3(1), h, S.x)
	}
	c = mix(c, sqrt(c), F.x)
	c = mix(c, c*c, F.y)
	c = mix(c, c*(1-c)*4, F.z)
	c = mix(c, 1-c, F.w)
	if M.x > 0.001 {
		l := dot(c, vec3(0.299, 0.587, 0.114))
		g := mix(Q0.rgb, Q1.rgb, clamp(l/0.35, 0, 1))
		g = mix(g, Q2.rgb, clamp((l-0.35)/0.35, 0, 1))
		g = mix(g, Q3.rgb, clamp((l-0.7)/0.3, 0, 1))
		c = mix(c, mix(g, c, M.y), M.x)
	}
	return vec4(c, 1)
}
`

// Los shaders se compilan una vez para todos los visualizadores.
var (
	shaderOnce             sync.Once
	warpShader, compShader *ebiten.Shader
	shaderErr              error
)

// loadShaders compila los shaders (la primera vez).
func loadShaders() error {
	shaderOnce.Do(func() {
		if warpShader, shaderErr = ebiten.NewShader([]byte(warpSrc)); shaderErr != nil {
			return
		}
		compShader, shaderErr = ebiten.NewShader([]byte(compSrc))
	})
	return shaderErr
}

// gpuState son las imágenes y las opciones de dibujo (que se reusan).
type gpuState struct {
	resized bool
	fb      [2]*ebiten.Image
	cur     int // la que tiene el último fotograma
	out     *ebiten.Image
	white   *ebiten.Image
	whiteIn *ebiten.Image

	warpOp ebiten.DrawTrianglesShaderOptions
	compOp ebiten.DrawRectShaderOptions
	triOp  ebiten.DrawTrianglesOptions

	uP, uG, uF, uS, uM []float32
	uH, uQ             [4][]float32
}

// prepare crea lo que falte (o lo rehace si cambió el tamaño).
func (r *Renderer) prepare() error {
	if err := loadShaders(); err != nil {
		return err
	}
	g := &r.gpu
	if g.white == nil {
		g.white = ebiten.NewImage(3, 3)
		g.white.Fill(color.White)
		g.whiteIn = g.white.SubImage(image.Rect(1, 1, 2, 2)).(*ebiten.Image)
		g.uP, g.uG, g.uF, g.uS, g.uM = make([]float32, 4), make([]float32, 4), make([]float32, 4), make([]float32, 4), make([]float32, 4)
		g.warpOp.Uniforms = map[string]any{"P": g.uP}
		g.compOp.Uniforms = map[string]any{"G": g.uG, "F": g.uF, "S": g.uS, "M": g.uM}
		for i := range g.uH {
			g.uH[i], g.uQ[i] = make([]float32, 4), make([]float32, 4)
			g.compOp.Uniforms["H"+string(rune('0'+i))] = g.uH[i]
			g.compOp.Uniforms["Q"+string(rune('0'+i))] = g.uQ[i]
		}
	}
	if g.resized || g.out == nil {
		for i := range g.fb {
			if g.fb[i] != nil {
				g.fb[i].Deallocate()
			}
			g.fb[i] = ebiten.NewImage(r.w, r.h)
		}
		if g.out != nil {
			g.out.Deallocate()
		}
		g.out = ebiten.NewImage(r.w, r.h)
		g.resized = false
	}
	return nil
}

// Draw pinta el fotograma que calculó Update y devuelve la imagen que se ve
// (de la resolución interna; es del visualizador: se reusa en el siguiente).
func (r *Renderer) Draw() (*ebiten.Image, error) {
	if err := r.prepare(); err != nil {
		return nil, err
	}
	g := &r.gpu
	prev, next := g.fb[g.cur], g.fb[1-g.cur]
	v := &r.comp

	// 1. El anterior, por la malla y apagándose.
	bias := float32(0)
	if v[oDecay] < 0.9995 {
		bias = 0.6 / 255
	}
	g.uP[0], g.uP[1], g.uP[2], g.uP[3] = float32(v[oDecay]), float32(v[oWrap]), float32(clamp(v[oDarkenCenter], 0, 1)), bias
	g.warpOp.Images[0] = prev
	g.warpOp.Blend = ebiten.BlendCopy
	next.DrawTrianglesShader(r.warpVerts, r.warpIdx, warpShader, &g.warpOp)

	// 2. Encima, lo que pinta el preset.
	for _, c := range r.geo.cmds {
		if c.i1 == c.i0 {
			continue
		}
		src := g.whiteIn
		g.triOp.Filter = ebiten.FilterNearest
		g.triOp.Address = ebiten.AddressUnsafe
		if c.textured {
			src = prev
			g.triOp.Filter = ebiten.FilterLinear
			g.triOp.Address = ebiten.AddressClampToZero
		}
		g.triOp.Blend = ebiten.BlendSourceOver
		if c.additive {
			g.triOp.Blend = ebiten.BlendLighter
		}
		next.DrawTriangles(r.geo.verts[c.v0:c.v1], r.geo.idx[c.i0:c.i1], src, &g.triOp)
	}

	// 3. La composición.
	g.uG[0], g.uG[1], g.uG[2], g.uG[3] = float32(clamp(v[oGamma], 0, 8)), float32(clamp(v[oEchoAlpha], 0, 1)), float32(v[oEchoZoom]), float32(clamp(v[oEchoOrient], 0, 3))
	g.uF[0], g.uF[1], g.uF[2], g.uF[3] = float32(clamp(v[oBrighten], 0, 1)), float32(clamp(v[oDarken], 0, 1)), float32(clamp(v[oSolarize], 0, 1)), float32(clamp(v[oInvert], 0, 1))
	g.uS[0] = float32(clamp(v[oShader], 0, 1))
	for i := range g.uH {
		copy(g.uH[i], r.hue[i][:])
		copy(g.uQ[i], r.palette[i][:])
	}
	g.uM[0], g.uM[1] = r.paletteMix, paletteKeep
	g.compOp.Images[0] = next
	g.compOp.Blend = ebiten.BlendCopy
	g.out.DrawRectShader(r.w, r.h, compShader, &g.compOp)
	g.cur = 1 - g.cur
	return g.out, nil
}

// Clear borra la realimentación (negro).
func (r *Renderer) Clear() {
	for _, f := range r.gpu.fb {
		if f != nil {
			f.Clear()
		}
	}
}

// paletteKeep es cuánto color del preset se guarda con los colores del disco
// (para que no quede plano).
const paletteKeep = 0.15

// SetPalette tiñe la imagen con un degradado de cuatro colores (de lo más
// oscuro a lo más claro, RGB de 0 a 1) en la proporción amount (0: los
// colores del preset; 1: los del degradado, con un poco del preset).
func (r *Renderer) SetPalette(stops [4][3]float32, amount float64) {
	r.palette = stops
	r.paletteMix = float32(clamp(amount, 0, 1))
}
