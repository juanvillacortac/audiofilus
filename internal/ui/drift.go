package ui

import (
	"cmp"
	"image/color"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
)

// Derrape: un visualizador en 3D. Un cupé deportivo low-poly (capó largo,
// maletero corto, como los de los 90; genérico, sin marcas) derrapa por una
// carretera que se genera con la música: las curvas cambian cada compás y se
// cierran cuando la canción suena fuerte, la velocidad sigue al tempo, las
// farolas destellan en los golpes y al fondo la ciudad es el espectro. El coche
// baila: rebota y se aplasta con el bombo, se mece con la caja y salta con los
// platillos.
//
// El motor es propio y mínimo: proyección en perspectiva y triángulos con
// sombreado plano, todo en intensidad (luz), en un solo lote. Se dibuja a media
// resolución y un shader lo pasa a la rampa de la paleta (fondo → acento →
// texto) con trama de Bayer: un solo color, como el resto de la interfaz.

// v3 es un punto o vector en 3D (metros; y hacia arriba, z hacia delante).
type v3 struct{ x, y, z float64 }

func (a v3) add(b v3) v3          { return v3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a v3) sub(b v3) v3          { return v3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a v3) mul(k float64) v3     { return v3{a.x * k, a.y * k, a.z * k} }
func (a v3) dot(b v3) float64     { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a v3) cross(b v3) v3        { return v3{a.y*b.z - a.z*b.y, a.z*b.x - a.x*b.z, a.x*b.y - a.y*b.x} }
func (a v3) norm() v3             { return a.mul(1 / math.Max(1e-9, math.Sqrt(a.dot(a)))) }
func rotY(p v3, a float64) v3     { s, c := math.Sincos(a); return v3{p.x*c + p.z*s, p.y, -p.x*s + p.z*c} }
func rotZ(p v3, a float64) v3     { s, c := math.Sincos(a); return v3{p.x*c - p.y*s, p.x*s + p.y*c, p.z} }
func rotX(p v3, a float64) v3     { s, c := math.Sincos(a); return v3{p.x, p.y*c - p.z*s, p.y*s + p.z*c} }
func lerp3(a, b v3, t float64) v3 { return a.add(b.sub(a).mul(t)) }

// face es un polígono convexo del modelo con su intensidad base (0..1, puede
// pasar de 1 para los brillos) y si es emisivo (no le afecta la luz).
type face struct {
	pts  []v3
	tone float64
	glow bool
}

// carModel es el coche, en metros, mirando a +z, con las ruedas aparte (para
// girarlas).
var carModel, wheelModel = buildCar()

