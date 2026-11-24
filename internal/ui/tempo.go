package ui

import (
	"math"
	"math/cmplx"
)

// El tempo de la música (#14): antes salía de la mediana de los intervalos
// entre golpes seguidos, plegados a 70–180 BPM. Con golpes sincopados (una
// guitarra a contratiempo, swing) esos intervalos no son el tempo ni su doble
// o su mitad (2/3 de golpe se pliega a otro tempo), y la mediana iba y venía;
// y a 72 BPM, justo en el borde del pliegue, un intervalo un poco largo se
// partía a 144. Ahora se busca el periodo que más se repite en el flujo de
// todas las bandas (con solo las graves, el charles y la caja no contaban y
// en música real salía la mitad o 2/3), y en toda la canción: la
// autocorrelación de cada ventana se promedia con las anteriores (tempoMemory),
// así que una sección distinta (un puente, una estrofa sin batería) no
// cambia el tempo, y cuanto más suena la canción, más firme es.

const (
	tempoWindow  = 12.0 // segundos de flujo con los que se estima
	tempoMinSpan = 5.0  // los que hacen falta para la primera estimación
	tempoEvery   = 0.5  // cada cuánto se vuelve a estimar
	tempoBin     = 0.01 // el paso de la rejilla de la autocorrelación (s)
	tempoMinPer  = 60.0 / 180
	tempoMaxPer  = 1.0 // 60 BPM
	tempoMaxMult = 6   // el múltiplo más largo de los peines (tempoMeters)
	tempoConfirm = 8   // estimaciones seguidas (4 s) para cambiar de tempo
	tempoSame    = 0.05
	tempoMemory  = 45.0 // segundos que pesa lo oído en el promedio (constante de tiempo)
	tempoJumpN   = 24   // estimaciones seguidas (12 s) de otro tempo en la ventana para saltar a él
)

// envPoint es un punto del flujo: su hora (beatClock) y cuánto subió el
// espectro.
type envPoint struct{ t, f float64 }

// tempoAC es la autocorrelación del flujo env, pasado a una rejilla regular
// de tempoBin y sin su media, hasta el retraso del periodo más largo por el
// peine. nil si hay menos de tempoMinSpan segundos.
func tempoAC(env []envPoint) []float64 {
	if len(env) < 2 || env[len(env)-1].t-env[0].t < tempoMinSpan {
		return nil
	}
	x, _ := envGrid(env)
	n := len(x)
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(n)
	for i := range x {
		x[i] -= mean
	}
	// La autocorrelación por FFT (el espectro de potencia, de vuelta): en la
	// consola, con el doble bucle, costaba milisegundos cada medio segundo.
	// Sin pasar del 40 % de la ventana: un retraso más largo se estima con poca
	// música y sale ruidoso (por encima de ac[0]), e inflaba los tempos lentos.
	maxLag := min(int(tempoMaxMult*tempoMaxPer/tempoBin)+2, n*2/5)
	size := 1
	for size < n+maxLag+1 { // sin solaparse: basta el retraso máximo
		size *= 2
	}
	z := make([]complex128, size)
	for i, v := range x {
		z[i] = complex(v, 0)
	}
	fftInPlace(z, false)
	for i, v := range z {
		z[i] = complex(real(v)*real(v)+imag(v)*imag(v), 0)
	}
	fftInPlace(z, true)
	ac := make([]float64, maxLag+1)
	for l := range ac {
		ac[l] = real(z[l]) / float64(size) / float64(n-l)
	}
	return ac
}

// envGrid pasa el flujo env (al menos dos puntos) a una rejilla regular de
// tempoBin, interpolando; t0 es la hora de la primera casilla.
func envGrid(env []envPoint) (x []float64, t0 float64) {
	t0 = env[0].t
	x = make([]float64, int((env[len(env)-1].t-t0)/tempoBin)+1)
	j := 0
	for i := range x {
		t := t0 + float64(i)*tempoBin
		for j < len(env)-2 && env[j+1].t < t {
			j++
		}
		a, b := env[j], env[j+1]
		if b.t > a.t {
			u := math.Max(0, math.Min(1, (t-a.t)/(b.t-a.t)))
			x[i] = a.f + u*(b.f-a.f)
		} else {
			x[i] = a.f
		}
	}
	return x, t0
}

