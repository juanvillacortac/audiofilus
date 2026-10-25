package mpd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
)

// La biblioteca que se sirve es la que la interfaz ya escaneó y guardó en
// data/library.json (como la de DLNA): el servicio no vuelve a leer
// etiquetas. Cada canción tiene su URI de MPD, su ruta relativa a la carpeta
// de música con "/" (con varias carpetas de música, cada una es un
// directorio de la raíz, con su nombre). Las pistas dentro de un archivo (un
// CUE, una de libgme) son "<archivo>/track0001", como en MPD.

// cacheEntry es una canción de data/library.json (los campos que escribe la
// interfaz, internal/ui/scan.go).
type cacheEntry struct {
	Path        string `json:"path"`
	ModTime     int64  `json:"mtime"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Track       int    `json:"track,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Year        int    `json:"year,omitempty"`
	Pic         bool   `json:"pic,omitempty"`
}

// song es una canción de la biblioteca.
type song struct {
	uri, path string
	tags      Tags
	mtime     time.Time
	pic       bool
}

// dir es un directorio de la biblioteca: sus subdirectorios y canciones.
type dir struct {
	uri   string // "" en la raíz
	subs  []*dir
	songs []*song
	mtime time.Time
}

// storedList es una lista guardada (.m3u/.m3u8).
type storedList struct {
	name, file string
	mtime      time.Time
}

// index es la biblioteca servida.
type index struct {
	songs  []*song
	root   *dir
	dirs   map[string]*dir
	byURI  map[string]*song
	byPath map[string]*song
	lists  []storedList
	roots  []string  // las carpetas de música
	when   time.Time // cuándo se leyó (db_update)
}

// libSource lee la biblioteca de data/library.json y la relee si cambió.
type libSource struct {
	dataDir string
	mu      sync.Mutex
	idx     *index
	modTime time.Time
	checked time.Time
}

// listsDir es donde van las listas que se guardan desde MPD (las mismas de
// la interfaz).
func (s *libSource) listsDir() string { return filepath.Join(s.dataDir, "playlists") }

// get da la biblioteca al día (mira si el archivo cambió, como mucho cada
// par de segundos).
func (s *libSource) get() *index {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx != nil && time.Since(s.checked) < 2*time.Second {
		return s.idx
	}
	s.checked = time.Now()
	p := filepath.Join(s.dataDir, "library.json")
	st, err := os.Stat(p)
	if err != nil {
		if s.idx == nil {
			s.idx = buildIndex(nil, nil, s.listsDir())
		}
		return s.idx
	}
	if s.idx != nil && st.ModTime().Equal(s.modTime) {
		return s.idx
	}
	var c struct {
		Dirs    []string     `json:"dirs"`
		Entries []cacheEntry `json:"entries"`
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	s.idx = buildIndex(c.Dirs, c.Entries, s.listsDir())
	s.modTime = st.ModTime()
	return s.idx
}

// reloadLists vuelve a buscar las listas guardadas (tras guardar o borrar
// una desde MPD).
func (s *libSource) reloadLists() {
	idx := s.get()
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *idx
	cp.lists = findLists(idx.roots, s.listsDir())
	s.idx = &cp
}

// buildIndex arma la biblioteca de las entradas de la caché.
func buildIndex(roots []string, entries []cacheEntry, listsDir string) *index {
	idx := &index{root: &dir{}, dirs: map[string]*dir{}, byURI: map[string]*song{}, byPath: map[string]*song{}, when: time.Now()}
	idx.dirs[""] = idx.root
	idx.roots = slices.Clone(roots)
	// El prefijo de cada carpeta de música: nada si es una sola; su nombre
	// (sin repetir) si son varias.
	prefix := map[string]string{}
	used := map[string]bool{}
	for _, r := range roots {
		if len(roots) == 1 {
			prefix[r] = ""
			continue
		}
		name := filepath.Base(r)
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s (%d)", filepath.Base(r), i)
		}
		used[name] = true
		prefix[r] = name + "/"
	}
	// Las pistas dentro de un mismo archivo, numeradas en su orden.
	virtual := map[string][]cacheEntry{}
	for _, e := range entries {
		if decode.IsVirtualPath(e.Path) {
			f := decode.FileOf(e.Path)
			virtual[f] = append(virtual[f], e)
		}
	}
	sub := map[string]int{}
	for _, es := range virtual {
		slices.SortStableFunc(es, func(a, b cacheEntry) int {
			return cmp.Or(cmp.Compare(a.Track, b.Track), cmp.Compare(virtualStart(a.Path), virtualStart(b.Path)))
		})
		for i, e := range es {
			sub[e.Path] = i + 1
		}
	}
	for _, e := range entries {
		// Lo remoto no está en ninguna carpeta.
		if strings.Contains(e.Path, "://") {
			continue
		}
		file := decode.FileOf(e.Path)
		uri := ""
		for _, r := range roots {
			rel, err := filepath.Rel(r, file)
			if err == nil && !strings.HasPrefix(rel, "..") {
				uri = prefix[r] + filepath.ToSlash(rel)
				break
			}
		}
		if uri == "" {
			continue
		}
		if n := sub[e.Path]; n > 0 {
			uri += fmt.Sprintf("/track%04d", n)
		}
		s := &song{uri: uri, path: e.Path, pic: e.Pic, tags: Tags{Title: e.Title, Artist: e.Artist,
			AlbumArtist: e.AlbumArtist, Album: e.Album, Genre: e.Genre}}
		if e.Track > 0 {
			s.tags.Track = strconv.Itoa(e.Track)
		}
		if e.Year > 0 {
			s.tags.Date = strconv.Itoa(e.Year)
		}
		if s.tags.Title == "" {
			s.tags.Title = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		}
		if e.ModTime > 0 {
			s.mtime = time.Unix(e.ModTime, 0)
		}
		if idx.byURI[uri] != nil {
			continue
		}
		idx.songs = append(idx.songs, s)
		idx.byURI[uri] = s
		idx.byPath[s.path] = s
		d := idx.dirFor(path.Dir(uri))
		d.songs = append(d.songs, s)
		for x := d; x != nil; x = idx.dirs[parentURI(x.uri)] {
			if s.mtime.After(x.mtime) {
				x.mtime = s.mtime
			}
			if x.uri == "" {
				break
			}
		}
	}
	slices.SortFunc(idx.songs, func(a, b *song) int { return strings.Compare(a.uri, b.uri) })
	for _, d := range idx.dirs {
		slices.SortFunc(d.subs, func(a, b *dir) int { return strings.Compare(a.uri, b.uri) })
		slices.SortFunc(d.songs, func(a, b *song) int { return strings.Compare(a.uri, b.uri) })
	}
	idx.lists = findLists(roots, listsDir)
	return idx
}

