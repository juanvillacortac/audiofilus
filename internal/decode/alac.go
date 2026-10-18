package decode

import (
	"fmt"
	"io"
	"os"

	"github.com/juanvillacortac/audiofilus/internal/decode/alac"
)

// Apple Lossless dentro de MP4, en Go puro: mp4.go saca los bloques y el
// paquete alac los decodifica.

type alacSource struct {
	f       *os.File
	trk     *mp4Track
	dec     *alac.Decoder
	next    int
	pos     int64
	skip    int64
	in      []byte
	pending []float32
}

func newALAC(f *os.File, trk *mp4Track) (Source, error) {
	dec, err := alac.New(trk.config)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrUnsupported, err)
	}
	return &alacSource{f: f, trk: trk, dec: dec}, nil
}

// decodeNext decodifica el siguiente bloque y lo deja en pending.
func (s *alacSource) decodeNext() error {
	if s.next >= s.trk.samples() {
		return io.EOF
	}
	i := s.next
	s.next++
	size := s.trk.sizes[i]
	if cap(s.in) < int(size) {
		s.in = make([]byte, size)
	}
	in := s.in[:size]
	if _, err := s.f.ReadAt(in, s.trk.offsets[i]); err != nil {
		return err
	}
	out := s.dec.Decode(in)
	bps := s.dec.BitDepth / 8
	ch := s.dec.Channels
	frames := len(out) / (bps * ch)
	sample := func(b []byte) float32 {
		if bps == 2 {
			return float32(int16(uint16(b[0])|uint16(b[1])<<8)) / 32768
		}
		v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
		return float32(v) / 8388608
	}
	for k := 0; k < frames; k++ {
		fr := out[k*bps*ch:]
		l := sample(fr)
		r := l
		if ch > 1 {
			r = sample(fr[bps:])
		}
		s.pending = append(s.pending, l, r)
	}
	return nil
}

func (s *alacSource) Read(dst []float32) (int, error) {
	for len(s.pending) == 0 || s.skip > 0 {
		if s.skip > 0 && len(s.pending) > 0 {
			drop := min(s.skip, int64(len(s.pending)/2))
			s.pending = s.pending[drop*2:]
			s.skip -= drop
			continue
		}
		if err := s.decodeNext(); err != nil {
			return 0, err
		}
	}
	n := copy(dst[:len(dst)&^1], s.pending)
	s.pending = s.pending[n:]
	s.pos += int64(n / 2)
	return n, nil
}

func (s *alacSource) SampleRate() int { return s.dec.SampleRate }

func (s *alacSource) Length() int64 {
	return s.trk.duration() * int64(s.dec.SampleRate) / s.trk.timescale
}

func (s *alacSource) Position() int64 { return s.pos }

func (s *alacSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	rate := int64(s.dec.SampleRate)
	i := s.trk.sampleAt(frame * s.trk.timescale / rate)
	start := s.trk.starts[i] * rate / s.trk.timescale
	s.next, s.pending, s.skip, s.pos = i, nil, frame-start, frame
	return nil
}

func (s *alacSource) Close() error { return s.f.Close() }
