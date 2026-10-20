package decode

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

// Los VGM/VGZ con libvgm (ValleyBell; tools/libvgm compila libvgmshim, una
// capa en C sobre ella). libgme solo emula los chips de la Mega Drive y la
// Master System; libvgm emula casi todos los de los VGM: las placas arcade
// (YM2151, SegaPCM, QSound, C140, OKIM6295...), la NES, la Game Boy, los OPL
// y muchos más. Si la biblioteca está, los VGM suenan con ella; si no, con
// libgme (y los de chips que libgme no emula dan error en vez de silencio,
// ver vgmchips.go). Como en libgme, si dan la vuelta se tocan dos vueltas y
// se apagan en sus últimos 8 segundos.

const vgmLoops = 2 // vueltas antes del fundido

// vgmLibNames son los nombres de la biblioteca (los tests añaden delante la
// que compila tools/libvgm en build/).
var vgmLibNames = []string{"libvgmshim.so", "libvgmshim.dll"}

var vgmLib struct {
	once   sync.Once
	err    error
	open   func(path *byte, rate, loops, fadeMs int32) uintptr
	render func(h uintptr, buf unsafe.Pointer, frames int32) int32
	length func(h uintptr) int32
	loops  func(h uintptr) int32
	seek   func(h uintptr, ms int32) int32
	tag    func(h uintptr, key *byte) uintptr
	close  func(h uintptr)
}

func loadVGM() error {
	if nativeDisabled() {
		return errDisabled
	}
	vgmLib.once.Do(func() {
		vgmLib.err = openLib(vgmLibNames, []libFunc{
			{&vgmLib.open, "vgm_open"}, {&vgmLib.render, "vgm_render"},
			{&vgmLib.length, "vgm_length_ms"}, {&vgmLib.loops, "vgm_loops"},
			{&vgmLib.seek, "vgm_seek"}, {&vgmLib.tag, "vgm_tag"}, {&vgmLib.close, "vgm_close"},
		})
	})
	return vgmLib.err
}

// VGMFullChips dice si los VGM se tocan con libvgm (con casi todos los
// chips: los packs de VGMRips de cualquier sistema suenan).
func VGMFullChips() bool { return loadVGM() == nil }

// isVGMFile dice si path es un VGM (o VGZ), por su extensión.
func isVGMFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".vgm" || ext == ".vgz"
}

// vgmOpen abre file con libvgm.
func vgmOpen(file string) (uintptr, error) {
	if err := loadVGM(); err != nil {
		return 0, errNoLib(err)
	}
	h := vgmLib.open(cString(file), gmeRate, vgmLoops, gmeFade)
	if h == 0 {
		return 0, fmt.Errorf("%w: %s: libvgm can't open it", ErrUnsupported, filepath.Base(file))
	}
	return h, nil
}

// vgmTag lee una etiqueta GD3 (la inglesa, o la japonesa si no hay).
func vgmTag(h uintptr, key string) string {
	if s := cGoString(vgmLib.tag(h, cString(key))); s != "" {
		return s
	}
	return cGoString(vgmLib.tag(h, cString(key+"-JPN")))
}

// vgmTracks es la única canción de un VGM, con sus etiquetas, como las de
// libgme (GMETracks).
func vgmTracks(file string) ([]GMETrack, error) {
	h, err := vgmOpen(file)
	if err != nil {
		return nil, err
	}
	defer vgmLib.close(h)
	return []GMETrack{{Song: 1, Title: vgmTag(h, "TITLE"), Game: vgmTag(h, "GAME"), Author: vgmTag(h, "ARTIST"),
		System: vgmTag(h, "SYSTEM"), Length: int(vgmLib.length(h)), Loops: vgmLib.loops(h) != 0}}, nil
}

// vgmSource toca un VGM con libvgm.
type vgmSource struct {
	h      uintptr
	length int64 // cuadros (con las vueltas y el fundido)
	pos    int64
	buf    []int16
}

// newVGM abre file con libvgm.
func newVGM(file string) (Source, error) {
	h, err := vgmOpen(file)
	if err != nil {
		return nil, err
	}
	return &vgmSource{h: h, length: int64(vgmLib.length(h)) * gmeRate / 1000}, nil
}

func (s *vgmSource) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	if cap(s.buf) < frames*2 {
		s.buf = make([]int16, frames*2)
	}
	buf := s.buf[:frames*2]
	got := int(vgmLib.render(s.h, unsafe.Pointer(&buf[0]), int32(frames)))
	if got <= 0 {
		return 0, io.EOF
	}
	for i, v := range buf[:got*2] {
		dst[i] = float32(v) / 32768
	}
	s.pos += int64(got)
	return got * 2, nil
}

func (s *vgmSource) SampleRate() int { return gmeRate }
func (s *vgmSource) Length() int64   { return s.length }
func (s *vgmSource) Position() int64 { return s.pos }

// SeekFrame salta a frame (libvgm emula hasta ahí: hacia atrás, desde el
// principio).
func (s *vgmSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.length)
	if vgmLib.seek(s.h, int32(frame*1000/gmeRate)) != 0 {
		return fmt.Errorf("libvgm: seek failed")
	}
	s.pos = frame
	return nil
}

func (s *vgmSource) Close() error {
	if s.h != 0 {
		vgmLib.close(s.h)
		s.h = 0
	}
	return nil
}
