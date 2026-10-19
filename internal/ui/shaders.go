package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

// Los shaders evitan sin/cos/atan2 por píxel (en la Mali de las portátiles
// cuestan caro, como se aprendió en Trancazo): las ondas son triangulares.

// bayerKage es la trama de Bayer 4×4 en Kage, de 0 a 1: m2 es la de 2×2
// ([[0,2],[3,1]] = 2x+3y-4xy) y la de 4×4 se compone con ella.
const bayerKage = `
func m2(x, y float) float {
	return 2*x + 3*y - 4*x*y
}

func bayer(p vec2) float {
	x := mod(floor(p.x), 4)
	y := mod(floor(p.y), 4)
	v := 4*m2(mod(x, 2), mod(y, 2)) + m2(floor(x/2), floor(y/2))
	return (v + 0.5) / 16
}

func tri(x float) float {
	return abs(fract(x) - 0.5) * 2
}
`

// bgSrc es el fondo: un degradado diagonal entre el fondo y el color del disco
// que ondula despacio, con trama de Bayer y cuantizado a pocos niveles, para
// que se vea como en una consola de 16 bits y no en bandas.
const bgSrc = `//kage:unit pixels
package main

var Time float
var Base vec3
var Glow vec3
var Glow2 vec3
var Size vec2
var Pump float
var Treble float
var Since float
var Ripple vec2
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	p := dst.xy - imageDstOrigin()
	uv := p / Size
	// Dos manchas de luz que se mueven despacio, con ondas triangulares.
	c1 := vec2(0.15 + 0.3*tri(Time*0.031), 0.2 + 0.2*tri(Time*0.023))
	c2 := vec2(0.85 - 0.3*tri(Time*0.027), 0.8 - 0.2*tri(Time*0.019))
	d1 := length((uv - c1) * vec2(Size.x/Size.y, 1))
	d2 := length((uv - c2) * vec2(Size.x/Size.y, 1))
	// Con la música: Pump (los graves, 0..1) agranda y aviva la primera
	// mancha; Treble (los agudos), la segunda; y en cada golpe sale una onda
	// de la primera (Since: segundos desde el golpe).
	a := clamp(0.55+0.12*Pump-d1, 0, 1) * (0.55 + 0.4*Pump)
	b := clamp(0.6+0.12*Treble-d2, 0, 1) * (0.35 + 0.4*Treble)
	if Since < 1.2 {
		dr := length((uv - Ripple) * vec2(Size.x/Size.y, 1))
		wave := 1 - clamp(abs(dr-Since*0.9)/0.025, 0, 1)
		a += wave * exp(-Since*3.5) * (0.12 + 0.25*Pump)
	}
	col := Base + (Glow-Base)*a + (Glow2-Base)*b
	// Cuantizado a 24 niveles por canal con trama.
	lv := 24.0
	col = floor(col*lv + bayer(p)) / lv
	return vec4(col, 1)
}
`

// shineSrc dibuja una portada con un brillo diagonal que la recorre, para la
// seleccionada. Progress va de 0 a 1 a lo largo de la pasada.
const shineSrc = `//kage:unit pixels
package main

var Progress float
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	c := imageSrc0At(src)
	o := imageSrc0Origin()
	s := imageSrc0Size()
	q := (src - o) / s
	d := (q.x + q.y) * 0.5 - (Progress*1.6 - 0.3)
	band := clamp(1 - abs(d)*9, 0, 1)
	// La luz también va tramada: se enciende por píxeles, no se mezcla.
	if band*0.6 > bayer(src) {
		c.rgb = min(c.rgb + vec3(0.35), vec3(1))
	}
	return c * color.a
}
`

