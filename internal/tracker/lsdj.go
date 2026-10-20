package tracker

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Importar canciones de LSDJ (Little Sound Dj, el tracker de la Game Boy):
// un .lsdsng (una canción suelta) o un .sav (la memoria del cartucho, con
// la canción abierta y hasta 32 guardadas). El tracker copia a propósito la
// estructura de LSDJ (canción → cadenas → frases, instrumentos, tablas,
// grooves, los mismos comandos y los mismos 24 ticks por tiempo), así que
// casi todo pasa tal cual.
//
// El formato sigue a liblsdj (github.com/stijnfrishert/liblsdj, MIT, de
// Stijn Frishert): la compresión de bloques (compression.c), la cabecera
// del .sav (sav.c), la memoria de la canción (song_offsets.h) y los bytes
// de cada instrumento (instrument*.c).

// Medidas del formato de LSDJ.
const (
	lsdjSongSize   = 0x8000 // la memoria de una canción, descomprimida
	lsdjBlockSize  = 0x200  // un bloque comprimido
	lsdjBlockCount = 191    // bloques de un .sav
	lsdjProjects   = 32     // canciones guardadas en un .sav
	lsdjNameLen    = 8
	lsdjEndBlock   = 0xFF // "fin" en un salto de bloque
)

// Dónde está cada cosa en la memoria de una canción (song_offsets.h).
const (
	lsdjPhraseNotes      = 0x0000 // 255 frases × 16 notas
	lsdjGrooves          = 0x1090 // 32 grooves × 16 pasos
	lsdjSongChains       = 0x1290 // 256 filas × 4 canales
	lsdjTableEnvelopes   = 0x1690 // 32 tablas × 16 volúmenes
	lsdjInstrNames       = 0x1E7A // 64 × 5 letras
	lsdjRB1              = 0x1E78 // "rb": la memoria está inicializada
	lsdjTableAlloc       = 0x2020 // 32 bytes (1 = en uso)
	lsdjInstrAlloc       = 0x2040 // 64 bytes (1 = en uso)
	lsdjChainPhrases     = 0x2080 // 128 cadenas × 16 frases
	lsdjChainTransposes  = 0x2880 // 128 cadenas × 16 transposiciones
	lsdjInstrParams      = 0x3080 // 64 instrumentos × 16 bytes
	lsdjTableTransposes  = 0x3480
	lsdjTableCmd1        = 0x3680
	lsdjTableCmd1Value   = 0x3880
	lsdjTableCmd2        = 0x3A80
	lsdjTableCmd2Value   = 0x3C80
	lsdjRB2              = 0x3E80
	lsdjPhraseAlloc      = 0x3E82 // bits
	lsdjChainAlloc       = 0x3EA2 // bits
	lsdjTempo            = 0x3FB4
	lsdjPhraseCmds       = 0x4000
	lsdjPhraseCmdValues  = 0x4FF0
	lsdjWaves            = 0x6000 // 256 ondas × 16 bytes (32 muestras de 4 bits)
	lsdjPhraseInstrs     = 0x7000
	lsdjRB3              = 0x7FF0
	lsdjFormatVersion    = 0x7FFF
	lsdjNumPhrases       = 255
	lsdjNumChains        = 128
	lsdjNumInstrs        = 64
	lsdjNumTables        = 32
	lsdjNumGrooves       = 32
	lsdjNumWaves         = 256
	lsdjSongRows         = 256
	lsdjEmpty            = 0xFF // cadena, frase o instrumento vacío
	lsdjMinFormatVersion = 4    // las de antes cambian la forma del vibrato y más
)

// lsdjDefaultWave y lsdjDefaultInstrument son lo que estampan los bytes
// especiales de la compresión (0xE0 0xF0 n y 0xE0 0xF1 n), de defaults.h de
// liblsdj.
var (
	lsdjDefaultWave = []byte{
		0x8E, 0xCD, 0xCC, 0xBB, 0xAA, 0xA9, 0x99, 0x88, 0x87, 0x76, 0x66, 0x55, 0x54, 0x43, 0x32, 0x31,
	}
	lsdjDefaultInstrument = []byte{
		0xA8, 0, 0, 0xFF, 0, 0, 3, 0, 0, 0xD0, 0, 0, 0, 0xF3, 0, 0,
	}
)