// buildCar arma el cupé: la carrocería es su perfil lateral (hasta la línea
// de cintura) extruido a lo ancho; encima, la cabina, más estrecha arriba;
// luego los pasos de rueda, las luces, los retrovisores, el alerón y los
// escapes. Las ruedas van aparte: neumático y llanta.
func buildCar() ([]face, []face) {
	const hw = 0.88 // media anchura de la carrocería
	// El perfil (z, y) de la carrocería, de delante a atrás por arriba.
	prof := [][2]float64{
		{2.22, 0.26}, {2.3, 0.5}, {2.12, 0.64}, // paragolpes y morro
		{0.8, 0.79},                 // capó, hasta el parabrisas
		{-1.7, 0.83}, {-2.15, 0.82}, // cintura y maletero
		{-2.28, 0.52}, {-2.2, 0.26}, // trasera
	}
	var faces []face
	var cz, cy float64
	for _, p := range prof {
		cz, cy = cz+p[0], cy+p[1]
	}
	cz, cy = cz/float64(len(prof)), cy/float64(len(prof))
	center := v3{0, cy, cz}
	add := func(tone float64, glow bool, pts ...v3) {
		faces = append(faces, face{pts: outward(pts, center), tone: tone, glow: glow})
	}
	// Los lados y las caras de arriba y abajo.
	for _, side := range []float64{-hw, hw} {
		for i := range prof {
			p, q := prof[i], prof[(i+1)%len(prof)]
			add(0.78, false, v3{side, cy, cz}, v3{side, p[1], p[0]}, v3{side, q[1], q[0]})
		}
	}
	for i := range prof {
		p, q := prof[i], prof[(i+1)%len(prof)]
		tone := 0.9
		switch i {
		case 0, 6: // paragolpes
			tone = 0.6
		case 7: // los bajos
			tone = 0.1
		}
		add(tone, false, v3{-hw, p[1], p[0]}, v3{hw, p[1], p[0]}, v3{hw, q[1], q[0]}, v3{-hw, q[1], q[0]})
	}
	// La cabina: de la cintura (más ancha) al techo (más estrecho).
	const bw, rw = 0.8, 0.64
	b0, b1 := v3{0, 0.79, 0.8}, v3{0, 0.83, -1.7}  // base: parabrisas y luneta
	r0, r1 := v3{0, 1.22, 0.05}, v3{0, 1.2, -1.05} // techo
	at := func(p v3, x float64) v3 { return v3{x, p.y, p.z} }
	cabin := v3{0, 1.0, -0.5}
	addC := func(tone float64, pts ...v3) {
		faces = append(faces, face{pts: outward(pts, cabin), tone: tone})
	}
	addC(0.22, at(b0, -bw), at(b0, bw), at(r0, rw), at(r0, -rw)) // parabrisas
	addC(0.95, at(r0, -rw), at(r0, rw), at(r1, rw), at(r1, -rw)) // techo
	addC(0.22, at(r1, -rw), at(r1, rw), at(b1, bw), at(b1, -bw)) // luneta
	for _, sg := range []float64{-1, 1} {
		// La ventanilla (oscura) y el pilar central (del color del coche).
		addC(0.2, at(b0, sg*bw), at(r0, sg*rw), at(r1, sg*rw), at(b1, sg*bw))
		pz0, pz1 := -0.62, -0.75
		addC(0.8,
			v3{sg * (bw + 0.004), 0.81, pz0}, v3{sg * (rw + 0.004), 1.21, pz0 + 0.1},
			v3{sg * (rw + 0.004), 1.21, pz1 + 0.1}, v3{sg * (bw + 0.004), 0.81, pz1})
	}
	// Los pasos de rueda: octógonos oscuros un pelo por fuera del lado.
	for _, wz := range []float64{1.35, -1.35} {
		for _, sg := range []float64{-1, 1} {
			var arch []v3
			for k := 0; k < 8; k++ {
				s, c := math.Sincos(2 * math.Pi * float64(k) / 8)
				arch = append(arch, v3{sg * (hw + 0.006), 0.34 + 0.42*c, wz + 0.42*s})
			}
			faces = append(faces, face{pts: outward(arch, v3{0, 0.34, wz}), tone: 0.06})
		}
	}
	// Faldones: una tira oscura entre las ruedas.
	for _, sg := range []float64{-1, 1} {
		x := sg * (hw + 0.007)
		add(0.3, false, v3{x, 0.28, 0.92}, v3{x, 0.4, 0.92}, v3{x, 0.4, -0.92}, v3{x, 0.28, -0.92})
	}
	// Faros escamoteables (en el morro, emisivos), parrilla, piloto corrido.
	for _, sg := range []float64{-1, 1} {
		x0, x1 := sg*0.42, sg*0.8
		faces = append(faces, face{pts: outward([]v3{{x0, 0.66, 2.0}, {x1, 0.66, 2.0}, {x1, 0.68, 1.75}, {x0, 0.68, 1.75}}, v3{0, 0, 1.9}), tone: 0.7})
		faces = append(faces, face{pts: outward([]v3{{x0, 0.52, 2.31}, {x1, 0.52, 2.27}, {x1, 0.6, 2.2}, {x0, 0.6, 2.24}}, v3{0, 0.4, 1.8}), tone: 1.4, glow: true})
	}
	faces = append(faces,
		face{pts: outward([]v3{{-0.32, 0.4, 2.29}, {0.32, 0.4, 2.29}, {0.32, 0.48, 2.3}, {-0.32, 0.48, 2.3}}, v3{0, 0.4, 1.8}), tone: 0.1},
		face{pts: outward([]v3{{-0.84, 0.64, -2.27}, {0.84, 0.64, -2.27}, {0.84, 0.76, -2.22}, {-0.84, 0.76, -2.22}}, v3{0, 0.6, -1.8}), tone: 1.1, glow: true},
	)
	// Retrovisores, alerón y escapes.
	faces = append(faces, box(v3{-0.95, 0.9, 0.62}, v3{0.07, 0.05, 0.09}, 0.7)...)
	faces = append(faces, box(v3{0.95, 0.9, 0.62}, v3{0.07, 0.05, 0.09}, 0.7)...)
	faces = append(faces, box(v3{0, 0.98, -2.0}, v3{0.8, 0.03, 0.18}, 0.6)...)
	faces = append(faces, box(v3{-0.6, 0.9, -2.0}, v3{0.03, 0.07, 0.05}, 0.4)...)
	faces = append(faces, box(v3{0.6, 0.9, -2.0}, v3{0.03, 0.07, 0.05}, 0.4)...)
	faces = append(faces, box(v3{0.55, 0.3, -2.26}, v3{0.06, 0.04, 0.05}, 0.25)...)
	faces = append(faces, box(v3{0.4, 0.3, -2.26}, v3{0.06, 0.04, 0.05}, 0.25)...)

	// Una rueda: neumático (prisma de ocho lados) y llanta clara por fuera,
	// centrada en el origen, eje x.
	var wheel []face
	const r, ww, n = 0.33, 0.12, 8
	ring := func(x, rad float64) []v3 {
		var out []v3
		for i := 0; i < n; i++ {
			s, c := math.Sincos(2*math.Pi*float64(i)/n + math.Pi/n)
			out = append(out, v3{x, rad * c, rad * s})
		}
		return out
	}
	in, out := ring(-ww, r), ring(ww, r)
	wheel = append(wheel, face{pts: outward(out, v3{}), tone: 0.12}, face{pts: outward(in, v3{}), tone: 0.08})
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		wheel = append(wheel, face{pts: outward([]v3{in[i], in[j], out[j], out[i]}, v3{}), tone: 0.1})
	}
	rim := ring(ww+0.004, 0.21)
	wheel = append(wheel, face{pts: outward(rim, v3{}), tone: 0.75})
	hub := ring(ww+0.008, 0.06)
	wheel = append(wheel, face{pts: outward(hub, v3{}), tone: 0.3})
	return faces, wheel
}

