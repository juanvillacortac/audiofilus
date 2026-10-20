package dlna

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// La biblioteca que se sirve: la que la interfaz ya escaneó y guardó en
// data/library.json (leer las etiquetas de cada archivo es lento en una
// portátil; el servicio no vuelve a escanear). Se relee si el archivo
// cambia. Las listas son las .m3u de las carpetas de música y de
// data/playlists, como en la interfaz.

// cacheEntry es una canción de data/library.json (los campos que usa la
// interfaz al escribirla, internal/ui/scan.go).
type cacheEntry struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Track       int    `json:"track,omitempty"`
	Pic         bool   `json:"pic,omitempty"`
}

// song es una canción servida.
type song struct {
	id                          string // "t" + hash de la ruta
	path                        string
	title, artist, album, alArt string
	track                       int
	size                        int64
	pic                         bool
}

// album es un álbum: sus canciones, en orden.
type album struct {
	id, title, artist string
	songs             []*song
}

// artist es un artista (el del álbum): sus álbumes.
type artistEntry struct {
	id, name string
	albums   []*album
}

// folder es una carpeta: sus subcarpetas y sus canciones.
type folder struct {
	id, name, path string
	subs           []*folder
	songs          []*song
}

// playlist es una lista .m3u.
type playlist struct {
	id, name string
	songs    []*song
}

// library es la biblioteca servida, con todo indexado por id.
type library struct {
	songs     []*song
	albums    []*album
	artists   []*artistEntry
	folders   []*folder // las raíces (las carpetas de música)
	playlists []*playlist
	byID      map[string]any
	update    uint32 // SystemUpdateID: cambia con cada relectura
}

// libSource lee la biblioteca de data/library.json y la relee si cambió.
type libSource struct {
	dataDir string
	mu      sync.Mutex
	lib     *library
	modTime time.Time
	checked time.Time
}

