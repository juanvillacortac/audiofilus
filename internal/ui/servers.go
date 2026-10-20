package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/remote"
)

// Las bibliotecas remotas (servidores Subsonic/OpenSubsonic: Navidrome,
// gonic, Airsonic...; y Jellyfin/Emby). Cada servidor de audiofilus.json es una biblioteca
// más, con sus álbumes, artistas, listas y búsqueda, armada igual que la
// local (buildLibrary) con canciones de ruta "subsonic://<servidor>/<id>". Sale
// en la barra de Select junto a la biblioteca; elegirla cambia a.lib (a.local
// es siempre la de la tarjeta).
//
// El catálogo se guarda en data/cache/<servidor>/catalog.json: al arrancar
// se ve enseguida (también sin red) y luego se pone al día con el servidor
// (search3 vacío, de 500 en 500). Al cargarlo se escribe el índice de
// etiquetas (meta.json) que usa el servicio para decir qué suena. Las
// portadas y lo que se escucha se bajan a esa misma caché (remote.Store).

// remoteSource es un servidor y su biblioteca.
type remoteSource struct {
	srv     config.Server
	lib     *Library
	songs   map[string]remote.Song // por id, para descargarlas
	cat     catalog                // el catálogo con que se armó (serverplaylists.go lo pone al día)
	loading bool                   // pidiendo el catálogo al servidor
	err     error                  // lo que falló la última vez
}

// catalog es lo que se guarda del catálogo de un servidor.
type catalog struct {
	Songs     []remote.Song     `json:"songs"`
	Playlists []remote.Playlist `json:"playlists"`
}

// remoteLoaded es un catálogo que llegó (del disco o del servidor).
type remoteLoaded struct {
	src   *remoteSource
	cat   catalog
	fresh bool // del servidor (si no, de la caché)
	err   error
}

// initServers prepara la caché de lo remoto y empieza a cargar cada servidor.
func (a *App) initServers() {
	a.store = remote.NewStore(a.paths.Data, func() []config.Server { return a.cfg.Servers })
	coverStore = a.store
	a.serversCh = make(chan remoteLoaded, 8)
	a.pingCh = make(chan pingResult, 4)
	a.remotes = nil
	for _, srv := range a.cfg.Servers {
		a.addSource(srv)
	}
}

// addSource añade un servidor y empieza a cargarlo.
func (a *App) addSource(srv config.Server) *remoteSource {
	src := &remoteSource{srv: srv, lib: buildLibrary(nil), loading: true}
	a.remotes = append(a.remotes, src)
	go a.loadCatalog(src)
	return src
}

// catalogFile es la caché del catálogo de un servidor.
func (a *App) catalogFile(srv config.Server) string {
	return filepath.Join(a.paths.Data, "cache", remote.Slug(srv.Name), "catalog.json")
}

