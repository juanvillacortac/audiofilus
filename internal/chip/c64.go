package chip

import (
	"encoding/binary"
	"errors"
	"math"
	"time"
)

// Los .sid (PSID y RSID, el formato de la HVSC) traen el programa del C64
// que toca la canción: se carga en la memoria, se llama a su rutina de
// inicio con el número de canción y luego a la de tocar 50 o 60 veces por
// segundo (o, si no tiene, la rutina se instala sola en una interrupción).
// Aquí está lo justo del C64 para eso: la RAM, el puerto del 6510 que
// muestra u oculta la E/S y la ROM, los dos CIA (sus temporizadores dan las
// interrupciones y las NMI de los digis), el VIC (solo la interrupción por
// línea de pantalla) y, en vez de la ROM del KERNAL (que no es libre), unas
// pocas instrucciones en sus direcciones de siempre: la entrada de la
// interrupción, que salta por $0314, y las salidas que recuperan los
// registros ($EA31, $EA81, $FEBC). Las demás rutinas del KERNAL solo
// vuelven.

// c64 es la máquina.
type c64 struct {
	ram    [65536]byte
	kernal [8192]byte
	cpu    cpu
	sids   []*sid
	sidAt  []uint16
	cia    [2]cia
	vic    vic
	port   byte  // $01: qué se ve en $A000-$FFFF
	clock  int64 // ciclos desde el principio
	pal    bool

	// La rutina de tocar, si la canción la tiene (si no, va por
	// interrupciones): cada period ciclos, con el puerto playPort.
	play     uint16
	playPort byte
	period   int64
	nextPlay int64
	callAt   int64 // cuándo empezó la llamada en curso (-1: ninguna)

	// El audio: los SID van hasta sidClock y las muestras salen en out.
	sidClock   int64
	nextSample float64 // ciclo en el que acaba la muestra en curso
	cps        float64 // ciclos por muestra
	accL, accR float64
	accN       int
	out        []float32
	skipping   bool
	dc         [2]dcBlock
	gain       float64
}

// retAddr es adonde vuelven las rutinas que se llaman desde fuera: un RTS
// con $FFFF en la pila va a $0000, y ahí la CPU queda parada (ocupada en
// nada) hasta la siguiente llamada o interrupción.
const retAddr = 0x0000

// Los ciclos de una pantalla y de una línea (PAL y NTSC).
const (
	palFrame  = 19656
	ntscFrame = 17095
	palLine   = 63
	ntscLine  = 65
)

// cia es un CIA 6526: sus dos temporizadores y sus interrupciones.
type cia struct {
	ta, tb   int
	la, lb   int
	cra, crb byte
	icr      byte // lo que ha pasado
	mask     byte // lo que interrumpe
	regs     [16]byte
	nmiLine  bool // CIA 2: la NMI ya está dada (hasta que se lea el ICR)
}

func (c *cia) read(r int) byte {
	switch r {
	case 4:
		return byte(c.ta)
	case 5:
		return byte(c.ta >> 8)
	case 6:
		return byte(c.tb)
	case 7:
		return byte(c.tb >> 8)
	case 13:
		v := c.icr
		if c.icr&c.mask&0x1f != 0 {
			v |= 0x80
		}
		c.icr, c.nmiLine = 0, false
		return v
	case 14:
		return c.cra
	case 15:
		return c.crb
	}
	return c.regs[r]
}

func (c *cia) write(r int, v byte) {
	c.regs[r] = v
	switch r {
	case 4:
		c.la = c.la&0xff00 | int(v)
	case 5:
		c.la = c.la&0xff | int(v)<<8
		if c.cra&1 == 0 {
			c.ta = c.la
		}
	case 6:
		c.lb = c.lb&0xff00 | int(v)
	case 7:
		c.lb = c.lb&0xff | int(v)<<8
		if c.crb&1 == 0 {
			c.tb = c.lb
		}
	case 13:
		if v&0x80 != 0 {
			c.mask |= v & 0x1f
		} else {
			c.mask &^= v & 0x1f
		}
	case 14:
		if v&0x10 != 0 {
			c.ta = c.la
		}
		c.cra = v &^ 0x10
	case 15:
		if v&0x10 != 0 {
			c.tb = c.lb
		}
		c.crb = v &^ 0x10
	}
}

