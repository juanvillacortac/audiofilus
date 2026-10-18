package decode

import (
	"fmt"
	"unsafe"
)

// Decodificadores de flujos: el audio llega a trozos (una radio por
// internet) y no se puede buscar en él. Feed mete los bytes que van llegando
// y Decode saca lo que se pueda sin esperar: si faltan bytes, devuelve 0 y
// quien lo usa decide (la radio pone silencio). MP3 con el modo "feed" de
// libmpg123 y AAC en ADTS con libfdk-aac, los dos que usan casi todas las
// emisoras.

// StreamDecoder decodifica un flujo que llega a trozos.
type StreamDecoder interface {
	// Feed mete bytes del flujo.
	Feed(b []byte)
	// Decode llena dst con muestras estéreo; 0 si necesita más bytes.
	Decode(dst []float32) (int, error)
	// SampleRate es la frecuencia, 0 hasta decodificar el primer bloque.
	SampleRate() int
	Close()
}

// NewStreamDecoder prepara el decodificador de codec ("mp3" o "aac").
func NewStreamDecoder(codec string) (StreamDecoder, error) {
	switch codec {
	case "mp3":
		return newMP3Stream()
	case "aac":
		return newAACStream()
	}
	return nil, fmt.Errorf("%w (stream codec %q)", ErrUnsupported, codec)
}

// mpgNeedMore es MPG123_NEED_MORE: le faltan bytes.
const mpgNeedMore = -10

type mp3Stream struct {
	mh   uintptr
	rate int
}

func newMP3Stream() (StreamDecoder, error) {
	if err := loadMPG123(); err != nil {
		return nil, errNoLib(err)
	}
	var e int32
	mh := mpg123.new(nil, &e)
	if mh == 0 {
		return nil, fmt.Errorf("mpg123_new failed (%d)", e)
	}
	mpg123.param(mh, mpgAddFlags, mpgForceStereo|mpgQuiet, 0)
	mpg123.none(mh)
	for _, r := range mpg123Rates {
		mpg123.format(mh, r, mpgStereo, mpgFloat32)
	}
	if mpg123.openFeed(mh) != mpgOK {
		err := fmt.Errorf("mpg123_open_feed: %s", mpg123.strerr(mh))
		mpg123.del(mh)
		return nil, err
	}
	return &mp3Stream{mh: mh}, nil
}

func (s *mp3Stream) Feed(b []byte) {
	if len(b) > 0 {
		mpg123.feed(s.mh, unsafe.Pointer(&b[0]), uintptr(len(b)))
	}
}

func (s *mp3Stream) Decode(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	for {
		var done uintptr
		r := mpg123.read(s.mh, unsafe.Pointer(&dst[0]), uintptr(frames*8), &done)
		n := int(done / 4)
		switch r {
		case mpgNewFormat:
			var rate int64
			var ch, enc int32
			if mpg123.getf(s.mh, &rate, &ch, &enc) == mpgOK {
				s.rate = int(rate)
			}
			if n > 0 {
				return n, nil
			}
			continue
		case mpgOK, mpgNeedMore, mpgDone:
			return n, nil
		default:
			// Un trozo dañado (pasa al reconectar): mpg123 se resincroniza solo.
			return n, nil
		}
	}
}

func (s *mp3Stream) SampleRate() int { return s.rate }

func (s *mp3Stream) Close() {
	if s.mh != 0 {
		mpg123.close(s.mh)
		mpg123.del(s.mh)
		s.mh = 0
	}
}

// aacTTADTS es TT_MP4_ADTS: AAC con cabeceras ADTS, como lo mandan las radios.
const aacTTADTS = 2

type aacStream struct {
	h       uintptr
	rate    int
	in      []byte // lo que aún no se pudo meter en el decodificador
	pcm     []int16
	pending []float32
}

func newAACStream() (StreamDecoder, error) {
	if err := loadFDK(); err != nil {
		return nil, errNoLib(err)
	}
	h := fdk.open(aacTTADTS, 1)
	if h == 0 {
		return nil, fmt.Errorf("aacDecoder_Open failed")
	}
	fdk.param(h, aacMaxOutChannel, 2)
	return &aacStream{h: h, pcm: make([]int16, aacMaxFrame)}, nil
}

func (s *aacStream) Feed(b []byte) { s.in = append(s.in, b...) }

// fill mete en el decodificador lo que quepa de s.in.
func (s *aacStream) fill() {
	if len(s.in) == 0 {
		return
	}
	ptr := unsafe.Pointer(&s.in[0])
	size := uint32(len(s.in))
	valid := size
	if fdk.fill(s.h, &ptr, &size, &valid) != 0 {
		s.in = s.in[:0]
		return
	}
	used := len(s.in) - int(valid)
	s.in = append(s.in[:0], s.in[used:]...)
}

func (s *aacStream) Decode(dst []float32) (int, error) {
	for len(s.pending) < len(dst) {
		s.fill()
		r := fdk.decode(s.h, unsafe.Pointer(&s.pcm[0]), int32(len(s.pcm)), 0)
		if r == aacNotEnoughBits {
			if len(s.in) == 0 {
				break // hacen falta más bytes
			}
			continue
		}
		if r != 0 {
			if len(s.in) == 0 {
				break
			}
			continue // un bloque dañado: se salta
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
	}
	n := copy(dst[:len(dst)&^1], s.pending)
	s.pending = append(s.pending[:0], s.pending[n:]...)
	return n, nil
}

func (s *aacStream) SampleRate() int { return s.rate }

func (s *aacStream) Close() {
	if s.h != 0 {
		fdk.close(s.h)
		s.h = 0
	}
}
