// Package scrobble cuenta lo que se escucha en Last.fm y ListenBrainz: el
// "sonando ahora" al empezar una canción y la escucha cuando ya sonó la
// mitad (o cuatro minutos). Lo que no se pudo mandar (sin internet) espera
// en data/scrobbles.json y se reintenta.
package scrobble

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Track es lo que se cuenta de una canción.
type Track struct {
	Title, Artist, Album string
	Duration             float64 // segundos
	At                   time.Time
}

// Service es un sitio donde contar escuchas.
type Service interface {
	Name() string
	NowPlaying(ctx context.Context, t Track) error
	Submit(ctx context.Context, ts []Track) error
}

// DefaultLastFMKey y DefaultLastFMSecret son la aplicación de Last.fm de
// audiofilus: las pone la compilación de los releases (-ldflags -X, desde
// los secretos del repositorio). Sin ellas, Last.fm necesita las propias en
// audiofilus.json.
var DefaultLastFMKey, DefaultLastFMSecret string

// UserAgent es como se presenta audiofilus.
const UserAgent = "audiofilus (https://github.com/juanvillacortac/audiofilus)"

var client = &http.Client{Timeout: 20 * time.Second}

// ListenBrainz cuenta en listenbrainz.org con el token del usuario (su
// página de ajustes lo da).
type ListenBrainz struct {
	Token string
	Base  string // vacío: api.listenbrainz.org (las pruebas ponen otro)
}

func (l *ListenBrainz) Name() string { return "listenbrainz" }

func (l *ListenBrainz) base() string {
	if l.Base != "" {
		return l.Base
	}
	return "https://api.listenbrainz.org"
}

// lbListen es una escucha en el formato de ListenBrainz.
type lbListen struct {
	ListenedAt int64 `json:"listened_at,omitempty"`
	Meta       struct {
		Artist string         `json:"artist_name"`
		Track  string         `json:"track_name"`
		Album  string         `json:"release_name,omitempty"`
		Info   map[string]any `json:"additional_info"`
	} `json:"track_metadata"`
}

func lbOf(t Track, at bool) lbListen {
	var l lbListen
	if at {
		l.ListenedAt = t.At.Unix()
	}
	l.Meta.Artist, l.Meta.Track, l.Meta.Album = t.Artist, t.Title, t.Album
	l.Meta.Info = map[string]any{"media_player": "audiofilus", "submission_client": "audiofilus"}
	if t.Duration > 0 {
		l.Meta.Info["duration_ms"] = int(t.Duration * 1000)
	}
	return l
}

func (l *ListenBrainz) post(ctx context.Context, kind string, ls []lbListen) error {
	body, _ := json.Marshal(map[string]any{"listen_type": kind, "payload": ls})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base()+"/1/submit-listens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+l.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &httpError{resp.StatusCode, fmt.Sprintf("listenbrainz: %s: %s", resp.Status, bytes.TrimSpace(b))}
	}
	return nil
}

func (l *ListenBrainz) NowPlaying(ctx context.Context, t Track) error {
	return l.post(ctx, "playing_now", []lbListen{lbOf(t, false)})
}

func (l *ListenBrainz) Submit(ctx context.Context, ts []Track) error {
	kind := "single"
	if len(ts) > 1 {
		kind = "import"
	}
	ls := make([]lbListen, len(ts))
	for i, t := range ts {
		ls[i] = lbOf(t, true)
	}
	return l.post(ctx, kind, ls)
}

