package ui

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/tags"
)

// entry es una canción en la caché de la biblioteca (data/library.json).
type entry struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ModTime     int64  `json:"mtime"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Track       int    `json:"track,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Year        int    `json:"year,omitempty"`
	Pic         bool   `json:"pic,omitempty"` // tiene portada incrustada
	// Cover es de dónde sacar la portada si no es el propio archivo (la de un
	// servidor remoto: "subsonic://<servidor>/cover/<id>").
	Cover string `json:"cover,omitempty"`
}

// libraryCache es data/library.json.
type libraryCache struct {
	Version int      `json:"v,omitempty"`
	Dirs    []string `json:"dirs"`
	Entries []entry  `json:"entries"`
}

// cacheVersion es la de las entradas de la caché: si la guardada es más
// vieja, las etiquetas se vuelven a leer una vez (la 2 trajo el género y el
// año, para las listas inteligentes; la 3, los varios artistas de una
// canción y las etiquetas de los FLAC con un ID3v2 delante).
const cacheVersion = 3

// Scan es un escaneo de la biblioteca en marcha. Leer las etiquetas de cada
// archivo es lento en una portátil, así que se guardan en data/library.json y
// la siguiente vez solo se leen los archivos nuevos o cambiados (por tamaño y
// fecha).
type Scan struct {
	Total atomic.Int64 // archivos encontrados
	Done  atomic.Int64 // archivos ya leídos

	mu   sync.Mutex
	lib  *Library
	err  error
	over bool
}

// StartScan escanea dirs en segundo plano.
func StartScan(paths config.Paths, dirs []string) *Scan {
	s := &Scan{}
	go s.run(paths, dirs)
	return s
}

// Result devuelve la biblioteca cuando el escaneo terminó (ok false mientras sigue).
func (s *Scan) Result() (lib *Library, err error, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lib, s.err, s.over
}

func (s *Scan) finish(lib *Library, err error) {
	s.mu.Lock()
	s.lib, s.err, s.over = lib, err, true
	s.mu.Unlock()
}

func (s *Scan) run(paths config.Paths, dirs []string) {
	cachePath := filepath.Join(paths.Data, "library.json")
	old := map[string]entry{}
	if b, err := os.ReadFile(cachePath); err == nil {
		var c libraryCache
		if json.Unmarshal(b, &c) == nil && c.Version >= cacheVersion {
			for _, e := range c.Entries {
				old[e.Path] = e
			}
		}
	}
	files, err := library.Expand(dirs)
	if err != nil {
		s.finish(nil, err)
		return
	}
	s.Total.Store(int64(len(files)))
	entries := make([]entry, 0, len(files))
	for _, p := range files {
		// Una pista de un CUE: la fecha y el tamaño son los de su archivo.
		file := decode.FileOf(p)
		info, err := os.Stat(file)
		if err != nil {
			s.Done.Add(1)
			continue
		}
		if e, ok := old[p]; ok && e.Size == info.Size() && e.ModTime == info.ModTime().Unix() {
			entries = append(entries, e)
			s.Done.Add(1)
			continue
		}
		entries = append(entries, readEntry(p, info))
		s.Done.Add(1)
	}
	// Si no se puede guardar (la tarjeta llena o en solo lectura), el
	// siguiente arranque escanea todo otra vez: queda en el log.
	if err := saveLibraryCache(paths.Data, cachePath, libraryCache{Version: cacheVersion, Dirs: dirs, Entries: entries}); err != nil {
		log.Printf("library cache: %v", err)
	}
	lib := buildLibrary(entries, dirs)
	lib.Roots = dirs
	// Las listas de la música y las propias (data/playlists, playlists.go).
	lib.addPlaylists(library.FindPlaylists(append(slices.Clone(dirs), filepath.Join(paths.Data, "playlists"))))
	s.finish(lib, nil)
}

// saveLibraryCache guarda c en cachePath (dentro de dir) de una vez: en un
// temporal y luego renombrándolo, para no dejar una caché a medias.
func saveLibraryCache(dir, cachePath string, c libraryCache) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath)
}

// loadCachedLibrary arma la biblioteca de dirs solo con data/library.json,
// sin mirar los archivos: al arrancar se enseña esta enseguida y el escaneo
// (que en una portátil recién encendida, con la tarjeta por FUSE, tarda) la
// pone al día por detrás. nil si no hay caché de esas carpetas. Las listas son
// solo las propias (data/playlists); las de la música llegan con el escaneo.
func loadCachedLibrary(paths config.Paths, dirs []string) *Library {
	b, err := os.ReadFile(filepath.Join(paths.Data, "library.json"))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("library cache: %v", err)
		}
		return nil
	}
	var c libraryCache
	// Una caché de una versión vieja también vale para enseñarla: le faltan
	// datos (el género, los varios artistas) que el escaneo de fondo pone al
	// día al releer las etiquetas, en vez de tapar la biblioteca con la
	// pantalla de escaneo tras actualizar.
	if err := json.Unmarshal(b, &c); err != nil {
		log.Printf("library cache: corrupt (%d bytes): %v", len(b), err)
		return nil
	}
	if !slices.Equal(c.Dirs, dirs) {
		log.Printf("library cache: music folders changed (%q, now %q)", c.Dirs, dirs)
		return nil
	}
	if len(c.Entries) == 0 {
		return nil
	}
	lib := buildLibrary(c.Entries, dirs)
	lib.Roots = dirs
	lib.addPlaylists(library.FindPlaylists([]string{filepath.Join(paths.Data, "playlists")}))
	return lib
}

// readEntry lee las etiquetas de un archivo.
func readEntry(p string, info os.FileInfo) (e entry) {
	e = entry{Path: p, Size: info.Size(), ModTime: info.ModTime().Unix()}
	// Música de consola (libgme): sus datos salen del propio archivo.
	if t, n, ok := library.ReadGMETrack(p); ok {
		e.Title, e.Artist, e.Album, e.Track = t.Title, t.Artist, t.Album, n
		return e
	}
	// Una pista de un CUE: el título y el número de la hoja, el resto (y la
	// portada) de las etiquetas de su archivo, si tiene.
	file, _, _, cue := decode.SplitCuePath(p)
	if cue {
		defer func() {
			if t, n, ok := library.ReadCueEntry(p); ok {
				e.Title, e.Track = t.Title, n
				e.Artist = orDefault(t.Artist, e.Artist)
				e.Album = orDefault(t.Album, e.Album)
			}
		}()
	} else {
		file = p
	}
	f, err := os.Open(file)
	if err != nil {
		return e
	}
	defer f.Close()
	m, err := tags.ReadFrom(f)
	if err != nil {
		return e
	}
	e.Title, e.Artist, e.AlbumArtist, e.Album = m.Title(), m.Artist(), m.AlbumArtist(), m.Album()
	e.Track, _ = m.Track()
	e.Genre, e.Year = m.Genre(), m.Year()
	e.Pic = m.Picture() != nil
	return e
}