// lsdjDecompress descomprime una canción que empieza en el bloque first de
// blocks (los bloques de 0x200 bytes uno tras otro): copia los bytes tal
// cual salvo 0xC0 (repetición: 0xC0 b n = b n veces; 0xC0 0xC0 = un 0xC0) y
// 0xE0 (especial: 0xE0 0xE0 = un 0xE0; 0xE0 0xF0 n = n veces la onda por
// defecto; 0xE0 0xF1 n = n veces el instrumento por defecto; 0xE0 0xFF =
// fin; 0xE0 k = sigue en el bloque k, contando desde 1). En un .sav se
// sigue el salto (follow); en un .lsdsng los bloques ya van seguidos y el
// número es el que tenían en el .sav de donde salió: se pasa al siguiente.
func lsdjDecompress(blocks []byte, first int, follow bool) ([]byte, error) {
	out := make([]byte, 0, lsdjSongSize)
	pos := first * lsdjBlockSize
	blockStart := pos
	read := func() (byte, error) {
		if pos >= len(blocks) {
			return 0, errors.New("lsdj: compressed data ends early")
		}
		b := blocks[pos]
		pos++
		return b, nil
	}
	for jumps := 0; ; {
		b, err := read()
		if err != nil {
			return nil, err
		}
		switch b {
		case 0xC0:
			v, err := read()
			if err != nil {
				return nil, err
			}
			if v == 0xC0 {
				out = append(out, v)
				break
			}
			n, err := read()
			if err != nil {
				return nil, err
			}
			out = append(out, bytes.Repeat([]byte{v}, int(n))...)
		case 0xE0:
			v, err := read()
			if err != nil {
				return nil, err
			}
			switch v {
			case 0xE0:
				out = append(out, v)
			case 0xF0, 0xF1:
				n, err := read()
				if err != nil {
					return nil, err
				}
				pat := lsdjDefaultWave
				if v == 0xF1 {
					pat = lsdjDefaultInstrument
				}
				out = append(out, bytes.Repeat(pat, int(n))...)
			case lsdjEndBlock:
				if len(out) != lsdjSongSize {
					return nil, fmt.Errorf("lsdj: song is %d bytes, want %d", len(out), lsdjSongSize)
				}
				return out, nil
			default:
				if jumps++; jumps > lsdjBlockCount {
					return nil, errors.New("lsdj: block jumps loop")
				}
				if follow {
					pos = (int(v) - 1) * lsdjBlockSize
				} else {
					pos = blockStart + lsdjBlockSize
				}
				blockStart = pos
			}
		default:
			out = append(out, b)
		}
		if len(out) > lsdjSongSize {
			return nil, errors.New("lsdj: song too large")
		}
	}
}

// ImportLSDSNG lee una canción de LSDJ en .lsdsng: 8 letras de nombre, la
// versión y los bloques comprimidos (el primero justo detrás).
func ImportLSDSNG(data []byte) (*Song, error) {
	if len(data) < lsdjNameLen+1+2 {
		return nil, errors.New("lsdj: file too short")
	}
	mem, err := lsdjDecompress(data[lsdjNameLen+1:], 0, false)
	if err != nil {
		return nil, err
	}
	return lsdjSong(mem, lsdjName(data[:lsdjNameLen]))
}

// ImportLSDJSav lee un .sav de LSDJ (los 128 KB de memoria del cartucho):
// devuelve cada canción guardada y, delante, la abierta (la que había en
// la memoria de trabajo), si está y no es una de las guardadas tal cual.
func ImportLSDJSav(data []byte) ([]*Song, error) {
	// La memoria de trabajo (0x0000), la cabecera (0x8000: nombres,
	// versiones, 'jk' en 0x813E, el proyecto abierto y la tabla de bloques)
	// y los bloques (0x8200, el primero es el 1).
	const header, blocksAt = lsdjSongSize, lsdjSongSize + lsdjBlockSize
	if len(data) < blocksAt {
		return nil, errors.New("lsdj: not an LSDJ .sav (too short)")
	}
	h := data[header:blocksAt]
	const namesLen = lsdjProjects * lsdjNameLen
	if h[namesLen+lsdjProjects+30] != 'j' || h[namesLen+lsdjProjects+31] != 'k' {
		return nil, errors.New("lsdj: not an LSDJ .sav (no 'jk' mark)")
	}
	active := int(h[namesLen+lsdjProjects+32])
	alloc := h[namesLen+lsdjProjects+33:]
	blocks := data[blocksAt:]
	var out []*Song
	seen := map[int]bool{}
	var activeName string
	var activeMem []byte
	for i := 0; i < lsdjBlockCount && i < len(alloc); i++ {
		p := int(alloc[i])
		if p == 0xFF || p >= lsdjProjects || seen[p] {
			continue
		}
		seen[p] = true
		name := lsdjName(h[p*lsdjNameLen : (p+1)*lsdjNameLen])
		if p == active {
			activeName = name
		}
		mem, err := lsdjDecompress(blocks, i, true)
		if err != nil {
			return nil, fmt.Errorf("song %d (%s): %w", p, name, err)
		}
		if p == active {
			activeMem = mem
		}
		s, err := lsdjSong(mem, name)
		if err != nil {
			return nil, fmt.Errorf("song %d (%s): %w", p, name, err)
		}
		out = append(out, s)
	}
	// La abierta: con sus cambios sin guardar, delante (con un * en el
	// nombre, como LSDJ marca lo no guardado). No si está vacía o es igual
	// a la guardada.
	if activeName == "" {
		activeName = "LSDJ"
	}
	working := data[:lsdjSongSize]
	if s, err := lsdjSong(working, activeName+" *"); err == nil && len(s.Chains) > 0 && !bytes.Equal(working, activeMem) {
		out = append([]*Song{s}, out...)
	}
	if len(out) == 0 {
		return nil, errors.New("lsdj: no songs in the .sav")
	}
	return out, nil
}