// tick avanza n ciclos.
func (c *cia) tick(n int) {
	under := 0
	if c.cra&1 != 0 {
		c.ta -= n
		for c.ta < 0 {
			under++
			c.icr |= 1
			if c.cra&8 != 0 { // una sola vez
				c.cra &^= 1
				c.ta = c.la
				break
			}
			c.ta += c.la + 1
		}
	}
	if c.crb&1 != 0 {
		switch c.crb >> 5 & 3 {
		case 0:
			c.tb -= n
		case 2, 3:
			c.tb -= under
		default:
			return
		}
		for c.tb < 0 {
			c.icr |= 2
			if c.crb&8 != 0 {
				c.crb &^= 1
				c.tb = c.lb
				break
			}
			c.tb += c.lb + 1
		}
	}
}

func (c *cia) irq() bool { return c.icr&c.mask&0x1f != 0 }

// vic es lo del VIC-II que importa a la música: la línea de pantalla y su
// interrupción.
type vic struct {
	regs           [64]byte
	status, enable byte
	pos            int // ciclo dentro de la pantalla
	frame, line    int
}

func (v *vic) raster() int { return v.pos / v.line }

func (v *vic) compare() int { return int(v.regs[0x12]) | int(v.regs[0x11]&0x80)<<1 }

func (v *vic) read(r int) byte {
	switch r {
	case 0x11:
		return v.regs[0x11]&0x7f | byte(v.raster()>>1)&0x80
	case 0x12:
		return byte(v.raster())
	case 0x19:
		s := v.status | 0x70
		if v.status&v.enable&15 != 0 {
			s |= 0x80
		}
		return s
	case 0x1a:
		return v.enable | 0xf0
	}
	return v.regs[r]
}

func (v *vic) write(r int, b byte) {
	switch r {
	case 0x19:
		v.status &^= b & 15
	case 0x1a:
		v.enable = b & 15
	default:
		v.regs[r] = b
	}
}

func (v *vic) tick(n int) {
	before := v.raster()
	v.pos += n
	if v.pos >= v.frame {
		v.pos -= v.frame
	}
	if after := v.raster(); after != before && after == v.compare() {
		v.status |= 1
	}
}

// buildKernal arma la ROM de mentira.
func (m *c64) buildKernal() {
	for i := range m.kernal {
		m.kernal[i] = 0x60 // RTS
	}
	put := func(at uint16, code ...byte) { copy(m.kernal[at-0xe000:], code) }
	put(0xff48, 0x48, 0x8a, 0x48, 0x98, 0x48, 0x6c, 0x14, 0x03) // PHA TXA PHA TYA PHA JMP ($0314)
	put(0xea31, 0x4c, 0x7e, 0xea)                               // JMP $EA7E
	put(0xea7e, 0xad, 0x0d, 0xdc)                               // LDA $DC0D
	put(0xea81, 0x68, 0xa8, 0x68, 0xaa, 0x68, 0x40)             // PLA TAY PLA TAX PLA RTI
	put(0xfebc, 0x68, 0xa8, 0x68, 0xaa, 0x68, 0x40)
	put(0xfe43, 0x78, 0x6c, 0x18, 0x03) // SEI JMP ($0318)
	put(0xfe47, 0x40)                   // RTI
	put(0xfffa, 0x43, 0xfe, 0xe2, 0xfc, 0x48, 0xff)
}

func (m *c64) ioVisible() bool { return m.port&4 != 0 && m.port&3 != 0 }

// sidFor es el SID de la dirección a (y su registro), o nil.
func (m *c64) sidFor(a uint16) (*sid, int) {
	for i := len(m.sids) - 1; i >= 1; i-- {
		if a >= m.sidAt[i] && a < m.sidAt[i]+32 {
			return m.sids[i], int(a - m.sidAt[i])
		}
	}
	if a >= 0xd400 && a < 0xd800 {
		return m.sids[0], int(a & 31)
	}
	return nil, 0
}

func (m *c64) read(a uint16) byte {
	switch {
	case a == 0:
		return 0x2f
	case a == 1:
		return m.port
	case a >= 0xe000 && m.port&2 != 0:
		return m.kernal[a-0xe000]
	case a >= 0xd000 && a < 0xe000 && m.ioVisible():
		return m.ioRead(a)
	}
	return m.ram[a]
}

