package radio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// TestParsePlaylist comprueba las listas de emisoras: .m3u con #EXTINF
// (nombre y logo), .pls con FileN/TitleN en desorden, y que lo que no es
// http se salte.
func TestParsePlaylist(t *testing.T) {
	m3u := "\ufeff#EXTM3U\n#EXTINF:-1 tvg-logo=\"http://x/logo.png\" group-title=\"Rock\",Rock FM\nhttp://rock.example/stream\n# comment\n/local/file.mp3\nhttps://bare.example:8000/live\n"
	got := ParsePlaylist(strings.NewReader(m3u))
	if len(got) != 2 || got[0].Name != "Rock FM" || got[0].Favicon != "http://x/logo.png" || got[0].URL != "http://rock.example/stream" {
		t.Fatalf("m3u = %+v", got)
	}
	if got[1].Name != "bare.example:8000" || got[1].Favicon != "" {
		t.Errorf("an entry without #EXTINF = %+v, want its host as name", got[1])
	}
	pls := "[playlist]\nTitle2=Two\nFile1=http://one.example/\nFile2=http://two.example/\nTitle1=One\nNumberOfEntries=2\n"
	got = ParsePlaylist(strings.NewReader(pls))
	if len(got) != 2 || got[0].Name != "One" || got[1].URL != "http://two.example/" {
		t.Errorf("pls = %+v", got)
	}
	if FromURL("http://a/", "A").UUID != FromURL("http://a/", "B").UUID {
		t.Error("the same address should give the same station")
	}
}

// TestLoadPlaylists comprueba que se lean todas las listas de la carpeta,
// con su nombre como género y sin repetir direcciones.
func TestLoadPlaylists(t *testing.T) {
	dir := t.TempDir()
	write := func(n, s string) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b.pls", "File1=http://two/\nTitle1=Two\n")
	write("a.m3u", "#EXTINF:-1,One\nhttp://one/\nhttp://two/\n")
	write("notes.txt", "http://three/\n")
	got := LoadPlaylists(dir)
	if len(got) != 2 || got[0].Name != "One" || got[0].Tags != "a" || got[1].URL != "http://two/" || got[1].Tags != "a" {
		t.Errorf("LoadPlaylists = %+v", got)
	}
	if LoadPlaylists(filepath.Join(dir, "missing")) != nil {
		t.Error("a missing folder should give nothing")
	}
}

// TestStreamFollowsPlaylist comprueba que una emisora cuya dirección es una
// lista (.pls) se conecte al flujo que nombra, y que una de HLS se rechace.
func TestStreamFollowsPlaylist(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/listen.pls":
			w.Header().Set("Content-Type", "audio/x-scpls")
			fmt.Fprintf(w, "[playlist]\nFile1=%s/stream\n", srv.URL)
		case "/hls.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\nseg1.ts\n")
		case "/stream":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write(make([]byte, 1024))
		}
	}))
	defer srv.Close()
	s := &Stream{url: srv.URL + "/listen.pls"}
	_ = s.connect(context.Background())
	if s.url != srv.URL+"/stream" || s.codec != "mp3" {
		t.Errorf("after the .pls: url %q, codec %q; want the stream, mp3", s.url, s.codec)
	}
	h := &Stream{url: srv.URL + "/hls.m3u8"}
	if err := h.connect(context.Background()); !errors.Is(err, decode.ErrUnsupported) {
		t.Errorf("HLS gave %v, want ErrUnsupported", err)
	}
}
