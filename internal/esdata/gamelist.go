package esdata

import (
	"encoding/xml"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Game es un juego de un sistema: lo que ES guarda de él en gamelist.xml. Las
// rutas son absolutas, como las ve la consola (Root.Path las lleva a la copia).
type Game struct {
	Path        string // el archivo del juego
	Name        string
	Desc        string
	Image       string // captura
	Thumbnail   string // la caja
	Marquee     string // el logo
	Video       string
	Manual      string
	ReleaseDate time.Time
	Developer   string
	Publisher   string
	Genre       string
	Players     string
	Rating      float64 // 0..1
	PlayCount   int
	LastPlayed  time.Time
	Favorite    bool
}

// File es el nombre del archivo del juego: el de las claves por juego de
// knulli.conf (gba["juego.zip"].core).
func (g *Game) File() string { return path.Base(g.Path) }

// Year es el año de salida, o "" si no se sabe.
func (g *Game) Year() string {
	if g.ReleaseDate.IsZero() {
		return ""
	}
	return strconv.Itoa(g.ReleaseDate.Year())
}

// xmlGame es un <game> tal como viene en el archivo.
type xmlGame struct {
	Path        string `xml:"path"`
	Name        string `xml:"name"`
	Desc        string `xml:"desc"`
	Image       string `xml:"image"`
	Thumbnail   string `xml:"thumbnail"`
	Marquee     string `xml:"marquee"`
	Video       string `xml:"video"`
	Manual      string `xml:"manual"`
	ReleaseDate string `xml:"releasedate"`
	Developer   string `xml:"developer"`
	Publisher   string `xml:"publisher"`
	Genre       string `xml:"genre"`
	Players     string `xml:"players"`
	Rating      string `xml:"rating"`
	PlayCount   string `xml:"playcount"`
	LastPlayed  string `xml:"lastplayed"`
	Favorite    string `xml:"favorite"`
	Hidden      string `xml:"hidden"`
}

// esTime lee una fecha de ES (20060727T000000).
func esTime(s string) time.Time {
	t, err := time.Parse("20060102T150405", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// resolve lleva una ruta del gamelist ("./images/x.png") a absoluta, desde la
// carpeta del sistema.
func resolve(dir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") {
		return p
	}
	return path.Join(dir, p)
}

// LoadGames lee los juegos del sistema s, ordenados por nombre: los de su
// gamelist.xml (sin los ocultos) y, en la consola, también los archivos de
// su carpeta con una extensión del sistema que no estén en él (como ES), y
// sin los del gamelist que ya no existen.
func (r Root) LoadGames(s *System) []Game {
	dir := s.Path
	var games []Game
	seen := map[string]bool{}
	if b, err := os.ReadFile(r.Path(path.Join(dir, "gamelist.xml"))); err == nil {
		var doc struct {
			Games []xmlGame `xml:"game"`
		}
		if xml.Unmarshal(b, &doc) == nil {
			for _, x := range doc.Games {
				if x.Hidden == "true" || strings.TrimSpace(x.Path) == "" {
					continue
				}
				g := Game{
					Path: resolve(dir, x.Path), Name: strings.TrimSpace(x.Name), Desc: strings.TrimSpace(x.Desc),
					Image: resolve(dir, x.Image), Thumbnail: resolve(dir, x.Thumbnail), Marquee: resolve(dir, x.Marquee),
					Video: resolve(dir, x.Video), Manual: resolve(dir, x.Manual),
					ReleaseDate: esTime(x.ReleaseDate), Developer: strings.TrimSpace(x.Developer),
					Publisher: strings.TrimSpace(x.Publisher), Genre: strings.TrimSpace(x.Genre),
					Players: strings.TrimSpace(x.Players), LastPlayed: esTime(x.LastPlayed),
					Favorite: x.Favorite == "true",
				}
				g.Rating, _ = strconv.ParseFloat(strings.TrimSpace(x.Rating), 64)
				g.PlayCount, _ = strconv.Atoi(strings.TrimSpace(x.PlayCount))
				if g.Name == "" {
					g.Name = stem(g.Path)
				}
				if r.Live() {
					if _, err := os.Stat(g.Path); err != nil {
						continue
					}
				}
				seen[g.Path] = true
				games = append(games, g)
			}
		}
	}
	if r.Live() {
		games = append(games, r.scanFiles(s, seen)...)
	}
	sort.SliceStable(games, func(i, j int) bool {
		return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name)
	})
	return games
}

// scanFiles son los archivos de la carpeta del sistema (y sus subcarpetas,
// salvo las de medios) con una de sus extensiones que no están en seen.
func (r Root) scanFiles(s *System, seen map[string]bool) []Game {
	exts := map[string]bool{}
	for _, e := range s.Extensions {
		exts[e] = true
	}
	var out []Game
	root := r.Path(s.Path)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && !descends(s, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !exts[strings.ToLower(filepath.Ext(p))] || seen[p] {
			return nil
		}
		out = append(out, Game{Path: p, Name: stem(p)})
		return nil
	})
	return out
}

// descends dice si se buscan juegos dentro de la subcarpeta name del
// sistema s: no en las de medios, y en Ports en ninguna (los ports son los
// .sh de su carpeta; cada subcarpeta son los datos de uno, con miles de
// archivos, y la de Audiofilus cambia sin parar).
func descends(s *System, name string) bool {
	if s.Name == "ports" {
		return false
	}
	switch name {
	case "images", "videos", "manuals", "media", "downloaded_images":
		return false
	}
	return true
}

// stem es el nombre de un archivo sin la carpeta ni la extensión.
func stem(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, path.Ext(b))
}
