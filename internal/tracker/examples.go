package tracker

import "strings"

// Las canciones de ejemplo: para oír lo que da el tracker y para aprender
// mirándolas. Cada una enseña algo: Pocket Pop las cadenas que se repiten y
// los acordes con C; Crypt Crawl las tablas (arpegios y bombos); Night Drive
// el modo Libre, con ocho canales, synths, paneo y un eco hecho con D;
// Amen Chip los grooves, los redobles con R y los saltos con H. Se escriben
// con ayudas (phrase, drums, chain...) en vez de paso a paso.

// note lee "C-4", "F#2" u "OFF" (0 si no se entiende).
func note(s string) int8 {
	if s == "OFF" {
		return NoteOff
	}
	if len(s) != 3 {
		return NoteEmpty
	}
	i := strings.Index("C-C#D-D#E-F-F#G-G#A-A#B-", s[:2])
	if i < 0 || i%2 != 0 || s[2] < '0' || s[2] > '9' {
		return NoteEmpty
	}
	return int8(i/2 + int(s[2]-'0')*12 + 1)
}

// build ayuda a escribir una canción: da números a las frases y las cadenas
// según se crean.
type build struct {
	s      *Song
	ph, ch int
}

func newBuild(name string, mode Mode, bpm int) *build {
	s := NewSong(name, mode)
	s.BPM = bpm
	s.Rows = nil
	s.Instrs = map[int]*Instrument{}
	return &build{s: s}
}

// phr es una frase que se está escribiendo.
type phr struct {
	id int
	p  *Phrase
}

// phrase crea una frase con una línea de notas, una por paso ("C-2 --- G-2
// ..."; "---" nada, "OFF" corta), todas con el instrumento instr.
func (b *build) phrase(instr int, notes string) *phr {
	p := NewPhrase()
	for i, n := range strings.Fields(notes) {
		if n != "---" {
			p[i].Note = note(n)
			if p[i].Note > 0 {
				p[i].Instr = int16(instr)
			}
		}
	}
	id := b.ph
	b.ph++
	b.s.Phrases[id] = p
	return &phr{id, p}
}

// hit es un golpe de batería: su instrumento, su nota y su volumen.
type hit struct {
	instr int
	note  string
	vol   int8
}

// drums crea una frase de batería: un carácter por paso, según kit ('.'
// nada).
func (b *build) drums(kit map[rune]hit, steps string) *phr {
	p := b.phrase(0, "")
	for i, c := range steps {
		if h, ok := kit[c]; ok {
			p.p[i] = Step{Note: note(h.note), Instr: int16(h.instr), Vol: h.vol}
		}
	}
	return p
}

// cmd pone un comando en el paso step (en su primer hueco).
func (p *phr) cmd(step int, code, arg byte) *phr {
	for i := range p.p[step].Cmds {
		if p.p[step].Cmds[i].None() {
			p.p[step].Cmds[i] = Cmd{code, arg}
			break
		}
	}
	return p
}

// vol pone el volumen de unos pasos.
func (p *phr) vol(v int8, steps ...int) *phr {
	for _, s := range steps {
		p.p[s].Vol = v
	}
	return p
}

// chords pone acordes con C: en cada paso dado, el arpegio (0x47 mayor,
// 0x37 menor). Van de dos en dos: paso y acorde.
func (p *phr) chords(pairs ...int) *phr {
	for i := 0; i+1 < len(pairs); i += 2 {
		p.cmd(pairs[i], CmdChord, byte(pairs[i+1]))
	}
	return p
}

// chain crea una cadena con esas frases, sin transponer.
func (b *build) chain(ps ...*phr) int {
	steps := make([][2]int, len(ps))
	for i, p := range ps {
		steps[i] = [2]int{p.id, 0}
	}
	return b.chainT(steps...)
}

// chainT crea una cadena de frases con su transposición: {frase, semitonos}.
func (b *build) chainT(steps ...[2]int) int {
	c := NewChain()
	for i, st := range steps {
		c[i] = ChainStep{Phrase: int16(st[0]), Transpose: int8(st[1])}
	}
	id := b.ch
	b.ch++
	b.s.Chains[id] = c
	return id
}

// row añade una fila a la canción: la cadena de cada canal (-1 ninguna).
func (b *build) row(chains ...int) {
	r := EmptyRow()
	for i, c := range chains {
		r[i] = int16(c)
	}
	b.s.Rows = append(b.s.Rows, r)
}

// done termina la canción.
func (b *build) done() *Song {
	b.s.Fix()
	return b.s
}

