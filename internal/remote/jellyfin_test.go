package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// TestJellyfinAllSongs pide un catálogo de varias páginas a un servidor falso:
// salen todas, sin repetir, en orden de álbum, y sin MediaSources (#13).
func TestJellyfinAllSongs(t *testing.T) {
	old := jfPage
	jfPage = 7
	defer func() { jfPage = old }()
	const total = 53
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Users/AuthenticateByName" {
			json.NewEncoder(w).Encode(map[string]any{"AccessToken": "t", "User": map[string]any{"Id": "u"}})
			return
		}
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("Fields") != "" {
			t.Errorf("Fields=%q", q.Get("Fields"))
		}
		start, _ := strconv.Atoi(q.Get("StartIndex"))
		lim, _ := strconv.Atoi(q.Get("Limit"))
		var items []map[string]any
		for i := start; i < min(start+lim, total); i++ {
			// Álbumes al revés de como los da el servidor, dos discos cada uno.
			items = append(items, map[string]any{"Id": fmt.Sprint(i), "Name": fmt.Sprint(i),
				"AlbumArtist": "A", "Album": fmt.Sprintf("album %02d", 9-i/6),
				"ParentIndexNumber": 1 + i%2, "IndexNumber": 1 + i%6/2})
		}
		body := map[string]any{"Items": items}
		if q.Get("EnableTotalRecordCount") != "false" {
			body["TotalRecordCount"] = total
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	j := newJellyfin(config.Server{URL: srv.URL, User: "a", Password: "b", Type: "jellyfin"})
	var last int
	songs, err := j.AllSongs(context.Background(), func(n int) { last = max(last, n) })
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != total || last != total {
		t.Fatalf("songs=%d progress=%d, want %d", len(songs), last, total)
	}
	if n := calls.Load(); n != (total+6)/7 {
		t.Errorf("calls=%d", n)
	}
	seen := map[string]bool{}
	for i, s := range songs {
		if seen[s.ID] {
			t.Fatalf("repeated %s", s.ID)
		}
		seen[s.ID] = true
		if i > 0 {
			p := songs[i-1]
			if p.Album > s.Album || p.Album == s.Album && (p.Disc > s.Disc || p.Disc == s.Disc && p.Track > s.Track) {
				t.Fatalf("out of order at %d: %+v then %+v", i, p, s)
			}
		}
	}
}