// vinylSrc pinta un vinilo: surcos concéntricos y un reflejo que gira con
// Angle (el reflejo se calcula con un producto escalar, sin atan2).
const vinylSrc = `//kage:unit pixels
package main

var Center vec2
var Radius float
var Dir vec2
var Label vec3
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	p := dst.xy - imageDstOrigin() - Center // dst.xy es del atlas, Center de la imagen
	r := length(p)
	if r > Radius {
		return vec4(0)
	}
	if r > Radius - 1 {
		return vec4(0.02, 0.02, 0.03, 1)
	}
	if r < Radius*0.36 {
		if r < 2 {
			return vec4(0, 0, 0, 1)
		}
		l := Label
		if r > Radius*0.33 {
			l *= 0.6
		}
		return vec4(l, 1)
	}
	base := 0.06 + 0.035*step(0.5, fract(r/3))
	n := p / max(r, 0.001)
	h := max(dot(n, Dir), 0) + max(dot(n, -Dir), 0)
	h = h * h * h * h
	v := base + 0.22*h
	v = floor(v*16 + bayer(dst.xy-imageDstOrigin())) / 16
	return vec4(v, v, v*1.1, 1)
}
`

// fadeSrc dibuja un texto ya pintado con sus bordes desvanecidos: en los
// últimos píxeles el texto se apaga hacia el fondo en ocho escalones de un
// octavo, columna a columna (tan suave como un degradado, pero sin pasos
// intermedios entre píxeles). Las letras se ven enteras, cada vez más
// apagadas; con trama de Bayer se deshacían en puntos sueltos.
const fadeSrc = `//kage:unit pixels
package main

var Width float
var FadeL float
var FadeR float

func Fragment(dst vec4, src vec2, color vec4) vec4 {
	c := imageSrc0At(src)
	x := src.x - imageSrc0Origin().x
	f := 1.0
	if FadeL > 0 {
		f = min(f, (x+0.5)/FadeL)
	}
	if FadeR > 0 {
		f = min(f, (Width-x-0.5)/FadeR)
	}
	// Ocho escalones, de 0 a 7/8, en el borde.
	f = floor(f*8) / 8
	return c * f * color.a
}
`

// dissolveSrc tapa la pantalla de negro píxel a píxel con una trama de Bayer
// de 8×8 (64 niveles: más fina que la de 4×4): con Progress=0 no tapa nada y
// con 1 lo tapa todo. Es el efecto al apagar y encender la pantalla.
const dissolveSrc = `//kage:unit pixels
package main

var Progress float

func m2(x, y float) float {
	return 2*x + 3*y - 4*x*y
}

func bayer8(p vec2) float {
	x := mod(floor(p.x), 8)
	y := mod(floor(p.y), 8)
	v := 16*m2(mod(x, 2), mod(y, 2)) + 4*m2(mod(floor(x/2), 2), mod(floor(y/2), 2)) + m2(floor(x/4), floor(y/4))
	return (v + 0.5) / 64
}

func Fragment(dst vec4, src vec2, color vec4) vec4 {
	if bayer8(dst.xy-imageDstOrigin()) < Progress {
		return vec4(0, 0, 0, 1)
	}
	return vec4(0)
}
`

// rhythmSrc es el fondo de Bloques, al estilo de los juegos de ritmo: anillos
// cuadrados que salen del centro con cada golpe (uno por golpe, separados por
// el tempo), un destello en el golpe, una onda horizontal al quitar líneas y
// líneas de barrido. Cuantizado con trama de Bayer, como el resto. En FEVER
// (Fever = 1) todo sube y el color va y viene entre los dos acentos.
const rhythmSrc = `//kage:unit pixels
package main

var Size vec2
var Center vec2
var Time float
var Since float
var Period float
var Level float
var Fever float
var Base vec3
var ColA vec3
var ColB vec3
var ClearSince float
var ClearY float
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	p := dst.xy - imageDstOrigin()
	// Anillos cuadrados (en píxeles de 2, para que se vean pixel art).
	d := max(abs(p.x-Center.x), abs(p.y-Center.y)*1.25)
	d = floor(d/2) * 2
	per := Period
	if per <= 0 {
		per = 0.5
	}
	speed := 150.0
	k := fract((d - Since*speed) / (per * speed))
	ring := 1 - clamp(k/0.08, 0, 1)
	ring *= clamp(1-d/(Size.x*0.75), 0, 1)
	amp := (0.12 + 0.3*Fever) * (0.4 + Level)
	v := ring * amp
	// El destello del golpe.
	v += exp(-Since*9) * (0.06 + 0.14*Fever)
	// La onda de las líneas quitadas.
	if ClearSince < 1 {
		w := ClearSince * 260
		band := 1 - clamp(abs(abs(p.y-ClearY)-w)/5, 0, 1)
		v += band * exp(-ClearSince*3) * 0.6
	}
	// Líneas de barrido.
	if mod(floor(p.y), 4) < 1 {
		v *= 0.6
	}
	v = clamp(floor(v*6+bayer(p))/6, 0, 1)
	col := mix(ColA, ColB, (0.5+0.5*sin(Time*1.7+d*0.02))*Fever)
	return vec4(Base+(col-Base)*v, 1)
}
`

