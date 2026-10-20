package tracker

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"sync"
)

// El reproductor: cada canal sigue su propia cadena por las filas de la
// canción (como en LSDJ, no tienen que ir al paso: una cadena corta termina
// antes y su canal pasa a la fila siguiente). El tiempo va en ticks (24 por
// tiempo con el groove 6/6); cada paso de una frase dura los ticks que diga
// el groove del canal, y los efectos, las tablas y las envolventes avanzan
// tick a tick.

// What es lo que se toca: la canción, una cadena o una frase (en bucle,
// para componer).
type What int

const (
	PlaySong What = iota
	PlayChain
	PlayPhrase
)

// fx son los efectos que siguen sonando de un paso al siguiente tick a tick.
type fx struct {
	chord      [2]int // C: arpegio (+x, +y)
	chordTick  int
	pitch      float64 // P: semitonos por tick
	bend       float64 // lo acumulado por P, X y L
	slideTo    float64 // L: la nota a la que va
	slideLeft  int
	vibSpeed   float64 // V
	vibDepth   float64
	vibPhase   float64
	retrigRate int // R: cada cuántos ticks
	retrigVol  int
	retrigTick int
	kill       int // K: ticks hasta cortar (-1: no)
	fadeIn     int // I: ticks que dura
	fadeOut    int // U
	fadeTick   int
	drop       int // X: ticks que dura la caída
	fine       float64
}

// channel es el estado de un canal.
type channel struct {
	v         voice
	row       int  // la fila de la canción
	chainPos  int  // el paso de la cadena
	step      int  // el paso de la frase
	ticks     int  // ticks que le quedan al paso
	delay     int  // D: ticks hasta que suene el paso
	pending   Step // el paso que espera por D
	groove    int
	groovePos int
	done      bool // la canción se acabó para este canal (o una frase/cadena sola)
	started   bool

	base      float64 // la nota del paso, con la transposición
	transpose int
	noteVol   float64 // el volumen del paso (0..1)
	fx        fx
	table     int // la tabla que corre (Empty: ninguna)
	tableRow  int
	tableTr   float64 // la transposición de la fila de la tabla
	pan       int8    // -1, 0, 1, o 2 (nada)
	hopTo     int     // H en una frase: el paso de la siguiente en que se sigue (-1: no)
	hits      int
}

// Player toca una canción: un io.Reader de muestras float32 estéreo para la
// salida de audio. Se usa desde dos hilos (el audio lee, la interfaz edita y
// manda): todo con mu.
type Player struct {
	mu      sync.Mutex
	song    *Song
	used    int // los canales que usa la canción (usedChannels)
	rate    float64
	gain    float64
	master  float64
	bpm     int
	playing bool
	what    What
	target  int // la cadena o la frase que se toca sola (o la fila de partida)
	solo    int // el canal en que suena (PlayChain, PlayPhrase)
	once    bool
	ended   bool
	chans   [MaxChannels]channel
	tickAcc float64
	muted   [MaxChannels]bool
	rng     *rand.Rand
	// Lo que sonó en cada canal, para los osciloscopios.
	scope    [MaxChannels][ScopeLen]float32
	scopeAt  int
	scopeDiv int
	// El reloj MIDI de fuera (clock.go): con él, los ticks los marca cada
	// pulso que llega, no el tempo propio.
	ext extClock
}

// ScopeLen son las muestras que guarda cada osciloscopio.
const ScopeLen = 64

// NewPlayer prepara un reproductor a rate muestras por segundo.
func NewPlayer(rate int) *Player {
	return &Player{rate: float64(rate), gain: 0.8, master: 1, rng: rand.New(rand.NewPCG(1, 2))}
}

// usedChannels es hasta qué canal tiene cadenas la canción (para la mezcla).
func usedChannels(s *Song) int {
	n := 0
	for _, r := range s.Rows {
		for ch := 0; ch < s.Channels(); ch++ {
			if r[ch] != Empty {
				n = max(n, ch+1)
			}
		}
	}
	return n
}

// SetSong cambia la canción (una copia): lo editado suena al momento.
func (p *Player) SetSong(s *Song) {
	c := s.Clone()
	p.mu.Lock()
	p.song = c
	p.used = usedChannels(c)
	if !p.playing {
		p.bpm = c.BPM
	}
	p.mu.Unlock()
}