// table crea la tabla id: una fila por tick, {volumen (-1 sin cambio),
// transposición}, y un salto H opcional en la última fila.
func (b *build) table(id int, hop int, rows ...[2]int) {
	t := NewTable()
	for i, r := range rows {
		t[i].Vol, t[i].Transpose = int8(r[0]), int8(r[1])
	}
	if hop >= 0 {
		t[len(rows)-1].Cmds[0] = Cmd{CmdHop, byte(hop)}
	}
	b.s.Tables[id] = t
}

const (
	maj = 0x47
	mnr = 0x37
	no  = -1
)

// Los instrumentos de Game Boy que usan los ejemplos.
func gbPulse(name string, duty, start, period int8, vib byte) *Instrument {
	return &Instrument{Name: name, Type: InstrPulse, Volume: 15, Table: Empty, Transpose: true, Duty: duty, Vibrato: vib,
		Env: Envelope{Start: start, Dir: -1, Period: period}}
}

func gbWave(name string, wave int16) *Instrument {
	return &Instrument{Name: name, Type: InstrWave, Volume: 15, Table: Empty, Transpose: true, Wave: wave, Env: Envelope{Start: 15}}
}

func gbNoise(name string, start, period int8, table int16) *Instrument {
	return &Instrument{Name: name, Type: InstrNoise, Volume: 15, Table: table, Env: Envelope{Start: start, Dir: -1, Period: period}}
}

// Examples son las canciones de ejemplo.
func Examples() []*Song {
	return []*Song{pocketPop(), cryptCrawl(), nightDrive(), amenChip()}
}

// pocketPop: pop alegre en Do, 120 BPM. Intro, estrofa y estribillo; cada
// parte es una cadena que se repite en la canción, y los acordes del pulso 1
// salen de C.
func pocketPop() *Song {
	b := newBuild("Pocket Pop", ModeGB, 120)
	s := b.s
	s.Instrs[0] = gbPulse("chords", 1, 9, 6, 0)
	s.Instrs[1] = gbPulse("lead", 2, 12, 0, 0x34)
	s.Instrs[2] = gbWave("bass", WaveTriangle)
	s.Instrs[3] = gbNoise("kick", 15, 1, 0)
	s.Instrs[4] = gbNoise("snare", 13, 2, Empty)
	s.Instrs[5] = gbNoise("hat", 6, 1, Empty)
	// El bombo: el ruido cae dos octavas en cuatro ticks.
	b.table(0, 3, [2]int{no, 0}, [2]int{no, -12}, [2]int{no, -24}, [2]int{no, -36})
	kit := map[rune]hit{'k': {3, "C-4", 15}, 's': {4, "D-5", 12}, 'h': {5, "C-7", no}, 'o': {5, "G-6", 9}}

	introB := b.phrase(2, "C-2 --- --- --- C-2 --- --- --- G-1 --- --- --- G-1 --- A#1 ---")
	introD := b.drums(kit, "k...s...k.k.s...")
	vB := b.phrase(2, "C-2 --- C-3 --- C-2 --- C-3 --- A-1 --- A-2 --- A-1 --- A-2 ---")
	v2B := b.phrase(2, "F-1 --- F-2 --- F-1 --- F-2 --- G-1 --- G-2 --- G-1 --- G-2 ---")
	vC := b.phrase(0, "C-4 --- --- --- --- --- --- --- A-3 --- --- --- --- --- --- ---").chords(0, maj, 8, mnr)
	v2C := b.phrase(0, "F-4 --- --- --- --- --- --- --- G-4 --- --- --- --- --- --- ---").chords(0, maj, 8, maj)
	vM := b.phrase(1, "--- --- --- --- E-5 --- G-5 --- --- --- E-5 --- D-5 --- C-5 ---").vol(7, 4, 6, 10, 12, 14).cmd(4, CmdWave, 1)
	v2M := b.phrase(1, "A-5 --- --- --- G-5 --- F-5 --- E-5 --- --- --- D-5 --- OFF ---").vol(7, 0, 4, 6, 8, 12)
	vD := b.drums(kit, "k.h.s.h.k.hks.h.")
	v2D := b.drums(kit, "k.h.s.h.k.hks.ho")
	cB := b.phrase(2, "F-2 F-2 F-3 F-2 G-2 G-2 G-3 G-2 E-2 E-2 E-3 E-2 A-1 A-1 A-2 A-1")
	cC := b.phrase(0, "F-4 --- --- --- G-4 --- --- --- E-4 --- --- --- A-3 --- --- ---").chords(0, maj, 4, maj, 8, mnr, 12, mnr)
	cM := b.phrase(1, "C-6 --- A-5 --- D-6 --- B-5 --- E-6 --- --- D-6 C-6 --- --- ---")
	cD := b.drums(kit, "kkh.s.hkk.h.s.ho")
	// El final del estribillo: un redoble de caja (R) que sube.
	cFill := b.drums(kit, "kkh.s.hkk.h.s.s.").cmd(14, CmdRetrig, 0x13)

	b.row(no, no, b.chain(introB), b.chain(introD))
	verse := [4]int{b.chain(vC, v2C), b.chain(vM, v2M), b.chain(vB, v2B), b.chain(vD, v2D)}
	chorus := [4]int{b.chain(cC, cC), b.chain(cM, cM), b.chain(cB, cB), b.chain(cD, cFill)}
	for _, r := range [][4]int{verse, verse, chorus, verse, chorus} {
		b.row(r[:]...)
	}
	return b.done()
}

