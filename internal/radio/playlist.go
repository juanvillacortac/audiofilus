package radio

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Las listas de emisoras propias: archivos .m3u, .m3u8 y .pls (los de
// IPTV y radio por internet: "#EXTINF:-1 tvg-logo=...,Nombre" y la dirección
// debajo, o "File1=", "Title1=") en la carpeta radio/ junto a la app. Cada
// dirección es una emisora; su UUID sale de la dirección, así se puede
// marcar como favorita y escuchar como las del directorio.

// PlaylistExtensions son las extensiones de las listas de emisoras.
var PlaylistExtensions = []string{".m3u", ".m3u8", ".pls"}

// FromURL es una emisora a partir de su dirección (y su nombre; sin él, el
// de su servidor).
func FromURL(u, name string) Station {
	sum := sha1.Sum([]byte(u))
	if name = strings.TrimSpace(name); name == "" {
		if p, err := url.Parse(u); err == nil && p.Host != "" {
			name = p.Host
		} else {
			name = u
		}
	}
	return Station{UUID: "url-" + hex.EncodeToString(sum[:8]), Name: name, URL: u}
}

// IsStreamURL dice si s es una dirección de emisora (http o https).
func IsStreamURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// extinfLogo y extinfGroup sacan el logo y la categoría de una línea
// #EXTINF (tvg-logo="..." y group-title="...").
var (
	extinfLogo  = regexp.MustCompile(`tvg-logo="([^"]*)"`)
	extinfGroup = regexp.MustCompile(`group-title="([^"]*)"`)
)

// ParsePlaylist lee las emisoras de una lista .m3u o .pls (se distingue por
// su contenido). Lo que no es una dirección http se salta.
func ParsePlaylist(r io.Reader) []Station {
	var out []Station
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	// m3u: el nombre, el logo y la categoría de #EXTINF van con la dirección
	// siguiente.
	name, logo, group := "", "", ""
	// pls: FileN y TitleN, en cualquier orden.
	plsURL, plsTitle := map[string]string{}, map[string]string{}
	var plsOrder []string
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		lower := strings.ToLower(line)
		switch {
		case line == "":
		case strings.HasPrefix(lower, "#extinf:"):
			if i := strings.LastIndex(line, ","); i >= 0 {
				name = strings.TrimSpace(line[i+1:])
			}
			if m := extinfLogo.FindStringSubmatch(line); m != nil {
				logo = m[1]
			}
			if m := extinfGroup.FindStringSubmatch(line); m != nil {
				group = strings.TrimSpace(m[1])
			}
		case strings.HasPrefix(line, "#"), strings.HasPrefix(line, "["):
		case strings.HasPrefix(lower, "file") && strings.Contains(line, "="):
			k, v, _ := strings.Cut(line, "=")
			n := strings.TrimPrefix(strings.ToLower(k), "file")
			if IsStreamURL(v) {
				if _, ok := plsURL[n]; !ok {
					plsOrder = append(plsOrder, n)
				}
				plsURL[n] = strings.TrimSpace(v)
			}
		case strings.HasPrefix(lower, "title") && strings.Contains(line, "="):
			k, v, _ := strings.Cut(line, "=")
			plsTitle[strings.TrimPrefix(strings.ToLower(k), "title")] = v
		case IsStreamURL(line):
			s := FromURL(line, name)
			s.Favicon, s.Tags = logo, group
			out = append(out, s)
			name, logo, group = "", "", ""
		}
	}
	for _, n := range plsOrder {
		out = append(out, FromURL(plsURL[n], plsTitle[n]))
	}
	return out
}

// LoadPlaylists lee todas las listas de dir: sus emisoras, cada una con su
// categoría (group-title) como género o, si no tiene, el nombre de su lista
// (sin extensión), en orden de lista y de aparición, sin repetir direcciones.
func LoadPlaylists(dir string) []Station {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		for _, x := range PlaylistExtensions {
			if !e.IsDir() && ext == x {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	seen := map[string]bool{}
	var out []Station
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		list := strings.TrimSuffix(n, filepath.Ext(n))
		for _, s := range ParsePlaylist(f) {
			if !seen[s.URL] {
				seen[s.URL] = true
				if s.Tags == "" {
					s.Tags = list
				}
				out = append(out, s)
			}
		}
		f.Close()
	}
	return out
}