// SetGain cambia el volumen (0..1).
func (p *Player) SetGain(g float64) {
	p.mu.Lock()
	p.gain = g
	p.mu.Unlock()
}

// Play empieza a tocar: la canción desde la fila target, o la cadena o la
// frase target en bucle en el canal ch.
func (p *Player) Play(what What, target, ch int) { p.start(what, target, ch, false) }

func (p *Player) start(what What, target, ch int, once bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.song == nil {
		return
	}
	p.playing, p.what, p.target, p.solo, p.once, p.ended = true, what, target, ch, once, false
	p.bpm, p.master, p.tickAcc = p.song.BPM, 1, 1 // el primer tick, ya
	for i := range p.chans {
		c := &p.chans[i]
		c.v.release()
		*c = channel{v: c.v, row: target, table: Empty, groove: p.song.Groove, pan: 0}
		c.fx.kill, c.hopTo = -1, -1
		c.done = what != PlaySong && i != ch
	}
}

// Stop la para (las voces se apagan sin clics).
func (p *Player) Stop() {
	p.mu.Lock()
	p.playing = false
	for i := range p.chans {
		p.chans[i].v.release()
	}
	p.mu.Unlock()
}

// Preview hace sonar una nota con un instrumento un momento en el canal ch
// (al escribirla, para oírla), con la canción parada.
func (p *Player) Preview(ch int, note int8, instr int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playing || p.song == nil || ch < 0 || ch >= MaxChannels || note <= 0 {
		return
	}
	in := p.song.Instrs[instr]
	if in == nil {
		return
	}
	c := &p.chans[ch]
	c.fx = fx{kill: int(p.tickRate() / 4)} // un cuarto de segundo
	c.base, c.noteVol, c.table = float64(note), 1, Empty
	c.v.ch = ch
	c.v.trigger(in, p.waveOf(in), p.song.Mode == ModeGB)
	c.v.smp = p.sampleOf(in) // los instrumentos de sample (voice.go)
	c.v.level = float64(in.Volume) / 15
	c.v.note = c.base
}

// Position es dónde va cada canal: fila, paso de cadena y paso de frase.
func (p *Player) Position(ch int) (row, chainPos, step int, playing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := &p.chans[ch]
	return c.row, c.chainPos, c.step, p.playing && !c.done
}

// SetMuted silencia (o no) un canal.
func (p *Player) SetMuted(ch int, m bool) {
	p.mu.Lock()
	if ch >= 0 && ch < MaxChannels {
		p.muted[ch] = m
	}
	p.mu.Unlock()
}

// Muted son los canales silenciados.
func (p *Player) Muted() [MaxChannels]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.muted
}

// Now es lo que suena en el canal ch: la cadena y la frase (Empty si
// ninguna), para que la interfaz siga la canción.
func (p *Player) Now(ch int) (chain, phrase int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	chain, phrase = Empty, Empty
	c := &p.chans[ch]
	if !p.playing || c.done || p.song == nil {
		return
	}
	switch p.what {
	case PlayPhrase:
		return Empty, p.target
	case PlayChain:
		chain = p.target
	default:
		if c.row < len(p.song.Rows) {
			chain = int(p.song.Rows[c.row][ch])
		}
	}
	if ch := p.song.Chains[chain]; chain != Empty && ch != nil && c.chainPos < Steps {
		phrase = int(ch[c.chainPos].Phrase)
	}
	return
}

// Playing dice si está tocando.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Scope copia la onda reciente del canal ch.
func (p *Player) Scope(ch int, dst []float32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range dst {
		dst[i] = p.scope[ch][(p.scopeAt+i*ScopeLen/len(dst))%ScopeLen]
	}
}

// Hits es cuántas notas lleva cada canal (cuando cambia, sonó una nueva).
func (p *Player) Hits() [MaxChannels]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out [MaxChannels]int
	for i := range p.chans {
		out[i] = p.chans[i].hits
	}
	return out
}

// tickRate son los ticks por segundo: 24 por tiempo.
func (p *Player) tickRate() float64 { return float64(p.bpm) * TicksPerBeat / 60 }