// lsdjName es un nombre de LSDJ (hasta 8 letras, rellenas con 0).
func lsdjName(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// lsdjNote pasa una nota de LSDJ a la nuestra: su 1 es C-3 en la pantalla
// de LSDJ, la nota más grave de la Game Boy (unos 65 Hz), que es C-2 en la
// numeración habitual y nuestra 25 (1 = C-0, 16 Hz).
func lsdjNote(n byte) int8 {
	if n == 0 {
		return NoteEmpty
	}
	if v := int(n) + 24; v <= NoteMax {
		return int8(v)
	}
	return NoteEmpty
}

// lsdjCommand pasa un comando de LSDJ (su código en la memoria) a uno
// nuestro. Desde el formato 8 los códigos se corrieron uno para meter B (1).
// La mayoría tienen la misma letra y el mismo argumento; los que no:
//   - H: en LSDJ Hxy, en una tabla, salta y repetidas x veces; aquí se
//     salta siempre al paso y (en una frase, la siguiente empieza en y).
//   - M: en LSDJ el volumen de cada lado (0-7 y 0-7); aquí uno de 0-F.
//   - F y P: LSDJ los cuenta en sus pasos de afinación; aquí en centésimas de
//     semitono: se pasan tal cual (dan un efecto parecido, no igual).
//   - B, y los de Arduinoboy (N, X, Q, Y), no tienen equivalente: se pierden.
func lsdjCommand(code, arg byte, version byte) Cmd {
	c := int(code)
	if version >= 8 {
		switch {
		case c == 1:
			return Cmd{} // B (desde LSDJ 7.1)
		case c > 1:
			c--
		}
	}
	letters := []byte{0, CmdTable, CmdChord, CmdDelay, CmdEnvelope, CmdFine, CmdGroove, CmdHop, CmdKill,
		CmdSlide, CmdMaster, CmdOutput, CmdPitch, CmdRetrig, CmdSweep, CmdTempo, CmdVibrato, CmdWave, CmdRandom}
	if c <= 0 || c >= len(letters) {
		return Cmd{}
	}
	cmd := Cmd{Code: letters[c], Arg: arg}
	switch cmd.Code {
	case CmdHop:
		cmd.Arg = arg & 0x0F
	case CmdMaster:
		l, r := int(arg>>4&7), int(arg&7)
		cmd.Arg = byte(max(l, r) * 15 / 7)
	case CmdOutput:
		// En LSDJ 1 izquierda, 2 derecha, 3 los dos, 0 nada: aquí el orden
		// es 0 nada, 1 izquierda, 2 derecha, 3 los dos (el mismo).
		cmd.Arg = arg & 3
	case CmdTable:
		cmd.Arg = arg & 0x1F
	}
	return cmd
}

// lsdjSong arma una canción a partir de la memoria descomprimida de LSDJ.
func lsdjSong(mem []byte, name string) (*Song, error) {
	if len(mem) != lsdjSongSize {
		return nil, fmt.Errorf("lsdj: song memory is %d bytes", len(mem))
	}
	rb := func(at int) bool { return mem[at] == 'r' && mem[at+1] == 'b' }
	if !rb(lsdjRB1) && !rb(lsdjRB2) && !rb(lsdjRB3) {
		return nil, errors.New("lsdj: not an LSDJ song (no 'rb' marks)")
	}
	ver := mem[lsdjFormatVersion]
	if ver < lsdjMinFormatVersion {
		return nil, fmt.Errorf("lsdj: song format version %d is too old (need %d or newer)", ver, lsdjMinFormatVersion)
	}
	if name == "" {
		name = "LSDJ"
	}
	s := NewSong(name, ModeGB)
	s.Instrs = map[int]*Instrument{}
	s.Grooves = map[int]Groove{}
	s.Waves = map[int]*Wave{}
	// El tempo: los valores bajos son de 256 en adelante.
	bpm := int(mem[lsdjTempo])
	if bpm < 40 {
		bpm += 256
	}
	s.BPM = bpm

	// Las filas de la canción: hasta la última con algo.
	s.Rows = nil
	last := -1
	for r := 0; r < lsdjSongRows; r++ {
		for ch := 0; ch < GBChannels; ch++ {
			if mem[lsdjSongChains+r*GBChannels+ch] != lsdjEmpty {
				last = r
			}
		}
	}
	usedChains := map[int]bool{}
	for r := 0; r <= last; r++ {
		row := EmptyRow()
		for ch := 0; ch < GBChannels; ch++ {
			if c := mem[lsdjSongChains+r*GBChannels+ch]; c != lsdjEmpty && int(c) < lsdjNumChains {
				row[ch] = int16(c)
				usedChains[int(c)] = true
			}
		}
		s.Rows = append(s.Rows, row)
	}

	// Las cadenas: las usadas en la canción y las que LSDJ tiene en uso.
	usedPhrases := map[int]bool{}
	for c := 0; c < lsdjNumChains; c++ {
		allocated := mem[lsdjChainAlloc+c/8]&(1<<(c%8)) != 0
		if !allocated && !usedChains[c] {
			continue
		}
		chain := NewChain()
		filled := false
		for st := 0; st < Steps; st++ {
			p := mem[lsdjChainPhrases+c*Steps+st]
			if p == lsdjEmpty || int(p) >= lsdjNumPhrases {
				continue
			}
			chain[st] = ChainStep{Phrase: int16(p), Transpose: int8(mem[lsdjChainTransposes+c*Steps+st])}
			usedPhrases[int(p)] = true
			filled = true
		}
		if filled || usedChains[c] {
			s.Chains[c] = chain
		}
	}

	// Las frases.
	usedInstrs := map[int]bool{}
	usedTables := map[int]bool{}
	for p := 0; p < lsdjNumPhrases; p++ {
		allocated := mem[lsdjPhraseAlloc+p/8]&(1<<(p%8)) != 0
		if !allocated && !usedPhrases[p] {
			continue
		}
		ph := NewPhrase()
		for st := 0; st < Steps; st++ {
			i := p*Steps + st
			step := &ph[st]
			step.Note = lsdjNote(mem[lsdjPhraseNotes+i])
			if in := mem[lsdjPhraseInstrs+i]; in != lsdjEmpty && int(in) < lsdjNumInstrs {
				step.Instr = int16(in)
				usedInstrs[int(in)] = true
			}
			if cmd := lsdjCommand(mem[lsdjPhraseCmds+i], mem[lsdjPhraseCmdValues+i], ver); !cmd.None() {
				step.Cmds[0] = cmd
				if cmd.Code == CmdTable {
					usedTables[int(cmd.Arg)] = true
				}
			}
		}
		s.Phrases[p] = ph
	}

	// Los instrumentos.
	for in := 0; in < lsdjNumInstrs; in++ {
		if mem[lsdjInstrAlloc+in] == 0 && !usedInstrs[in] {
			continue
		}
		inst := lsdjInstrument(mem, in, ver, s.BPM)
		if inst.Table != Empty {
			usedTables[int(inst.Table)] = true
		}
		s.Instrs[in] = inst
	}

	// Las tablas: una fila por tick, con su volumen, su transposición y dos
	// comandos.
	for t := 0; t < lsdjNumTables; t++ {
		if mem[lsdjTableAlloc+t] == 0 && !usedTables[t] {
			continue
		}
		tb := NewTable()
		for st := 0; st < Steps; st++ {
			i := t*Steps + st
			// El volumen de la tabla es una envolvente (volumen inicial en
			// el nibble alto); 0 es "sin cambio".
			if env := mem[lsdjTableEnvelopes+i]; env != 0 {
				tb[st].Vol = int8(env >> 4)
			}
			tb[st].Transpose = int8(mem[lsdjTableTransposes+i])
			tb[st].Cmds[0] = lsdjCommand(mem[lsdjTableCmd1+i], mem[lsdjTableCmd1Value+i], ver)
			tb[st].Cmds[1] = lsdjCommand(mem[lsdjTableCmd2+i], mem[lsdjTableCmd2Value+i], ver)
		}
		s.Tables[t] = tb
	}

	// Los grooves: ticks por paso, hasta el primer 0.
	for g := 0; g < lsdjNumGrooves; g++ {
		var gr Groove
		for st := 0; st < Steps; st++ {
			v := mem[lsdjGrooves+g*Steps+st]
			if v == 0 {
				break
			}
			gr = append(gr, int(v))
		}
		if len(gr) > 0 {
			s.Grooves[g] = gr
		}
	}
	if len(s.Grooves[0]) == 0 {
		s.Grooves[0] = Groove{6, 6}
	}

	// Las ondas: 16 bytes, dos muestras de 4 bits en cada uno (la alta
	// primero), como la wave RAM.
	for w := 0; w < lsdjNumWaves; w++ {
		var wv Wave
		for k := 0; k < WaveLen/2; k++ {
			b := mem[lsdjWaves+w*16+k]
			wv[2*k], wv[2*k+1] = b>>4, b&0x0F
		}
		s.Waves[w] = &wv
	}

	s.Fix()
	return s, nil
}

// lsdjInstrument lee un instrumento de LSDJ (16 bytes; ver instrument*.c de
// liblsdj).
func lsdjInstrument(mem []byte, n int, ver byte, bpm int) *Instrument {
	b := mem[lsdjInstrParams+n*16 : lsdjInstrParams+(n+1)*16]
	name := lsdjName(mem[lsdjInstrNames+n*5 : lsdjInstrNames+(n+1)*5])
	in := &Instrument{Name: strings.ToLower(name), Volume: 15, Table: Empty, Transpose: b[5]&0x20 == 0}
	if b[6]&0x20 != 0 { // la tabla, si está activada
		in.Table = int16(b[6] & 0x1F)
	}
	switch b[7] & 3 { // el paneo: 1 izquierda, 2 derecha, 3 los dos, 0 nada
	case 1:
		in.Pan = -1
	case 2:
		in.Pan = 1
	}
	// La envolvente de los pulsos y el ruido: hasta LSDJ 8.1 (formato 11) es
	// el registro NRx2 de la Game Boy (volumen inicial, sentido y paso);
	// desde ahí es un ADSR, que se aproxima con su nivel inicial y, si luego
	// baja, una caída al ritmo de su decay.
	env := func() Envelope {
		if ver < 11 {
			e := Envelope{Start: int8(b[1] >> 4), Period: int8(b[1] & 7)}
			if e.Period > 0 {
				e.Dir = -1
				if b[1]&8 != 0 {
					e.Dir = 1
				}
			}
			return e
		}
		start, attack := int8(b[1]>>4), int8(b[9]>>4)
		sustain, decay := int8(b[10]>>4), int8(b[9]&7)
		e := Envelope{Start: max(start, attack)}
		if sustain < e.Start && decay > 0 {
			e.Dir, e.Period = -1, decay
		}
		return e
	}
	// La duración (NRx1): con el bit 6, (64-t)/256 segundos; aquí en ticks.
	length := func() int {
		if b[3]&0x40 == 0 {
			return 0
		}
		secs := float64(64-int(b[3]&0x3F)) / 256
		return max(1, int(math.Round(secs*float64(bpm)*TicksPerBeat/60)))
	}
	switch b[0] {
	case 0: // pulso
		in.Type = InstrPulse
		in.Env = env()
		in.Length = length()
		in.Sweep = b[4]
		in.Duty = int8(b[7] >> 6)
	case 1: // onda: la primera onda de su synth (los 16 de cada synth están en orden)
		in.Type = InstrWave
		synth := b[2] >> 4
		if ver >= 16 {
			synth = b[3] >> 4
		}
		in.Wave = int16(synth) * 16
		in.Env = Envelope{Start: 15}
		// El volumen de la onda: 0xA8 lleno, 0x40 la mitad, 0x60 un cuarto,
		// 0 nada.
		switch b[1] {
		case 0x40:
			in.WaveVol = 1
		case 0x60:
			in.WaveVol = 2
		case 0x00:
			in.Volume = 0
		}
	case 3: // ruido
		in.Type = InstrNoise
		in.Env = env()
		in.Length = length()
	default:
		// Kit (2): las baterías sampleadas de LSDJ no se pueden traer; se
		// aproximan con un golpe de ruido corto.
		in.Type = InstrNoise
		in.Env = Envelope{Start: 15, Dir: -1, Period: 2}
		if in.Name == "" {
			in.Name = "kit"
		}
	}
	if in.Name == "" {
		in.Name = string(in.Type)
	}
	return in
}
