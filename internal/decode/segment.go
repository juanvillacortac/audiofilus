package decode

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Las pistas de una hoja CUE (un álbum entero en un solo archivo) viven en la
// cola como rutas normales con un trozo de tiempo al final, como los
// fragmentos de medios de las URL: "/música/álbum.flac#t=12.345,256.789" es
// ese archivo del segundo 12,345 al 256,789 (sin el final, hasta que acabe).

// cueSuffix reconoce el "#t=inicio[,fin]" del final de una ruta.
var cueSuffix = regexp.MustCompile(`#t=(\d+(?:\.\d+)?)(?:,(\d+(?:\.\d+)?))?$`)

// CuePath es la ruta de la pista de file que va de start a end (end 0: hasta
// el final del archivo).
func CuePath(file string, start, end time.Duration) string {
	p := file + "#t=" + secs(start)
	if end > 0 {
		p += "," + secs(end)
	}
	return p
}

// secs escribe d en segundos con hasta milisegundos, sin ceros de sobra.
func secs(d time.Duration) string {
	s := strconv.FormatFloat(float64(d.Milliseconds())/1000, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// SplitCuePath separa una ruta de pista CUE en el archivo y su trozo; ok es
// false si p es una ruta normal.
func SplitCuePath(p string) (file string, start, end time.Duration, ok bool) {
	m := cueSuffix.FindStringSubmatchIndex(p)
	if m == nil {
		return p, 0, 0, false
	}
	parse := func(a, b int) time.Duration {
		if a < 0 {
			return 0
		}
		f, _ := strconv.ParseFloat(p[a:b], 64)
		return time.Duration(f * float64(time.Second))
	}
	start, end = parse(m[2], m[3]), parse(m[4], m[5])
	if end > 0 && end <= start {
		return p, 0, 0, false
	}
	return p[:m[0]], start, end, true
}

// IsCuePath dice si p es una pista de una hoja CUE.
func IsCuePath(p string) bool {
	_, _, _, ok := SplitCuePath(p)
	return ok
}

// segment es una pista CUE: un trozo de otra fuente, con posiciones
// relativas a su inicio y un final exacto (al frame).
type segment struct {
	src        Source
	start, end int64 // en frames de src; end -1: hasta el final
	pos        int64 // relativa a start
}

// OpenSegment abre una pista CUE (ver CuePath): el archivo con Open, ya
// colocado en su inicio.
func OpenSegment(path string) (Source, error) {
	file, start, end, ok := SplitCuePath(path)
	if !ok {
		return nil, fmt.Errorf("%s: not a CUE track path", path)
	}
	src, err := Open(file)
	if err != nil {
		return nil, err
	}
	s, err := newSegment(src, start, end)
	if err != nil {
		src.Close()
		return nil, err
	}
	return s, nil
}

// newSegment recorta src al trozo de start a end (end 0: hasta el final).
func newSegment(src Source, start, end time.Duration) (*segment, error) {
	rate := int64(src.SampleRate())
	s := &segment{src: src, start: durFrames(start, rate), end: -1}
	if end > 0 {
		s.end = durFrames(end, rate)
	}
	if n := src.Length(); n >= 0 {
		s.start = min(s.start, n)
		if s.end < 0 || s.end > n {
			s.end = n
		}
	}
	if err := src.SeekFrame(s.start); err != nil {
		return nil, err
	}
	return s, nil
}

// durFrames pasa d a frames a rate, redondeando.
func durFrames(d time.Duration, rate int64) int64 {
	return (int64(d)*rate + int64(time.Second)/2) / int64(time.Second)
}

func (s *segment) Read(dst []float32) (int, error) {
	if s.end >= 0 {
		left := (s.end - s.start - s.pos) * 2
		if left <= 0 {
			return 0, io.EOF
		}
		if int64(len(dst)) > left {
			dst = dst[:left] // la última lectura acaba justo en el final
		}
	}
	// Al llegar justo al final, la próxima lectura dice EOF.
	n, err := s.src.Read(dst)
	s.pos += int64(n / 2)
	return n, err
}

func (s *segment) SampleRate() int { return s.src.SampleRate() }

func (s *segment) Length() int64 {
	if s.end < 0 {
		if n := s.src.Length(); n >= 0 {
			return n - s.start
		}
		return -1
	}
	return s.end - s.start
}

func (s *segment) Position() int64 { return s.pos }

func (s *segment) SeekFrame(frame int64) error {
	frame = clampFrame(frame, s.Length())
	if err := s.src.SeekFrame(s.start + frame); err != nil {
		return err
	}
	s.pos = s.src.Position() - s.start
	return nil
}

func (s *segment) Close() error { return s.src.Close() }