// loadCatalog lee el catálogo guardado y luego lo pide al servidor.
func (a *App) loadCatalog(src *remoteSource) {
	if b, err := os.ReadFile(a.catalogFile(src.srv)); err == nil {
		var cat catalog
		if json.Unmarshal(b, &cat) == nil && len(cat.Songs) > 0 {
			a.serversCh <- remoteLoaded{src: src, cat: cat}
			ebiten.ScheduleFrame()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := remote.New(src.srv)
	var cat catalog
	var err error
	if cat.Songs, err = c.AllSongs(ctx, nil); err == nil {
		cat.Playlists, _ = c.Playlists(ctx)
		if b, jerr := json.Marshal(cat); jerr == nil {
			f := a.catalogFile(src.srv)
			_ = os.MkdirAll(filepath.Dir(f), 0o755)
			_ = os.WriteFile(f, b, 0o644)
		}
		meta := make(map[string]remote.Meta, len(cat.Songs))
		for _, s := range cat.Songs {
			meta[s.ID] = remote.Meta{Title: s.Title, Artist: s.Artist, Album: s.Album}
		}
		_ = a.store.SaveMeta(src.srv.Name, meta)
	}
	a.serversCh <- remoteLoaded{src: src, cat: cat, fresh: true, err: err}
	ebiten.ScheduleFrame()
}

// updateServers recoge los catálogos que llegaron. Va en cada Update.
func (a *App) updateServers() {
	for {
		select {
		case r := <-a.serversCh:
			if r.fresh {
				r.src.loading, r.src.err = false, r.err
				if r.err != nil {
					a.flash(T("srv_error", r.src.srv.Name, r.err))
					continue
				}
			}
			r.src.lib, r.src.songs = buildRemoteLibrary(r.src.srv.Name, r.cat)
			r.src.cat = r.cat
			if r.fresh {
				a.mergeServerMarks(r.src.srv.Name, r.cat.Songs)
			}
			if a.source > 0 && a.remotes[a.source-1] == r.src {
				a.lib = r.src.lib
				a.resetBrowse()
				a.search.lastQuery = ""
			}
			a.lastTrack = "" // lo que suena puede ser de aquí: se vuelve a buscar
		default:
			return
		}
	}
}

// buildRemoteLibrary arma la biblioteca de un servidor con su catálogo.
func buildRemoteLibrary(server string, cat catalog) (*Library, map[string]remote.Song) {
	entries := make([]entry, 0, len(cat.Songs))
	songs := make(map[string]remote.Song, len(cat.Songs))
	for _, s := range cat.Songs {
		if s.Album == "[Unknown Album]" { // Navidrome: como la biblioteca local, "Sin álbum"
			s.Album = ""
		}
		e := entry{Path: remote.SongPath(server, s.ID), Title: s.Title, Artist: s.Artist,
			AlbumArtist: s.AlbumArtist, Album: s.Album, Track: s.Disc*1000 + s.Track}
		if s.CoverArt != "" {
			e.Cover = remote.CoverPath(server, s.CoverArt)
		}
		entries = append(entries, e)
		songs[s.ID] = s
	}
	lib := buildLibrary(entries)
	for _, p := range cat.Playlists {
		pl := &Playlist{Name: p.Name, Remote: true, server: server, srvID: p.ID}
		for k, id := range p.Songs {
			if s, ok := lib.song[remote.SongPath(server, id)]; ok {
				pl.Songs = append(pl.Songs, s)
				pl.srvIdx = append(pl.srvIdx, k)
			}
		}
		lib.Playlists = append(lib.Playlists, pl)
	}
	return lib, songs
}

// setSource cambia la biblioteca que se ve: 0 la de la tarjeta, i>0 el
// servidor i-1.
func (a *App) setSource(i int) {
	if i < 0 || i > len(a.remotes) || i == a.source && a.lib != nil && a.source >= 0 {
		return
	}
	a.source = i
	if i == 0 {
		a.lib = a.local
	} else {
		a.lib = a.remotes[i-1].lib
	}
	a.resetBrowse()
	a.search.lastQuery = ""
	a.updateSearchResults()
}

// setLocal pone la biblioteca de la tarjeta (al terminar de buscar música).
func (a *App) setLocal(lib *Library) {
	a.local = lib
	if a.source == 0 {
		a.lib = lib
		a.resetBrowse()
	}
	a.lastTrack = ""
}

// sourceName es el nombre de la biblioteca que se ve, para la barra de arriba.
func (a *App) sourceName() string {
	if a.source > 0 {
		return a.remotes[a.source-1].srv.Name
	}
	return T("library")
}

// albumOf es el álbum de path, en la biblioteca que sea (la de la tarjeta o
// la del servidor de donde viene).
func (a *App) albumOf(path string) (*Album, int) {
	if remote.IsRemote(path) {
		server, _, _, _ := remote.Parse(path)
		for _, r := range a.remotes {
			if remote.Slug(r.srv.Name) == server {
				return r.lib.AlbumOf(path)
			}
		}
		return nil, -1
	}
	if a.local == nil {
		return nil, -1
	}
	return a.local.AlbumOf(path)
}

// remoteSong es la canción del catálogo de path, si es remota.
func (a *App) remoteSong(path string) (*remoteSource, remote.Song, bool) {
	server, cover, id, ok := remote.Parse(path)
	if !ok || cover {
		return nil, remote.Song{}, false
	}
	for _, r := range a.remotes {
		if remote.Slug(r.srv.Name) == server {
			s, ok := r.songs[id]
			return r, s, ok
		}
	}
	return nil, remote.Song{}, false
}

// remoteState es lo que se enseña en una biblioteca remota vacía: cargando o
// el error.
func (a *App) remoteState() string {
	if a.source == 0 {
		return ""
	}
	r := a.remotes[a.source-1]
	switch {
	case r.loading:
		return T("srv_loading", r.srv.Name)
	case r.err != nil:
		return T("srv_error", r.srv.Name, r.err)
	}
	return ""
}

// pingResult es la prueba de un servidor que se está añadiendo o probando.
type pingResult struct {
	srv    config.Server
	adding bool
	err    error
}

// pingServer prueba srv en segundo plano; el resultado lo recoge updatePings.
func (a *App) pingServer(srv config.Server, adding bool) {
	a.flash(T("srv_testing", srv.Name))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := remote.New(srv).Ping(ctx)
		a.pingCh <- pingResult{srv, adding, err}
		ebiten.ScheduleFrame()
	}()
}

// updatePings recoge las pruebas de servidores. Va en cada Update.
func (a *App) updatePings() {
	for {
		select {
		case r := <-a.pingCh:
			switch {
			case r.err == nil && r.adding:
				a.cfg.Servers = append(a.cfg.Servers, r.srv)
				a.saveConfig()
				a.addSource(r.srv)
				a.flash(T("srv_added", r.srv.Name))
			case r.err == nil:
				a.flash(T("srv_ok", r.srv.Name))
			case r.adding:
				a.openServerFailed(r.srv, r.err)
			default:
				a.flash(T("srv_error", r.srv.Name, r.err))
			}
		default:
			return
		}
	}
}

// openServersMenu es el menú de servidores de Ajustes: cada uno (con sus
// opciones) y añadir uno.
func (a *App) openServersMenu() {
	m := &menu{title: T("srv_title"), subtitle: T("srv_sub")}
	for i := range a.cfg.Servers {
		srv := a.cfg.Servers[i]
		m.items = append(m.items, menuItem{label: srv.Name, icon: "server", do: func() { a.openServerMenu(i) }})
	}
	m.items = append(m.items, menuItem{label: T("srv_add"), icon: "plus", do: a.chooseServerType})
	a.menu = m
}

// chooseServerType pregunta el tipo de servidor antes de sus datos.
func (a *App) chooseServerType() {
	a.menu = &menu{title: T("srv_add"), subtitle: T("srv_type_q"), items: []menuItem{
		{label: "Subsonic / Navidrome", icon: "server", do: func() {
			a.addServerFlow(config.Server{Name: "Navidrome", URL: "http://:4533"})
		}},
		{label: "Jellyfin / Emby", icon: "server", do: func() {
			a.addServerFlow(config.Server{Name: "Jellyfin", Type: "jellyfin", URL: "http://:8096"})
		}},
	}}
}

// serverKind es cómo se llama el tipo de un servidor.
func serverKind(srv config.Server) string {
	if strings.EqualFold(srv.Type, "jellyfin") {
		return "Jellyfin"
	}
	return "Subsonic"
}

// addServerFlow pide nombre, dirección, usuario y contraseña (con lo que ya
// haya en srv, para corregir) y prueba la conexión antes de guardarlo.
func (a *App) addServerFlow(srv config.Server) {
	a.openPrompt(T("srv_name"), srv.Name, func(name string) {
		srv.Name = name
		a.openRawPrompt(T("srv_url"), srv.URL, false, func(u string) {
			if !strings.Contains(u, "://") {
				u = "http://" + u
			}
			srv.URL = u
			a.openRawPrompt(T("srv_user"), srv.User, false, func(user string) {
				srv.User = user
				a.openRawPrompt(T("srv_pass"), "", true, func(pass string) {
					srv.Password = pass
					a.pingServer(srv, true)
				})
			})
		})
	})
	// El nombre también tal cual (lleva mayúsculas y minúsculas).
	a.prompt.typed = true
}

// openServerFailed es lo que se ofrece si el servidor nuevo no contesta.
func (a *App) openServerFailed(srv config.Server, err error) {
	a.menu = &menu{title: T("srv_failed"), subtitle: err.Error(), items: []menuItem{
		{label: T("srv_retry"), icon: "repeat", do: func() { a.pingServer(srv, true) }},
		{label: T("srv_fix"), icon: "pencil", do: func() { a.addServerFlow(srv) }},
		{label: T("srv_save_anyway"), icon: "check", do: func() {
			a.cfg.Servers = append(a.cfg.Servers, srv)
			a.saveConfig()
			a.addSource(srv)
		}},
		{label: T("pl_keep"), icon: "arrow_l", do: func() {}},
	}}
}

// qualities son las calidades de escucha que se pueden elegir, en orden.
var qualities = []string{"", "320", "128", "96", "original"}

// qualityLabel es cómo se ve una calidad.
func qualityLabel(q string) string {
	switch q {
	case "":
		return "MP3 192 kbps"
	case "original":
		return T("srv_original")
	}
	return "MP3 " + q + " kbps"
}

// openServerMenu son las opciones de un servidor.
func (a *App) openServerMenu(i int) {
	srv := a.cfg.Servers[i]
	m := &menu{title: srv.Name, subtitle: serverKind(srv) + " · " + srv.URL}
	m.items = []menuItem{
		{label: T("srv_test"), icon: "check", do: func() { a.pingServer(a.cfg.Servers[i], false) }},
		{label: T("srv_quality"), icon: "speaker",
			value: func() string { return qualityLabel(a.cfg.Servers[i].Quality) },
			do: func() {
				q := a.cfg.Servers[i].Quality
				for k, v := range qualities {
					if v == q {
						a.cfg.Servers[i].Quality = qualities[(k+1)%len(qualities)]
						break
					}
				}
				a.saveConfig()
				if i < len(a.remotes) {
					a.remotes[i].srv = a.cfg.Servers[i]
				}
			}},
		{label: T("srv_refresh"), icon: "repeat", do: func() {
			if i < len(a.remotes) && !a.remotes[i].loading {
				a.remotes[i].loading = true
				go a.loadCatalog(a.remotes[i])
				a.flash(T("srv_loading", srv.Name))
			}
		}},
		{label: T("srv_edit"), icon: "pencil", do: func() { a.editServerFlow(i) }},
		{label: T("srv_remove"), icon: "trash", danger: true, do: func() {
			a.menu = &menu{title: T("srv_remove_q", srv.Name), items: []menuItem{
				{label: T("pl_keep"), icon: "arrow_l", do: func() {}},
				{label: T("srv_remove"), icon: "trash", danger: true, do: func() { a.removeServer(i) }},
			}}
		}},
	}
	a.menu = m
}

// editServerFlow vuelve a pedir los datos de un servidor (la contraseña, de
// nuevo) y lo guarda sin cambiar su caché si conserva el nombre.
func (a *App) editServerFlow(i int) {
	old := a.cfg.Servers[i]
	a.removeServer(i)
	a.addServerFlow(old)
}

// removeServer quita un servidor (su caché se queda en data/cache).
func (a *App) removeServer(i int) {
	if i >= len(a.cfg.Servers) {
		return
	}
	a.cfg.Servers = append(a.cfg.Servers[:i], a.cfg.Servers[i+1:]...)
	a.saveConfig()
	if i < len(a.remotes) {
		a.remotes = append(a.remotes[:i], a.remotes[i+1:]...)
	}
	if a.source > len(a.remotes) || a.source == i+1 {
		a.source = -1 // fuerza el cambio
		a.setSource(0)
	} else if a.source > i+1 {
		a.source--
	}
}