// ValidateToken comprueba el token y devuelve el usuario.
func (l *ListenBrainz) ValidateToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base()+"/1/validate-token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+l.Token)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Valid bool   `json:"valid"`
		User  string `json:"user_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if !r.Valid {
		return "", errors.New("listenbrainz: invalid token")
	}
	return r.User, nil
}

// LastFM cuenta en last.fm. Necesita la clave de una aplicación de Last.fm
// (Key y Secret: last.fm/api/account/create) y la sesión del usuario
// (Session), que da el inicio de sesión web (ver AuthURL y GetSession).
type LastFM struct {
	Key, Secret, Session string
	Base                 string // vacío: ws.audioscrobbler.com
}

func (l *LastFM) Name() string { return "lastfm" }

func (l *LastFM) base() string {
	if l.Base != "" {
		return l.Base
	}
	return "https://ws.audioscrobbler.com/2.0/"
}

// sign firma los parámetros como pide Last.fm: los nombres en orden, cada
// uno seguido de su valor, el secreto al final y el md5 de todo.
func (l *LastFM) sign(p url.Values) {
	keys := make([]string, 0, len(p))
	for k := range p {
		if k != "format" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + p.Get(k))
	}
	b.WriteString(l.Secret)
	sum := md5.Sum([]byte(b.String()))
	p.Set("api_sig", hex.EncodeToString(sum[:]))
}

// call llama a un método de la API (firmado) y lee la respuesta en out.
func (l *LastFM) call(ctx context.Context, method string, p url.Values, post bool, out any) error {
	p.Set("method", method)
	p.Set("api_key", l.Key)
	l.sign(p)
	p.Set("format", "json")
	var req *http.Request
	var err error
	if post {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, l.base(), strings.NewReader(p.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, l.base()+"?"+p.Encode(), nil)
	}
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != 0 {
		return &httpError{e.Error, fmt.Sprintf("lastfm: %s (%d)", e.Message, e.Error)}
	}
	if resp.StatusCode != http.StatusOK {
		return &httpError{resp.StatusCode, "lastfm: " + resp.Status}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// GetToken pide un token para iniciar sesión; AuthURL es la página donde
// el usuario lo autoriza (la interfaz la muestra como QR).
func (l *LastFM) GetToken(ctx context.Context) (string, error) {
	var r struct {
		Token string `json:"token"`
	}
	err := l.call(ctx, "auth.getToken", url.Values{}, false, &r)
	return r.Token, err
}

// AuthURL es la página para autorizar el token.
func (l *LastFM) AuthURL(token string) string {
	return "https://www.last.fm/api/auth/?api_key=" + url.QueryEscape(l.Key) + "&token=" + url.QueryEscape(token)
}

// GetSession cambia un token ya autorizado por la sesión (y el usuario).
// Mientras no se autorice, falla con el error 14 de Last.fm.
func (l *LastFM) GetSession(ctx context.Context, token string) (key, user string, err error) {
	var r struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	err = l.call(ctx, "auth.getSession", url.Values{"token": {token}}, false, &r)
	return r.Session.Key, r.Session.Name, err
}

func (l *LastFM) NowPlaying(ctx context.Context, t Track) error {
	p := url.Values{"artist": {t.Artist}, "track": {t.Title}, "sk": {l.Session}}
	if t.Album != "" {
		p.Set("album", t.Album)
	}
	if t.Duration > 0 {
		p.Set("duration", strconv.Itoa(int(t.Duration)))
	}
	return l.call(ctx, "track.updateNowPlaying", p, true, nil)
}

func (l *LastFM) Submit(ctx context.Context, ts []Track) error {
	// De 50 en 50, lo más que admite Last.fm.
	for len(ts) > 0 {
		n := min(len(ts), 50)
		p := url.Values{"sk": {l.Session}}
		for i, t := range ts[:n] {
			k := func(s string) string { return fmt.Sprintf("%s[%d]", s, i) }
			p.Set(k("artist"), t.Artist)
			p.Set(k("track"), t.Title)
			p.Set(k("timestamp"), strconv.FormatInt(t.At.Unix(), 10))
			if t.Album != "" {
				p.Set(k("album"), t.Album)
			}
			if t.Duration > 0 {
				p.Set(k("duration"), strconv.Itoa(int(t.Duration)))
			}
		}
		if err := l.call(ctx, "track.scrobble", p, true, nil); err != nil {
			return err
		}
		ts = ts[n:]
	}
	return nil
}

// httpError es un error del servidor, con su código: los de credenciales
// no se reintentan (ver permanent).
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

// permanent dice si reintentar no sirve: credenciales malas (401, o las de
// Last.fm: 4 token, 9 sesión, 10 clave, 26 clave suspendida) o una escucha
// que el servicio rechaza (400).
func permanent(err error) bool {
	var h *httpError
	if !errors.As(err, &h) {
		return false
	}
	switch h.code {
	case 400, 401, 4, 9, 10, 26:
		return true
	}
	return false
}

// Queue manda las escuchas a los servicios, y guarda en un archivo las que
// no se pudieron mandar (sin conexión) para reintentarlas luego.
type Queue struct {
	file string

	mu      sync.Mutex
	pending map[string][]Track // por servicio
}

// NewQueue usa dataDir/scrobbles.json.
func NewQueue(dataDir string) *Queue {
	q := &Queue{file: filepath.Join(dataDir, "scrobbles.json"), pending: map[string][]Track{}}
	if b, err := os.ReadFile(q.file); err == nil {
		_ = json.Unmarshal(b, &q.pending)
	}
	return q
}

// Pending es cuántas escuchas esperan.
func (q *Queue) Pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, ts := range q.pending {
		n += len(ts)
	}
	return n
}

// Submit manda t a los servicios (con lo que esperaba antes); lo que falle
// por la conexión queda guardado.
func (q *Queue) Submit(ctx context.Context, svcs []Service, t *Track) []error {
	var errs []error
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, s := range svcs {
		ts := q.pending[s.Name()]
		if t != nil {
			ts = append(ts, *t)
		}
		if len(ts) == 0 {
			continue
		}
		err := s.Submit(ctx, ts)
		switch {
		case err == nil:
			delete(q.pending, s.Name())
		case permanent(err):
			delete(q.pending, s.Name()) // reintentar no sirve
			errs = append(errs, err)
		default:
			// Sin conexión: se guardan (como mucho las últimas 5000).
			q.pending[s.Name()] = ts[max(0, len(ts)-5000):]
			errs = append(errs, err)
		}
	}
	q.saveLocked()
	return errs
}

func (q *Queue) saveLocked() {
	if len(q.pending) == 0 {
		_ = os.Remove(q.file)
		return
	}
	if b, err := json.Marshal(q.pending); err == nil {
		_ = os.WriteFile(q.file, b, 0o644)
	}
}
