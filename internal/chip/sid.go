package chip

import "math"

// El SID (MOS 6581 y 8580) del Commodore 64: tres voces (oscilador de 24
// bits con triangular, sierra, pulso y ruido, sincronía y modulación en
// anillo, y su envolvente ADSR) y un filtro de estado variable (paso bajo,
// banda y alto) con resonancia. Es un modelo propio, por pasos de unos pocos
// ciclos (no ciclo a ciclo como reSID), que se apoya en lo que se sabe del
// chip: los periodos del contador de la envolvente, su bajada exponencial,
// el error del contador (el "ADSR bug") y el registro de ruido de 23 bits.
// Las formas combinadas son el Y de las simples, que se acerca a las reales.
// El volumen general suma una continua en el 6581, así que los digis (las
// muestras que se tocan escribiendo el volumen) suenan; en el 8580, casi no.

// sidRates son los ciclos entre pasos de la envolvente de cada valor de
// ataque, caída o relajación.
var sidRates = [16]int{9, 32, 63, 95, 149, 220, 267, 313, 392, 977, 1954, 3126, 3907, 11720, 19532, 31251}

// Los estados de la envolvente.
const (
	envAttack = iota
	envDecay  // caída y sostenido
	envRelease
)

// Los bits del registro de control de una voz.
const (
	ctrlGate  = 0x01
	ctrlSync  = 0x02
	ctrlRing  = 0x04
	ctrlTest  = 0x08
	ctrlTri   = 0x10
	ctrlSaw   = 0x20
	ctrlPulse = 0x40
	ctrlNoise = 0x80
)

// sidVoice es una voz.
type sidVoice struct {
	freq, pw uint32
	ctrl     byte
	ad, sr   byte
	acc      uint32 // el acumulador de fase, de 24 bits
	msbUp    bool   // su bit alto acaba de subir (para la sincronía)
	lfsr     uint32 // el ruido
	state    int
	env      int // 0-255
	rateCt   int // el contador de 15 bits
	expCt    int
	expPer   int
}

// rate es el periodo del estado actual.
func (v *sidVoice) rate() int {
	switch v.state {
	case envAttack:
		return sidRates[v.ad>>4]
	case envDecay:
		return sidRates[v.ad&15]
	}
	return sidRates[v.sr&15]
}

// envStep es un paso de la envolvente.
func (v *sidVoice) envStep() {
	switch v.state {
	case envAttack:
		v.env = (v.env + 1) & 0xff
		if v.env == 0xff {
			v.state = envDecay
		}
		v.expCt = 0
		return
	case envDecay:
		if v.env == int(v.sr>>4)*0x11 {
			return
		}
	}
	v.expCt++
	if v.expCt < v.expPer {
		return
	}
	v.expCt = 0
	if v.env > 0 {
		v.env--
	}
	switch v.env {
	case 0xff:
		v.expPer = 1
	case 0x5d:
		v.expPer = 2
	case 0x36:
		v.expPer = 4
	case 0x1a:
		v.expPer = 8
	case 0x0e:
		v.expPer = 16
	case 0x06:
		v.expPer = 30
	case 0x00:
		v.expPer = 1
	}
}

// clockEnv avanza la envolvente n ciclos. El contador cuenta hasta el
// periodo y vuelve a 0; si el periodo baja por debajo de donde va, tiene
// que dar la vuelta entera (los 15 bits) antes del siguiente paso: es el
// error del chip que los músicos evitan con el "hard restart".
func (v *sidVoice) clockEnv(n int) {
	for n > 0 {
		if v.state == envRelease && v.env == 0 || v.state == envDecay && v.env == 0 {
			v.rateCt = (v.rateCt + n) & 0x7fff
			return
		}
		p := v.rate()
		d := p - v.rateCt
		if d <= 0 {
			d += 0x8000
		}
		if n < d {
			v.rateCt = (v.rateCt + n) & 0x7fff
			return
		}
		n -= d
		v.rateCt = 0
		v.envStep()
	}
}

// setCtrl escribe el registro de control (la puerta empieza o suelta la
// nota; el bit de prueba para el oscilador y llena el ruido).
func (v *sidVoice) setCtrl(c byte) {
	if c&ctrlGate != 0 && v.ctrl&ctrlGate == 0 {
		v.state = envAttack
	} else if c&ctrlGate == 0 && v.ctrl&ctrlGate != 0 {
		v.state = envRelease
	}
	if c&ctrlTest != 0 {
		v.acc = 0
		v.lfsr = 0x7fffff
	}
	v.ctrl = c
}

// wave es la salida de la forma de onda (12 bits); ringMSB es el bit alto
// del acumulador de la voz que la modula.
func (v *sidVoice) wave(ringMSB uint32) uint32 {
	out := uint32(0xfff)
	if v.ctrl&0xf0 == 0 {
		return 0
	}
	if v.ctrl&ctrlTri != 0 {
		msb := v.acc & 0x800000
		if v.ctrl&ctrlRing != 0 {
			msb ^= ringMSB
		}
		t := v.acc
		if msb != 0 {
			t = ^t
		}
		out &= t >> 11 & 0xfff
	}
	if v.ctrl&ctrlSaw != 0 {
		out &= v.acc >> 12
	}
	if v.ctrl&ctrlPulse != 0 {
		if v.ctrl&ctrlTest == 0 && v.acc>>12 < v.pw {
			out = 0
		}
	}
	if v.ctrl&ctrlNoise != 0 {
		l := v.lfsr
		n := (l>>20&1)<<11 | (l>>18&1)<<10 | (l>>14&1)<<9 | (l>>11&1)<<8 |
			(l>>9&1)<<7 | (l>>5&1)<<6 | (l>>2&1)<<5 | (l&1)<<4
		out &= n
	}
	return out
}