// paletteSrc pasa los visualizadores (intensidad en el rojo, 0..1 = 0..1,5) a
// la rampa de la paleta: del fondo al acento y, en los brillos, al texto, por
// escalones con trama de Bayer; lo marcado en el verde (el coche) va en bandas
// planas, sin trama, para que resalte. Un solo color, como el resto.
const paletteSrc = `//kage:unit pixels
package main

var Bg vec3
var Mid vec3
var Hi vec3
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	s := imageSrc0At(src)
	v := s.r * 1.5
	p := dst.xy - imageDstOrigin()
	if s.g > 0.5 {
		v = floor(v*5+0.5) / 5 // sólido: bandas planas, sin trama
	} else {
		v = floor(v*5+bayer(p)) / 5
	}
	col := mix(Bg, Mid, clamp(v, 0, 1))
	if v > 1 {
		col = mix(Mid, Hi, clamp((v-1)*2, 0, 1))
	}
	return vec4(col, 1)
}
`

// gbSrc es el modo Game Boy (eggs.go): la luminancia de cada píxel, con trama
// de Bayer, en los cuatro verdes de la pantalla de la Game Boy.
const gbSrc = `//kage:unit pixels
package main
` + bayerKage + `
func Fragment(dst vec4, src vec2, color vec4) vec4 {
	c := imageSrc0At(src).rgb
	l := dot(c, vec3(0.299, 0.587, 0.114))
	p := dst.xy - imageDstOrigin()
	v := clamp(floor(l*3.4+bayer(p)-0.2), 0, 3)
	if v < 0.5 {
		return vec4(0.059, 0.220, 0.059, 1)
	}
	if v < 1.5 {
		return vec4(0.188, 0.384, 0.188, 1)
	}
	if v < 2.5 {
		return vec4(0.545, 0.675, 0.059, 1)
	}
	return vec4(0.608, 0.737, 0.059, 1)
}
`

// plasmaSrc es el plasma del visualizador (en intensidad): sumas de senos que
// se mueven, con los graves que lo agitan, los agudos que lo afinan y una onda
// desde el centro en cada golpe.
const plasmaSrc = `//kage:unit pixels
package main

var Size vec2
var Time float
var Bass float
var Treble float
var Level float
var Since float
var Scale float

func Fragment(dst vec4, src vec2, color vec4) vec4 {
	p := (dst.xy - imageDstOrigin()) / Size.y * 6 * Scale
	t := Time * (0.6 + 0.8*Level)
	v := sin(p.x*0.9+t) + sin(p.y*1.1-t*1.3+Bass*3)
	v += sin((p.x+p.y)*0.7+t*0.7) + sin(length(p-vec2(3*Scale, 3*Scale))*(1.5+Treble*3)-t*2)
	v = v/4*0.5 + 0.5
	// La onda del golpe.
	c := (dst.xy - imageDstOrigin()) - Size/2
	d := length(c) / Size.y
	if Since < 1 {
		v += (1 - clamp(abs(d-Since*0.8)/0.04, 0, 1)) * exp(-Since*3) * 0.6
	}
	v *= 0.5 + 0.6*Level + 0.3*Bass
	return vec4(clamp(v, 0, 1), 0, 0, 1)
}
`

