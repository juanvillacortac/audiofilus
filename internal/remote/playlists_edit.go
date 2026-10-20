package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Editar las listas del servidor: crear, añadir, quitar, mover, renombrar y
// borrar. Las canciones van por su id en el servidor (en Store, por su ruta
// "subsonic://servidor/id"); las posiciones cuentan desde 0, en el orden de
// Playlists. Después de editar, la interfaz vuelve a leer las listas
// (Playlists) para tener el catálogo al día.

// PlaylistEditor es un servidor cuyas listas se pueden editar (los dos tipos
// lo son).
type PlaylistEditor interface {
	// CreatePlaylist crea una lista con esas canciones y devuelve su id.
	CreatePlaylist(ctx context.Context, name string, songIDs []string) (string, error)
	// AddToPlaylist añade canciones al final.
	AddToPlaylist(ctx context.Context, id string, songIDs []string) error
	// RemoveFromPlaylist quita las canciones de esas posiciones.
	RemoveFromPlaylist(ctx context.Context, id string, indexes []int) error
	// MovePlaylistItem lleva la canción de la posición from a la to.
	MovePlaylistItem(ctx context.Context, id string, from, to int) error
	// RenamePlaylist le cambia el nombre.
	RenamePlaylist(ctx context.Context, id, name string) error
	// DeletePlaylist la borra.
	DeletePlaylist(ctx context.Context, id string) error
}

var (
	_ PlaylistEditor = (*Client)(nil)
	_ PlaylistEditor = (*jellyfin)(nil)
)

// ErrIndex es lo que devuelven quitar y mover con una posición que no está.
var ErrIndex = errors.New("remote: playlist position out of range")

// --- Subsonic ---------------------------------------------------------------

// CreatePlaylist crea la lista (createPlaylist con name y los songId).
func (c *Client) CreatePlaylist(ctx context.Context, name string, songIDs []string) (string, error) {
	v := url.Values{"name": {name}, "songId": songIDs}
	var r struct {
		P struct {
			ID string `json:"id"`
		} `json:"playlist"`
	}
	if err := c.get(ctx, "createPlaylist", v, &r); err != nil {
		return "", err
	}
	if r.P.ID == "" {
		// Los servidores con API anterior a la 1.14 no devuelven la lista:
		// se busca por nombre (la más nueva con ese nombre).
		lists, err := c.Playlists(ctx)
		if err != nil {
			return "", err
		}
		for i := len(lists) - 1; i >= 0; i-- {
			if lists[i].Name == name {
				return lists[i].ID, nil
			}
		}
		return "", fmt.Errorf("createPlaylist: %q not found after creating it", name)
	}
	return r.P.ID, nil
}

// AddToPlaylist añade al final (updatePlaylist con songIdToAdd).
func (c *Client) AddToPlaylist(ctx context.Context, id string, songIDs []string) error {
	if len(songIDs) == 0 {
		return nil
	}
	return c.get(ctx, "updatePlaylist", url.Values{"playlistId": {id}, "songIdToAdd": songIDs}, nil)
}

// RemoveFromPlaylist quita por posición (updatePlaylist con
// songIndexToRemove).
func (c *Client) RemoveFromPlaylist(ctx context.Context, id string, indexes []int) error {
	if len(indexes) == 0 {
		return nil
	}
	v := url.Values{"playlistId": {id}}
	for _, i := range indexes {
		if i < 0 {
			return ErrIndex
		}
		v.Add("songIndexToRemove", strconv.Itoa(i))
	}
	return c.get(ctx, "updatePlaylist", v, nil)
}

// playlistSongs son los ids de una lista, en orden.
func (c *Client) playlistSongs(ctx context.Context, id string) ([]string, error) {
	var d struct {
		P struct {
			Entry []struct {
				ID string `json:"id"`
			} `json:"entry"`
		} `json:"playlist"`
	}
	if err := c.get(ctx, "getPlaylist", url.Values{"id": {id}}, &d); err != nil {
		return nil, err
	}
	out := make([]string, len(d.P.Entry))
	for i, e := range d.P.Entry {
		out[i] = e.ID
	}
	return out, nil
}

