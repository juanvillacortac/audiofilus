package tracker

import (
	"encoding/binary"
	"math"
	"testing"
)

// testSine es un ciclo de seno de n muestras de 8 bits.
func testSine(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(int8(math.Round(100 * math.Sin(2*math.Pi*float64(i)/float64(n)))))
	}
	return b
}

// buildMOD arma un MOD de 4 canales en memoria (datos sintéticos): un
// sample (un seno en bucle) y un patrón que se toca dos veces.
func buildMOD() []byte {
	b := make([]byte, 1084+64*4*4)
	copy(b, "test mod")
	h := b[20:]
	copy(h, "sine")
	binary.BigEndian.PutUint16(h[22:], 32) // 64 bytes
	h[25] = 64
	binary.BigEndian.PutUint16(h[26:], 0)
	binary.BigEndian.PutUint16(h[28:], 32)
	b[950] = 2
	b[951] = 127
	b[952], b[953] = 0, 0
	copy(b[1080:], "M.K.")
	cell := func(row, ch, period, instr int, fx, param byte) {
		o := 1084 + (row*4+ch)*4
		b[o] = byte(instr&0xF0) | byte(period>>8)
		b[o+1] = byte(period)
		b[o+2] = byte(instr&0x0F)<<4 | fx
		b[o+3] = param
	}
	cell(0, 0, 428, 1, 0xC, 32)   // C-4, volumen a la mitad
	cell(1, 1, 381, 1, 0x0, 0x47) // D-4 con arpegio
	cell(4, 2, 0, 0, 0xF, 3)      // speed 3
	cell(8, 0, 340, 0, 0x3, 5)    // E-4 deslizándose
	cell(10, 2, 428, 1, 0xE, 0xC2)
	cell(20, 3, 0, 0, 0xD, 0) // el patrón acaba en la fila 20
	b = append(b, testSine(64)...)
	return b
}

// Un MOD se importa: filas por el orden, cadenas de frases de 16 pasos, las
// notas, el volumen, los efectos y el sample.
func TestImportMOD(t *testing.T) {
	s, err := ImportMOD(buildMOD())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeFree || s.BPM != 125 || s.Name != "test mod" {
		t.Fatalf("mode %s bpm %d name %q", s.Mode, s.BPM, s.Name)
	}
	if len(s.Rows) != 2 || s.Rows[0] != s.Rows[1] {
		t.Fatalf("rows %v", s.Rows)
	}
	c0 := s.Chains[int(s.Rows[0][0])]
	if c0[1].Phrase == Empty || c0[2].Phrase != Empty {
		t.Fatalf("chain 0: %v (21 rows should be 2 phrases)", c0)
	}
	p0, p1 := s.Phrases[int(c0[0].Phrase)], s.Phrases[int(c0[1].Phrase)]
	if p0[0].Note != 49 || p0[0].Instr != 0 || p0[0].Vol != 8 {
		t.Errorf("C-4 step: %+v", p0[0])
	}
	if p0[8].Note != 53 || p0[8].Cmds[0].Code != CmdSlide {
		t.Errorf("tone porta step: %+v", p0[8])
	}
	if cmdOf(p1[4], CmdHop) == nil {
		t.Errorf("pattern break at row 20 has no hop: %+v", p1[4])
	}
	p1ch1 := s.Phrases[int(s.Chains[int(s.Rows[0][1])][0].Phrase)]
	if p1ch1[1].Note != 51 || cmdOf(p1ch1[1], CmdChord) == nil || cmdOf(p1ch1[1], CmdChord).Arg != 0x47 {
		t.Errorf("arpeggio step: %+v", p1ch1[1])
	}
	for ch := 0; ch < 4; ch++ {
		p := s.Phrases[int(s.Chains[int(s.Rows[0][ch])][0].Phrase)]
		if g := cmdOf(p[3], CmdGroove); g == nil || len(s.Grooves[int(g.Arg)]) != 1 || s.Grooves[int(g.Arg)][0] != 3 {
			t.Errorf("channel %d: speed 3 (row 4) not a groove on row 3: %+v", ch, p[3])
		}
	}
	p2 := s.Phrases[int(s.Chains[int(s.Rows[0][2])][0].Phrase)]
	if cmdOf(p2[10], CmdKill) == nil {
		t.Errorf("EC2 not a kill: %+v", p2[10])
	}
	d := s.Samples[0]
	if d == nil || d.Len() != 64 || d.LoopLen != 64 || s.Instrs[0].Type != InstrSample || !s.Instrs[0].Loop {
		t.Fatalf("sample %+v instr %+v", d, s.Instrs[0])
	}
	// Y suena.
	out := Render(s, 48000, 4)
	peak := 0.0
	for _, v := range out {
		peak = math.Max(peak, math.Abs(float64(v)))
	}
	if peak < 0.05 {
		t.Errorf("imported MOD is silent (peak %v)", peak)
	}
}

