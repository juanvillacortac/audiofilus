package decode

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// La música de consolas viejas con libgme (Game_Music_Emu, ver native.go):
// NSF/NSFE (NES), SPC (Super Nintendo), GBS (Game Boy), VGM/VGZ y GYM
// (Mega Drive, Master System y arcades), HES (PC Engine), KSS (MSX), AY
// (Spectrum) y SAP (Atari). No son grabaciones: llevan el código y los datos
// del chip de sonido, y libgme los emula. Un archivo puede tener muchas
// canciones (un NSF trae la banda sonora entera): cada una es una pista con
// su propia ruta, "juego.nsf#song=3" (ver GMEPath). Muchas no terminan nunca
// (dan la vuelta): duran lo que dice el archivo o, si no lo dice, lo que
// libgme propone (2,5 minutos), y se apagan en sus últimos 8 segundos.

// GMEExtensions son las extensiones de libgme.
var GMEExtensions = []string{".nsf", ".nsfe", ".spc", ".gbs", ".vgm", ".vgz", ".gym", ".hes", ".kss", ".ay", ".sap"}

const (
	gmeRate = 48000
	// gmeFade es lo que dura el fundido del final.
	gmeFade = 8000 // ms
	// gmeSong es la marca de la canción en la ruta.
	gmeSong = "#song="
)

var gme struct {
	once     sync.Once
	err      error
	open     func(path *byte, out *uintptr, rate int32) uintptr
	count    func(emu uintptr) int32
	start    func(emu uintptr, index int32) uintptr
	play     func(emu uintptr, count int32, out unsafe.Pointer) uintptr
	del      func(emu uintptr)
	setFade  func(emu uintptr, startMs int32)
	ended    func(emu uintptr) int32
	tell     func(emu uintptr) int32
	seek     func(emu uintptr, ms int32) uintptr
	info     func(emu uintptr, out *uintptr, track int32) uintptr
	freeInfo func(info uintptr)
}

func loadGME() error {
	if nativeDisabled() {
		return errDisabled
	}
	gme.once.Do(func() {
		gme.err = openLib([]string{"libgme.so.0", "libgme.so", "libgme-0.dll", "libgme.dll"}, []libFunc{
			{&gme.open, "gme_open_file"}, {&gme.count, "gme_track_count"},
			{&gme.start, "gme_start_track"}, {&gme.play, "gme_play"},
			{&gme.del, "gme_delete"}, {&gme.setFade, "gme_set_fade"},
			{&gme.ended, "gme_track_ended"}, {&gme.tell, "gme_tell"},
			{&gme.seek, "gme_seek"}, {&gme.info, "gme_track_info"},
			{&gme.freeInfo, "gme_free_info"},
		})
	})
	return gme.err
}

// GMEAvailable dice si libgme está (sin ella, estos formatos no se ofrecen).
func GMEAvailable() bool { return loadGME() == nil }

// IsGMEFile dice si path es un archivo de libgme, por su extensión.
func IsGMEFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range GMEExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// GMEPath es la ruta de la canción song (desde 1) del archivo file.
func GMEPath(file string, song int) string { return file + gmeSong + strconv.Itoa(song) }

// SplitGMEPath separa una ruta de GMEPath en su archivo y su canción.
func SplitGMEPath(p string) (file string, song int, ok bool) {
	i := strings.LastIndex(p, gmeSong)
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(p[i+len(gmeSong):])
	if err != nil || n < 1 || !IsGMEFile(p[:i]) {
		return "", 0, false
	}
	return p[:i], n, true
}

// FileOf es el archivo de una pista: el mismo path, o el archivo entero si
// es una pista de un CUE o una canción de un archivo de libgme.
func FileOf(path string) string {
	if f, _, _, ok := SplitCuePath(path); ok {
		return f
	}
	if f, _, ok := SplitGMEPath(path); ok {
		return f
	}
	return path
}

// IsVirtualPath dice si path es una pista dentro de un archivo (CUE o
// libgme), no un archivo.
func IsVirtualPath(path string) bool { return FileOf(path) != path }

// GMETrack es una canción de un archivo de libgme.
type GMETrack struct {
	Song                        int // desde 1
	Title, Game, Author, System string
	Length                      int  // ms que dura (con el fundido)
	Loops                       bool // da la vuelta (o no dice cuánto dura): se apaga al final
}

// gmeError es el error de una llamada (un texto de C, o 0 si fue bien).
func gmeError(call string, p uintptr) error {
	if p == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", call, cGoString(p))
}