func (m *c64) write(a uint16, v byte) {
	switch {
	case a == 1:
		m.port = v
	case a >= 0xd000 && a < 0xe000 && m.ioVisible():
		m.ioWrite(a, v)
		return
	}
	m.ram[a] = v
}

func (m *c64) ioRead(a uint16) byte {
	if s, r := m.sidFor(a); s != nil {
		m.syncSID(m.clock)
		return s.read(r)
	}
	switch a & 0xff00 {
	case 0xd000, 0xd100, 0xd200, 0xd300:
		return m.vic.read(int(a & 0x3f))
	case 0xdc00:
		return m.cia[0].read(int(a & 15))
	case 0xdd00:
		return m.cia[1].read(int(a & 15))
	}
	return m.ram[a]
}

func (m *c64) ioWrite(a uint16, v byte) {
	if s, r := m.sidFor(a); s != nil {
		m.syncSID(m.clock)
		s.write(r, v)
		return
	}
	switch a & 0xff00 {
	case 0xd000, 0xd100, 0xd200, 0xd300:
		m.vic.write(int(a&0x3f), v)
	case 0xdc00:
		m.cia[0].write(int(a&15), v)
	case 0xdd00:
		m.cia[1].write(int(a&15), v)
	default:
		m.ram[a] = v
	}
}

// tick avanza los CIA y el VIC n ciclos.
func (m *c64) tick(n int) {
	m.clock += int64(n)
	m.cia[0].tick(n)
	m.cia[1].tick(n)
	m.vic.tick(n)
}

// call empieza la rutina at (vuelve a retAddr).
func (m *c64) call(at uint16) {
	m.cpu.s = 0xff
	m.cpu.push16(0xffff)
	m.cpu.pc = at
	m.cpu.p |= flagI
	m.callAt = m.clock
}

// maxCall es lo más que puede tardar una llamada (en segundos de C64): si
// no vuelve, se da por perdida.
const maxCall = 2

// run ejecuta hasta el ciclo t.
func (m *c64) run(t int64) {
	cpuHz := int64(sidClockPAL)
	for m.clock < t {
		if m.cpu.pc == retAddr {
			// Parada: la siguiente llamada, una interrupción o esperar.
			if m.play != 0 {
				if m.clock >= m.nextPlay {
					m.port = m.playPort
					m.call(m.play)
					m.nextPlay += m.period
					continue
				}
				m.tick(int(min(8, t-m.clock, m.nextPlay-m.clock)))
				continue
			}
			m.callAt = -1
			if m.interrupts() {
				continue
			}
			m.tick(int(min(8, t-m.clock)))
			continue
		}
		if m.play == 0 && m.interrupts() {
			continue
		}
		if m.callAt >= 0 && m.clock-m.callAt > maxCall*cpuHz || m.cpu.jammed {
			m.cpu.pc, m.cpu.jammed, m.cpu.s = retAddr, false, 0xff
			continue
		}
		m.tick(m.cpu.step())
	}
}

// interrupts da la NMI o la IRQ que haya pendiente, y dice si dio alguna.
func (m *c64) interrupts() bool {
	if m.cia[1].irq() && !m.cia[1].nmiLine {
		m.cia[1].nmiLine = true
		m.cpu.interrupt(0xfffa)
		m.tick(7)
		return true
	}
	if m.cpu.p&flagI == 0 && (m.cia[0].irq() || m.vic.status&m.vic.enable&15 != 0) {
		m.cpu.interrupt(0xfffe)
		m.tick(7)
		return true
	}
	return false
}

// sidSub es lo más que avanzan los SID de una vez (unos 160 kHz).
const sidSub = 6

// skipStep son los ciclos que avanzan los SID de una vez al saltar.
const skipStep = 2000

// syncSID lleva los SID hasta el ciclo t, sacando las muestras que tocan
// (al saltar, sin muestras y a pasos largos: solo importa su estado).
func (m *c64) syncSID(t int64) {
	if m.skipping {
		for m.sidClock < t {
			n := int(min(skipStep, t-m.sidClock))
			for _, s := range m.sids {
				s.clock(n)
			}
			m.sidClock += int64(n)
		}
		return
	}
	for m.sidClock < t {
		n := int(min(int64(sidSub), t-m.sidClock, int64(math.Ceil(m.nextSample-float64(m.sidClock)))))
		n = max(n, 1)
		var l, r float64
		for _, s := range m.sids {
			o := s.clock(n)
			l += o * (1 - s.pan)
			r += o * s.pan
		}
		m.accL += l * float64(n)
		m.accR += r * float64(n)
		m.accN += n
		m.sidClock += int64(n)
		if float64(m.sidClock) >= m.nextSample {
			k := float64(m.accN)
			m.out = append(m.out,
				clamp1(m.dc[0].next(m.accL/k)*m.gain),
				clamp1(m.dc[1].next(m.accR/k)*m.gain))
			m.accL, m.accR, m.accN = 0, 0, 0
			m.nextSample += m.cps
		}
	}
}

