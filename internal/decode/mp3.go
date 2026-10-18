package decode

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/hajimehoshi/go-mp3"
)

// go-mp3 siempre entrega estéreo de 16 bits little-endian.
const mp3BytesPerFrame = 4

// mp3Source decodifica MP3 con go-mp3 (Go puro).
type mp3Source struct {
	f   *os.File
	dec *mp3.Decoder
	buf []byte
	pos int64
}

func newMP3(f *os.File) (Source, error) {
	dec, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, err
	}
	return &mp3Source{f: f, dec: dec}, nil
}

func (s *mp3Source) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	need := frames * mp3BytesPerFrame
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	buf := s.buf[:need]
	n, err := io.ReadFull(s.dec, buf)
	n -= n % mp3BytesPerFrame
	for i := 0; i < n/2; i++ {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(buf[i*2:]))) / 32768
	}
	s.pos += int64(n / mp3BytesPerFrame)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if n > 0 && err == io.EOF {
		err = nil
	}
	return n / 2, err
}

func (s *mp3Source) SampleRate() int { return s.dec.SampleRate() }
func (s *mp3Source) Length() int64   { return s.dec.Length() / mp3BytesPerFrame }
func (s *mp3Source) Position() int64 { return s.pos }

func (s *mp3Source) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	if _, err := s.dec.Seek(frame*mp3BytesPerFrame, io.SeekStart); err != nil {
		return err
	}
	s.pos = frame
	return nil
}

func (s *mp3Source) Close() error { return s.f.Close() }