// cryptCrawl: el tema de la mazmorra, en La menor, 140 BPM. Los arpegios
// del pulso 1 son una tabla (el instrumento la corre con cada nota), la
// melodía se arrastra con L y cae con P, y el bajo camina en la onda
// "tilted saw" de PICO-8.
func cryptCrawl() *Song {
	b := newBuild("Crypt Crawl", ModeGB, 140)
	s := b.s
	// Tabla 1: arpegio menor (0, 3, 7) en bucle; tabla 2: mayor.
	b.table(1, 0, [2]int{no, 0}, [2]int{no, 3}, [2]int{no, 7})
	b.table(2, 0, [2]int{no, 0}, [2]int{no, 4}, [2]int{no, 7})
	b.table(0, 3, [2]int{no, 0}, [2]int{no, -12}, [2]int{no, -24}, [2]int{no, -36})
	s.Instrs[0] = gbPulse("arp min", 0, 8, 0, 0)
	s.Instrs[0].Table = 1
	s.Instrs[1] = gbPulse("arp maj", 0, 8, 0, 0)
	s.Instrs[1].Table = 2
	s.Instrs[2] = gbPulse("lead", 2, 10, 0, 0x23)
	s.Instrs[3] = gbWave("bass", WaveTiltedSaw)
	s.Instrs[3].WaveVol = 1
	s.Instrs[4] = gbNoise("kick", 15, 1, 0)
	s.Instrs[5] = gbNoise("snare", 12, 2, Empty)
	s.Instrs[6] = gbNoise("hat", 5, 1, Empty)
	s.Instrs[6].Short = true // el registro de 7 bits: más metálico
	kit := map[rune]hit{'k': {4, "C-4", 15}, 's': {5, "D-5", 12}, 'h': {6, "C-7", no}}

	aA := b.phrase(0, "A-3 --- --- --- --- --- --- --- --- --- --- --- --- --- --- ---")
	aA.p[8] = Step{Note: note("F-3"), Instr: 1, Vol: Empty}
	bA := b.phrase(0, "A-3 --- --- --- --- --- --- --- --- --- --- --- --- --- --- ---")
	bA.p[8] = Step{Note: note("E-3"), Instr: 1, Vol: Empty}
	cA := b.phrase(0, "D-4 --- --- --- --- --- --- --- --- --- --- --- --- --- --- ---")
	cA.p[8] = Step{Note: note("E-4"), Instr: 1, Vol: Empty}
	aB := b.phrase(3, "A-1 --- A-1 C-2 --- A-1 E-2 --- A-1 --- G-1 --- F-1 --- E-1 ---")
	bB := b.phrase(3, "A-1 --- A-1 C-2 --- A-1 E-2 --- E-1 --- E-1 G#1 --- E-1 B-1 ---")
	cB := b.phrase(3, "D-2 --- D-2 F-2 --- D-2 A-2 --- E-2 --- E-2 G#2 --- E-2 B-2 ---")
	bM := b.phrase(2, "A-4 --- C-5 --- E-5 --- D-5 --- C-5 --- B-4 --- G#4 --- --- ---")
	bM.cmd(2, CmdSlide, 3).cmd(4, CmdSlide, 3) // las notas se arrastran
	cM := b.phrase(2, "F-5 --- E-5 --- D-5 --- A-5 --- G#5 --- --- --- B-5 --- OFF ---")
	cM.cmd(8, CmdPitch, 0xfa) // G#5 se cae
	aD := b.drums(kit, "k...h...k.k.h...")
	bD := b.drums(kit, "k...s...k.k.s..h")
	cD := b.drums(kit, "k.k.s.k.k.k.s.ss")

	intro := [4]int{b.chain(aA, aA), no, b.chain(aB, aB), b.chain(aD, aD)}
	verse := [4]int{b.chain(bA, bA), b.chain(bM, bM), b.chain(bB, bB), b.chain(bD, bD)}
	climb := [4]int{b.chain(cA, bA), b.chain(cM, bM), b.chain(cB, bB), b.chain(cD, bD)}
	end := [4]int{b.chain(cA, cA), b.chain(cM, cM), b.chain(cB, cB), b.chain(cD, cD)}
	for _, r := range [][4]int{intro, verse, climb, end} {
		b.row(r[:]...)
	}
	return b.done()
}

