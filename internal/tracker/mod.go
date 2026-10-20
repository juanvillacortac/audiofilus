package tracker

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Los módulos (MOD de ProTracker y XM de FastTracker 2): importarlos como
// canciones del tracker (en modo Libre, con instrumentos de sample) y
// exportar una canción a ellos. Los dos formatos pasan por la misma forma
// intermedia, modSong: patrones de filas por canal, con su nota, su
// instrumento, su volumen y un efecto, como los guardan los dos.
//
// Al importar, cada patrón se parte en frases de 16 pasos por canal, que
// hacen una cadena por patrón y canal (las frases y cadenas iguales se
// comparten), y cada posición del orden es una fila de la canción. El tempo
// cuadra justo: una fila de módulo dura speed ticks de 2,5/BPM s, y un paso
// del tracker groove ticks de 60/(BPM·24) s, que es lo mismo; así que el
// BPM del módulo es el de la canción y cada speed, un groove de un valor.
//
// Los efectos se pasan a comandos lo mejor que se puede:
//
//	0xy arpegio            → C xy
//	1xx/2xx portamento     → P (centésimas de semitono por tick)
//	3xx portamento a nota  → L (los ticks que tarda en llegar)
//	4xy vibrato            → V xy
//	5xy/6xy (con volumen)  → L / V (sin el deslizamiento de volumen)
//	8xx paneo              → O (izquierda, centro o derecha)
//	9xx desplazamiento     → nada
//	Axy, EAx/EBx volumen que se desliza → el volumen del paso, fila a
//	                       fila (el tracker no desliza dentro de un paso)
//	Bxx/Dxx salto, corte   → el patrón termina ahí (H en todos los canales)
//	Cxx volumen            → el volumen del paso (0..64 → 0..F)
//	E9x redoble            → R x
//	ECx cortar             → K x (EC0: la nota se corta, OFF)
//	EDx retrasar           → D x
//	Fxx speed / BPM        → G (un groove por speed) / T, en todos los canales
//
// Lo que no cabe en los dos comandos de un paso se pierde (primero van el
// salto y el speed, que cambian el tiempo).

// modCell es una celda de un módulo.
type modCell struct {
	note  int8 // 0 nada, NoteOff, o la nota del tracker (1 = C-0)
	instr int  // 0 nada, o el instrumento (desde 1)
	vol   int  // -1 nada, o 0..64
	fx    byte // el efecto (0..35, como en XM: 10 = A, 15 = F...)
	param byte
	pan   int // -1 nada, o 0..255 (la columna de volumen de XM)
	porta int // -1 nada, o la velocidad de un 3xx de la columna de volumen
	// El volumen que se desliza desde la columna de volumen de XM: por tick
	// (slide) o una vez en la fila (fine), en unidades de 0..64.
	slide, fine int
}

// modPattern es un patrón: filas × canales.
type modPattern [][]modCell

// modSong es un módulo, MOD o XM.
type modSong struct {
	name     string
	channels int
	orders   []int
	patterns []modPattern
	speed    int // ticks por fila
	bpm      int
	linear   bool // XM con frecuencias lineales (si no, periodos de Amiga)
	instrs   []*Instrument
	samples  []*SampleData // samples[i] es el de instrs[i] (nil: sin sample)
	// defVol es el volumen con que empieza cada instrumento (0..64): en los
	// módulos es el del canal al tocar la nota, no un multiplicador, así que
	// va en el volumen del paso y el instrumento queda a F.
	defVol []int
	speed0 int // el speed al empezar el patrón que se convierte
}

// modPeriod es el periodo de Amiga de una nota del tracker (C-4 = 428).
func modPeriod(note float64) float64 { return 428 * math.Pow(2, (49-note)/12) }

// periodNote es la nota del tracker de un periodo de Amiga.
func periodNote(period int) int8 {
	if period <= 0 {
		return 0
	}
	n := 49 + math.Round(12*math.Log2(428/float64(period)))
	return int8(max(1, min(NoteMax, n)))
}

