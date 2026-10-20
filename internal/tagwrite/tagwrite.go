// Package tagwrite lee y escribe las etiquetas de los archivos de audio, en
// Go puro: MP3 (ID3v2.3 y 2.4), FLAC, Ogg Vorbis, Ogg Opus y M4A/MP4. Cada
// formato se lee con su propio analizador (el mismo que escribe, para que lo
// que se lee sea justo lo que se escribiría) y se reescribe tocando solo los
// campos que cambian: lo que no entiende (otros marcos de ID3, otros bloques
// de FLAC, otros átomos de MP4) se conserva tal cual, y el audio no se toca
// ni un byte (en Ogg, ni un byte de los paquetes de audio).
//
// La escritura es atómica: el archivo nuevo se escribe aparte, en la misma
// carpeta, y se renombra encima. Si algo falla, el original queda como
// estaba.
package tagwrite

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tags son las etiquetas de una canción. En Write, un campo vacío (o 0, o
// nil) no cambia lo que ya tiene el archivo; para borrarlo se nombra en
// Clear.
type Tags struct {
	Title, Artist, Album, AlbumArtist, Genre, Composer, Comment string
	Year, Track, TrackTotal, Disc, DiscTotal                    int
	ReplayGain                                                  *Gain
	Cover                                                       *Cover
	// Clear son los campos que Write borra: "title", "artist", "album",
	// "albumartist", "genre", "composer", "comment", "year", "track",
	// "tracktotal", "disc", "disctotal", "replaygain" y "cover".
	Clear []string
}

// Gain es la ganancia ReplayGain (en dB respecto a la referencia de
// ReplayGain, -18 LUFS) y los picos (1 = el máximo). Sin HasAlbum, Write
// deja la del álbum que hubiera.
type Gain struct {
	TrackGain, TrackPeak, AlbumGain, AlbumPeak float64
	HasAlbum                                   bool
}

// Cover es la portada (la delantera, tipo 3 en ID3 y FLAC).
type Cover struct {
	MIME string
	Data []byte
}

// ErrUnsupported es el error de un formato que no se sabe escribir.
var ErrUnsupported = errors.New("tagwrite: unsupported format")

// ErrCorrupt es el error de un archivo que no tiene la forma de su formato.
var ErrCorrupt = errors.New("tagwrite: corrupt or unexpected file structure")

// Los formatos.
type format int

const (
	fmtNone format = iota
	fmtMP3
	fmtFLAC
	fmtOgg // Vorbis u Opus (se ve al leer)
	fmtMP4
)

// formatOf es el formato de path, por su extensión.
func formatOf(path string) format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return fmtMP3
	case ".flac":
		return fmtFLAC
	case ".ogg", ".oga", ".opus":
		return fmtOgg
	case ".m4a", ".m4b", ".mp4":
		return fmtMP4
	}
	return fmtNone
}

// Supported dice si Write sabe escribir path (por su extensión; WAV y AIFF
// no).
func Supported(path string) bool { return formatOf(path) != fmtNone }

// editor es un archivo abierto de un formato: lo que tiene y cómo cambiarlo.
// Los set con "" (o 0, o nil) borran el campo.
type editor interface {
	tags() Tags
	setText(field, value string)
	setYear(y int)
	setTrack(n, total int)
	setDisc(n, total int)
	setGain(g *Gain)
	setCover(c *Cover)
	// write escribe el archivo entero, con lo cambiado.
	write(w io.Writer) error
}

// open lee path con el analizador de su formato.
func open(path string) (editor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ed editor
	switch formatOf(path) {
	case fmtMP3:
		ed, err = parseMP3(data)
	case fmtFLAC:
		ed, err = parseFLAC(data)
	case fmtOgg:
		ed, err = parseOgg(data)
	case fmtMP4:
		ed, err = parseMP4(data)
	default:
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), ErrUnsupported)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return ed, nil
}

// Read lee las etiquetas de path.
func Read(path string) (Tags, error) {
	ed, err := open(path)
	if err != nil {
		return Tags{}, err
	}
	return ed.tags(), nil
}

