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
}

// Artist son los artistas juntos ("A, B").
func (t Track) Artist() string { return strings.Join(t.Artists, ", ") }

// searchURL y la versión del cliente web de YouTube Music. La versión puede
// ser vieja: el servidor acepta las de meses atrás.
const (
	searchURL     = "https://music.youtube.com/youtubei/v1/search?prettyPrint=false"
	clientVersion = "1.20260101.01.00"
	// songsFilter es el filtro "Canciones" de la búsqueda.
	songsFilter = "EgWKAQIIAWoQEAMQBBAJEAoQBRAREBAQFQ%3D%3D"
)

var durationRe = regexp.MustCompile(`^\d+(:\d\d)+$`)

// Search busca canciones (no vídeos ni álbumes) que casen con query.
func Search(ctx context.Context, query string) ([]Track, error) {
	body, _ := json.Marshal(map[string]any{
		"context": map[string]any{"client": map[string]any{
			"clientName": "WEB_REMIX", "clientVersion": clientVersion, "hl": "es",
		}},
		"query":  query,
		"params": songsFilter,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", searchURL, bytes.NewReader(body))
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
		return nil, fmt.Errorf("youtube music search: %s", resp.Status)
	}
	var doc any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
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

// walk llama a fn con cada musicResponsiveListItemRenderer de la respuesta.
func walk(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		if r, ok := x["musicResponsiveListItemRenderer"].(map[string]any); ok {
			fn(r)
		}
		for _, c := range x {
			walk(c, fn)
		}
	case []any:
		for _, c := range x {
			walk(c, fn)
		}
	}
}

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
	if thumbs, ok := get(r, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails").([]any); ok && len(thumbs) > 0 {
		t.Thumb = str(get(thumbs[len(thumbs)-1], "url"))
	}
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
