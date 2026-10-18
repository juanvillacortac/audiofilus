package decode

import (
	"fmt"
	"io"
	"os"

	"github.com/jfreymuth/oggvorbis"
)

// vorbisSource decodifica Ogg Vorbis con jfreymuth/oggvorbis (Go puro).
type vorbisSource struct {
	f        *os.File
	r        *oggvorbis.Reader
	channels int
	buf      []float32
}

func newVorbis(f *os.File) (Source, error) {
	r, err := oggvorbis.NewReader(f)
	if err != nil {
		// Un .ogg también puede llevar Opus, que necesitaría cgo o FFmpeg; se
		// informa como no soportado.
		return nil, fmt.Errorf("%w (ogg without vorbis: %v)", ErrUnsupported, err)
	}
	return &vorbisSource{f: f, r: r, channels: r.Channels()}, nil
}

func (s *vorbisSource) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	if s.channels == 2 {
		n, err := s.r.Read(dst[:frames*2])
		n -= n % 2
		return n, normalizeEOF(n, err)
	}
	need := frames * s.channels
	if cap(s.buf) < need {
		s.buf = make([]float32, need)
	}
	n, err := s.r.Read(s.buf[:need])
	got := n / s.channels
	toStereo(dst, s.buf[:got*s.channels], s.channels)
	return got * 2, normalizeEOF(got, err)
}

func (s *vorbisSource) SampleRate() int { return s.r.SampleRate() }
func (s *vorbisSource) Length() int64   { return s.r.Length() }
func (s *vorbisSource) Position() int64 { return s.r.Position() }

func (s *vorbisSource) SeekFrame(frame int64) error {
	return s.r.SetPosition(clampFrame(frame, s.Length()))
}

func (s *vorbisSource) Close() error { return s.f.Close() }

// toStereo escribe len(src)/channels frames estéreo en dst. El mono se duplica;
// con más canales se quedan los dos primeros (frontal izquierdo y derecho).
func toStereo(dst, src []float32, channels int) {
	frames := len(src) / channels
	for i := 0; i < frames; i++ {
		l := src[i*channels]
		r := l
		if channels > 1 {
			r = src[i*channels+1]
		}
		dst[i*2] = l
		dst[i*2+1] = r
	}
}

// normalizeEOF oculta io.EOF si la lectura todavía trajo datos: el EOF llegará
// en la siguiente llamada.
func normalizeEOF(n int, err error) error {
	if err == io.EOF && n > 0 {
		return nil
	}
	return err
}