// fftInPlace es la FFT (inverse: la inversa, sin dividir por el tamaño) de
// z, cuyo tamaño es potencia de 2.
func fftInPlace(z []complex128, inverse bool) {
	n := len(z)
	for i, j := 1, 0; i < n; i++ { // orden de bits invertido
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			z[i], z[j] = z[j], z[i]
		}
	}
	sign := -1.0
	if inverse {
		sign = 1
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, sign*2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				a, b := z[start+k], z[start+k+size/2]*wk
				z[start+k], z[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}

// tempoMeters son los peines de los compases: los múltiplos del periodo que
// se repiten en un compás binario (2 y 4 golpes: el 4/4) y en uno ternario (3
// y 6: el 3/4 y el 6/8). Con un solo peine (1, 2 y 3 golpes) una canción
// sincopada en 4/4 salía a 2/3 de su tempo (Miss You de Trentemøller, 127 BPM,
// a 85: sus figuras de 3+3+2 se repiten cada golpe y medio), y con 1 a 4, una
// en 6/8 salía a 2/3 del suyo (3 Libras, 144, a 97).
var tempoMeters = [][]int{{1, 2, 4}, {1, 3, 6}}

// liveTernary es lo que pesa el compás ternario frente al binario en el tempo
// en vivo (en el mapa, 1: ahí se ve la canción entera y 3 Libras, en 6/8, le
// gana al binario por un 3 %). En vivo se decide con pocos segundos de
// música, y el 4/4 es mucho más común: con el swing (tresillos) un 72 binario
// se leía como un 108 ternario (el #14).
const liveTernary = 0.9

// combScore es lo que se repite el periodo per en ac con el peine comb (los
// múltiplos de tempoMeters): la media de su autocorrelación, relativa a la
// energía del flujo (ac[0]) y acotada a ±1. Los múltiplos que no caben en ac
// no cuentan; ok es false si no cupo ninguno.
func combScore(ac []float64, per float64, comb []int) (score float64, ok bool) {
	var s float64
	n := 0
	for _, k := range comb {
		l := float64(k) * per / tempoBin
		i := int(l)
		if i+1 >= len(ac) {
			break
		}
		u := l - float64(i)
		s += math.Max(-1, math.Min(1, (ac[i]+u*(ac[i+1]-ac[i]))/ac[0]))
		n++
	}
	if n == 0 {
		return 0, false
	}
	return s / float64(n), true
}

// tempoScore es lo que se repite el periodo per en ac en el compás que mejor
// le cuadra (combScore con cada peine de tempoMeters; el ternario,
// multiplicado por ternary), pesado por lo habitual que es ese tempo
// (tempoPrior).
func tempoScore(ac []float64, per, ternary float64) float64 {
	if len(ac) == 0 || ac[0] <= 0 {
		return 0
	}
	best := math.Inf(-1)
	for m, comb := range tempoMeters {
		if v, ok := combScore(ac, per, comb); ok {
			if m == 1 {
				v *= ternary
			}
			best = math.Max(best, v)
		}
	}
	if math.IsInf(best, -1) {
		return 0
	}
	return best * tempoPrior(per)
}

// meterOf es el compás del periodo per en ac: 3 golpes si el peine ternario le
// cuadra mejor (con su peso ternary), si no 4.
func meterOf(ac []float64, per, ternary float64) int {
	if len(ac) == 0 || ac[0] <= 0 {
		return 4
	}
	bin, ok := combScore(ac, per, tempoMeters[0])
	ter, ok3 := combScore(ac, per, tempoMeters[1])
	if ok3 && (!ok || ter*ternary > bin) {
		return 3
	}
	return 4
}

// tempoPrior es lo habitual que es un tempo: 1 a 120 BPM y menos cuanto más
// lejos, en octavas (una campana de una octava). Entre un tempo y su mitad
// que se repiten igual (un bombo en cada golpe también se repite cada dos),
// gana el más cercano a 120; uno lento solo gana si de verdad lo es (sin
// nada que suene entre medias).
func tempoPrior(per float64) float64 {
	o := math.Log2(per / 0.5)
	return math.Exp(-0.5 * o * o)
}

// bestTempo es el periodo de 60 a 180 BPM que más se repite en ac, y su
// puntuación.
func bestTempo(ac []float64, ternary float64) (per, score float64) {
	score = math.Inf(-1)
	for p := tempoMinPer; p <= tempoMaxPer; p += 0.002 {
		if s := tempoScore(ac, p, ternary); s > score {
			per, score = p, s
		}
	}
	// Afinado alrededor del mejor, a medio milisegundo.
	for p := per - 0.002; p <= per+0.002; p += 0.0005 {
		if s := tempoScore(ac, p, ternary); s > score {
			per, score = p, s
		}
	}
	return per, score
}

// sameBar dice si el tempo c es el mismo compás que per contado de otra forma:
// el doble, la mitad, 3/2 o 2/3 (o casi el mismo).
func sameBar(c, per float64) bool {
	for _, r := range []float64{1, 2, 0.5, 1.5, 2.0 / 3, 3, 1.0 / 3} {
		if math.Abs(c/per-r)/r < tempoSame {
			return true
		}
	}
	return false
}

// updateTempo vuelve a estimar el tempo con el flujo de los últimos
// tempoWindow segundos.
func (b *beat) updateTempo() {
	if n := len(b.env); n > 0 {
		cut := b.env[n-1].t - tempoWindow
		i := 0
		for i < n && b.env[i].t < cut {
			i++
		}
		b.env = b.env[i:]
	}
	if b.mapped {
		return // la rejilla la pone el mapa de tempo (mapBeat)
	}
	ac := tempoAC(b.env)
	if ac == nil || ac[0] <= 0 {
		return // poco flujo o silencio: no cuenta para el promedio
	}
	// El compás (las corcheas de beat.slot), con el tempo que quede.
	defer func() {
		if b.per > 0 && b.acc != nil {
			b.bar = 2 * meterOf(b.acc, b.per, liveTernary)
		}
	}()
	// El promedio de la canción, con cada ventana relativa a su energía (una
	// parte fuerte no pesa más que una suave).
	// Las ventanas no dan siempre la misma longitud (tempoAC la limita a su
	// tamaño, que varía en un punto): se promedia lo que tienen en común, sin
	// empezar de cero.
	if b.acc == nil {
		b.acc = make([]float64, len(ac))
		for i, v := range ac {
			b.acc[i] = v / ac[0]
		}
	} else {
		b.acc = b.acc[:min(len(b.acc), len(ac))]
		d := math.Exp(-tempoEvery / tempoMemory)
		for i := range b.acc {
			b.acc[i] = d*b.acc[i] + (1-d)*ac[i]/ac[0]
		}
	}
	now := b.env[len(b.env)-1].t
	anchor := now
	if n := len(b.onsets); n > 0 {
		anchor = b.onsets[n-1]
	}
	if b.tempoJump(ac, anchor) {
		return
	}
	ac = b.acc
	c, sc := bestTempo(ac, liveTernary)
	if sc < 0.05 { // nada se repite (silencio, un pasaje sin golpes): se queda
		return
	}
	switch {
	case b.per == 0:
		b.per, b.anchor, b.matched = c, anchor, anchor
		return
	case math.Abs(c-b.per)/b.per < tempoSame: // el mismo: se afina
		b.per += (c - b.per) * 0.25
		b.candN = 0
		return
	}
	// Otro tempo. Se queda el actual si sigue casi igual de bien, o si es el
	// mismo compás (el doble, la mitad...) y aún se oye. Mientras no hay una
	// ventana entera de música, el primer tempo es una apuesta: se corrige
	// sin esa protección.
	cur := tempoScore(ac, b.per, liveTernary)
	settled := now-b.envStart >= tempoWindow
	keep := 0.8
	if !settled {
		keep = 0.95
	}
	if cur >= keep*sc || (settled && sameBar(c, b.per) && cur >= 0.4*sc) {
		b.candN = 0
		return
	}
	if b.cand > 0 && math.Abs(c-b.cand)/b.cand < tempoSame {
		need := tempoConfirm
		if !settled {
			need = 2
		}
		if b.candN++; b.candN >= need { // confirmado: se cambia
			b.per, b.anchor, b.matched, b.candN = c, anchor, anchor, 0
		}
		return
	}
	b.cand, b.candN = c, 1
}

// tempoJump cambia de golpe a otro tempo cuando la canción cambia de verdad
// (una mezcla sin pausas, un tema con dos partes): el promedio de la canción
// tardaría medio minuto en olvidar el anterior. Hace falta que la ventana
// actual (ac, sin promediar) lleve tempoJumpN estimaciones seguidas en el
// mismo tempo nuevo, que no sea el mismo compás que el actual y que el actual
// ya casi no se oiga en ella. Entonces el promedio empieza de nuevo desde esa
// ventana. Dice si cambió.
func (b *beat) tempoJump(ac []float64, anchor float64) bool {
	if b.per == 0 {
		return false
	}
	c, sc := bestTempo(ac, liveTernary)
	if sc < 0.05 || sameBar(c, b.per) || tempoScore(ac, b.per, liveTernary) >= 0.35*sc {
		b.jumpN = 0
		return false
	}
	if b.jumpN == 0 || math.Abs(c-b.jump)/b.jump >= tempoSame {
		b.jump, b.jumpN = c, 1
		return false
	}
	if b.jumpN++; b.jumpN < tempoJumpN {
		return false
	}
	b.per, b.anchor, b.matched = c, anchor, anchor
	b.jumpN, b.candN = 0, 0
	b.acc = make([]float64, len(ac))
	for i, v := range ac {
		b.acc[i] = v / ac[0]
	}
	return true
}
