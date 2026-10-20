package tracker

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const rate = 48000

// song1 es una canción de una fila: en PU1, una cadena con una frase que
// tiene una nota en el paso 0.
func song1(mode Mode) *Song {
	s := NewSong("t", mode)
	ph := NewPhrase()
	ph[0] = Step{Note: 49, Instr: 0, Vol: Empty}
	s.Phrases[0] = ph
	ch := NewChain()
	ch[0].Phrase = 0
	s.Chains[0] = ch
	s.Rows[0][PU1] = 0
	return s
}

// run toca n muestras y devuelve el pico.
func run(p *Player, n int) float64 {
	b := make([]byte, n*8)
	p.Read(b)
	peak := 0.0
	for i := 0; i < len(b); i += 4 {
		v := math.Float32frombits(uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24)
		peak = math.Max(peak, math.Abs(float64(v)))
	}
	return peak
}

// Una frase de 16 pasos con el groove 6/6 a 120 BPM dura 2 segundos: 96
// ticks a 48 por segundo; la nota vuelve a sonar al empezar de nuevo.
func TestPhraseTiming(t *testing.T) {
	p := NewPlayer(rate)
	p.SetSong(song1(ModeGB))
	p.Play(PlaySong, 0, 0)
	run(p, rate/10)
	if h := p.Hits()[PU1]; h != 1 {
		t.Fatalf("hits after 0.1 s = %d, want 1", h)
	}
	run(p, rate*19/10-rate/100)
	if h := p.Hits()[PU1]; h != 1 {
		t.Fatalf("hits just before 2 s = %d, want 1", h)
	}
	run(p, rate/50)
	if h := p.Hits()[PU1]; h != 2 {
		t.Fatalf("hits after 2 s = %d, want 2", h)
	}
}

// Un groove de 3/3 hace la frase el doble de rápida.
func TestGroove(t *testing.T) {
	s := song1(ModeGB)
	s.Grooves[0] = Groove{3, 3}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate*11/10)
	if h := p.Hits()[PU1]; h != 2 {
		t.Fatalf("hits after 1.1 s with groove 3/3 = %d, want 2", h)
	}
}

// Las cadenas avanzan por sus pasos y, al acabar, el canal pasa a la fila
// siguiente; una fila vacía descansa lo que dura una frase.
func TestChainAndRows(t *testing.T) {
	s := song1(ModeGB)
	s.Chains[0][1].Phrase = 0 // dos frases
	s.Rows = append(s.Rows, EmptyRow(), EmptyRow())
	s.Rows[2][PU1] = 0
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate*3)
	if row, pos, _, _ := p.Position(PU1); row != 0 || pos != 1 {
		t.Fatalf("at 3 s: row %d chain step %d, want 0, 1", row, pos)
	}
	run(p, rate*2)
	if row, _, _, _ := p.Position(PU1); row != 1 {
		t.Fatalf("at 5 s: row %d, want 1 (resting)", row)
	}
	run(p, rate*2)
	if row, _, _, _ := p.Position(PU1); row != 2 {
		t.Fatalf("at 7 s: row %d, want 2", row)
	}
	if h := p.Hits()[PU1]; h != 3 {
		t.Fatalf("hits = %d, want 3", h)
	}
}

// H en una frase la corta: la siguiente empieza en el paso que diga.
func TestPhraseHop(t *testing.T) {
	s := song1(ModeGB)
	s.Phrases[0][3].Cmds[0] = Cmd{CmdHop, 0}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	// 4 pasos de 6 ticks = 0,5 s por vuelta.
	run(p, rate*11/10)
	if h := p.Hits()[PU1]; h != 3 {
		t.Fatalf("hits after 1.1 s with H00 on step 3 = %d, want 3", h)
	}
}

// K corta la nota: después solo queda silencio.
func TestKill(t *testing.T) {
	s := song1(ModeGB)
	s.Instrs[0].Env = Envelope{Start: 15}
	s.Phrases[0][0].Cmds[0] = Cmd{CmdKill, 4}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	if peak := run(p, rate/20); peak < 0.05 {
		t.Fatalf("no sound before the kill (peak %v)", peak)
	}
	run(p, rate/20)
	if peak := run(p, rate/10); peak > 1e-3 {
		t.Fatalf("sound after K04 (peak %v)", peak)
	}
}

// La envolvente de la GB baja un paso cada period/64 s: con 15 y period 1
// la nota se apaga en un cuarto de segundo.
func TestGBEnvelope(t *testing.T) {
	s := song1(ModeGB)
	s.Instrs[0].Env = Envelope{Start: 15, Dir: -1, Period: 1}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate*3/10)
	if peak := run(p, rate/10); peak > 1e-3 {
		t.Fatalf("envelope did not reach zero (peak %v)", peak)
	}
}

// T cambia el tempo: a 240 BPM la frase dura un segundo.
func TestTempoCommand(t *testing.T) {
	s := song1(ModeGB)
	s.Phrases[0][0].Cmds[0] = Cmd{CmdTempo, 240}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate*11/10)
	if h := p.Hits()[PU1]; h != 2 {
		t.Fatalf("hits after 1.1 s at T240 = %d, want 2", h)
	}
}

// Una tabla corre una fila por tick: con su transposición, la nota cambia.
func TestTable(t *testing.T) {
	s := song1(ModeGB)
	tb := NewTable()
	tb[0].Transpose = 12
	tb[1].Cmds[0] = Cmd{CmdHop, 1}
	s.Tables[0] = tb
	s.Instrs[0].Table = 0
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate/100)
	p.mu.Lock()
	first := p.chans[PU1].v.note
	p.mu.Unlock()
	if first != 61 {
		t.Fatalf("note on the table's first row = %v, want 61", first)
	}
	run(p, rate/10)
	p.mu.Lock()
	n := p.chans[PU1].v.note
	p.mu.Unlock()
	if n != 49 {
		t.Fatalf("note after the table hop = %v, want 49", n)
	}
}