// waveOf es la onda de un instrumento de onda.
func (p *Player) waveOf(in *Instrument) *Wave {
	if w := p.song.Waves[int(in.Wave)]; w != nil {
		return w
	}
	presets := PresetWaves()
	w := presets[WaveTriangle]
	return &w
}

// grooveTicks son los ticks del paso que empieza en el canal.
func (p *Player) grooveTicks(c *channel) int {
	g := p.song.Grooves[c.groove]
	if len(g) == 0 {
		g = Groove{6, 6}
	}
	t := g[c.groovePos%len(g)]
	c.groovePos++
	return max(1, t)
}

// chainAt es la cadena que toca el canal ch ahora (nil si ninguna).
func (p *Player) chainAt(ch int) *Chain {
	c := &p.chans[ch]
	switch p.what {
	case PlayChain:
		return p.song.Chains[p.target]
	case PlayPhrase:
		return nil
	}
	if c.row >= len(p.song.Rows) {
		return nil
	}
	id := p.song.Rows[c.row][ch]
	if id == Empty {
		return nil
	}
	return p.song.Chains[int(id)]
}

// phraseAt es la frase que toca el canal ahora, con su transposición.
func (p *Player) phraseAt(ch int) (*Phrase, int) {
	c := &p.chans[ch]
	if p.what == PlayPhrase {
		return p.song.Phrases[p.target], 0
	}
	chain := p.chainAt(ch)
	if chain == nil || c.chainPos >= Steps || chain[c.chainPos].Phrase == Empty {
		return nil, 0
	}
	return p.song.Phrases[int(chain[c.chainPos].Phrase)], int(chain[c.chainPos].Transpose)
}

// advance pasa el canal al paso siguiente: de frase, de cadena y de fila.
func (p *Player) advance(ch int) {
	c := &p.chans[ch]
	c.step++
	if c.hopTo >= 0 {
		// H en la frase: se acaba aquí y la siguiente empieza en ese paso.
		c.step = Steps
	}
	if c.step < Steps {
		return
	}
	c.step = max(0, c.hopTo)
	c.hopTo = -1
	if p.what == PlayPhrase {
		return // en bucle
	}
	c.chainPos++
	chain := p.chainAt(ch)
	if p.what == PlayChain {
		if chain == nil || c.chainPos >= Steps || chain[c.chainPos].Phrase == Empty {
			c.chainPos = 0
		}
		return
	}
	if chain == nil && (c.chainPos < 1 || p.rowHasChains(c.row)) {
		return // descansa (ver sync)
	}
	if chain != nil && c.chainPos < Steps && chain[c.chainPos].Phrase != Empty {
		return
	}
	// Se acabó la cadena: la fila siguiente.
	c.chainPos = 0
	c.row++
	if c.row >= len(p.song.Rows) {
		c.row = 0
		if p.once {
			c.done = true
			c.v.release()
		}
	}
}

// rowHasChains dice si alguna cadena suena en la fila. Un canal sin cadena
// en una fila que sí tiene descansa hasta que los demás pasan a la
// siguiente (sync), así los canales siguen juntos de fila en fila (en LSDJ
// el canal se pararía); en una fila vacía, todos descansan una frase.
func (p *Player) rowHasChains(row int) bool {
	if row >= len(p.song.Rows) {
		return false
	}
	for ch := 0; ch < p.song.Channels(); ch++ {
		if id := p.song.Rows[row][ch]; id != Empty && p.song.Chains[int(id)] != nil {
			return true
		}
	}
	return false
}

// sync pasa a la fila siguiente los canales que descansan en cuanto se van
// los que tocaban: van al paso en que están ellos.
func (p *Player) sync() {
	if !p.playing || p.what != PlaySong {
		return
	}
	n := p.song.Channels()
	for ch := 0; ch < n; ch++ {
		c := &p.chans[ch]
		if c.done || p.chainAt(ch) != nil {
			continue
		}
		var lead *channel
		waiting := false
		for o := 0; o < n; o++ {
			oc := &p.chans[o]
			if o == ch || c.row >= len(p.song.Rows) {
				continue
			}
			id := p.song.Rows[c.row][o]
			if id == Empty || p.song.Chains[int(id)] == nil {
				continue
			}
			if oc.row == c.row && !oc.done {
				waiting = true
				break
			}
			lead = oc
		}
		if waiting || lead == nil {
			continue
		}
		c.row, c.chainPos, c.step, c.ticks, c.groovePos, c.done = lead.row, lead.chainPos, lead.step, lead.ticks, lead.groovePos, lead.done
		c.delay = 0
		if c.done {
			c.v.release()
			continue
		}
		p.startStep(ch)
	}
}