// Skip avanza frames cuadros sin sonar, mucho más rápido que Render (para
// saltar): la CPU va igual, pero los SID avanzan a pasos largos y su filtro
// empieza de cero al acabar.
func (m *c64) Skip(frames int) {
	m.skipping = true
	m.run(m.clock + int64(float64(frames)*m.cps))
	m.syncSID(m.clock)
	m.skipping = false
	for _, s := range m.sids {
		s.lp, s.bp = 0, 0
	}
	m.out = m.out[:0]
	m.accL, m.accR, m.accN = 0, 0, 0
	m.nextSample = float64(m.sidClock) + m.cps
}

// Render da las siguientes muestras.
func (m *c64) Render(dst []float32) int {
	for len(m.out) < len(dst) {
		need := (len(dst) - len(m.out)) / 2
		t := m.clock + int64(float64(need+1)*m.cps)
		m.run(t)
		m.syncSID(m.clock)
	}
	n := copy(dst, m.out)
	m.out = m.out[:copy(m.out, m.out[n:])]
	return n &^ 1
}

// psid es un .sid leído.
type psid struct {
	rsid                 bool
	data                 []byte
	load, init, playAt   uint16
	songs, start         int
	speed                uint32
	flags                uint16
	sidAt                []uint16
	name, author, issued string
	lengths              []time.Duration // de Songlengths.md5, si está
}

// loadSID lee un .sid.
func loadSID(b []byte, path string) (song, error) {
	if len(b) < 0x76 || string(b[:4]) != "PSID" && string(b[:4]) != "RSID" {
		return nil, ErrFormat
	}
	be := binary.BigEndian
	p := &psid{rsid: string(b[:4]) == "RSID"}
	version := be.Uint16(b[4:])
	off := int(be.Uint16(b[6:]))
	p.load, p.init, p.playAt = be.Uint16(b[8:]), be.Uint16(b[10:]), be.Uint16(b[12:])
	p.songs, p.start = int(be.Uint16(b[14:])), int(be.Uint16(b[16:]))
	p.speed = be.Uint32(b[18:])
	p.name, p.author, p.issued = fixed(b[0x16:0x36]), fixed(b[0x36:0x56]), fixed(b[0x56:0x76])
	p.sidAt = []uint16{0xd400}
	if version >= 2 && len(b) >= 0x7c {
		p.flags = be.Uint16(b[0x76:])
		for _, x := range b[0x7a:0x7c] {
			if version >= 3 && x != 0 && x&1 == 0 && (x >= 0x42 && x <= 0x7f || x >= 0xe0) {
				p.sidAt = append(p.sidAt, 0xd000|uint16(x)<<4)
			}
		}
		if version < 4 && len(p.sidAt) > 2 {
			p.sidAt = p.sidAt[:2]
		}
	}
	if off > len(b) || p.songs < 1 {
		return nil, ErrFormat
	}
	if p.flags&1 != 0 {
		return nil, errors.New("chip: Compute's Sidplayer (MUS) tunes are not supported")
	}
	if p.rsid && p.flags&2 != 0 {
		return nil, errors.New("chip: BASIC sid tunes are not supported")
	}
	p.data = b[off:]
	if p.load == 0 {
		if len(p.data) < 2 {
			return nil, ErrFormat
		}
		p.load = binary.LittleEndian.Uint16(p.data)
		p.data = p.data[2:]
	}
	if p.init == 0 {
		p.init = p.load
	}
	if int(p.load)+len(p.data) > 0x10000 {
		p.data = p.data[:0x10000-int(p.load)]
	}
	p.songs = min(p.songs, 256)
	p.lengths = songLengths(path, b)
	return p, nil
}

// sidDefault es lo que suena una canción de la que no se sabe lo que dura
// (con el fundido al final).
const sidDefault = 3 * time.Minute