// outward da la vuelta a un polígono si su normal apunta hacia center.
func outward(pts []v3, center v3) []v3 {
	n := pts[1].sub(pts[0]).cross(pts[2].sub(pts[0]))
	var mid v3
	for _, p := range pts {
		mid = mid.add(p)
	}
	mid = mid.mul(1 / float64(len(pts)))
	if n.dot(mid.sub(center)) >= 0 {
		return pts
	}
	out := make([]v3, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}

// box son las seis caras de una caja de centro c y medias medidas h.
func box(c, h v3, tone float64) []face {
	p := func(sx, sy, sz float64) v3 { return v3{c.x + sx*h.x, c.y + sy*h.y, c.z + sz*h.z} }
	return boxOut(c, []face{
		{pts: []v3{p(-1, 1, -1), p(1, 1, -1), p(1, 1, 1), p(-1, 1, 1)}, tone: tone},           // arriba
		{pts: []v3{p(-1, -1, 1), p(1, -1, 1), p(1, -1, -1), p(-1, -1, -1)}, tone: tone * 0.5}, // abajo
		{pts: []v3{p(-1, -1, 1), p(-1, 1, 1), p(1, 1, 1), p(1, -1, 1)}, tone: tone},           // delante
		{pts: []v3{p(1, -1, -1), p(1, 1, -1), p(-1, 1, -1), p(-1, -1, -1)}, tone: tone},       // detrás
		{pts: []v3{p(1, -1, 1), p(1, 1, 1), p(1, 1, -1), p(1, -1, -1)}, tone: tone},           // derecha
		{pts: []v3{p(-1, -1, -1), p(-1, 1, -1), p(-1, 1, 1), p(-1, -1, 1)}, tone: tone},       // izquierda
	})
}

// unitBox son las caras de box ya orientadas, en esquinas de ±1: la
// orientación no depende de las medidas, así que se calcula una vez.
var unitBox = box(v3{}, v3{1, 1, 1}, 1)

// box es como la función box, sin crear memoria: las caras van en búferes del
// motor, que valen hasta la siguiente llamada (se pasan a addFace enseguida).
func (d *r3d) box(c, h v3, tone float64) []face {
	for i, f := range unitBox {
		pts := d.boxPts[i*4 : i*4+4]
		for k, p := range f.pts {
			pts[k] = v3{c.x + p.x*h.x, c.y + p.y*h.y, c.z + p.z*h.z}
		}
		d.boxFaces[i] = face{pts: pts, tone: f.tone * tone}
	}
	return d.boxFaces[:]
}

// boxOut orienta hacia fuera las caras de una caja.
func boxOut(c v3, fs []face) []face {
	for i := range fs {
		fs[i].pts = outward(fs[i].pts, c)
	}
	return fs
}

// Las medidas de la carretera.
const (
	drSegLen   = 4.0  // metros por tramo
	drAhead    = 70   // tramos por delante (280 m)
	drBehind   = 6    // tramos por detrás
	drRoadHalf = 4.0  // media anchura del asfalto
	drVerge    = 40.0 // anchura del arcén y el campo a cada lado
)

// drSeg es un tramo de carretera: dónde empieza, hacia dónde va y su curva.
type drSeg struct {
	pos     v3
	heading float64 // ángulo en el plano (0 = +z)
	curve   float64 // radianes por metro
	index   int     // número de tramo desde el principio (para las rayas y farolas)
}

// smoke es una bocanada de humo de las ruedas.
type smoke struct {
	pos       v3
	age, life float64
}

// driftState es el visualizador.
type driftState struct {
	segs      []drSeg
	next      int     // el número del próximo tramo
	s         float64 // metros recorridos desde el primer tramo de segs
	curve     float64 // la curva que se está generando
	target    float64 // la curva a la que va
	slope     float64
	beatsLeft int // golpes hasta el próximo cambio de curva

	lateral                        float64 // desplazamiento del coche respecto al centro
	drift                          float64 // ángulo de derrape (rad)
	bounceY                        float64 // el rebote (muelle)
	bounceV                        float64
	squash                         float64 // el aplastamiento del bombo (0..1)
	roll                           float64 // el balanceo de la caja (rad)
	rollSide                       float64
	wheel                          float64 // el giro de las ruedas
	smoke                          []smoke
	cam                            int     // 0 persecución, 1 lateral, 2 aérea
	speed                          float64 // la velocidad (m/s), que va hacia la que toca
	seenKick, seenSnare, seenCrash float64

	img *ebiten.Image // el 3D, a media resolución (en intensidad)
	out *ebiten.Image // ya en la paleta, a media resolución
	r3d
	rng *rand.Rand
}

// r3d es el motor 3D de los visualizadores: triángulos ya en la cámara, que
// se ordenan y se pintan en un solo lote (Derrape, Trinchera).
type r3d struct {
	verts []ebiten.Vertex
	idx   []uint16
	tris  []drTri
	white *ebiten.Image
	// Búferes de trabajo, reutilizados en cada cara (sin crear memoria por
	// fotograma: en la consola, el recolector de basura se notaba).
	vbuf, cbuf, tbuf []v3
	vt               []float64
	drawn            int // triángulos del último render (para -bench)
	order            []int32
	boxFaces         [6]face
	boxPts           [24]v3
}

// scratch es un búfer de n puntos para transformar un polígono antes de
// pasarlo a addFace (que no lo guarda).
func (d *r3d) scratch(n int) []v3 {
	if cap(d.tbuf) < n {
		d.tbuf = make([]v3, n)
	}
	return d.tbuf[:n]
}

func newDrift() *driftState {
	d := &driftState{rng: rand.New(rand.NewPCG(7, 11)), beatsLeft: 4}
	d.white = newWhite()
	for i := 0; i < drAhead+drBehind; i++ {
		d.extend(0)
	}
	return d
}

// extend añade un tramo al final, con la curva que toca.
func (d *driftState) extend(level float64) {
	var p v3
	var h float64
	if n := len(d.segs); n > 0 {
		l := d.segs[n-1]
		p = l.pos.add(rotY(v3{0, 0, drSegLen}, l.heading))
		h = l.heading + l.curve*drSegLen
	}
	d.curve += (d.target - d.curve) * 0.06
	d.slope += (0 - d.slope) * 0.02
	p.y += d.slope * drSegLen
	p.y = math.Max(-6, math.Min(6, p.y))
	d.segs = append(d.segs, drSeg{pos: p, heading: h, curve: d.curve, index: d.next})
	d.next++
}

// at es la posición y el rumbo de la carretera a s metros del primer tramo.
func (d *driftState) at(s float64) (v3, float64, float64) {
	i := int(s / drSegLen)
	i = max(0, min(i, len(d.segs)-2))
	f := s/drSegLen - float64(i)
	a, b := d.segs[i], d.segs[i+1]
	return lerp3(a.pos, b.pos, f), a.heading + (b.heading-a.heading)*f, a.curve
}

// stepDrift mueve el coche y la carretera dt segundos, con la música.
func (a *App) stepDrift(dt float64) {
	d := a.drift
	level, _, _, ok := a.meterNow()
	if !ok || !a.isPlaying {
		level = 0
	}
	// La velocidad: con el tempo y lo fuerte que suena; parado, frena.
	want := 0.0
	if a.isPlaying {
		tempo := 1.0
		if bpm := a.beat.BPM(); bpm > 0 {
			tempo = math.Max(0.7, math.Min(1.4, float64(bpm)/120))
		}
		want = (18 + 14*level) * tempo
	}
	// Acelera y frena poco a poco (al pausar no se para en seco).
	d.speed += (want - d.speed) * math.Min(1, 1.2*dt)
	speed := d.speed
	d.s += speed * dt
	d.wheel += speed * dt / 0.34
	// Cada compás (4 golpes), una curva nueva: más cerrada si suena fuerte.
	if a.beatHit {
		d.beatsLeft--
		if d.beatsLeft <= 0 {
			d.beatsLeft = 4
			side := 1.0
			if d.target > 0 || (d.target == 0 && d.rng.IntN(2) == 0) {
				side = -1
			}
			if d.rng.IntN(5) == 0 {
				side = 0 // de vez en cuando, una recta
			}
			d.target = side * (0.006 + 0.03*level + 0.01*d.rng.Float64())
			d.slope = (d.rng.Float64() - 0.5) * 0.06
		}
	}
	// La carretera: se tiran los tramos de detrás y se añaden por delante.
	for d.s > drSegLen*drBehind {
		d.segs = d.segs[1:]
		d.s -= drSegLen
		d.extend(level)
	}
	_, _, curve := d.at(d.s + 8)
	// El derrape: el coche apunta hacia dentro de la curva (sobreviraje) y se
	// abre hacia fuera; en las rectas se endereza.
	d.drift += (math.Max(-0.6, math.Min(0.6, curve*28)) - d.drift) * 4 * dt
	d.lateral += (math.Max(-2.2, math.Min(2.2, -curve*90)) - d.lateral) * 1.5 * dt
	// El baile: el bombo hace rebotar y aplasta, la caja mece, los platillos
	// hacen saltar. Sin batería reconocida, cada golpe es un bombo.
	dr := &a.beat.drums
	kick := a.beatHit && !dr.any
	if dr.last[drumKick] != d.seenKick {
		d.seenKick, kick = dr.last[drumKick], true
	}
	if kick {
		d.bounceV += 1.6
		d.squash = 1
	}
	if dr.last[drumSnare] != d.seenSnare {
		d.seenSnare = dr.last[drumSnare]
		d.rollSide = -d.rollSide
		if d.rollSide == 0 {
			d.rollSide = 1
		}
		d.roll += 0.1 * d.rollSide
	}
	if dr.last[drumCrash] != d.seenCrash {
		d.seenCrash = dr.last[drumCrash]
		d.bounceV += 3.5
	}
	d.bounceV += (-60*d.bounceY - 7*d.bounceV) * dt
	d.bounceY = math.Max(0, d.bounceY+d.bounceV*dt)
	if d.bounceY == 0 && d.bounceV < 0 {
		d.bounceV = 0
	}
	d.squash = math.Max(0, d.squash-dt*6)
	d.roll += (-d.roll) * 5 * dt
	// El humo de las ruedas de atrás al derrapar.
	if math.Abs(d.drift) > 0.15 && speed > 0 {
		pos, head, _ := d.at(d.s)
		for _, side := range []float64{-0.8, 0.8} {
			if d.rng.IntN(2) == 0 { // una de cada dos: menos humo, más limpio
				continue
			}
			w := rotY(v3{side + d.lateral, 0.25, -1.4}, head+d.drift)
			jit := v3{(d.rng.Float64() - 0.5) * 0.4, 0, (d.rng.Float64() - 0.5) * 0.4}
			d.smoke = append(d.smoke, smoke{pos: pos.add(w).add(jit), life: 0.5 + 0.4*d.rng.Float64()})
		}
	}
	kept := d.smoke[:0]
	for _, p := range d.smoke {
		p.age += dt
		p.pos.y += 0.6 * dt
		if p.age < p.life {
			kept = append(kept, p)
		}
	}
	d.smoke = kept
}

// drTri es un triángulo ya en el espacio de la cámara, con su intensidad.
type drTri struct {
	p     [3]v3
	tone  float64
	tones [3]float64 // por vértice (si grad); si no, todos tone
	grad  bool
	depth float64
	layer int  // 0 el suelo (se pinta antes), 1 lo de encima
	solid bool // el coche: sin trama, para que resalte sobre lo tramado
}

// drCam es la cámara: posición y ejes.
type drCam struct {
	pos, r, u, f v3
	focal        float64
	w, h         float64
}

func lookAt(pos, target v3, focal, w, h float64) drCam {
	f := target.sub(pos).norm()
	r := f.cross(v3{0, 1, 0}).norm().mul(-1)
	u := r.cross(f).mul(-1)
	return drCam{pos, r, u, f, focal, w, h}
}

func (c *drCam) view(p v3) v3 {
	d := p.sub(c.pos)
	return v3{d.dot(c.r), d.dot(c.u), d.dot(c.f)}
}

// light es la dirección de la luz (de arriba, delante y un lado).
var light = v3{0.4, 0.85, 0.35}.norm()

// addFace pasa un polígono del mundo a triángulos de la cámara: ilumina,
// quita las caras de espaldas, recorta contra el plano cercano y oscurece con
// la distancia (niebla hacia el fondo).
func (d *r3d) addFace(c *drCam, pts []v3, tone float64, glow, cull bool, layer int) {
	d.addFaceS(c, pts, tone, glow, cull, layer, false)
}

// addFaceS es addFace, diciendo si la cara es sólida (sin trama).
func (d *r3d) addFaceS(c *drCam, pts []v3, tone float64, glow, cull bool, layer int, solid bool) {
	d.addFaceG(c, pts, tone, glow, cull, layer, solid, false)
}

// addFaceG es addFace con degradado: cada vértice, más oscuro abajo y más
// claro arriba (por su altura dentro de la cara), para que el coche y la nave
// tengan sombreado y la trama los integre con el resto.
func (d *r3d) addFaceG(c *drCam, pts []v3, tone float64, glow, cull bool, layer int, solid, grad bool) {
	if len(pts) < 3 {
		return
	}
	n := pts[1].sub(pts[0]).cross(pts[2].sub(pts[0])).norm()
	if cull && n.dot(pts[0].sub(c.pos)) >= 0 {
		return
	}
	t := tone
	if !glow {
		lit := 0.3 + 0.7*math.Max(0, n.dot(light))
		if solid {
			lit = 0.5 + 0.5*math.Max(0, n.dot(light)) // el coche, sin caras negras
		}
		t *= lit
	}
	vp := d.vbuf[:0]
	for _, p := range pts {
		vp = append(vp, c.view(p))
	}
	d.vbuf = vp
	clipped := clipNear(d.cbuf[:0], vp, 0.3)
	d.cbuf = clipped
	if len(clipped) < 3 {
		return
	}
	grad = grad && len(clipped) == len(vp) // recortado no se degrada
	vp = clipped
	vt := d.vt[:0]
	if grad {
		lo := pts[0].y
		for _, p := range pts {
			lo = math.Min(lo, p.y)
		}
		for _, p := range pts {
			vt = append(vt, 0.65+0.5*math.Min(1, (p.y-lo+0.3)/1.3))
		}
	}
	d.vt = vt
	var depth float64
	for _, p := range vp {
		depth += p.z
	}
	depth /= float64(len(vp))
	// Niebla: lo lejano se va hacia el fondo (intensidad baja).
	t *= math.Max(0, 1-depth/260)
	for i := 1; i+1 < len(vp); i++ {
		tri := drTri{p: [3]v3{vp[0], vp[i], vp[i+1]}, tone: t, depth: depth, layer: layer, solid: solid, grad: grad}
		if grad {
			tri.tones = [3]float64{t * vt[0], t * vt[i], t * vt[i+1]}
		}
		d.tris = append(d.tris, tri)
	}
}

// clipNear recorta un polígono (en la cámara) por el plano z = near.
func clipNear(out, pts []v3, near float64) []v3 {
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		ain, bin := a.z >= near, b.z >= near
		if ain {
			out = append(out, a)
		}
		if ain != bin {
			t := (near - a.z) / (b.z - a.z)
			out = append(out, lerp3(a, b, t))
		}
	}
	return out
}

