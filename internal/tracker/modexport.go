package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Exportar una canción a XM o a MOD. Primero se aplana (flatten): cada fila
// de la canción da un patrón, con lo que toca cada canal en esa fila (sus
// cadenas, frase tras frase, con la transposición de la cadena y los saltos
// H), tan largo como la cadena más larga, igual que en el reproductor. Lo
// que cambia el tiempo para todos (el speed de cada paso según el groove, y
// T) va en efectos F: en XM en canales de control aparte, en MOD en el
// primer canal que tenga el efecto libre.
//
// Los instrumentos que no son de sample se convierten en uno: un ciclo de
// su forma (el pulso con su ancho, la onda, la forma del synth) en bucle, o
// ruido; su envolvente pasa a la de volumen del XM. Las tablas, los
// comandos sin equivalente (A, E, F, M, S, W, X, Z...) y un groove distinto
// en cada canal se pierden.

// flatSong es una canción aplanada, lista para escribir.
type flatSong struct {
	channels int         // los de la canción
	ctrl     int         // los de control (XM)
	rows     [][]flatRow // por fila de la canción, sus filas de módulo
	speed    int         // el speed y el BPM del principio
	bpm      int
	instrs   []int // los instrumentos del tracker que se exportan (índice+1 = el del módulo)
}

// flatRow es una fila de módulo: una celda por canal y los efectos de
// control (speed, BPM, corte de patrón).
type flatRow struct {
	cells []modCell
	ctrl  [][2]byte // efecto, parámetro
}

// flatten aplana la canción para exportarla (xm: frecuencias lineales).
func flatten(s *Song, xm bool) *flatSong {
	n := s.Channels()
	f := &flatSong{channels: n, bpm: s.BPM}
	instrIdx := map[int]int{}
	instrOf := func(id int) int {
		if i, ok := instrIdx[id]; ok {
			return i
		}
		f.instrs = append(f.instrs, id)
		instrIdx[id] = len(f.instrs)
		return instrIdx[id]
	}
	g := s.Grooves[s.Groove]
	if len(g) == 0 {
		g = Groove{6, 6}
	}
	gpos, lastSpeed := 0, -1
	f.speed = g[0]
	lastInstr := make([]int, n)
	prevNote := make([]float64, n)
	for i := range lastInstr {
		lastInstr[i], prevNote[i] = Empty, 49
	}
	for _, row := range s.Rows {
		type seqStep struct {
			st Step
			tr int
		}
		seqs := make([][]seqStep, n)
		length := 0
		for ch := 0; ch < n; ch++ {
			c := s.Chains[int(row[ch])]
			if row[ch] == Empty || c == nil {
				continue
			}
			start := 0
			for _, cs := range c {
				if cs.Phrase == Empty {
					break
				}
				ph := s.Phrases[int(cs.Phrase)]
				if ph == nil {
					ph = NewPhrase()
				}
				next := 0
				for k := start; k < Steps; k++ {
					seqs[ch] = append(seqs[ch], seqStep{ph[k], int(cs.Transpose)})
					if hop := cmdOf(ph[k], CmdHop); hop != nil {
						next = int(hop.Arg) % Steps
						break
					}
				}
				start = next
			}
			length = max(length, len(seqs[ch]))
		}
		if length == 0 {
			length = Steps // una fila sin cadenas descansa una frase
		}
		var out []flatRow
		for i := 0; i < length; i++ {
			fr := flatRow{cells: make([]modCell, n)}
			speed := g[gpos%len(g)]
			gpos++
			if speed != lastSpeed {
				fr.ctrl = append(fr.ctrl, [2]byte{0xF, byte(min(31, speed))})
				lastSpeed = speed
			}
			if !xm && len(f.rows) == 0 && i == 0 && s.BPM != 125 {
				// El MOD no guarda el BPM en la cabecera: va en la primera fila.
				fr.ctrl = append(fr.ctrl, [2]byte{0xF, byte(max(32, min(255, s.BPM)))})
			}
			for ch := 0; ch < n; ch++ {
				cell := modCell{vol: -1, pan: -1, porta: -1}
				if i < len(seqs[ch]) {
					ss := seqs[ch][i]
					st := ss.st
					if st.Instr != Empty && s.Instrs[int(st.Instr)] != nil {
						lastInstr[ch] = int(st.Instr)
						cell.instr = instrOf(int(st.Instr))
					}
					in := s.Instrs[lastInstr[ch]]
					switch {
					case st.Note > 0:
						note := int(st.Note)
						if in == nil || in.Transpose {
							note += ss.tr
						}
						cell.note = int8(max(1, min(NoteMax, note)))
					case st.Note == NoteOff:
						cell.note = NoteOff
					}
					if st.Vol != Empty {
						cell.vol = int(math.Round(float64(st.Vol) * 64 / 15))
					}
					for _, c := range st.Cmds {
						switch c.Code {
						case CmdGroove:
							if ng := s.Grooves[int(c.Arg)]; len(ng) > 0 {
								g, gpos = ng, 0
							}
						case CmdTempo:
							if c.Arg >= 0x20 {
								fr.ctrl = append(fr.ctrl, [2]byte{0xF, c.Arg})
							}
						}
						if cell.fx == 0 && cell.param == 0 {
							cell.fx, cell.param = exportFx(c, cell.note, prevNote[ch], xm)
						}
					}
					if cell.note > 0 {
						prevNote[ch] = float64(cell.note)
					}
				}
				fr.cells[ch] = cell
			}
			f.ctrl = max(f.ctrl, len(fr.ctrl))
			out = append(out, fr)
		}
		f.rows = append(f.rows, out)
	}
	return f
}

