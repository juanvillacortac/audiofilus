package tracker

import "math"

// Las voces: lo que suena en cada canal. En el modo Game Boy imitan la APU
// de la consola: los pulsos con sus cuatro anchos, la envolvente de
// volumen a 64 Hz y el barrido de PU1; la onda con sus 32 muestras de 4
// bits y su volumen a desplazamientos (100, 50, 25 %); el ruido con su
// registro de 15 o 7 bits; y todas con la frecuencia redondeada a los
// periodos de 11 bits del hardware. En el modo Libre son osciladores sin
// esas restricciones, con una envolvente ADSR.

// dutyPatterns son los ocho pasos de cada ancho de pulso de la GB.
var dutyPatterns = [4][8]float64{
	{-1, -1, -1, -1, -1, -1, -1, 1}, // 12,5 %
	{1, -1, -1, -1, -1, -1, -1, 1},  // 25 %
	{1, -1, -1, -1, -1, 1, 1, 1},    // 50 %
	{-1, 1, 1, 1, 1, 1, 1, -1},      // 75 %
}

// noteHz es la frecuencia de una nota (1 = C-0, como en las frases), con
// los semitonos (fraccionarios) que sumen los efectos.
func noteHz(note float64) float64 { return 440 * math.Pow(2, (note+11-69)/12) }

// gbPeriod redondea f a lo que la GB puede tocar: f = clock/(2048-x), con x
// de 11 bits (clock 131072 en los pulsos, 65536 en la onda).
func gbPeriod(f, clock float64) float64 {
	x := math.Round(2048 - clock/f)
	x = math.Max(0, math.Min(2047, x))
	return clock / (2048 - x)
}

// voice es un canal sonando.
type voice struct {
	in   *Instrument
	wave *Wave
	gb   bool
	ch   int // el canal (PU1 tiene el barrido)

	gate  bool
	note  float64 // la nota con todos los efectos, en semitonos
	level float64 // el volumen de la nota (0..1, sin la envolvente)
	duty  int
	short bool // ruido de 7 bits

	// La envolvente: en GB, el nivel 0..15 y su reloj; en Libre, la fase
	// ADSR (0 ataque, 1 caída, 2 sostenido, 3 soltar) y el nivel 0..1.
	env      float64
	envStage int
	envClock float64
	envStart int8 // la envolvente pedida por E (si no, la del instrumento)
	envDir   int8
	envPer   int8

	// El barrido de PU1 (NR10): reloj y frecuencia actual en Hz.
	sweepClock float64
	sweepHz    float64

	phase float64
	lfsr  uint16
	noise float64
	amp   float64 // la amplitud que sale, que va hacia la pedida (sin clics)
}

// trigger empieza una nota con el instrumento in.
func (v *voice) trigger(in *Instrument, wave *Wave, gb bool) {
	v.in, v.wave, v.gb = in, wave, gb
	v.gate = true
	v.duty = int(in.Duty & 3)
	v.short = in.Short
	v.envStart, v.envDir, v.envPer = in.Env.Start, in.Env.Dir, in.Env.Period
	v.envStage, v.envClock = 0, 0
	v.env = float64(in.Env.Start)
	if !gb {
		v.env = 0
		if in.Env.Attack == 0 {
			v.env, v.envStage = 1, 1
		}
	}
	v.sweepClock, v.sweepHz = 0, 0
	if v.lfsr == 0 {
		v.lfsr = 0x7fff
	}
	if in.Type == InstrNoise {
		v.lfsr = 0x7fff
	}
}

// release suelta la nota (en Libre, la envolvente va a su release).
func (v *voice) release() {
	v.gate = false
	if !v.gb {
		v.envStage = 3
	}
}

// envTick avanza la envolvente un tick (dt segundos; en Libre las fases van
// en ticks).
func (v *voice) envTick(dt float64) {
	if v.in == nil {
		return
	}
	if v.gb {
		if v.envPer <= 0 || v.envDir == 0 {
			return
		}
		v.envClock += dt
		for step := float64(v.envPer) / 64; v.envClock >= step; v.envClock -= step {
			v.env = math.Max(0, math.Min(15, v.env+float64(v.envDir)))
		}
		return
	}
	e := v.in.Env
	sus := float64(e.Sustain) / 15
	switch v.envStage {
	case 0:
		v.env += 1 / float64(max(1, e.Attack))
		if v.env >= 1 {
			v.env, v.envStage = 1, 1
		}
	case 1:
		v.env -= (1 - sus) / float64(max(1, e.Decay))
		if v.env <= sus {
			v.env, v.envStage = sus, 2
		}
	case 3:
		v.env -= 1 / float64(max(1, e.Release))
		if v.env < 0 {
			v.env = 0
		}
	}
}