// get da la biblioteca al día (mira si el archivo cambió, como mucho cada
// par de segundos).
func (s *libSource) get() *library {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lib != nil && time.Since(s.checked) < 2*time.Second {
		return s.lib
	}
	s.checked = time.Now()
	p := filepath.Join(s.dataDir, "library.json")
	st, err := os.Stat(p)
	if err != nil {
		if s.lib == nil {
			s.lib = buildLibrary(nil, nil, nil, 1)
		}
		return s.lib
	}
	if s.lib != nil && st.ModTime().Equal(s.modTime) {
		return s.lib
	}
	var c struct {
		Dirs    []string     `json:"dirs"`
		Entries []cacheEntry `json:"entries"`
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	var upd uint32 = 1
	if s.lib != nil {
		upd = s.lib.update + 1
	}
	lists := findPlaylists(append(append([]string{}, c.Dirs...), filepath.Join(s.dataDir, "playlists")))
	s.lib = buildLibrary(c.Dirs, c.Entries, lists, upd)
	s.modTime = st.ModTime()
	return s.lib
}

// hashID es un id corto y estable para una ruta o un nombre.
func hashID(prefix, s string) string {
	h := sha1.Sum([]byte(s))
	return prefix + hex.EncodeToString(h[:8])
}

// buildLibrary arma la biblioteca de las entradas de la caché.
func buildLibrary(dirs []string, entries []cacheEntry, lists []string, update uint32) *library {
	l := &library{byID: map[string]any{}, update: update}
	byPath := map[string]*song{}
	albums := map[string]*album{}
	for _, e := range entries {
		// Lo remoto no es un archivo que servir; las pistas de un CUE o de
		// libgme sí (se sirven decodificadas, en WAV).
		if strings.Contains(e.Path, "://") {
			continue
		}
		s := &song{id: hashID("t", e.Path), path: e.Path, title: e.Title, artist: e.Artist, album: e.Album,
			alArt: e.AlbumArtist, track: e.Track, size: e.Size, pic: e.Pic}
		if s.title == "" {
			s.title = strings.TrimSuffix(filepath.Base(decode.FileOf(e.Path)), filepath.Ext(decode.FileOf(e.Path)))
		}
		if s.alArt == "" {
			s.alArt = s.artist
		}
		l.songs = append(l.songs, s)
		l.byID[s.id] = s
		byPath[s.path] = s
		key := strings.ToLower(s.alArt + "\x00" + s.album)
		al := albums[key]
		if al == nil {
			title := s.album
			if title == "" {
				title = "?"
			}
			al = &album{id: hashID("al", key), title: title, artist: s.alArt}
			albums[key] = al
			l.albums = append(l.albums, al)
			l.byID[al.id] = al
		}
		al.songs = append(al.songs, s)
	}
	sort.SliceStable(l.songs, func(i, j int) bool { return lessFold(l.songs[i].title, l.songs[j].title) })
	sort.SliceStable(l.albums, func(i, j int) bool { return lessFold(l.albums[i].title, l.albums[j].title) })
	arts := map[string]*artistEntry{}
	for _, al := range l.albums {
		sort.SliceStable(al.songs, func(i, j int) bool {
			if al.songs[i].track != al.songs[j].track {
				return al.songs[i].track < al.songs[j].track
			}
			return al.songs[i].path < al.songs[j].path
		})
		name := al.artist
		if name == "" {
			name = "?"
		}
		k := strings.ToLower(name)
		ar := arts[k]
		if ar == nil {
			ar = &artistEntry{id: hashID("ar", k), name: name}
			arts[k] = ar
			l.artists = append(l.artists, ar)
			l.byID[ar.id] = ar
		}
		ar.albums = append(ar.albums, al)
	}
	sort.SliceStable(l.artists, func(i, j int) bool { return lessFold(l.artists[i].name, l.artists[j].name) })
	// Las carpetas, desde cada carpeta de música.
	for _, root := range dirs {
		f := &folder{id: hashID("f", root), name: filepath.Base(root), path: root}
		l.folders = append(l.folders, f)
		l.byID[f.id] = f
	}
	for _, s := range l.songs {
		file := decode.FileOf(s.path)
		for _, root := range l.folders {
			rel, err := filepath.Rel(root.path, filepath.Dir(file))
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			f := root
			if rel != "." {
				for _, part := range strings.Split(rel, string(filepath.Separator)) {
					f = l.subFolder(f, part)
				}
			}
			f.songs = append(f.songs, s)
			break
		}
	}
	for _, f := range l.byID {
		if f, ok := f.(*folder); ok {
			sort.SliceStable(f.subs, func(i, j int) bool { return lessFold(f.subs[i].name, f.subs[j].name) })
			sort.SliceStable(f.songs, func(i, j int) bool { return f.songs[i].path < f.songs[j].path })
		}
	}
	for _, p := range lists {
		pl := &playlist{id: hashID("pl", p), name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))}
		for _, f := range readM3U(p) {
			if s := byPath[f]; s != nil {
				pl.songs = append(pl.songs, s)
			}
		}
		l.playlists = append(l.playlists, pl)
		l.byID[pl.id] = pl
	}
	return l
}

// subFolder es la subcarpeta name de f (creándola).
func (l *library) subFolder(f *folder, name string) *folder {
	for _, s := range f.subs {
		if s.name == name {
			return s
		}
	}
	s := &folder{id: hashID("f", filepath.Join(f.path, name)), name: name, path: filepath.Join(f.path, name)}
	f.subs = append(f.subs, s)
	l.byID[s.id] = s
	return s
}

// lessFold ordena sin distinguir mayúsculas.
func lessFold(a, b string) bool { return strings.ToLower(a) < strings.ToLower(b) }

// findPlaylists busca las .m3u/.m3u8 de dirs.
func findPlaylists(dirs []string) []string {
	var out []string
	for _, root := range dirs {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !d.IsDir() && (ext == ".m3u" || ext == ".m3u8") {
				out = append(out, p)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// readM3U lee las rutas de una lista (las relativas, contra su carpeta).
func readM3U(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	dir := filepath.Dir(path)
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = filepath.FromSlash(strings.ReplaceAll(line, `\`, "/"))
		if !filepath.IsAbs(line) {
			line = filepath.Join(dir, line)
		}
		out = append(out, line)
	}
	return out
}
