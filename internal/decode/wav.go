package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	wavFormatPCM        = 1
	wavFormatFloat      = 3
	wavFormatExtensible = 0xFFFE
)

// wavSource lee WAV PCM (8, 16, 24 o 32 bits) y float de 32 bits. También
// sirve para AIFF (ver aiff.go), que guarda lo mismo en big-endian.
type wavSource struct {
	f          *os.File
	format     int
	channels   int
	sampleRate int
	bits       int
	big        bool // muestras big-endian (AIFF); en WAV, little-endian
	signed8    bool // muestras de 8 bits con signo (AIFF); en WAV, sin signo
	dataStart  int64
	frames     int64
	pos        int64
	buf        []byte
}

func newWAV(f *os.File) (Source, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, err
	}
	s := &wavSource{f: f}
	gotFmt := false
	for {
		var ch [8]byte
		if _, err := io.ReadFull(f, ch[:]); err != nil {
			return nil, errors.New("wav: no data chunk")
		}
		id := string(ch[:4])
		size := int64(binary.LittleEndian.Uint32(ch[4:]))
		switch id {
		case "fmt ":
			b := make([]byte, size)
			if _, err := io.ReadFull(f, b); err != nil || size < 16 {
				return nil, errors.New("wav: bad fmt chunk")
			}
			s.format = int(binary.LittleEndian.Uint16(b[0:]))
			s.channels = int(binary.LittleEndian.Uint16(b[2:]))
			s.sampleRate = int(binary.LittleEndian.Uint32(b[4:]))
			s.bits = int(binary.LittleEndian.Uint16(b[14:]))
			if s.format == wavFormatExtensible && size >= 26 {
				s.format = int(binary.LittleEndian.Uint16(b[24:]))
			}
			gotFmt = true
		case "data":
			if !gotFmt {
				return nil, errors.New("wav: data before fmt")
			}
			if err := s.validate(); err != nil {
				return nil, err
			}
			pos, err := f.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, err
			}
			s.dataStart = pos
			s.frames = size / int64(s.frameBytes())
			return s, nil
		default:
			if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
}

// validate comprueba que el formato del chunk fmt se pueda leer.
func (s *wavSource) validate() error {
	if s.channels < 1 || s.sampleRate < 1 {
		return errors.New("wav: bad fmt values")
	}
	switch {
	case s.format == wavFormatPCM && (s.bits == 8 || s.bits == 16 || s.bits == 24 || s.bits == 32):
	case s.format == wavFormatFloat && s.bits == 32:
	default:
		return fmt.Errorf("%w (wav format %d, %d bits)", ErrUnsupported, s.format, s.bits)
	}
	return nil
}

// frameBytes es el tamaño de un frame (todas las muestras de un instante).
func (s *wavSource) frameBytes() int { return s.channels * s.bits / 8 }

// sample convierte una muestra del archivo a float32 en [-1, 1].
func (s *wavSource) sample(b []byte) float32 {
	var order binary.ByteOrder = binary.LittleEndian
	if s.big {
		order = binary.BigEndian
	}
	switch {
	case s.format == wavFormatFloat:
		return math.Float32frombits(order.Uint32(b))
	case s.bits == 8 && s.signed8:
		return float32(int8(b[0])) / 128
	case s.bits == 8:
		return (float32(b[0]) - 128) / 128
	case s.bits == 16:
		return float32(int16(order.Uint16(b))) / 32768
	case s.bits == 24 && s.big:
		v := int32(int8(b[0]))<<16 | int32(b[1])<<8 | int32(b[2])
		return float32(v) / 8388608
	case s.bits == 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
		return float32(v) / 8388608
	default:
		return float32(int32(order.Uint32(b))) / 2147483648
	}
}

func (s *wavSource) Read(dst []float32) (int, error) {
	frames := int64(len(dst) / 2)
	if left := s.frames - s.pos; frames > left {
		frames = left
	}
	if frames <= 0 {
		return 0, io.EOF
	}
	fb := s.frameBytes()
	need := int(frames) * fb
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	buf := s.buf[:need]
	n, err := io.ReadFull(s.f, buf)
	got := n / fb
	sb := s.bits / 8
	for i := 0; i < got; i++ {
		frame := buf[i*fb:]
		l := s.sample(frame)
		r := l
		if s.channels > 1 {
			r = s.sample(frame[sb:])
		}
		dst[i*2] = l
		dst[i*2+1] = r
	}
	s.pos += int64(got)
	if got > 0 {
		err = nil
	} else if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return got * 2, err
}

func (s *wavSource) SampleRate() int { return s.sampleRate }
func (s *wavSource) Length() int64   { return s.frames }
func (s *wavSource) Position() int64 { return s.pos }

func (s *wavSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.frames)
	if _, err := s.f.Seek(s.dataStart+frame*int64(s.frameBytes()), io.SeekStart); err != nil {
		return err
	}
	s.pos = frame
	return nil
}

func (s *wavSource) Close() error { return s.f.Close() }