// sid es un chip.
type sid struct {
	v      [3]sidVoice
	fc     int  // corte, 11 bits
	resFlt byte // resonancia (alto) y qué voces van al filtro (bajo)
	modeV  byte // modos del filtro (alto) y volumen (bajo)
	is8580 bool
	lp, bp float64
	pan    float64
	// w1 es el coeficiente del filtro por ciclo; damp, su amortiguación.
	w1, damp float64
	regs     [32]byte
}

func newSID(is8580 bool, pan float64) *sid {
	s := &sid{is8580: is8580, pan: pan}
	for i := range s.v {
		s.v[i].lfsr = 0x7fffff
		s.v[i].expPer = 1
		s.v[i].state = envRelease
	}
	s.filterCoef()
	return s
}

// sidClockPAL y sidClockNTSC son los relojes del C64.
const (
	sidClockPAL  = 985248
	sidClockNTSC = 1022727
)

// filterCoef calcula el filtro desde sus registros. El 8580 corta casi en
// línea recta de 30 Hz a 12 kHz; el 6581, en curva (exponencial aquí) de
// unos 220 Hz a 18 kHz, con mucho de su recorrido en los graves.
func (s *sid) filterCoef() {
	x := float64(s.fc) / 2047
	var hz float64
	if s.is8580 {
		hz = 30 + x*12000
	} else {
		hz = 220 * math.Pow(18000.0/220, x)
	}
	s.w1 = 2 * math.Pi * hz / sidClockPAL
	res := float64(s.resFlt >> 4)
	s.damp = 1.41 - res*(1.41-0.25)/15
}

// write escribe el registro r (0-31).
func (s *sid) write(r int, val byte) {
	r &= 31
	s.regs[r] = val
	if r < 21 {
		v := &s.v[r/7]
		switch r % 7 {
		case 0:
			v.freq = v.freq&0xff00 | uint32(val)
		case 1:
			v.freq = v.freq&0xff | uint32(val)<<8
		case 2:
			v.pw = v.pw&0xf00 | uint32(val)
		case 3:
			v.pw = v.pw&0xff | uint32(val&15)<<8
		case 4:
			v.setCtrl(val)
		case 5:
			v.ad = val
		case 6:
			v.sr = val
		}
		return
	}
	switch r {
	case 21:
		s.fc = s.fc&^7 | int(val&7)
		s.filterCoef()
	case 22:
		s.fc = s.fc&7 | int(val)<<3
		s.filterCoef()
	case 23:
		s.resFlt = val
		s.filterCoef()
	case 24:
		s.modeV = val
	}
}

// read lee un registro: solo la salida del oscilador 3 y su envolvente se
// leen (las rutinas los usan como números al azar o para efectos); el resto
// da lo último escrito (en el chip, lo que quede en el bus).
func (s *sid) read(r int) byte {
	switch r & 31 {
	case 27:
		return byte(s.v[2].wave(s.v[1].acc&0x800000) >> 4)
	case 28:
		return byte(s.v[2].env)
	}
	return s.regs[r&31]
}

// sidSrc es la voz que sincroniza o modula a cada una (la anterior).
var sidSrc = [3]int{2, 0, 1}

// clock avanza n ciclos y da la salida (de -1 a 1, más o menos).
func (s *sid) clock(n int) float64 {
	for i := range s.v {
		v := &s.v[i]
		v.msbUp = false
		if v.ctrl&ctrlTest != 0 {
			continue
		}
		prev := v.acc
		next := prev + v.freq*uint32(n)
		v.msbUp = prev&0x800000 == 0 && next&0x800000 != 0
		// El ruido avanza cada vez que sube el bit 19.
		for k := (int64(next) - 0x80000) >> 20; k > (int64(prev)-0x80000)>>20; k-- {
			b := (v.lfsr>>22 ^ v.lfsr>>17) & 1
			v.lfsr = (v.lfsr<<1 | b) & 0x7fffff
		}
		v.acc = next & 0xffffff
	}
	for i := range s.v {
		v := &s.v[i]
		if v.ctrl&ctrlSync != 0 && s.v[sidSrc[i]].msbUp {
			v.acc = 0
		}
		v.clockEnv(n)
	}
	var direct, filt float64
	for i := range s.v {
		v := &s.v[i]
		w := v.wave(s.v[sidSrc[i]].acc & 0x800000)
		var o float64
		if v.ctrl&0xf0 != 0 {
			o = (float64(w)/4095 - 0.5) * float64(v.env) / 255
		}
		switch {
		case s.resFlt>>i&1 != 0:
			filt += o
		case i == 2 && s.modeV&0x80 != 0: // la voz 3 apagada
		default:
			direct += o
		}
	}
	// El filtro de estado variable (Chamberlin), con el coeficiente para n
	// ciclos.
	w := min(s.w1*float64(n), 1.2)
	hp := filt - s.lp - s.damp*s.bp
	s.bp += w * hp
	s.lp += w * s.bp
	out := direct
	if s.modeV&0x10 != 0 {
		out += s.lp
	}
	if s.modeV&0x20 != 0 {
		out += s.bp
	}
	if s.modeV&0x40 != 0 {
		out += hp
	}
	vol := float64(s.modeV&15) / 15
	dc := 0.0
	if !s.is8580 {
		dc = 0.35 // la continua del 6581, que hace sonar los digis
	} else {
		dc = 0.03
	}
	return (out + dc) * vol
}