// cmdOf es el comando code de un paso (nil si no lo tiene).
func cmdOf(st Step, code byte) *Cmd {
	for i := range st.Cmds {
		if st.Cmds[i].Code == code {
			return &st.Cmds[i]
		}
	}
	return nil
}

// exportFx pasa un comando a un efecto de módulo (0, 0 si no tiene).
func exportFx(c Cmd, note int8, prev float64, xm bool) (byte, byte) {
	a := c.Arg
	switch c.Code {
	case CmdChord:
		return 0, a
	case CmdPitch:
		cents := float64(int8(a))
		if cents == 0 {
			return 0, 0
		}
		fx := byte(1)
		if cents < 0 {
			fx = 2
		}
		var v float64
		if xm {
			v = math.Abs(cents) * 16 / 100
		} else {
			p := modPeriod(prev)
			v = p * (1 - math.Pow(2, -math.Abs(cents)/1200))
		}
		return fx, byte(max(1, min(255, math.Round(v))))
	case CmdSlide:
		if note <= 0 {
			return 0, 0
		}
		ticks := float64(max(1, int(a)))
		var v float64
		if xm {
			v = math.Abs(float64(note)-prev) * 16 / ticks
		} else {
			v = math.Abs(modPeriod(float64(note))-modPeriod(prev)) / ticks
		}
		return 3, byte(max(1, min(255, math.Ceil(v))))
	case CmdVibrato:
		return 4, a
	case CmdKill:
		if a > 0 {
			return 0xE, 0xC0 | min(a, 15)
		}
	case CmdDelay:
		if a > 0 {
			return 0xE, 0xD0 | min(a, 15)
		}
	case CmdRetrig:
		if a&15 > 0 {
			return 0xE, 0x90 | a&15
		}
	case CmdOutput:
		switch a & 3 {
		case 1:
			return 8, 0x00
		case 2:
			return 8, 0xFF
		case 3:
			return 8, 0x80
		}
	}
	return 0, 0
}

