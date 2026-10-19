package ui

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// El medidor del servicio (engine/meter.go) llega con el estado cuatro veces
// por segundo: puntos con la hora a la que se oyen. Aquí se toma el del
// instante actual (interpolando entre dos), así el visualizador y el tetris van
// a tiempo con lo que suena aunque el estado llegue a saltos.

// meterNow son el nivel, los graves y el espectro (0..1) de lo que suena ahora;
// ok es false si el servicio no mandó medidor (en pausa o sin servicio).
func (a *App) meterNow() (level, bass float64, bands [engine.MeterBands]float64, ok bool) {
	if a.offline && a.isPlaying {
		return a.fakeMeter()
	}
	pts := a.status.Meter
	if len(pts) == 0 || !a.isPlaying {
		return 0, 0, bands, false
	}
	now := time.Now().UnixMilli()
	i := 0
	for i < len(pts)-1 && pts[i+1].At <= now {
		i++
	}
	p0, p1 := pts[i], pts[min(i+1, len(pts)-1)]
	f := 0.0
	if p1.At > p0.At {
		f = math.Max(0, math.Min(1, float64(now-p0.At)/float64(p1.At-p0.At)))
	}
	lerp := func(x, y byte) float64 { return (float64(x) + (float64(y)-float64(x))*f) / 255 }
	level, bass = lerp(p0.Level, p1.Level), lerp(p0.Bass, p1.Bass)
	if len(p0.Bands) == engine.MeterBands && len(p1.Bands) == engine.MeterBands {
		for b := range bands {
			bands[b] = lerp(p0.Bands[b], p1.Bands[b])
		}
	}
	return level, bass, bands, true
}

// beat sigue el ritmo de la canción, como un juego de ritmo:
//
//   - Detección de golpes (onsets): por cada punto del medidor (uno cada
//     ~21 ms, con su hora exacta), cuánto subieron las bandas graves y medias
//     respecto al punto anterior (flujo espectral). Es un golpe si pasa de la
//     media más 1,5 desviaciones del último segundo, con 0,25 s entre golpes.
//   - Tempo: los intervalos entre golpes se pliegan a 70–180 BPM (un golpe
//     perdido o uno de más no cambian el tempo) y se toma la mediana. Solo
//     cambia si el nuevo se repite varias veces.
//   - Rejilla: con el tempo, los golpes salen de una rejilla regular, que
//     sigue aunque la música baje (no se pierde); los golpes detectados solo
//     corrigen su fase. Sin tempo aún, cuenta cada golpe detectado.
//
// Al cambiar de canción se empieza de cero (resetBeat).
type beat struct {
	at    float64 // el último golpe (de la rejilla, o detectado), en beatClock
	count int     // golpes desde el principio (para alternar colores)

	last   int64 // el último punto del medidor procesado (ms Unix)
	prev   [engine.MeterBands]float64
	prevB  float64
	flux   []float64 // el flujo del último segundo, para el umbral
	onsets []float64 // los últimos golpes detectados (beatClock)

	per     float64 // el tempo: segundos por golpe (0 si aún no se sabe)
	cand    float64 // un tempo nuevo que se está confirmando
	candN   int
	anchor  float64 // un golpe de la rejilla (beatClock)
	matched float64 // el último golpe detectado que cuadró con la rejilla

	drums drums // la batería: qué suena en cada corchea del compás
}

// Los instrumentos de la batería que se reconocen, uno por carril de Ritmo.
const (
	drumKick  = iota // bombo: graves
	drumSnare        // caja: medios
	drumHat          // charles: agudos
	drumCrash        // platillos: agudos muy fuertes
	drumCount
)

// drums reconoce los golpes de cada instrumento de la batería (cuánto suben
// sus bandas, con su umbral) y apunta en qué corchea del compás de 4 tiempos
// suena cada uno. La música repite sus patrones: lo que tocó la batería en los
// últimos compases es lo que viene, y de ahí salen las notas de Ritmo (que
// tienen que aparecer antes de sonar).
type drums struct {
	flux    [drumCount][]float64  // el flujo del último segundo de cada uno
	last    [drumCount]float64    // su último golpe
	hit     [drumCount][8]float64 // cuándo sonó en cada corchea del compás
	hatPeak float64               // la fuerza habitual de un golpe de charles
	any     bool                  // ya se apuntó algo
}

// drumBands son las bandas de cada instrumento (de las 16 del medidor).
var drumBands = [drumCount][2]int{{0, 3}, {4, 9}, {11, 16}, {11, 16}}

// slot es la corchea del compás (0..7) en la que cae t, según la rejilla.
func (b *beat) slot(t float64) int {
	s := int(math.Round((t - b.anchor) / (b.per / 2)))
	return ((s % 8) + 8) % 8
}

