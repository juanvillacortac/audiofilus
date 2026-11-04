package esdata

import (
	"bytes"
	"encoding/gob"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// El caché de los juegos: leer los gamelist.xml y, en la consola, mirar que
// existe cada ROM y buscar las que no están en ellos son miles de accesos a
// la tarjeta, y la interfaz se cierra con cada juego que se lanza (al volver
// se leería todo otra vez). El caché guarda los juegos de cada sistema con
// la huella de lo que se leyó: la fecha y el tamaño de su gamelist.xml y la
// fecha de cada carpeta de sus ROMs (que cambia al añadir, borrar o
// renombrar algo dentro). Si nada cambió, los juegos salen del caché; si
// cambió algo de un sistema, solo ese se vuelve a leer.

// cacheVersion cambia cuando cambia lo que se guarda (un caché de otra
// versión se descarta).
const cacheVersion = 2

// GameCache es el caché de los juegos de todos los sistemas.
type GameCache struct {
	Version int
	Systems map[string]*CachedSystem
	dirty   bool
}

// CachedSystem son los juegos de un sistema con la huella de lo que se leyó.
type CachedSystem struct {
	Key      string           // la definición del sistema (carpeta y extensiones)
	Gamelist Stamp            // su gamelist.xml
	Dirs     map[string]int64 // las carpetas de ROMs → su fecha (UnixNano)
	Games    []Game
}

// Stamp es la huella de un archivo: su fecha y su tamaño (-1 si no existe).
type Stamp struct {
	ModTime int64
	Size    int64
}

// LoadGameCache lee el caché de path; si no está o no sirve, uno vacío.
func LoadGameCache(path string) *GameCache {
	c := &GameCache{Version: cacheVersion, Systems: map[string]*CachedSystem{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var old GameCache
	if gob.NewDecoder(bytes.NewReader(b)).Decode(&old) != nil || old.Version != cacheVersion || old.Systems == nil {
		return c
	}
	return &old
}

// Dirty dice si el caché cambió desde que se leyó (y hay que guardarlo).
func (c *GameCache) Dirty() bool { return c.dirty }

// Save guarda el caché en path (en un temporal que luego se renombra: un
// corte a medias no deja un caché roto).
func (c *GameCache) Save(path string) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	c.dirty = false
	return nil
}

// Prune quita del caché los sistemas que no están en keep.
func (c *GameCache) Prune(keep map[string]bool) {
	for name := range c.Systems {
		if !keep[name] {
			delete(c.Systems, name)
			c.dirty = true
		}
	}
}

// LoadGamesCached es LoadGames con el caché c: si lo que se leyó la última
// vez no cambió, sus juegos; si no, los lee y los apunta en c. hit dice si
// salieron del caché.
func (r Root) LoadGamesCached(s *System, c *GameCache) (games []Game, hit bool) {
	key := s.Path + "|" + strings.Join(s.Extensions, " ")
	if cs := c.Systems[s.Name]; cs != nil && cs.Key == key && r.fresh(s, cs) {
		return cs.Games, true
	}
	// La huella se toma antes de leer los juegos (si algo cambia mientras,
	// la próxima vez no coincidirá); las carpetas, que dependen de dónde
	// están los juegos, después, con la fecha que ya tenían al leerlos.
	cs := &CachedSystem{Key: key, Gamelist: r.stamp(filepath.Join(s.Path, "gamelist.xml"))}
	before := time.Now()
	cs.Games = r.LoadGames(s)
	cs.Dirs = r.romDirs(s, cs.Games)
	for d, t := range cs.Dirs {
		if t > before.UnixNano() {
			cs.Dirs[d] = 0 // cambió mientras se leía: que no coincida
		}
	}
	c.Systems[s.Name] = cs
	c.dirty = true
	return cs.Games, false
}

// fresh dice si nada de lo que se leyó para cs cambió.
func (r Root) fresh(s *System, cs *CachedSystem) bool {
	if r.stamp(filepath.Join(s.Path, "gamelist.xml")) != cs.Gamelist {
		return false
	}
	for dir, t := range cs.Dirs {
		info, err := os.Stat(r.Path(dir))
		if err != nil || !info.IsDir() || info.ModTime().UnixNano() != t {
			return false
		}
	}
	return true
}

// stamp es la huella del archivo p.
func (r Root) stamp(p string) Stamp {
	info, err := os.Stat(r.Path(p))
	if err != nil {
		return Stamp{Size: -1}
	}
	return Stamp{ModTime: info.ModTime().UnixNano(), Size: info.Size()}
}

// romDirs son las carpetas de ROMs del sistema que hay que vigilar, con su
// fecha: la suya y, de las de dentro, las que tienen algún juego de games
// (y las que hay en medio). Un juego nuevo en otra subcarpeta cambia la
// fecha de la carpeta que la contiene, que también se vigila: no hace falta
// mirar las miles de carpetas que no tienen juegos. Fuera de la consola no
// se mira la carpeta (LoadGames tampoco).
func (r Root) romDirs(s *System, games []Game) map[string]int64 {
	dirs := map[string]int64{}
	if !r.Live() {
		return dirs
	}
	base := path.Clean(s.Path)
	add := func(d string) {
		if _, ok := dirs[d]; ok {
			return
		}
		if info, err := os.Stat(r.Path(d)); err == nil && info.IsDir() {
			dirs[d] = info.ModTime().UnixNano()
		}
	}
	add(base)
	for _, g := range games {
		for d := path.Dir(g.Path); strings.HasPrefix(d, base+"/"); d = path.Dir(d) {
			add(d)
		}
	}
	return dirs
}
