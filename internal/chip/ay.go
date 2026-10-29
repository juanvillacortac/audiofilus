// Package chip toca música de chips de sonido de ordenadores de 8 bits
// emulándolos en Go puro (sin bibliotecas en C, así funciona igual en las
// consolas, el PC, Windows y los teléfonos):
//
//   - el AY-3-8910 / YM2149 (ZX Spectrum, Amstrad CPC, MSX, Atari ST), con
//     los volcados de registros YM y VTX y los módulos de Pro Tracker 3 (PT3);
//   - el SID 6581/8580 del Commodore 64, con su 6502 y lo justo del C64 para
//     las rutinas de los PSID/RSID (los .sid de la HVSC).
//
// Cada archivo da una o varias canciones (Tracks); Open da una que suena
// (Tune) en estéreo a la frecuencia pedida.
package chip

import "math"

// El AY/YM se emula como ayumi, de Peter Sovietov (MIT, ver LICENSE.ayumi):
// los contadores de tono, ruido y envolvente avanzan a la frecuencia del chip
// entre 8, y las tablas del DAC son las suyas, medidas en chips de verdad.
// Para bajar a la frecuencia de salida, en vez de su FIR largo se promedian
// los pasos de cada muestra (unos 4 a 48 kHz): suena casi igual y cuesta
// mucho menos.

// ayDAC y ymDAC son el nivel de cada uno de los 32 pasos de volumen (el AY
// tiene 16 y los repite).
var (
	ayDAC = [32]float64{
		0.0, 0.0,
		0.00999465934234, 0.00999465934234,
		0.0144502937362, 0.0144502937362,
		0.0210574502174, 0.0210574502174,
		0.0307011520562, 0.0307011520562,
		0.0455481803616, 0.0455481803616,
		0.0644998855573, 0.0644998855573,
		0.107362478065, 0.107362478065,
		0.126588845655, 0.126588845655,
		0.20498970016, 0.20498970016,
		0.292210269322, 0.292210269322,
		0.372838941024, 0.372838941024,
		0.492530708782, 0.492530708782,
		0.635324635691, 0.635324635691,
		0.805584802014, 0.805584802014,
		1.0, 1.0,
	}
	ymDAC = [32]float64{
		0.0, 0.0,
		0.00465400167849, 0.00772106507973,
		0.0109559777218, 0.0139620050355,
		0.0169985503929, 0.0200198367285,
		0.024368657969, 0.029694056611,
		0.0350652323186, 0.0403906309606,
		0.0485389486534, 0.0583352407111,
		0.0680552376593, 0.0777752346075,
		0.0925154497597, 0.111085679408,
		0.129747463188, 0.148485542077,
		0.17666895552, 0.211551079576,
		0.246387426566, 0.281101701381,
		0.333730067903, 0.400427252613,
		0.467383840696, 0.53443198291,
		0.635172045472, 0.75800717174,
		0.879926756695, 1.0,
	}
)

// Las mezclas estéreo de los tres canales (cuánto va a la derecha): ABC es
// la del Spectrum (A a la izquierda, C a la derecha), sin llegar a los
// extremos para que con auriculares no canse; mono, todos al centro.
var (
	panMono = [3]float64{0.5, 0.5, 0.5}
	panABC  = [3]float64{0.2, 0.5, 0.8}
	panACB  = [3]float64{0.2, 0.8, 0.5}
	panBAC  = [3]float64{0.5, 0.2, 0.8}
	panBCA  = [3]float64{0.8, 0.2, 0.5}
	panCAB  = [3]float64{0.5, 0.8, 0.2}
	panCBA  = [3]float64{0.8, 0.5, 0.2}
)

// Las formas de la envolvente: qué hace en cada uno de sus dos tramos.
const (
	envDown = iota
	envUp
	envHoldBottom
	envHoldTop
)

// ayShapes son los dos tramos de cada una de las 16 formas.
var ayShapes = [16][2]int{
	{envDown, envHoldBottom}, {envDown, envHoldBottom}, {envDown, envHoldBottom}, {envDown, envHoldBottom},
	{envUp, envHoldBottom}, {envUp, envHoldBottom}, {envUp, envHoldBottom}, {envUp, envHoldBottom},
	{envDown, envDown}, {envDown, envHoldBottom}, {envDown, envUp}, {envDown, envHoldTop},
	{envUp, envUp}, {envUp, envHoldTop}, {envUp, envDown}, {envUp, envHoldBottom},
}

// ayChannel es un canal de tono.
type ayChannel struct {
	period, counter int
	tone            int  // 0 o 1
	tOff, nOff      int  // 1: tono o ruido apagados en el mezclador
	envOn           bool // el volumen lo da la envolvente
	volume          int  // 0-15
	pan             float64
	// override, si es >= 0, sustituye al volumen del canal (de 0 a 1): los
	// efectos del Atari (voz SID, digidrums) que el chip no hace solo.
	override float64
}