// add mira un punto del medidor (cuánto subió cada banda) en la hora t.
func (d *drums) add(t float64, rise [engine.MeterBands]float64, b *beat) {
	var f [drumCount]float64
	for k := 0; k < drumCrash; k++ {
		for i := drumBands[k][0]; i < drumBands[k][1]; i++ {
			f[k] += rise[i]
		}
	}
	for k := 0; k < drumCrash; k++ {
		var mean, sd float64
		for _, x := range d.flux[k] {
			mean += x
		}
		n := len(d.flux[k])
		if n > 0 {
			mean /= float64(n)
			for _, x := range d.flux[k] {
				sd += (x - mean) * (x - mean)
			}
			sd = math.Sqrt(sd / float64(n))
		}
		d.flux[k] = append(d.flux[k], f[k])
		if n+1 > 48 {
			d.flux[k] = d.flux[k][1:]
		}
		if n < 12 || f[k] < mean+1.6*sd+0.015 || t-d.last[k] < 0.1 || b.per <= 0 {
			continue
		}
		kind := k
		if k == drumHat {
			// Un agudo mucho más fuerte que un charles normal: platillo.
			switch {
			case d.hatPeak == 0:
				d.hatPeak = f[k]
			case f[k] > 2.2*d.hatPeak:
				kind = drumCrash
			default:
				d.hatPeak += (f[k] - d.hatPeak) * 0.2
			}
		}
		d.last[k] = t
		d.hit[kind][b.slot(t)] = t
		d.any = true
	}
}

// at dice qué instrumentos sonaron en la corchea slot en los últimos dos
// compases (8 tiempos) antes de t.
func (d *drums) at(slot int, t, per float64) [drumCount]bool {
	var out [drumCount]bool
	for k := range out {
		h := d.hit[k][slot]
		out[k] = h > 0 && t-h <= 8*per+per/2
	}
	return out
}

// resetBeat empieza el ritmo de cero (canción nueva).
func (a *App) resetBeat() { a.beat = beat{} }

// foldPeriod lleva un intervalo a 0,33–0,86 s (70–180 BPM) doblándolo o
// partiéndolo.
func foldPeriod(d float64) float64 {
	for d > 0 && d < 0.33 {
		d *= 2
	}
	for d > 0.86 {
		d /= 2
	}
	return d
}

// period es el tempo (segundos por golpe), o 0 si aún no se sabe.
func (b *beat) period() float64 { return b.per }

// BPM es el tempo estimado (0 si aún no se sabe).
func (b *beat) BPM() int {
	if b.per > 0 {
		return int(60/b.per + 0.5)
	}
	return 0
}

// onBeat dice si t cae a menos de tol de un golpe (de la rejilla, si la hay).
func (b *beat) onBeat(t, tol float64) bool {
	if b.per > 0 {
		k := math.Round((t - b.anchor) / b.per)
		return math.Abs(t-(b.anchor+k*b.per)) <= tol
	}
	return b.at > 0 && math.Abs(t-b.at) <= tol
}

// onset registra un golpe detectado en t: corrige la fase de la rejilla y
// el tempo.
func (b *beat) onset(t float64) {
	b.onsets = append(b.onsets, t)
	if len(b.onsets) > 12 {
		b.onsets = b.onsets[1:]
	}
	if b.per > 0 {
		k := math.Round((t - b.anchor) / b.per)
		g := b.anchor + k*b.per
		switch {
		case math.Abs(t-g) < 0.07: // cuadra: se corrige la fase un poco
			b.anchor += 0.3 * (t - g) // sin mover el ancla de compás
			b.matched = t
		case t-b.matched > 4: // hace rato que nada cuadra: se vuelve a anclar
			b.anchor, b.matched = t, t
		}
	}
	// El tempo: la mediana de los intervalos plegados.
	var iv []float64
	for i := 1; i < len(b.onsets); i++ {
		if d := foldPeriod(b.onsets[i] - b.onsets[i-1]); d > 0 {
			iv = append(iv, d)
		}
	}
	if len(iv) < 4 {
		return
	}
	sort.Float64s(iv)
	c := iv[len(iv)/2]
	// Afinado con todo el tramo: los puntos del medidor van cada ~21 ms, y un
	// intervalo suelto lleva ese error; muchos golpes seguidos, no.
	if span := t - b.onsets[0]; span > 4*c {
		c = span / math.Round(span/c)
	}
	switch {
	case b.per == 0:
		b.per, b.anchor, b.matched = c, t, t
	case math.Abs(c-b.per)/b.per < 0.06:
		b.per += (c - b.per) * 0.2 // el mismo tempo: se afina
		b.candN = 0
	case b.cand > 0 && math.Abs(c-b.cand)/b.cand < 0.06:
		if b.candN++; b.candN >= 4 { // otro tempo, confirmado: se cambia
			b.per, b.anchor, b.matched, b.candN = c, t, t, 0
		}
	default:
		b.cand, b.candN = c, 1
	}
}