// drawDriftScene pinta la escena en intensidad en img (a media resolución);
// visual.go la pasa a la paleta.
func (a *App) drawDriftScene(img *ebiten.Image) {
	if a.drift == nil {
		a.drift = newDrift()
	}
	d := a.drift
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	_, bass, bands, _ := a.meterNow()
	pulse := a.beatPulse()

	// La cámara.
	carPos, head, _ := d.at(d.s)
	carPos = carPos.add(rotY(v3{d.lateral, 0, 0}, head))
	fw := rotY(v3{0, 0, 1}, head)
	var cam drCam
	focal := float64(lh) * 1.1
	switch d.cam {
	case 1: // lateral, girando despacio alrededor
		ang := head + math.Pi/2 + 0.6*math.Sin(a.t*0.25)
		cam = lookAt(carPos.add(rotY(v3{0, 1.2, 7.5}, ang)), carPos.add(v3{0, 0.6, 0}), focal, float64(lw), float64(lh))
	case 2: // aérea
		cam = lookAt(carPos.add(fw.mul(-9)).add(v3{0, 13, 0}), carPos.add(fw.mul(6)), focal, float64(lw), float64(lh))
	default: // persecución
		cam = lookAt(carPos.add(fw.mul(-6)).add(v3{0, 1.9 + 0.15*bass, 0}), carPos.add(fw.mul(5)).add(v3{0, 0.7, 0}), focal, float64(lw), float64(lh))
	}

	// El cielo y la ciudad (en 2D, al fondo): un degradado por bandas, el sol
	// que late con los graves y edificios que son el espectro.
	horizon := lh * 45 / 100
	for y := 0; y < horizon; y += 2 {
		a.batch.add(0, y, lw, 2, gray(0.05+0.18*float64(y)/float64(horizon)))
	}
	sun := 14 + int(4*bass)
	sx := lw/2 - int(math.Mod(head*80, float64(lw))) + lw
	sx = sx%(lw+2*sun) - sun
	for y := -sun; y < 0; y++ {
		if (-y)%4 == 1 && y > -sun/2 {
			continue // las rayas del sol
		}
		half := int(math.Sqrt(float64(sun*sun - y*y)))
		a.batch.add(sx-half, horizon+y, 2*half, 1, gray(0.55+0.3*pulse))
	}
	const bwid = 6
	off := int(math.Mod(-head*120, bwid*16))
	for i := -2; i < lw/bwid+2; i++ {
		band := ((i % 16) + 16) % 16
		v := bands[band]
		bh := 4 + int(v*float64(horizon)*0.5)
		x := i*bwid + off%bwid
		a.batch.add(x, horizon-bh, bwid-1, bh, gray(0.16+0.12*v))
	}
	a.batch.add(0, horizon, lw, lh-horizon, gray(0.03))
	a.batch.draw(img)

	// El mundo en triángulos.
	d.tris = d.tris[:0]
	for i := 0; i+1 < len(d.segs); i++ {
		s0, s1 := d.segs[i], d.segs[i+1]
		side := func(s drSeg, x float64) v3 { return s.pos.add(rotY(v3{x, 0, 0}, s.heading)) }
		// Asfalto, con bandas alternas que dan la sensación de velocidad.
		tone := 0.22
		if s0.index%2 == 0 {
			tone = 0.26
		}
		d.addFace(&cam, []v3{side(s0, -drRoadHalf), side(s0, drRoadHalf), side(s1, drRoadHalf), side(s1, -drRoadHalf)}, tone, false, false, 0)
		// Los bordillos y el campo.
		for _, sg := range []float64{-1, 1} {
			ct := 0.6
			if s0.index%2 == 0 {
				ct = 0.3
			}
			e0, e1 := drRoadHalf*sg, (drRoadHalf+0.6)*sg
			d.addFace(&cam, orient([]v3{side(s0, e0), side(s0, e1), side(s1, e1), side(s1, e0)}, sg), ct, false, false, 0)
			f1 := (drRoadHalf + drVerge) * sg
			d.addFace(&cam, orient([]v3{side(s0, e1), side(s0, f1), side(s1, f1), side(s1, e1)}, sg), 0.08, false, false, 0)
		}
		// La raya del centro, discontinua (no junto a la cámara: tan cerca se
		// veía como un poste delante del coche).
		if s0.index%3 == 0 && i > drBehind {
			d.addFace(&cam, []v3{side(s0, -0.12).add(v3{0, 0.01, 0}), side(s0, 0.12).add(v3{0, 0.01, 0}), side(s1, 0.12).add(v3{0, 0.01, 0}), side(s1, -0.12).add(v3{0, 0.01, 0})}, 0.8, true, false, 0)
		}
		// Farolas cada 10 tramos, que destellan con los golpes.
		if s0.index%10 == 0 {
			for _, sg := range []float64{-1, 1} {
				base := side(s0, (drRoadHalf+1.8)*sg)
				for _, f := range box(base.add(v3{0, 2.5, 0}), v3{0.1, 2.5, 0.1}, 0.35) {
					d.addFace(&cam, f.pts, f.tone, false, true, 1)
				}
				lamp := box(base.add(v3{-0.5 * sg, 5.1, 0}), v3{0.5, 0.08, 0.15}, 0.9+0.6*pulse)
				for _, f := range lamp {
					d.addFace(&cam, f.pts, f.tone, true, true, 1)
				}
			}
		}
	}
	// El coche: con el derrape, el balanceo, el aplastamiento y el rebote.
	yaw := head + d.drift
	roll := d.roll - d.drift*0.15
	squash := 1 - 0.1*d.squash
	place := func(p v3) v3 {
		p.y *= squash
		p = rotZ(p, roll)
		p = rotY(p, yaw)
		return p.add(carPos).add(v3{0, d.bounceY, 0})
	}
	for _, f := range carModel {
		pts := d.scratch(len(f.pts))
		for i, p := range f.pts {
			pts[i] = place(p)
		}
		tone := f.tone * 1.35 // más claro que el entorno, para que resalte
		if f.tone < 0.3 {
			tone = f.tone * 0.6 // las ventanillas, más oscuras
		}
		if f.glow {
			tone = f.tone + 0.4*pulse // los faros destellan con los golpes
		}
		d.addFaceG(&cam, pts, tone, f.glow, true, 1, false, !f.glow)
	}
	for _, wp := range []v3{{-0.86, 0.34, 1.35}, {0.86, 0.34, 1.35}, {-0.86, 0.34, -1.35}, {0.86, 0.34, -1.35}} {
		steer := 0.0
		if wp.z > 0 {
			steer = -d.drift * 0.8 // las de delante, contravolante
		}
		for _, f := range wheelModel {
			pts := d.scratch(len(f.pts))
			for i, p := range f.pts {
				p = rotX(p, d.wheel)
				if wp.x < 0 {
					p.x = -p.x
				}
				p = rotY(p, steer)
				pts[i] = place(p.add(wp))
			}
			if wp.x < 0 { // el espejo da la vuelta a las caras
				for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
					pts[i], pts[j] = pts[j], pts[i]
				}
			}
			d.addFaceG(&cam, pts, f.tone, false, true, 1, false, true)
		}
	}
	// El humo: bocanadas redondas (octógonos de cara a la cámara) que se
	// abren, suben y se apagan poco a poco.
	for _, p := range d.smoke {
		k := p.age / p.life
		sz := 0.18 + 0.7*k
		c := cam.view(p.pos)
		if c.z < 0.5 {
			continue
		}
		tone := 0.45 * (1 - k) * (1 - k)
		const n = 8
		for i := 0; i < n; i++ {
			a0, a1 := 2*math.Pi*float64(i)/n, 2*math.Pi*float64(i+1)/n
			p0 := v3{c.x + sz*math.Cos(a0), c.y + sz*math.Sin(a0), c.z}
			p1 := v3{c.x + sz*math.Cos(a1), c.y + sz*math.Sin(a1), c.z}
			d.tris = append(d.tris, drTri{p: [3]v3{c, p0, p1}, tone: tone, depth: c.z, layer: 1})
		}
	}

	d.render(img, &cam)
}

