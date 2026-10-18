package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// AIFF y AIFF-C sin comprimir: el mismo PCM que WAV pero big-endian (salvo
// "sowt", que es little-endian), así que lo lee wavSource.

func newAIFF(f *os.File) (Source, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, err
	}
	aifc := string(hdr[8:12]) == "AIFC"
	s := &wavSource{f: f, format: wavFormatPCM, big: true, signed8: true}
	gotComm := false
	for {
		var ch [8]byte
		if _, err := io.ReadFull(f, ch[:]); err != nil {
			return nil, errors.New("aiff: no SSND chunk")
		}
		id := string(ch[:4])
		size := int64(binary.BigEndian.Uint32(ch[4:]))
		switch id {
		case "COMM":
			b := make([]byte, size)
			if _, err := io.ReadFull(f, b); err != nil || size < 18 {
				return nil, errors.New("aiff: bad COMM chunk")
			}
			s.channels = int(binary.BigEndian.Uint16(b[0:]))
			s.frames = int64(binary.BigEndian.Uint32(b[2:]))
			s.bits = int(binary.BigEndian.Uint16(b[6:]))
			s.sampleRate = int(extended80(b[8:18]))
			if aifc && size >= 22 {
				switch string(b[18:22]) {
				case "NONE", "twos":
				case "sowt":
					s.big = false
				case "fl32", "FL32":
					s.format = wavFormatFloat
				default:
					return nil, fmt.Errorf("%w (AIFF-C compression %q)", ErrUnsupported, b[18:22])
				}
			}
			if size%2 == 1 {
				_, _ = f.Seek(1, io.SeekCurrent)
			}
			gotComm = true
		case "SSND":
			if !gotComm {
				return nil, errors.New("aiff: SSND before COMM")
			}
			if err := s.validate(); err != nil {
				return nil, err
			}
			var off [8]byte
			if _, err := io.ReadFull(f, off[:]); err != nil {
				return nil, err
			}
			pos, err := f.Seek(int64(binary.BigEndian.Uint32(off[:4])), io.SeekCurrent)
			if err != nil {
				return nil, err
			}
			s.dataStart = pos
			if max := (size - 8) / int64(s.frameBytes()); s.frames > max {
				s.frames = max
			}
			return s, nil
		default:
			if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
}

// extended80 convierte un número de coma flotante extendido de 80 bits (IEEE
// 754, big-endian), el formato de la frecuencia en AIFF.
func extended80(b []byte) float64 {
	exp := int(binary.BigEndian.Uint16(b[0:]) & 0x7FFF)
	mant := binary.BigEndian.Uint64(b[2:])
	if exp == 0 && mant == 0 {
		return 0
	}
	v := math.Ldexp(float64(mant), exp-16383-63)
	if b[0]&0x80 != 0 {
		v = -v
	}
	return v
}