// palClock es el reloj de Paula (PAL): un sample suena a palClock/(2·periodo)
// muestras por segundo; el de C-4 (428), a unos 8287 Hz.
const palClock = 7093789.2

// chanState es lo que el importador recuerda de cada canal.
type chanState struct {
	note    float64 // la última nota (para los portamentos)
	porta   byte    // el último 3xx
	vibrato byte    // el último 4xy
	vol     int     // el volumen del canal (0..64)
}

// toSong convierte un módulo en canción del tracker.
func (m *modSong) toSong() (*Song, error) {
	s := NewSong(strings.TrimSpace(m.name), ModeFree)
	if s.Name == "" {
		s.Name = "Module"
	}
	s.Instrs, s.Rows, s.Samples = map[int]*Instrument{}, nil, map[int]*SampleData{}
	s.BPM = max(MinBPM, min(MaxBPM, m.bpm))
	speed := max(1, min(31, m.speed))
	m.speed0 = speed
	grooveOf := map[int]int{speed: 0}
	s.Grooves = map[int]Groove{0: {speed}}
	groove := func(sp int) int {
		sp = max(1, min(32, sp))
		if id, ok := grooveOf[sp]; ok {
			return id
		}
		id := len(grooveOf)
		if id >= NumGrooves {
			return 0
		}
		grooveOf[sp] = id
		s.Grooves[id] = Groove{sp}
		return id
	}
	for i, in := range m.instrs {
		if in == nil || i >= NumInstr {
			continue
		}
		s.Instrs[i] = in
		if i < len(m.samples) && m.samples[i] != nil {
			s.Samples[i] = m.samples[i]
			in.Sample = int16(i)
		}
	}
	nch := min(m.channels, MaxChannels)
	phraseIDs, chainIDs := map[string]int{}, map[string]int{}
	chainOf := map[[2]int]int{} // (patrón, canal) → cadena
	state := make([]chanState, nch)
	for i := range state {
		state[i].note, state[i].vol = 49, 64
	}
	addPhrase := func(p *Phrase) int {
		b, _ := json.Marshal(p)
		if id, ok := phraseIDs[string(b)]; ok {
			return id
		}
		id := len(phraseIDs)
		phraseIDs[string(b)] = id
		s.Phrases[id] = p
		return id
	}
	addChain := func(ids []int) int {
		key := fmt.Sprint(ids)
		if id, ok := chainIDs[key]; ok {
			return id
		}
		id := len(chainIDs)
		chainIDs[key] = id
		c := NewChain()
		for i, ph := range ids {
			c[i].Phrase = int16(ph)
		}
		s.Chains[id] = c
		return id
	}
	convert := func(pi int) {
		pat := m.patterns[pi]
		rows := min(len(pat), 16*Steps)
		// El patrón termina en el primer salto (Bxx o Dxx) de cualquier canal.
		for r := 0; r < rows; r++ {
			for _, c := range pat[r] {
				if c.fx == 0xB || c.fx == 0xD {
					rows = r + 1
				}
			}
		}
		rows = max(1, rows)
		// Lo que vale para todos los canales en cada fila: el speed y el BPM.
		global := make([][]Cmd, rows)
		speedAt := make([]int, rows)
		sp := m.speed0
		for r := 0; r < rows; r++ {
			for _, c := range pat[r] {
				if c.fx == 0xF && c.param > 0 && c.param < 0x20 {
					sp = int(c.param)
				}
			}
			speedAt[r] = sp
			for _, c := range pat[r] {
				if c.fx == 0xF && c.param > 0 {
					if c.param < 0x20 {
						// El groove vale desde el paso siguiente al que lo
						// cambia: va en la fila de antes (en la primera del
						// todo, es el groove con que empieza la canción).
						g := Cmd{CmdGroove, byte(groove(int(c.param)))}
						switch {
						case r > 0:
							global[r-1] = append(global[r-1], g)
						case len(s.Rows) == 0:
							s.Groove = int(g.Arg)
						default:
							global[r] = append(global[r], g)
						}
					} else {
						global[r] = append(global[r], Cmd{CmdTempo, c.param})
					}
				}
			}
		}
		m.speed0 = sp
		nph := (rows + Steps - 1) / Steps
		for ch := 0; ch < nch; ch++ {
			st := &state[ch]
			var ids []int
			for ph := 0; ph < nph; ph++ {
				p := NewPhrase()
				for k := 0; k < Steps; k++ {
					r := ph*Steps + k
					if r >= rows {
						break
					}
					var cell modCell
					if ch < len(pat[r]) {
						cell = pat[r][ch]
					} else {
						cell = modCell{vol: -1, pan: -1, porta: -1}
					}
					step := &p[k]
					var cmds []Cmd
					if r == rows-1 && rows%Steps != 0 {
						cmds = append(cmds, Cmd{CmdHop, 0}) // la frase (y el patrón) acaba aquí
					}
					// El BPM va en un canal; el groove, en todos.
					for _, g := range global[r] {
						if g.Code == CmdGroove || ch == 0 {
							cmds = append(cmds, g)
						}
					}
					cmds = append(cmds, m.cellCmds(cell, step, st, speedAt[r])...)
					for i := 0; i < len(step.Cmds) && i < len(cmds); i++ {
						step.Cmds[i] = cmds[i]
					}
				}
				ids = append(ids, addPhrase(p))
			}
			chainOf[[2]int{pi, ch}] = addChain(ids)
		}
	}
	for _, o := range m.orders {
		if o < 0 || o >= len(m.patterns) || len(s.Rows) >= SongRows {
			continue
		}
		if _, ok := chainOf[[2]int{o, 0}]; !ok && nch > 0 {
			convert(o)
		}
		row := EmptyRow()
		for ch := 0; ch < nch; ch++ {
			row[ch] = int16(chainOf[[2]int{o, ch}])
		}
		s.Rows = append(s.Rows, row)
	}
	if len(s.Rows) == 0 {
		return nil, errors.New("module has no patterns in its order")
	}
	s.Fix()
	return s, nil
}

