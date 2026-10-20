package tracker

import (
	"bytes"
	"testing"
)

// lsdjEncode comprime una memoria de canción como lo leería LSDJ, sin
// repeticiones: los bytes tal cual (0xC0 y 0xE0 duplicados), en bloques de
// 0x200 que saltan al siguiente, y 0xE0 0xFF al final. Si first > 0, el
// primer bloque es ese (en un .sav).
func lsdjEncode(mem []byte, first int) []byte {
	var out []byte
	block := []byte{}
	n := first
	flush := func(jump byte) {
		block = append(block, 0xE0, jump)
		out = append(out, block...)
		out = append(out, make([]byte, lsdjBlockSize-len(block))...)
		block = block[:0]
		n++
	}
	for _, b := range mem {
		piece := []byte{b}
		if b == 0xC0 || b == 0xE0 {
			piece = []byte{b, b}
		}
		if len(block)+len(piece)+2 > lsdjBlockSize {
			flush(byte(n + 2)) // el siguiente, contando desde 1
		}
		block = append(block, piece...)
	}
	flush(lsdjEndBlock)
	return out
}

// lsdjTestSong es una memoria de canción con lo mínimo: una fila con una
// cadena en PU1 y otra en NOI, una frase con notas, un comando y un
// instrumento de pulso con tabla, tempo 150 y la marca "rb".
func lsdjTestSong() []byte {
	mem := make([]byte, lsdjSongSize)
	copy(mem[lsdjRB1:], "rb")
	copy(mem[lsdjRB2:], "rb")
	copy(mem[lsdjRB3:], "rb")
	mem[lsdjFormatVersion] = 9
	mem[lsdjTempo] = 150
	for i := range 4 * lsdjSongRows {
		mem[lsdjSongChains+i] = lsdjEmpty
	}
	for i := range lsdjNumChains * Steps {
		mem[lsdjChainPhrases+i] = lsdjEmpty
	}
	for i := range lsdjNumPhrases * Steps {
		mem[lsdjPhraseInstrs+i] = lsdjEmpty
	}
	// Fila 0: cadena 3 en PU1, cadena 4 en NOI; fila 1: la 3 otra vez.
	mem[lsdjSongChains+0*4+PU1] = 3
	mem[lsdjSongChains+0*4+NOI] = 4
	mem[lsdjSongChains+1*4+PU1] = 3
	// Cadena 3: frase 7 y frase 7 transpuesta +12; cadena 4: frase 8.
	mem[lsdjChainPhrases+3*16+0] = 7
	mem[lsdjChainPhrases+3*16+1] = 7
	mem[lsdjChainTransposes+3*16+1] = 12
	mem[lsdjChainPhrases+4*16+0] = 8
	// Frase 7: C-3 de LSDJ (1) con el instrumento 2 y un C37 en el paso 0;
	// G-3 (8) en el paso 4.
	mem[lsdjPhraseNotes+7*16+0] = 1
	mem[lsdjPhraseInstrs+7*16+0] = 2
	mem[lsdjPhraseCmds+7*16+0] = 3 // C (formato ≥ 8: el código se corre uno)
	mem[lsdjPhraseCmdValues+7*16+0] = 0x37
	mem[lsdjPhraseNotes+7*16+4] = 8
	mem[lsdjPhraseNotes+8*16+0] = 40
	mem[lsdjPhraseInstrs+8*16+0] = 5
	// Instrumento 2: pulso, envolvente F bajando de a 3, ancho 25 %, tabla 6.
	in := mem[lsdjInstrParams+2*16:]
	in[0], in[1], in[6], in[7] = 0, 0xF3, 0x20|6, 1<<6|3
	copy(mem[lsdjInstrNames+2*5:], "LEAD")
	mem[lsdjInstrAlloc+2] = 1
	// Instrumento 5: ruido.
	mem[lsdjInstrParams+5*16] = 3
	mem[lsdjInstrParams+5*16+1] = 0xA1
	// Tabla 6: arpegio 0, +12 y un H00 en la fila 1.
	mem[lsdjTableTransposes+6*16+1] = 12
	mem[lsdjTableCmd1+6*16+1] = 8 // H (formato ≥ 8)
	// Groove 0: 7/5 (swing).
	mem[lsdjGrooves+0] = 7
	mem[lsdjGrooves+1] = 5
	// La onda 0, para ver el orden de los nibbles.
	mem[lsdjWaves+0] = 0x1F
	// Algunos 0xC0 y 0xE0 sueltos, que el codificador tiene que escapar.
	mem[0x5FF0] = 0xC0
	mem[0x5FF1] = 0xE0
	return mem
}

