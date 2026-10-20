package ui

import (
	"context"
	"time"

	"bytes"
	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"image"
	"image/color"
	_ "image/jpeg" // portadas JPEG
	_ "image/png"  // portadas PNG
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dhowden/tag"
	xdraw "golang.org/x/image/draw"

	"github.com/juanvillacortac/audiofilus/internal/library"
)

// Song es una canción con sus etiquetas.
type Song struct {
	Path, Title, Artist, Album string
	Track                      int
}

// Album agrupa canciones y su portada. La portada la prepara el cargador en
// segundo plano (ver covers.go).
type Album struct {
	Title, Artist string
	Songs         []Song
	coverPath     string // archivo del que sacar la portada; vacío si no tiene
	stray         bool   // no está en la biblioteca (lo que suena de YouTube Music)

	mu       sync.Mutex
	decoded  bool // ya se leyó la portada una vez (para sus colores)
	accent   [2]color.RGBA
	hasColor bool // accent ya sale de la portada

	scaled  map[int]*coverImage // solo se toca desde el hilo de la interfaz
	missing bool                // no tiene portada: se dibuja la de relleno
}

// Library es la biblioteca: los álbumes y todas las canciones.
type Library struct {
	Albums    []*Album
	Songs     []Song
	Artists   []*Artist
	Playlists []*Playlist
	Roots     []string          // las carpetas de música
	byPath    map[string]*Album // el álbum de cada canción
	song      map[string]Song   // cada canción por su ruta
}

// Artist son los álbumes de un artista (por el artista del álbum).
type Artist struct {
	Name   string
	Albums []*Album
	Songs  int
}

// AllSongs son las canciones del artista, álbum por álbum.
func (ar *Artist) AllSongs() []Song {
	var out []Song
	for _, al := range ar.Albums {
		out = append(out, al.Songs...)
	}
	return out
}

// Playlist es una lista .m3u, con las canciones que están en la biblioteca.
type Playlist struct {
	Name, Path string
	Songs      []Song
	Remote     bool // de un servidor: se oye, pero no se edita desde aquí
}

// Song devuelve la canción de una ruta, si está en la biblioteca.
func (l *Library) Song(path string) (Song, bool) {
	s, ok := l.song[path]
	return s, ok
}

// Folder devuelve las subcarpetas (con música dentro) y las canciones de dir.
func (l *Library) Folder(dir string) (dirs []string, songs []Song) {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	seen := map[string]bool{}
	for _, s := range l.Songs {
		if !strings.HasPrefix(s.Path, prefix) {
			continue
		}
		rest := s.Path[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			sub := prefix + rest[:i]
			if !seen[sub] {
				seen[sub] = true
				dirs = append(dirs, sub)
			}
			continue
		}
		songs = append(songs, s)
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j]) })
	sort.SliceStable(songs, func(i, j int) bool { return strings.ToLower(songs[i].Path) < strings.ToLower(songs[j].Path) })
	return dirs, songs
}

// AlbumOf devuelve el álbum de una canción y su posición en él.
func (l *Library) AlbumOf(path string) (*Album, int) {
	al := l.byPath[path]
	if al == nil {
		return nil, -1
	}
	for i, s := range al.Songs {
		if s.Path == path {
			return al, i
		}
	}
	return al, -1
}

// buildLibrary agrupa las canciones escaneadas por álbum (álbum + artista del
// álbum) y las ordena: los álbumes por artista y título, las canciones de cada
// álbum por número de pista.
func buildLibrary(entries []entry) *Library {
	lib := &Library{byPath: map[string]*Album{}, song: map[string]Song{}}
	byKey := map[string]*Album{}
	for _, e := range entries {
		s := Song{Path: e.Path, Title: e.Title, Artist: firstArtist(e.Artist), Album: e.Album, Track: e.Track}
		file := audioFile(e.Path)
		if s.Title == "" {
			s.Title = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		}
		if s.Album == "" {
			s.Album = "Sin álbum"
		}
		albumArtist := firstArtist(e.AlbumArtist)
		if albumArtist == "" {
			albumArtist = s.Artist
		}
		key := strings.ToLower(albumArtist + "\x00" + s.Album)
		a := byKey[key]
		if a == nil {
			a = &Album{Title: s.Album, Artist: albumArtist, scaled: map[int]*coverImage{}}
			byKey[key] = a
			lib.Albums = append(lib.Albums, a)
		}
		if e.Cover != "" && a.coverPath == "" {
			a.coverPath = e.Cover
		} else if e.Pic && a.coverPath == "" {
			a.coverPath = file
		}
		a.Songs = append(a.Songs, s)
		lib.Songs = append(lib.Songs, s)
		lib.byPath[s.Path] = a
		lib.song[s.Path] = s
	}
	sort.Slice(lib.Albums, func(i, j int) bool {
		ai, aj := lib.Albums[i], lib.Albums[j]
		if !strings.EqualFold(ai.Artist, aj.Artist) {
			return strings.ToLower(ai.Artist) < strings.ToLower(aj.Artist)
		}
		return strings.ToLower(ai.Title) < strings.ToLower(aj.Title)
	})
	for _, a := range lib.Albums {
		sort.SliceStable(a.Songs, func(i, j int) bool { return a.Songs[i].Track < a.Songs[j].Track })
	}
	// Los artistas salen de los álbumes, que ya están ordenados por artista.
	var cur *Artist
	for _, al := range lib.Albums {
		if cur == nil || !strings.EqualFold(cur.Name, al.Artist) {
			cur = &Artist{Name: al.Artist}
			lib.Artists = append(lib.Artists, cur)
		}
		cur.Albums = append(cur.Albums, al)
		cur.Songs += len(al.Songs)
	}
	return lib
}

