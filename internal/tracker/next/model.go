// Package next es el tracker nuevo de audiofilus (ver docs/tracker.md): un
// híbrido de LSDJ (canción → cadenas → frases, instrumentos, tablas, grooves
// y sus comandos) y PICO-8 (instrumento y comando en cada nota, formas de
// onda listas, piano roll), con dos modos por canción: Game Boy (los cuatro
// canales de la consola, con su APU) y Libre (ocho canales, cualquier
// sonido en cualquiera). Reemplaza a internal/tracker cuando tenga sus
// pantallas.
package next

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Mode es el modo de sonido de una canción.
type Mode string

const (
	ModeGB   Mode = "gb"   // 4 canales: PU1, PU2, WAV, NOI, por la APU de la Game Boy
	ModeFree Mode = "free" // 8 canales, cualquier instrumento en cualquiera
)

// Medidas.
const (
	MaxChannels = 8
	GBChannels  = 4
	SongRows    = 256
	NumChains   = 256
	NumPhrases  = 256
	NumInstr    = 64
	NumTables   = 32
	NumGrooves  = 32
	NumWaves    = 256
	Steps       = 16 // pasos de una cadena, una frase, una tabla
	WaveLen     = 32 // muestras de una onda (de 4 bits, como la wave RAM)
	MinBPM      = 40
	MaxBPM      = 300
	// TicksPerBeat con el groove por defecto (6/6): 24, como LSDJ.
	TicksPerBeat = 24
)

// Los canales del modo Game Boy.
const (
	PU1 = iota
	PU2
	WAV
	NOI
)

// Las notas: 0 vacía (sigue lo que sonaba), NoteOff corta. 1..NoteMax son
// C-0..B-9.
const (
	NoteEmpty = 0
	NoteOff   = -1
	NoteMax   = 120
)

// Empty en un número de cadena, frase, instrumento, tabla o volumen: nada.
const Empty = -1

// Cmd es un comando: una letra (ver docs/tracker.md) y su argumento.
type Cmd struct {
	Code byte `json:"c,omitempty"`
	Arg  byte `json:"a,omitempty"`
}

// None dice si no hay comando.
func (c Cmd) None() bool { return c.Code == 0 }

// Los comandos.
const (
	CmdTable    = 'A'
	CmdChord    = 'C'
	CmdDelay    = 'D'
	CmdEnvelope = 'E'
	CmdFine     = 'F'
	CmdGroove   = 'G'
	CmdHop      = 'H'
	CmdFadeIn   = 'I'
	CmdKill     = 'K'
	CmdSlide    = 'L'
	CmdMaster   = 'M'
	CmdOutput   = 'O'
	CmdPitch    = 'P'
	CmdRetrig   = 'R'
	CmdSweep    = 'S'
	CmdTempo    = 'T'
	CmdFadeOut  = 'U'
	CmdVibrato  = 'V'
	CmdWave     = 'W'
	CmdDrop     = 'X'
	CmdRandom   = 'Z'
)

// Commands son los comandos, en el orden en que se eligen.
var Commands = []byte{CmdTable, CmdChord, CmdDelay, CmdEnvelope, CmdFine, CmdGroove, CmdHop, CmdFadeIn,
	CmdKill, CmdSlide, CmdMaster, CmdOutput, CmdPitch, CmdRetrig, CmdSweep, CmdTempo, CmdFadeOut,
	CmdVibrato, CmdWave, CmdDrop, CmdRandom}

// Step es un paso de una frase.
type Step struct {
	Note  int8   `json:"n,omitempty"` // NoteEmpty, NoteOff o 1..NoteMax
	Instr int16  `json:"i"`           // Empty o 0..NumInstr-1
	Vol   int8   `json:"v"`           // Empty o 0..15
	Cmds  [2]Cmd `json:"x,omitzero"`
}

// Phrase son 16 pasos.
type Phrase [Steps]Step

// ChainStep es un paso de una cadena: qué frase y cuánto se transpone.
type ChainStep struct {
	Phrase    int16 `json:"p"` // Empty o 0..NumPhrases-1
	Transpose int8  `json:"t,omitempty"`
}

// Chain son 16 frases en fila.
type Chain [Steps]ChainStep

// Row es una fila de la canción: la cadena de cada canal (Empty: ninguna).
type Row [MaxChannels]int16

// InstrType es el tipo de un instrumento.
type InstrType string

