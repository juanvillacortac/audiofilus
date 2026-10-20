package remote

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// TestSubsonicLive prueba contra un servidor de verdad (solo con
// AUDIOFILUS_SUBSONIC=url,usuario,contraseña).
func TestSubsonicLive(t *testing.T) {
	liveServer(t, os.Getenv("AUDIOFILUS_SUBSONIC"), "")
}

// TestJellyfinLive es lo mismo contra Jellyfin (AUDIOFILUS_JELLYFIN).
func TestJellyfinLive(t *testing.T) {
	liveServer(t, os.Getenv("AUDIOFILUS_JELLYFIN"), "jellyfin")
}

// liveServer prueba catálogo, escucha y portada contra el servidor de env
// ("url,usuario,contraseña").
func liveServer(t *testing.T, env, typ string) {
	if env == "" {
		t.Skip("set AUDIOFILUS_SUBSONIC / AUDIOFILUS_JELLYFIN=url,user,password")
	}
	var srv config.Server
	parts := splitN(env, 3)
	srv.Name, srv.Type, srv.URL, srv.User, srv.Password = "Prueba", typ, parts[0], parts[1], parts[2]
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := New(srv)
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	songs, err := c.AllSongs(ctx, nil)
	if err != nil || len(songs) == 0 {
		t.Fatalf("songs: %v (%d)", err, len(songs))
	}
	t.Logf("%d songs in %v; first: %+v", len(songs), time.Since(start), songs[0])
	// Una que el servidor ya haya analizado (Jellyfin tarda en leer las
	// etiquetas tras escanear).
	song := songs[0]
	for _, s := range songs {
		if s.Duration > 0 && s.CoverArt != "" {
			song = s
			break
		}
	}
	t.Logf("using %+v", song)
	st := NewStore(t.TempDir(), func() []config.Server { return []config.Server{srv} })
	p := SongPath(srv.Name, song.ID)
	if _, ready, _ := st.Local(p); ready {
		t.Fatal("ready before download")
	}
	var local string
	for i := 0; i < 300; i++ {
		l, ready, err := st.Local(p)
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			local = l
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	info, err := os.Stat(local)
	if err != nil {
		t.Fatalf("not downloaded: %v", err)
	}
	t.Logf("stream cached: %s (%d KB)", local, info.Size()>>10)
	b, err := st.Cover(ctx, CoverPath(srv.Name, song.CoverArt), 300)
	if err != nil || len(b) < 100 {
		t.Fatalf("cover: %v (%d bytes)", err, len(b))
	}
}

func splitN(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		j := 0
		for j < len(s) && s[j] != ',' {
			j++
		}
		out = append(out, s[:j])
		if j < len(s) {
			s = s[j+1:]
		} else {
			s = ""
		}
	}
	return append(out, s)
}

// TestLiveUserData marca una canción como favorita, comprueba que el
// catálogo la traiga marcada, la desmarca y cuenta una escucha (con
// AUDIOFILUS_JELLYFIN o AUDIOFILUS_SUBSONIC; si no, se salta).
func TestLiveUserData(t *testing.T) {
	for _, c := range []struct{ env, typ string }{{os.Getenv("AUDIOFILUS_JELLYFIN"), "jellyfin"}, {os.Getenv("AUDIOFILUS_SUBSONIC"), ""}} {
		if c.env == "" {
			continue
		}
		parts := splitN(c.env, 3)
		srv := config.Server{Name: "Prueba", Type: c.typ, URL: parts[0], User: parts[1], Password: parts[2]}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		b := New(srv)
		if err := b.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		songs, err := b.AllSongs(ctx, nil)
		if err != nil || len(songs) == 0 {
			t.Fatalf("songs: %d, %v", len(songs), err)
		}
		id := songs[0].ID
		starred := func() bool {
			all, err := b.AllSongs(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range all {
				if s.ID == id {
					return s.Starred != ""
				}
			}
			return false
		}
		if err := b.SetFavorite(ctx, id, true); err != nil {
			t.Fatalf("%s favorite: %v", c.typ, err)
		}
		if !starred() {
			t.Errorf("%s: the song is not starred after SetFavorite", c.typ)
		}
		if err := b.SetFavorite(ctx, id, false); err != nil || starred() {
			t.Errorf("%s: unstar failed (%v)", c.typ, err)
		}
		if c.typ == "" { // Jellyfin no valora
			if err := b.SetRating(ctx, id, 4); err != nil {
				t.Fatalf("rating: %v", err)
			}
			all, _ := b.AllSongs(ctx, nil)
			for _, s := range all {
				if s.ID == id && s.Rating != 4 {
					t.Errorf("rating = %d after SetRating(4)", s.Rating)
				}
			}
			_ = b.SetRating(ctx, id, 0)
		}
		if err := b.Scrobble(ctx, id, time.Now()); err != nil {
			t.Errorf("%s scrobble: %v", c.typ, err)
		}
		cancel()
	}
}