// moduleSample es el sample con que suena un instrumento en un módulo, y la
// nota a la que suena a su Rate.
func moduleSample(s *Song, in *Instrument) (*SampleData, int) {
	root := int(in.Root)
	if root == 0 {
		root = 49
	}
	if in.Type == InstrSample {
		if d := s.Samples[int(in.Sample)]; d != nil && d.Len() > 0 {
			return d, root
		}
		return nil, 49
	}
	// Un ciclo de la forma, en bucle, que a C-4 suena a su nota.
	cycle := func(n int, f func(x float64) float64) (*SampleData, int) {
		d := &SampleData{Bits: 8, Data: make([]byte, n), Rate: int(math.Round(noteHz(49) * float64(n))), LoopLen: n}
		for i := range d.Data {
			d.Data[i] = byte(int8(max(-127, min(127, math.Round(f(float64(i)/float64(n))*100)))))
		}
		return d, 49
	}
	duty := int(in.Duty & 3)
	switch in.Type {
	case InstrPulse:
		if s.Mode == ModeGB {
			return cycle(64, func(x float64) float64 { return dutyPatterns[duty][int(x*8)&7] })
		}
		return cycle(64, func(x float64) float64 { return synthShape("square", x, duty) })
	case InstrWave:
		w := s.Waves[int(in.Wave)]
		if w == nil {
			pw := PresetWaves()[WaveTriangle]
			w = &pw
		}
		return cycle(WaveLen, func(x float64) float64 { return float64(w[int(x*WaveLen)%WaveLen])/7.5 - 1 })
	case InstrSynth:
		return cycle(64, func(x float64) float64 { return synthShape(in.Shape, x, duty) })
	case InstrNoise:
		// El registro del ruido, un bit por muestra: a C-4 avanza como en la
		// voz (la frecuencia de la nota ×8).
		const n = 8192
		d := &SampleData{Bits: 8, Data: make([]byte, n), Rate: int(math.Round(noteHz(49) * 8)), LoopLen: n}
		lfsr := uint16(0x7fff)
		for i := range d.Data {
			bit := (lfsr ^ lfsr>>1) & 1
			lfsr = lfsr>>1 | bit<<14
			if in.Short {
				lfsr = lfsr&^0x40 | bit<<6
			}
			d.Data[i] = byte(int8(int(^lfsr&1)*200 - 100))
		}
		return d, 49
	}
	return nil, 49
}

// semitonesFrom es cuántos semitonos (con decimales) sube un sample a
// Rate (que a la nota root suena tal cual) respecto de uno a ref Hz en C-4.
func semitonesFrom(d *SampleData, root int, ref float64) float64 {
	return 12*math.Log2(float64(d.Rate)/ref) + float64(49-root)
}

// ExportXM escribe la canción como módulo XM (FastTracker 2).
func ExportXM(s *Song) ([]byte, error) {
	f := flatten(s, true)
	nch := f.channels + f.ctrl
	if nch%2 == 1 {
		nch++ // FastTracker 2 solo admite un número par de canales
	}
	var b bytes.Buffer
	le := binary.LittleEndian
	w16 := func(v int) { binary.Write(&b, le, uint16(v)) }
	w32 := func(v int) { binary.Write(&b, le, uint32(v)) }
	str := func(t string, n int) {
		out := make([]byte, n)
		copy(out, t)
		b.Write(out)
	}
	// Los patrones: uno por fila de la canción (los iguales, una vez).
	var pats [][]byte
	var orders []int
	seen := map[string]int{}
	for _, rows := range f.rows {
		for start := 0; start < len(rows); start += 256 {
			chunk := rows[start:min(len(rows), start+256)]
			data := xmPattern(chunk, f.channels, nch)
			key := string(data) + fmt.Sprint(len(chunk))
			id, ok := seen[key]
			if !ok {
				id = len(pats)
				seen[key] = id
				pats = append(pats, xmPatternBlock(data, len(chunk)))
			}
			orders = append(orders, id)
		}
	}
	if len(orders) > 256 {
		return nil, fmt.Errorf("song too long for XM (%d patterns in order)", len(orders))
	}
	b.WriteString(xmMagic)
	str(s.Name, 20)
	b.WriteByte(0x1A)
	str("audiofilus", 20)
	w16(0x0104)
	w32(276)
	w16(len(orders))
	w16(0)
	w16(nch)
	w16(len(pats))
	w16(len(f.instrs))
	w16(1) // frecuencias lineales
	w16(f.speed)
	w16(max(32, min(255, f.bpm)))
	ord := make([]byte, 256)
	for i, o := range orders {
		ord[i] = byte(o)
	}
	b.Write(ord)
	for _, p := range pats {
		b.Write(p)
	}
	for _, id := range f.instrs {
		writeXMInstrument(&b, s, s.Instrs[id])
	}
	return b.Bytes(), nil
}

