package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Lo que se puede quitar de la vista (#12): pantallas de la barra de Select,
// secciones de la biblioteca y, en cada sección, formatos de archivo. Todo se
// guarda en audiofilus.json (config.UI) y se elige en Ajustes > Biblioteca.

// tabIDs son los nombres de las secciones de la biblioteca en
// audiofilus.json, en el orden de tabs (Buscar no se puede quitar).
var tabIDs = [tabCount]string{tabSearch: "", tabSongs: "songs", tabAlbums: "albums", tabArtists: "artists", tabLists: "lists", tabFolders: "folders"}

// formatTabs son las secciones donde se pueden quitar formatos (las listas
// son de quien las hizo: salen enteras).
var formatTabs = []int{tabSongs, tabAlbums, tabArtists, tabFolders}

// screenHidden dice si la pantalla id se quitó de la barra.
func (a *App) screenHidden(id string) bool {
	return id != "" && slices.Contains(a.cfg.UI.HiddenScreens, id)
}

// tabHidden dice si la sección tab se quitó.
func (a *App) tabHidden(tab int) bool {
	return tab > 0 && tab < tabCount && slices.Contains(a.cfg.UI.HiddenTabs, tabIDs[tab])
}

// visibleTabs son las secciones que se ven, en orden.
func (a *App) visibleTabs() []int {
	var out []int
	for t := range tabCount {
		if !a.tabHidden(t) {
			out = append(out, t)
		}
	}
	return out
}

// stepTab es la sección visible d pasos (1 o -1) después de la actual, dando
// la vuelta.
func (a *App) stepTab(d int) int {
	vis := a.visibleTabs()
	i := max(slices.Index(vis, a.libTab), 0)
	return vis[(i+d+len(vis))%len(vis)]
}

// homeTab es la sección por la que se entra a la biblioteca: Canciones, o la
// primera que se vea si se quitó.
func (a *App) homeTab() int {
	if !a.tabHidden(tabSongs) {
		return tabSongs
	}
	vis := a.visibleTabs()
	if len(vis) > 1 {
		return vis[1]
	}
	return tabSearch
}

// toggleIn quita v de list si está, y si no lo añade.
func toggleIn(list []string, v string) []string {
	if i := slices.Index(list, v); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	return append(list, v)
}

// openScreensMenu elige qué pantallas salen en la barra de Select.
func (a *App) openScreensMenu() {
	m := &menu{title: T("set_screens"), subtitle: T("screens_sub")}
	for _, e := range dockEntries {
		if e.id == "" {
			continue
		}
		m.items = append(m.items, menuItem{label: dockLabel(e), icon: e.icon,
			value:  func() string { return onOffLabel(!a.screenHidden(e.id)) },
			active: func() bool { return !a.screenHidden(e.id) },
			do: func() {
				a.cfg.UI.HiddenScreens = toggleIn(a.cfg.UI.HiddenScreens, e.id)
				a.saveConfig()
			}})
	}
	a.menu = m
}

// openTabsMenu elige qué secciones salen en la biblioteca.
func (a *App) openTabsMenu() {
	m := &menu{title: T("set_tabs"), subtitle: T("tabs_sub")}
	names := tabs()
	for t := tabSongs; t < tabCount; t++ {
		m.items = append(m.items, menuItem{label: names[t], icon: "folder",
			value:  func() string { return onOffLabel(!a.tabHidden(t)) },
			active: func() bool { return !a.tabHidden(t) },
			do: func() {
				a.cfg.UI.HiddenTabs = toggleIn(a.cfg.UI.HiddenTabs, tabIDs[t])
				a.saveConfig()
				if a.tabHidden(a.libTab) {
					a.libTab = a.homeTab()
				}
			}})
	}
	a.menu = m
}

// formatsValue es lo que se ve de los formatos de una sección: todos, o
// cuántos se quitaron.
func (a *App) formatsValue(tab int) string {
	n := len(a.cfg.UI.HiddenFormats[tabIDs[tab]])
	if n == 0 {
		return T("fmt_all")
	}
	return fmt.Sprintf(T("fmt_some"), n)
}

// formatsSummary es el valor de la baldosa: todos, o cuántos se quitaron en
// total.
func (a *App) formatsSummary() string {
	n := 0
	for _, t := range formatTabs {
		n += len(a.cfg.UI.HiddenFormats[tabIDs[t]])
	}
	if n == 0 {
		return T("fmt_all")
	}
	return fmt.Sprintf(T("fmt_some"), n)
}

// openFormatsMenu elige, sección por sección, qué formatos salen en ella.
func (a *App) openFormatsMenu() {
	m := &menu{title: T("set_formats"), subtitle: T("formats_sub")}
	names := tabs()
	for _, t := range formatTabs {
		m.items = append(m.items, menuItem{label: names[t], icon: "folder", close: true,
			value: func() string { return a.formatsValue(t) },
			do:    func() { a.openTabFormats(t, m) }})
	}
	a.menu = m
}

// openTabFormats es la lista de formatos de la biblioteca para la sección
// tab: los marcados salen.
func (a *App) openTabFormats(tab int, parent *menu) {
	id := tabIDs[tab]
	m := &menu{title: tabs()[tab], subtitle: T("formats_sub"), parent: parent}
	for _, ext := range libraryFormats(a.local) {
		m.items = append(m.items, menuItem{label: strings.ToUpper(ext), icon: "note",
			value:  func() string { return onOffLabel(!slices.Contains(a.cfg.UI.HiddenFormats[id], ext)) },
			active: func() bool { return !slices.Contains(a.cfg.UI.HiddenFormats[id], ext) },
			do: func() {
				if a.cfg.UI.HiddenFormats == nil {
					a.cfg.UI.HiddenFormats = map[string][]string{}
				}
				list := toggleIn(a.cfg.UI.HiddenFormats[id], ext)
				if len(list) == 0 {
					delete(a.cfg.UI.HiddenFormats, id)
				} else {
					a.cfg.UI.HiddenFormats[id] = list
				}
				a.saveConfig()
				a.stacks[tab] = nil // se vuelve a armar con el filtro nuevo
			}})
	}
	a.menu = m
}

// libraryFormats son las extensiones de las canciones de lib, ordenadas.
func libraryFormats(lib *Library) []string {
	if lib == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, s := range lib.Songs {
		if e := songFormat(s.Path); e != "" {
			seen[e] = true
		}
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// songFormat es la extensión del archivo de una canción, en minúsculas y
// sin el punto; vacía en las de un servidor (no se filtran).
func songFormat(path string) string {
	if strings.Contains(path, "://") {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(audioFile(path))), ".")
}

// hiddenFormats son los formatos quitados de la sección tab (nil si
// ninguno, o si se ve la de un servidor).
func (a *App) hiddenFormats(tab int) map[string]bool {
	list := a.cfg.UI.HiddenFormats[tabIDs[tab]]
	if len(list) == 0 || a.source != 0 {
		return nil
	}
	m := make(map[string]bool, len(list))
	for _, e := range list {
		m[e] = true
	}
	return m
}

// keepSongs son las canciones de songs que se ven en la sección tab.
func (a *App) keepSongs(tab int, songs []Song) []Song {
	hide := a.hiddenFormats(tab)
	if hide == nil {
		return songs
	}
	out := make([]Song, 0, len(songs))
	for _, s := range songs {
		if !hide[songFormat(s.Path)] {
			out = append(out, s)
		}
	}
	return out
}

// keepAlbums son los álbumes con alguna canción que se vea en la sección
// tab.
func (a *App) keepAlbums(tab int, albums []*Album) []*Album {
	if a.hiddenFormats(tab) == nil {
		return albums
	}
	var out []*Album
	for _, al := range albums {
		if len(a.keepSongs(tab, al.Songs)) > 0 {
			out = append(out, al)
		}
	}
	return out
}

// keepArtists son los artistas con algún álbum que se vea en la sección
// tab.
func (a *App) keepArtists(tab int, artists []*Artist) []*Artist {
	if a.hiddenFormats(tab) == nil {
		return artists
	}
	var out []*Artist
	for _, ar := range artists {
		if len(a.keepAlbums(tab, ar.Albums)) > 0 {
			out = append(out, ar)
		}
	}
	return out
}

// regroupAlbums vuelve a armar la biblioteca de la tarjeta tras cambiar
// Álbumes por carpeta: desde la caché, sin volver a leer los archivos.
func (a *App) regroupAlbums() {
	albumsByFolder.Store(a.cfg.UI.AlbumsByFolder)
	if lib := loadCachedLibrary(a.paths, a.cfg.MusicDirs); lib != nil {
		if a.local != nil {
			lib.Playlists = a.local.Playlists // las de la música, que la caché no trae
		}
		a.setLocal(lib)
	}
}