// volume es lo fuerte que suena (0..1), con la envolvente.
func (v *voice) volume() float64 {
	if v.in == nil {
		return 0
	}
	if v.gb {
		if !v.gate {
			return 0
		}
		// La onda de la GB no tiene envolvente: el volumen de la nota, en
		// sus cuatro pasos (0, 25, 50, 100 %) y el desplazamiento del
		// instrumento.
		if v.in.Type == InstrWave || v.in.Type == InstrSample {
			shift := [4]float64{0, 0.25, 0.5, 1}[int(math.Round(v.level*3))]
			return shift / float64(int(1)<<max(0, int(v.in.WaveVol)))
		}
		return v.env / 15 * v.level
	}
	if !v.gate && v.envStage != 3 {
		return 0
	}
	return v.env * v.level
}

// sample da la siguiente muestra (-1..1) a rate muestras por segundo.
func (v *voice) sample(rate float64) float64 {
	target := v.volume()
	v.amp += (target - v.amp) * (400 / rate) // ~2,5 ms: sin clics
	if v.amp < 1e-4 && target == 0 {
		return 0
	}
	f := noteHz(v.note)
	var out float64
	switch v.in.Type {
	case InstrPulse:
		if v.gb {
			f = gbPeriod(f, 131072)
			if v.ch == PU1 && v.in.Sweep != 0 {
				f = v.sweep(f, rate)
			}
			v.phase += f / rate
			v.phase -= math.Floor(v.phase)
			out = dutyPatterns[v.duty][int(v.phase*8)&7]
		} else {
			v.phase += f / rate
			v.phase -= math.Floor(v.phase)
			width := [4]float64{0.125, 0.25, 0.5, 0.75}[v.duty]
			out = -1
			if v.phase < width {
				out = 1
			}
		}
	case InstrWave:
		if v.gb {
			f = gbPeriod(f, 65536)
		}
		v.phase += f / rate
		v.phase -= math.Floor(v.phase)
		w := v.wave
		if w == nil {
			out = 0
		} else {
			out = float64(w[int(v.phase*WaveLen)%WaveLen])/7.5 - 1
		}
	case InstrNoise:
		// El registro avanza a la frecuencia de la nota (×8, como el reloj
		// del ruido de la GB para las notas graves).
		v.phase += f * 8 / rate
		for v.phase >= 1 {
			v.phase--
			bit := (v.lfsr ^ v.lfsr>>1) & 1
			v.lfsr = v.lfsr>>1 | bit<<14
			if v.short {
				v.lfsr = v.lfsr&^0x40 | bit<<6
			}
			v.noise = float64(^v.lfsr&1)*2 - 1
		}
		out = v.noise
	case InstrSynth:
		v.phase += f / rate
		v.phase -= math.Floor(v.phase)
		out = synthShape(v.in.Shape, v.phase, v.duty)
	}
	return out * v.amp
}

// sweep aplica el barrido de PU1: cada (NR10 bits 4-6)/128 s, la frecuencia
// sube o baja f/2^shift.
func (v *voice) sweep(f, rate float64) float64 {
	period := float64(v.in.Sweep>>4&7) / 128
	shift := float64(v.in.Sweep & 7)
	if v.sweepHz == 0 {
		v.sweepHz = f
	}
	if period == 0 || shift == 0 {
		return v.sweepHz
	}
	v.sweepClock += 1 / rate
	for v.sweepClock >= period {
		v.sweepClock -= period
		d := v.sweepHz / math.Pow(2, shift)
		if v.in.Sweep&8 != 0 {
			v.sweepHz -= d
		} else {
			v.sweepHz += d
		}
		if v.sweepHz > 20000 || v.sweepHz < 20 {
			v.gate = false // la GB apaga el canal al pasarse
		}
	}
	return v.sweepHz
}

// synthShape es una forma del synth del modo Libre, en la fase x.
func synthShape(name string, x float64, duty int) float64 {
	switch name {
	case "sine":
		return math.Sin(2 * math.Pi * x)
	case "saw":
		return 2*x - 1
	case "square":
		if x < [4]float64{0.125, 0.25, 0.5, 0.75}[duty] {
			return 1
		}
		return -1
	case "organ":
		return shape(WaveOrgan, x)
	case "phantom":
		return shape(WavePhantom, x)
	case "noise":
		return shape(WaveNoise, x)
	}
	return 1 - 4*math.Abs(x-0.5) // triangle
}
