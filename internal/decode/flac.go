package decode

import (
	"io"
	"os"

	"github.com/mewkiz/flac"
)

// flacSource decodifica FLAC con mewkiz/flac (Go puro).
type flacSource struct {
	f      *os.File
	stream *flac.Stream
	scale  float32
	// Bloque FLAC ya decodificado que aún no se entregó, en float32 estéreo.
	pending []float32
	pos     int64
}

func newFLAC(f *os.File) (Source, error) {
	stream, err := flac.NewSeek(f)
	if err != nil {
		return nil, err
	}
	bps := stream.Info.BitsPerSample
	return &flacSource{
		f:      f,
		stream: stream,
		scale:  1 / float32(int64(1)<<(bps-1)),
	}, nil
}

func (s *flacSource) Read(dst []float32) (int, error) {
	n := 0
	defer func() { s.pos += int64(n / 2) }()
	for n+1 < len(dst) {
		if len(s.pending) == 0 {
			if err := s.decodeFrame(); err != nil {
				if n > 0 && err == io.EOF {
					return n, nil
				}
				return n, err
			}
		}
		c := copy(dst[n:len(dst)-len(dst)%2], s.pending)
		s.pending = s.pending[c:]
		n += c
	}
	return n, nil
}

// decodeFrame decodifica el siguiente bloque FLAC en s.pending.
func (s *flacSource) decodeFrame() error {
	fr, err := s.stream.ParseNext()
	if err != nil {
		return err
	}
	ch := len(fr.Subframes)
	frames := int(fr.BlockSize)
	out := s.pending[:0]
	if cap(out) < frames*2 {
		out = make([]float32, 0, frames*2)
	}
	left := fr.Subframes[0].Samples
	right := left
	if ch > 1 {
		right = fr.Subframes[1].Samples
	}
	for i := 0; i < frames; i++ {
		out = append(out, float32(left[i])*s.scale, float32(right[i])*s.scale)
	}
	s.pending = out
	return nil
}

func (s *flacSource) SampleRate() int { return int(s.stream.Info.SampleRate) }

func (s *flacSource) Length() int64 {
	if s.stream.Info.NSamples == 0 {
		return -1
	}
	return int64(s.stream.Info.NSamples)
}

func (s *flacSource) Position() int64 { return s.pos }

func (s *flacSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	// Stream.Seek cae al principio del bloque FLAC que contiene frame.
	got, err := s.stream.Seek(uint64(frame))
	if err != nil {
		return err
	}
	s.pending = s.pending[:0]
	s.pos = int64(got)
	// Se decodifica y descarta el inicio de ese bloque para quedar justo en frame.
	for s.pos < frame {
		if err := s.decodeFrame(); err != nil {
			return err
		}
		skip := int(frame-s.pos) * 2
		if skip >= len(s.pending) {
			s.pos += int64(len(s.pending) / 2)
			s.pending = s.pending[:0]
			continue
		}
		s.pending = s.pending[skip:]
		s.pos = frame
	}
	return nil
}

func (s *flacSource) Close() error {
	s.stream.Close()
	return s.f.Close()
}
