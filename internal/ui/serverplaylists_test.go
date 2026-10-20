package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/remote"
)

// fakePlaylists es un servidor Subsonic con las listas en memoria (lo justo
// para editarlas y volver a leerlas).
func fakePlaylists(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	names, songs, next := map[string]string{}, map[string][]string{}, 0
	ok := func(w http.ResponseWriter, extra map[string]any) {
		r := map[string]any{"status": "ok", "version": "1.16.1"}
		for k, v := range extra {
			r[k] = v
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": r})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		switch strings.TrimPrefix(r.URL.Path, "/rest/") {
		case "createPlaylist":
			if id := q.Get("playlistId"); id != "" {
				songs[id] = q["songId"]
				ok(w, nil)
				return
			}
			next++
			id := "pl" + strconv.Itoa(next)
			names[id], songs[id] = q.Get("name"), q["songId"]
			ok(w, map[string]any{"playlist": map[string]any{"id": id, "name": q.Get("name")}})
		case "updatePlaylist":
			id := q.Get("playlistId")
			if n := q.Get("name"); n != "" {
				names[id] = n
			}
			rm := map[int]bool{}
			for _, s := range q["songIndexToRemove"] {
				i, _ := strconv.Atoi(s)
				rm[i] = true
			}
			var keep []string
			for i, s := range songs[id] {
				if !rm[i] {
					keep = append(keep, s)
				}
			}
			songs[id] = append(keep, q["songIdToAdd"]...)
			ok(w, nil)
		case "deletePlaylist":
			delete(names, q.Get("id"))
			delete(songs, q.Get("id"))
			ok(w, nil)
		case "getPlaylists":
			var ls []map[string]any
			for id, n := range names {
				ls = append(ls, map[string]any{"id": id, "name": n})
			}
			ok(w, map[string]any{"playlists": map[string]any{"playlist": ls}})
		case "getPlaylist":
			var es []map[string]any
			for _, s := range songs[q.Get("id")] {
				es = append(es, map[string]any{"id": s})
			}
			ok(w, map[string]any{"playlist": map[string]any{"id": q.Get("id"), "entry": es}})
		default:
			ok(w, nil)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Con una biblioteca de servidor abierta: crear una lista suya con
// canciones suyas, mover y quitar una canción, renombrarla y borrarla; cada
// cambio llega al servidor y la vista abierta lo sigue.
func TestServerPlaylistEditing(t *testing.T) {
	fake := fakePlaylists(t)
	srv := config.Server{Name: "Navi", URL: fake.URL, User: "ana", Password: "x"}
	a := &App{paths: config.Paths{Data: t.TempDir()}}
	a.store = remote.NewStore(a.paths.Data, func() []config.Server { return []config.Server{srv} })
	src := &remoteSource{srv: srv, cat: catalog{Songs: []remote.Song{
		{ID: "s1", Title: "Uno", Artist: "A", Album: "X", Track: 1},
		{ID: "s2", Title: "Dos", Artist: "A", Album: "X", Track: 2},
		{ID: "s3", Title: "Tres", Artist: "A", Album: "X", Track: 3},
	}}}
	src.lib, src.songs = buildRemoteLibrary(srv.Name, src.cat)
	a.remotes, a.source, a.lib = []*remoteSource{src}, 1, src.lib
	a.libTab = tabLists
	a.stacks[tabLists] = []*view{a.rootView(tabLists)}
	wait := func() {
		select {
		case r := <-a.srvPlCh:
			a.srvApply(r)
		case <-time.After(10 * time.Second):
			t.Fatal("no answer from the server")
		}
	}
	titles := func(pl *Playlist) string {
		var s []string
		for _, x := range pl.Songs {
			s = append(s, x.Title)
		}
		return strings.Join(s, ",")
	}

	// Añadir el álbum a una lista nueva del servidor.
	if !a.srvAddToPlaylist(src.lib.Albums[0].Songs) {
		t.Fatal("server songs didn't get the server's playlists")
	}
	a.menu.items[0].do() // "Nueva lista en Navi"
	a.prompt.done("Gym")
	wait()
	if len(a.lib.Playlists) != 1 || a.lib.Playlists[0].Name != "Gym" || titles(a.lib.Playlists[0]) != "Uno,Dos,Tres" {
		t.Fatalf("playlists after create: %+v", a.lib.Playlists)
	}

	// Abrirla y bajar la primera canción.
	pl := a.lib.Playlists[0]
	a.push(&view{kind: vPlaylist, title: pl.Name, songs: pl.Songs, pl: pl})
	a.srvPlaylistSongItems(pl, 0)[1].do()
	wait()
	v := a.cur()
	if titles(v.pl) != "Dos,Uno,Tres" || v.sel != 1 {
		t.Fatalf("after move down: %q sel %d", titles(v.pl), v.sel)
	}

	// Quitar la última.
	a.srvPlaylistSongItems(v.pl, 2)[2].do()
	wait()
	if titles(a.cur().pl) != "Dos,Uno" {
		t.Fatalf("after remove: %q", titles(a.cur().pl))
	}

	// Renombrarla.
	a.srvPlaylistItems(a.cur().pl)[0].do()
	a.prompt.done("Correr")
	wait()
	if a.cur().title != "Correr" {
		t.Fatalf("after rename: view title %q", a.cur().title)
	}

	// Borrarla (con la confirmación): la vista sale de ella.
	for _, it := range a.srvPlaylistItems(a.cur().pl) {
		if it.danger {
			it.do()
		}
	}
	a.menu.items[1].do() // "Sí, borrarla"
	wait()
	if len(a.lib.Playlists) != 0 || a.cur().kind != vPlaylists {
		t.Fatalf("after delete: %d playlists, view %v, message %q", len(a.lib.Playlists), a.cur().kind, a.message)
	}

	// Canciones de la tarjeta: a las listas de aquí, no a las del servidor.
	if a.srvAddToPlaylist([]Song{{Path: "/music/a.mp3"}}) {
		t.Error("local songs were offered the server's playlists")
	}
}
