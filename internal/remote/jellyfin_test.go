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
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// TestJellyfinAllSongs pide un catálogo de varias páginas a un servidor falso:
// salen todas, sin repetir, en orden de álbum, y sin MediaSources (#13). El
// avance trae el total, y la primera página llega antes, una sola vez.
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
		if q.Get("SortOrder") != "Descending,Ascending" {
			t.Errorf("SortOrder=%q, want the newest first", q.Get("SortOrder"))
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
	var last, firsts, firstLen int
	songs, err := j.AllSongs(context.Background(), func(p Progress) {
		last = max(last, p.Got)
		if p.Total != total {
			t.Errorf("total = %d", p.Total)
		}
		if p.First != nil {
			firsts++
			firstLen = len(p.First)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != total || last != total {
		t.Fatalf("songs=%d progress=%d, want %d", len(songs), last, total)
	}
	if firsts != 1 || firstLen != 7 {
		t.Errorf("first page sent %d times with %d songs", firsts, firstLen)
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

// TestChangesApply comprueba cómo se ponen los cambios sobre el catálogo:
// lo cambiado se reemplaza, lo nuevo se agrega, los favoritos salen de la
// lista y whole avisa cuando el servidor tiene otro número de canciones.
func TestChangesApply(t *testing.T) {
	saved := []Song{{ID: "a", Title: "A", Album: "x"}, {ID: "b", Title: "B", Album: "x", Starred: "1"}}
	same := Changes{Favorites: map[string]bool{"b": true}, Total: 2}
	if _, whole, changed := same.Apply(saved); !whole || changed {
		t.Errorf("nothing changed: whole=%v changed=%v", whole, changed)
	}
	ch := Changes{Songs: []Song{{ID: "a", Title: "A2", Album: "x"}, {ID: "c", Title: "C", Album: "y"}},
		Favorites: map[string]bool{"a": true}, Total: 3}
	out, whole, changed := ch.Apply(saved)
	if !whole || !changed || len(out) != 3 {
		t.Fatalf("whole=%v changed=%v songs=%d", whole, changed, len(out))
	}
	by := map[string]Song{}
	for _, s := range out {
		by[s.ID] = s
	}
	if by["a"].Title != "A2" || by["a"].Starred != "1" || by["b"].Starred != "" || by["c"].Title != "C" {
		t.Errorf("got %+v", out)
	}
	if saved[0].Title != "A" {
		t.Error("Apply changed the saved catalog")
	}
	deleted := Changes{Favorites: map[string]bool{}, Total: 1}
	if _, whole, _ := deleted.Apply(saved); whole {
		t.Error("a song was deleted on the server but whole is true")
	}
}

// TestJellyfinChanged pide los cambios a un servidor falso: usa la fecha del
// servidor (cabecera Date) y no la del reloj local, pide solo lo guardado
// desde since y saca los favoritos aparte.
func TestJellyfinChanged(t *testing.T) {
	serverNow := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	since := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", serverNow.Format(http.TimeFormat))
		if r.URL.Path == "/Users/AuthenticateByName" {
			json.NewEncoder(w).Encode(map[string]any{"AccessToken": "t", "User": map[string]any{"Id": "u"}})
			return
		}
		q := r.URL.Query()
		body := map[string]any{"Items": []any{}, "TotalRecordCount": 40}
		switch {
		case q.Get("Limit") == "0":
		case q.Get("Filters") == "IsFavorite":
			body["Items"] = []any{map[string]any{"Id": "f1"}, map[string]any{"Id": "f2"}}
		case q.Get("MinDateLastSaved") != "":
			if q.Get("MinDateLastSaved") != since.Format(time.RFC3339) {
				t.Errorf("MinDateLastSaved=%q", q.Get("MinDateLastSaved"))
			}
			body["Items"] = []any{map[string]any{"Id": "n1", "Name": "new"}}
		default:
			t.Errorf("unexpected query %v", q)
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	j := newJellyfin(config.Server{URL: srv.URL, User: "a", Password: "b", Type: "jellyfin"})
	ch, err := j.Changed(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total != 40 || !ch.At.Equal(serverNow) || len(ch.Songs) != 1 || ch.Songs[0].ID != "n1" ||
		len(ch.Favorites) != 2 || !ch.Favorites["f2"] {
		t.Errorf("got %+v", ch)
	}
}