// xmPattern empaqueta las filas de un patrón (cells de ch canales, y los de
// control hasta nch).
func xmPattern(rows []flatRow, ch, nch int) []byte {
	var b bytes.Buffer
	for _, r := range rows {
		for c := 0; c < nch; c++ {
			var note, inst, vol, fx, param byte
			if c < ch {
				cell := r.cells[c]
				switch {
				case cell.note == NoteOff:
					note = 97
				case cell.note > 0:
					n := int(cell.note)
					for n > 96 {
						n -= 12
					}
					note = byte(n)
				}
				inst = byte(cell.instr)
				if cell.vol >= 0 {
					vol = byte(0x10 + min(64, cell.vol))
				}
				fx, param = cell.fx, cell.param
			} else if k := c - ch; k < len(r.ctrl) {
				fx, param = r.ctrl[k][0], r.ctrl[k][1]
			}
			flags := byte(0x80)
			for i, v := range []byte{note, inst, vol, fx, param} {
				if v != 0 {
					flags |= 1 << i
				}
			}
			b.WriteByte(flags)
			for i, v := range []byte{note, inst, vol, fx, param} {
				if flags&(1<<i) != 0 {
					b.WriteByte(v)
				}
			}
		}
	}
	return b.Bytes()
}

// xmPatternBlock es un patrón con su cabecera.
func xmPatternBlock(data []byte, rows int) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&b, le, uint32(9))
	b.WriteByte(0)
	binary.Write(&b, le, uint16(rows))
	binary.Write(&b, le, uint16(len(data)))
	b.Write(data)
	return b.Bytes()
}

// writeXMInstrument escribe un instrumento XM con un sample (el de
// moduleSample) y la envolvente de volumen del instrumento.
func writeXMInstrument(b *bytes.Buffer, s *Song, in *Instrument) {
	le := binary.LittleEndian
	d, root := moduleSample(s, in)
	h := make([]byte, 263)
	copy(h[4:26], in.Name)
	if d == nil {
		le.PutUint32(h, 29)
		b.Write(h[:29])
		return
	}
	le.PutUint32(h, 263)
	le.PutUint16(h[27:], 1)
	le.PutUint32(h[29:], 40)
	// La envolvente de volumen.
	type pt struct{ x, y int }
	var pts []pt
	sus := 0
	fade := 0xFFF // sin release: al soltar se corta (como en el tracker)
	if s.Mode == ModeGB {
		e := in.Env
		y0 := int(e.Start) * 64 / 15
		pts = append(pts, pt{0, y0})
		if e.Period > 0 && e.Dir != 0 {
			tps := float64(e.Period) * float64(s.BPM) / 160 // ticks por paso de la envolvente
			steps := int(e.Start)
			end := 0
			if e.Dir > 0 {
				steps, end = 15-int(e.Start), 64
			}
			pts = append(pts, pt{max(1, int(math.Round(float64(steps)*tps))), end})
		} else {
			pts = append(pts, pt{1, y0})
		}
		sus = len(pts) - 1
	} else {
		e := in.Env
		sy := int(e.Sustain) * 64 / 15
		if e.Attack > 0 {
			pts = append(pts, pt{0, 0}, pt{e.Attack, 64})
		} else {
			pts = append(pts, pt{0, 64})
		}
		pts = append(pts, pt{pts[len(pts)-1].x + max(1, e.Decay), sy})
		sus = len(pts) - 1
		if e.Release > 0 {
			fade = max(1, min(0xFFF, 32768/e.Release))
		}
	}
	for i, p := range pts {
		le.PutUint16(h[129+4*i:], uint16(min(0xFFFF, p.x)))
		le.PutUint16(h[131+4*i:], uint16(p.y))
	}
	h[225] = byte(len(pts))
	h[227] = byte(sus)
	h[233] = 1 | 2 // activa, con sostener
	le.PutUint16(h[239:], uint16(fade))
	b.Write(h)
	// El sample.
	data := d.Data
	bits16 := d.Bits == 16
	div := 1
	if bits16 {
		div = 2
	}
	sh := make([]byte, 40)
	le.PutUint32(sh[0:], uint32(len(data)))
	typ := byte(0)
	if in.Type != InstrSample || in.Loop {
		if d.LoopLen > 1 {
			le.PutUint32(sh[4:], uint32(d.LoopStart*div))
			le.PutUint32(sh[8:], uint32(d.LoopLen*div))
			typ = 1
			if d.PingPong {
				typ = 2
			}
		}
	}
	if bits16 {
		typ |= 16
	}
	sh[12] = byte(max(0, min(64, int(math.Round(float64(in.Volume)*64/15)))))
	semis := semitonesFrom(d, root, 8363)
	rel := math.Round(semis)
	sh[13] = byte(int8(max(-128, min(127, math.Round((semis-rel)*128)))))
	sh[14] = typ
	sh[15] = map[int8]byte{-1: 0x20, 0: 0x80, 1: 0xE0}[in.Pan]
	sh[16] = byte(int8(max(-96, min(95, rel))))
	copy(sh[18:], in.Name)
	b.Write(sh)
	b.Write(delta(data, bits16))
}

