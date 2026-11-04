package decode

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unsafe"
)

// WMA (y lo que se añada a la compilación) con FFmpeg: tools/ffmpeg compila
// libavshim, un FFmpeg recortado con una capa en C encima (shim.c) que abre,
// decodifica a estéreo float32, dice cuánto dura y salta. Go no toca
// ninguna estructura de FFmpeg: todo eso queda en la capa en C, compilada
// contra la versión que se lleva. Si la biblioteca falta, esos archivos no
// se pueden abrir (ErrUnsupported), y sus etiquetas se leen igual (asf.go en
// internal/library).

// avLibNames son los nombres de la biblioteca (los tests añaden delante la
// que compila tools/ffmpeg en build/).
var avLibNames = []string{"libavshim.so", "libavshim.dll"}

var avLib struct {
	once  sync.Once
	err   error
	open  func(path *byte, rate *int32, frames *int64) uintptr
	read  func(h uintptr, out unsafe.Pointer, frames int32) int32
	seek  func(h uintptr, frame int64) int32
	close func(h uintptr)
}

func loadAV() error {
	if nativeDisabled() {
		return errDisabled
	}
	avLib.once.Do(func() {
		avLib.err = openLib(avLibNames, []libFunc{
			{&avLib.open, "afav_open"}, {&avLib.read, "afav_read"},
			{&avLib.seek, "afav_seek"}, {&avLib.close, "afav_close"},
		})
	})
	return avLib.err
}

// asfMagic es el GUID con que empieza un archivo ASF (WMA, WMV).
var asfMagic = []byte{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11, 0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C}

// isASF reconoce un archivo ASF por sus primeros bytes.
func isASF(b []byte) bool { return bytes.HasPrefix(b, asfMagic) }

// avSource decodifica un archivo con libavshim.
type avSource struct {
	h      uintptr
	rate   int
	length int64 // cuadros, o -1
	pos    int64
}

// newAV abre path con libavshim. f solo sirvió para reconocerlo: FFmpeg lo
// abre por su cuenta, así que se cierra.
func newAV(f *os.File, path string) (Source, error) {
	if err := loadAV(); err != nil {
		return nil, errNoLib(err)
	}
	var rate int32
	var frames int64
	h := avLib.open(cString(path), &rate, &frames)
	if h == 0 {
		return nil, fmt.Errorf("%w: %s: ffmpeg can't decode it", ErrUnsupported, filepath.Base(path))
	}
	f.Close()
	return &avSource{h: h, rate: int(rate), length: frames}, nil
}

func (s *avSource) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	got := int(avLib.read(s.h, unsafe.Pointer(&dst[0]), int32(frames)))
	switch {
	case got < 0:
		return 0, fmt.Errorf("ffmpeg: decoding failed")
	case got == 0:
		return 0, io.EOF
	}
	s.pos += int64(got)
	return got * 2, nil
}

func (s *avSource) SampleRate() int { return s.rate }
func (s *avSource) Length() int64   { return s.length }
func (s *avSource) Position() int64 { return s.pos }

// SeekFrame salta a frame (FFmpeg va al paquete anterior y descarta hasta
// ahí).
func (s *avSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.length)
	if avLib.seek(s.h, frame) != 0 {
		return fmt.Errorf("ffmpeg: seek to %d failed", frame)
	}
	s.pos = frame
	return nil
}

func (s *avSource) Close() error {
	if s.h != 0 {
		avLib.close(s.h)
		s.h = 0
	}
	return nil
}