// MovePlaylistItem mueve una canción. Subsonic no sabe mover: se reescribe
// la lista entera en el orden nuevo (createPlaylist con playlistId, que
// reemplaza sus canciones).
func (c *Client) MovePlaylistItem(ctx context.Context, id string, from, to int) error {
	songs, err := c.playlistSongs(ctx, id)
	if err != nil {
		return err
	}
	order, err := moved(songs, from, to)
	if err != nil || from == to {
		return err
	}
	return c.get(ctx, "createPlaylist", url.Values{"playlistId": {id}, "songId": order}, nil)
}

// RenamePlaylist cambia el nombre (updatePlaylist con name).
func (c *Client) RenamePlaylist(ctx context.Context, id, name string) error {
	return c.get(ctx, "updatePlaylist", url.Values{"playlistId": {id}, "name": {name}}, nil)
}

// DeletePlaylist borra la lista.
func (c *Client) DeletePlaylist(ctx context.Context, id string) error {
	return c.get(ctx, "deletePlaylist", url.Values{"id": {id}}, nil)
}

// moved es s con el elemento de from llevado a to.
func moved(s []string, from, to int) ([]string, error) {
	if from < 0 || from >= len(s) || to < 0 || to >= len(s) {
		return nil, ErrIndex
	}
	out := append([]string(nil), s[:from]...)
	out = append(out, s[from+1:]...)
	out = append(out[:to], append([]string{s[from]}, out[to:]...)...)
	return out, nil
}

// --- Jellyfin ---------------------------------------------------------------

// CreatePlaylist crea la lista (POST /Playlists) y devuelve su id.
func (j *jellyfin) CreatePlaylist(ctx context.Context, name string, songIDs []string) (string, error) {
	if err := j.login(ctx); err != nil {
		return "", err
	}
	body := map[string]any{"Name": name, "Ids": songIDs, "UserId": j.user(), "MediaType": "Audio"}
	var r struct {
		ID string `json:"Id"`
	}
	if err := j.call(ctx, http.MethodPost, "/Playlists", body, &r); err != nil {
		return "", err
	}
	return r.ID, nil
}

// AddToPlaylist añade al final (POST /Playlists/{id}/Items?Ids=).
func (j *jellyfin) AddToPlaylist(ctx context.Context, id string, songIDs []string) error {
	if len(songIDs) == 0 {
		return nil
	}
	if err := j.login(ctx); err != nil {
		return err
	}
	q := url.Values{"Ids": {strings.Join(songIDs, ",")}, "UserId": {j.user()}}
	return j.call(ctx, http.MethodPost, "/Playlists/"+url.PathEscape(id)+"/Items?"+q.Encode(), nil, nil)
}

// entries son las entradas de una lista, en orden: cada una tiene su propio
// id (PlaylistItemId), que es el que se usa para quitar y mover (la misma
// canción puede estar dos veces).
func (j *jellyfin) entries(ctx context.Context, id string) ([]string, error) {
	if err := j.login(ctx); err != nil {
		return nil, err
	}
	var r struct {
		Items []struct {
			ID             string `json:"Id"`
			PlaylistItemID string `json:"PlaylistItemId"`
		}
	}
	q := url.Values{"UserId": {j.user()}}
	if err := j.call(ctx, http.MethodGet, "/Playlists/"+url.PathEscape(id)+"/Items?"+q.Encode(), nil, &r); err != nil {
		return nil, err
	}
	out := make([]string, len(r.Items))
	for i, it := range r.Items {
		out[i] = it.PlaylistItemID
	}
	return out, nil
}

// RemoveFromPlaylist quita por posición (DELETE /Playlists/{id}/Items con
// los EntryIds de esas posiciones).
func (j *jellyfin) RemoveFromPlaylist(ctx context.Context, id string, indexes []int) error {
	if len(indexes) == 0 {
		return nil
	}
	es, err := j.entries(ctx, id)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(indexes))
	for _, i := range indexes {
		if i < 0 || i >= len(es) {
			return ErrIndex
		}
		ids = append(ids, es[i])
	}
	q := url.Values{"EntryIds": {strings.Join(ids, ",")}}
	return j.call(ctx, http.MethodDelete, "/Playlists/"+url.PathEscape(id)+"/Items?"+q.Encode(), nil, nil)
}

