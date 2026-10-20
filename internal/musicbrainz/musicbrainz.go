// Package musicbrainz busca los datos de una canción o un álbum en
// MusicBrainz (la API WS/2 en JSON, sin clave) y su portada en el Cover Art
// Archive (o, si no está, en el buscador de iTunes), para completar las
// etiquetas y las portadas que faltan. Solo devuelve los datos: escribirlos
// en los archivos es cosa de internal/tagwrite.
//
// MusicBrainz pide no pasar de una petición por segundo y un User-Agent que
// diga quién pregunta: el Client espera lo que haga falta entre peticiones.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/version"
)

// ErrNotFound es lo que devuelven las búsquedas y las portadas sin nada.
var ErrNotFound = errors.New("musicbrainz: not found")

// Client habla con MusicBrainz, el Cover Art Archive e iTunes.
type Client struct {
	HTTP *http.Client
	// Las direcciones base (los tests las cambian por un servidor local).
	MB, CAA, ITunes string
	UA              string
	// Interval es el tiempo mínimo entre peticiones a MusicBrainz.
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

// New es un Client con los servidores de verdad.
func New() *Client {
	return &Client{
		HTTP:     &http.Client{Timeout: 30 * time.Second},
		MB:       "https://musicbrainz.org/ws/2",
		CAA:      "https://coverartarchive.org",
		ITunes:   "https://itunes.apple.com",
		UA:       "audiofilus/" + version.Version + " ( https://github.com/juanvillacortac/audiofilus )",
		Interval: time.Second,
	}
}

// wait espera a que toque la siguiente petición a MusicBrainz.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	next := c.last.Add(c.Interval)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	c.last = next
	c.mu.Unlock()
	select {
	case <-time.After(time.Until(next)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// get pide u y devuelve el cuerpo (con el límite de MusicBrainz si mb).
func (c *Client) get(ctx context.Context, u string, mb bool, limit int64) ([]byte, string, error) {
	if mb {
		if err := c.wait(ctx); err != nil {
			return nil, "", err
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, resp.Header.Get("Content-Type"), err
}

// getJSON pide u a MusicBrainz y lee la respuesta en v.
func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	b, _, err := c.get(ctx, u, true, 8<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Track es una pista de una edición.
type Track struct {
	Disc        int    // el disco (1..)
	Number      int    // la posición en el disco (1..)
	NumberText  string // como lo escribe la edición ("1", "A1"...)
	Title       string
	Artist      string
	Length      time.Duration
	ID          string // el de la pista en esta edición
	RecordingID string
}

// Medium es un disco de una edición.
type Medium struct {
	Position int
	Format   string // "CD", "Digital Media", "12\" Vinyl"...
	Title    string
	Tracks   []Track
}

// Release es una edición de un álbum.
type Release struct {
	ID         string
	Title      string
	Artist     string
	ArtistID   string
	Date       string // "1997-07-01", "1997" o ""
	Year       int
	Country    string
	Label      string
	Genres     []string // los más votados primero
	GroupID    string   // el del grupo de ediciones (el álbum, sin importar la edición)
	Type       string   // "Album", "Single", "EP"...
	TrackCount int
	Formats    []string // los de cada disco
	Score      int      // de la búsqueda (0-100)
	Rank       float64  // el orden propio: la puntuación más cuánto cuadra el número de pistas
	Media      []Medium // solo con GetRelease
}

// Tracks son todas las pistas, disco tras disco.
func (r Release) Tracks() []Track {
	var out []Track
	for _, m := range r.Media {
		out = append(out, m.Tracks...)
	}
	return out
}

// Recording es una grabación (una canción) encontrada.
type Recording struct {
	ID       string
	Title    string
	Artist   string
	Length   time.Duration
	Score    int
	Releases []ReleaseRef // las ediciones en que sale
}

// ReleaseRef es una edición en que sale una grabación.
type ReleaseRef struct {
	ID, Title, Date, GroupID, Type string
}

// Lo que devuelve la API (lo que se usa).
type (
	mbCredit []struct {
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
		Artist     struct {
			ID string `json:"id"`
		} `json:"artist"`
	}
	mbGroup struct {
		ID          string   `json:"id"`
		PrimaryType string   `json:"primary-type"`
		Genres      []mbTag  `json:"genres"`
		Tags        []mbTag  `json:"tags"`
		Secondary   []string `json:"secondary-types"`
	}
	mbTag struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	mbRelease struct {
		ID         string   `json:"id"`
		Score      int      `json:"score"`
		Title      string   `json:"title"`
		Date       string   `json:"date"`
		Country    string   `json:"country"`
		TrackCount int      `json:"track-count"`
		Credit     mbCredit `json:"artist-credit"`
		Group      mbGroup  `json:"release-group"`
		Genres     []mbTag  `json:"genres"`
		Tags       []mbTag  `json:"tags"`
		LabelInfo  []struct {
			Label *struct {
				Name string `json:"name"`
			} `json:"label"`
		} `json:"label-info"`
		Media []struct {
			Position   int    `json:"position"`
			Format     string `json:"format"`
			Title      string `json:"title"`
			TrackCount int    `json:"track-count"`
			Tracks     []struct {
				ID        string   `json:"id"`
				Number    string   `json:"number"`
				Position  int      `json:"position"`
				Title     string   `json:"title"`
				Length    int64    `json:"length"`
				Credit    mbCredit `json:"artist-credit"`
				Recording struct {
					ID     string   `json:"id"`
					Length int64    `json:"length"`
					Credit mbCredit `json:"artist-credit"`
				} `json:"recording"`
			} `json:"tracks"`
		} `json:"media"`
	}
)

// name junta los nombres de un crédito ("A & B feat. C").
func (c mbCredit) name() string {
	var b strings.Builder
	for _, a := range c {
		b.WriteString(a.Name + a.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// topTags son los nombres de unas etiquetas, las más votadas primero.
func topTags(tags []mbTag, max int) []string {
	t := append([]mbTag(nil), tags...)
	sort.SliceStable(t, func(i, j int) bool { return t[i].Count > t[j].Count })
	var out []string
	for _, g := range t {
		if len(out) == max {
			break
		}
		out = append(out, g.Name)
	}
	return out
}

// convert pasa una edición de la API a la nuestra.
func (r mbRelease) convert() Release {
	out := Release{ID: r.ID, Title: r.Title, Artist: r.Credit.name(), Date: r.Date, Country: r.Country,
		GroupID: r.Group.ID, Type: r.Group.PrimaryType, TrackCount: r.TrackCount, Score: r.Score}
	if len(r.Credit) > 0 {
		out.ArtistID = r.Credit[0].Artist.ID
	}
	if len(r.Date) >= 4 {
		out.Year, _ = strconv.Atoi(r.Date[:4])
	}
	for _, l := range r.LabelInfo {
		if l.Label != nil && l.Label.Name != "" {
			out.Label = l.Label.Name
			break
		}
	}
	// Los géneros: los de la edición, si no los del álbum, si no sus
	// etiquetas libres.
	for _, g := range [][]mbTag{r.Genres, r.Group.Genres, r.Tags, r.Group.Tags} {
		if len(g) > 0 {
			out.Genres = topTags(g, 3)
			break
		}
	}
	count := 0
	for _, m := range r.Media {
		out.Formats = append(out.Formats, m.Format)
		count += m.TrackCount
		med := Medium{Position: m.Position, Format: m.Format, Title: m.Title}
		for _, t := range m.Tracks {
			tr := Track{Disc: m.Position, Number: t.Position, NumberText: t.Number, Title: t.Title,
				ID: t.ID, RecordingID: t.Recording.ID}
			tr.Artist = t.Credit.name()
			if tr.Artist == "" {
				tr.Artist = t.Recording.Credit.name()
			}
			if tr.Artist == "" {
				tr.Artist = out.Artist
			}
			ms := t.Length
			if ms == 0 {
				ms = t.Recording.Length
			}
			tr.Length = time.Duration(ms) * time.Millisecond
			med.Tracks = append(med.Tracks, tr)
		}
		out.Media = append(out.Media, med)
	}
	if out.TrackCount == 0 {
		out.TrackCount = count
	}
	return out
}

// luceneEscape escapa un texto para las búsquedas de MusicBrainz (Lucene).
func luceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SearchRelease busca las ediciones de un álbum, ordenadas por lo bien que
// cuadran: la puntuación de MusicBrainz y, si se sabe (trackCount > 0),
// que tengan las mismas pistas que el álbum de la biblioteca.
func (c *Client) SearchRelease(ctx context.Context, artist, album string, trackCount int) ([]Release, error) {
	var q []string
	if album = strings.TrimSpace(album); album != "" {
		q = append(q, `release:"`+luceneEscape(album)+`"`)
	}
	if artist = strings.TrimSpace(artist); artist != "" {
		q = append(q, `artist:"`+luceneEscape(artist)+`"`)
	}
	if len(q) == 0 {
		return nil, ErrNotFound
	}
	u := c.MB + "/release/?fmt=json&limit=25&query=" + url.QueryEscape(strings.Join(q, " AND "))
	var res struct {
		Releases []mbRelease `json:"releases"`
	}
	if err := c.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(res.Releases))
	for _, r := range res.Releases {
		rel := r.convert()
		rel.Media = nil // en la búsqueda no vienen las pistas
		rel.Rank = float64(rel.Score) / 100
		if trackCount > 0 {
			switch d := rel.TrackCount - trackCount; {
			case d == 0:
				rel.Rank += 0.3
			case d == 1 || d == -1:
				rel.Rank += 0.1
			default:
				rel.Rank -= 0.2
			}
		}
		// Un álbum de verdad antes que un recopilatorio o un sencillo.
		if rel.Type == "Album" {
			rel.Rank += 0.05
		}
		out = append(out, rel)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank > out[j].Rank })
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// GetRelease lee una edición entera: sus discos y pistas, el sello y los
// géneros.
func (c *Client) GetRelease(ctx context.Context, mbid string) (Release, error) {
	u := c.MB + "/release/" + url.PathEscape(mbid) + "?fmt=json&inc=recordings+artist-credits+labels+genres+tags+release-groups"
	var r mbRelease
	if err := c.getJSON(ctx, u, &r); err != nil {
		return Release{}, err
	}
	return r.convert(), nil
}

// LookupRecording busca una canción suelta; con durationSec > 0, las de
// duración parecida primero.
func (c *Client) LookupRecording(ctx context.Context, artist, title string, durationSec int) ([]Recording, error) {
	q := `recording:"` + luceneEscape(strings.TrimSpace(title)) + `"`
	if artist = strings.TrimSpace(artist); artist != "" {
		q += ` AND artist:"` + luceneEscape(artist) + `"`
	}
	u := c.MB + "/recording/?fmt=json&limit=25&query=" + url.QueryEscape(q)
	var res struct {
		Recordings []struct {
			ID       string   `json:"id"`
			Score    int      `json:"score"`
			Title    string   `json:"title"`
			Length   int64    `json:"length"`
			Credit   mbCredit `json:"artist-credit"`
			Releases []struct {
				ID    string  `json:"id"`
				Title string  `json:"title"`
				Date  string  `json:"date"`
				Group mbGroup `json:"release-group"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := c.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	var out []Recording
	for _, r := range res.Recordings {
		rec := Recording{ID: r.ID, Title: r.Title, Artist: r.Credit.name(), Length: time.Duration(r.Length) * time.Millisecond, Score: r.Score}
		for _, rel := range r.Releases {
			rec.Releases = append(rec.Releases, ReleaseRef{ID: rel.ID, Title: rel.Title, Date: rel.Date, GroupID: rel.Group.ID, Type: rel.Group.PrimaryType})
		}
		out = append(out, rec)
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	if durationSec > 0 {
		// Con la duración, las que duran parecido (±10 s) primero, luego las
		// que no la dicen; sin filtrarlas en la búsqueda, porque hay
		// grabaciones sin duración.
		want := time.Duration(durationSec) * time.Second
		key := func(r Recording) int {
			k := r.Score
			switch d := r.Length - want; {
			case r.Length == 0:
				k -= 15 // sin duración: detrás de las que coinciden
			case d > 10*time.Second || d < -10*time.Second:
				k -= 50
			}
			return k
		}
		sort.SliceStable(out, func(i, j int) bool { return key(out[i]) > key(out[j]) })
	}
	return out, nil
}
