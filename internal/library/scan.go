// Package library busca archivos de audio en disco y lee sus etiquetas.
package library

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/tags"
)

// Expand convierte archivos, carpetas (recursivamente) y listas .m3u/.m3u8 en
// una lista ordenada de archivos de audio. Lo de cada carpeta se ordena por ruta.
func Expand(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		if strings.Contains(p, "://") {
			out = append(out, p) // remota: viaja tal cual
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			// Quizá estropeada por JSON o en otra normalización Unicode
			// (decode.FindFile); si no aparece, el error es el de la original.
			fixed := decode.FindFile(p)
			if fixed == p {
				return nil, err
			}
			if info, err = os.Stat(fixed); err != nil {
				return nil, err
			}
			p = fixed
		}
		switch {
		case info.IsDir():
			files, err := scanDir(p)
			if err != nil {
				return nil, err
			}
			out = append(out, files...)
		case isPlaylist(p):
			files, err := readM3U(p)
			if err != nil {
				return nil, err
			}
			out = append(out, files...)
		case IsCue(p):
			c, err := ReadCue(p)
			if err != nil {
				return nil, err
			}
			for _, t := range c.Tracks {
				out = append(out, t.Path())
			}
		case decode.IsAudioFile(p):
			out = append(out, p)
		}
	}
	return out, nil
}

// scanDir lista los archivos de audio bajo root, saltando las carpetas ocultas.
func scanDir(root string) ([]string, error) {
	var files, cues []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // lo que no se puede leer se salta
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		switch {
		case d.IsDir():
		case decode.IsAudioFile(path):
			files = append(files, path)
		case IsCue(path):
			cues = append(cues, path)
		}
		return nil
	})
	files = withCues(files, cues)
	files = withSongs(files)
	sort.Strings(files)
	return files, err
}

// withSongs cambia, en files, cada archivo de libgme con varias canciones
// (un NSF trae la banda sonora entera) por una pista por canción (ver
// decode.GMEPath). Sin libgme quedan como están (y no se podrán abrir).
func withSongs(files []string) []string {
	var out []string
	for _, f := range files {
		if !decode.IsGMEFile(f) {
			out = append(out, f)
			continue
		}
		tracks, err := decode.GMETracks(f)
		if err != nil || len(tracks) <= 1 {
			out = append(out, f)
			continue
		}
		for _, t := range tracks {
			out = append(out, decode.GMEPath(f, t.Song))
		}
	}
	return out
}

// withCues cambia, en files, cada archivo que tenga una hoja CUE (un disco
// entero en un archivo) por sus pistas (ver decode.CuePath). Si la hoja no
// se puede leer o no encuentra su archivo, queda el archivo tal cual.
func withCues(files, cues []string) []string {
	if len(cues) == 0 {
		return files
	}
	skip := map[string]bool{}
	var tracks []string
	for _, c := range cues {
		cue, err := ReadCue(c)
		if err != nil || len(cue.Tracks) == 0 {
			continue
		}
		for _, t := range cue.Tracks {
			tracks = append(tracks, t.Path())
			skip[t.File] = true
		}
	}
	out := tracks
	for _, f := range files {
		if !skip[f] {
			out = append(out, f)
		}
	}
	return out
}

// isPlaylist dice si p es una lista M3U.
// FindPlaylists busca listas .m3u/.m3u8 dentro de dirs, ordenadas por ruta.
func FindPlaylists(dirs []string) []string {
	var out []string
	for _, root := range dirs {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			if !d.IsDir() && isPlaylist(path) {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func isPlaylist(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".m3u" || ext == ".m3u8"
}

// readM3U lee una lista M3U; las rutas relativas se resuelven contra su carpeta
// y se aceptan barras de Windows.
func readM3U(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dir := filepath.Dir(path)
	var files []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = m3uPath(dir, line)
		if decode.IsAudioFile(line) || decode.IsVirtualPath(line) {
			files = append(files, line)
		}
	}
	return files, sc.Err()
}

// m3uPath es la ruta de una línea de una lista de dir: las relativas
// contra dir, con barras de Windows o no. Si no está tal cual en el disco,
// se prueba la línea como Windows-1252 (las .m3u sin el 8 hechas en Windows
// lo son) y luego con decode.FindFile (NFC frente a NFD); si tampoco, queda
// la de la línea.
func m3uPath(dir, line string) string {
	abs := func(l string) string {
		l = filepath.FromSlash(strings.ReplaceAll(l, `\`, "/"))
		if !filepath.IsAbs(l) {
			l = filepath.Join(dir, l)
		}
		return l
	}
	p := abs(line)
	ascii := strings.IndexFunc(line, func(r rune) bool { return r >= utf8.RuneSelf }) < 0
	if ascii {
		return p // nada que arreglar: sin mirar el disco, que en la consola es lento
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	cands := []string{p}
	if !utf8.ValidString(line) {
		cands = append([]string{abs(fromWindows1252([]byte(line)))}, cands...)
	}
	for _, c := range cands {
		if f := decode.FindFile(c); f != c {
			return f
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return p
}

// Track son las etiquetas que se muestran de una canción.
type Track struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// RemoteTrack da las etiquetas de una canción remota ("subsonic://..."), que no
// es un archivo: las pone el servicio con el índice de internal/remote.
var RemoteTrack func(path string) (Track, bool)

// ReadTrack lee las etiquetas de path; si no tiene título, usa el nombre del archivo.
func ReadTrack(path string) Track {
	t := Track{Path: path}
	if strings.Contains(path, "://") {
		if RemoteTrack != nil {
			if r, ok := RemoteTrack(path); ok {
				r.Path = path
				return r
			}
		}
		t.Title = path[strings.LastIndex(path, "/")+1:]
		return t
	}
	if c, ok := ReadCueTrack(path); ok {
		return c
	}
	if g, _, ok := ReadGMETrack(path); ok {
		return g
	}
	f, err := os.Open(path)
	if err != nil {
		f, err = os.Open(decode.FindFile(path))
	}
	if err == nil {
		if m, err := tags.ReadFrom(f); err == nil {
			t.Title, t.Artist, t.Album = m.Title(), m.Artist(), m.Album()
			if t.Artist == "" {
				t.Artist = m.AlbumArtist()
			}
		}
		f.Close()
	}
	// Las etiquetas de su archivo aparte (<archivo>.tags.json), si lo tiene,
	// mandan: lo escribe audiofilus con los datos buenos (del RSS de un
	// podcast, de YouTube Music) para audios sin etiquetas o con etiquetas de
	// relleno ("Title Placeholder Episode ID: ..." en algunos podcasts).
	if b, err := os.ReadFile(path + ".tags.json"); err == nil {
		var side Track
		if json.Unmarshal(b, &side) == nil {
			if side.Title != "" {
				t.Title = side.Title
			}
			if side.Artist != "" {
				t.Artist = side.Artist
			}
			if side.Album != "" {
				t.Album = side.Album
			}
		}
	}
	if t.Title == "" {
		t.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return t
}
