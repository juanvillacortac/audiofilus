package ui

import (
	"math"
	"math/rand/v2"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Trinchera: un visualizador espacial en 3D. Un caza (diseño propio: alas en
// delta y dos motores) vuela por la trinchera de una estación. La trinchera
// se construye con la música: los módulos de las paredes y del suelo
// sobresalen según el espectro del momento en que se generan. La nave
// zigzaguea, se inclina con la caja, hace un tonel con los platillos, sus
// motores laten con los graves y dispara en cada golpe; las torretas del borde
// le disparan. Arriba, estrellas. Usa el motor de Derrape (r3d).

const (
	trHalf  = 6.0 // media anchura de la trinchera
	trDepth = 9.0 // profundidad
	trSeg   = 4.0 // metros por tramo
	trAhead = 55
)

// trBox es un módulo que sobresale: centro y medias medidas.
type trBox struct{ c, h v3 }

// trSegment es un tramo de trinchera con sus módulos.
type trSegment struct {
	z      float64
	index  int
	boxes  []trBox
	faces  []face // las caras de los módulos, calculadas una vez
	turret int    // -1, 0 o 1: torreta en el borde izquierdo, ninguna o derecho
}

// bolt es un disparo: dónde está y hacia dónde va.
type bolt struct {
	pos, vel v3
	life     float64
	enemy    bool
}

// trenchState es el visualizador.
type trenchState struct {
	r3d
	segs                 []trSegment
	next                 int
	z                    float64 // dónde va la nave
	x, y                 float64 // su posición en la sección de la trinchera
	vx, vy               float64
	roll                 float64
	rollTo               float64
	barrel               float64 // vuelta de tonel en curso (rad que faltan)
	speed                float64
	cam                  int // 0 persecución, 1 cabina, 2 frontal
	bolts                []bolt
	stars                [][3]float64
	seenSnare, seenCrash float64
	beats                int
	rng                  *rand.Rand
}

func newTrench() *trenchState {
	t := &trenchState{rng: rand.New(rand.NewPCG(5, 3)), y: 4}
	t.white = newWhite()
	for i := 0; i < 160; i++ {
		t.stars = append(t.stars, [3]float64{t.rng.Float64(), t.rng.Float64(), 0.3 + 0.7*t.rng.Float64()})
	}
	var bands [engine.MeterBands]float64
	for i := 0; i < trAhead+4; i++ {
		t.extend(bands, 0.3)
	}
	return t
}

// extend añade un tramo con módulos que salen del espectro de ahora.
func (t *trenchState) extend(bands [engine.MeterBands]float64, level float64) {
	s := trSegment{z: float64(t.next) * trSeg, index: t.next}
	for _, side := range []float64{-1, 1} {
		for k := 0; k < 2; k++ {
			b := bands[(t.next*3+k*5+int(side+1)*2)%engine.MeterBands]
			out := 0.25 + 2.2*b*(0.5+level)
			y := 1 + t.rng.Float64()*(trDepth-2.5)
			hz, hy := 0.6+0.9*t.rng.Float64(), 0.4+0.9*t.rng.Float64()
			s.boxes = append(s.boxes, trBox{
				c: v3{side * (trHalf - out/2), y, s.z + trSeg/2 + (t.rng.Float64()-0.5)*1.5},
				h: v3{out / 2, hy, hz},
			})
		}
	}
	if t.rng.IntN(3) == 0 { // algo en el suelo
		up := 0.2 + 1.4*bands[t.next%4]
		s.boxes = append(s.boxes, trBox{c: v3{(t.rng.Float64() - 0.5) * 8, up / 2, s.z + trSeg/2}, h: v3{0.8, up / 2, 0.8}})
	}
	if t.next%9 == 0 {
		s.turret = 1 - 2*t.rng.IntN(2)
	}
	for _, b := range s.boxes {
		s.faces = append(s.faces, box(b.c, b.h, 0.62)...)
	}
	if s.turret != 0 {
		base := v3{float64(s.turret) * (trHalf + 1.5), trDepth + 0.8, s.z}
		s.faces = append(s.faces, box(base, v3{0.8, 0.8, 0.8}, 0.55)...)
		s.faces = append(s.faces, box(base.add(v3{-float64(s.turret) * 0.6, 0.9, 0}), v3{0.6, 0.12, 0.12}, 0.7)...)
	}
	t.segs = append(t.segs, s)
	t.next++
}

// shipModel es el caza, mirando a +z (diseño propio: alas en delta).
var shipModel = buildShip()

func buildShip() []face {
	var f []face
	c := v3{0, 0, 0}
	add := func(tone float64, glow bool, pts ...v3) {
		f = append(f, face{pts: outward(pts, c), tone: tone, glow: glow})
	}
	// Fuselaje: un prisma de seis lados que se afina hacia el morro.
	const n = 6
	ring := func(z, r float64) []v3 {
		var out []v3
		for i := 0; i < n; i++ {
			s, co := math.Sincos(2*math.Pi*float64(i)/n + math.Pi/6)
			out = append(out, v3{r * co, r * s * 0.7, z})
		}
		return out
	}
	nose, mid, back := ring(2.4, 0.08), ring(0.6, 0.45), ring(-1.8, 0.5)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		add(0.85, false, nose[i], nose[j], mid[j], mid[i])
		add(0.8, false, mid[i], mid[j], back[j], back[i])
	}
	add(0.4, false, back...)
	add(0.9, false, nose...)
	// Cabina, oscura, encima.
	add(0.2, false, v3{-0.22, 0.3, 1.2}, v3{0.22, 0.3, 1.2}, v3{0.18, 0.42, 0.2}, v3{-0.18, 0.42, 0.2})
	// Alas en delta, finas, con algo de diedro.
	for _, sg := range []float64{-1, 1} {
		root0, root1 := v3{sg * 0.4, 0, 0.6}, v3{sg * 0.45, 0, -1.6}
		tip := v3{sg * 2.6, 0.25, -1.4}
		up := v3{0, 0.04, 0}
		add(0.75, false, root0.add(up), tip.add(up), root1.add(up))
		add(0.4, false, root0.sub(up), root1.sub(up), tip.sub(up))
		// Motor bajo el ala, con la tobera que brilla.
		pod := v3{sg * 1.1, -0.15, -1.2}
		f = append(f, box(pod, v3{0.18, 0.16, 0.7}, 0.6)...)
		add(1.4, true, pod.add(v3{-0.14, -0.12, -0.71}), pod.add(v3{0.14, -0.12, -0.71}), pod.add(v3{0.14, 0.12, -0.71}), pod.add(v3{-0.14, 0.12, -0.71}))
	}
	// Deriva.
	add(0.7, false, v3{0, 0.3, -0.6}, v3{0, 1.1, -1.7}, v3{0, 0.3, -1.8})
	add(0.7, false, v3{0, 0.3, -0.6}, v3{0, 0.3, -1.8}, v3{0, 1.1, -1.7})
	return f
}

// stepTrench mueve la nave y la trinchera dt segundos, con la música.
func (a *App) stepTrench(dt float64) {
	t := a.trench
	level, _, bands, ok := a.meterNow()
	if !ok || !a.isPlaying {
		level = 0
	}
	want := 0.0
	if a.isPlaying {
		tempo := 1.0
		if bpm := a.beat.BPM(); bpm > 0 {
			tempo = math.Max(0.7, math.Min(1.4, float64(bpm)/120))
		}
		want = (30 + 25*level) * tempo
	}
	t.speed += (want - t.speed) * math.Min(1, 1.2*dt) // sin frenazos
	speed := t.speed
	t.z += speed * dt
	for len(t.segs) > 0 && t.segs[0].z+trSeg < t.z-12 {
		t.segs = t.segs[1:]
		t.extend(bands, level)
	}
	// El rumbo: un zigzag suave que se agita con la música.
	tx := 3.2 * math.Sin(a.t*0.6+math.Sin(a.t*0.23)*2) * (0.5 + 0.5*level)
	ty := 4 + 2*math.Sin(a.t*0.41)
	t.vx += ((tx-t.x)*3 - t.vx*2.5) * dt
	t.vy += ((ty-t.y)*3 - t.vy*2.5) * dt
	t.x += t.vx * dt
	t.y += t.vy * dt
	// El baile: la caja inclina a un lado y a otro; los platillos, un tonel.
	dr := &a.beat.drums
	if dr.last[drumSnare] != t.seenSnare {
		t.seenSnare = dr.last[drumSnare]
		if t.rollTo <= 0 {
			t.rollTo = 0.45
		} else {
			t.rollTo = -0.45
		}
	}
	if dr.last[drumCrash] != t.seenCrash {
		t.seenCrash = dr.last[drumCrash]
		if t.barrel == 0 {
			t.barrel = 2 * math.Pi
		}
	}
	t.rollTo *= math.Exp(-dt * 2)
	bank := -t.vx * 0.12
	t.roll += (t.rollTo + bank - t.roll) * 6 * dt
	if t.barrel > 0 {
		step := math.Min(t.barrel, 12*dt)
		t.barrel -= step
		t.roll += step
	}
	// Disparos: la nave en cada golpe; una torreta de vez en cuando.
	if a.beatHit && a.isPlaying {
		t.beats++
		ship := v3{t.x, t.y, t.z}
		// Salen de los cañones bajo las alas (la punta de los motores),
		// girando con la nave.
		for _, sg := range []float64{-1, 1} {
			muzzle := rotZ(v3{sg * 1.1, -0.15, -0.4}, t.roll)
			t.bolts = append(t.bolts, bolt{pos: ship.add(muzzle), vel: v3{0, 0, speed + 140}, life: 0.8})
		}
		if t.beats%2 == 0 {
			for _, s := range t.segs {
				if s.turret != 0 && s.z > t.z+40 && s.z < t.z+120 {
					from := v3{float64(s.turret) * (trHalf + 1), trDepth + 1, s.z}
					dir := v3{t.x, t.y, t.z + 20}.sub(from).norm()
					t.bolts = append(t.bolts, bolt{pos: from, vel: dir.mul(90), life: 1.5, enemy: true})
					break
				}
			}
		}
	}
	kept := t.bolts[:0]
	for _, b := range t.bolts {
		b.life -= dt
		b.pos = b.pos.add(b.vel.mul(dt))
		if b.life > 0 {
			kept = append(kept, b)
		}
	}
	t.bolts = kept
}

// drawTrenchScene pinta la escena en intensidad en img.
func (a *App) drawTrenchScene(img *ebiten.Image) {
	if a.trench == nil {
		a.trench = newTrench()
	}
	t := a.trench
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	_, bass, _, _ := a.meterNow()
	pulse := a.beatPulse()
	ship := v3{t.x, t.y, t.z}
	focal := float64(lh) * 1.05
	var cam drCam
	switch t.cam {
	case 1: // cabina: en primera persona, inclinada con la nave
		eye := ship.add(rotZ(v3{0, 0.5, 0.9}, t.roll))
		cam = lookAt(eye, eye.add(v3{0, 0, 10}).add(rotZ(v3{0, 0.3, 0}, t.roll)), focal, float64(lw), float64(lh))
	case 2: // frontal: delante de la nave, mirándola
		cam = lookAt(ship.add(v3{2, 1.5, 9}), ship.add(v3{0, 0.2, 0}), focal, float64(lw), float64(lh))
	default: // persecución
		cam = lookAt(ship.add(v3{-t.x * 0.3, 2.6, -9}), ship.add(v3{0, 0.4, 12}), focal, float64(lw), float64(lh))
	}

	// Las estrellas, que se estiran con la velocidad.
	for _, s := range t.stars {
		x := int(s[0] * float64(lw))
		y := int(s[1] * float64(lh) * 0.5)
		a.batch.add(x, y, 1, 1+int(pulse*2), gray(0.4+0.6*s[2]))
	}
	a.batch.draw(img)

	// La trinchera.
	for i, s := range t.segs {
		z0, z1 := s.z, s.z+trSeg
		if z1 < t.z-12 {
			continue
		}
		alt := 0.0
		if s.index%2 == 0 {
			alt = 0.04
		}
		// Suelo y paredes.
		t.addFace(&cam, []v3{{-trHalf, 0, z0}, {-trHalf, 0, z1}, {trHalf, 0, z1}, {trHalf, 0, z0}}, 0.22+alt, false, false, 0)
		t.addFace(&cam, []v3{{-trHalf, 0, z0}, {-trHalf, trDepth, z0}, {-trHalf, trDepth, z1}, {-trHalf, 0, z1}}, 0.34+alt, false, false, 0)
		t.addFace(&cam, []v3{{trHalf, 0, z0}, {trHalf, 0, z1}, {trHalf, trDepth, z1}, {trHalf, trDepth, z0}}, 0.34+alt, false, false, 0)
		// La superficie de la estación, a los lados, arriba.
		for _, sg := range []float64{-1, 1} {
			q := []v3{{sg * trHalf, trDepth, z0}, {sg * (trHalf + 40), trDepth, z0}, {sg * (trHalf + 40), trDepth, z1}, {sg * trHalf, trDepth, z1}}
			t.addFace(&cam, orient(q, -sg), 0.16+alt, false, false, 0)
		}
		// Las luces del borde, que destellan con los golpes.
		if s.index%4 == 0 {
			for _, sg := range []float64{-1, 1} {
				for _, f := range box(v3{sg * (trHalf - 0.1), trDepth - 0.15, z0}, v3{0.12, 0.12, 0.25}, 1.0+0.5*pulse) {
					t.addFace(&cam, f.pts, f.tone, true, true, 1)
				}
			}
		}
		// Los módulos y la torreta (caras ya calculadas).
		for _, f := range s.faces {
			t.addFace(&cam, f.pts, f.tone, false, true, 1)
		}
		_ = i
	}
	// Los disparos: rayas emisivas.
	for _, b := range t.bolts {
		dir := b.vel.norm().mul(2.5)
		w := v3{0.08, 0, 0}
		if b.enemy {
			w = v3{0, 0.12, 0}
		}
		tone := 1.5
		if b.enemy {
			tone = 1.2
		}
		p0, p1 := b.pos, b.pos.add(dir)
		t.addFace(&cam, []v3{p0.sub(w), p0.add(w), p1.add(w), p1.sub(w)}, tone, true, false, 1)
		t.addFace(&cam, []v3{p0.sub(v3{0, 0.08, 0}), p0.add(v3{0, 0.08, 0}), p1.add(v3{0, 0.08, 0}), p1.sub(v3{0, 0.08, 0})}, tone, true, false, 1)
	}
	// La nave.
	for _, f := range shipModel {
		pts := t.scratch(len(f.pts))
		for i, p := range f.pts {
			p = rotZ(p, t.roll)
			pts[i] = p.add(ship)
		}
		tone := f.tone * 1.3
		if f.tone < 0.3 {
			tone = f.tone
		}
		if f.glow {
			tone = 1.0 + 0.5*bass + 0.3*pulse // los motores laten con los graves
		}
		t.addFaceG(&cam, pts, tone, f.glow, true, 1, false, !f.glow)
	}
	t.render(img, &cam)
}
