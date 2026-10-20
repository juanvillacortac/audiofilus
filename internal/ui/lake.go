package ui

import (
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Lago: un visualizador en 3D de un lago de noche (motor r3d de drift.go). El
// agua ondula con los graves y cada golpe deja ondas que se abren; los
// nenúfares flotan y las flores de loto abren y cierran los pétalos con los
// medios. Las ranas saltan de hoja en hoja con el bombo, los patos nadan en
// círculo y meten la cabeza con la caja, los peces saltan con los platillos,
// las libélulas revolotean con los agudos y las luciérnagas parpadean con el
// charles. En la orilla, juncos que se mecen. Un solo color, como el resto.

const lkRadius = 16.0 // radio del agua

// lkRipple es una onda que se abre desde donde cayó algo (o un golpe).
type lkRipple struct {
	x, z, at float64
	amp      float64
}

// lkPad es una hoja de nenúfar; las que tienen flor la llevan en el centro.
type lkPad struct {
	x, z, r, rot float64
	flower       bool
}

// lkFrog es una rana sobre una hoja, o saltando a otra.
type lkFrog struct {
	pad, to int
	jumpAt  float64 // cuándo empezó el salto (0: quieta)
}

// lkDuck es un pato que nada en círculo.
type lkDuck struct {
	cx, cz, r, ang, speed float64
	dipAt                 float64
}

// lkFish es un pez saltando.
type lkFish struct {
	x0, z0, x1, z1, at float64
}

// lakeState es el visualizador.
type lakeState struct {
	r3d
	pads                                    []lkPad
	frogs                                   []lkFrog
	ducks                                   []lkDuck
	fish                                    []lkFish
	ripples                                 []lkRipple
	reeds                                   [][3]float64 // ángulo, altura, fase
	flies                                   [][3]float64 // luciérnagas: ángulo, radio, altura
	waves                                   []lkWave
	swell, wt                               float64
	grid                                    []v3      // la malla del agua, punto a punto
	ring                                    []float64 // alturas de un anillo de onda
	cam                                     int
	camAng                                  float64
	open                                    float64 // lo abiertas que están las flores
	seenKick, seenSnare, seenCrash, seenHat float64
	blink                                   float64
	rng                                     *rand.Rand
}

func newLake() *lakeState {
	l := &lakeState{rng: rand.New(rand.NewPCG(9, 2))}
	l.white = newWhite()
	for len(l.pads) < 14 {
		ang, r := l.rng.Float64()*2*math.Pi, 2+l.rng.Float64()*(lkRadius-4)
		x, z := r*math.Cos(ang), r*math.Sin(ang)
		ok := true
		for _, p := range l.pads {
			if math.Hypot(p.x-x, p.z-z) < 2.4 {
				ok = false
			}
		}
		if ok {
			l.pads = append(l.pads, lkPad{x, z, 0.8 + 0.4*l.rng.Float64(), l.rng.Float64() * 6, len(l.pads)%3 == 0})
		}
	}
	for i := 0; i < 3; i++ {
		l.frogs = append(l.frogs, lkFrog{pad: 1 + i*4})
	}
	for i := 0; i < 2; i++ {
		l.ducks = append(l.ducks, lkDuck{cx: float64(i*6 - 3), cz: float64(3 - i*5), r: 4 + float64(i)*2, ang: float64(i) * 3, speed: 0.25 - 0.1*float64(i)})
	}
	for i := 0; i < 90; i++ {
		l.reeds = append(l.reeds, [3]float64{l.rng.Float64() * 2 * math.Pi, 1.2 + 2*l.rng.Float64(), l.rng.Float64() * 6})
	}
	for i := 0; i < 24; i++ {
		l.flies = append(l.flies, [3]float64{l.rng.Float64() * 2 * math.Pi, lkRadius + 1 + 3*l.rng.Float64(), 0.8 + 2*l.rng.Float64()})
	}
	return l
}

// lkWave es una onda lista para height: lo que no depende del punto se
// calcula una vez por fotograma (prepareWaves), no una por vértice.
type lkWave struct {
	x, z  float64
	front float64 // radio del frente
	gain  float64 // amplitud, ya apagada con la edad
}

// lkWaveTail es hasta dónde detrás del frente se nota una onda: más allá,
// exp(-0,25·d) es menos de un 5 % y no se ve.
const lkWaveTail = 12.0

// prepareWaves deja en l.waves las ondas del instante t que todavía se notan.
func (l *lakeState) prepareWaves(t, bass float64) {
	l.waves = l.waves[:0]
	for _, r := range l.ripples {
		age := t - r.at
		if age < 0 {
			continue
		}
		gain := 1.6 * r.amp * math.Exp(-age*1.2)
		if gain < 0.005 {
			continue
		}
		l.waves = append(l.waves, lkWave{r.x, r.z, age * 5, gain})
	}
	l.swell = 0.6 + 1.8*bass
	l.wt = t
}

// height es la altura del agua en (x, z) en el instante de prepareWaves.
func (l *lakeState) height(x, z float64) float64 {
	t := l.wt
	h := (0.06*math.Sin(0.55*x+1.3*t) + 0.06*math.Sin(0.6*z-1.1*t) + 0.035*math.Sin(0.3*(x+z)+0.7*t)) * l.swell
	for _, w := range l.waves {
		dx, dz := x-w.x, z-w.z
		d2 := dx*dx + dz*dz
		if d2 > w.front*w.front {
			continue
		}
		d := math.Sqrt(d2)
		behind := w.front - d
		if behind > lkWaveTail {
			continue
		}
		h += w.gain * math.Sin(-2.2*behind) * math.Exp(-behind*0.25)
	}
	return h
}

// stepLake mueve el lago dt segundos, con la música.
func (a *App) stepLake(dt float64) {
	l := a.lake
	level, _, bands, _ := a.meterNow()
	t := a.t
	dr := &a.beat.drums
	// La onda de cada golpe, en un sitio del lago.
	if a.beatHit && a.hearing() {
		ang, r := l.rng.Float64()*2*math.Pi, l.rng.Float64()*(lkRadius-3)
		l.ripples = append(l.ripples, lkRipple{r * math.Cos(ang), r * math.Sin(ang), t, 0.12 + 0.2*level})
	}
	// El bombo hace saltar una rana (la que esté quieta) a otra hoja.
	kick := a.beatHit && !dr.any
	if dr.last[drumKick] != l.seenKick {
		l.seenKick, kick = dr.last[drumKick], true
	}
	if kick && a.hearing() {
		for i := range l.frogs {
			f := &l.frogs[i]
			if f.jumpAt == 0 {
				f.to = (f.pad + 1 + l.rng.IntN(len(l.pads)-1)) % len(l.pads)
				f.jumpAt = t
				break
			}
		}
	}
	for i := range l.frogs {
		f := &l.frogs[i]
		if f.jumpAt > 0 && t-f.jumpAt > 0.7 {
			f.pad, f.jumpAt = f.to, 0
			p := l.pads[f.pad]
			l.ripples = append(l.ripples, lkRipple{p.x, p.z, t, 0.1})
		}
	}
	// La caja: los patos meten la cabeza.
	if dr.last[drumSnare] != l.seenSnare {
		l.seenSnare = dr.last[drumSnare]
		d := &l.ducks[l.rng.IntN(len(l.ducks))]
		d.dipAt = t
	}
	for i := range l.ducks {
		l.ducks[i].ang += l.ducks[i].speed * dt * (0.5 + level)
	}
	// Los platillos: salta un pez.
	if dr.last[drumCrash] != l.seenCrash || (a.beatHit && !dr.any && l.rng.IntN(6) == 0) {
		l.seenCrash = dr.last[drumCrash]
		ang, r := l.rng.Float64()*2*math.Pi, 2+l.rng.Float64()*(lkRadius-6)
		x0, z0 := r*math.Cos(ang), r*math.Sin(ang)
		da := l.rng.Float64() * 2 * math.Pi
		l.fish = append(l.fish, lkFish{x0, z0, x0 + 3*math.Cos(da), z0 + 3*math.Sin(da), t})
		l.ripples = append(l.ripples, lkRipple{x0, z0, t, 0.15})
	}
	keptF := l.fish[:0]
	for _, f := range l.fish {
		if t-f.at < 0.9 {
			keptF = append(keptF, f)
		} else {
			l.ripples = append(l.ripples, lkRipple{f.x1, f.z1, t, 0.18})
		}
	}
	l.fish = keptF
	// El charles: las luciérnagas parpadean.
	if dr.last[drumHat] != l.seenHat {
		l.seenHat, l.blink = dr.last[drumHat], 1
	}
	l.blink = math.Max(0, l.blink-dt*5)
	// Las flores siguen a los medios.
	mid := 0.0
	for k := 4; k < 10; k++ {
		mid += bands[k] / 6
	}
	l.open += (math.Min(1, mid*1.6) - l.open) * math.Min(1, 4*dt)
	keptR := l.ripples[:0]
	for _, r := range l.ripples {
		if t-r.at < 4 {
			keptR = append(keptR, r)
		}
	}
	l.ripples = keptR
	l.camAng += dt * 0.06
}

// drawLakeScene pinta el lago en intensidad en img.
func (a *App) drawLakeScene(img *ebiten.Image) {
	if a.lake == nil {
		a.lake = newLake()
	}
	l := a.lake
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	level, bass, bands, _ := a.meterNow()
	t := a.t
	pulse := a.beatPulse()
	var cam drCam
	focal := float64(lh) * 1.1
	switch l.cam {
	case 1: // a ras del agua
		p := v3{math.Cos(l.camAng) * (lkRadius + 2), 1.2, math.Sin(l.camAng) * (lkRadius + 2)}
		cam = lookAt(p, v3{0, 0.5, 0}, focal, float64(lw), float64(lh))
	case 2: // desde arriba
		p := v3{math.Cos(l.camAng) * 8, 30, math.Sin(l.camAng) * 8}
		cam = lookAt(p, v3{}, focal, float64(lw), float64(lh))
	default: // en órbita
		p := v3{math.Cos(l.camAng) * 19, 6.5, math.Sin(l.camAng) * 19}
		cam = lookAt(p, v3{0, 0, 0}, focal, float64(lw), float64(lh))
	}

	// El cielo: degradado por bandas, estrellas y la luna, que late con los graves.
	horizon := lh * 30 / 100
	if l.cam == 1 {
		horizon = lh * 48 / 100
	}
	for y := 0; y < horizon; y += 2 {
		a.batch.add(0, y, lw, 2, gray(0.04+0.14*float64(y)/float64(horizon)))
	}
	r := rand.New(rand.NewPCG(1, 1))
	for i := 0; i < 50; i++ {
		a.batch.add(r.IntN(lw), r.IntN(max(1, horizon)), 1, 1, gray(0.3+0.5*r.Float64()))
	}
	moon := 9 + int(3*bass)
	mx, my := lw*3/4, horizon/3+moon
	for y := -moon; y <= moon; y++ {
		half := int(math.Sqrt(float64(moon*moon - y*y)))
		a.batch.add(mx-half, my+y, 2*half, 1, gray(1.0+0.3*pulse))
	}
	a.batch.add(0, horizon, lw, lh-horizon, gray(0.05))
	a.batch.draw(img)

	// La orilla: un anillo de tierra alrededor del agua (capa 0).
	const ring = 28
	for i := 0; i < ring; i++ {
		a0, a1 := 2*math.Pi*float64(i)/ring, 2*math.Pi*float64(i+1)/ring
		in0, in1 := v3{lkRadius * math.Cos(a0), 0.05, lkRadius * math.Sin(a0)}, v3{lkRadius * math.Cos(a1), 0.05, lkRadius * math.Sin(a1)}
		out0, out1 := in0.mul(3), in1.mul(3)
		out0.y, out1.y = 0.4, 0.4
		l.addFace(&cam, outward([]v3{in0, out0, out1, in1}, v3{0, -10, 0}), 0.14, false, false, 0)
	}
	// El agua: una malla en anillos y sectores (redonda, sin escalones). Las
	// crestas, más claras y los valles más oscuros; destellos donde la cara
	// mira a la luz.
	l.prepareWaves(t, bass)
	hAt := l.height
	// Cada punto de la malla se calcula una vez (lo comparten hasta seis
	// triángulos).
	const rings, sectors = 14, 36
	if len(l.grid) != (rings+1)*sectors {
		l.grid = make([]v3, (rings+1)*sectors)
	}
	for ri := 0; ri <= rings; ri++ {
		r := lkRadius * float64(ri) / rings
		for si := 0; si < sectors; si++ {
			an := 2 * math.Pi * float64(si) / sectors
			x, z := r*math.Cos(an), r*math.Sin(an)
			l.grid[ri*sectors+si] = v3{x, hAt(x, z), z}
		}
	}
	pt := func(ri, si int) v3 { return l.grid[ri*sectors+si%sectors] }
	var tris [2][3]v3
	for ri := 0; ri < rings; ri++ {
		for si := 0; si < sectors; si++ {
			p00, p01 := pt(ri, si), pt(ri, si+1)
			p10, p11 := pt(ri+1, si), pt(ri+1, si+1)
			tris = [2][3]v3{{p00, p10, p11}, {p00, p11, p01}}
			nt := 2
			if ri == 0 {
				nt = 1 // el centro: un triángulo
			}
			for k := 0; k < nt; k++ {
				tri := &tris[k]
				n := tri[1].sub(tri[0]).cross(tri[2].sub(tri[0])).norm()
				if n.y < 0 {
					tri[1], tri[2] = tri[2], tri[1]
					n = n.mul(-1)
				}
				hm := (tri[0].y + tri[1].y + tri[2].y) / 3
				tone := 0.3 + 0.08*level + 1.3*hm // crestas claras, valles oscuros
				if n.dot(light) > 0.95 {
					tone += 0.6 // destello
				}
				l.addFace(&cam, tri[:], math.Max(0.08, tone), true, false, 1)
			}
		}
	}
	// Las ondas, como anillos claros que se abren y se apagan.
	for _, r := range l.ripples {
		age := t - r.at
		rad := age * 5
		fade := math.Exp(-age * 1.1)
		if rad < 0.2 || fade < 0.08 {
			continue
		}
		// La altura, una vez por ángulo (el anillo es estrecho: dentro y
		// fuera casi no cambia).
		const n = 24
		if len(l.ring) < n+1 {
			l.ring = make([]float64, n+1)
		}
		for k := 0; k <= n; k++ {
			an := 2 * math.Pi * float64(k) / n
			l.ring[k] = hAt(r.x+rad*math.Cos(an), r.z+rad*math.Sin(an)) + 0.03
		}
		for k := 0; k < n; k++ {
			a0, a1 := 2*math.Pi*float64(k)/n, 2*math.Pi*float64(k+1)/n
			c0, s0, c1, s1 := math.Cos(a0), math.Sin(a0), math.Cos(a1), math.Sin(a1)
			out0 := v3{r.x + (rad+0.12)*c0, l.ring[k], r.z + (rad+0.12)*s0}
			out1 := v3{r.x + (rad+0.12)*c1, l.ring[k+1], r.z + (rad+0.12)*s1}
			if out0.x*out0.x+out0.z*out0.z > lkRadius*lkRadius || out1.x*out1.x+out1.z*out1.z > lkRadius*lkRadius {
				continue // fuera del lago
			}
			pts := l.scratch(4)
			pts[0] = v3{r.x + (rad-0.12)*c0, l.ring[k], r.z + (rad-0.12)*s0}
			pts[1], pts[2] = out0, out1
			pts[3] = v3{r.x + (rad-0.12)*c1, l.ring[k+1], r.z + (rad-0.12)*s1}
			l.addFace(&cam, pts, 0.5+0.9*fade, true, false, 2)
		}
	}
	// Los juncos de la orilla, que se mecen con la música.
	for _, rd := range l.reeds {
		ang := rd[0]
		base := v3{(lkRadius + 0.8) * math.Cos(ang), 0, (lkRadius + 0.8) * math.Sin(ang)}
		sway := math.Sin(t*1.5+rd[2]) * (0.15 + 0.5*level)
		tip := base.add(v3{sway * math.Sin(ang), rd[1], -sway * math.Cos(ang)})
		side := v3{-math.Sin(ang), 0, math.Cos(ang)}.mul(0.08)
		l.addFace(&cam, []v3{base.sub(side), base.add(side), tip}, 0.35, true, false, 2)
	}
	// Los nenúfares y las flores.
	for _, p := range l.pads {
		y := hAt(p.x, p.z) + 0.03
		var disk []v3
		const n = 9
		for k := 0; k < n; k++ { // con la muesca: un hueco entre el último y el primero
			an := p.rot + (2*math.Pi-0.6)*float64(k)/(n-1)
			disk = append(disk, v3{p.x + p.r*math.Cos(an), y, p.z + p.r*math.Sin(an)})
		}
		disk = append(disk, v3{p.x, y, p.z})
		l.addFace(&cam, outward(disk, v3{p.x, y - 5, p.z}), 0.5, false, false, 2)
		if p.flower {
			// El loto: dos coronas de pétalos que se abren con los medios.
			for ring := 0; ring < 2; ring++ {
				petals := 6
				open := 0.25 + 0.9*l.open*(1-0.4*float64(ring))
				length := 0.6 - 0.2*float64(ring)
				for k := 0; k < petals; k++ {
					an := 2*math.Pi*float64(k)/float64(petals) + float64(ring)*0.5 + p.rot
					dir := v3{math.Cos(an), 0, math.Sin(an)}
					side := v3{-math.Sin(an), 0, math.Cos(an)}.mul(0.16)
					base := v3{p.x, y + 0.05, p.z}
					tip := base.add(dir.mul(length * math.Sin(open))).add(v3{0, length * math.Cos(open), 0})
					mid := base.add(dir.mul(length * 0.5 * math.Sin(open))).add(v3{0, length * 0.5 * math.Cos(open), 0})
					tone := 1.0 + 0.3*float64(ring) + 0.2*pulse
					l.addFace(&cam, []v3{base, mid.add(side), tip}, tone, true, false, 3)
					l.addFace(&cam, []v3{base, tip, mid.sub(side)}, tone*0.85, true, false, 3)
				}
			}
		}
	}
	// Las ranas: quietas en su hoja o saltando (en arco) a otra.
	for _, f := range l.frogs {
		from := l.pads[f.pad]
		pos := v3{from.x, hAt(from.x, from.z) + 0.05, from.z}
		yaw := 0.0
		if f.jumpAt > 0 {
			to := l.pads[f.to]
			u := math.Min(1, (t-f.jumpAt)/0.7)
			dst := v3{to.x, hAt(to.x, to.z) + 0.05, to.z}
			pos = lerp3(pos, dst, u).add(v3{0, 2.2 * 4 * u * (1 - u), 0})
			yaw = math.Atan2(to.x-from.x, to.z-from.z)
		}
		squash := 1 - 0.25*pulse
		for _, b := range [][2]v3{{{0, 0.18, 0}, {0.3, 0.18, 0.35}}, {{0, 0.38, 0.32}, {0.22, 0.13, 0.15}}, {{-0.15, 0.55, 0.38}, {0.06, 0.06, 0.06}}, {{0.15, 0.55, 0.38}, {0.06, 0.06, 0.06}}, {{-0.32, 0.08, -0.2}, {0.1, 0.08, 0.25}}, {{0.32, 0.08, -0.2}, {0.1, 0.08, 0.25}}} {
			c := b[0]
			c.y *= squash
			for _, fc := range l.box(c, b[1], 0.65) {
				pts := l.scratch(len(fc.pts))
				for i, p := range fc.pts {
					pts[i] = rotY(p, yaw).add(pos)
				}
				l.addFaceG(&cam, pts, fc.tone*1.2, false, true, 3, false, true)
			}
		}
	}
	// Los patos: cuerpo, cuello, cabeza y pico; la cabeza baja con la caja.
	for _, d := range l.ducks {
		x, z := d.cx+d.r*math.Cos(d.ang), d.cz+d.r*math.Sin(d.ang)
		pos := v3{x, hAt(x, z), z}
		yaw := math.Atan2(-math.Sin(d.ang), math.Cos(d.ang)) + math.Pi/2*sign(d.speed)
		dip := 0.0
		if u := (t - d.dipAt) / 0.6; d.dipAt > 0 && u < 1 {
			dip = math.Sin(u * math.Pi)
		}
		parts := [][2]v3{
			{{0, 0.25, 0}, {0.3, 0.2, 0.5}},
			{{0, 0.55 - 0.45*dip, 0.38 + 0.25*dip}, {0.1, 0.22, 0.1}},
			{{0, 0.82 - 0.7*dip, 0.45 + 0.35*dip}, {0.14, 0.12, 0.17}},
			{{0, 0.8 - 0.7*dip, 0.66 + 0.35*dip}, {0.06, 0.04, 0.09}},
		}
		for k, b := range parts {
			tone := 0.85
			if k == 3 {
				tone = 1.2
			}
			for _, fc := range l.box(b[0], b[1], tone) {
				pts := l.scratch(len(fc.pts))
				for i, p := range fc.pts {
					pts[i] = rotY(p, yaw).add(pos)
				}
				l.addFaceG(&cam, pts, fc.tone, false, true, 3, false, true)
			}
		}
	}
	// Los peces saltando: en arco, de cabeza.
	for _, f := range l.fish {
		u := (t - f.at) / 0.9
		pos := v3{f.x0 + (f.x1-f.x0)*u, 2.5 * 4 * u * (1 - u), f.z0 + (f.z1-f.z0)*u}
		yaw := math.Atan2(f.x1-f.x0, f.z1-f.z0)
		pitch := (0.5 - u) * 2.2
		body := []v3{{0, 0, 0.5}, {0, 0.18, 0}, {0, 0, -0.45}, {0, -0.15, 0}}
		tail := []v3{{0, 0, -0.4}, {0, 0.25, -0.7}, {0, -0.25, -0.7}}
		for _, poly := range [][]v3{body, tail} {
			for _, sx := range []float64{-0.06, 0.06} {
				pts := l.scratch(len(poly))
				for i, p := range poly {
					p.x = sx
					pts[i] = rotY(rotX(p, -pitch), yaw).add(pos)
				}
				l.addFace(&cam, pts, 1.1, true, false, 3)
			}
		}
	}
	// Las libélulas: cruces que revolotean con los agudos.
	var treble float64
	for k := 10; k < engine.MeterBands; k++ {
		treble += bands[k] / 6
	}
	for i := 0; i < 4; i++ {
		ang := t*0.4 + float64(i)*1.6
		pos := v3{7 * math.Cos(ang), 1.5 + 0.5*math.Sin(t*2+float64(i)), 7 * math.Sin(ang*1.3)}
		pos = pos.add(v3{math.Sin(t*23+float64(i)) * treble * 0.6, math.Sin(t*19) * treble * 0.4, 0})
		wing := 0.5 * (0.3 + 0.7*math.Abs(math.Sin(t*40)))
		l.addFace(&cam, []v3{pos.add(v3{-wing, 0, 0.05}), pos.add(v3{wing, 0, 0.05}), pos.add(v3{wing, 0, -0.05}), pos.add(v3{-wing, 0, -0.05})}, 0.9, true, false, 3)
		l.addFace(&cam, []v3{pos.add(v3{-0.03, 0, 0.4}), pos.add(v3{0.03, 0, 0.4}), pos.add(v3{0.03, 0, -0.4}), pos.add(v3{-0.03, 0, -0.4})}, 0.7, true, false, 3)
	}
	// Las luciérnagas, que parpadean con el charles.
	for i, fl := range l.flies {
		ang := fl[0] + t*0.05
		pos := v3{fl[1] * math.Cos(ang), fl[2] + 0.3*math.Sin(t+float64(i)), fl[1] * math.Sin(ang)}
		glow := 0.6 + 0.9*l.blink*float64((i+int(t*3))%2)
		for _, fc := range l.box(pos, v3{0.07, 0.07, 0.07}, glow) {
			l.addFace(&cam, fc.pts, fc.tone, true, true, 3)
		}
	}
	l.render(img, &cam)
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}
