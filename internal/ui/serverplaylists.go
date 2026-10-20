package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/remote"
)

// Las listas de los servidores (Subsonic/OpenSubsonic y Jellyfin), editables
// desde aquí con internal/remote: renombrar y borrar una lista; dentro de
// ella, subir, bajar o quitar una canción; y "Añadir a una lista" con
// canciones de un servidor ofrece sus listas (y una nueva en él). Las de la
// tarjeta y las de un servidor no se mezclan: una lista del servidor solo
// puede tener canciones suyas.
//
// Cada cambio va al servidor en segundo plano; luego se vuelven a pedir sus
// listas, se guardan en el catálogo (data/cache/<servidor>/catalog.json) y
// se rehace su biblioteca, conservando la vista en que se estaba.

// srvPlDone es lo que vuelve de editar una lista de un servidor.
type srvPlDone struct {
	src   *remoteSource
	lists []remote.Playlist // las listas del servidor tras el cambio
	fresh bool              // se pudieron leer (si no, la biblioteca se queda como estaba)
	err   error             // el error del cambio
	msg   string            // el aviso si fue bien
	sel   int               // dónde dejar el cursor en la lista abierta (-1: donde estaba)
}

// srvSource es el servidor de nombre name.
func (a *App) srvSource(name string) *remoteSource {
	for _, r := range a.remotes {
		if r.srv.Name == name {
			return r
		}
	}
	return nil
}

// srvCurrent es el servidor que se está viendo (nil en la biblioteca de la
// tarjeta).
func (a *App) srvCurrent() *remoteSource {
	if a.source > 0 && a.source <= len(a.remotes) {
		return a.remotes[a.source-1]
	}
	return nil
}

// srvEdit hace op contra el servidor de src en segundo plano y luego pone al
// día sus listas.
func (a *App) srvEdit(src *remoteSource, msg string, sel int, op func(ctx context.Context, st *remote.Store, server string) error) {
	if a.srvPlCh == nil {
		a.srvPlCh = make(chan srvPlDone, 4)
	}
	ch, st := a.srvPlCh, a.store
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r := srvPlDone{src: src, msg: msg, sel: sel}
		r.err = op(ctx, st, remote.Slug(src.srv.Name))
		// Se vuelven a pedir las listas también si falló: el servidor puede
		// haber hecho parte del cambio.
		if lists, err := remote.New(src.srv).Playlists(ctx); err == nil {
			r.lists, r.fresh = lists, true // vacío también vale: se borró la última
		}
		ch <- r
		ebiten.ScheduleFrame()
	}()
}

// pollSrvPlaylists atiende lo que vuelve de editar listas de un servidor.
func (a *App) pollSrvPlaylists() {
	for {
		select {
		case r := <-a.srvPlCh:
			a.srvApply(r)
		default:
			return
		}
	}
}

// srvApply pone las listas que llegaron en la biblioteca del servidor y en
// las vistas abiertas, y avisa.
func (a *App) srvApply(r srvPlDone) {
	src := r.src
	if r.fresh {
		src.cat.Playlists = r.lists
		if b, err := json.Marshal(src.cat); err == nil && len(src.cat.Songs) > 0 {
			f := a.catalogFile(src.srv)
			_ = os.MkdirAll(filepath.Dir(f), 0o755)
			_ = os.WriteFile(f, b, 0o644)
		}
		src.lib, src.songs = buildRemoteLibrary(src.srv.Name, src.cat)
		if a.srvCurrent() == src {
			// Las vistas abiertas de una lista pasan a la nueva (por su id);
			// si ya no existe, refreshPlaylists sale de ella.
			byID := map[string]*Playlist{}
			for _, pl := range src.lib.Playlists {
				byID[pl.srvID] = pl
			}
			for _, stack := range a.stacks {
				for _, v := range stack {
					if v.pl != nil && v.pl.server == src.srv.Name {
						if pl, ok := byID[v.pl.srvID]; ok {
							v.pl = pl
						}
					}
				}
			}
			a.lib = src.lib
			a.refreshPlaylists()
			if v := a.cur(); r.sel >= 0 && v.kind == vPlaylist {
				v.sel = max(0, min(r.sel, v.count()-1))
			}
		}
	}
	if r.err != nil {
		a.flash(T("pl_error", r.err))
		return
	}
	if r.msg != "" {
		a.flash(r.msg)
	}
}

