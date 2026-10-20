package musicbrainz

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// png1 es una imagen PNG mínima (la cabecera basta para adivinar su tipo).
var png1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// fake es un MusicBrainz, un Cover Art Archive y un iTunes de mentira, con
// las respuestas de testdata (recortes de las de verdad). Cuenta las
// peticiones a MusicBrainz y guarda la última consulta.
type fake struct {
	mbHits atomic.Int32
	query  atomic.Value
	ua     atomic.Value
}

func newFake(t *testing.T) (*Client, *fake) {
	f := &fake{}
	mux := http.NewServeMux()
	file := func(name string) []byte {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	mux.HandleFunc("/ws/2/release/", func(w http.ResponseWriter, r *http.Request) {
		f.mbHits.Add(1)
		f.ua.Store(r.UserAgent())
		if r.URL.Path == "/ws/2/release/" {
			f.query.Store(r.URL.Query().Get("query"))
			w.Write(file("search.json"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "6a09041b-0f79-3278-88d9-0552dd28d37f") {
			w.Write(file("release.json"))
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/ws/2/recording/", func(w http.ResponseWriter, r *http.Request) {
		f.mbHits.Add(1)
		f.query.Store(r.URL.Query().Get("query"))
		w.Write(file("recording.json"))
	})
	// El CAA: la edición "nocover" no tiene portada pero su álbum sí.
	mux.HandleFunc("/caa/release/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "nocover") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/front-500") {
			t.Errorf("cover path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-release"))
	})
	mux.HandleFunc("/caa/release-group/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(png1) // sin tipo: se adivina por los bytes
	})
	mux.HandleFunc("/itunes/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"artistName":"Someone Else","collectionName":"X","artworkUrl100":"` + "http://" + r.Host + `/itunes/art/wrong/100x100bb.jpg"},
			{"artistName":"Radiohead","collectionName":"OK Computer","artworkUrl100":"` + "http://" + r.Host + `/itunes/art/ok/100x100bb.jpg"}]}`))
	})
	mux.HandleFunc("/itunes/art/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/ok/600x600") {
			t.Errorf("itunes art %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg-itunes"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New()
	c.MB, c.CAA, c.ITunes, c.Interval = srv.URL+"/ws/2", srv.URL+"/caa", srv.URL+"/itunes", 0
	return c, f
}

// SearchRelease arma la consulta, lee las ediciones y las ordena: con 12
// pistas en la biblioteca, las de 12 antes que la de 13 aunque esta tenga
// la misma puntuación.
func TestSearchRelease(t *testing.T) {
	c, f := newFake(t)
	rels, err := c.SearchRelease(context.Background(), "Radiohead", `OK "Computer"`, 12)
	if err != nil {
		t.Fatal(err)
	}
	if q := f.query.Load().(string); q != `release:"OK \"Computer\"" AND artist:"Radiohead"` {
		t.Errorf("query %q", q)
	}
	if !strings.HasPrefix(f.ua.Load().(string), "audiofilus/") {
		t.Errorf("user agent %q", f.ua.Load())
	}
	if len(rels) != 3 {
		t.Fatalf("%d releases", len(rels))
	}
	if rels[0].ID != "6a09041b-0f79-3278-88d9-0552dd28d37f" || rels[1].TrackCount != 12 || rels[2].TrackCount != 13 {
		t.Errorf("order: %s/%d %s/%d %s/%d", rels[0].ID, rels[0].TrackCount, rels[1].ID, rels[1].TrackCount, rels[2].ID, rels[2].TrackCount)
	}
	r := rels[0]
	if r.Title != "OK Computer" || r.Artist != "Radiohead" || r.Year != 1997 || r.GroupID == "" || r.Type != "Album" {
		t.Errorf("release %+v", r)
	}
}

// GetRelease lee los discos y las pistas (con su duración y artista), el
// sello y los géneros (de su álbum, porque la edición no tiene).
func TestGetRelease(t *testing.T) {
	c, _ := newFake(t)
	r, err := c.GetRelease(context.Background(), "6a09041b-0f79-3278-88d9-0552dd28d37f")
	if err != nil {
		t.Fatal(err)
	}
	if r.Label != "Capitol Records" || r.Date != "1997-07-01" || len(r.Genres) == 0 || r.Genres[0] != "alternative rock" {
		t.Errorf("release %+v", r)
	}
	tr := r.Tracks()
	if len(tr) != 5 || len(r.Media) != 2 {
		t.Fatalf("%d tracks in %d media", len(tr), len(r.Media))
	}
	if tr[1].Title != "Paranoid Android" || tr[1].Number != 2 || tr[1].Disc != 1 || tr[1].Length != 383493*time.Millisecond || tr[1].Artist != "Radiohead" {
		t.Errorf("track 2 %+v", tr[1])
	}
	if tr[4].Disc != 2 || tr[4].Number != 1 || tr[4].Title != "Lull" {
		t.Errorf("disc 2 track %+v", tr[4])
	}
	if _, err := c.GetRelease(context.Background(), "unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown release: %v", err)
	}
}

// LookupRecording pone primero la de duración parecida, aunque tenga menos
// puntuación.
func TestLookupRecording(t *testing.T) {
	c, f := newFake(t)
	recs, err := c.LookupRecording(context.Background(), "Radiohead", "Karma Police", 281)
	if err != nil {
		t.Fatal(err)
	}
	if q := f.query.Load().(string); q != `recording:"Karma Police" AND artist:"Radiohead"` {
		t.Errorf("query %q", q)
	}
	if len(recs) != 2 || recs[0].Length != 280*time.Second || recs[0].Score != 90 {
		t.Errorf("recordings %+v", recs)
	}
	if len(recs[0].Releases) == 0 || recs[0].Releases[0].GroupID == "" {
		t.Errorf("releases %+v", recs[0].Releases)
	}
}

// La portada: la de la edición; si no tiene, la de su álbum; si no hay
// ninguna en el CAA, la de iTunes (del artista que coincide).
func TestCovers(t *testing.T) {
	c, _ := newFake(t)
	ctx := context.Background()
	b, mime, err := c.FrontCover(ctx, "rel1", "grp1", 500)
	if err != nil || string(b) != "jpeg-release" || mime != "image/jpeg" {
		t.Errorf("release cover %q %q %v", b, mime, err)
	}
	b, mime, err = c.FrontCover(ctx, "nocover", "grp1", 500)
	if err != nil || !bytes.Equal(b, png1) || mime != "image/png" {
		t.Errorf("group cover %q %q %v", b, mime, err)
	}
	if _, _, err := c.FrontCover(ctx, "nocover", "", 500); !errors.Is(err, ErrNotFound) {
		t.Errorf("no cover: %v", err)
	}
	b, _, err = c.Cover(ctx, "nocover", "", "Radiohead", "OK Computer", 600)
	if err != nil || string(b) != "jpeg-itunes" {
		t.Errorf("itunes fallback %q %v", b, err)
	}
}

// Las peticiones a MusicBrainz van como mucho una por intervalo.
func TestRateLimit(t *testing.T) {
	c, f := newFake(t)
	c.Interval = 60 * time.Millisecond
	start := time.Now()
	for range 4 {
		if _, err := c.SearchRelease(context.Background(), "a", "b", 0); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 170*time.Millisecond {
		t.Errorf("4 requests in %v, want at least 3 intervals", el)
	}
	if f.mbHits.Load() != 4 {
		t.Errorf("%d hits", f.mbHits.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Interval = time.Hour
	if _, err := c.SearchRelease(ctx, "a", "b", 0); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait: %v", err)
	}
}

// MatchAlbum empareja por título aunque los números estén mal o falten,
// usa el número y la duración para desempatar, y deja sin pareja lo que no
// está en la edición.
func TestMatchAlbum(t *testing.T) {
	rel := Release{Media: []Medium{{Position: 1, Tracks: []Track{
		{Disc: 1, Number: 1, Title: "Airbag", Length: 284 * time.Second},
		{Disc: 1, Number: 2, Title: "Paranoid Android", Length: 383 * time.Second},
		{Disc: 1, Number: 3, Title: "Exit Music (For a Film)", Length: 264 * time.Second},
		{Disc: 1, Number: 4, Title: "Intro", Length: 60 * time.Second},
		{Disc: 1, Number: 5, Title: "Intro", Length: 90 * time.Second},
	}}}}
	local := []LocalTrack{
		{Title: "paranoid android (remastered)", Number: 7, Duration: 384},
		{Title: "Exit Music", Number: 3},
		{Title: "Airbág", Duration: 283},
		{Title: "Intro", Duration: 91},
		{Title: "Something Else Entirely", Number: 9},
	}
	m, conf := MatchAlbum(local, rel)
	want := []string{"Paranoid Android", "Exit Music (For a Film)", "Airbag", "Intro"}
	for i, w := range want {
		if m[i].Track == nil || m[i].Track.Title != w {
			t.Fatalf("local %d matched %+v, want %q", i, m[i].Track, w)
		}
	}
	if m[3].Track.Number != 5 {
		t.Errorf("Intro of 91 s matched track %d, want 5 (by duration)", m[3].Track.Number)
	}
	if m[4].Track != nil {
		t.Errorf("unrelated song matched %+v", m[4].Track)
	}
	if conf <= 0.5 || conf >= 1 {
		t.Errorf("confidence %.2f", conf)
	}
	if similarity("Pokémon Theme", "pokemon theme!") != 1 {
		t.Errorf("accents and punctuation should not count")
	}
}

// Contra los servidores de verdad, solo con AUDIOFILUS_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_LIVE") == "" {
		t.Skip("set AUDIOFILUS_LIVE=1 to talk to MusicBrainz")
	}
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rels, err := c.SearchRelease(ctx, "Radiohead", "OK Computer", 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("search: %d releases, first %q (%s, %d tracks, %s)", len(rels), rels[0].Title, rels[0].Date, rels[0].TrackCount, rels[0].ID)
	rel, err := c.GetRelease(ctx, rels[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	tr := rel.Tracks()
	if len(tr) < 12 {
		t.Fatalf("%d tracks", len(tr))
	}
	t.Logf("release: %d tracks, label %q, genres %v, first %q %v", len(tr), rel.Label, rel.Genres, tr[0].Title, tr[0].Length)
	img, mime, err := c.Cover(ctx, rel.ID, rel.GroupID, rel.Artist, rel.Title, 500)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cover: %d bytes %s", len(img), mime)
	recs, err := c.LookupRecording(ctx, "Radiohead", "Karma Police", 264)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("recording: %q %v (%d releases)", recs[0].Title, recs[0].Length, len(recs[0].Releases))
	if img2, _, err := c.ITunesCover(ctx, "Radiohead", "OK Computer", 600); err == nil {
		t.Logf("itunes cover: %d bytes", len(img2))
	} else {
		t.Logf("itunes cover: %v", err)
	}
}
