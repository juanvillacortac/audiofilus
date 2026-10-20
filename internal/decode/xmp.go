package decode

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

// Módulos tracker (MOD, XM, IT, S3M y los demás que entiende libxmp) con
// libxmp (ver native.go). Un módulo no se decodifica sino que se "toca": libxmp
// mezcla sus canales a 48 kHz, en estéreo de 16 bits.

// ModuleExtensions son las extensiones de módulos tracker (no tienen una firma
// común al principio del archivo, así que se reconocen por la extensión).
var ModuleExtensions = []string{".mod", ".xm", ".it", ".s3m", ".mptm", ".669", ".med", ".mmd0", ".mmd1", ".mmd2", ".mmd3", ".mtm", ".stm", ".far", ".ult"}

const (
	xmpRate = 48000
	// xmpInfoSize basta para struct xmp_frame_info (con sus 64 canales).
	xmpInfoSize = 8 << 10
	// Desplazamientos de time y total_time (ms) en xmp_frame_info.
	xmpInfoTime  = 7 * 4
	xmpInfoTotal = 8 * 4
)

var xmp struct {
	once    sync.Once
	err     error
	create  func() uintptr
	free    func(ctx uintptr)
	load    func(ctx uintptr, path *byte) int32
	release func(ctx uintptr)
	start   func(ctx uintptr, rate, format int32) int32
	play    func(ctx uintptr, buf unsafe.Pointer, size, loop int32) int32
	info    func(ctx uintptr, info unsafe.Pointer)
	end     func(ctx uintptr)
	seek    func(ctx uintptr, ms int32) int32
}

func loadXMP() error {
	if nativeDisabled() {
		return errDisabled
	}
	xmp.once.Do(func() {
		xmp.err = openLib([]string{"libxmp.so.4", "libxmp.so", "libxmp-4.dll", "libxmp.dll"}, []libFunc{
			{&xmp.create, "xmp_create_context"}, {&xmp.free, "xmp_free_context"},
			{&xmp.load, "xmp_load_module"}, {&xmp.release, "xmp_release_module"},
			{&xmp.start, "xmp_start_player"}, {&xmp.play, "xmp_play_buffer"},
			{&xmp.info, "xmp_get_frame_info"}, {&xmp.end, "xmp_end_player"},
			{&xmp.seek, "xmp_seek_time"},
		})
	})
	return xmp.err
}

// isModule dice si path es un módulo tracker, por su extensión.
func isModule(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range ModuleExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// xmpSource toca un módulo con libxmp.
type xmpSource struct {
	ctx    uintptr
	length int64
	pos    int64
	buf    []int16
	ended  bool
}

// newXMP abre path (f se cierra: libxmp lee el archivo por su cuenta).
func newXMP(f *os.File, path string) (Source, error) {
	f.Close()
	if err := loadXMP(); err != nil {
		return nil, errNoLib(err)
	}
	ctx := xmp.create()
	if ctx == 0 {
		return nil, fmt.Errorf("xmp_create_context failed")
	}
	if r := xmp.load(ctx, cString(path)); r != 0 {
		xmp.free(ctx)
		return nil, fmt.Errorf("%w (libxmp error %d)", ErrUnsupported, -r)
	}
	if r := xmp.start(ctx, xmpRate, 0); r != 0 { // 0: estéreo, 16 bits con signo
		xmp.release(ctx)
		xmp.free(ctx)
		return nil, fmt.Errorf("xmp_start_player: error %d", -r)
	}
	s := &xmpSource{ctx: ctx, length: -1}
	if total := s.infoInt(xmpInfoTotal); total > 0 {
		s.length = int64(total) * xmpRate / 1000
	}
	return s, nil
}

// infoInt lee un int de xmp_frame_info.
func (s *xmpSource) infoInt(off int) int32 {
	info := make([]byte, xmpInfoSize)
	xmp.info(s.ctx, unsafe.Pointer(&info[0]))
	return int32(binary.NativeEndian.Uint32(info[off:]))
}

func (s *xmpSource) Read(dst []float32) (int, error) {
	n := len(dst) &^ 1
	if n == 0 {
		return 0, nil
	}
	if s.ended {
		return 0, io.EOF
	}
	if cap(s.buf) < n {
		s.buf = make([]int16, n)
	}
	buf := s.buf[:n]
	// loop 1: se toca una vez y se acaba (sin él, los módulos con bucle no
	// terminan nunca).
	if r := xmp.play(s.ctx, unsafe.Pointer(&buf[0]), int32(n*2), 1); r != 0 {
		s.ended = true
		return 0, io.EOF
	}
	for i, v := range buf {
		dst[i] = float32(v) / 32768
	}
	s.pos += int64(n / 2)
	return n, nil
}

func (s *xmpSource) SampleRate() int { return xmpRate }
func (s *xmpSource) Length() int64   { return s.length }
func (s *xmpSource) Position() int64 { return s.pos }

func (s *xmpSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.length)
	if r := xmp.seek(s.ctx, int32(frame*1000/xmpRate)); r < 0 {
		return fmt.Errorf("xmp_seek_time: error %d", -r)
	}
	s.pos, s.ended = frame, false
	return nil
}

func (s *xmpSource) Close() error {
	if s.ctx != 0 {
		xmp.end(s.ctx)
		xmp.release(s.ctx)
		xmp.free(s.ctx)
		s.ctx = 0
	}
	return nil
}