// cellCmds pasa una celda a un paso: su nota, instrumento y volumen van en
// step; devuelve los comandos de su efecto.
func (m *modSong) cellCmds(c modCell, step *Step, st *chanState, speed int) []Cmd {
	var cmds []Cmd
	porta := c.fx == 3 || c.fx == 5 || c.porta >= 0
	if c.note != 0 {
		step.Note = c.note
	}
	volSet := false
	if c.instr > 0 && c.instr-1 < len(m.instrs) && m.instrs[c.instr-1] != nil && c.instr-1 < NumInstr {
		step.Instr = int16(c.instr - 1)
		if c.instr-1 < len(m.defVol) {
			st.vol, volSet = m.defVol[c.instr-1], true
		}
	}
	if c.vol >= 0 {
		st.vol, volSet = min(64, c.vol), true
	}
	// Los deslizamientos de volumen: el volumen al acabar la fila.
	ticks := max(0, speed-1)
	slide := func(d int) {
		st.vol, volSet = max(0, min(64, st.vol+d)), true
	}
	if c.slide != 0 {
		slide(c.slide * ticks)
	}
	if c.fine != 0 {
		slide(c.fine)
	}
	switch {
	case c.fx == 0xA || c.fx == 5 || c.fx == 6:
		if x, y := int(c.param>>4), int(c.param&15); x > 0 {
			slide(x * ticks)
		} else if y > 0 {
			slide(-y * ticks)
		}
	case c.fx == 0xE && c.param>>4 == 0xA:
		slide(int(c.param & 15))
	case c.fx == 0xE && c.param>>4 == 0xB:
		slide(-int(c.param & 15))
	case c.fx == 0xC:
		st.vol, volSet = min(64, int(c.param)), true
	}
	// Una nota sin volumen suena al del canal (en el tracker, a todo).
	if volSet || c.note > 0 {
		step.Vol = int8(math.Round(float64(st.vol) * 15 / 64))
	}
	if c.pan >= 0 {
		cmds = append(cmds, Cmd{CmdOutput, panArg(c.pan)})
	}
	if porta && c.note > 0 {
		var speed byte // 5xy: la velocidad es la del último 3xx
		if c.fx == 3 {
			speed = c.param
		}
		if c.porta >= 0 {
			speed = byte(c.porta)
		}
		if speed == 0 {
			speed = st.porta
		}
		st.porta = speed
		target := float64(c.note)
		ticks := 16
		if speed > 0 {
			if m.linear {
				ticks = int(math.Ceil(math.Abs(target-st.note) * 16 / float64(speed)))
			} else {
				ticks = int(math.Ceil(math.Abs(modPeriod(target)-modPeriod(st.note)) / float64(speed)))
			}
		}
		cmds = append(cmds, Cmd{CmdSlide, byte(max(1, min(255, ticks)))})
		st.note = target
	} else if c.note > 0 {
		st.note = float64(c.note)
	}
	p, x, y := c.param, c.param>>4, c.param&15
	switch c.fx {
	case 0:
		if p != 0 {
			cmds = append(cmds, Cmd{CmdChord, p})
		}
	case 1, 2:
		cents := 0.0
		if m.linear {
			cents = float64(p) * 100 / 16
		} else {
			per := modPeriod(st.note)
			if per > float64(p) {
				cents = 1200 * math.Log2(per/(per-float64(p)))
			}
		}
		if c.fx == 2 {
			cents = -cents
		}
		cmds = append(cmds, Cmd{CmdPitch, byte(int8(max(-127, min(127, math.Round(cents)))))})
	case 4, 6:
		if c.fx == 4 && p != 0 {
			st.vibrato = p
		}
		if st.vibrato != 0 {
			cmds = append(cmds, Cmd{CmdVibrato, st.vibrato})
		}
	case 8:
		cmds = append(cmds, Cmd{CmdOutput, panArg(int(p))})
	case 0xE:
		switch x {
		case 0x9:
			if y > 0 {
				cmds = append(cmds, Cmd{CmdRetrig, y})
			}
		case 0xC:
			if y == 0 {
				step.Note = NoteOff
			} else {
				cmds = append(cmds, Cmd{CmdKill, y})
			}
		case 0xD:
			if y > 0 {
				cmds = append(cmds, Cmd{CmdDelay, y})
			}
		}
	case 20: // XM Kxx: soltar la nota
		if step.Note == NoteEmpty {
			step.Note = NoteOff
		}
	}
	return cmds
}

