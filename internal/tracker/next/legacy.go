package next

import (
	"encoding/json"
	"math"
)

// Las canciones del tracker de antes (formato 1): patrones de 16 pasos con
// los cuatro canales a la vez, y un orden de patrones. Se convierten al
// abrirlas: cada patrón da una frase por canal (con una cadena de una
// frase), cada posición del orden una fila de la canción, y sus efectos van
// a los comandos de ahora: A (arpegio) → C, D (ancho) → W, B (desliz) → P.

type legacyCell struct {
	Note int8  `json:"n"`
	Vol  int8  `json:"v"`
	Fx   byte  `json:"f"`
	Arg  uint8 `json:"a"`
}

type legacySong struct {
	Name     string              `json:"name"`
	BPM      int                 `json:"bpm"`
	Patterns [][16][4]legacyCell `json:"patterns"`
	Order    []int               `json:"order"`
}

// fromLegacy convierte una canción del tracker de antes. Sus canales eran
// pulso, pulso, triangular y ruido: la triangular va al canal de onda con
// la onda triangular de las de PICO-8.
func fromLegacy(b []byte) (*Song, error) {
	var old legacySong
	if err := json.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	s := NewSong(old.Name, ModeGB)
	s.BPM = old.BPM
	// Los instrumentos: los del tracker de antes sonaban sin envolvente.
	s.Instrs[0] = &Instrument{Name: "pulse", Type: InstrPulse, Volume: 15, Table: Empty, Transpose: true, Duty: 2, Env: Envelope{Start: 15}}
	s.Instrs[1] = &Instrument{Name: "triangle", Type: InstrWave, Volume: 15, Table: Empty, Transpose: true, Wave: WaveTriangle, Env: Envelope{Start: 15}}
	s.Instrs[2] = &Instrument{Name: "noise", Type: InstrNoise, Volume: 15, Table: Empty, Transpose: true, Env: Envelope{Start: 15, Dir: -1, Period: 1}}
	instrOf := [4]int16{0, 0, 1, 2}
	// Patrón p, canal c → frase y cadena p*4+c.
	for p, pat := range old.Patterns {
		for c := 0; c < GBChannels; c++ {
			ph := NewPhrase()
			used := false
			for st := 0; st < Steps; st++ {
				cell := pat[st][c]
				step := &ph[st]
				step.Note = convertNote(cell.Note)
				if cell.Note > 0 {
					step.Instr = instrOf[c]
				}
				if cell.Vol >= 0 {
					step.Vol = cell.Vol
				}
				switch cell.Fx {
				case 'A':
					step.Cmds[0] = Cmd{CmdChord, cell.Arg}
				case 'D':
					step.Cmds[0] = Cmd{CmdWave, cell.Arg & 3}
				case 'B':
					step.Cmds[0] = Cmd{CmdPitch, legacyBend(cell.Arg)}
				}
				if step.Note != NoteEmpty || step.Vol != Empty || !step.Cmds[0].None() {
					used = true
				}
			}
			if !used {
				continue
			}
			id := p*GBChannels + c
			s.Phrases[id] = ph
			ch := NewChain()
			ch[0].Phrase = int16(id)
			s.Chains[id] = ch
		}
	}
	s.Rows = nil
	for _, p := range old.Order {
		row := EmptyRow()
		for c := 0; c < GBChannels; c++ {
			if _, ok := s.Chains[p*GBChannels+c]; ok {
				row[c] = int16(p*GBChannels + c)
			}
		}
		s.Rows = append(s.Rows, row)
	}
	s.Fix()
	return s, nil
}

// convertNote pasa una nota de antes (1 = C-0, -1 corte) a la de ahora,
// que usa la misma numeración.
func convertNote(n int8) int8 {
	switch {
	case n > 0:
		return n
	case n < 0:
		return NoteOff
	}
	return NoteEmpty
}

// legacyBend pasa un desliz de antes (semitonos por segundo ×4, con signo en
// el bit alto) al comando P (centésimas de semitono por tick, con signo).
func legacyBend(a byte) byte {
	perSec := float64(a&0x7f) / 4
	if a&0x80 != 0 {
		perSec = -perSec
	}
	// A 120 BPM hay 48 ticks por segundo.
	perTick := perSec / 48 * 100
	return byte(int8(math.Max(-127, math.Min(127, math.Round(perTick)))))
}
