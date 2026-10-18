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