const (
	InstrPulse  InstrType = "pulse"
	InstrWave   InstrType = "wave"
	InstrNoise  InstrType = "noise"
	InstrSynth  InstrType = "synth"  // solo en modo Libre
	InstrSample InstrType = "sample" // un sample (en Game Boy, en WAV)
)

// Envelope es la envolvente de volumen. En el modo Game Boy se usan Start,
// Dir y Period como el registro de la consola (volumen inicial, sube o
// baja, cada Period/64 s); en el modo Libre, ADSR (en ticks y 0..15).
type Envelope struct {
	Start   int8 `json:"start"`            // 0..15
	Dir     int8 `json:"dir,omitempty"`    // -1 baja, 0 fijo, 1 sube
	Period  int8 `json:"period,omitempty"` // 0..7 (0: sin cambio)
	Attack  int  `json:"a,omitempty"`
	Decay   int  `json:"d,omitempty"`
	Sustain int8 `json:"s,omitempty"`
	Release int  `json:"r,omitempty"`
}

// Instrument es un instrumento.
type Instrument struct {
	Name      string    `json:"name,omitempty"`
	Type      InstrType `json:"type"`
	Volume    int8      `json:"vol"` // 0..15 (escala el de la nota)
	Env       Envelope  `json:"env"`
	Table     int16     `json:"table"`            // Empty o una tabla que corre con cada nota
	Pan       int8      `json:"pan,omitempty"`    // -1 izquierda, 0 los dos, 1 derecha
	Length    int       `json:"length,omitempty"` // ticks hasta cortar (0: no corta)
	Transpose bool      `json:"transpose"`        // le afecta la transposición de la cadena
	Vibrato   byte      `json:"vib,omitempty"`    // velocidad·16 + profundidad

	Duty  int8 `json:"duty,omitempty"`  // pulso: 0 12,5 %, 1 25 %, 2 50 %, 3 75 %
	Sweep byte `json:"sweep,omitempty"` // pulso en PU1: el registro NR10

	Wave     int16  `json:"wave,omitempty"`    // onda: su número
	WaveVol  int8   `json:"wavevol,omitempty"` // onda: 0 100 %, 1 50 %, 2 25 % (el desplazamiento de la GB)
	Short    bool   `json:"short,omitempty"`   // ruido: el registro de 7 bits (más tonal)
	Shape    string `json:"shape,omitempty"`   // synth: sine, triangle, saw, square, organ, phantom, noise
	Sample   string `json:"sample,omitempty"`  // sample: el archivo (en la carpeta de la canción)
	Root     int8   `json:"root,omitempty"`    // sample: la nota a la que suena tal cual
	Loop     bool   `json:"loop,omitempty"`    // sample: en bucle
	SynthPWM byte   `json:"pwm,omitempty"`     // synth square: velocidad·16 + profundidad
}

// TableRow es una fila de una tabla.
type TableRow struct {
	Vol       int8   `json:"v"` // Empty o 0..15
	Transpose int8   `json:"t,omitempty"`
	Cmds      [2]Cmd `json:"x,omitzero"`
}

// Table son 16 filas que corren tick a tick con una nota.
type Table [Steps]TableRow

// Groove son los ticks de cada paso, alternados (6/6 recto, 8/4 swing...).
type Groove []int

// Wave es una onda de 32 muestras de 4 bits (0..15).
type Wave [WaveLen]uint8

// Song es una canción.
type Song struct {
	Format  int                 `json:"format"` // 2 (1 eran los patrones del tracker de antes)
	Name    string              `json:"name"`
	Mode    Mode                `json:"mode"`
	BPM     int                 `json:"bpm"`
	Groove  int                 `json:"groove"` // el groove con el que empieza
	Rows    []Row               `json:"rows"`
	Chains  map[int]*Chain      `json:"chains"`
	Phrases map[int]*Phrase     `json:"phrases"`
	Instrs  map[int]*Instrument `json:"instruments"`
	Tables  map[int]*Table      `json:"tables,omitempty"`
	Grooves map[int]Groove      `json:"grooves"`
	Waves   map[int]*Wave       `json:"waves,omitempty"`
}

// Channels es cuántos canales tiene la canción.
func (s *Song) Channels() int {
	if s.Mode == ModeFree {
		return MaxChannels
	}
	return GBChannels
}

// EmptyRow es una fila sin cadenas.
func EmptyRow() Row {
	var r Row
	for i := range r {
		r[i] = Empty
	}
	return r
}

// NewChain es una cadena vacía.
func NewChain() *Chain {
	var c Chain
	for i := range c {
		c[i].Phrase = Empty
	}
	return &c
}

