package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// LRCLIB (lrclib.net) es una base de letras libre, sin clave, con letras
// sincronizadas: https://lrclib.net/docs. Pide que cada cliente se presente
// con su nombre y su página en el User-Agent.
const (
	lrclibURL = "https://lrclib.net"
	userAgent = "audiofilus (https://github.com/juanvillacortac/audiofilus)"
)

// Found es una letra encontrada en internet.
type Found struct {
	ID           int     `json:"id"`
	Track        string  `json:"trackName"`
	Artist       string  `json:"artistName"`
	Album        string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	Plain        string  `json:"plainLyrics"`
	Synced       string  `json:"syncedLyrics"`
}

// Lyrics es la letra de lo encontrado: la sincronizada si la hay.
func (f Found) Lyrics() Lyrics {
	if strings.TrimSpace(f.Synced) != "" {
		return Parse(f.Synced)
	}
	return Parse(f.Plain)
}

// LRCLIB es el cliente de LRCLIB.
type LRCLIB struct {
	HTTP *http.Client
	Base string // vacío: lrclib.net (las pruebas ponen un servidor propio)
}

// Query es lo que se sabe de la canción.
type Query struct {
	Title, Artist, Album string
	Duration             float64 // segundos; 0 si no se sabe
}

// Search busca la canción por título y artista; si no sale nada, con el
// título limpio de "(feat. ...)", "- Remastered" y parecidos, y por último
// como texto libre. Devuelve lo encontrado de más a menos parecido.
func (c *LRCLIB) Search(ctx context.Context, q Query) ([]Found, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, fmt.Errorf("lrclib: no title")
	}
	tries := []url.Values{{"track_name": {title}}}
	if q.Artist != "" {
		tries[0].Set("artist_name", q.Artist)
	}
	if clean := CleanTitle(title); clean != title {
		v := url.Values{"track_name": {clean}}
		if q.Artist != "" {
			v.Set("artist_name", q.Artist)
		}
		tries = append(tries, v)
	}
	tries = append(tries, url.Values{"q": {strings.TrimSpace(q.Artist + " " + CleanTitle(title))}})
	var lastErr error
	for _, v := range tries {
		var found []Found
		if err := c.get(ctx, "/api/search?"+v.Encode(), &found); err != nil {
			lastErr = err
			continue
		}
		if len(found) > 0 {
			rank(found, q)
			return found, nil
		}
	}
	return nil, lastErr
}

// Best es la mejor de las encontradas para esta canción: con letra (mejor
// sincronizada) y una duración que case con la suya. ok es false si
// ninguna sirve.
func Best(found []Found, q Query) (Found, bool) {
	for _, f := range found {
		if f.Instrumental || (f.Synced == "" && strings.TrimSpace(f.Plain) == "") {
			continue
		}
		if q.Duration > 0 && f.Duration > 0 && math.Abs(f.Duration-q.Duration) > 4 {
			continue
		}
		return f, true
	}
	return Found{}, false
}

// rank ordena lo encontrado: primero las que casan en duración, luego las
// sincronizadas, luego las que casan en artista y título.
func rank(found []Found, q Query) {
	score := func(f Found) int {
		s := 0
		if q.Duration > 0 && f.Duration > 0 {
			switch d := math.Abs(f.Duration - q.Duration); {
			case d <= 2:
				s += 40
			case d <= 5:
				s += 20
			}
		}
		if f.Synced != "" {
			s += 30
		} else if strings.TrimSpace(f.Plain) == "" {
			s -= 50
		}
		if norm(f.Artist) == norm(q.Artist) {
			s += 10
		}
		if norm(CleanTitle(f.Track)) == norm(CleanTitle(q.Title)) {
			s += 10
		}
		return s
	}
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && score(found[j]) > score(found[j-1]); j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
}

func (c *LRCLIB) get(ctx context.Context, path string, out any) error {
	base := c.Base
	if base == "" {
		base = lrclibURL
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lrclib: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// cleanRe son los añadidos al título que no están en la base de letras:
// "(feat. X)", "[Remastered 2011]", "- Live", "- 2011 Remaster".
var cleanRe = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*[\(\[](feat\.?|ft\.?|with|remaster(ed)?|\d{4} remaster(ed)?|live|radio edit|explicit|clean|bonus track|mono|stereo)[^\)\]]*[\)\]]`),
	regexp.MustCompile(`(?i)\s+-\s+(\d{4}\s+)?(remaster(ed)?|live|radio edit|single version|mono|stereo)\b.*$`),
}

// CleanTitle quita del título lo que suele estorbar al buscar la letra.
func CleanTitle(t string) string {
	for _, re := range cleanRe {
		t = re.ReplaceAllString(t, "")
	}
	return strings.TrimSpace(t)
}

// norm deja un texto para comparar: minúsculas y sin signos.
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