// drawRhythm pinta el fondo de Bloques (ver rhythmSrc).
func drawRhythm(dst *ebiten.Image, th Theme, cx, cy int, t, since, period, level, fever, clearSince float64, clearY int) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"Size": []float32{float32(w), float32(h)}, "Center": []float32{float32(cx), float32(cy)},
		"Time": float32(t), "Since": float32(since), "Period": float32(period),
		"Level": float32(level), "Fever": float32(fever),
		"Base": vec3(th.Bg), "ColA": vec3(th.Accent), "ColB": vec3(th.Accent2),
		"ClearSince": float32(clearSince), "ClearY": float32(clearY),
	}
	dst.DrawRectShader(w, h, rhythmShader, op)
}

// Shaders compilados.
var (
	bgShader       *ebiten.Shader
	shineShader    *ebiten.Shader
	vinylShader    *ebiten.Shader
	fadeShader     *ebiten.Shader
	dissolveShader *ebiten.Shader
	rhythmShader   *ebiten.Shader
	paletteShader  *ebiten.Shader
	gbShader       *ebiten.Shader
	plasmaShader   *ebiten.Shader
)

// LoadShaders compila los shaders de la interfaz.
func LoadShaders() error {
	var err error
	for _, s := range []struct {
		dst **ebiten.Shader
		src string
	}{{&bgShader, bgSrc}, {&shineShader, shineSrc}, {&vinylShader, vinylSrc}, {&fadeShader, fadeSrc}, {&dissolveShader, dissolveSrc}, {&rhythmShader, rhythmSrc}, {&paletteShader, paletteSrc}, {&plasmaShader, plasmaSrc}, {&gbShader, gbSrc}} {
		if *s.dst, err = ebiten.NewShader([]byte(s.src)); err != nil {
			return err
		}
	}
	return nil
}

func vec3(c color.RGBA) []float32 {
	return []float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255}
}

// drawBackground pinta el fondo animado del tema.
// bgPump y bgTreble son cuánto respira el fondo con los graves y con los
// agudos (0..1), y bgSince los segundos desde el último golpe (para la onda);
// los pone la interfaz en cada fotograma (App.updatePump).
var bgPump, bgTreble float32
var bgSince float32 = 9

// bgRipple es de dónde sale la onda del último golpe (0..1 de la pantalla), y
// bgRippleBeat, de qué golpe es.
var (
	bgRipple     = [2]float32{0.3, 0.3}
	bgRippleBeat int
)

func drawBackground(dst *ebiten.Image, th Theme, t float64) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"Time":   float32(t),
		"Base":   vec3(th.Bg),
		"Glow":   vec3(mix(th.Bg, th.Accent, 0.55)),
		"Glow2":  vec3(mix(th.Bg, th.Accent2, 0.45)),
		"Size":   []float32{float32(w), float32(h)},
		"Pump":   bgPump,
		"Treble": bgTreble,
		"Since":  bgSince,
		"Ripple": bgRipple[:],
	}
	dst.DrawRectShader(w, h, bgShader, op)
}

// drawDissolve tapa dst de negro con la trama, en la proporción progress (0..1).
func drawDissolve(dst *ebiten.Image, progress float64) {
	if progress <= 0 {
		return
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{"Progress": float32(progress)}
	dst.DrawRectShader(w, h, dissolveShader, op)
}

// drawShine dibuja img en (x, y) con el brillo que la recorre.
func drawShine(dst, img *ebiten.Image, x, y int, progress float64) {
	b := img.Bounds()
	op := &ebiten.DrawRectShaderOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.Images[0] = img
	op.Uniforms = map[string]any{"Progress": float32(progress)}
	dst.DrawRectShader(b.Dx(), b.Dy(), shineShader, op)
}

// drawVinyl pinta un vinilo de radio r con centro en (cx, cy), girado angle.
func drawVinyl(dst *ebiten.Image, cx, cy, r int, angle float64, label color.RGBA) {
	op := &ebiten.DrawRectShaderOptions{}
	op.GeoM.Translate(float64(cx-r), float64(cy-r))
	s, c := sincos(angle)
	op.Uniforms = map[string]any{
		"Center": []float32{float32(cx) + 0.5, float32(cy) + 0.5},
		"Radius": float32(r),
		"Dir":    []float32{float32(c), float32(s)},
		"Label":  vec3(label),
	}
	dst.DrawRectShader(2*r+1, 2*r+1, vinylShader, op)
}