// updateBeat procesa los puntos nuevos del medidor y dice si en este
// fotograma hay un golpe (de la rejilla o, sin tempo, detectado).
func (a *App) updateBeat() bool {
	b := &a.beat
	if a.offline && a.isPlaying {
		return a.fakeBeat()
	}
	clock := a.beatClock()
	if !a.isPlaying {
		return false
	}
	now := time.Now().UnixMilli()
	detected := false
	for _, p := range a.status.Meter {
		if p.At <= b.last || p.At > now {
			continue
		}
		b.last = p.At
		bass := float64(p.Bass) / 255
		var f float64
		var rise [engine.MeterBands]float64 // cuánto subió cada banda
		if len(p.Bands) == engine.MeterBands {
			for k := 0; k < engine.MeterBands; k++ {
				v := float64(p.Bands[k]) / 255
				rise[k] = math.Max(0, v-b.prev[k])
				b.prev[k] = v
				if k < 8 { // graves y medios: bombo, caja, bajo
					f += rise[k]
				}
			}
		}
		f += 0.5 * math.Max(0, bass-b.prevB)
		b.prevB = bass
		// El umbral: la media y la desviación del último segundo.
		var mean, sd float64
		for _, x := range b.flux {
			mean += x
		}
		if len(b.flux) > 0 {
			mean /= float64(len(b.flux))
			for _, x := range b.flux {
				sd += (x - mean) * (x - mean)
			}
			sd = math.Sqrt(sd / float64(len(b.flux)))
		}
		b.flux = append(b.flux, f)
		if len(b.flux) > 48 {
			b.flux = b.flux[1:]
		}
		t := float64(p.At) / 1000 // la hora a la que se oye
		b.drums.add(t, rise, b)
		lastOnset := -1.0
		if n := len(b.onsets); n > 0 {
			lastOnset = b.onsets[n-1]
		}
		if len(b.flux) >= 12 && f > mean+1.5*sd+0.02 && (lastOnset < 0 || t-lastOnset > 0.25) {
			b.onset(t)
			detected = true
		}
	}
	// Con tempo, el golpe lo marca la rejilla (sigue aunque la música baje);
	// sin tempo, cada golpe detectado.
	hit := false
	if b.per > 0 {
		k := math.Floor((clock - b.anchor) / b.per)
		g := b.anchor + k*b.per
		if g > b.at+b.per/2 {
			hit = true
			b.at = g
		}
	} else if detected {
		hit = true
		b.at = clock
	}
	if hit {
		b.count++
	}
	return hit
}

// fakeBeat es el ritmo de la maqueta: 120 BPM exactos.
func (a *App) fakeBeat() bool {
	b := &a.beat
	b.per, b.anchor = 0.5, 0
	g := math.Floor(a.t/0.5) * 0.5
	if g > b.at+0.25 || b.at == 0 {
		b.at = g
		b.count++
		return true
	}
	return false
}

// followGrid acerca una rejilla de ritmo (per, anchor) a la del medidor (tp,
// ta): el tempo y la fase poco a poco, sin saltos que se vean ni se sientan, y
// de golpe solo si el tempo es otro de verdad (otra canción). Sin tempo del
// medidor (tp 0) se queda como está.
func followGrid(per, anchor *float64, tp, ta, now float64) {
	// El ancla, al último golpe: así cambiar el periodo no mueve los cercanos.
	*anchor += math.Floor((now-*anchor) / *per) * *per
	if tp == 0 {
		return
	}
	if math.Abs(tp-*per) / *per > 0.08 {
		*per, *anchor = tp, ta+math.Floor((now-ta)/tp)*tp
		return
	}
	*per += (tp - *per) * 0.02
	e := math.Mod(ta-*anchor, *per) // el desfase, entre -per/2 y per/2
	if e > *per/2 {
		e -= *per
	} else if e < -*per/2 {
		e += *per
	}
	*anchor += e * 0.05
}

// steadyGrid es la rejilla suave de los juegos (Bloques y Ritmo; Cripta
// lleva la suya igual): sigue al medidor con followGrid, una vez por
// fotograma. per es 0 mientras el medidor no haya dado ningún tempo.
type steadyGrid struct {
	per, anchor float64
	at          float64 // el fotograma de la última vez
}

