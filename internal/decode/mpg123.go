package decode

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"
)

// MP3 con libmpg123 (ver native.go). Es el decodificador de referencia, en C
// con ensamblador para ARM: en la RG34XX-SP gasta ~0,6 % de un núcleo, frente al
// ~18 % de go-mp3, que queda como respaldo.

// Constantes de mpg123.h y fmt123.h.
const (
	mpgOK          = 0
	mpgDone        = -12
	mpgNewFormat   = -11
	mpgAddFlags    = 2     // MPG123_ADD_FLAGS
	mpgForceStereo = 0x8   // MPG123_FORCE_STEREO
	mpgQuiet       = 0x20  // MPG123_QUIET
	mpgGapless     = 0x40  // MPG123_GAPLESS
	mpgStereo      = 2     // MPG123_STEREO
	mpgFloat32     = 0x200 // MPG123_ENC_FLOAT_32
)

// mpg123 son las funciones de la biblioteca. Las de off_t (open_fd, seek, tell,
// length) son de 64 bits en arm64 y amd64, los únicos destinos.
var mpg123 struct {
	once   sync.Once
	err    error
	init   func() int32
	new    func(decoder *byte, err *int32) uintptr
	del    func(mh uintptr)
	param  func(mh uintptr, typ int32, value int64, fvalue float64) int32
	none   func(mh uintptr) int32
	format func(mh uintptr, rate int64, channels, encodings int32) int32
	getf   func(mh uintptr, rate *int64, channels, encoding *int32) int32
	open   func(mh uintptr, fd int32) int32
	close  func(mh uintptr) int32
	read   func(mh uintptr, out unsafe.Pointer, size uintptr, done *uintptr) int32
	seek   func(mh uintptr, off int64, whence int32) int64
	tell   func(mh uintptr) int64
	length func(mh uintptr) int64
	strerr func(mh uintptr) string
	// Para los flujos (stream.go): abrir sin archivo y darle los bytes.
	openFeed func(mh uintptr) int32
	feed     func(mh uintptr, in unsafe.Pointer, size uintptr) int32
}

// loadMPG123 abre libmpg123 una sola vez.
func loadMPG123() error {
	if nativeDisabled() {
		return errDisabled
	}
	mpg123.once.Do(func() {
		mpg123.err = openLib([]string{"libmpg123.so.0", "libmpg123.so", "libmpg123-0.dll"}, []libFunc{
			{&mpg123.init, "mpg123_init"}, {&mpg123.new, "mpg123_new"}, {&mpg123.del, "mpg123_delete"},
			{&mpg123.param, "mpg123_param"}, {&mpg123.none, "mpg123_format_none"}, {&mpg123.format, "mpg123_format"},
			{&mpg123.getf, "mpg123_getformat"}, {&mpg123.open, "mpg123_open_fd"}, {&mpg123.close, "mpg123_close"},
			{&mpg123.read, "mpg123_read"}, {&mpg123.seek, "mpg123_seek"}, {&mpg123.tell, "mpg123_tell"},
			{&mpg123.length, "mpg123_length"}, {&mpg123.strerr, "mpg123_strerror"},
			{&mpg123.openFeed, "mpg123_open_feed"}, {&mpg123.feed, "mpg123_feed"},
		})
		if mpg123.err == nil {
			mpg123.init()
		}
	})
	return mpg123.err
}

// mpg123Rates son las frecuencias de MP3 que se aceptan (todas, en estéreo float).
var mpg123Rates = []int64{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000}

// mpg123Source decodifica un MP3 con libmpg123.
type mpg123Source struct {
	f    *os.File
	mh   uintptr
	rate int
}

// newMPG123 abre f con libmpg123; ErrUnsupported si la biblioteca no está.
func newMPG123(f *os.File) (Source, error) {
	if err := loadMPG123(); err != nil {
		return nil, errNoLib(err)
	}
	var e int32
	mh := mpg123.new(nil, &e)
	if mh == 0 {
		return nil, fmt.Errorf("mpg123_new failed (%d)", e)
	}
	s := &mpg123Source{f: f, mh: mh}
	fail := func(what string) (Source, error) {
		err := fmt.Errorf("%s: %s", what, mpg123.strerr(mh))
		mpg123.del(mh)
		return nil, err
	}
	mpg123.param(mh, mpgAddFlags, mpgForceStereo|mpgQuiet|mpgGapless, 0)
	mpg123.none(mh)
	for _, r := range mpg123Rates {
		mpg123.format(mh, r, mpgStereo, mpgFloat32)
	}
	if mpg123.open(mh, int32(f.Fd())) != mpgOK {
		return fail("mpg123_open_fd")
	}
	var rate int64
	var ch, enc int32
	if mpg123.getf(mh, &rate, &ch, &enc) != mpgOK {
		mpg123.close(mh)
		return fail("mpg123_getformat")
	}
	if ch != 2 || enc != mpgFloat32 {
		mpg123.close(mh)
		mpg123.del(mh)
		return nil, fmt.Errorf("mpg123: unexpected output format (%d channels, encoding %#x)", ch, enc)
	}
	s.rate = int(rate)
	return s, nil
}

func (s *mpg123Source) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	for {
		var done uintptr
		r := mpg123.read(s.mh, unsafe.Pointer(&dst[0]), uintptr(frames*8), &done)
		n := int(done / 4)
		switch r {
		case mpgOK:
			return n, nil
		case mpgDone:
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		case mpgNewFormat:
			if n > 0 {
				return n, nil
			}
			continue // el formato ya está fijado (estéreo float): se sigue
		default:
			if n > 0 {
				return n, nil
			}
			return 0, fmt.Errorf("mpg123_read: %s", mpg123.strerr(s.mh))
		}
	}
}

func (s *mpg123Source) SampleRate() int { return s.rate }

func (s *mpg123Source) Length() int64 {
	if n := mpg123.length(s.mh); n > 0 {
		return n
	}
	return -1
}

func (s *mpg123Source) Position() int64 { return max(0, mpg123.tell(s.mh)) }

func (s *mpg123Source) SeekFrame(frame int64) error {
	if l := s.Length(); l > 0 {
		frame = min(frame, l)
	}
	if mpg123.seek(s.mh, max(0, frame), io.SeekStart) < 0 {
		return fmt.Errorf("mpg123_seek: %s", mpg123.strerr(s.mh))
	}
	return nil
}

func (s *mpg123Source) Close() error {
	if s.mh != 0 {
		mpg123.close(s.mh)
		mpg123.del(s.mh)
		s.mh = 0
	}
	return s.f.Close()
}