// Write cambia en path los campos de t que traen algo (y borra los de
// t.Clear), sin tocar el resto ni el audio.
func Write(path string, t Tags) error {
	ed, err := open(path)
	if err != nil {
		return err
	}
	old := ed.tags()
	want := merge(old, t)
	clear := map[string]bool{}
	for _, c := range t.Clear {
		clear[strings.ToLower(c)] = true
	}
	changed := false
	text := func(field, o, n string) {
		if o != n || (clear[field] && o != "") {
			ed.setText(field, n)
			changed = true
		}
	}
	text("title", old.Title, want.Title)
	text("artist", old.Artist, want.Artist)
	text("album", old.Album, want.Album)
	text("albumartist", old.AlbumArtist, want.AlbumArtist)
	text("genre", old.Genre, want.Genre)
	text("composer", old.Composer, want.Composer)
	text("comment", old.Comment, want.Comment)
	if old.Year != want.Year {
		ed.setYear(want.Year)
		changed = true
	}
	if old.Track != want.Track || old.TrackTotal != want.TrackTotal {
		ed.setTrack(want.Track, want.TrackTotal)
		changed = true
	}
	if old.Disc != want.Disc || old.DiscTotal != want.DiscTotal {
		ed.setDisc(want.Disc, want.DiscTotal)
		changed = true
	}
	if !sameGain(old.ReplayGain, want.ReplayGain) {
		ed.setGain(want.ReplayGain)
		changed = true
	}
	if !sameCover(old.Cover, want.Cover) {
		ed.setCover(want.Cover)
		changed = true
	}
	if !changed {
		return nil
	}
	return writeAtomic(path, ed.write)
}

// sameGain dice si dos ganancias son la misma (a la precisión con que se
// escriben: centésimas de dB, o 1/256 dB en Opus).
func sameGain(a, b *Gain) bool {
	if a == nil || b == nil {
		return a == b
	}
	near := func(x, y, tol float64) bool { return math.Abs(x-y) <= tol }
	const db, peak = 1.0/256 + 1e-9, 5e-7
	if !near(a.TrackGain, b.TrackGain, db) || !near(a.TrackPeak, b.TrackPeak, peak) || a.HasAlbum != b.HasAlbum {
		return false
	}
	return !a.HasAlbum || (near(a.AlbumGain, b.AlbumGain, db) && near(a.AlbumPeak, b.AlbumPeak, peak))
}

// sameCover dice si dos portadas son la misma imagen.
func sameCover(a, b *Cover) bool {
	if a == nil || b == nil {
		return a == b
	}
	return bytes.Equal(a.Data, b.Data)
}

// merge es old con lo que trae t encima (y sin lo de t.Clear).
func merge(old, t Tags) Tags {
	out := old
	out.Clear = nil
	str := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	num := func(dst *int, v int) {
		if v != 0 {
			*dst = v
		}
	}
	str(&out.Title, t.Title)
	str(&out.Artist, t.Artist)
	str(&out.Album, t.Album)
	str(&out.AlbumArtist, t.AlbumArtist)
	str(&out.Genre, t.Genre)
	str(&out.Composer, t.Composer)
	str(&out.Comment, t.Comment)
	num(&out.Year, t.Year)
	num(&out.Track, t.Track)
	num(&out.TrackTotal, t.TrackTotal)
	num(&out.Disc, t.Disc)
	num(&out.DiscTotal, t.DiscTotal)
	if t.ReplayGain != nil {
		g := *t.ReplayGain
		if !g.HasAlbum && old.ReplayGain != nil && old.ReplayGain.HasAlbum {
			g.AlbumGain, g.AlbumPeak, g.HasAlbum = old.ReplayGain.AlbumGain, old.ReplayGain.AlbumPeak, true
		}
		out.ReplayGain = &g
	}
	if t.Cover != nil {
		c := *t.Cover
		out.Cover = &c
	}
	for _, c := range t.Clear {
		switch strings.ToLower(c) {
		case "title":
			out.Title = ""
		case "artist":
			out.Artist = ""
		case "album":
			out.Album = ""
		case "albumartist":
			out.AlbumArtist = ""
		case "genre":
			out.Genre = ""
		case "composer":
			out.Composer = ""
		case "comment":
			out.Comment = ""
		case "year":
			out.Year = 0
		case "track":
			out.Track = 0
		case "tracktotal":
			out.TrackTotal = 0
		case "disc":
			out.Disc = 0
		case "disctotal":
			out.DiscTotal = 0
		case "replaygain":
			out.ReplayGain = nil
		case "cover":
			out.Cover = nil
		}
	}
	return out
}

// writeAtomic escribe path entero con fill, aparte (en la misma carpeta, con
// los mismos permisos) y luego encima.
func writeAtomic(path string, fill func(io.Writer) error) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tagwrite-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := fill(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmp, st.Mode().Perm())
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	ok = true
	return nil
}

// Las ayudas comunes de texto.

// parseNumPair lee "3" o "3/12".
func parseNumPair(s string) (n, total int) {
	s = strings.TrimSpace(s)
	a, b, _ := strings.Cut(s, "/")
	n, _ = strconv.Atoi(strings.TrimSpace(a))
	total, _ = strconv.Atoi(strings.TrimSpace(b))
	return max(0, n), max(0, total)
}

// parseYear lee el año del principio de una fecha ("2019", "2019-05-01").
func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return y
}