// Un sample suena a su Rate en su nota raíz, y una octava arriba al doble.
func TestSamplePitch(t *testing.T) {
	s := NewSong("s", ModeFree)
	s.Samples = map[int]*SampleData{}
	s.Samples[0] = &SampleData{Rate: 48000, Bits: 8, Data: testSine(100), LoopLen: 100}
	s.Instrs[0] = &Instrument{Type: InstrSample, Volume: 15, Table: Empty, Root: 49, Loop: true, Sample: 0, Env: Envelope{Sustain: 15}}
	for _, c := range []struct {
		note int8
		hz   float64
	}{{49, 480}, {61, 960}} {
		p := NewPlayer(48000)
		p.SetSong(s)
		p.Preview(0, c.note, 0)
		run(p, 960) // que se asiente (Preview corta a los 0,25 s)
		b := make([]byte, 8*9600)
		p.Read(b)
		cross, prev := 0, 0.0
		for i := 0; i+8 <= len(b); i += 8 {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i:])))
			if prev <= 0 && v > 0 {
				cross++
			}
			prev = v
		}
		if hz := float64(cross) * 5; math.Abs(hz-c.hz) > c.hz*0.03 {
			t.Errorf("note %d: %v Hz, want %v", c.note, hz, c.hz)
		}
	}
}

// notesOf son las notas de una canción aplanada, por canal, en orden.
func notesOf(s *Song, n int) [][]int8 {
	f := flatten(s, true)
	out := make([][]int8, n)
	for _, rows := range f.rows {
		for _, r := range rows {
			for ch := 0; ch < n; ch++ {
				if ch < len(r.cells) && r.cells[ch].note != 0 {
					out[ch] = append(out[ch], r.cells[ch].note)
				}
			}
		}
	}
	return out
}

// Los ejemplos pasan a XM y vuelven con las mismas notas, en los mismos
// canales; y el módulo suena.
func TestXMRoundTrip(t *testing.T) {
	for _, ex := range Examples() {
		data, err := ExportXM(ex)
		if err != nil {
			t.Fatalf("%s: %v", ex.Name, err)
		}
		back, err := ImportXM(data)
		if err != nil {
			t.Fatalf("%s: import: %v", ex.Name, err)
		}
		a, b := notesOf(ex, ex.Channels()), notesOf(back, ex.Channels())
		for ch := range a {
			for i, n := range a[ch] {
				for n > 96 {
					n -= 12 // XM llega hasta B-7: las de más arriba bajan de octava
				}
				a[ch][i] = n
			}
			if len(a[ch]) != len(b[ch]) {
				t.Errorf("%s channel %d: %d notes, back %d", ex.Name, ch, len(a[ch]), len(b[ch]))
				continue
			}
			for i := range a[ch] {
				if a[ch][i] != b[ch][i] {
					t.Errorf("%s channel %d note %d: %d, back %d", ex.Name, ch, i, a[ch][i], b[ch][i])
					break
				}
			}
		}
		if len(back.Samples) == 0 {
			t.Errorf("%s: no samples came back", ex.Name)
		}
		out := Render(back, 48000, 3)
		peak := 0.0
		for _, v := range out {
			peak = math.Max(peak, math.Abs(float64(v)))
		}
		if peak < 0.05 {
			t.Errorf("%s: imported XM is silent", ex.Name)
		}
	}
}

// Una canción de 4 canales con un sample pasa a MOD y vuelve con sus notas
// (el sample suena a 8287 Hz en C-4, como los del MOD: no se corren; las
// notas, dentro de las tres octavas del MOD).
func TestMODRoundTrip(t *testing.T) {
	s := NewSong("mod", ModeFree)
	s.Samples = map[int]*SampleData{}
	s.Samples[0] = &SampleData{Rate: int(math.Round(palClock / (2 * 428))), Bits: 8, Data: testSine(64), LoopLen: 64}
	s.Instrs = map[int]*Instrument{0: sampleInstrument("sine", true)}
	ph := NewPhrase()
	for i, n := range []int8{49, 51, 53, 54, 56, 58, 60, 61} {
		ph[2*i] = Step{Note: n, Instr: 0, Vol: Empty}
	}
	s.Phrases[0] = ph
	c := NewChain()
	c[0].Phrase, c[1].Phrase, c[1].Transpose = 0, 0, -12
	s.Chains[0] = c
	s.Rows = []Row{EmptyRow()}
	s.Rows[0][0] = 0
	data, err := ExportMOD(s)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportMOD(data)
	if err != nil {
		t.Fatal(err)
	}
	a, b := notesOf(s, 1), notesOf(back, 1)
	if len(a[0]) != 16 || len(b[0]) != len(a[0]) {
		t.Fatalf("notes %v, back %v", a[0], b[0])
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("notes %v, back %v", a[0], b[0])
		}
	}
	if _, err := ExportMOD(Examples()[2]); err == nil {
		t.Errorf("an 8-channel song exported as MOD")
	}
}