// nightDrive: synthwave lento en el modo Libre, en Mi menor, 100 BPM, con
// un poco de swing (groove 7/5). Ocho canales: bajo de sierra, arpegio
// cuadrado a la izquierda, pad de órgano, melodía con vibrato y slides,
// bombo, caja, charles y, a la derecha, la melodía otra vez, más baja y
// tarde (D): un eco.
func nightDrive() *Song {
	b := newBuild("Night Drive", ModeFree, 100)
	s := b.s
	s.Grooves[1] = Groove{7, 5}
	s.Groove = 1
	synth := func(name, shape string, a, d int, sus int8, r int, vol int8) *Instrument {
		return &Instrument{Name: name, Type: InstrSynth, Volume: vol, Table: Empty, Transpose: true, Shape: shape,
			Env: Envelope{Attack: a, Decay: d, Sustain: sus, Release: r}}
	}
	s.Instrs[0] = synth("bass", "saw", 0, 8, 6, 3, 13)
	s.Instrs[1] = synth("arp", "square", 0, 6, 4, 4, 9)
	s.Instrs[1].Pan, s.Instrs[1].Duty = -1, 1
	s.Instrs[2] = synth("pad", "organ", 12, 12, 10, 24, 8)
	s.Instrs[3] = synth("lead", "triangle", 1, 10, 11, 10, 14)
	s.Instrs[3].Vibrato = 0x33
	s.Instrs[4] = &Instrument{Name: "kick", Type: InstrSynth, Volume: 15, Table: 0, Shape: "sine", Env: Envelope{Decay: 10}}
	s.Instrs[5] = &Instrument{Name: "snare", Type: InstrNoise, Volume: 12, Table: Empty, Env: Envelope{Decay: 9}}
	s.Instrs[6] = &Instrument{Name: "hat", Type: InstrNoise, Volume: 6, Table: Empty, Env: Envelope{Decay: 3}}
	s.Instrs[7] = synth("echo", "triangle", 1, 10, 11, 10, 7)
	s.Instrs[7].Pan, s.Instrs[7].Vibrato = 1, 0x33
	// El bombo: un seno que cae rápido (la tabla), como los de las cajas de
	// ritmos.
	b.table(0, 5, [2]int{no, 12}, [2]int{no, 6}, [2]int{no, 2}, [2]int{no, -1}, [2]int{no, -3}, [2]int{no, -4})

	bass := b.phrase(0, "E-1 E-2 E-1 E-2 E-1 E-2 E-1 E-2 C-1 C-2 C-1 C-2 D-1 D-2 D-1 D-2")
	arp := b.phrase(1, "E-4 --- --- --- --- --- --- --- C-4 --- --- --- D-4 --- --- ---").chords(0, mnr, 8, maj, 12, maj)
	pad := b.phrase(2, "E-3 --- --- --- --- --- --- --- C-3 --- --- --- D-3 --- --- ---").chords(0, 0x7c, 8, 0x7c, 12, 0x7c)
	lead := b.phrase(3, "B-4 --- --- --- --- --- G-5 --- --- --- F#5 --- --- --- D-5 ---")
	lead.cmd(6, CmdSlide, 4).cmd(14, CmdPitch, 0xfc)
	echo := b.phrase(7, "B-4 --- --- --- --- --- G-5 --- --- --- F#5 --- --- --- D-5 ---")
	for _, st := range []int{0, 6, 10, 14} {
		echo.cmd(st, CmdDelay, 4) // menos que un paso (5 o 7 ticks): si no, no llega
	}
	kick := b.drums(map[rune]hit{'k': {4, "C-2", no}}, "k.......k.....k.")
	snare := b.drums(map[rune]hit{'s': {5, "D-5", no}}, "....s.......s...")
	hats := b.drums(map[rune]hit{'h': {6, "C-8", no}, 'o': {6, "G-7", 10}}, "h.h.h.h.h.h.h.ho")

	in := b.chain(bass, bass)
	b.row(in, b.chain(arp, arp), b.chain(pad, pad), no, b.chain(kick, kick), no, b.chain(hats, hats), no)
	full := []int{in, b.chain(arp, arp), b.chain(pad, pad), b.chain(lead, lead), b.chain(kick, kick), b.chain(snare, snare), b.chain(hats, hats), b.chain(echo, echo)}
	b.row(full...)
	b.row(full...)
	// La salida: el bajo y el pad se apagan (U).
	outB := b.phrase(0, "E-1 E-2 E-1 E-2 E-1 E-2 E-1 E-2 E-1 --- --- --- --- --- --- ---").cmd(0, CmdFadeOut, 0x80)
	outP := b.phrase(2, "E-3 --- --- --- --- --- --- --- --- --- --- --- --- --- --- OFF").chords(0, 0x7c).cmd(0, CmdFadeOut, 0x80)
	b.row(b.chain(outB), no, b.chain(outP), no, no, no, no, no)
	return b.done()
}

