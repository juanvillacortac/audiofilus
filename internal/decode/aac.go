package decode

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"
)

// AAC dentro de MP4/M4A: mp4.go saca los bloques y libfdk-aac (ver native.go)
// los decodifica uno a uno, mezclado a estéreo de 16 bits.

const (
	aacTTRaw         = 0      // TT_MP4_RAW: bloques sueltos, como vienen en MP4
	aacMaxOutChannel = 0x0012 // AAC_PCM_MAX_OUTPUT_CHANNELS
	aacDecIntr       = 4      // AACDEC_INTR: hubo un salto
	aacDecClrHist    = 8      // AACDEC_CLRHIST: olvidar lo anterior
	aacNotEnoughBits = 0x1002
	aacMaxFrame      = 4096 * 8 // muestras de sobra para cualquier bloque
)

var fdk struct {
	once   sync.Once
	err    error
	open   func(transport int32, layers uint32) uintptr
	config func(h uintptr, conf *unsafe.Pointer, length *uint32) int32
	param  func(h uintptr, param int32, value int32) int32
	fill   func(h uintptr, buf *unsafe.Pointer, size *uint32, valid *uint32) int32
	decode func(h uintptr, pcm unsafe.Pointer, size int32, flags uint32) int32
	info   func(h uintptr) unsafe.Pointer
	close  func(h uintptr)
}

func loadFDK() error {
	if nativeDisabled() {
		return errDisabled
	}
	fdk.once.Do(func() {
		fdk.err = openLib([]string{"libfdk-aac.so.2", "libfdk-aac.so"}, []libFunc{
			{&fdk.open, "aacDecoder_Open"}, {&fdk.config, "aacDecoder_ConfigRaw"},
			{&fdk.param, "aacDecoder_SetParam"}, {&fdk.fill, "aacDecoder_Fill"},
			{&fdk.decode, "aacDecoder_DecodeFrame"}, {&fdk.info, "aacDecoder_GetStreamInfo"},
			{&fdk.close, "aacDecoder_Close"},
		})
	})
	return fdk.err
}

// aacSource decodifica la pista AAC de un MP4.
type aacSource struct {
	f       *os.File
	trk     *mp4Track
	h       uintptr
	next    int   // la próxima muestra (bloque) a decodificar
	rate    int   // frecuencia de salida (con HE-AAC, el doble que la del núcleo)
	pos     int64 // frames entregados
	skip    int64 // frames a tirar tras un salto, hasta la posición pedida
	flags   uint32
	in      []byte
	pcm     []int16
	pending []float32 // lo decodificado que aún no se entregó
}

func newAAC(f *os.File, trk *mp4Track) (Source, error) {
	if err := loadFDK(); err != nil {
		return nil, errNoLib(err)
	}
	h := fdk.open(aacTTRaw, 1)
	if h == 0 {
		return nil, fmt.Errorf("aacDecoder_Open failed")
	}
	conf := unsafe.Pointer(&trk.config[0])
	n := uint32(len(trk.config))
	if r := fdk.config(h, &conf, &n); r != 0 {
		fdk.close(h)
		return nil, fmt.Errorf("aacDecoder_ConfigRaw: error %#x", r)
	}
	fdk.param(h, aacMaxOutChannel, 2)
	s := &aacSource{f: f, trk: trk, h: h, pcm: make([]int16, aacMaxFrame)}
	// La frecuencia de salida se sabe al decodificar el primer bloque (con
	// HE-AAC es el doble de la que dice el MP4).
	if err := s.decodeNext(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// decodeNext decodifica el siguiente bloque y lo deja en pending.
func (s *aacSource) decodeNext() error {
	for s.next < s.trk.samples() {
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
		ptr := unsafe.Pointer(&in[0])
		valid := size
		if r := fdk.fill(s.h, &ptr, &size, &valid); r != 0 {
			return fmt.Errorf("aacDecoder_Fill: error %#x", r)
		}
		r := fdk.decode(s.h, unsafe.Pointer(&s.pcm[0]), int32(len(s.pcm)), s.flags)
		s.flags = 0
		if r == aacNotEnoughBits {
			continue
		}
		if r != 0 {
			continue // un bloque dañado: se salta, como hacen los reproductores
		}
		info := (*[3]int32)(fdk.info(s.h)) // sampleRate, frameSize, numChannels
		rate, frames, ch := int(info[0]), int(info[1]), int(info[2])
		if s.rate == 0 {
			s.rate = rate
		}
		for k := 0; k < frames; k++ {
			l := float32(s.pcm[k*ch]) / 32768
			r := l
			if ch > 1 {
				r = float32(s.pcm[k*ch+1]) / 32768
			}
			s.pending = append(s.pending, l, r)
		}
		return nil
	}
	return io.EOF
}

func (s *aacSource) Read(dst []float32) (int, error) {
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
	if len(s.pending) == 0 {
		s.pending = s.pending[:0:0]
	}
	s.pos += int64(n / 2)
	return n, nil
}

func (s *aacSource) SampleRate() int { return s.rate }

func (s *aacSource) Length() int64 {
	return s.trk.duration() * int64(s.rate) / s.trk.timescale
}

func (s *aacSource) Position() int64 { return s.pos }

func (s *aacSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	tm := frame * s.trk.timescale / int64(s.rate)
	i := s.trk.sampleAt(tm)
	start := s.trk.starts[i] * int64(s.rate) / s.trk.timescale
	s.next, s.pending, s.skip, s.pos = i, nil, frame-start, frame
	s.flags = aacDecIntr | aacDecClrHist
	return nil
}

func (s *aacSource) Close() error {
	if s.h != 0 {
		fdk.close(s.h)
		s.h = 0
	}
	return s.f.Close()
}

// isMP4 reconoce un MP4/M4A por su caja ftyp.
func isMP4(magic []byte) bool {
	return len(magic) >= 8 && string(magic[4:8]) == "ftyp"
}

// newM4A abre un MP4 de audio y elige el decodificador por el códec.
func newM4A(f *os.File) (Source, error) {
	trk, err := openMP4(f)
	if err != nil {
		return nil, err
	}
	switch trk.codec {
	case "mp4a":
		return newAAC(f, trk)
	case "alac":
		return newALAC(f, trk)
	}
	return nil, fmt.Errorf("%w (MP4 codec %q)", ErrUnsupported, trk.codec)
}