// steadyBeat da la rejilla suave, al día. Sin música sonando no hay golpes
// (per 0): la rejilla se guarda, pero no se inventan golpes en silencio.
func (a *App) steadyBeat() (per, anchor float64) {
	if !a.isPlaying && !a.offline {
		return 0, 0
	}
	s := &a.steady
	if s.at == a.t && s.per > 0 {
		return s.per, s.anchor
	}
	s.at = a.t
	tp, ta := a.beat.period(), a.beat.anchor
	switch {
	case s.per == 0 && tp > 0:
		s.per, s.anchor = tp, ta
	case s.per > 0:
		followGrid(&s.per, &s.anchor, tp, ta, a.beatClock())
	}
	return s.per, s.anchor
}

// steadyOnBeat dice si ahora cae a menos de tol de un golpe de la rejilla
// suave.
func (a *App) steadyOnBeat(tol float64) bool {
	per, anchor := a.steadyBeat()
	if per == 0 {
		return false
	}
	now := a.beatClock()
	g := anchor + math.Round((now-anchor)/per)*per
	return math.Abs(now-g) <= tol
}

// beatPulse va de 1 (justo en el golpe) a 0 en 0,25 s.
func (a *App) beatPulse() float64 {
	if a.beat.at == 0 {
		return 0
	}
	return math.Max(0, math.Min(1, 1-(a.beatClock()-a.beat.at)/0.25))
}

// beatClock es el reloj del ritmo, en segundos: la hora real (los puntos del
// medidor la llevan, y el reloj de la interfaz se atrasa si un fotograma
// tarda); en la maqueta, el de la interfaz, que es el que avanza.
func (a *App) beatClock() float64 {
	if a.offline {
		return a.t
	}
	return float64(time.Now().UnixMilli()) / 1000
}

// updatePump hace que el fondo reaccione a la música: la primera mancha de
// luz con los graves, la segunda con los agudos (suben rápido y bajan
// despacio) y una onda en cada golpe.
func (a *App) updatePump() {
	// Con fuerza en Reproduciendo y Ritmo, donde manda la música; más suave
	// en el resto, para no molestar al leer.
	gain := 0.55
	if a.screen == NowPlaying || a.screen == Rhythm || a.screen == Crypt || a.screen == Snake || a.screen == Shooter || a.screen == Visual {
		gain = 1
	}
	var bass, treble float64
	if _, b, bands, ok := a.meterNow(); ok {
		bass = math.Min(1, b*1.6) * gain
		for k := 10; k < engine.MeterBands; k++ {
			treble += bands[k]
		}
		treble = math.Min(1, treble/float64(engine.MeterBands-10)*1.3) * gain
	}
	// Sube rápido y baja despacio.
	follow := func(v *float32, target float64) {
		p := float64(*v)
		if target > p {
			p += (target - p) * 0.35
		} else {
			p += (target - p) * 0.06
		}
		*v = float32(p)
	}
	follow(&bgPump, bass)
	follow(&bgTreble, treble)
	bgSince = 9
	if a.beat.at > 0 && a.isPlaying {
		bgSince = float32(math.Max(0, a.beatClock()-a.beat.at) / gain)
	}
	// Cada golpe lanza la onda desde un sitio distinto (lejos de los bordes y
	// del anterior): siempre desde el mismo punto se veía repetitivo.
	if a.beat.count != bgRippleBeat {
		bgRippleBeat = a.beat.count
		r := rand.New(rand.NewPCG(uint64(a.beat.count), 99))
		for i := 0; i < 4; i++ {
			x, y := 0.15+0.7*r.Float64(), 0.15+0.7*r.Float64()
			if math.Hypot(x-float64(bgRipple[0]), y-float64(bgRipple[1])) > 0.3 {
				bgRipple = [2]float32{float32(x), float32(y)}
				break
			}
		}
	}
}

// fakeMeter es un medidor inventado para la maqueta (capturas, sin servicio):
// un bombo a 120 BPM y un espectro que se mueve, para ver las reacciones.
func (a *App) fakeMeter() (level, bass float64, bands [engine.MeterBands]float64, ok bool) {
	kick := math.Exp(-math.Mod(a.t, 0.5) * 9)
	for b := range bands {
		f := float64(b) / engine.MeterBands
		v := 0.5 + 0.5*math.Sin(a.t*3.1+f*9)
		v *= 0.45 + 0.55*(0.5+0.5*math.Sin(a.t*1.7+f*23))
		bands[b] = v*(1-0.55*f) + kick*0.5*(1-f)
	}
	return 0.6, 0.15 + 0.6*kick, bands, true
}