// startStep empieza el paso del canal (o, con D, lo deja para luego).
func (p *Player) startStep(ch int) {
	c := &p.chans[ch]
	ph, tr := p.phraseAt(ch)
	if ph == nil {
		return // fila vacía: descansa
	}
	st := ph[c.step]
	c.transpose = tr
	// D: el paso suena más tarde (si llega antes de que acabe).
	for _, cm := range st.Cmds {
		if cm.Code == CmdDelay && cm.Arg > 0 {
			c.delay, c.pending = int(cm.Arg), st
			return
		}
	}
	p.playStep(ch, st)
}

// playStep aplica un paso: la nota, el volumen y los comandos.
func (p *Player) playStep(ch int, st Step) {
	c := &p.chans[ch]
	cmds := st.Cmds
	// Z: el argumento del otro comando, al azar entre 0 y el suyo.
	for i, cm := range cmds {
		if cm.Code == CmdRandom {
			o := &cmds[1-i]
			if !o.None() && cm.Arg > 0 {
				o.Arg += byte(p.rng.IntN(int(cm.Arg) + 1))
			}
		}
	}
	slide := false
	for _, cm := range cmds {
		if cm.Code == CmdSlide {
			slide = true
		}
	}
	if st.Note > 0 && !slide {
		in := p.song.Instrs[int(st.Instr)]
		if st.Instr == Empty {
			in = c.v.in // sin instrumento: el que sonaba
		}
		if in != nil {
			tr := 0
			if in.Transpose {
				tr = c.transpose
			}
			c.base = float64(int(st.Note) + tr)
			c.fx = fx{kill: -1}
			if in.Length > 0 {
				c.fx.kill = in.Length
			}
			if in.Vibrato != 0 {
				c.fx.vibSpeed, c.fx.vibDepth = float64(in.Vibrato>>4), float64(in.Vibrato&15)
			}
			c.v.ch = ch
			c.v.trigger(in, p.waveOf(in), p.song.Mode == ModeGB)
			c.v.smp = p.sampleOf(in) // los instrumentos de sample (voice.go)
			c.noteVol = 1
			c.table, c.tableRow, c.tableTr = int(in.Table), 0, 0
			if in.Pan != 0 {
				c.pan = in.Pan
			}
			c.hits++
		}
	}
	if st.Note == NoteOff {
		c.v.release()
	}
	if st.Vol != Empty {
		c.noteVol = float64(st.Vol) / 15
	}
	for _, cm := range cmds {
		if cm.Code == CmdSlide && st.Note > 0 {
			c.fx.slideTo = float64(int(st.Note) + c.transpose)
			c.fx.slideLeft = max(1, int(cm.Arg))
			continue
		}
		p.command(ch, cm)
	}
}

