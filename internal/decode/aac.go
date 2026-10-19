package decode

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"
)

// AAC dentro de MP4/M4A: mp4.go saca los bloques y un aacCodec los
// decodifica uno a uno: libfdk-aac (ver native.go) donde esté; si no, el
// del sistema, si lo hay (en iOS, el AudioConverter de AudioToolbox:
// aac_ios.go). Sale mezclado a estéreo.

// aacCodec decodifica bloques AAC sueltos (como vienen en un MP4).
type aacCodec interface {
	// decode decodifica un bloque y añade lo que salga a out (estéreo
	// intercalado); rate es la frecuencia de salida (0 si aún no salió nada).
	decode(packet []byte, out []float32) ([]float32, int, error)
	reset() // tras un salto: olvidar lo anterior
	close()
}

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
		fdk.err = openLib([]string{"libfdk-aac.so.2", "libfdk-aac.so", "libfdk-aac-2.dll"}, []libFunc{
			{&fdk.open, "aacDecoder_Open"}, {&fdk.config, "aacDecoder_ConfigRaw"},
			{&fdk.param, "aacDecoder_SetParam"}, {&fdk.fill, "aacDecoder_Fill"},
			{&fdk.decode, "aacDecoder_DecodeFrame"}, {&fdk.info, "aacDecoder_GetStreamInfo"},
			{&fdk.close, "aacDecoder_Close"},
		})
	})
	return fdk.err
}

// fdkCodec es aacCodec con libfdk-aac.
type fdkCodec struct {
	h     uintptr
	flags uint32
	pcm   []int16
}

// newFDK prepara libfdk-aac para la configuración (AudioSpecificConfig) de
// la pista.
func newFDK(config []byte) (*fdkCodec, error) {
	if err := loadFDK(); err != nil {
		return nil, err
	}
	h := fdk.open(aacTTRaw, 1)
	if h == 0 {
		return nil, fmt.Errorf("aacDecoder_Open failed")
	}
	conf := unsafe.Pointer(&config[0])
	n := uint32(len(config))
	if r := fdk.config(h, &conf, &n); r != 0 {
		fdk.close(h)
		return nil, fmt.Errorf("aacDecoder_ConfigRaw: error %#x", r)
	}
	fdk.param(h, aacMaxOutChannel, 2)
	return &fdkCodec{h: h, pcm: make([]int16, aacMaxFrame)}, nil
}

func (c *fdkCodec) decode(in []byte, out []float32) ([]float32, int, error) {
	ptr := unsafe.Pointer(&in[0])
	size := uint32(len(in))
	valid := size
	if r := fdk.fill(c.h, &ptr, &size, &valid); r != 0 {
		return out, 0, fmt.Errorf("aacDecoder_Fill: error %#x", r)
	}
	r := fdk.decode(c.h, unsafe.Pointer(&c.pcm[0]), int32(len(c.pcm)), c.flags)
	c.flags = 0
	if r != 0 {
		return out, 0, nil // le faltan datos, o un bloque dañado: se salta, como hacen los reproductores
	}
	info := (*[3]int32)(fdk.info(c.h)) // sampleRate, frameSize, numChannels
	rate, frames, ch := int(info[0]), int(info[1]), int(info[2])
	for k := 0; k < frames; k++ {
		l := float32(c.pcm[k*ch]) / 32768
		r := l
		if ch > 1 {
			r = float32(c.pcm[k*ch+1]) / 32768
		}
		out = append(out, l, r)
	}
	return out, rate, nil
}

func (c *fdkCodec) reset() { c.flags = aacDecIntr | aacDecClrHist }

func (c *fdkCodec) close() {
	if c.h != 0 {
		fdk.close(c.h)
		c.h = 0
	}
}

// aacSource decodifica la pista AAC de un MP4.
type aacSource struct {
	f       *os.File
	trk     *mp4Track
	codec   aacCodec
	next    int   // la próxima muestra (bloque) a decodificar
	rate    int   // frecuencia de salida (con HE-AAC, el doble que la del núcleo)
	pos     int64 // frames entregados
	skip    int64 // frames a tirar tras un salto, hasta la posición pedida
	in      []byte
	pending []float32 // lo decodificado que aún no se entregó
}

func newAAC(f *os.File, trk *mp4Track) (Source, error) {
	var codec aacCodec
	fc, err := newFDK(trk.config)
	if err == nil {
		codec = fc
	} else if pc, perr := newPlatformAAC(trk.config); perr == nil {
		codec = pc // sin libfdk-aac, el del sistema
	} else {
		return nil, errNoLib(err)
	}
	s := &aacSource{f: f, trk: trk, codec: codec}
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
		before := len(s.pending)
		var rate int
		var err error
		s.pending, rate, err = s.codec.decode(in, s.pending)
		if err != nil {
			return err
		}
		if s.rate == 0 && rate > 0 {
			s.rate = rate
		}
		if len(s.pending) == before {
			continue // aún no salió nada (le faltaban datos, o se saltó)
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
	s.codec.reset()
	return nil
}

func (s *aacSource) Close() error {
	s.codec.close()
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

// esdsCookie envuelve la AudioSpecificConfig en un descriptor ES de MP4, la
// "magic cookie" que AudioToolbox espera para AAC.
func esdsCookie(asc []byte) []byte {
	descr := func(tag byte, size int) []byte {
		return []byte{tag, byte(size>>21) | 0x80, byte(size>>14) | 0x80, byte(size>>7) | 0x80, byte(size & 0x7f)}
	}
	var b []byte
	b = append(b, descr(0x03, 3+5+13+5+len(asc))...)
	b = append(b, 0, 0, 0)                                     // ES_ID y sin banderas
	b = append(b, descr(0x04, 13+5+len(asc))...)               // DecoderConfigDescriptor
	b = append(b, 0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0) // AAC, audio, sin búfer ni bitrate
	b = append(b, descr(0x05, len(asc))...)                    // DecoderSpecificInfo
	return append(b, asc...)
}
