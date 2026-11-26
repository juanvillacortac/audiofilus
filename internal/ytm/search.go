// Package ytm es YouTube Music: buscar canciones (la API interna de la web,
// sin cuenta, en Go puro) y, con un yt-dlp que el usuario pone junto a
// audiofilus, escucharlas sin guardarlas o descargarlas a la biblioteca con la
// misma estructura que ytm-gym. Sin yt-dlp, la interfaz no ofrece nada de
// esto.
package ytm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Track es una canción de YouTube Music.
type Track struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists"`
	Album    string   `json:"album"`
	Duration string   `json:"duration"` // "3:59"
	Thumb    string   `json:"thumb"`    // la miniatura más grande (cuadrada)
	// Track, TrackTotal y Year, solo si viene de un álbum (AlbumTracks): se
	// escriben al descargarla.
	Track      int    `json:"track,omitempty"`
	TrackTotal int    `json:"track_total,omitempty"`
	Year       string `json:"year,omitempty"`
}

// Artist son los artistas juntos ("A, B").
func (t Track) Artist() string { return strings.Join(t.Artists, ", ") }

// searchURL y la versión del cliente web de YouTube Music. La versión puede
// ser vieja: el servidor acepta las de meses atrás.
const (
	searchURL     = "https://music.youtube.com/youtubei/v1/search?prettyPrint=false"
	browseURL     = "https://music.youtube.com/youtubei/v1/browse?prettyPrint=false"
	clientVersion = "1.20260101.01.00"
	// songsFilter es el filtro "Canciones" de la búsqueda.
	songsFilter = "EgWKAQIIAWoQEAMQBBAJEAoQBRAREBAQFQ%3D%3D"
	// albumsFilter es el filtro "Álbumes".
	albumsFilter = "EgWKAQIYAWoMEA4QChADEAQQCRAF"
)

var (
	durationRe = regexp.MustCompile(`^\d+(:\d\d)+$`)
	yearRe     = regexp.MustCompile(`^\d{4}$`)
)

// Search busca canciones (no vídeos ni álbumes) que casen con query.
func Search(ctx context.Context, query string) ([]Track, error) {
	doc, err := call(ctx, searchURL, map[string]any{"query": query, "params": songsFilter})
	if err != nil {
		return nil, err
	}
	var out []Track
	walk(doc, func(r map[string]any) {
		if t, ok := parseItem(r); ok {
			out = append(out, t)
		}
	})
	return out, nil
}

// Album es un álbum de YouTube Music (o un EP, o un sencillo).
type Album struct {
	ID     string `json:"id"` // el browseId ("MPREb_...")
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Year   string `json:"year"`
	Thumb  string `json:"thumb"`
}

// SearchAlbums busca álbumes que casen con query, como el filtro "Álbumes"
// de la web: con "vespertine bjork", el álbum entero y no canciones sueltas
// de otros discos o versiones.
func SearchAlbums(ctx context.Context, query string) ([]Album, error) {
	doc, err := call(ctx, searchURL, map[string]any{"query": query, "params": albumsFilter})
	if err != nil {
		return nil, err
	}
	var out []Album
	walk(doc, func(r map[string]any) {
		if al, ok := parseAlbum(r); ok {
			out = append(out, al)
		}
	})
	return out, nil
}