// Una canción .lsdsng se descomprime y pasa al tracker: filas, cadenas con
// su transposición, notas (C-3 de LSDJ es nuestra 25), instrumento, comando,
// tabla, groove, onda y tempo.
func TestImportLSDSNG(t *testing.T) {
	mem := lsdjTestSong()
	file := append([]byte("TESTSONG"), 0)
	file = append(file, lsdjEncode(mem, 0)...)
	s, err := ImportLSDSNG(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "TESTSONG" || s.Mode != ModeGB || s.BPM != 150 {
		t.Errorf("name %q mode %s bpm %d", s.Name, s.Mode, s.BPM)
	}
	if len(s.Rows) != 2 || s.Rows[0][PU1] != 3 || s.Rows[0][NOI] != 4 || s.Rows[0][PU2] != Empty || s.Rows[1][PU1] != 3 {
		t.Fatalf("rows %v", s.Rows)
	}
	c := s.Chains[3]
	if c == nil || c[0].Phrase != 7 || c[1].Phrase != 7 || c[1].Transpose != 12 || c[2].Phrase != Empty {
		t.Fatalf("chain 3 %+v", c)
	}
	p := s.Phrases[7]
	if p == nil {
		t.Fatal("phrase 7 missing")
	}
	if p[0].Note != 25 || p[0].Instr != 2 || p[0].Cmds[0] != (Cmd{CmdChord, 0x37}) {
		t.Errorf("step 0 %+v", p[0])
	}
	if p[4].Note != 32 || p[4].Instr != Empty || p[1].Note != NoteEmpty {
		t.Errorf("step 4 %+v, step 1 %+v", p[4], p[1])
	}
	in := s.Instrs[2]
	if in == nil || in.Type != InstrPulse || in.Name != "lead" || in.Env != (Envelope{Start: 15, Dir: -1, Period: 3}) ||
		in.Duty != 1 || in.Table != 6 || in.Pan != 0 || !in.Transpose {
		t.Errorf("instrument 2 %+v", in)
	}
	if n := s.Instrs[5]; n == nil || n.Type != InstrNoise || n.Env.Start != 10 {
		t.Errorf("instrument 5 %+v", n)
	}
	tb := s.Tables[6]
	if tb == nil || tb[1].Transpose != 12 || tb[1].Cmds[0] != (Cmd{CmdHop, 0}) {
		t.Errorf("table 6 %+v", tb)
	}
	if g := s.Grooves[0]; len(g) != 2 || g[0] != 7 || g[1] != 5 {
		t.Errorf("groove 0 %v", g)
	}
	if w := s.Waves[0]; w[0] != 1 || w[1] != 15 {
		t.Errorf("wave 0 starts %v %v", w[0], w[1])
	}
	// Y suena.
	out := Render(s, 22050, 10)
	peak := float32(0)
	for _, v := range out {
		peak = max(peak, v, -v)
	}
	if peak < 0.05 {
		t.Errorf("imported song is silent (peak %v)", peak)
	}
}

// La descompresión: repeticiones (0xC0 b n), un 0xC0 suelto (0xC0 0xC0) y
// las ondas e instrumentos por defecto (0xE0 0xF0 n y 0xE0 0xF1 n).
func TestLSDJDecompress(t *testing.T) {
	var want []byte
	var in []byte
	add := func(enc []byte, dec []byte) { in = append(in, enc...); want = append(want, dec...) }
	add([]byte{0xC0, 0x42, 5}, bytes.Repeat([]byte{0x42}, 5))
	add([]byte{0xC0, 0xC0}, []byte{0xC0})
	add([]byte{0xE0, 0xE0}, []byte{0xE0})
	add([]byte{0xE0, 0xF0, 2}, append(append([]byte{}, lsdjDefaultWave...), lsdjDefaultWave...))
	add([]byte{0xE0, 0xF1, 1}, lsdjDefaultInstrument)
	// El resto, en ceros con repeticiones de 255, hasta la medida justa.
	for rest := lsdjSongSize - len(want); rest > 0; {
		n := min(255, rest)
		add([]byte{0xC0, 0, byte(n)}, make([]byte, n))
		rest -= n
	}
	in = append(in, 0xE0, lsdjEndBlock)
	got, err := lsdjDecompress(in, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("decompressed bytes differ")
	}
	// Si el tamaño no da, es un error.
	if _, err := lsdjDecompress([]byte{0x01, 0xE0, lsdjEndBlock}, 0, false); err == nil {
		t.Error("accepted a short song")
	}
}

// Un .sav trae la canción abierta (marcada con *) y las guardadas, cada una
// desde su primer bloque.
func TestImportLSDJSav(t *testing.T) {
	sav := make([]byte, lsdjSongSize+lsdjBlockSize+lsdjBlockCount*lsdjBlockSize)
	mem := lsdjTestSong()
	copy(sav, mem) // la abierta
	h := sav[lsdjSongSize:]
	copy(h[0:], "FIRST")
	copy(h[3*lsdjNameLen:], "THIRD")
	const meta = lsdjProjects*lsdjNameLen + lsdjProjects
	h[meta+30], h[meta+31] = 'j', 'k'
	h[meta+32] = 3 // la abierta es la 3
	alloc := h[meta+33:]
	for i := range lsdjBlockCount {
		alloc[i] = 0xFF
	}
	blocks := sav[lsdjSongSize+lsdjBlockSize:]
	// La canción 0 desde el bloque 0, la 3 desde donde termine.
	enc0 := lsdjEncode(mem, 0)
	copy(blocks, enc0)
	n0 := len(enc0) / lsdjBlockSize
	for i := range n0 {
		alloc[i] = 0
	}
	mem3 := lsdjTestSong()
	mem3[lsdjTempo] = 30 // 30 + 256 = 286 BPM
	enc3 := lsdjEncode(mem3, n0)
	copy(blocks[n0*lsdjBlockSize:], enc3)
	for i := range len(enc3) / lsdjBlockSize {
		alloc[n0+i] = 3
	}
	songs, err := ImportLSDJSav(sav)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 3 {
		t.Fatalf("%d songs, want 3", len(songs))
	}
	if songs[0].Name != "THIRD *" || songs[1].Name != "FIRST" || songs[2].Name != "THIRD" {
		t.Errorf("names %q %q %q", songs[0].Name, songs[1].Name, songs[2].Name)
	}
	if songs[2].BPM != MaxBPM && songs[2].BPM != 286 {
		t.Errorf("song 3 bpm %d, want 286 (or clamped to %d)", songs[2].BPM, MaxBPM)
	}
	// Un archivo que no es de LSDJ.
	if _, err := ImportLSDJSav(make([]byte, len(sav))); err == nil {
		t.Error("accepted a blank .sav")
	}
}
