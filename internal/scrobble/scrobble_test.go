package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// TestListenBrainz comprueba el token en la cabecera, el tipo de envío
// ("playing_now", "single" o "import") y los datos de la escucha.
func TestListenBrainz(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token abc" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
	}))
	defer srv.Close()
	lb := &ListenBrainz{Token: "abc", Base: srv.URL}
	tr := Track{Title: "Song", Artist: "Band", Album: "LP", Duration: 200, At: time.Unix(1000, 0)}
	ctx := context.Background()
	if err := lb.NowPlaying(ctx, tr); err != nil {
		t.Fatal(err)
	}
	if err := lb.Submit(ctx, []Track{tr}); err != nil {
		t.Fatal(err)
	}
	if err := lb.Submit(ctx, []Track{tr, tr}); err != nil {
		t.Fatal(err)
	}
	kinds := []string{got[0]["listen_type"].(string), got[1]["listen_type"].(string), got[2]["listen_type"].(string)}
	if kinds[0] != "playing_now" || kinds[1] != "single" || kinds[2] != "import" {
		t.Errorf("listen types = %v", kinds)
	}
	l := got[1]["payload"].([]any)[0].(map[string]any)
	meta := l["track_metadata"].(map[string]any)
	if l["listened_at"].(float64) != 1000 || meta["track_name"] != "Song" || meta["release_name"] != "LP" ||
		meta["additional_info"].(map[string]any)["duration_ms"].(float64) != 200000 {
		t.Errorf("listen = %+v", l)
	}
}

// TestLastFMSignature comprueba la firma: los parámetros en orden, sin
// "format", y el secreto al final.
func TestLastFMSignature(t *testing.T) {
	l := &LastFM{Key: "k", Secret: "s"}
	p := url.Values{"method": {"auth.getSession"}, "token": {"t"}, "api_key": {"k"}, "format": {"json"}}
	l.sign(p)
	sum := md5.Sum([]byte("api_keykmethodauth.getSessiontokents"))
	if p.Get("api_sig") != hex.EncodeToString(sum[:]) {
		t.Errorf("api_sig = %s", p.Get("api_sig"))
	}
}

// TestQueueRetries comprueba que, sin conexión, la escucha quede guardada
// (también en disco) y se mande junto con la siguiente cuando vuelve; y que
// un token malo no se reintente.
func TestQueueRetries(t *testing.T) {
	var down atomic.Bool
	var listens atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Authorization") == "Token bad" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		var body struct {
			Payload []any `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		listens.Add(int32(len(body.Payload)))
	}))
	defer srv.Close()
	dir := t.TempDir()
	svcs := []Service{&ListenBrainz{Token: "abc", Base: srv.URL}}
	ctx := context.Background()
	tr := Track{Title: "S", Artist: "A", At: time.Now()}

	down.Store(true)
	q := NewQueue(dir)
	if errs := q.Submit(ctx, svcs, &tr); len(errs) != 1 || q.Pending() != 1 {
		t.Fatalf("offline: errs %v, pending %d", errs, q.Pending())
	}
	down.Store(false)
	q = NewQueue(dir) // como tras reiniciar el servicio
	if errs := q.Submit(ctx, svcs, &tr); len(errs) != 0 || q.Pending() != 0 || listens.Load() != 2 {
		t.Errorf("back online: errs %v, pending %d, sent %d (want 2)", errs, q.Pending(), listens.Load())
	}
	bad := []Service{&ListenBrainz{Token: "bad", Base: srv.URL}}
	if errs := q.Submit(ctx, bad, &tr); len(errs) != 1 || q.Pending() != 0 {
		t.Errorf("bad token: errs %v, pending %d (want it dropped)", errs, q.Pending())
	}
}