// ay es un AY-3-8910 o un YM2149.
type ay struct {
	ch                   [3]ayChannel
	noisePeriod, noiseCt int
	noise                int // el registro de 17 bits
	envPeriod, envCt     int
	envShape, envSeg     int
	env                  int // 0-31
	dac                  *[32]float64
	step                 float64 // pasos del chip (reloj/8) por muestra
	x                    float64 // fracción de paso acumulada
	regs                 [16]byte
	// mute dice qué canales no suenan (para los tests y para depurar).
	mute [3]bool
}

// newAY crea un chip de reloj clock Hz que da muestras a rate Hz. ym elige
// las tablas del YM2149 (32 pasos de envolvente) en vez de las del AY.
func newAY(clock float64, rate int, ym bool, pan [3]float64) *ay {
	a := &ay{dac: &ayDAC, step: clock / 8 / float64(rate), noise: 1}
	if ym {
		a.dac = &ymDAC
	}
	for i := range a.ch {
		a.ch[i].period, a.ch[i].pan, a.ch[i].override = 1, pan[i], -1
	}
	a.noisePeriod, a.envPeriod = 1, 1
	a.setShape(0)
	return a
}

// write escribe el registro r (0-13). La forma de la envolvente (13) la
// reinicia, también si es la misma.
func (a *ay) write(r int, v byte) {
	if r < 0 || r > 13 {
		return
	}
	a.regs[r] = v
	switch r {
	case 0, 1, 2, 3, 4, 5:
		i := r / 2
		p := (int(a.regs[i*2+1]&15)<<8 | int(a.regs[i*2])) & 0xfff
		a.ch[i].period = max(p, 1)
	case 6:
		a.noisePeriod = max(int(v&0x1f), 1)
	case 7:
		for i := range a.ch {
			a.ch[i].tOff = int(v>>i) & 1
			a.ch[i].nOff = int(v>>(i+3)) & 1
		}
	case 8, 9, 10:
		a.ch[r-8].volume = int(v & 15)
		a.ch[r-8].envOn = v&0x10 != 0
	case 11, 12:
		a.envPeriod = max(int(a.regs[12])<<8|int(a.regs[11]), 1)
	case 13:
		a.setShape(int(v & 15))
	}
}

func (a *ay) setShape(s int) {
	a.envShape, a.envCt, a.envSeg = s, 0, 0
	a.resetSegment()
}

// resetSegment pone la envolvente al principio de su tramo: arriba si baja
// o se queda arriba, abajo si sube o se queda abajo.
func (a *ay) resetSegment() {
	switch ayShapes[a.envShape][a.envSeg] {
	case envDown, envHoldTop:
		a.env = 31
	default:
		a.env = 0
	}
}

// tick avanza un paso del chip (reloj/8) y da la salida izquierda y derecha.
func (a *ay) tick() (l, r float64) {
	// El ruido: cambia cada 2 periodos; el registro de 17 bits.
	a.noiseCt++
	if a.noiseCt >= a.noisePeriod<<1 {
		a.noiseCt = 0
		bit := (a.noise ^ a.noise>>3) & 1
		a.noise = a.noise>>1 | bit<<16
	}
	noise := a.noise & 1
	// La envolvente.
	a.envCt++
	if a.envCt >= a.envPeriod {
		a.envCt = 0
		switch ayShapes[a.envShape][a.envSeg] {
		case envDown:
			a.env--
			if a.env < 0 {
				a.envSeg ^= 1
				a.resetSegment()
			}
		case envUp:
			a.env++
			if a.env > 31 {
				a.envSeg ^= 1
				a.resetSegment()
			}
		}
	}
	for i := range a.ch {
		c := &a.ch[i]
		c.counter++
		if c.counter >= c.period {
			c.counter = 0
			c.tone ^= 1
		}
		if a.mute[i] {
			continue
		}
		// El tono y el ruido abren o cierran el canal; abierto, suena su
		// volumen (o la envolvente, o el efecto).
		var v float64
		switch {
		case (c.tone|c.tOff)&(noise|c.nOff) == 0:
		case c.override >= 0:
			v = c.override
		case c.envOn:
			v = a.dac[a.env]
		default:
			v = a.dac[c.volume*2+1]
		}
		l += v * (1 - c.pan)
		r += v * c.pan
	}
	return l, r
}

// sample da la siguiente muestra: el promedio de los pasos del chip que caen
// en ella.
func (a *ay) sample() (l, r float64) {
	a.x += a.step
	n := int(a.x)
	a.x -= float64(n)
	for i := 0; i < n; i++ {
		tl, tr := a.tick()
		l += tl
		r += tr
	}
	if n > 0 {
		l /= float64(n)
		r /= float64(n)
	}
	return l, r
}

// dcBlock quita la continua (el AY solo da valores positivos): un paso alto
// de un polo muy grave.
type dcBlock struct{ x, y float64 }

func (d *dcBlock) next(x float64) float64 {
	d.y = x - d.x + 0.9995*d.y
	d.x = x
	return d.y
}

// clamp1 deja v entre -1 y 1.
func clamp1(v float64) float32 {
	return float32(math.Max(-1, math.Min(1, v)))
}
