// Package podcast son los podcasts: buscarlos y ver los más escuchados con
// la búsqueda de Apple (sin clave), leer sus RSS y bajar los episodios para
// escucharlos sin conexión.
package podcast

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// UserAgent es como se presenta audiofilus a los servidores de los podcasts.
const UserAgent = "audiofilus/1.0"

// Show es un podcast.
type Show struct {
	ID      int64  `json:"id,omitempty"` // el de Apple, si viene de ahí
	Title   string `json:"title"`
	Author  string `json:"author"`
	Feed    string `json:"feed"`
	Artwork string `json:"artwork"`
}

// Episode es un episodio.
type Episode struct {
	GUID     string    `json:"guid"`
	Title    string    `json:"title"`
	Date     time.Time `json:"date"`
	Duration int       `json:"duration"` // segundos; 0 si no se sabe
	URL      string    `json:"url"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
}

var client = &http.Client{Timeout: 30 * time.Second}

// getJSON pide u y deja la respuesta en out.
func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("podcast: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// itunesResult es un resultado de la búsqueda de Apple.
type itunesResult struct {
	ID      int64  `json:"collectionId"`
	Name    string `json:"collectionName"`
	Artist  string `json:"artistName"`
	Feed    string `json:"feedUrl"`
	Artwork string `json:"artworkUrl600"`
}

func (r itunesResult) show() Show {
	return Show{ID: r.ID, Title: r.Name, Author: r.Artist, Feed: r.Feed, Artwork: r.Artwork}
}

// Search busca podcasts por nombre (country: código de dos letras o "").
func Search(ctx context.Context, term, country string) ([]Show, error) {
	q := url.Values{"media": {"podcast"}, "term": {term}, "limit": {"50"}}
	if country != "" {
		q.Set("country", country)
	}
	var r struct {
		Results []itunesResult `json:"results"`
	}
	if err := getJSON(ctx, "https://itunes.apple.com/search?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	var out []Show
	for _, x := range r.Results {
		if x.Feed != "" {
			out = append(out, x.show())
		}
	}
	return out, nil
}

// Top son los más escuchados de un país en Apple Podcasts ("" es EE. UU.).
func Top(ctx context.Context, country string) ([]Show, error) {
	cc := strings.ToLower(country)
	if cc == "" {
		cc = "us"
	}
	var top struct {
		Feed struct {
			Entry []struct {
				ID struct {
					Attr struct {
						ID string `json:"im:id"`
					} `json:"attributes"`
				} `json:"id"`
			} `json:"entry"`
		} `json:"feed"`
	}
	if err := getJSON(ctx, "https://itunes.apple.com/"+cc+"/rss/toppodcasts/limit=50/json", &top); err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range top.Feed.Entry {
		ids = append(ids, e.ID.Attr.ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// Las direcciones de los RSS, con lookup (el ranking no las trae).
	var r struct {
		Results []itunesResult `json:"results"`
	}
	if err := getJSON(ctx, "https://itunes.apple.com/lookup?country="+cc+"&id="+strings.Join(ids, ","), &r); err != nil {
		return nil, err
	}
	byID := map[int64]itunesResult{}
	for _, x := range r.Results {
		byID[x.ID] = x
	}
	var out []Show
	for _, id := range ids {
		n, _ := strconv.ParseInt(id, 10, 64)
		if x, ok := byID[n]; ok && x.Feed != "" {
			out = append(out, x.show())
		}
	}
	return out, nil
}

// rss es lo que se lee de un feed.
type rss struct {
	Channel struct {
		Title  string `xml:"title"`
		Author string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd author"`
		Image  struct {
			Href string `xml:"href,attr"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		Image2 struct {
			URL string `xml:"url"`
		} `xml:"image"`
		Items []struct {
			Title     string `xml:"title"`
			GUID      string `xml:"guid"`
			PubDate   string `xml:"pubDate"`
			Duration  string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
			Enclosure struct {
				URL    string `xml:"url,attr"`
				Type   string `xml:"type,attr"`
				Length string `xml:"length,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// Fetch lee el RSS de un podcast: sus datos y sus episodios (los que tienen
// audio), del más nuevo al más viejo.
func Fetch(ctx context.Context, feed string) (Show, []Episode, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", feed, nil)
	if err != nil {
		return Show{}, nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Show{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Show{}, nil, fmt.Errorf("podcast: %s", resp.Status)
	}
	var r rss
	dec := xml.NewDecoder(io.LimitReader(resp.Body, 32<<20))
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&r); err != nil {
		return Show{}, nil, fmt.Errorf("podcast: bad feed: %w", err)
	}
	c := r.Channel
	show := Show{Title: strings.TrimSpace(c.Title), Author: strings.TrimSpace(c.Author), Feed: feed, Artwork: c.Image.Href}
	if show.Artwork == "" {
		show.Artwork = c.Image2.URL
	}
	var eps []Episode
	for _, it := range c.Items {
		if it.Enclosure.URL == "" || !strings.HasPrefix(it.Enclosure.Type, "audio") && it.Enclosure.Type != "" {
			continue
		}
		size, _ := strconv.ParseInt(it.Enclosure.Length, 10, 64)
		e := Episode{GUID: strings.TrimSpace(it.GUID), Title: strings.TrimSpace(it.Title), URL: it.Enclosure.URL,
			Type: it.Enclosure.Type, Size: size, Duration: parseDuration(it.Duration), Date: parseDate(it.PubDate)}
		if e.GUID == "" {
			e.GUID = e.URL
		}
		eps = append(eps, e)
	}
	return show, eps, nil
}

// parseDuration lee itunes:duration: "1:02:03", "62:03" o "3723".
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	total := 0
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.Atoi(strings.Split(p, ".")[0])
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// parseDate lee la fecha de un RSS (RFC 1123, con sus variantes).
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Download baja el episodio a path (y lo deja ahí aunque se corte a medias,
// con .part), con el progreso en bytes.
func Download(ctx context.Context, e Episode, path string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", e.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	cl := &http.Client{} // sin plazo total: un episodio grande tarda
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("podcast: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	if total <= 0 {
		total = e.Size
	}
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				_ = os.Remove(tmp)
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, max(total, done+1))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			_ = os.Remove(tmp)
			return rerr
		}
	}
	f.Close()
	return os.Rename(tmp, path)
}

// Ext es la extensión del audio de un episodio (de su tipo o su dirección).
func (e Episode) Ext() string {
	switch {
	case strings.Contains(e.Type, "mp4"), strings.Contains(e.Type, "m4a"), strings.Contains(e.Type, "aac"):
		return ".m4a"
	case strings.Contains(e.Type, "ogg"):
		return ".ogg"
	case strings.Contains(e.Type, "opus"):
		return ".opus"
	}
	if u, err := url.Parse(e.URL); err == nil {
		if x := strings.ToLower(filepath.Ext(u.Path)); x == ".m4a" || x == ".mp3" || x == ".ogg" || x == ".opus" {
			return x
		}
	}
	return ".mp3"
}