// La GB solo toca las frecuencias de sus periodos de 11 bits.
func TestGBPeriod(t *testing.T) {
	f := gbPeriod(noteHz(49), 131072)
	x := 2048 - 131072/f
	if math.Abs(x-math.Round(x)) > 1e-9 {
		t.Fatalf("period %v is not an integer", x)
	}
	if math.Abs(f-noteHz(49))/noteHz(49) > 0.01 {
		t.Fatalf("A-4 rounded to %v Hz", f)
	}
}

// noteHz: la nota 58 (A-4, con 1 = C-0) es 440 Hz.
func TestNoteHz(t *testing.T) {
	if f := noteHz(58); math.Abs(f-440) > 1e-9 {
		t.Fatalf("noteHz(58) = %v, want 440", f)
	}
}

// Render termina una canción que se toca una vez (con su segundo de cola).
func TestRender(t *testing.T) {
	out := Render(song1(ModeFree), rate, 60)
	secs := float64(len(out)/2) / rate
	if secs < 2.9 || secs > 3.1 {
		t.Fatalf("render length %v s, want about 3", secs)
	}
}

// Las ondas de PICO-8 caben en los 4 bits de la wave RAM y no son planas.
func TestPresetWaves(t *testing.T) {
	for i, w := range PresetWaves() {
		lo, hi := uint8(15), uint8(0)
		for _, v := range w {
			if v > 15 {
				t.Fatalf("wave %d has %d", i, v)
			}
			lo, hi = min(lo, v), max(hi, v)
		}
		if hi-lo < 8 {
			t.Errorf("wave %s is almost flat (%d..%d)", WaveNames[i], lo, hi)
		}
	}
}

// Una canción del tracker de antes se convierte: cada patrón da una frase y
// una cadena por canal usado, y el orden las filas.
func TestLegacy(t *testing.T) {
	old := `{"name":"old","bpm":140,"patterns":[` + legacyPattern(25, 'A', 0x47) + `,` + legacyPattern(37, 'B', 0x88) + `],"order":[0,1,0]}`
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BPM != 140 || len(s.Rows) != 3 {
		t.Fatalf("bpm %d rows %d", s.BPM, len(s.Rows))
	}
	if s.Rows[1][PU1] != 4 || s.Rows[2][PU1] != 0 || s.Rows[0][PU2] != Empty {
		t.Fatalf("rows %v", s.Rows)
	}
	st := s.Phrases[0][0]
	if st.Note != 25 || st.Instr != 0 || st.Cmds[0] != (Cmd{CmdChord, 0x47}) {
		t.Fatalf("step %+v", st)
	}
	if c := s.Phrases[4][0].Cmds[0]; c.Code != CmdPitch || int8(c.Arg) >= 0 {
		t.Fatalf("bend down converted to %+v", c)
	}
}

// legacyPattern es un patrón de antes con una nota en el paso 0 de PU1.
func legacyPattern(note int, fx byte, arg byte) string {
	out := "["
	for st := 0; st < 16; st++ {
		if st > 0 {
			out += ","
		}
		out += "["
		for c := 0; c < 4; c++ {
			if c > 0 {
				out += ","
			}
			if st == 0 && c == 0 {
				out += `{"n":` + itoa(note) + `,"v":-1,"f":` + itoa(int(fx)) + `,"a":` + itoa(int(arg)) + `}`
			} else {
				out += `{"n":0,"v":-1}`
			}
		}
		out += "]"
	}
	return out + "]"
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// Los ejemplos suenan, y sus canales pasan de fila a la vez (cada fila dura
// lo que su cadena más larga, también para los canales que descansan).
func TestExamples(t *testing.T) {
	for _, s := range Examples() {
		p := NewPlayer(rate)
		p.SetSong(s)
		p.start(PlaySong, 0, 0, true)
		peak := 0.0
		for i := 0; i < rate*120 && !p.ended; i++ {
			l, _ := p.next()
			peak = math.Max(peak, math.Abs(l))
			if p.tickAcc+p.tickRate()/p.rate < 1 {
				continue // solo justo antes de cada tick
			}
			row0 := p.chans[0].row
			for ch := 1; ch < s.Channels(); ch++ {
				if p.chans[ch].row != row0 && !p.chans[ch].done && !p.chans[0].done {
					t.Fatalf("%s: at %.2f s channel %d is on row %d, channel 0 on %d", s.Name, float64(i)/rate, ch, p.chans[ch].row, row0)
				}
			}
		}
		if !p.ended {
			t.Errorf("%s did not end", s.Name)
		}
		if peak < 0.1 || peak > 1 {
			t.Errorf("%s peak %v", s.Name, peak)
		}
	}
}

// D retrasa el paso: la nota suena unos ticks más tarde.
func TestDelay(t *testing.T) {
	s := song1(ModeGB)
	s.Phrases[0][0].Cmds[0] = Cmd{CmdDelay, 3}
	p := NewPlayer(rate)
	p.SetSong(s)
	p.Play(PlaySong, 0, 0)
	run(p, rate/48) // un tick
	if h := p.Hits()[PU1]; h != 0 {
		t.Fatalf("note played before its delay")
	}
	run(p, rate*3/48)
	if h := p.Hits()[PU1]; h != 1 {
		t.Fatalf("delayed note did not play (hits %d)", h)
	}
}
