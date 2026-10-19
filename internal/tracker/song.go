// Package tracker es el tracker de audiofilus: canciones chiptune de cuatro
// canales (dos de pulso, uno triangular y uno de ruido), al estilo de los
// trackers de Game Boy. Una canción es una lista de patrones; un patrón, 16
// pasos de semicorchea por canal, y cada paso una nota, un volumen y un
// efecto. El sintetizador (synth.go) la toca en tiempo real o la pasa a WAV.
package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Medidas de una canción.
const (
	Steps       = 16 // pasos por patrón (semicorcheas: 4 por tiempo)
	Channels    = 4  // pulso 1, pulso 2, triangular, ruido
	MaxPatterns = 64
	MaxOrder    = 64
	MinBPM      = 40
	MaxBPM      = 300
)

// Los canales.
const (
	Pulse1 = iota
	Pulse2
	Triangle
	Noise
)

// Los valores especiales de Cell.Note.
const (
	NoteEmpty = 0  // nada: sigue lo que sonaba
	NoteOff   = -1 // corta el canal
	NoteMax   = 96 // C-0 (1) a B-7 (96)
)

// Los efectos.
const (
	FxNone = 0
	FxArp  = 'A' // arpegio: nota, +x, +y semitonos (Arg = xy), muy rápido
	FxDuty = 'D' // ancho del pulso: 0 12,5 %, 1 25 %, 2 50 %, 3 75 %
	FxBend = 'B' // desliza el tono: Arg 01-7F sube, 81-FF baja (en semitonos por segundo ×4)
)

// Cell es un paso de un canal.
type Cell struct {
	Note int8  `json:"n,omitempty"` // NoteEmpty, NoteOff o 1..NoteMax
	Vol  int8  `json:"v"`           // -1 sin cambio, 0..15
	Fx   byte  `json:"f,omitempty"` // FxNone, FxArp, FxDuty o FxBend
	Arg  uint8 `json:"a,omitempty"`
}

// Empty dice si el paso no tiene nada.
func (c Cell) Empty() bool { return c.Note == NoteEmpty && c.Vol < 0 && c.Fx == FxNone }

// Pattern son 16 pasos de los cuatro canales.
type Pattern [Steps][Channels]Cell

// NewPattern es un patrón vacío.
func NewPattern() Pattern {
	var p Pattern
	for s := range p {
		for c := range p[s] {
			p[s][c].Vol = -1
		}
	}
	return p
}

// Song es una canción: el tempo, los patrones y en qué orden suenan.
type Song struct {
	Name     string    `json:"name"`
	BPM      int       `json:"bpm"`
	Patterns []Pattern `json:"patterns"`
	Order    []int     `json:"order"`
}

// NewSong es una canción nueva, con un ritmo de ejemplo en el primer patrón
// para que suene algo nada más empezar: bombo y caja en el ruido, un bajo en
// la triangular y un arpegio en el primer pulso.
func NewSong() *Song {
	p := NewPattern()
	for s := 0; s < Steps; s += 4 {
		// Bombo (ruido grave) en 0 y 8, caja (ruido agudo) en 4 y 12.
		n := int8(13)
		if s%8 == 4 {
			n = 49
		}
		p[s][Noise] = Cell{Note: n, Vol: 12}
	}
	bass := []int8{25, 25, 32, 30} // C-2 C-2 G-2 F-2
	for i, n := range bass {
		p[i*4][Triangle] = Cell{Note: n, Vol: 15}
	}
	p[0][Pulse1] = Cell{Note: 49, Vol: 9, Fx: FxArp, Arg: 0x47} // C-4 mayor
	p[8][Pulse1] = Cell{Note: 54, Vol: 9, Fx: FxArp, Arg: 0x37} // F-4 menor
	p[0][Pulse2] = Cell{Note: 61, Vol: 6, Fx: FxDuty, Arg: 1}
	p[6][Pulse2] = Cell{Note: NoteOff, Vol: -1}
	return &Song{Name: "song", BPM: 120, Patterns: []Pattern{p}, Order: []int{0}}
}

// Clone copia la canción (el sintetizador toca su propia copia).
func (s *Song) Clone() *Song {
	c := *s
	c.Patterns = append([]Pattern(nil), s.Patterns...)
	c.Order = append([]int(nil), s.Order...)
	return &c
}

// Fix deja la canción dentro de los límites (al leerla de un archivo).
func (s *Song) Fix() {
	s.BPM = max(MinBPM, min(MaxBPM, s.BPM))
	if len(s.Patterns) == 0 {
		s.Patterns = []Pattern{NewPattern()}
	}
	if len(s.Patterns) > MaxPatterns {
		s.Patterns = s.Patterns[:MaxPatterns]
	}
	if len(s.Order) == 0 {
		s.Order = []int{0}
	}
	if len(s.Order) > MaxOrder {
		s.Order = s.Order[:MaxOrder]
	}
	for i, o := range s.Order {
		s.Order[i] = max(0, min(len(s.Patterns)-1, o))
	}
}

// Load lee una canción de path.
func Load(path string) (*Song, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Song
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.Fix()
	return &s, nil
}

// Save guarda la canción en path (escribiendo a un temporal y renombrando,
// para no dejarla a medias).
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

var noteNames = [12]string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}

// NoteName es el nombre de una nota: "C-4", "F#2", "---" (vacía) u "OFF".
func NoteName(n int8) string {
	switch {
	case n == NoteEmpty:
		return "---"
	case n == NoteOff:
		return "OFF"
	}
	i := int(n) - 1
	return fmt.Sprintf("%s%d", noteNames[i%12], i/12)
}
