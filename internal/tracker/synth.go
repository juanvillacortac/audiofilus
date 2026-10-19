package tracker

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// El sintetizador: cuatro voces como las de una consola de 8 bits. Los pulsos
// tienen cuatro anchos; la triangular, 16 escalones (por eso suena "a
// consola"); el ruido es un registro de desplazamiento de 15 bits, y su tono
// es la frecuencia a la que avanza. Cada voz va hacia su volumen con un
// fundido de unos milisegundos para no hacer clics; el ruido, además, decae
// solo (suena a percusión).

// tickRate es la frecuencia de los efectos (arpegio, desliz): 60 por segundo.
const tickRate = 60

// voice es un canal sonando.
type voice struct {
	kind   int
	note   float64 // en semitonos (MIDI), con el desliz
	base   int     // la nota de la celda (MIDI)
	arp    [2]int
	bend   float64 // semitonos por segundo
	duty   int
	vol    float64 // el volumen pedido, 0..1
	gate   bool
	env    float64 // la amplitud actual
	decay  float64 // el ruido: cuánto queda del golpe
	phase  float64
	lfsr   uint16
	noiseV float64
}

// dutyFrac son los anchos de pulso.
var dutyFrac = [4]float64{0.125, 0.25, 0.5, 0.75}

// freq es la frecuencia de una nota MIDI.
func freq(midi float64) float64 { return 440 * math.Pow(2, (midi-69)/12) }

// sample da la siguiente muestra de la voz (-1..1).
func (v *voice) sample(rate float64, arpStep int) float64 {
	target := 0.0
	if v.gate {
		target = v.vol
		if v.kind == Noise {
			target *= v.decay
			v.decay *= 1 - 9/rate // ~0,1 s
		}
	}
	v.env += (target - v.env) * (300 / rate) // ~3 ms de fundido
	if v.env < 1e-4 && !v.gate {
		return 0
	}
	n := v.note
	if arpStep > 0 {
		n += float64(v.arp[arpStep-1])
	}
	f := freq(n)
	var out float64
	switch v.kind {
	case Pulse1, Pulse2:
		v.phase += f / rate
		v.phase -= math.Floor(v.phase)
		out = -1
		if v.phase < dutyFrac[v.duty] {
			out = 1
		}
	case Triangle:
		v.phase += f / rate
		v.phase -= math.Floor(v.phase)
		tri := 1 - 4*math.Abs(v.phase-0.5) // -1..1
		out = math.Round(tri*7.5) / 7.5    // 16 escalones
	case Noise:
		// El tono del ruido: cuántas veces por segundo avanza el registro.
		v.phase += f * 8 / rate
		for v.phase >= 1 {
			v.phase--
			bit := (v.lfsr ^ v.lfsr>>1) & 1
			v.lfsr = v.lfsr>>1 | bit<<14
			v.noiseV = float64(v.lfsr&1)*2 - 1
		}
		out = v.noiseV
	}
	return out * v.env
}

// apply pone una celda en la voz.
func (v *voice) apply(c Cell) {
	if c.Vol >= 0 {
		v.vol = float64(c.Vol) / 15
	}
	switch c.Fx {
	case FxDuty:
		v.duty = int(c.Arg & 3)
	case FxBend:
		if c.Arg&0x80 != 0 {
			v.bend = -float64(c.Arg&0x7f) / 4
		} else {
			v.bend = float64(c.Arg) / 4
		}
	}
	switch {
	case c.Note > 0:
		v.base = int(c.Note) - 1 + 12 // C-0 es MIDI 12
		v.note = float64(v.base)
		v.gate = true
		v.decay = 1
		if c.Fx != FxBend {
			v.bend = 0
		}
		v.arp = [2]int{}
		if v.lfsr == 0 {
			v.lfsr = 1
		}
	case c.Note == NoteOff:
		v.gate = false
	}
	if c.Fx == FxArp {
		v.arp = [2]int{int(c.Arg >> 4), int(c.Arg & 15)}
	}
}

// Player toca una canción. Es un io.Reader de muestras float32 estéreo (lo
// que pide la salida de audio de la interfaz); parado, da silencio. Se usa
// desde dos hilos (el del audio lee, la interfaz edita y manda): todo con mu.
type Player struct {
	mu      sync.Mutex
	song    *Song
	rate    float64
	gain    float64
	playing bool
	loop    int // el patrón que se repite, o -1 para toda la canción
	once    bool
	pos     int // la posición en Order
	step    int
	left    float64 // muestras que le quedan al paso actual
	tick    float64
	arpStep int
	voices  [Channels]voice
	ended   bool
	preview [Channels]float64 // muestras que le quedan a la nota de prueba
}

// NewPlayer prepara un reproductor a rate muestras por segundo.
func NewPlayer(rate int) *Player {
	p := &Player{rate: float64(rate), gain: 0.8, loop: -1}
	for i := range p.voices {
		p.voices[i].kind = i
		p.voices[i].duty = 2
		p.voices[i].lfsr = 1
	}
	return p
}

// SetSong cambia la canción (una copia): lo editado suena al momento.
func (p *Player) SetSong(s *Song) {
	c := s.Clone()
	p.mu.Lock()
	p.song = c
	p.mu.Unlock()
}

// SetGain cambia el volumen (0..1).
func (p *Player) SetGain(g float64) {
	p.mu.Lock()
	p.gain = g
	p.mu.Unlock()
}

// PlayPattern repite el patrón pat desde el principio.
func (p *Player) PlayPattern(pat int) { p.start(0, pat, false) }

