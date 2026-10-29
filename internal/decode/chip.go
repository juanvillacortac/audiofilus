package decode

import (
	"io"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/chip"
)

// La música de chips de ordenadores de 8 bits, emulada en Go (internal/chip,
// sin bibliotecas en C): SID (Commodore 64), YM (Atari ST), VTX y PT3 (ZX
// Spectrum). Van por el mismo camino que los de libgme: un .sid puede traer
// varias canciones, y cada una es una pista con su ruta "archivo#song=n"
// (GMEPath). Las que dan la vuelta se apagan en sus últimos 8 segundos.

// chipTracks son las canciones de un archivo de chips, como las de libgme.
func chipTracks(file string) ([]GMETrack, error) {
	ts, err := chip.Tracks(file)
	if err != nil {
		return nil, err
	}
	out := make([]GMETrack, len(ts))
	for i, t := range ts {
		out[i] = GMETrack{Song: t.Song, Title: t.Title, Game: t.Game, Author: t.Author, System: t.System,
			Length: int(t.Length / time.Millisecond), Loops: t.Loops}
	}
	return out, nil
}

// chipSource toca una canción de un archivo de chips.
type chipSource struct {
	file   string
	song   int
	tune   chip.Tune
	length int64 // cuadros
	fade   int64 // desde dónde se apaga (length si no se apaga)
	pos    int64
	ended  bool
}

// newChip abre la canción song (desde 1) de file.
func newChip(file string, song int) (Source, error) {
	tune, t, err := chip.Open(file, song, gmeRate)
	if err != nil {
		return nil, err
	}
	s := &chipSource{file: file, song: song, tune: tune, length: int64(t.Length) * gmeRate / int64(time.Second)}
	s.fade = s.length
	if t.Loops {
		s.fade = s.length - min(gmeFade*gmeRate/1000, s.length/4)
	}
	return s, nil
}

func (s *chipSource) Read(dst []float32) (int, error) {
	n := len(dst) &^ 1
	if left := (s.length - s.pos) * 2; int64(n) > left {
		n = int(left)
	}
	if n <= 0 || s.ended {
		s.ended = true
		return 0, io.EOF
	}
	got := s.tune.Render(dst[:n])
	if got < n {
		// Acabó antes de lo que decía (un volcado más corto): hasta aquí.
		s.length = s.pos + int64(got/2)
		s.fade = min(s.fade, s.length)
	}
	for i := 0; i < got; i += 2 {
		if f := s.pos + int64(i/2); f >= s.fade {
			g := float32(s.length-f) / float32(s.length-s.fade)
			dst[i] *= g
			dst[i+1] *= g
		}
	}
	s.pos += int64(got / 2)
	if got == 0 {
		s.ended = true
		return 0, io.EOF
	}
	return got, nil
}

func (s *chipSource) SampleRate() int { return gmeRate }
func (s *chipSource) Length() int64   { return s.length }
func (s *chipSource) Position() int64 { return s.pos }

// SeekFrame salta a frame. Como libgme, no salta de verdad: emula hasta ahí
// sin guardar el audio (hacia atrás, desde el principio).
func (s *chipSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.length)
	if frame < s.pos {
		tune, _, err := chip.Open(s.file, s.song, gmeRate)
		if err != nil {
			return err
		}
		s.tune, s.pos = tune, 0
	}
	if sk, ok := s.tune.(chip.Skipper); ok {
		sk.Skip(int(frame - s.pos))
		s.pos, s.ended = frame, false
		return nil
	}
	buf := make([]float32, 8192)
	for s.pos < frame {
		n := int(min(int64(len(buf)), (frame-s.pos)*2))
		got := s.tune.Render(buf[:n])
		s.pos += int64(got / 2)
		if got < n {
			break
		}
	}
	s.ended = false
	return nil
}

func (s *chipSource) Close() error { return nil }