// delta codifica un sample como los guarda XM.
func delta(data []byte, bits16 bool) []byte {
	out := make([]byte, len(data))
	if bits16 {
		var prev int16
		for i := 0; i+1 < len(data); i += 2 {
			v := int16(binary.LittleEndian.Uint16(data[i:]))
			binary.LittleEndian.PutUint16(out[i:], uint16(v-prev))
			prev = v
		}
		return out
	}
	var prev int8
	for i, x := range data {
		out[i] = byte(int8(x) - prev)
		prev = int8(x)
	}
	return out
}

// modPeriods son los periodos de ProTracker, de C-1 (856) a B-3 (113): las
// notas 37 a 72 del tracker (C-4 = 428).
var modPeriods = []int{
	856, 808, 762, 720, 678, 640, 604, 570, 538, 508, 480, 453,
	428, 404, 381, 360, 340, 320, 302, 285, 269, 254, 240, 226,
	214, 202, 190, 180, 170, 160, 151, 143, 135, 127, 120, 113,
}

// ExportMOD escribe la canción como MOD de ProTracker (4 canales, 31
// samples de 8 bits). Si usa más de 4 canales, error.
func ExportMOD(s *Song) ([]byte, error) {
	f := flatten(s, false)
	used := 0
	for _, rows := range f.rows {
		for _, r := range rows {
			for c, cell := range r.cells {
				if cell.note != 0 || cell.instr != 0 || cell.fx != 0 || cell.param != 0 {
					used = max(used, c+1)
				}
			}
		}
	}
	if used > 4 {
		return nil, fmt.Errorf("MOD has 4 channels; this song uses %d", used)
	}
	if len(f.instrs) > 31 {
		f.instrs = f.instrs[:31] // los demás quedan sin instrumento
	}
	// Los samples: a 8 bits, y cuántos semitonos se corren sus notas para
	// que suenen a su altura (el ajuste fino, en el sample).
	type smp struct {
		data              []byte
		vol, fine         int
		loopStart, loopLn int
		shift             int
		name              string
	}
	smps := make([]smp, 31)
	for i, id := range f.instrs {
		in := s.Instrs[id]
		d, root := moduleSample(s, in)
		if d == nil {
			continue
		}
		data := d.Data
		if d.Bits == 16 {
			data = make([]byte, d.Len())
			for k := range data {
				data[k] = byte(int8(d.At(k) * 127))
			}
		}
		data = data[:min(len(data), 131070)&^1]
		semis := semitonesFrom(d, root, palClock/(2*428))
		shift := int(math.Round(semis))
		sm := smp{data: data, vol: max(0, min(64, int(math.Round(float64(in.Volume)*64/15)))), shift: shift,
			fine: max(-8, min(7, int(math.Round((semis-float64(shift))*8)))), name: in.Name}
		if (in.Type != InstrSample || in.Loop) && d.LoopLen > 1 {
			sm.loopStart, sm.loopLn = min(d.LoopStart, len(data))&^1, min(d.LoopLen, len(data)-d.LoopStart)&^1
		}
		smps[i] = sm
	}
	// Los patrones de 64 filas; el último trozo de cada fila de la canción
	// termina con un corte (D00).
	var pats [][]byte
	var orders []int
	seen := map[string]int{}
	lastInstr := [4]int{}
	for _, rows := range f.rows {
		for start := 0; start < len(rows); start += 64 {
			chunk := rows[start:min(len(rows), start+64)]
			p := make([]byte, 64*4*4)
			for r, fr := range chunk {
				ctrl := fr.ctrl
				if r == len(chunk)-1 && len(chunk) < 64 {
					ctrl = append(ctrl, [2]byte{0xD, 0})
				}
				for c := 0; c < 4 && c < len(fr.cells); c++ {
					cell := fr.cells[c]
					if cell.instr > 0 && cell.instr <= 31 && smps[cell.instr-1].data != nil {
						lastInstr[c] = cell.instr
					}
					period := 0
					fx, param := cell.fx, cell.param
					switch {
					case cell.note > 0:
						n := int(cell.note)
						if li := lastInstr[c]; li > 0 {
							n += smps[li-1].shift
						}
						for n < 37 {
							n += 12
						}
						for n > 72 {
							n -= 12
						}
						period = modPeriods[n-37]
					case cell.note == NoteOff && fx == 0 && param == 0:
						fx, param = 0xE, 0xC0
					}
					if cell.vol >= 0 && fx == 0 && param == 0 {
						fx, param = 0xC, byte(min(64, cell.vol))
					}
					if fx == 0 && param == 0 && len(ctrl) > 0 {
						fx, param = ctrl[0][0], ctrl[0][1]
						ctrl = ctrl[1:]
					}
					inst := cell.instr
					if inst > 31 {
						inst = 0 // de los que no cupieron
					}
					o := (r*4 + c) * 4
					p[o] = byte(inst&0xF0) | byte(period>>8)
					p[o+1] = byte(period)
					p[o+2] = byte(inst&0x0F)<<4 | fx&0x0F
					p[o+3] = param
				}
			}
			key := string(p)
			id, ok := seen[key]
			if !ok {
				id = len(pats)
				seen[key] = id
				pats = append(pats, p)
			}
			orders = append(orders, id)
		}
	}
	if len(orders) > 128 {
		return nil, fmt.Errorf("song too long for MOD (%d patterns in order, max 128)", len(orders))
	}
	if len(pats) > 100 {
		return nil, errors.New("song too long for MOD (more than 100 different patterns)")
	}
	var b bytes.Buffer
	be := binary.BigEndian
	name := make([]byte, 20)
	copy(name, s.Name)
	b.Write(name)
	for _, sm := range smps {
		h := make([]byte, 30)
		copy(h[:22], sm.name)
		be.PutUint16(h[22:], uint16(len(sm.data)/2))
		h[24] = byte(sm.fine) & 0x0F
		h[25] = byte(sm.vol)
		be.PutUint16(h[26:], uint16(sm.loopStart/2))
		ll := sm.loopLn / 2
		if ll == 0 {
			ll = 1 // sin bucle
		}
		be.PutUint16(h[28:], uint16(ll))
		b.Write(h)
	}
	b.WriteByte(byte(len(orders)))
	b.WriteByte(127)
	ord := make([]byte, 128)
	for i, o := range orders {
		ord[i] = byte(o)
	}
	b.Write(ord)
	if len(pats) > 64 {
		b.WriteString("M!K!")
	} else {
		b.WriteString("M.K.")
	}
	for _, p := range pats {
		b.Write(p)
	}
	for _, sm := range smps {
		b.Write(sm.data)
	}
	return b.Bytes(), nil
}
