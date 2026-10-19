package decode

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

// flacSource decodifica FLAC con mewkiz/flac (Go puro). La librería solo lee
// los metadatos: los bloques se leen con un lector propio (r) para poder
// saltar sin su Stream.Seek, que en archivos sin SEEKTABLE decodifica el
// archivo entero para construirla (~5 s en la consola: el servicio no
// respondía mientras, y al arrancar no llegaba a abrir su socket).
type flacSource struct {
	f      *os.File
	stream *flac.Stream
	r      *bufio.Reader
	// Dónde empieza el primer bloque de audio y cuánto mide el archivo.
	dataStart, size int64
	scale           float32
	// Bloque FLAC ya decodificado que aún no se entregó, en float32 estéreo.
	pending []float32
	pos     int64
}

func newFLAC(f *os.File) (Source, error) {
	stream, err := flac.NewSeek(f)
	if err != nil {
		return nil, err
	}
	start, err := flacDataStart(f)
	if err != nil {
		stream.Close()
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		stream.Close()
		return nil, err
	}
	s := &flacSource{
		f:         f,
		stream:    stream,
		r:         bufio.NewReaderSize(f, 64<<10),
		dataStart: start,
		size:      st.Size(),
		scale:     1 / float32(int64(1)<<(stream.Info.BitsPerSample-1)),
	}
	if err := s.seekByte(start); err != nil {
		stream.Close()
		return nil, err
	}
	return s, nil
}

// flacDataStart lee la cabecera del archivo (un ID3v2 opcional, "fLaC" y los
// bloques de metadatos) y devuelve dónde empieza el audio.
func flacDataStart(f *os.File) (int64, error) {
	var hdr [10]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return 0, err
	}
	off := int64(0)
	if string(hdr[:3]) == "ID3" {
		size := int64(hdr[6])<<21 | int64(hdr[7])<<14 | int64(hdr[8])<<7 | int64(hdr[9])
		off = 10 + size
		if hdr[5]&0x10 != 0 {
			off += 10 // pie del ID3v2
		}
	}
	var sig [4]byte
	if _, err := f.ReadAt(sig[:], off); err != nil {
		return 0, err
	}
	if string(sig[:]) != "fLaC" {
		return 0, errors.New("flac: signature not found")
	}
	off += 4
	for {
		var b [4]byte
		if _, err := f.ReadAt(b[:], off); err != nil {
			return 0, err
		}
		off += 4 + int64(binary.BigEndian.Uint32(b[:])&0xFFFFFF)
		if b[0]&0x80 != 0 {
			return off, nil // era el último bloque de metadatos
		}
	}
}

// seekByte pone el lector en el byte off del archivo.
func (s *flacSource) seekByte(off int64) error {
	if _, err := s.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	s.r.Reset(s.f)
	return nil
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
	fr, err := frame.Parse(s.r)
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

// flacNear es lo bastante cerca del destino para dejar de buscar y
// decodificar lo que falta (en muestras: ~1 s a 44,1 kHz).
const flacNear = 1 << 16

func (s *flacSource) SeekFrame(target int64) error {
	target = clampFrame(target, s.Length())
	// Se busca por bisección en bytes el último bloque que empieza antes del
	// destino (leyendo solo cabeceras), y desde ahí se decodifica.
	best, bestAt := s.dataStart, int64(0)
	if n := s.Length(); n > 0 && target >= flacNear {
		lo, hi := s.dataStart, s.size
		guess := s.dataStart + int64(float64(target)/float64(n)*float64(s.size-s.dataStart))
		for i := 0; i < 40 && hi-lo > 4096; i++ {
			guess = min(max(guess, lo), hi-1)
			off, at, ok := s.frameAfter(guess, hi)
			switch {
			case !ok || at > target:
				hi = guess
			default:
				best, bestAt = off, at
				lo = off + 1
				if target-at < flacNear {
					hi = lo // ya está cerca
				}
			}
			guess = lo + (hi-lo)/2
		}
	}
	if err := s.seekByte(best); err != nil {
		return err
	}
	s.pending = s.pending[:0]
	s.pos = bestAt
	// Se decodifica y descarta hasta quedar justo en target.
	for s.pos < target {
		if err := s.decodeFrame(); err != nil {
			return err
		}
		skip := int(target-s.pos) * 2
		if skip >= len(s.pending) {
			s.pos += int64(len(s.pending) / 2)
			s.pending = s.pending[:0]
			continue
		}
		s.pending = s.pending[skip:]
		s.pos = target
	}
	return nil
}

// frameAfter busca la primera cabecera de bloque válida desde el byte from
// (sin pasar de limit) y devuelve dónde está y su primera muestra.
func (s *flacSource) frameAfter(from, limit int64) (off, at int64, ok bool) {
	info := s.stream.Info
	buf := make([]byte, 32<<10)
	for from < limit {
		n, err := s.f.ReadAt(buf, from)
		if n < 16 {
			return 0, 0, false
		}
		for i := 0; i+16 <= n; i++ {
			// El código de sincronía: 14 bits a 1 y un 0 (0xFFF8 o 0xFFF9).
			if buf[i] != 0xFF || buf[i+1]&0xFE != 0xF8 {
				continue
			}
			fr, err := frame.New(bytes.NewReader(buf[i:n]))
			if err != nil || int(fr.Channels.Count()) != int(info.NChannels) ||
				(fr.SampleRate != 0 && fr.SampleRate != info.SampleRate) ||
				(fr.BitsPerSample != 0 && fr.BitsPerSample != info.BitsPerSample) {
				continue
			}
			first := int64(fr.Num)
			if fr.HasFixedBlockSize {
				first *= int64(info.BlockSizeMax)
			}
			if info.NSamples > 0 && first >= int64(info.NSamples) {
				continue
			}
			return from + int64(i), first, true
		}
		if err != nil {
			return 0, 0, false
		}
		from += int64(n - 15) // con solape, por si la cabecera quedó cortada
	}
	return 0, 0, false
}

func (s *flacSource) Close() error {
	s.stream.Close()
	return s.f.Close()
}