// PlaySong toca la canción desde la posición pos de Order, en bucle.
func (p *Player) PlaySong(pos int) { p.start(pos, -1, false) }

func (p *Player) start(pos, loop int, once bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.playing, p.loop, p.once, p.ended = true, loop, once, false
	p.pos, p.step, p.left, p.tick = pos, 0, 0, 0
	for i := range p.voices {
		p.voices[i].gate = false
	}
}

// Preview hace sonar una celda un momento en el canal ch, con la canción
// parada (al escribir una nota, para oírla).
func (p *Player) Preview(ch int, c Cell) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing || ch < 0 || ch >= Channels {
		return
	}
	v := &p.voices[ch]
	if c.Vol < 0 && v.vol == 0 {
		c.Vol = 12
	}
	v.apply(c)
	p.preview[ch] = p.rate * 0.25
}

// Stop la para (las voces se apagan con su fundido).
func (p *Player) Stop() {
	p.mu.Lock()
	p.playing = false
	for i := range p.voices {
		p.voices[i].gate = false
	}
	p.mu.Unlock()
}

// Position dice qué suena: el patrón, el paso y la posición en Order.
func (p *Player) Position() (pattern, step, pos int, playing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.song == nil {
		return 0, 0, 0, false
	}
	pattern = p.pattern()
	return pattern, p.step, p.pos, p.playing
}

// Level es lo fuerte que suena cada canal ahora (0..1), para los medidores.
func (p *Player) Level() [Channels]float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out [Channels]float64
	for i, v := range p.voices {
		out[i] = v.env
	}
	return out
}

// pattern es el patrón que suena (con mu tomado).
func (p *Player) pattern() int {
	if p.loop >= 0 {
		return min(p.loop, len(p.song.Patterns)-1)
	}
	if p.pos >= len(p.song.Order) {
		p.pos = 0
	}
	return p.song.Order[p.pos]
}

// stepLen son las muestras de un paso (una semicorchea).
func (p *Player) stepLen() float64 { return p.rate * 60 / float64(p.song.BPM) / 4 }

// next produce una muestra estéreo (con mu tomado).
func (p *Player) next() (l, r float64) {
	if p.song == nil {
		return 0, 0
	}
	if p.playing {
		if p.left <= 0 {
			pat := p.song.Patterns[p.pattern()]
			for c := 0; c < Channels; c++ {
				p.voices[c].apply(pat[p.step][c])
			}
			p.left += p.stepLen()
		}
		p.left--
		if p.left <= 0 {
			p.step++
			if p.step == Steps {
				p.step = 0
				if p.loop < 0 {
					p.pos++
					if p.pos >= len(p.song.Order) {
						p.pos = 0
						if p.once {
							p.playing, p.ended = false, true
							for i := range p.voices {
								p.voices[i].gate = false
							}
						}
					}
				}
			}
		}
	}
	for i := range p.preview {
		if p.preview[i] > 0 {
			if p.preview[i]--; p.preview[i] <= 0 {
				p.voices[i].gate = false
			}
		}
	}
	// Los efectos, 60 veces por segundo.
	p.tick += tickRate / p.rate
	if p.tick >= 1 {
		p.tick--
		p.arpStep = (p.arpStep + 1) % 3
		for i := range p.voices {
			v := &p.voices[i]
			v.note += v.bend / tickRate
			v.note = math.Max(12, math.Min(120, v.note))
		}
	}
	var s [Channels]float64
	for i := range p.voices {
		a := p.arpStep
		if p.voices[i].arp == [2]int{} {
			a = 0
		}
		s[i] = p.voices[i].sample(p.rate, a)
	}
	// La mezcla: los pulsos un poco a cada lado, la triangular y el ruido al
	// centro.
	l = 0.2*(0.65*s[0]+0.35*s[1]) + 0.24*s[2] + 0.13*s[3]
	r = 0.2*(0.35*s[0]+0.65*s[1]) + 0.24*s[2] + 0.13*s[3]
	return l * p.gain, r * p.gain
}

// Read llena b de muestras float32 estéreo (little endian).
func (p *Player) Read(b []byte) (int, error) {
	n := len(b) / 8 * 8
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := 0; i < n; i += 8 {
		l, r := p.next()
		binary.LittleEndian.PutUint32(b[i:], math.Float32bits(float32(l)))
		binary.LittleEndian.PutUint32(b[i+4:], math.Float32bits(float32(r)))
	}
	return n, nil
}

// Render toca la canción entera una vez (con un segundo de cola) y devuelve
// las muestras estéreo intercaladas: para exportarla.
func Render(s *Song, rate int) []float32 {
	p := NewPlayer(rate)
	p.SetSong(s)
	p.start(0, -1, true)
	var out []float32
	tail := rate
	for {
		l, r := p.next()
		out = append(out, float32(l), float32(r))
		if p.ended {
			tail--
			if tail == 0 {
				return out
			}
		}
	}
}

// WriteWAV escribe muestras estéreo intercaladas como WAV de 16 bits.
func WriteWAV(w io.Writer, samples []float32, rate int) error {
	data := len(samples) * 2
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+data))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 2)
	binary.LittleEndian.PutUint32(hdr[24:], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(hdr[32:], 4)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(data))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	buf := make([]byte, data)
	for i, x := range samples {
		v := int16(math.Max(-1, math.Min(1, float64(x))) * 32767)
		binary.LittleEndian.PutUint16(buf[2*i:], uint16(v))
	}
	_, err := w.Write(buf)
	return err
}
