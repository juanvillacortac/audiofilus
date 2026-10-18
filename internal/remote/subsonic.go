// Package remote son las bibliotecas remotas: por ahora Subsonic y
// OpenSubsonic (Navidrome, gonic, Airsonic-Advanced, Ampache, LMS, Nextcloud
// Music). Una canción remota tiene una ruta "subsonic://<servidor>/<id>" que
// viaja como cualquier otra (cola, listas, estado); para sonar, Store la baja
// a una caché en la tarjeta (data/cache/<servidor>) y la interfaz y el
// servicio la abren desde ahí.
package remote

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
)

// apiVersion es la versión de la API de Subsonic que se pide (la de tokens
// con sal, 1.13+, y search3 vacío para sacar todo el catálogo).
const apiVersion = "1.16.1"

// Backend es un tipo de servidor: Subsonic (Client) o Jellyfin.
type Backend interface {
	// Ping comprueba la dirección, el usuario y la contraseña (en Jellyfin,
	// además, inicia la sesión: las URL de abajo la necesitan).
	Ping(ctx context.Context) error
	// AllSongs es todo el catálogo; progress recibe cuántas canciones van.
	AllSongs(ctx context.Context, progress func(n int)) ([]Song, error)
	// Playlists son las listas del servidor, con sus canciones.
	Playlists(ctx context.Context) ([]Playlist, error)
	// StreamURL es para escuchar (con la calidad del servidor), DownloadURL
	// el archivo original y CoverURL una portada a size píxeles.
	StreamURL(id string) string
	DownloadURL(id string) string
	CoverURL(coverArt string, size int) string
	fetch(ctx context.Context, u string) (*http.Response, error)
}

// New prepara el cliente de un servidor, según su tipo.
func New(srv config.Server) Backend {
	if strings.EqualFold(srv.Type, "jellyfin") {
		return newJellyfin(srv)
	}
	return &Client{srv: srv, http: &http.Client{Timeout: 60 * time.Second}}
}

// Client habla con un servidor Subsonic.
type Client struct {
	srv  config.Server
	http *http.Client
}

// Song es una canción del catálogo remoto.
type Song struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"displayAlbumArtist"`
	Album       string `json:"album"`
	AlbumID     string `json:"albumId"`
	Track       int    `json:"track"`
	Disc        int    `json:"discNumber"`
	Duration    int    `json:"duration"`
	Suffix      string `json:"suffix"`
	CoverArt    string `json:"coverArt"`
}

// Playlist es una lista del servidor, con los ids de sus canciones.
type Playlist struct {
	ID    string
	Name  string
	Songs []string
}

// params son la autenticación (token con sal, sin mandar la contraseña) y lo
// fijo de cada petición.
func (c *Client) params(extra url.Values) url.Values {
	salt := make([]byte, 6)
	_, _ = rand.Read(salt)
	s := hex.EncodeToString(salt)
	sum := md5.Sum([]byte(c.srv.Password + s))
	v := url.Values{"u": {c.srv.User}, "t": {hex.EncodeToString(sum[:])}, "s": {s}, "v": {apiVersion}, "c": {"audiofilus"}, "f": {"json"}}
	for k, vs := range extra {
		v[k] = vs
	}
	return v
}

// endpoint es la URL de un método de la API.
func (c *Client) endpoint(method string, extra url.Values) string {
	return strings.TrimRight(c.srv.URL, "/") + "/rest/" + method + "?" + c.params(extra).Encode()
}

// apiError es el error que devuelve el servidor (contraseña mala...).
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// get llama a method y deja en out la respuesta (lo que va dentro de
// "subsonic-response").
func (c *Client) get(ctx context.Context, method string, extra url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint(method, extra), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", method, resp.Status)
	}
	var env struct {
		R json.RawMessage `json:"subsonic-response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var head struct {
		Status string    `json:"status"`
		Error  *apiError `json:"error"`
	}
	if err := json.Unmarshal(env.R, &head); err != nil {
		return err
	}
	if head.Status != "ok" {
		if head.Error != nil {
			return fmt.Errorf("%s: %s (%d)", method, head.Error.Message, head.Error.Code)
		}
		return fmt.Errorf("%s: status %q", method, head.Status)
	}
	if out != nil {
		return json.Unmarshal(env.R, out)
	}
	return nil
}