// MovePlaylistItem mueve una entrada
// (POST /Playlists/{id}/Items/{entrada}/Move/{posición}).
func (j *jellyfin) MovePlaylistItem(ctx context.Context, id string, from, to int) error {
	es, err := j.entries(ctx, id)
	if err != nil {
		return err
	}
	if from < 0 || from >= len(es) || to < 0 || to >= len(es) {
		return ErrIndex
	}
	if from == to {
		return nil
	}
	return j.call(ctx, http.MethodPost, fmt.Sprintf("/Playlists/%s/Items/%s/Move/%d", url.PathEscape(id), url.PathEscape(es[from]), to), nil, nil)
}

// RenamePlaylist cambia el nombre (POST /Playlists/{id}, Jellyfin 10.9 o
// más nuevo).
func (j *jellyfin) RenamePlaylist(ctx context.Context, id, name string) error {
	if err := j.login(ctx); err != nil {
		return err
	}
	return j.call(ctx, http.MethodPost, "/Playlists/"+url.PathEscape(id), map[string]any{"Name": name}, nil)
}

// DeletePlaylist borra la lista (DELETE /Items/{id}).
func (j *jellyfin) DeletePlaylist(ctx context.Context, id string) error {
	if err := j.login(ctx); err != nil {
		return err
	}
	return j.call(ctx, http.MethodDelete, "/Items/"+url.PathEscape(id), nil, nil)
}

// --- Store: por servidor y rutas --------------------------------------------
//
// En Store, server es el slug del servidor (el de sus rutas, Slug(nombre)).

// editor es el cliente de un servidor, como editor de listas.
func (s *Store) editor(server string) (PlaylistEditor, error) {
	c, _, err := s.client(server)
	if err != nil {
		return nil, err
	}
	e, ok := c.(PlaylistEditor)
	if !ok {
		return nil, fmt.Errorf("remote: %s can't edit playlists", server)
	}
	return e, nil
}

// songIDs pasa rutas de canciones de un servidor ("subsonic://servidor/id")
// a sus ids; las de otro servidor o locales son un error (una lista del
// servidor solo puede tener canciones suyas).
func songIDs(server string, paths []string) ([]string, error) {
	ids := make([]string, 0, len(paths))
	for _, p := range paths {
		srv, cover, id, ok := Parse(p)
		if !ok || cover || srv != Slug(server) {
			return nil, fmt.Errorf("remote: %s is not a song of %s", p, server)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreatePlaylist crea en server una lista con esas canciones (rutas suyas).
func (s *Store) CreatePlaylist(ctx context.Context, server, name string, paths []string) (string, error) {
	e, err := s.editor(server)
	if err != nil {
		return "", err
	}
	ids, err := songIDs(server, paths)
	if err != nil {
		return "", err
	}
	return e.CreatePlaylist(ctx, name, ids)
}

// AddToPlaylist añade canciones (rutas de server) al final de la lista id.
func (s *Store) AddToPlaylist(ctx context.Context, server, id string, paths []string) error {
	e, err := s.editor(server)
	if err != nil {
		return err
	}
	ids, err := songIDs(server, paths)
	if err != nil {
		return err
	}
	return e.AddToPlaylist(ctx, id, ids)
}

// RemoveFromPlaylist quita de la lista id las canciones de esas posiciones.
func (s *Store) RemoveFromPlaylist(ctx context.Context, server, id string, indexes []int) error {
	e, err := s.editor(server)
	if err != nil {
		return err
	}
	return e.RemoveFromPlaylist(ctx, id, indexes)
}

// MovePlaylistItem mueve una canción de la lista id de from a to.
func (s *Store) MovePlaylistItem(ctx context.Context, server, id string, from, to int) error {
	e, err := s.editor(server)
	if err != nil {
		return err
	}
	return e.MovePlaylistItem(ctx, id, from, to)
}

// RenamePlaylist cambia el nombre de la lista id.
func (s *Store) RenamePlaylist(ctx context.Context, server, id, name string) error {
	e, err := s.editor(server)
	if err != nil {
		return err
	}
	return e.RenamePlaylist(ctx, id, name)
}

// DeletePlaylist borra la lista id.
func (s *Store) DeletePlaylist(ctx context.Context, server, id string) error {
	e, err := s.editor(server)
	if err != nil {
		return err
	}
	return e.DeletePlaylist(ctx, id)
}
