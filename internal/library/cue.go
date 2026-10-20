package library

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Cue es una hoja CUE: un álbum guardado en uno o varios archivos grandes,
// con dónde empieza cada pista.
type Cue struct {
	Title, Performer, Genre, Date string
	// Gain es el ReplayGain del álbum (REM REPLAYGAIN_ALBUM_*).
	Gain   CueGain
	Tracks []CueTrack
}

// CueGain es un ReplayGain de la hoja: la ganancia en dB y el pico (1 es
// el máximo). OK es false si la hoja no lo trae.
type CueGain struct {
	DB, Peak float64
	OK       bool
}

// CueTrack es una pista de la hoja.
type CueTrack struct {
	Number           int
	Title, Performer string
	// File es el archivo de audio (ruta absoluta si la hoja se leyó con
	// ReadCue; tal cual la escribe la hoja con ParseCue).
	File string
	// Start y End son el trozo del archivo; End 0 es hasta que acabe.
	Start, End time.Duration
	// Gain es el ReplayGain de la pista (REM REPLAYGAIN_TRACK_*).
	Gain CueGain
}

// Path es la ruta de la pista para la cola (ver decode.CuePath).
func (t CueTrack) Path() string { return decode.CuePath(t.File, t.Start, t.End) }

// ParseCue lee una hoja CUE. Las pistas sin INDEX 01 se descartan; el final
// de cada una es el inicio de la siguiente del mismo archivo (el INDEX 00
// de la siguiente, su pausa previa, no cuenta: suena con la anterior).
func ParseCue(r io.Reader) (*Cue, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := string(data)
	if !utf8.Valid(data) {
		text = fromWindows1252(data)
	}
	c := &Cue{}
	var file string
	var cur *CueTrack
	hasStart := map[int]bool{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		cmd, rest := cueField(strings.TrimSpace(sc.Text()))
		switch strings.ToUpper(cmd) {
		case "REM":
			key, val := cueField(rest)
			switch strings.ToUpper(key) {
			case "GENRE":
				c.Genre = cueValue(val)
			case "DATE":
				c.Date = cueValue(val)
			case "REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK", "REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK":
				g := &c.Gain
				if strings.Contains(strings.ToUpper(key), "TRACK") {
					if cur == nil {
						continue
					}
					g = &cur.Gain
				}
				v, err := strconv.ParseFloat(strings.Fields(cueValue(val) + " x")[0], 64)
				if err != nil {
					continue
				}
				if strings.HasSuffix(strings.ToUpper(key), "GAIN") {
					g.DB, g.OK = v, true
				} else {
					g.Peak = v
				}
			}
		case "PERFORMER":
			if cur != nil {
				cur.Performer = cueValue(rest)
			} else {
				c.Performer = cueValue(rest)
			}
		case "TITLE":
			if cur != nil {
				cur.Title = cueValue(rest)
			} else {
				c.Title = cueValue(rest)
			}
		case "FILE":
			file = cueFileName(rest)
			cur = nil
		case "TRACK":
			num, kind := cueField(rest)
			if !strings.EqualFold(kind, "AUDIO") {
				cur = nil // datos (un CD mixto): no suena
				continue
			}
			n, _ := strconv.Atoi(num)
			c.Tracks = append(c.Tracks, CueTrack{Number: n, File: file})
			cur = &c.Tracks[len(c.Tracks)-1]
		case "INDEX":
			num, at := cueField(rest)
			if cur == nil || num != "01" {
				continue
			}
			d, ok := cueTime(at)
			if !ok {
				return nil, errors.New("cue: bad INDEX time " + strconv.Quote(at))
			}
			cur.Start = d
			hasStart[len(c.Tracks)-1] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var out []CueTrack
	for i, t := range c.Tracks {
		if hasStart[i] && t.File != "" {
			out = append(out, t)
		}
	}
	for i := range out {
		if i+1 < len(out) && out[i+1].File == out[i].File {
			out[i].End = out[i+1].Start
		}
		if out[i].Performer == "" {
			out[i].Performer = c.Performer
		}
	}
	if len(out) == 0 {
		return nil, errors.New("cue: no audio tracks")
	}
	c.Tracks = out
	return c, nil
}

// ReadCue lee la hoja path y resuelve los archivos de sus pistas (ver
// ResolveCueFile); las pistas de un archivo que no existe se descartan.
func ReadCue(path string) (*Cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := ParseCue(f)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	var out []CueTrack
	for _, t := range c.Tracks {
		if p, ok := ResolveCueFile(dir, t.File); ok {
			t.File = p
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("cue: audio file not found")
	}
	c.Tracks = out
	return c, nil
}

// ResolveCueFile busca en dir el archivo que nombra una hoja: tal cual; sin
// distinguir mayúsculas; y con el mismo nombre y otra extensión de audio
// (muchas hojas dicen .wav y el archivo se convirtió a .flac).
func ResolveCueFile(dir, name string) (string, bool) {
	name = filepath.FromSlash(strings.ReplaceAll(name, `\`, "/"))
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	dir = filepath.Dir(p)
	base := filepath.Base(p)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	// Tal cual, con el nombre que tiene en el disco (en Windows, Stat
	// encuentra "bonus.wav" aunque se llame "BONUS.WAV").
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		for _, e := range ents {
			if e.Name() == base {
				return p, true
			}
		}
	}
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(e.Name(), base) {
			return filepath.Join(dir, e.Name()), true
		}
	}
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() && decode.IsAudioFile(n) && strings.EqualFold(strings.TrimSuffix(n, filepath.Ext(n)), stem) {
			return filepath.Join(dir, n), true
		}
	}
	return "", false
}

// cueField parte una línea en la primera palabra y el resto.
func cueField(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i+1:])
}

// cueValue quita las comillas a un valor, si las tiene.
func cueValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		if j := strings.LastIndexByte(s, '"'); j > 0 {
			return s[1:j]
		}
	}
	return s
}

// cueFileName es el nombre de FILE: entre comillas, o todo menos el tipo
// final (WAVE, MP3, AIFF, BINARY...).
func cueFileName(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		return cueValue(s)
	}
	if i := strings.LastIndexAny(s, " \t"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// cueTime lee un tiempo mm:ss:ff (75 cuadros por segundo, los de un CD).
func cueTime(s string) (time.Duration, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, false
		}
		n[i] = v
	}
	frames := (n[0]*60+n[1])*75 + n[2]
	return time.Duration(frames) * time.Second / 75, true
}

// cp1252 son los caracteres de Windows-1252 entre 0x80 y 0x9F (el resto
// coincide con Latin-1); 0 donde no hay ninguno.
var cp1252 = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// fromWindows1252 pasa a UTF-8 un texto en Windows-1252 (el de muchas hojas
// CUE hechas en Windows).
func fromWindows1252(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		r := rune(c)
		if c >= 0x80 && c < 0xA0 {
			if r = cp1252[c-0x80]; r == 0 {
				r = utf8.RuneError
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// IsCue dice si p es una hoja CUE.
func IsCue(p string) bool { return strings.EqualFold(filepath.Ext(p), ".cue") }

// ReadCueTrack da las etiquetas de una pista CUE (ver decode.CuePath): busca
// en la carpeta de su archivo la hoja que la tiene y saca de ella el título,
// el intérprete y el álbum. ok es false si path no es una pista CUE o
// ninguna hoja la nombra.
func ReadCueTrack(path string) (Track, bool) {
	t, _, ok := ReadCueEntry(path)
	return t, ok
}

// ReadCueEntry es ReadCueTrack, más el número de la pista en su disco.
func ReadCueEntry(path string) (Track, int, bool) {
	if c, ok := cueTracks.Load(path); ok {
		c := c.(cueEntry)
		return c.t, c.n, true
	}
	t, n, ok := readCueTrack(path)
	if ok {
		cueTracks.Store(path, cueEntry{t, n})
	}
	return t, n, ok
}

// cueEntry es lo guardado de una pista CUE.
type cueEntry struct {
	t Track
	n int
}

// cueTracks guarda lo leído por ReadCueTrack: el engine lo pide en cada
// cambio de canción y leer las hojas de la carpeta cada vez sobra.
var cueTracks sync.Map

func readCueTrack(path string) (Track, int, bool) {
	file, start, _, ok := decode.SplitCuePath(path)
	if !ok {
		return Track{}, 0, false
	}
	ents, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		return Track{}, 0, false
	}
	for _, e := range ents {
		if e.IsDir() || !IsCue(e.Name()) {
			continue
		}
		c, err := ReadCue(filepath.Join(filepath.Dir(file), e.Name()))
		if err != nil {
			continue
		}
		for _, t := range c.Tracks {
			if t.File == file && t.Start.Truncate(time.Millisecond) == start {
				return Track{Path: path, Title: t.Title, Artist: t.Performer, Album: c.Title}, t.Number, true
			}
		}
	}
	return Track{}, 0, false
}

// ReadGMETrack da las etiquetas de una canción de libgme (un archivo, o una
// de sus canciones, ver decode.GMEPath): el título (o "Juego #n" si no
// tiene), el autor y el juego como disco; y su número. ok es false si no es
// de libgme o no se puede leer.
func ReadGMETrack(path string) (Track, int, bool) {
	file, song, ok := decode.SplitGMEPath(path)
	if !ok {
		if !decode.IsGMEFile(path) {
			return Track{}, 0, false
		}
		file, song = path, 1
	}
	if c, ok := gmeTracks.Load(path); ok {
		c := c.(cueEntry)
		return c.t, c.n, true
	}
	tracks, err := decode.GMETracks(file)
	if err != nil || song > len(tracks) {
		return Track{}, 0, false
	}
	g := tracks[song-1]
	game := g.Game
	if game == "" {
		game = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	title := g.Title
	if title == "" {
		title = game
		if len(tracks) > 1 {
			title = fmt.Sprintf("%s #%d", game, song)
		}
	}
	t := Track{Path: path, Title: title, Artist: g.Author, Album: game}
	gmeTracks.Store(path, cueEntry{t, song})
	return t, song, true
}

// gmeTracks guarda lo leído por ReadGMETrack (abrir el archivo es emularlo).
var gmeTracks sync.Map
