package lyrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Option es una letra posible para una canción, de donde sea.
type Option struct {
	// ID dice de dónde sale, y es lo que se guarda al elegirla:
	// "file:<ruta>", "tag", "tag:sylt", "tag:<campo>", "lrclib:<id>" o "none".
	ID     string
	Label  string // el archivo, "Etiqueta", "Artista - Título"...
	Detail string // el álbum y la duración (LRCLIB)
	Lyrics Lyrics
	// Guess: un .lrc de la carpeta con otro nombre. Se ofrece para elegirlo
	// a mano, pero no se usa solo (puede ser de otra canción).
	Guess bool
}

// Kind es el tipo de origen: "file", "tag", "lrclib" o "none".
func (o Option) Kind() string {
	k, _, _ := strings.Cut(o.ID, ":")
	return k
}

// Synced dice si la letra está sincronizada.
func (o Option) Synced() bool { return o.Lyrics.Synced }

// maxFile es lo más que se lee de un archivo de letra (uno más grande no lo es).
const maxFile = 512 << 10

// IsLocal dice si path es un archivo (no una radio, un servidor o YouTube
// Music, que no tienen archivos de letra ni etiquetas a mano, ni una pista
// de un CUE, cuyo archivo y etiquetas son los del disco entero).
func IsLocal(path string) bool {
	return path != "" && !strings.Contains(path, "://") && filepath.IsAbs(path) && !decode.IsVirtualPath(path)
}

// Local son las letras que hay a mano para la canción en path: los archivos
// .lrc y .txt con su mismo nombre (junto a ella o en una carpeta "lyrics"),
// los otros .lrc de su carpeta, y la de sus etiquetas.
func Local(path string) []Option {
	if !IsLocal(path) {
		return nil
	}
	var out []Option
	seen := map[string]bool{}
	addFile := func(p string, guess bool) {
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Size() > maxFile {
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		if l := Parse(string(b)); !l.Empty() {
			out = append(out, Option{ID: "file:" + p, Label: filepath.Base(p), Lyrics: l, Guess: guess})
		}
	}
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, sub := range []string{"", "lyrics", "Lyrics", "LYRICS"} {
		for _, ext := range []string{".lrc", ".LRC", ".txt", ".TXT"} {
			addFile(filepath.Join(dir, sub, base+ext), false)
		}
	}
	// Los otros .lrc de la carpeta: a veces se llaman "01 - Título.lrc"
	// cuando la canción es "Título.flac", o al revés.
	var others []string
	for _, sub := range []string{"", "lyrics", "Lyrics"} {
		ents, _ := os.ReadDir(filepath.Join(dir, sub))
		for _, e := range ents {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".lrc") {
				others = append(others, filepath.Join(dir, sub, e.Name()))
			}
		}
	}
	sort.SliceStable(others, func(i, j int) bool {
		return similar(base, others[i]) > similar(base, others[j])
	})
	var guesses []Option
	n := len(out)
	for _, p := range others {
		addFile(p, true)
	}
	out, guesses = out[:n], out[n:]
	// Las etiquetas van antes que las adivinadas.
	return append(append(out, Embedded(path)...), guesses...)
}

// similar cuenta las palabras del nombre de la canción que están en el del
// archivo: para poner primero el .lrc que más se parece.
func similar(base, file string) int {
	f := norm(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	n := 0
	for _, w := range strings.Fields(strings.ToLower(base)) {
		if w = norm(w); len(w) > 1 && strings.Contains(f, w) {
			n++
		}
	}
	return n
}

// Embedded son las letras de las etiquetas de la canción: la sincronizada de
// ID3 (SYLT), la de USLT o ©lyr, y en FLAC y Ogg los campos LYRICS,
// UNSYNCEDLYRICS y SYNCEDLYRICS (que a menudo traen un LRC entero).
func Embedded(path string) []Option {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil
	}
	var out []Option
	seen := map[string]bool{}
	add := func(id string, l Lyrics) {
		if l.Empty() {
			return
		}
		key := Format(l)
		if seen[key] {
			return // la misma letra en dos campos
		}
		seen[key] = true
		out = append(out, Option{ID: id, Label: "", Lyrics: l})
	}
	raw := m.Raw()
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if b, ok := raw[k].([]byte); ok && strings.HasPrefix(k, "SYLT") {
			if l, err := ParseSYLT(b); err == nil {
				add("tag:sylt", l)
			}
		}
	}
	add("tag", Parse(m.Lyrics()))
	for _, k := range []string{"syncedlyrics", "lyrics", "unsyncedlyrics", "unsynced lyrics"} {
		if s, ok := raw[k].(string); ok {
			add("tag:"+k, Parse(s))
		}
	}
	return out
}

// Online busca la canción en LRCLIB y devuelve cada resultado con letra
// como una opción.
func Online(ctx context.Context, c *LRCLIB, q Query) ([]Option, error) {
	found, err := c.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []Option
	for _, f := range found {
		if o, ok := OptionOf(f); ok {
			out = append(out, o)
		}
	}
	return out, nil
}

// OptionOf convierte un resultado de LRCLIB en una opción (si trae letra).
func OptionOf(f Found) (Option, bool) {
	l := f.Lyrics()
	if f.Instrumental {
		l = Lyrics{Instrumental: true}
	}
	if l.Empty() {
		return Option{}, false
	}
	detail := f.Album
	if f.Duration > 0 {
		d := time.Duration(f.Duration) * time.Second
		dur := fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
		if detail != "" {
			detail += " · "
		}
		detail += dur
	}
	label := f.Track
	if f.Artist != "" {
		label = f.Artist + " - " + f.Track
	}
	return Option{ID: fmt.Sprintf("lrclib:%d", f.ID), Label: label, Detail: detail, Lyrics: l}, true
}
