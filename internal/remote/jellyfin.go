package remote

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	date   time.Time // la hora del servidor en la última respuesta (cabecera Date)
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
		return netErr("jellyfin: "+apiPath(path), err)
	}
	defer resp.Body.Close()
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		j.mu.Lock()
		j.date = d
		j.mu.Unlock()
	}
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

// Catálogo de Jellyfin (#13). Lo caro era pedir MediaSources (más del doble
// de JSON y Jellyfin lo arma canción por canción) y ordenar por cuatro campos
// en cada página. Ahora va sin Fields extra, de 1000 en 1000 y con varias
// páginas a la vez; el orden de álbum lo pone sortSongs. Las páginas van de la
// más nueva a la más vieja: la primera son las 1000 últimas que se agregaron,
// y se muestran mientras llega el resto (con 60 000 canciones tarda minutos).
var (
	jfPage    = 1000 // var para que las pruebas usen páginas chicas
	jfWorkers = 4
)

// songsQuery es la consulta de una página de canciones.
func songsQuery(start int, count bool) url.Values {
	return url.Values{"IncludeItemTypes": {"Audio"}, "Recursive": {"true"},
		"StartIndex": {fmt.Sprint(start)}, "Limit": {fmt.Sprint(jfPage)}, "SortBy": {"DateCreated,SortName"}, "SortOrder": {"Descending,Ascending"},
		"EnableImageTypes": {"Primary"}, "ImageTypeLimit": {"1"},
		"EnableTotalRecordCount": {fmt.Sprint(count)}}
}

// AllSongs saca todas las canciones: la primera página trae el total y el
// resto se pide en paralelo.
func (j *jellyfin) AllSongs(ctx context.Context, progress func(Progress)) ([]Song, error) {
	if err := j.login(ctx); err != nil {
		return nil, err
	}
	first, total, err := j.items(ctx, songsQuery(0, true))
	if err != nil {
		return nil, err
	}
	pages := make([][]jfItem, (max(total, len(first))+jfPage-1)/jfPage)
	if len(pages) == 0 {
		return nil, nil
	}
	pages[0] = first
	if len(pages) == 1 {
		return jfSongs(pages, total), nil
	}
	// progress va con candado: así quien lo pasa no tiene que cuidarse de
	// los hilos.
	var mu sync.Mutex
	got := len(first)
	report := func(n int, first []Song) {
		mu.Lock()
		defer mu.Unlock()
		got += n
		if progress != nil {
			progress(Progress{Got: got, Total: total, First: first})
		}
	}
	report(0, jfSongs(pages[:1], len(first)))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	next := make(chan int)
	errc := make(chan error, jfWorkers)
	var wg sync.WaitGroup
	for range min(jfWorkers, len(pages)-1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range next {
				items, _, err := j.items(ctx, songsQuery(p*jfPage, false))
				if err != nil {
					errc <- err
					cancel()
					return
				}
				pages[p] = items
				report(len(items), nil)
			}
		}()
	}
feed:
	for p := 1; p < len(pages); p++ {
		select {
		case next <- p:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	select {
	case err := <-errc:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return jfSongs(pages, total), nil
}

// jfSongs junta las páginas en canciones ordenadas por álbum. Una canción
// puede caer en dos páginas si la biblioteca cambia mientras tanto: se queda
// la primera.
func jfSongs(pages [][]jfItem, total int) []Song {
	seen := make(map[string]bool, total)
	out := make([]Song, 0, total)
	for _, pg := range pages {
		for _, it := range pg {
			if !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it.song())
			}
		}
	}
	sortSongs(out)
	return out
}

// Sincronizar sin bajar todo (#13). Jellyfin dice qué canciones se agregaron
// o cambiaron desde una fecha (MinDateLastSaved, con la hora del servidor: la
// de las portátiles suele estar mal), pero no cuáles se borraron ni los
// favoritos (marcar uno no cambia la canción). Así que se pide también el
// total, para saber si falta algo, y la lista de favoritos, que es corta.

// audioCount pide cuántas canciones hay y devuelve también la hora del
// servidor en ese momento.
func (j *jellyfin) audioCount(ctx context.Context) (int, time.Time, error) {
	if err := j.login(ctx); err != nil {
		return 0, time.Time{}, err
	}
	_, total, err := j.items(ctx, url.Values{"IncludeItemTypes": {"Audio"}, "Recursive": {"true"},
		"Limit": {"0"}, "EnableTotalRecordCount": {"true"}})
	j.mu.Lock()
	at := j.date
	j.mu.Unlock()
	if at.IsZero() {
		at = time.Now()
	}
	return total, at, err
}

// SyncPoint es la hora del servidor antes de una carga entera: lo que
// cambie desde entonces sale en el próximo Changed.
func (j *jellyfin) SyncPoint(ctx context.Context) (time.Time, error) {
	_, at, err := j.audioCount(ctx)
	return at, err
}

// Changed es lo que cambió desde since (ver Syncer).
func (j *jellyfin) Changed(ctx context.Context, since time.Time) (Changes, error) {
	var ch Changes
	var err error
	if ch.Total, ch.At, err = j.audioCount(ctx); err != nil {
		return ch, err
	}
	for start := 0; ; start += jfPage {
		q := songsQuery(start, false)
		q.Set("MinDateLastSaved", since.UTC().Format(time.RFC3339))
		items, _, err := j.items(ctx, q)
		if err != nil {
			return ch, err
		}
		for _, it := range items {
			ch.Songs = append(ch.Songs, it.song())
		}
		if len(items) < jfPage {
			break
		}
	}
	ch.Favorites = map[string]bool{}
	for start := 0; ; start += jfPage {
		q := url.Values{"IncludeItemTypes": {"Audio"}, "Recursive": {"true"}, "Filters": {"IsFavorite"},
			"StartIndex": {fmt.Sprint(start)}, "Limit": {fmt.Sprint(jfPage)}, "SortBy": {"SortName"},
			"EnableImages": {"false"}, "EnableTotalRecordCount": {"false"}}
		items, _, err := j.items(ctx, q)
		if err != nil {
			return ch, err
		}
		for _, it := range items {
			ch.Favorites[it.ID] = true
		}
		if len(items) < jfPage {
			break
		}
	}
	return ch, nil
}

// sortSongs deja las canciones por artista del álbum, álbum, disco y pista.
func sortSongs(s []Song) {
	slices.SortStableFunc(s, func(a, b Song) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.AlbumArtist), strings.ToLower(b.AlbumArtist)),
			cmp.Compare(strings.ToLower(a.Album), strings.ToLower(b.Album)),
			cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Track, b.Track))
	})
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

// apiPath es path sin la consulta ni el ID del usuario, para los errores:
// "/Users/<id>/Items?..." queda en "/Items".
func apiPath(path string) string {
	path, _, _ = strings.Cut(path, "?")
	if rest, ok := strings.CutPrefix(path, "/Users/"); ok {
		if _, after, ok := strings.Cut(rest, "/"); ok {
			return "/" + after
		}
	}
	return path
}