// numPair escribe "3" o "3/12".
func numPair(n, total int) string {
	switch {
	case n > 0 && total > 0:
		return fmt.Sprintf("%d/%d", n, total)
	case n > 0:
		return strconv.Itoa(n)
	case total > 0:
		return fmt.Sprintf("0/%d", total)
	}
	return ""
}

// formatGain es una ganancia como en las etiquetas: "-6.54 dB".
func formatGain(g float64) string { return fmt.Sprintf("%.2f dB", g) }

// formatPeak es un pico: "0.987654".
func formatPeak(p float64) string { return fmt.Sprintf("%.6f", p) }

// parseGainDB lee "-6.54 dB" (o con coma, o sin unidad).
func parseGainDB(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s), "\x00"))
	low := strings.ToLower(s)
	if strings.HasSuffix(low, "db") {
		s = strings.TrimSpace(s[:len(s)-2])
	}
	s = strings.Replace(s, ",", ".", 1)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// parsePeak lee un pico.
func parsePeak(s string) float64 {
	s = strings.Replace(strings.TrimSpace(strings.TrimRight(s, "\x00")), ",", ".", 1)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

// gainFromFields saca la ganancia de unos campos (nombres en minúsculas):
// REPLAYGAIN_* y, si no están, las R128_* de Opus (en 1/256 dB respecto a
// -23 LUFS: se les suman 5 dB para la referencia de ReplayGain).
func gainFromFields(get func(key string) (string, bool)) *Gain {
	var g Gain
	has := false
	if v, ok := get("replaygain_track_gain"); ok {
		if x, ok := parseGainDB(v); ok {
			g.TrackGain, has = x, true
		}
	}
	if !has {
		if v, ok := get("r128_track_gain"); ok {
			if x, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				g.TrackGain, has = float64(x)/256+5, true
			}
		}
	}
	if v, ok := get("replaygain_album_gain"); ok {
		if x, ok := parseGainDB(v); ok {
			g.AlbumGain, g.HasAlbum = x, true
		}
	}
	if !g.HasAlbum {
		if v, ok := get("r128_album_gain"); ok {
			if x, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				g.AlbumGain, g.HasAlbum = float64(x)/256+5, true
			}
		}
	}
	if v, ok := get("replaygain_track_peak"); ok {
		g.TrackPeak = parsePeak(v)
	}
	if v, ok := get("replaygain_album_peak"); ok {
		g.AlbumPeak = parsePeak(v)
	}
	if !has && !g.HasAlbum {
		return nil
	}
	return &g
}

// r128 es una ganancia de ReplayGain en el formato de Opus: entero en 1/256
// dB respecto a -23 LUFS (5 dB menos que la referencia de ReplayGain).
func r128(g float64) string {
	v := math.Round((g - 5) * 256)
	return strconv.Itoa(int(max(-32768, min(32767, v))))
}

// genres son los géneros de ID3v1 (los 80 del estándar), para los TCON
// numéricos ("(17)").
var genres = [...]string{
	"Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk", "Grunge", "Hip-Hop", "Jazz", "Metal",
	"New Age", "Oldies", "Other", "Pop", "R&B", "Rap", "Reggae", "Rock", "Techno", "Industrial",
	"Alternative", "Ska", "Death Metal", "Pranks", "Soundtrack", "Euro-Techno", "Ambient", "Trip-Hop", "Vocal", "Jazz+Funk",
	"Fusion", "Trance", "Classical", "Instrumental", "Acid", "House", "Game", "Sound Clip", "Gospel", "Noise",
	"AlternRock", "Bass", "Soul", "Punk", "Space", "Meditative", "Instrumental Pop", "Instrumental Rock", "Ethnic", "Gothic",
	"Darkwave", "Techno-Industrial", "Electronic", "Pop-Folk", "Eurodance", "Dream", "Southern Rock", "Comedy", "Cult", "Gangsta",
	"Top 40", "Christian Rap", "Pop/Funk", "Jungle", "Native American", "Cabaret", "New Wave", "Psychadelic", "Rave", "Showtunes",
	"Trailer", "Lo-Fi", "Tribal", "Acid Punk", "Acid Jazz", "Polka", "Retro", "Musical", "Rock & Roll", "Hard Rock",
}

// genreName pasa un género numérico de ID3 ("17", "(17)", "(17)Rock") a su
// nombre; si no lo es, lo deja.
func genreName(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "(") {
		if i := strings.Index(t, ")"); i > 0 {
			if rest := strings.TrimSpace(t[i+1:]); rest != "" {
				return rest
			}
			t = t[1:i]
		}
	}
	if n, err := strconv.Atoi(t); err == nil && n >= 0 && n < len(genres) {
		return genres[n]
	}
	return s
}