// command aplica un comando en el canal (de una frase o de una tabla).
func (p *Player) command(ch int, cm Cmd) {
	c := &p.chans[ch]
	a := cm.Arg
	switch cm.Code {
	case CmdTable:
		if a == 0xff {
			c.table = Empty
		} else {
			c.table, c.tableRow = int(a)%NumTables, 0
		}
	case CmdChord:
		c.fx.chord = [2]int{int(a >> 4), int(a & 15)}
		c.fx.chordTick = 0
	case CmdEnvelope:
		c.v.envStart, c.v.envPer = int8(a>>4), int8(a&7)
		c.v.envDir = -1
		if a&8 != 0 {
			c.v.envDir = 1
		}
		if c.v.envPer == 0 {
			c.v.envDir = 0
		}
		c.v.env = float64(c.v.envStart)
	case CmdFine:
		c.fx.fine = float64(int8(a)) / 100
	case CmdGroove:
		if _, ok := p.song.Grooves[int(a)]; ok {
			c.groove, c.groovePos = int(a), 0
		}
	case CmdFadeIn:
		c.fx.fadeIn, c.fx.fadeTick = max(1, int(a)), 0
	case CmdFadeOut:
		c.fx.fadeOut, c.fx.fadeTick = max(1, int(a)), 0
	case CmdKill:
		c.fx.kill = int(a)
	case CmdMaster:
		p.master = float64(a&15) / 15
	case CmdOutput:
		c.pan = [4]int8{2, -1, 1, 0}[a&3]
	case CmdPitch:
		c.fx.pitch = float64(int8(a)) / 100
	case CmdRetrig:
		c.fx.retrigRate, c.fx.retrigVol, c.fx.retrigTick = int(a&15), int(int8(a)>>4), 0
	case CmdSweep:
		if c.v.in != nil {
			if c.v.in.Type == InstrNoise {
				c.v.short = a&1 != 0
			} else {
				in := *c.v.in
				in.Sweep = a
				c.v.in = &in
			}
		}
	case CmdHop:
		c.hopTo = int(a) % Steps
	case CmdTempo:
		if a >= MinBPM {
			p.bpm = int(a)
		}
	case CmdVibrato:
		c.fx.vibSpeed, c.fx.vibDepth = float64(a>>4), float64(a&15)
	case CmdWave:
		if c.v.in != nil && c.v.in.Type == InstrWave {
			if w := p.song.Waves[int(a)]; w != nil {
				c.v.wave = w
			}
		} else {
			c.v.duty = int(a & 3)
		}
	case CmdDrop:
		c.fx.drop, c.fx.fadeTick = max(1, int(a)), 0
	}
}

// tick avanza un tick: el paso de cada canal, sus tablas y sus efectos.
func (p *Player) tick() {
	dt := p.tickDur()
	for ch := 0; ch < p.song.Channels(); ch++ {
		c := &p.chans[ch]
		if p.playing && !c.done {
			if c.ticks <= 0 {
				if c.started {
					p.advance(ch)
				}
				c.started = true
				c.ticks = p.grooveTicks(c)
				c.delay = 0 // un paso retrasado que no llegó a sonar se pierde
				p.startStep(ch)
			} else if c.delay > 0 {
				if c.delay--; c.delay == 0 {
					p.playStep(ch, c.pending)
				}
			}
			c.ticks--
		}
		p.runTable(ch)
		p.effects(c)
		c.v.envTick(dt)
	}
	p.sync()
}

// runTable aplica la fila de la tabla que corre y pasa a la siguiente.
func (p *Player) runTable(ch int) {
	c := &p.chans[ch]
	if c.table == Empty || !c.v.gate {
		return
	}
	t := p.song.Tables[c.table]
	if t == nil {
		c.table = Empty
		return
	}
	row := t[c.tableRow]
	if row.Vol != Empty {
		c.noteVol = float64(row.Vol) / 15
	}
	c.tableTr = float64(row.Transpose) // la tabla transpone desde la nota
	next := (c.tableRow + 1) % Steps
	for _, cm := range row.Cmds {
		if cm.Code == CmdHop {
			// H en una tabla: la fila siguiente es otra (una por tick, así
			// que un bucle de saltos no cuelga nada).
			next = int(cm.Arg) % Steps
			continue
		}
		p.command(ch, cm)
	}
	c.tableRow = next
}