// addPlaylists lee las listas .m3u y se queda con las canciones que están en
// la biblioteca.
func (l *Library) addPlaylists(paths []string) {
	for _, p := range paths {
		files, err := library.Expand([]string{p})
		if err != nil {
			continue
		}
		pl := &Playlist{Name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), Path: p}
		for _, f := range files {
			if s, ok := l.song[f]; ok {
				pl.Songs = append(pl.Songs, s)
			}
		}
		l.Playlists = append(l.Playlists, pl)
	}
}

// firstArtist se queda con el primer artista de una lista ("A, B, C" → "A").
func firstArtist(s string) string {
	if i := strings.Index(s, ", "); i > 0 {
		return s[:i]
	}
	return s
}

// HasCover dice si el álbum tiene alguna canción con portada incrustada.
func (a *Album) HasCover() bool { return a.coverPath != "" }

// decodeCover lee y decodifica la portada incrustada; la primera vez también
// saca sus colores. No se guarda la imagen original: decodificada ocupa
// varios MB (una de 1000×1000 son 4 MB), y con cientos de álbumes no cabría en
// una portátil de 512 MB. Corre en los trabajadores del cargador.
func (a *Album) decodeCover() image.Image {
	if a.coverPath == "" {
		return nil
	}
	if remote.IsRemote(a.coverPath) {
		return a.decodeRemoteCover()
	}
	f, err := os.Open(a.coverPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var img image.Image
	switch strings.ToLower(filepath.Ext(a.coverPath)) {
	case ".jpg", ".jpeg", ".png", ".img":
		// Una imagen suelta (la miniatura de YouTube Music, ytm.Fetch).
		img, _, err = image.Decode(f)
	default:
		m, terr := tag.ReadFrom(f)
		if terr != nil || m.Picture() == nil {
			return nil
		}
		img, _, err = image.Decode(bytes.NewReader(m.Picture().Data))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.decoded {
		a.decoded = true
		if err == nil {
			a.accent = accentColors(img)
			a.hasColor = true
		}
	}
	if err != nil {
		return nil
	}
	return img
}

// Accent son los dos colores de la portada para teñir la interfaz; ok es false
// si todavía no se leyó la portada.
func (a *Album) Accent() (c1, c2 color.RGBA, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.hasColor {
		return DefaultTheme.Accent, DefaultTheme.Accent2, a.decoded
	}
	return a.accent[0], a.accent[1], true
}

// scaleCover reduce la portada a size×size, recortada al cuadrado, y la trama.
func scaleCover(src image.Image, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	r := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	r.Max = r.Min.Add(image.Pt(side, side))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, r, xdraw.Src, nil)
	ditherCover(dst)
	return dst
}

// bayer4 es la matriz de Bayer de 4×4, de 0 a 15.
var bayer4 = [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

// ditherCover reduce cada canal a 4 bits con trama de Bayer: la portada sigue
// reconociéndose pero con la textura de una consola de 16 bits.
func ditherCover(img *image.RGBA) {
	const levels = 15
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			i := y*img.Stride + x*4
			t := (float64(bayer4[y&3][x&3]) + 0.5) / 16
			for c := 0; c < 3; c++ {
				v := float64(img.Pix[i+c]) / 255 * levels
				q := int(v)
				if v-float64(q) > t {
					q++
				}
				img.Pix[i+c] = uint8(q * 255 / levels)
			}
			img.Pix[i+3] = 0xff
		}
	}
}

// accentColors elige los dos colores más vivos de la portada: el promedio de
// los píxeles más saturados, y el de los más claros de otro tono.
func accentColors(img image.Image) [2]color.RGBA {
	small := image.NewRGBA(image.Rect(0, 0, 24, 24))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	type px struct {
		c   color.RGBA
		sat float64
		hue float64
	}
	var all []px
	for i := 0; i+3 < len(small.Pix); i += 4 {
		c := color.RGBA{small.Pix[i], small.Pix[i+1], small.Pix[i+2], 0xff}
		h, s, v := hsv(c)
		all = append(all, px{c, s * v, h})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sat > all[j].sat })
	if len(all) == 0 || all[0].sat < 0.08 {
		return [2]color.RGBA{DefaultTheme.Accent, DefaultTheme.Accent2}
	}
	a := brighten(all[0].c)
	b := DefaultTheme.Accent2
	for _, p := range all[1:] {
		if d := math.Abs(p.hue - all[0].hue); d > 0.15 && d < 0.85 && p.sat > 0.15 {
			b = brighten(p.c)
			break
		}
	}
	return [2]color.RGBA{a, b}
}

// brighten lleva un color a un brillo legible sobre el fondo oscuro.
func brighten(c color.RGBA) color.RGBA {
	h, s, _ := hsv(c)
	return fromHSV(h, math.Min(1, math.Max(s, 0.55)), 0.95)
}

// coverStore es la caché de lo remoto, para las portadas de los servidores
// (la pone la interfaz al arrancar).
var coverStore *remote.Store

// decodeRemoteCover baja (o lee de la caché) la portada de un servidor.
func (a *Album) decodeRemoteCover() image.Image {
	if coverStore == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := coverStore.Cover(ctx, a.coverPath, 300)
	var img image.Image
	if err == nil {
		img, _, err = image.Decode(bytes.NewReader(b))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.decoded && err == nil {
		a.decoded = true
		a.accent = accentColors(img)
		a.hasColor = true
	}
	if err != nil {
		return nil
	}
	return img
}

// audioFile es el archivo de una canción: el mismo path, o el del disco
// entero si es una pista de un CUE (ver decode.CuePath).
func audioFile(path string) string {
	if f, _, _, ok := decode.SplitCuePath(path); ok {
		return f
	}
	return path
}
