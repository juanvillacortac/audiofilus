// Package history guarda lo que se escuchó (data/history.jsonl, una línea
// por escucha) y saca de ahí lo reciente, lo más escuchado y las
// estadísticas. Lo escribe el servicio, que es quien sabe qué sonó aunque la
// interfaz esté cerrada; la interfaz lo lee.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Listen es una escucha: una canción que sonó al menos la mitad (o cuatro
// minutos), la regla de Last.fm y ListenBrainz.
type Listen struct {
	At       int64   `json:"t"` // segundos Unix de cuando empezó
	Path     string  `json:"path"`
	Title    string  `json:"title,omitempty"`
	Artist   string  `json:"artist,omitempty"`
	Album    string  `json:"album,omitempty"`
	Duration float64 `json:"dur,omitempty"` // segundos
}

// File es el archivo del historial en la carpeta de datos.
func File(dataDir string) string { return filepath.Join(dataDir, "history.jsonl") }

var mu sync.Mutex

// Append añade una escucha al historial.
func Append(dataDir string, l Listen) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(File(dataDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Load lee el historial entero, de lo más viejo a lo más nuevo (sin
// historial, vacío). Las líneas rotas se saltan.
func Load(dataDir string) []Listen {
	f, err := os.Open(File(dataDir))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Listen
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var l Listen
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Path != "" {
			out = append(out, l)
		}
	}
	return out
}

// Recent son las últimas n canciones distintas que sonaron, de la más
// reciente a la más vieja.
func Recent(ls []Listen, n int) []Listen {
	seen := map[string]bool{}
	var out []Listen
	for i := len(ls) - 1; i >= 0 && len(out) < n; i-- {
		if !seen[ls[i].Path] {
			seen[ls[i].Path] = true
			out = append(out, ls[i])
		}
	}
	return out
}

// Count es algo (una canción, un artista, un disco) y cuántas veces sonó.
type Count struct {
	Name  string // el título, el artista o el disco
	Sub   string // el artista (de una canción o un disco)
	Path  string // la canción (en TopSongs)
	Plays int
}

// Stats son las estadísticas de un historial.
type Stats struct {
	Plays      int
	Hours      float64
	Songs      int // canciones distintas
	Artists    int
	Since      time.Time // la primera escucha
	TopSongs   []Count
	TopArtists []Count
	TopAlbums  []Count
}

// Compute saca las estadísticas de ls (los n primeros de cada lista) de lo
// escuchado desde since (cero: todo).
func Compute(ls []Listen, since time.Time, n int) Stats {
	var s Stats
	songs, artists, albums := map[string]*Count{}, map[string]*Count{}, map[string]*Count{}
	add := func(m map[string]*Count, key string, c Count) {
		if key == "" {
			return
		}
		if m[key] == nil {
			m[key] = &c
		}
		m[key].Plays++
	}
	for _, l := range ls {
		if l.At < since.Unix() {
			continue
		}
		if s.Plays == 0 {
			s.Since = time.Unix(l.At, 0)
		}
		s.Plays++
		s.Hours += l.Duration / 3600
		add(songs, l.Path, Count{Name: l.Title, Sub: l.Artist, Path: l.Path})
		add(artists, strings.ToLower(l.Artist), Count{Name: l.Artist})
		if l.Album != "" {
			add(albums, strings.ToLower(l.Artist+"\x00"+l.Album), Count{Name: l.Album, Sub: l.Artist})
		}
	}
	s.Songs, s.Artists = len(songs), len(artists)
	s.TopSongs, s.TopArtists, s.TopAlbums = top(songs, n), top(artists, n), top(albums, n)
	return s
}

// top son los n que más sonaron (a igualdad, por nombre).
func top(m map[string]*Count, n int) []Count {
	out := make([]Count, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
