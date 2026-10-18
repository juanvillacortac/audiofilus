package decode

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"
)

// Opus (.opus, o Opus dentro de .ogg) con libopusfile (ver native.go). Opus
// siempre decodifica a 48 kHz.

const opHole = -3 // OP_HOLE: un hueco en los datos; se sigue leyendo

var opusfile struct {
	once  sync.Once
	err   error
	open  func(path *byte, err *int32) uintptr
	read  func(of uintptr, pcm unsafe.Pointer, size int32) int32
	total func(of uintptr, li int32) int64
	seek  func(of uintptr, off int64) int32
	tell  func(of uintptr) int64
	free  func(of uintptr)
}

func loadOpusfile() error {
	if nativeDisabled() {
		return errDisabled
	}
	opusfile.once.Do(func() {
		opusfile.err = openLib([]string{"libopusfile.so.0", "libopusfile.so"}, []libFunc{
			{&opusfile.open, "op_open_file"}, {&opusfile.read, "op_read_float_stereo"},
			{&opusfile.total, "op_pcm_total"}, {&opusfile.seek, "op_pcm_seek"},
			{&opusfile.tell, "op_pcm_tell"}, {&opusfile.free, "op_free"},
		})
	})
	return opusfile.err
}

// opusSource decodifica Opus con libopusfile.
type opusSource struct {
	of     uintptr
	length int64
}

// newOpus abre path (f se cierra: libopusfile abre el archivo por su cuenta).
func newOpus(f *os.File, path string) (Source, error) {
	f.Close()
	if err := loadOpusfile(); err != nil {
		return nil, errNoLib(err)
	}
	var e int32
	of := opusfile.open(cString(path), &e)
	if of == 0 {
		return nil, fmt.Errorf("op_open_file failed (%d)", e)
	}
	s := &opusSource{of: of, length: opusfile.total(of, -1)}
	if s.length < 0 {
		s.length = -1
	}
	return s, nil
}

func (s *opusSource) Read(dst []float32) (int, error) {
	if len(dst) < 2 {
		return 0, nil
	}
	for {
		n := opusfile.read(s.of, unsafe.Pointer(&dst[0]), int32(len(dst)&^1))
		switch {
		case n > 0:
			return int(n) * 2, nil
		case n == 0:
			return 0, io.EOF
		case n == opHole:
			continue
		default:
			return 0, fmt.Errorf("op_read_float_stereo: error %d", n)
		}
	}
}

func (s *opusSource) SampleRate() int { return 48000 }
func (s *opusSource) Length() int64   { return s.length }
func (s *opusSource) Position() int64 { return max(0, opusfile.tell(s.of)) }

func (s *opusSource) SeekFrame(frame int64) error {
	if r := opusfile.seek(s.of, clampFrame(frame, s.length)); r < 0 {
		return fmt.Errorf("op_pcm_seek: error %d", r)
	}
	return nil
}

func (s *opusSource) Close() error {
	if s.of != 0 {
		opusfile.free(s.of)
		s.of = 0
	}
	return nil
}
