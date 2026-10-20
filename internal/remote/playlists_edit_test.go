package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// fakeLists son las listas de un servidor falso: id → nombre y canciones.
type fakeLists struct {
	mu    sync.Mutex
	names map[string]string
	songs map[string][]string
	next  int
}

func newFakeLists() *fakeLists {
	return &fakeLists{names: map[string]string{}, songs: map[string][]string{}}
}

func (f *fakeLists) create(name string, ids []string) string {
	f.next++
	id := "pl" + strconv.Itoa(f.next)
	f.names[id], f.songs[id] = name, append([]string(nil), ids...)
	return id
}

// fakeSubsonic es un servidor Subsonic con las listas en memoria.
func fakeSubsonic(t *testing.T, f *fakeLists) *httptest.Server {
	ok := func(w http.ResponseWriter, extra map[string]any) {
		r := map[string]any{"status": "ok", "version": "1.16.1"}
		for k, v := range extra {
			r[k] = v
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": r})
	}
	fail := func(w http.ResponseWriter, msg string) {
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "failed", "error": map[string]any{"code": 70, "message": msg}}})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("u") != "ana" || q.Get("t") == "" {
			fail(w, "wrong user")
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		method := strings.TrimPrefix(r.URL.Path, "/rest/")
		switch method {
		case "ping":
			ok(w, nil)
		case "createPlaylist":
			if id := q.Get("playlistId"); id != "" {
				if _, found := f.names[id]; !found {
					fail(w, "not found")
					return
				}
				f.songs[id] = q["songId"]
				ok(w, nil)
				return
			}
			id := f.create(q.Get("name"), q["songId"])
			ok(w, map[string]any{"playlist": map[string]any{"id": id, "name": q.Get("name")}})
		case "updatePlaylist":
			id := q.Get("playlistId")
			if _, found := f.names[id]; !found {
				fail(w, "not found")
				return
			}
			if n := q.Get("name"); n != "" {
				f.names[id] = n
			}
			// Como Navidrome: se quitan las posiciones (todas sobre la lista
			// de antes) y luego se añade al final.
			rm := map[int]bool{}
			for _, s := range q["songIndexToRemove"] {
				i, _ := strconv.Atoi(s)
				rm[i] = true
			}
			var keep []string
			for i, s := range f.songs[id] {
				if !rm[i] {
					keep = append(keep, s)
				}
			}
			f.songs[id] = append(keep, q["songIdToAdd"]...)
			ok(w, nil)
		case "deletePlaylist":
			delete(f.names, q.Get("id"))
			delete(f.songs, q.Get("id"))
			ok(w, nil)
		case "getPlaylists":
			var ls []map[string]any
			for id, n := range f.names {
				ls = append(ls, map[string]any{"id": id, "name": n})
			}
			ok(w, map[string]any{"playlists": map[string]any{"playlist": ls}})
		case "getPlaylist":
			var es []map[string]any
			for _, s := range f.songs[q.Get("id")] {
				es = append(es, map[string]any{"id": s})
			}
			ok(w, map[string]any{"playlist": map[string]any{"id": q.Get("id"), "entry": es}})
		default:
			fail(w, "unknown "+method)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeJellyfin es un servidor Jellyfin con las listas en memoria (cada
// entrada con su PlaylistItemId propio).
func fakeJellyfin(t *testing.T, f *fakeLists) *httptest.Server {
	entryOf := func(id string, i int) string { return fmt.Sprintf("%s-e%d-%s", id, i, f.songs[id][i]) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Users/AuthenticateByName" {
			json.NewEncoder(w).Encode(map[string]any{"AccessToken": "tok", "User": map[string]any{"Id": "u1"}})
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), `Token="tok"`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Playlists":
			var b struct {
				Name      string
				Ids       []string
				UserId    string
				MediaType string
			}
			json.NewDecoder(r.Body).Decode(&b)
			if b.UserId != "u1" || b.MediaType != "Audio" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": f.create(b.Name, b.Ids)})
		case len(parts) == 3 && parts[0] == "Playlists" && parts[2] == "Items":
			id := parts[1]
			if _, found := f.names[id]; !found {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				var items []map[string]any
				for i, s := range f.songs[id] {
					items = append(items, map[string]any{"Id": s, "PlaylistItemId": entryOf(id, i)})
				}
				json.NewEncoder(w).Encode(map[string]any{"Items": items})
			case http.MethodPost:
				f.songs[id] = append(f.songs[id], strings.Split(q.Get("Ids"), ",")...)
				w.WriteHeader(http.StatusNoContent)
			case http.MethodDelete:
				rm := map[string]bool{}
				for _, e := range strings.Split(q.Get("EntryIds"), ",") {
					rm[e] = true
				}
				var keep []string
				for i, s := range f.songs[id] {
					if !rm[entryOf(id, i)] {
						keep = append(keep, s)
					}
				}
				f.songs[id] = keep
				w.WriteHeader(http.StatusNoContent)
			}
		case len(parts) == 6 && parts[0] == "Playlists" && parts[4] == "Move" && r.Method == http.MethodPost:
			id := parts[1]
			to, _ := strconv.Atoi(parts[5])
			from := -1
			for i := range f.songs[id] {
				if entryOf(id, i) == parts[3] {
					from = i
				}
			}
			order, err := moved(f.songs[id], from, to)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.songs[id] = order
			w.WriteHeader(http.StatusNoContent)
		case len(parts) == 2 && parts[0] == "Playlists" && r.Method == http.MethodPost:
			var b struct{ Name string }
			json.NewDecoder(r.Body).Decode(&b)
			f.names[parts[1]] = b.Name
			w.WriteHeader(http.StatusNoContent)
		case len(parts) == 2 && parts[0] == "Items" && r.Method == http.MethodDelete:
			delete(f.names, parts[1])
			delete(f.songs, parts[1])
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// editFlow crea, añade, mueve, quita, renombra y borra con un editor, y
// comprueba el servidor falso tras cada paso.
func editFlow(t *testing.T, e PlaylistEditor, f *fakeLists) {
	ctx := context.Background()
	check := func(step, id string, want []string) {
		t.Helper()
		f.mu.Lock()
		got := f.songs[id]
		f.mu.Unlock()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: songs %v, want %v", step, got, want)
		}
	}
	id, err := e.CreatePlaylist(ctx, "Gym", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	check("create", id, []string{"a", "b"})
	if err := e.AddToPlaylist(ctx, id, []string{"c", "d", "b"}); err != nil {
		t.Fatal(err)
	}
	check("add", id, []string{"a", "b", "c", "d", "b"})
	if err := e.MovePlaylistItem(ctx, id, 3, 0); err != nil {
		t.Fatal(err)
	}
	check("move up", id, []string{"d", "a", "b", "c", "b"})
	if err := e.MovePlaylistItem(ctx, id, 1, 4); err != nil {
		t.Fatal(err)
	}
	check("move down", id, []string{"d", "b", "c", "b", "a"})
	// La misma canción dos veces: se quita solo la de esa posición.
	if err := e.RemoveFromPlaylist(ctx, id, []int{3, 0}); err != nil {
		t.Fatal(err)
	}
	check("remove", id, []string{"b", "c", "a"})
	// Jellyfin comprueba las posiciones (Subsonic las deja al servidor).
	if _, isJF := e.(*jellyfin); isJF {
		if err := e.RemoveFromPlaylist(ctx, id, []int{7}); err != ErrIndex {
			t.Errorf("remove out of range: %v, want ErrIndex", err)
		}
	}
	if err := e.MovePlaylistItem(ctx, id, 0, 9); err != ErrIndex {
		t.Errorf("move out of range: %v, want ErrIndex", err)
	}
	if err := e.RenamePlaylist(ctx, id, "Gym 2"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	name := f.names[id]
	f.mu.Unlock()
	if name != "Gym 2" {
		t.Errorf("rename: %q", name)
	}
	if err := e.DeletePlaylist(ctx, id); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	_, still := f.names[id]
	f.mu.Unlock()
	if still {
		t.Errorf("delete: playlist still there")
	}
}

// Las listas de Subsonic se editan con createPlaylist, updatePlaylist y
// deletePlaylist; mover reescribe la lista en el orden nuevo.
func TestSubsonicPlaylistEdit(t *testing.T) {
	f := newFakeLists()
	srv := fakeSubsonic(t, f)
	editFlow(t, New(config.Server{Name: "Casa", URL: srv.URL, User: "ana", Password: "x"}).(*Client), f)
}

// Las de Jellyfin, con /Playlists y los PlaylistItemId de cada entrada.
func TestJellyfinPlaylistEdit(t *testing.T) {
	f := newFakeLists()
	srv := fakeJellyfin(t, f)
	editFlow(t, New(config.Server{Name: "Casa", Type: "jellyfin", URL: srv.URL, User: "ana", Password: "x"}).(*jellyfin), f)
}

// Store edita por rutas del servidor y no acepta canciones de otro.
func TestStorePlaylistEdit(t *testing.T) {
	f := newFakeLists()
	srv := fakeSubsonic(t, f)
	cfg := config.Server{Name: "Casa Música", URL: srv.URL, User: "ana", Password: "x"}
	st := NewStore(t.TempDir(), func() []config.Server { return []config.Server{cfg} })
	ctx := context.Background()
	slug := Slug(cfg.Name)
	id, err := st.CreatePlaylist(ctx, slug, "Viaje", []string{SongPath(cfg.Name, "s1"), SongPath(cfg.Name, "s2")})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.songs[id]; !slices.Equal(got, []string{"s1", "s2"}) {
		t.Fatalf("songs %v", got)
	}
	if err := st.AddToPlaylist(ctx, slug, id, []string{"/music/local.mp3"}); err == nil {
		t.Errorf("a local song was accepted in a server playlist")
	}
	if err := st.AddToPlaylist(ctx, slug, id, []string{SongPath("Otro", "z")}); err == nil {
		t.Errorf("a song from another server was accepted")
	}
	if err := st.RenamePlaylist(ctx, "nadie", id, "x"); err == nil {
		t.Errorf("unknown server accepted")
	}
}

// moved lleva un elemento de una posición a otra.
func TestMoved(t *testing.T) {
	s := []string{"a", "b", "c", "d"}
	for _, c := range []struct {
		from, to int
		want     []string
	}{
		{0, 3, []string{"b", "c", "d", "a"}},
		{3, 0, []string{"d", "a", "b", "c"}},
		{1, 2, []string{"a", "c", "b", "d"}},
		{2, 2, []string{"a", "b", "c", "d"}},
	} {
		got, err := moved(s, c.from, c.to)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("moved(%d, %d) = %v %v, want %v", c.from, c.to, got, err, c.want)
		}
	}
	if !slices.Equal(s, []string{"a", "b", "c", "d"}) {
		t.Errorf("moved changed its input: %v", s)
	}
	if _, err := moved(s, -1, 0); err != ErrIndex {
		t.Errorf("negative from: %v", err)
	}
}
