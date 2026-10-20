package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Las favoritas y las valoraciones (data/favorites.json): de cada canción,
// si es favorita, sus estrellas (1 a 5) y sus datos, para poder listarlas
// aunque no esté en la biblioteca de ahora (una de un servidor, por ejemplo).

// Mark es lo que se sabe de una canción favorita o valorada.
type Mark struct {
	Fav    bool   `json:"fav,omitempty"`
	Rating int    `json:"rating,omitempty"` // 0 sin valorar
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	At     int64  `json:"t,omitempty"` // cuándo se marcó (para ordenarlas)
}

// Marks son las marcas de todas las canciones, guardadas en dataDir.
type Marks struct {
	file string

	mu sync.Mutex
	m  map[string]Mark
}

// LoadMarks lee las marcas (sin archivo, ninguna).
func LoadMarks(dataDir string) *Marks {
	ms := &Marks{file: filepath.Join(dataDir, "favorites.json"), m: map[string]Mark{}}
	if b, err := os.ReadFile(ms.file); err == nil {
		_ = json.Unmarshal(b, &ms.m)
	}
	return ms
}

// Get es la marca de path.
func (ms *Marks) Get(path string) Mark {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.m[path]
}

// Set cambia la marca de path (con sus datos) y la guarda. Sin favorita ni
// valoración, la canción sale del archivo.
func (ms *Marks) Set(path string, mk Mark) error {
	ms.mu.Lock()
	if mk.Fav && !ms.m[path].Fav {
		mk.At = time.Now().Unix()
	} else if mk.At == 0 {
		mk.At = ms.m[path].At
	}
	if !mk.Fav && mk.Rating == 0 {
		delete(ms.m, path)
	} else {
		ms.m[path] = mk
	}
	ms.mu.Unlock()
	return ms.save()
}

// Favorites son las rutas de las favoritas, de la última marcada a la
// primera.
func (ms *Marks) Favorites() []string {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	var out []string
	for p, mk := range ms.m {
		if mk.Fav {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := ms.m[out[i]], ms.m[out[j]]
		if a.At != b.At {
			return a.At > b.At
		}
		return out[i] < out[j]
	})
	return out
}

// Remote es una favorita o valoración que viene de un servidor.
type Remote struct {
	Path                 string
	Fav                  bool
	Rating               int
	Title, Artist, Album string
}

// Merge añade las favoritas y valoraciones que vienen de un servidor (lo de
// aquí manda: no se quita nada, y una valoración propia no se pisa). Guarda
// una sola vez, si algo cambió.
func (ms *Marks) Merge(rs []Remote) {
	ms.mu.Lock()
	changed := false
	for _, r := range rs {
		cur, ok := ms.m[r.Path]
		if (cur.Fav || !r.Fav) && (cur.Rating != 0 || r.Rating == 0) {
			continue
		}
		if r.Fav && !cur.Fav {
			cur.Fav, cur.At = true, time.Now().Unix()
		}
		if cur.Rating == 0 {
			cur.Rating = r.Rating
		}
		if !ok || cur.Title == "" {
			cur.Title, cur.Artist, cur.Album = r.Title, r.Artist, r.Album
		}
		ms.m[r.Path] = cur
		changed = true
	}
	ms.mu.Unlock()
	if changed {
		_ = ms.save()
	}
}

// save escribe el archivo.
func (ms *Marks) save() error {
	ms.mu.Lock()
	b, err := json.MarshalIndent(ms.m, "", "  ")
	ms.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ms.file), 0o755); err != nil {
		return err
	}
	tmp := ms.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ms.file)
}