// newWhite es la imagen blanca de la que salen los triángulos.
func newWhite() *ebiten.Image {
	w := ebiten.NewImage(3, 3)
	w.Fill(color.White)
	return w
}

// render pinta los triángulos en img: primero el suelo (capa 0), luego lo
// demás, de lejos a cerca.
func (d *r3d) render(img *ebiten.Image, cam *drCam) {
	// Se ordenan índices, no los triángulos: mover cada uno son más de cien
	// bytes.
	d.order = d.order[:0]
	for i := range d.tris {
		d.order = append(d.order, int32(i))
	}
	slices.SortFunc(d.order, func(i, j int32) int {
		a, b := &d.tris[i], &d.tris[j]
		if a.layer != b.layer {
			return a.layer - b.layer
		}
		return cmp.Compare(b.depth, a.depth)
	})
	d.verts, d.idx = d.verts[:0], d.idx[:0]
	flush := func() {
		if len(d.idx) > 0 {
			img.DrawTriangles(d.verts, d.idx, d.white, nil)
		}
		d.verts, d.idx = d.verts[:0], d.idx[:0]
	}
	for _, ti := range d.order {
		t := &d.tris[ti]
		if len(d.verts) > 65000 {
			flush()
		}
		var solid float32 // el canal verde marca lo que va sin trama
		if t.solid {
			solid = 1
		}
		base := uint16(len(d.verts))
		for k, p := range t.p {
			tone := t.tone
			if t.grad {
				tone = t.tones[k]
			}
			g := float32(math.Max(0, math.Min(1.5, tone)) / 1.5)
			sx := cam.w/2 + cam.focal*p.x/p.z
			sy := cam.h/2 - cam.focal*p.y/p.z
			d.verts = append(d.verts, ebiten.Vertex{DstX: float32(sx), DstY: float32(sy), SrcX: 1, SrcY: 1, ColorR: g, ColorG: solid, ColorB: 0, ColorA: 1})
		}
		d.idx = append(d.idx, base, base+1, base+2)
	}
	flush()
	d.drawn = len(d.tris)
	d.tris = d.tris[:0]
}

// orient da la vuelta a un cuadrilátero del lado izquierdo para que su cara
// mire hacia arriba igual que los del derecho.
func orient(q []v3, side float64) []v3 {
	if side < 0 {
		return []v3{q[3], q[2], q[1], q[0]}
	}
	return q
}

// gray es una intensidad (0..1,5, como las caras) en el color con que se
// pinta en los visualizadores antes de pasarlo a la paleta: va en el rojo; el
// verde marca lo que se pinta sin trama (solo el coche).
func gray(t float64) color.RGBA {
	g := uint8(math.Max(0, math.Min(1, t/1.5)) * 255)
	return color.RGBA{g, 0, 0, 255}
}