// amenChip: breakbeat rápido, 170 BPM. La batería partida tiene redobles
// (R), golpes que llegan tarde (D) y una frase que salta a la mitad de la
// siguiente (H); el bajo del estribillo es una sola frase que la cadena
// transpone; los pulsos cambian de ancho con W.
func amenChip() *Song {
	b := newBuild("Amen Chip", ModeGB, 170)
	s := b.s
	s.Instrs[0] = gbPulse("stab", 0, 10, 2, 0)
	s.Instrs[1] = gbPulse("lead", 1, 8, 0, 0x24)
	s.Instrs[2] = gbWave("bass", WaveSquare)
	s.Instrs[2].WaveVol = 1
	s.Instrs[3] = gbNoise("kick", 15, 1, 0)
	s.Instrs[4] = gbNoise("snare", 13, 1, Empty)
	s.Instrs[5] = gbNoise("ghost", 6, 1, Empty)
	b.table(0, 3, [2]int{no, 0}, [2]int{no, -12}, [2]int{no, -24}, [2]int{no, -36})
	kit := map[rune]hit{'k': {3, "C-4", 15}, 's': {4, "D-5", 13}, 'g': {5, "D-5", no}}

	break1 := b.drums(kit, "k.k.s.gsk.k.s.gs")
	break2 := b.drums(kit, "k.k.s.gs.ks.s.gs").cmd(9, CmdDelay, 2)
	// Medio break con un redoble: H en el paso 7 salta a la frase siguiente,
	// que empieza en su paso 8 (las dos mitades suman una frase).
	half := b.drums(kit, "k.k.s.ss........").cmd(6, CmdRetrig, 0x02).cmd(7, CmdHop, 8)
	roll := b.drums(kit, "kss.skskk.s.ssss").cmd(12, CmdRetrig, 0x13)

	bass := b.phrase(2, "C-2 --- C-3 --- --- C-2 --- D#2 --- --- F-2 --- G-2 --- --- ---")
	bassC := b.phrase(2, "F-2 F-3 F-2 F-3 F-2 F-3 F-2 F-3 F-2 F-3 F-2 F-3 F-2 F-3 F-2 F-3")
	stab := b.phrase(0, "C-4 --- --- --- --- --- --- --- G#3 --- --- --- --- --- --- ---").chords(0, mnr, 8, maj).cmd(0, CmdWave, 0)
	stabC := b.phrase(0, "F-4 --- --- --- G-4 --- --- --- G#4 --- --- --- A#4 --- --- ---").chords(0, mnr, 4, mnr, 8, maj, 12, maj)
	lead := b.phrase(1, "G-5 --- G-5 --- A#5 --- G-5 --- F-5 --- D#5 --- F-5 --- --- ---")
	lead.cmd(4, CmdWave, 2).cmd(8, CmdWave, 1)
	leadC := b.phrase(1, "C-6 --- --- A#5 --- G#5 --- G-5 --- --- F-5 --- G-5 --- --- ---").cmd(0, CmdWave, 2)

	intro := [4]int{no, no, b.chain(bass, bass, bass, bass), b.chain(break1, break1, break1, half, roll)}
	verse := [4]int{b.chain(stab, stab), b.chain(lead, lead), b.chain(bass, bass), b.chain(break1, break2)}
	chorus := [4]int{b.chain(stabC), b.chain(leadC), b.chainT([2]int{bassC.id, 0}), b.chain(roll)}
	for _, r := range [][4]int{intro, verse, verse, chorus, verse, chorus} {
		b.row(r[:]...)
	}
	return b.done()
}