// effects aplica los efectos que siguen tick a tick y calcula la nota y el
// volumen que suenan.
func (p *Player) effects(c *channel) {
	f := &c.fx
	if f.kill > 0 {
		if f.kill--; f.kill == 0 {
			c.v.release()
		}
	}
	if f.retrigRate > 0 {
		if f.retrigTick++; f.retrigTick >= f.retrigRate {
			f.retrigTick = 0
			if c.v.in != nil {
				c.v.trigger(c.v.in, c.v.wave, c.v.gb)
				c.noteVol = math.Max(0, math.Min(1, c.noteVol+float64(f.retrigVol)/15))
			}
		}
	}
	f.bend += f.pitch
	if f.slideLeft > 0 {
		cur := c.base + f.bend
		f.bend += (f.slideTo - cur) / float64(f.slideLeft)
		f.slideLeft--
	}
	vib := 0.0
	if f.vibDepth > 0 {
		f.vibPhase += f.vibSpeed / 32
		vib = math.Sin(2*math.Pi*f.vibPhase) * f.vibDepth / 16
	}
	arp := 0
	if f.chord != [2]int{} {
		arp = [3]int{0, f.chord[0], f.chord[1]}[f.chordTick%3]
		f.chordTick++
	}
	vol := c.noteVol
	if f.fadeIn > 0 {
		f.fadeTick++
		vol *= math.Min(1, float64(f.fadeTick)/float64(f.fadeIn))
	}
	if f.fadeOut > 0 {
		f.fadeTick++
		vol *= math.Max(0, 1-float64(f.fadeTick)/float64(f.fadeOut))
	}
	if f.drop > 0 {
		f.fadeTick++
		k := math.Min(1, float64(f.fadeTick)/float64(f.drop))
		f.bend -= 36 * k / float64(f.drop)
		vol *= 1 - k
	}
	c.v.note = c.base + c.tableTr + f.bend + f.fine + vib + float64(arp)
	inVol := 1.0
	if c.v.in != nil {
		inVol = float64(c.v.in.Volume) / 15
	}
	c.v.level = vol * inVol
}

// next da una muestra estéreo (con mu tomado).
func (p *Player) next() (l, r float64) {
	if p.song == nil {
		return 0, 0
	}
	// Con el reloj de fuera, tocando, los ticks llegan con cada pulso
	// (ExternalTick); si no, salen del tempo propio.
	if !(p.ext.on && p.playing) {
		p.tickAcc += p.tickRate() / p.rate
		for p.tickAcc >= 1 {
			p.tickAcc--
			p.tick()
		}
	}
	n := p.song.Channels()
	allDone := p.playing
	for ch := 0; ch < n; ch++ {
		c := &p.chans[ch]
		if !c.done {
			allDone = false
		}
		if c.v.in == nil {
			continue
		}
		s := c.v.sample(p.rate)
		if p.muted[ch] {
			s = 0
		}
		if p.scopeDiv == 0 {
			p.scope[ch][p.scopeAt] = float32(s)
		}
		gl, gr := panGains(c.pan, ch, n)
		l += s * gl
		r += s * gr
	}
	if p.scopeDiv++; p.scopeDiv >= 16 {
		p.scopeDiv = 0
		p.scopeAt = (p.scopeAt + 1) % ScopeLen
	}
	if allDone && p.once {
		p.playing, p.ended = false, true
	}
	// Cada canal a 1/n de fondo (con los 4 de la GB, como la mezcla de antes),
	// con n los canales que usa la canción: un MOD de 4 canales en el modo
	// Libre suena como uno de la GB, no a la mitad.
	g := p.gain * p.master * 0.9 / math.Sqrt(float64(min(n, max(GBChannels, p.used)))*1.6)
	return l * g, r * g
}

// panGains reparte un canal entre los dos lados: según su O (o su
// instrumento) o, sin decir nada, un poco abiertos los pulsos.
func panGains(pan int8, ch, n int) (l, r float64) {
	switch pan {
	case -1:
		return 1, 0
	case 1:
		return 0, 1
	case 2:
		return 0, 0
	}
	if n == GBChannels {
		switch ch {
		case PU1:
			return 0.8, 0.45
		case PU2:
			return 0.45, 0.8
		}
	}
	return 0.7, 0.7
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
// las muestras estéreo intercaladas: para exportarla. maxSeconds corta las
// que no terminan (todos sus canales en bucle).
func Render(s *Song, rate int, maxSeconds float64) []float32 {
	p := NewPlayer(rate)
	p.SetSong(s)
	p.start(PlaySong, 0, 0, true)
	var out []float32
	tail, limit := rate, int(maxSeconds*float64(rate))
	for i := 0; ; i++ {
		l, r := p.next()
		out = append(out, float32(l), float32(r))
		if p.ended || i >= limit {
			if tail--; tail == 0 {
				return out
			}
		}
	}
}
