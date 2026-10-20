// Package lyrics busca, lee y guarda las letras de las canciones: de un
// .lrc o .txt junto a la canción, de sus etiquetas (USLT, SYLT, LYRICS) o de
// internet (LRCLIB). Las sincronizadas (LRC) dicen en qué momento va cada
// línea; las otras son solo texto.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Line es una línea de la letra y cuándo empieza (en una letra sin
// sincronizar, At es 0).
type Line struct {
	At   time.Duration
	Text string
}

// Lyrics es una letra lista para mostrar.
type Lyrics struct {
	Lines  []Line
	Synced bool
	// Instrumental: la canción no tiene letra, y se sabe (LRCLIB lo dice).
	Instrumental bool
}

// Empty dice si no hay nada que mostrar (una instrumental sí lo tiene).
func (l Lyrics) Empty() bool {
	if l.Instrumental {
		return false
	}
	for _, ln := range l.Lines {
		if strings.TrimSpace(ln.Text) != "" {
			return false
		}
	}
	return true
}

// Index es la línea que toca en pos: la última que ya empezó, o -1 antes
// de la primera. En una letra sin sincronizar siempre es -1.
func (l Lyrics) Index(pos time.Duration) int {
	if !l.Synced {
		return -1
	}
	return sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].At > pos }) - 1
}

// Los huecos instrumentales: la intro cuenta si la primera línea tarda al
// menos breakIntro, y entre dos líneas, si después de cantar la primera
// (lo que se calcula por sus palabras) quedan al menos breakGap.
const (
	breakIntro = 4 * time.Second
	breakGap   = 6 * time.Second
)

// WithBreaks añade a una letra sincronizada líneas vacías donde no se canta:
// antes de la primera (la intro) y en los silencios largos entre dos. Una
// línea vacía es un hueco instrumental (la vista pinta ahí unos puntos que
// se llenan hasta la siguiente).
func (l Lyrics) WithBreaks() Lyrics {
	if !l.Synced || len(l.Lines) == 0 {
		return l
	}
	var out []Line
	if l.Lines[0].At >= breakIntro {
		out = append(out, Line{})
	}
	for i, ln := range l.Lines {
		out = append(out, ln)
		if ln.Text == "" || i+1 >= len(l.Lines) {
			continue
		}
		next := l.Lines[i+1]
		if next.Text == "" {
			continue // ya trae su hueco
		}
		// Lo que tarda en cantarse: unos 0,4 s por palabra, más un respiro.
		sing := time.Duration(len(strings.Fields(ln.Text)))*400*time.Millisecond + time.Second
		sing = min(max(sing, 2*time.Second), 7*time.Second)
		if next.At-(ln.At+sing) >= breakGap {
			out = append(out, Line{At: ln.At + sing})
		}
	}
	l.Lines = out
	return l
}

// lrcTime es una marca de tiempo de LRC: [mm:ss], [mm:ss.xx] o [mm:ss.xxx]
// (algunos usan ":" en vez de ".").
var lrcTime = regexp.MustCompile(`^\[(\d+):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// lrcTag es una etiqueta de LRC que no es un tiempo ([ar:...], [offset:...]).
var lrcTag = regexp.MustCompile(`^\[([a-zA-Z#]+):([^\]]*)\]`)

// lrcWord son las marcas por palabra del LRC "mejorado" (<mm:ss.xx>), que
// aquí no se usan.
var lrcWord = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>`)

// Parse lee una letra: LRC si tiene marcas de tiempo, si no texto plano.
func Parse(s string) Lyrics {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var timed []Line
	var offset time.Duration
	for _, raw := range strings.Split(s, "\n") {
		rest := strings.TrimSpace(raw)
		var stamps []time.Duration
		for {
			if m := lrcTime.FindStringSubmatch(rest); m != nil {
				stamps = append(stamps, stampOf(m))
				rest = rest[len(m[0]):]
				continue
			}
			if m := lrcTag.FindStringSubmatch(rest); m != nil && len(stamps) == 0 {
				if strings.EqualFold(m[1], "offset") {
					if ms, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil {
						offset = time.Duration(ms) * time.Millisecond
					}
				}
				rest = rest[len(m[0]):]
				continue
			}
			break
		}
		text := strings.TrimSpace(lrcWord.ReplaceAllString(rest, ""))
		for _, at := range stamps {
			timed = append(timed, Line{At: at, Text: text})
		}
	}
	if len(timed) > 0 {
		// Un offset positivo adelanta la letra.
		for i := range timed {
			timed[i].At = max(0, timed[i].At-offset)
		}
		sort.SliceStable(timed, func(i, j int) bool { return timed[i].At < timed[j].At })
		return Lyrics{Lines: timed, Synced: true}
	}
	return plain(s)
}

// plain parte un texto en líneas, sin más de una línea en blanco seguida
// ni blancos al principio o al final.
func plain(s string) Lyrics {
	var out []Line
	blank := true
	for _, raw := range strings.Split(s, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			if !blank {
				out = append(out, Line{})
			}
			blank = true
			continue
		}
		out = append(out, Line{Text: t})
		blank = false
	}
	for len(out) > 0 && out[len(out)-1].Text == "" {
		out = out[:len(out)-1]
	}
	return Lyrics{Lines: out}
}

func stampOf(m []string) time.Duration {
	mm, _ := strconv.Atoi(m[1])
	ss, _ := strconv.Atoi(m[2])
	d := time.Duration(mm)*time.Minute + time.Duration(ss)*time.Second
	if f := m[3]; f != "" {
		n, _ := strconv.Atoi(f)
		// Décimas, centésimas o milésimas según cuántas cifras tenga.
		for i := len(f); i < 3; i++ {
			n *= 10
		}
		d += time.Duration(n) * time.Millisecond
	}
	return d
}

// Format escribe una letra como LRC (o como texto, si no está sincronizada):
// es lo que se guarda en la caché.
func Format(l Lyrics) string {
	var b strings.Builder
	for _, ln := range l.Lines {
		if l.Synced {
			ms := ln.At.Milliseconds()
			b.WriteString("[" + pad2(int(ms/60000)) + ":" + pad2(int(ms/1000%60)) + "." + pad2(int(ms%1000/10)) + "] ")
		}
		b.WriteString(ln.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