// parseAlbum lee un álbum de la búsqueda: la primera columna es el título y
// la segunda "Álbum • Artista • 2001", con el artista enlazado.
func parseAlbum(r map[string]any) (Album, bool) {
	var al Album
	al.ID = str(get(r, "navigationEndpoint", "browseEndpoint", "browseId"))
	cols, _ := r["flexColumns"].([]any)
	if !strings.HasPrefix(al.ID, "MPRE") || len(cols) < 2 {
		return al, false
	}
	al.Title = runsText(get(cols[0], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
	runs, _ := get(cols[1], "musicResponsiveListItemFlexColumnRenderer", "text", "runs").([]any)
	var artists []string
	for _, run := range runs {
		text := strings.TrimSpace(str(get(run, "text")))
		switch {
		case str(get(run, "navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType")) == "MUSIC_PAGE_TYPE_ARTIST":
			artists = append(artists, text)
		case yearRe.MatchString(text):
			al.Year = text
		}
	}
	al.Artist = strings.Join(artists, ", ")
	if al.Artist == "" && len(runs) >= 3 { // "Álbum • Artista • 2001" sin enlace
		al.Artist = strings.TrimSpace(str(get(runs[2], "text")))
	}
	al.Thumb = lastThumb(get(r, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
	return al, al.Title != ""
}

// AlbumTracks son las canciones de un álbum, en orden, con su artista, el
// álbum y su portada (las filas de la página del álbum no los traen: van en
// la cabecera).
func AlbumTracks(ctx context.Context, al Album) ([]Track, error) {
	doc, err := call(ctx, browseURL, map[string]any{"browseId": al.ID})
	if err != nil {
		return nil, err
	}
	walkKey(doc, "musicResponsiveHeaderRenderer", func(h map[string]any) {
		if t := runsText(get(h, "title", "runs")); t != "" {
			al.Title = t
		}
		if a := runsText(get(h, "straplineTextOne", "runs")); a != "" {
			al.Artist = a
		}
		if th := lastThumb(get(h, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails")); th != "" {
			al.Thumb = th
		}
	})
	var out []Track
	walk(doc, func(r map[string]any) {
		t := Track{ID: str(get(r, "playlistItemData", "videoId")), Album: al.Title, Thumb: al.Thumb}
		cols, _ := r["flexColumns"].([]any)
		if t.ID == "" || len(cols) == 0 {
			return
		}
		t.Title = runsText(get(cols[0], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
		if len(cols) > 1 { // en un recopilatorio, el artista de cada canción
			runs, _ := get(cols[1], "musicResponsiveListItemFlexColumnRenderer", "text", "runs").([]any)
			for _, run := range runs {
				if str(get(run, "navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType")) == "MUSIC_PAGE_TYPE_ARTIST" {
					t.Artists = append(t.Artists, strings.TrimSpace(str(get(run, "text"))))
				}
			}
		}
		if len(t.Artists) == 0 && al.Artist != "" {
			t.Artists = strings.Split(al.Artist, ", ")
		}
		if fixed, ok := r["fixedColumns"].([]any); ok && len(fixed) > 0 {
			if d := runsText(get(fixed[0], "musicResponsiveListItemFixedColumnRenderer", "text", "runs")); durationRe.MatchString(d) {
				t.Duration = d
			}
		}
		if t.Title != "" {
			out = append(out, t)
		}
	})
	for i := range out {
		out[i].Track, out[i].TrackTotal, out[i].Year = i+1, len(out), al.Year
	}
	return out, nil
}

// call hace una petición a la API interna de la web (search o browse) con
// el contexto del cliente y devuelve el JSON.
func call(ctx context.Context, url string, payload map[string]any) (any, error) {
	payload["context"] = map[string]any{"client": map[string]any{
		"clientName": "WEB_REMIX", "clientVersion": clientVersion, "hl": "es",
	}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube music: %s", resp.Status)
	}
	var doc any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// lastThumb es la URL de la última (la más grande) de unas miniaturas.
func lastThumb(v any) string {
	if thumbs, ok := v.([]any); ok && len(thumbs) > 0 {
		return str(get(thumbs[len(thumbs)-1], "url"))
	}
	return ""
}

// walkKey llama a fn con cada objeto bajo la clave key.
func walkKey(v any, key string, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		if r, ok := x[key].(map[string]any); ok {
			fn(r)
		}
		for _, c := range x {
			walkKey(c, key, fn)
		}
	case []any:
		for _, c := range x {
			walkKey(c, key, fn)
		}
	}
}

// walk llama a fn con cada musicResponsiveListItemRenderer de la respuesta.
func walk(v any, fn func(map[string]any)) { walkKey(v, "musicResponsiveListItemRenderer", fn) }

// parseItem lee un resultado: la primera columna es el título y la segunda,
// "Artista • Álbum • 3:59", con cada artista y el álbum en su propio enlace.
func parseItem(r map[string]any) (Track, bool) {
	var t Track
	t.ID = str(get(r, "playlistItemData", "videoId"))
	cols, _ := r["flexColumns"].([]any)
	if t.ID == "" || len(cols) < 2 {
		return t, false
	}
	t.Title = runsText(get(cols[0], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
	runs, _ := get(cols[1], "musicResponsiveListItemFlexColumnRenderer", "text", "runs").([]any)
	for _, run := range runs {
		text := strings.TrimSpace(str(get(run, "text")))
		switch str(get(run, "navigationEndpoint", "browseEndpoint", "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType")) {
		case "MUSIC_PAGE_TYPE_ARTIST":
			t.Artists = append(t.Artists, text)
		case "MUSIC_PAGE_TYPE_ALBUM":
			t.Album = text
		default:
			if durationRe.MatchString(text) {
				t.Duration = text
			}
		}
	}
	if len(t.Artists) == 0 && len(runs) > 0 { // artistas sin enlace: el primer trozo
		t.Artists = []string{strings.TrimSpace(str(get(runs[0], "text")))}
	}
	t.Thumb = lastThumb(get(r, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
	return t, t.Title != ""
}

// get baja por un árbol de JSON; nil si algo no está.
func get(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// runsText junta el texto de unos "runs".
func runsText(v any) string {
	runs, _ := v.([]any)
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(str(get(r, "text")))
	}
	return b.String()
}