// Ping comprueba la dirección, el usuario y la contraseña.
func (c *Client) Ping(ctx context.Context) error { return c.get(ctx, "ping", nil, nil) }

// AllSongs saca todo el catálogo con search3 vacío, de 500 en 500 (OpenSubsonic
// lo exige para sincronizar). progress recibe cuántas van.
func (c *Client) AllSongs(ctx context.Context, progress func(n int)) ([]Song, error) {
	const page = 500
	var out []Song
	for off := 0; ; off += page {
		var r struct {
			Res struct {
				Song []Song `json:"song"`
			} `json:"searchResult3"`
		}
		q := url.Values{"query": {""}, "songCount": {fmt.Sprint(page)}, "songOffset": {fmt.Sprint(off)}, "artistCount": {"0"}, "albumCount": {"0"}}
		if err := c.get(ctx, "search3", q, &r); err != nil {
			return out, err
		}
		out = append(out, r.Res.Song...)
		if progress != nil {
			progress(len(out))
		}
		if len(r.Res.Song) < page {
			return out, nil
		}
	}
}

// Playlists son las listas del servidor, con sus canciones.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	var r struct {
		P struct {
			Playlist []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"playlist"`
		} `json:"playlists"`
	}
	if err := c.get(ctx, "getPlaylists", nil, &r); err != nil {
		return nil, err
	}
	var out []Playlist
	for _, p := range r.P.Playlist {
		var d struct {
			P struct {
				Entry []struct {
					ID string `json:"id"`
				} `json:"entry"`
			} `json:"playlist"`
		}
		if err := c.get(ctx, "getPlaylist", url.Values{"id": {p.ID}}, &d); err != nil {
			continue
		}
		pl := Playlist{ID: p.ID, Name: p.Name}
		for _, e := range d.P.Entry {
			pl.Songs = append(pl.Songs, e.ID)
		}
		out = append(out, pl)
	}
	return out, nil
}

// StreamURL es la URL para escuchar id con la calidad del servidor.
func (c *Client) StreamURL(id string) string {
	v := url.Values{"id": {id}}
	switch q := c.srv.Quality; q {
	case "original":
		v.Set("format", "raw")
	case "":
		v.Set("format", "mp3")
		v.Set("maxBitRate", "192")
	default:
		v.Set("format", "mp3")
		v.Set("maxBitRate", q)
	}
	return c.endpoint("stream", v)
}

// DownloadURL es la URL del archivo original (con sus etiquetas y portada).
func (c *Client) DownloadURL(id string) string {
	return c.endpoint("download", url.Values{"id": {id}})
}

// CoverURL es la URL de la portada coverArt a size píxeles.
func (c *Client) CoverURL(coverArt string, size int) string {
	return c.endpoint("getCoverArt", url.Values{"id": {coverArt}, "size": {fmt.Sprint(size)}})
}

// fetch hace una petición GET y devuelve la respuesta si es 200.
func (c *Client) fetch(ctx context.Context, u string) (*http.Response, error) {
	return getFile(ctx, u)
}

// getFile hace una petición GET de un archivo (una canción, una portada) y
// devuelve la respuesta si es 200 y no es un error de la API.
func getFile(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{} // sin plazo total: una canción grande tarda
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("subsonic: %s", resp.Status)
	}
	// Un error de la API llega como JSON con 200.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/xml") {
		resp.Body.Close()
		return nil, errors.New("subsonic: the server returned an error instead of the file")
	}
	return resp, nil
}