// panArg es el argumento de O para un paneo 0..255.
func panArg(p int) byte {
	switch {
	case p < 0x50:
		return 1 // izquierda
	case p > 0xB0:
		return 2 // derecha
	}
	return 3 // los dos
}

// ImportMOD lee un módulo de ProTracker (M.K. y compatibles de 4, 6 u 8
// canales) como canción del tracker.
func ImportMOD(data []byte) (*Song, error) {
	if len(data) < 1084 {
		return nil, errors.New("not a MOD file: too short")
	}
	tag := string(data[1080:1084])
	ch := 0
	switch tag {
	case "M.K.", "M!K!", "FLT4", "4CHN":
		ch = 4
	case "6CHN":
		ch = 6
	case "8CHN", "FLT8", "CD81", "OKTA", "OCTA":
		ch = 8
	default:
		if strings.HasSuffix(tag, "CHN") && tag[0] >= '1' && tag[0] <= '9' {
			ch = int(tag[0] - '0')
		} else if strings.HasSuffix(tag, "CH") && tag[0] >= '1' && tag[0] <= '3' {
			ch = int(tag[0]-'0')*10 + int(tag[1]-'0')
		}
	}
	if ch == 0 {
		return nil, fmt.Errorf("not a MOD file (tag %q)", tag)
	}
	m := &modSong{name: cstr(data[0:20]), channels: ch, speed: 6, bpm: 125}
	m.defVol = make([]int, 31)
	type smpHead struct{ length, loopStart, loopLen, vol, finetune int }
	heads := make([]smpHead, 31)
	for i := range heads {
		h := data[20+30*i:]
		heads[i] = smpHead{
			length:    int(binary.BigEndian.Uint16(h[22:])) * 2,
			finetune:  int(int8(h[24]<<4) >> 4),
			vol:       min(64, int(h[25])),
			loopStart: int(binary.BigEndian.Uint16(h[26:])) * 2,
			loopLen:   int(binary.BigEndian.Uint16(h[28:])) * 2,
		}
		m.instrs = append(m.instrs, nil)
		m.samples = append(m.samples, nil)
		_ = cstr(h[0:22])
	}
	n := int(data[950])
	npat := 0
	for i := 0; i < min(n, 128); i++ {
		o := int(data[952+i])
		m.orders = append(m.orders, o)
		npat = max(npat, o+1)
	}
	// Todos los patrones del orden (los que pasan de 128 posiciones, también).
	for i := min(n, 128); i < 128; i++ {
		npat = max(npat, int(data[952+i])+1)
	}
	off := 1084
	size := 64 * ch * 4
	for p := 0; p < npat; p++ {
		if off+size > len(data) {
			return nil, errors.New("MOD file is truncated (patterns)")
		}
		pat := make(modPattern, 64)
		for r := 0; r < 64; r++ {
			pat[r] = make([]modCell, ch)
			for c := 0; c < ch; c++ {
				b := data[off+(r*ch+c)*4:]
				period := int(b[0]&0x0F)<<8 | int(b[1])
				cell := modCell{instr: int(b[0]&0xF0 | b[2]>>4), fx: b[2] & 0x0F, param: b[3], vol: -1, pan: -1, porta: -1}
				cell.note = periodNote(period)
				pat[r][c] = cell
			}
		}
		m.patterns = append(m.patterns, pat)
		off += size
	}
	// Los samples, uno tras otro después de los patrones.
	for i, h := range heads {
		if h.length <= 2 || off >= len(data) {
			off += h.length
			continue
		}
		end := min(len(data), off+h.length)
		d := &SampleData{Name: cstr(data[20+30*i : 20+30*i+22]), Bits: 8, Data: append([]byte(nil), data[off:end]...),
			Rate: int(math.Round(palClock / (2 * 428) * math.Pow(2, float64(h.finetune)/96)))}
		off += h.length
		if h.loopLen > 2 && h.loopStart+h.loopLen <= len(d.Data) {
			d.LoopStart, d.LoopLen = h.loopStart, h.loopLen
		}
		in := sampleInstrument(d.Name, d.LoopLen > 0)
		m.instrs[i], m.samples[i], m.defVol[i] = in, d, h.vol
	}
	// Los que no tienen sample no son instrumentos (sus notas no suenan).
	return m.toSong()
}

// sampleInstrument es un instrumento de sample a todo volumen (el de cada
// nota va en su paso) y con una envolvente que no cambia nada (un corte
// rápido al soltar).
func sampleInstrument(name string, loop bool) *Instrument {
	return &Instrument{Name: strings.ToLower(strings.TrimSpace(name)), Type: InstrSample, Volume: 15,
		Table: Empty, Transpose: true, Root: 49, Loop: loop, Env: Envelope{Start: 15, Sustain: 15, Release: 2}}
}

// cstr lee un texto de largo fijo (hasta el primer 0).
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimRight(strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return ' '
		}
		return r
	}, string(b)), " ")
}
