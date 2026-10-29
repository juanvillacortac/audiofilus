package chip

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Extensions son las extensiones que se tocan aquí.
var Extensions = []string{".sid", ".ym", ".vtx", ".pt3"}

// Is dice si path es de los que se tocan aquí, por su extensión.
func Is(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range Extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// ErrFormat es el error de un archivo que no se entiende.
var ErrFormat = errors.New("chip: unsupported or corrupt file")

// Track es una canción de un archivo.
type Track struct {
	Song                        int // desde 1
	Title, Author, Game, System string
	// Length es lo que dura (con el fundido, si da la vuelta).
	Length time.Duration
	// Loops dice si da la vuelta (o no se sabe cuándo acaba) y se apaga al
	// final de Length.
	Loops bool
}

// Tune es una canción que suena.
type Tune interface {
	// Render llena dst con muestras estéreo intercaladas y dice cuántas
	// escribió (siempre pares); menos de len(dst) solo al acabar.
	Render(dst []float32) int
}

// Skipper es una Tune que sabe avanzar sin sonar, más rápido que Render.
type Skipper interface {
	Skip(frames int)
}

// maxFile es lo más que se lee de un archivo (los más grandes son YM de unos
// cientos de KB).
const maxFile = 16 << 20

// readFile lee path, si no es demasiado grande.
func readFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFile {
		return nil, fmt.Errorf("%s: too big", filepath.Base(path))
	}
	return os.ReadFile(path)
}

// song es un archivo leído: sus canciones y cómo tocar cada una.
type song interface {
	tracks() []Track
	play(n, rate int) (Tune, error)
}

// load lee path según su extensión.
func load(path string) (song, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var s song
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ym":
		s, err = loadYM(b, base)
	case ".vtx":
		s, err = loadVTX(b, base)
	case ".pt3":
		s, err = loadPT3(b, base)
	case ".sid":
		s, err = loadSID(b, path)
	default:
		err = ErrFormat
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return s, nil
}

// Tracks son las canciones de path.
func Tracks(path string) ([]Track, error) {
	s, err := load(path)
	if err != nil {
		return nil, err
	}
	return s.tracks(), nil
}

// Open empieza a tocar la canción n (desde 1) de path a rate Hz.
func Open(path string, n, rate int) (Tune, Track, error) {
	s, err := load(path)
	if err != nil {
		return nil, Track{}, err
	}
	ts := s.tracks()
	if n < 1 || n > len(ts) {
		return nil, Track{}, fmt.Errorf("%s: no song %d", filepath.Base(path), n)
	}
	t, err := s.play(n, rate)
	if err != nil {
		return nil, Track{}, err
	}
	return t, ts[n-1], nil
}

// cstr lee un texto terminado en cero desde b[*at], y avanza *at.
func cstr(b []byte, at *int) string {
	i := *at
	for i < len(b) && b[i] != 0 {
		i++
	}
	s := string(b[min(*at, len(b)):i])
	*at = i + 1
	return strings.TrimSpace(latin1(s))
}

// latin1 pasa a UTF-8 un texto de 8 bits (los de estos formatos son ASCII o
// Latin-1).
func latin1(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	r := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		r[i] = rune(s[i])
	}
	return string(r)
}

// fixed lee un texto de largo fijo (relleno de ceros o espacios).
func fixed(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(latin1(string(b)))
}
