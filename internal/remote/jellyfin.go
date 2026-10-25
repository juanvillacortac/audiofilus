package remote

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// Jellyfin (y Emby, que habla casi lo mismo): se inicia sesión con usuario y
// contraseña y las demás peticiones llevan el token. Las canciones son los
// Items de tipo Audio; las portadas, las imágenes Primary del álbum (o de la
// canción si no tiene).
type jellyfin struct {
	srv  config.Server
	http *http.Client

	mu     sync.Mutex
	token  string
	userID string
}

func newJellyfin(srv config.Server) *jellyfin {
	return &jellyfin{srv: srv, http: &http.Client{Timeout: 60 * time.Second}}
}

// deviceID es un id fijo por usuario, servidor y programa (Jellyfin lista
// las sesiones por dispositivo, y al iniciar sesión cierra la anterior del
// mismo: la interfaz y el servicio se cerrarían la sesión el uno al otro).
func (j *jellyfin) deviceID() string {
	sum := sha1.Sum([]byte("audiofilus|" + j.srv.URL + "|" + j.srv.User + "|" + filepath.Base(os.Args[0])))
	return hex.EncodeToString(sum[:8])
}

// auth es la cabecera con que Jellyfin identifica al cliente (y el token,
// si ya hay sesión).
func (j *jellyfin) auth() string {
	h := fmt.Sprintf(`MediaBrowser Client="Audiofilus", Device="Audiofilus", DeviceId="%s", Version="1.0"`, j.deviceID())
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.token != "" {
		h += fmt.Sprintf(`, Token="%s"`, j.token)
	}
	return h
}

func (j *jellyfin) base() string { return strings.TrimRight(j.srv.URL, "/") }