// virtualStart es lo que ordena dos pistas del mismo archivo sin número:
// el inicio de la de un CUE, o el número de canción de libgme.
func virtualStart(p string) float64 {
	if _, start, _, ok := decode.SplitCuePath(p); ok {
		return start.Seconds()
	}
	if _, n, ok := decode.SplitGMEPath(p); ok {
		return float64(n)
	}
	return 0
}

// parentURI es el directorio padre de uri ("" para la raíz).
func parentURI(uri string) string {
	p := path.Dir(uri)
	if p == "." || p == "/" {
		return ""
	}
	return p
}

// dirFor es el directorio uri (creándolo con sus padres).
func (idx *index) dirFor(uri string) *dir {
	if uri == "." || uri == "/" {
		uri = ""
	}
	if d := idx.dirs[uri]; d != nil {
		return d
	}
	parent := idx.dirFor(parentURI(uri))
	d := &dir{uri: uri}
	parent.subs = append(parent.subs, d)
	idx.dirs[uri] = d
	return d
}

// walk llama a fn con cada canción de d y de sus subdirectorios, en orden.
func (d *dir) walk(fn func(*song)) {
	for _, s := range d.songs {
		fn(s)
	}
	for _, sub := range d.subs {
		sub.walk(fn)
	}
}

// findLists busca las listas guardadas: las de las carpetas de música y las
// de data/playlists (las de la interfaz).
func findLists(roots []string, listsDir string) []storedList {
	var out []storedList
	seen := map[string]bool{}
	for _, f := range library.FindPlaylists(append(slices.Clone(roots), listsDir)) {
		name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		if seen[name] {
			continue
		}
		seen[name] = true
		l := storedList{name: name, file: f}
		if st, err := os.Stat(f); err == nil {
			l.mtime = st.ModTime()
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b storedList) int { return strings.Compare(a.name, b.name) })
	return out
}

// list es la lista guardada name.
func (idx *index) list(name string) (storedList, bool) {
	for _, l := range idx.lists {
		if l.name == name {
			return l, true
		}
	}
	return storedList{}, false
}