// NewPhrase es una frase vacía.
func NewPhrase() *Phrase {
	var p Phrase
	for i := range p {
		p[i].Instr, p[i].Vol = Empty, Empty
	}
	return &p
}

// NewTable es una tabla vacía.
func NewTable() *Table {
	var t Table
	for i := range t {
		t[i].Vol = Empty
	}
	return &t
}

// NewSong es una canción vacía en mode, con sus instrumentos de base (uno
// de cada tipo que su modo admite) y las ondas de PICO-8.
func NewSong(name string, mode Mode) *Song {
	s := &Song{Format: 2, Name: name, Mode: mode, BPM: 120, Rows: []Row{EmptyRow()},
		Chains: map[int]*Chain{}, Phrases: map[int]*Phrase{}, Instrs: map[int]*Instrument{},
		Tables: map[int]*Table{}, Grooves: map[int]Groove{0: {6, 6}}, Waves: map[int]*Wave{}}
	for i, w := range PresetWaves() {
		w := w
		s.Waves[i] = &w
	}
	s.Instrs[0] = DefaultInstrument(InstrPulse)
	s.Instrs[1] = DefaultInstrument(InstrWave)
	s.Instrs[2] = DefaultInstrument(InstrNoise)
	if mode == ModeFree {
		s.Instrs[3] = DefaultInstrument(InstrSynth)
	}
	return s
}

// DefaultInstrument es un instrumento de tipo t con valores de base.
func DefaultInstrument(t InstrType) *Instrument {
	in := &Instrument{Name: string(t), Type: t, Volume: 15, Table: Empty, Transpose: true,
		Env: Envelope{Start: 15, Sustain: 15}}
	switch t {
	case InstrPulse:
		in.Duty = 2
		in.Env = Envelope{Start: 12, Dir: -1, Period: 3, Sustain: 10, Decay: 12}
	case InstrNoise:
		in.Env = Envelope{Start: 15, Dir: -1, Period: 2, Decay: 8}
	case InstrSynth:
		in.Shape = "triangle"
		in.Env = Envelope{Start: 15, Attack: 1, Decay: 6, Sustain: 10, Release: 6}
	}
	return in
}

// Fix deja la canción dentro de los límites (al leerla de un archivo).
func (s *Song) Fix() {
	if s.Mode != ModeFree {
		s.Mode = ModeGB
	}
	s.Format = 2
	s.BPM = max(MinBPM, min(MaxBPM, s.BPM))
	if len(s.Rows) == 0 {
		s.Rows = []Row{EmptyRow()}
	}
	if len(s.Rows) > SongRows {
		s.Rows = s.Rows[:SongRows]
	}
	for _, m := range []any{&s.Chains, &s.Phrases, &s.Instrs, &s.Tables, &s.Waves} {
		switch m := m.(type) {
		case *map[int]*Chain:
			if *m == nil {
				*m = map[int]*Chain{}
			}
		case *map[int]*Phrase:
			if *m == nil {
				*m = map[int]*Phrase{}
			}
		case *map[int]*Instrument:
			if *m == nil {
				*m = map[int]*Instrument{}
			}
		case *map[int]*Table:
			if *m == nil {
				*m = map[int]*Table{}
			}
		case *map[int]*Wave:
			if *m == nil {
				*m = map[int]*Wave{}
			}
		}
	}
	if s.Grooves == nil {
		s.Grooves = map[int]Groove{}
	}
	if len(s.Grooves[0]) == 0 {
		s.Grooves[0] = Groove{6, 6}
	}
	for k, g := range s.Grooves {
		for i := range g {
			g[i] = max(1, min(32, g[i]))
		}
		if len(g) == 0 {
			delete(s.Grooves, k)
		}
	}
	if _, ok := s.Grooves[s.Groove]; !ok {
		s.Groove = 0
	}
}

// Clone copia la canción entera (el reproductor toca su propia copia).
func (s *Song) Clone() *Song {
	b, _ := json.Marshal(s)
	var c Song
	_ = json.Unmarshal(b, &c)
	c.Fix()
	return &c
}

// Load lee una canción de path; las del tracker de antes (patrones) se
// convierten (ver legacy.go).
func Load(path string) (*Song, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var head struct {
		Format   int             `json:"format"`
		Patterns json.RawMessage `json:"patterns"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if head.Format < 2 && head.Patterns != nil {
		return fromLegacy(b)
	}
	var s Song
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	s.Fix()
	return &s, nil
}

// Save escribe la canción en path.
func (s *Song) Save(path string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