// call hace una petición a la API y deja el JSON de la respuesta en out.
func (j *jellyfin) call(ctx context.Context, method, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, j.base()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", j.auth())
	req.Header.Set("Content-Type", "application/json")
	resp, err := j.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("jellyfin: wrong user or password")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("jellyfin: %s %s", path, resp.Status)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Ping inicia la sesión (con eso comprueba todo).
func (j *jellyfin) Ping(ctx context.Context) error {
	var r struct {
		AccessToken string
		User        struct {
			ID string `json:"Id"`
		}
	}
	if err := j.call(ctx, "POST", "/Users/AuthenticateByName", map[string]string{"Username": j.srv.User, "Pw": j.srv.Password}, &r); err != nil {
		return err
	}
	j.mu.Lock()
	j.token, j.userID = r.AccessToken, r.User.ID
	j.mu.Unlock()
	return nil
}

// login inicia la sesión si aún no la hay.
func (j *jellyfin) login(ctx context.Context) error {
	j.mu.Lock()
	ok := j.token != ""
	j.mu.Unlock()
	if ok {
		return nil
	}
	return j.Ping(ctx)
}

// jfItem es un Item de Jellyfin.
type jfItem struct {
	ID                string   `json:"Id"`
	Name              string   `json:"Name"`
	Album             string   `json:"Album"`
	AlbumID           string   `json:"AlbumId"`
	AlbumArtist       string   `json:"AlbumArtist"`
	Artists           []string `json:"Artists"`
	IndexNumber       int      `json:"IndexNumber"`
	ParentIndexNumber int      `json:"ParentIndexNumber"`
	RunTimeTicks      int64    `json:"RunTimeTicks"`
	Container         string   `json:"Container"`
	AlbumImageTag     string   `json:"AlbumPrimaryImageTag"`
	ImageTags         struct {
		Primary string `json:"Primary"`
	} `json:"ImageTags"`
	UserData struct {
		IsFavorite bool `json:"IsFavorite"`
	} `json:"UserData"`
}

// song pasa un Item a Song (la portada: la del álbum o la propia).
func (it jfItem) song() Song {
	s := Song{ID: it.ID, Title: it.Name, Album: it.Album, AlbumID: it.AlbumID, AlbumArtist: it.AlbumArtist,
		Track: it.IndexNumber, Disc: it.ParentIndexNumber, Duration: int(it.RunTimeTicks / 10_000_000), Suffix: it.Container}
	s.Artist = strings.Join(it.Artists, ", ")
	if it.UserData.IsFavorite {
		s.Starred = "1"
	}
	if s.Artist == "" {
		s.Artist = it.AlbumArtist
	}
	switch {
	case it.AlbumImageTag != "" && it.AlbumID != "":
		s.CoverArt = it.AlbumID
	case it.ImageTags.Primary != "":
		s.CoverArt = it.ID
	}
	return s
}

// items pide una página de Items del usuario.
func (j *jellyfin) items(ctx context.Context, q url.Values) ([]jfItem, int, error) {
	j.mu.Lock()
	user := j.userID
	j.mu.Unlock()
	var r struct {
		Items            []jfItem
		TotalRecordCount int
	}
	err := j.call(ctx, "GET", "/Users/"+user+"/Items?"+q.Encode(), nil, &r)
	return r.Items, r.TotalRecordCount, err
}

// AllSongs saca todas las canciones, de 500 en 500.
func (j *jellyfin) AllSongs(ctx context.Context, progress func(n int)) ([]Song, error) {
	if err := j.login(ctx); err != nil {
		return nil, err
	}
	const page = 500
	var out []Song
	for start := 0; ; start += page {
		q := url.Values{"IncludeItemTypes": {"Audio"}, "Recursive": {"true"}, "StartIndex": {fmt.Sprint(start)},
			"Limit": {fmt.Sprint(page)}, "Fields": {"MediaSources"}, "SortBy": {"AlbumArtist,Album,ParentIndexNumber,IndexNumber"}}
		items, total, err := j.items(ctx, q)
		if err != nil {
			return out, err
		}
		for _, it := range items {
			out = append(out, it.song())
		}
		if progress != nil {
			progress(len(out))
		}
		if len(items) < page || len(out) >= total {
			return out, nil
		}
	}
}

// Playlists son las listas del usuario, con sus canciones.
func (j *jellyfin) Playlists(ctx context.Context) ([]Playlist, error) {
	if err := j.login(ctx); err != nil {
		return nil, err
	}
	lists, _, err := j.items(ctx, url.Values{"IncludeItemTypes": {"Playlist"}, "Recursive": {"true"}})
	if err != nil {
		return nil, err
	}
	var out []Playlist
	for _, l := range lists {
		items, _, err := j.items(ctx, url.Values{"ParentId": {l.ID}, "IncludeItemTypes": {"Audio"}})
		if err != nil {
			continue
		}
		pl := Playlist{ID: l.ID, Name: l.Name}
		for _, it := range items {
			pl.Songs = append(pl.Songs, it.ID)
		}
		out = append(out, pl)
	}
	return out, nil
}

// key es el token para las URL (que no llevan cabeceras).
func (j *jellyfin) key() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.token
}

// StreamURL es para escuchar: el endpoint "universal" transcodifica a MP3
// con la calidad del servidor, o da el original con "original".
func (j *jellyfin) StreamURL(id string) string {
	j.mu.Lock()
	user := j.userID
	j.mu.Unlock()
	v := url.Values{"UserId": {user}, "DeviceId": {j.deviceID()}, "ApiKey": {j.key()}}
	if j.srv.Quality == "original" {
		v.Set("static", "true")
		return j.base() + "/Audio/" + id + "/stream?" + v.Encode()
	}
	kbps := j.srv.Quality
	if kbps == "" {
		kbps = "192"
	}
	v.Set("Container", "mp3")
	v.Set("TranscodingContainer", "mp3")
	v.Set("TranscodingProtocol", "http")
	v.Set("AudioCodec", "mp3")
	v.Set("MaxStreamingBitrate", kbps+"000")
	return j.base() + "/Audio/" + id + "/universal?" + v.Encode()
}

// DownloadURL es el archivo original.
func (j *jellyfin) DownloadURL(id string) string {
	return j.base() + "/Items/" + id + "/Download?ApiKey=" + url.QueryEscape(j.key())
}

// CoverURL es la imagen Primary de un Item a size píxeles.
func (j *jellyfin) CoverURL(id string, size int) string {
	return fmt.Sprintf("%s/Items/%s/Images/Primary?maxHeight=%d&maxWidth=%d&format=Jpg&ApiKey=%s", j.base(), id, size, size, url.QueryEscape(j.key()))
}

func (j *jellyfin) fetch(ctx context.Context, u string) (*http.Response, error) {
	return getFile(ctx, u)
}
