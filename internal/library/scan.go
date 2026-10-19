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

	"github.com/dhowden/tag"

	"github.com/juanvillacortac/audiofilus/internal/decode"
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
			return nil, err
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
		case decode.IsAudioFile(p):
			out = append(out, p)
		}
	}
	return out, nil
}

// scanDir lista los archivos de audio bajo root, saltando las carpetas ocultas.
func scanDir(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // lo que no se puede leer se salta
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if !d.IsDir() && decode.IsAudioFile(path) {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
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
		line = filepath.FromSlash(strings.ReplaceAll(line, `\`, "/"))
		if !filepath.IsAbs(line) {
			line = filepath.Join(dir, line)
		}
		if decode.IsAudioFile(line) {
			files = append(files, line)
		}
	}
	return files, sc.Err()
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
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
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