func (p *psid) tracks() []Track {
	out := make([]Track, p.songs)
	for i := range out {
		out[i] = Track{Song: i + 1, Game: p.name, Author: p.author, System: "Commodore 64",
			Length: sidDefault, Loops: true}
		if i < len(p.lengths) && p.lengths[i] > 0 {
			out[i].Length, out[i].Loops = p.lengths[i], false
		}
	}
	return out
}

// bankFor es el puerto $01 con el que se llama a una rutina de un PSID: con
// la ROM del BASIC y del KERNAL visibles salvo que la rutina esté debajo.
func bankFor(a uint16) byte {
	switch {
	case a < 0xa000:
		return 0x37
	case a < 0xd000:
		return 0x36
	case a >= 0xe000:
		return 0x35
	}
	return 0x34
}

func (p *psid) play(n, rate int) (Tune, error) {
	m := &c64{pal: p.flags>>2&3 != 2, callAt: -1}
	frame, line := palFrame, palLine
	clock := float64(sidClockPAL)
	if !m.pal {
		frame, line, clock = ntscFrame, ntscLine, sidClockNTSC
	}
	m.vic.frame, m.vic.line = frame, line
	m.cps = clock / float64(rate)
	m.nextSample = m.cps
	m.cpu.mem = m
	m.buildKernal()
	for i := range m.cia {
		// Al encenderse, los temporizadores están en $FFFF.
		m.cia[i].la, m.cia[i].lb, m.cia[i].ta, m.cia[i].tb = 0xffff, 0xffff, 0xffff, 0xffff
	}
	for i, at := range p.sidAt {
		model := p.flags >> 4 & 3
		if i > 0 {
			model = p.flags >> (4 + 2*uint(i)) & 3
			if model == 0 {
				model = p.flags >> 4 & 3
			}
		}
		pan := 0.5
		if len(p.sidAt) > 1 {
			pan = 0.2 + 0.6*float64(i)/float64(len(p.sidAt)-1)
		}
		m.sids = append(m.sids, newSID(model == 2, pan))
		m.sidAt = append(m.sidAt, at)
	}
	m.gain = 0.9 / math.Sqrt(float64(len(m.sids)))
	copy(m.ram[p.load:], p.data)
	// Lo que deja el KERNAL al arrancar: los vectores, la bandera PAL y el
	// CIA 1 dando su interrupción de 60 Hz.
	m.ram[0x0314], m.ram[0x0315] = 0x31, 0xea
	m.ram[0x0316], m.ram[0x0317] = 0x81, 0xea
	m.ram[0x0318], m.ram[0x0319] = 0x47, 0xfe
	if m.pal {
		m.ram[0x02a6] = 1
	}
	m.cia[0].write(4, 0x25)
	m.cia[0].write(5, 0x40)
	m.cia[0].write(13, 0x81)
	m.cia[0].write(14, 0x01)
	m.port = 0x37
	if !p.rsid {
		m.port = bankFor(p.init)
	}
	// El inicio, hasta que vuelva (o, en un RSID que se queda en su bucle,
	// hasta maxCall: el bucle sigue luego como programa principal).
	m.call(p.init)
	m.cpu.a = byte(n - 1)
	if p.rsid {
		m.cpu.p &^= flagI // en un RSID, con las interrupciones del KERNAL en marcha
	}
	for limit := m.clock + int64(clock)*maxCall; m.cpu.pc != retAddr && m.clock < limit && !m.cpu.jammed; {
		if !m.interrupts() {
			m.tick(m.cpu.step())
		}
	}
	m.callAt = -1
	if !p.rsid && p.playAt != 0 {
		m.play, m.playPort = p.playAt, bankFor(p.playAt)
		m.period = int64(frame)
		if p.speed>>uint(min(n-1, 31))&1 != 0 {
			m.period = int64(m.cia[0].la)
			if m.period == 0 {
				m.period = 0x4025
			}
		}
		m.nextPlay = m.clock
	} else if m.cpu.pc == retAddr {
		m.cpu.p &^= flagI // la rutina ya instaló su interrupción
	}
	// Lo que sonó durante el inicio no cuenta.
	m.sidClock, m.out = m.clock, nil
	m.accL, m.accR, m.accN = 0, 0, 0
	m.nextSample = float64(m.clock) + m.cps
	return m, nil
}