// srvPlaylistItems son las opciones de una lista de un servidor en su menú:
// renombrarla, borrarla y crear otra en el servidor.
func (a *App) srvPlaylistItems(pl *Playlist) []menuItem {
	src := a.srvSource(pl.server)
	if pl.srvID == "" || src == nil {
		return nil
	}
	items := []menuItem{
		{label: T("pl_rename"), icon: "pencil", do: func() {
			a.openPrompt(T("pl_name"), pl.Name, func(name string) {
				a.srvEdit(src, T("pl_saved", name), -1, func(ctx context.Context, st *remote.Store, server string) error {
					return st.RenamePlaylist(ctx, server, pl.srvID, name)
				})
			})
		}},
	}
	if it, ok := a.srvNewPlaylistItem(); ok {
		items = append(items, it)
	}
	items = append(items, menuItem{label: T("pl_delete"), icon: "trash", danger: true, do: func() {
		a.menu = &menu{title: T("pl_delete_q", pl.Name), subtitle: src.srv.Name, items: []menuItem{
			{label: T("pl_keep"), icon: "arrow_l", do: func() {}},
			{label: T("pl_delete_yes"), icon: "trash", danger: true, do: func() {
				a.srvEdit(src, T("pl_deleted", pl.Name), -1, func(ctx context.Context, st *remote.Store, server string) error {
					return st.DeletePlaylist(ctx, server, pl.srvID)
				})
			}},
		}}
	}})
	return items
}

// srvNewPlaylistItem es "Nueva lista en <servidor>" (vacía), si se está
// viendo un servidor.
func (a *App) srvNewPlaylistItem() (menuItem, bool) {
	src := a.srvCurrent()
	if src == nil {
		return menuItem{}, false
	}
	return menuItem{label: T("spl_new", src.srv.Name), icon: "plus", do: func() {
		a.openPrompt(T("pl_name"), "", func(name string) {
			a.srvEdit(src, T("pl_saved", name), -1, func(ctx context.Context, st *remote.Store, server string) error {
				_, err := st.CreatePlaylist(ctx, server, name, nil)
				return err
			})
		})
	}}, true
}

// srvPlaylistSongItems son las opciones de la canción i dentro de una lista
// de un servidor: subirla, bajarla y quitarla (por su posición en la lista
// del servidor, que puede tener canciones que aquí no se ven).
func (a *App) srvPlaylistSongItems(pl *Playlist, i int) []menuItem {
	src := a.srvSource(pl.server)
	if src == nil || i < 0 || i >= len(pl.srvIdx) {
		return nil
	}
	move := func(d int) func() {
		return func() {
			j := i + d
			if j < 0 || j >= len(pl.srvIdx) {
				return
			}
			from, to := pl.srvIdx[i], pl.srvIdx[j]
			a.srvEdit(src, "", j, func(ctx context.Context, st *remote.Store, server string) error {
				return st.MovePlaylistItem(ctx, server, pl.srvID, from, to)
			})
		}
	}
	at := pl.srvIdx[i]
	return []menuItem{
		{label: T("pl_up"), icon: "arrow_u", do: move(-1)},
		{label: T("pl_down"), icon: "arrow_d", do: move(1)},
		{label: T("pl_remove"), icon: "trash", danger: true, do: func() {
			a.srvEdit(src, "", i, func(ctx context.Context, st *remote.Store, server string) error {
				return st.RemoveFromPlaylist(ctx, server, pl.srvID, []int{at})
			})
		}},
	}
}

// srvAddToPlaylist es "Añadir a una lista" con canciones de un servidor:
// todas de uno solo y que esté configurado. Ofrece sus listas y una nueva en
// él; dice false si no son de un servidor (las de la tarjeta van a las
// listas de aquí).
func (a *App) srvAddToPlaylist(songs []Song) bool {
	if len(songs) == 0 {
		return false
	}
	slug := ""
	paths := make([]string, 0, len(songs))
	for _, s := range songs {
		srv, cover, _, ok := remote.Parse(s.Path)
		if !ok || cover || (slug != "" && srv != slug) {
			return false
		}
		slug = srv
		paths = append(paths, s.Path)
	}
	var src *remoteSource
	for _, r := range a.remotes {
		if remote.Slug(r.srv.Name) == slug {
			src = r
		}
	}
	if src == nil {
		return false
	}
	n := N("n_songs", len(songs))
	m := &menu{title: T("t_addlist"), subtitle: src.srv.Name + " · " + n}
	m.items = append(m.items, menuItem{label: T("spl_new", src.srv.Name), icon: "plus", do: func() {
		a.openPrompt(T("pl_name"), "", func(name string) {
			a.srvEdit(src, T("pl_saved", name), -1, func(ctx context.Context, st *remote.Store, server string) error {
				_, err := st.CreatePlaylist(ctx, server, name, paths)
				return err
			})
		})
	}})
	for _, pl := range src.lib.Playlists {
		if pl.srvID == "" {
			continue
		}
		m.items = append(m.items, menuItem{label: pl.Name, icon: "list", do: func() {
			a.srvEdit(src, T("pl_added", n, pl.Name), -1, func(ctx context.Context, st *remote.Store, server string) error {
				return st.AddToPlaylist(ctx, server, pl.srvID, paths)
			})
		}})
	}
	a.menu = m
	return true
}