// cGoString lee un texto de C.
func cGoString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); i < 4096; i++ {
		c := *(*byte)(unsafe.Add(unsafe.Pointer(nil), p+i)) //nolint:govet // memoria de libgme
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// gmeOpen abre file con libgme.
func gmeOpen(file string) (uintptr, error) {
	if err := loadGME(); err != nil {
		return 0, errNoLib(err)
	}
	if err := vgmCheck(file); err != nil {
		return 0, err
	}
	var emu uintptr
	if err := gmeError("gme_open_file", gme.open(cString(file), &emu, gmeRate)); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	return emu, nil
}

// gmeInfo lee la información de la canción i (desde 0).
func gmeInfo(emu uintptr, i int) GMETrack {
	t := GMETrack{Song: i + 1, Length: 150000, Loops: true}
	var p uintptr
	if gmeError("gme_track_info", gme.info(emu, &p, int32(i))) != nil || p == 0 {
		return t
	}
	defer gme.freeInfo(p)
	// struct gme_info_t: 16 int (length, intro_length, loop_length,
	// play_length, fade_length y 11 reservados) y luego punteros a texto
	// (system, game, song, author...).
	ints := (*[16]int32)(unsafe.Add(unsafe.Pointer(nil), p))
	strs := (*[7]uintptr)(unsafe.Add(unsafe.Pointer(nil), p+16*4))
	if pl := ints[3]; pl > 0 {
		t.Length = int(pl)
	}
	// Con duración propia y sin bucle, termina sola.
	t.Loops = ints[0] <= 0 || ints[2] > 0
	t.System, t.Game, t.Title, t.Author = cGoString(strs[0]), cGoString(strs[1]), cGoString(strs[2]), cGoString(strs[3])
	return t
}

// GMETracks son las canciones de un archivo de libgme.
func GMETracks(file string) ([]GMETrack, error) {
	emu, err := gmeOpen(file)
	if err != nil {
		return nil, err
	}
	defer gme.del(emu)
	n := int(gme.count(emu))
	out := make([]GMETrack, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, gmeInfo(emu, i))
	}
	return out, nil
}

// gmeSource toca una canción con libgme.
type gmeSource struct {
	emu    uintptr
	length int64 // cuadros (con el fundido)
	pos    int64
	buf    []int16
	ended  bool
}

// newGME abre la canción song (desde 1) de file.
func newGME(file string, song int) (Source, error) {
	emu, err := gmeOpen(file)
	if err != nil {
		return nil, err
	}
	if song < 1 || song > int(gme.count(emu)) {
		gme.del(emu)
		return nil, fmt.Errorf("%s: no song %d", filepath.Base(file), song)
	}
	t := gmeInfo(emu, song-1)
	if err := gmeError("gme_start_track", gme.start(emu, int32(song-1))); err != nil {
		gme.del(emu)
		return nil, err
	}
	// Si da la vuelta, se apaga y el fundido termina justo al acabar lo que
	// dura (en una corta, en su último cuarto); si termina sola, sin fundido.
	if t.Loops {
		gme.setFade(emu, int32(t.Length-min(gmeFade, t.Length/4)))
	} else {
		gme.setFade(emu, int32(t.Length+gmeFade))
	}
	return &gmeSource{emu: emu, length: int64(t.Length) * gmeRate / 1000}, nil
}

func (s *gmeSource) Read(dst []float32) (int, error) {
	n := len(dst) &^ 1
	if left := (s.length - s.pos) * 2; int64(n) > left {
		n = int(left)
	}
	if n <= 0 || s.ended || gme.ended(s.emu) != 0 {
		s.ended = true
		return 0, io.EOF
	}
	if cap(s.buf) < n {
		s.buf = make([]int16, n)
	}
	buf := s.buf[:n]
	if err := gmeError("gme_play", gme.play(s.emu, int32(n), unsafe.Pointer(&buf[0]))); err != nil {
		s.ended = true
		return 0, err
	}
	for i, v := range buf {
		dst[i] = float32(v) / 32768
	}
	s.pos += int64(n / 2)
	return n, nil
}

func (s *gmeSource) SampleRate() int { return gmeRate }
func (s *gmeSource) Length() int64   { return s.length }
func (s *gmeSource) Position() int64 { return s.pos }

// SeekFrame salta a frame. libgme no salta de verdad: emula hasta ahí (hacia
// atrás, desde el principio), lo que en una canción larga tarda un poco.
func (s *gmeSource) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.length)
	if err := gmeError("gme_seek", gme.seek(s.emu, int32(frame*1000/gmeRate))); err != nil {
		return err
	}
	s.pos, s.ended = frame, false
	return nil
}

func (s *gmeSource) Close() error {
	if s.emu != 0 {
		gme.del(s.emu)
		s.emu = 0
	}
	return nil
}
