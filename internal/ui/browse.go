package ui

import (
	"fmt"
	"image"
	"math"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// Las secciones de la biblioteca, en el orden de tabs.
const (
	tabSearch = iota // Buscar (search.go)
	tabSongs
	tabAlbums
	tabArtists
	tabLists
	tabFolders
	tabCount
)

// tabs son los nombres de las secciones de la biblioteca.
func tabs() []string {
	return []string{T("tab_search"), T("tab_songs"), T("tab_albums"), T("tab_artists"), T("tab_lists"), T("tab_folders")}
}

// viewKind es qué muestra una vista de la biblioteca.
type viewKind int

const (
	vSongs     viewKind = iota // una lista de canciones (todas)
	vAlbums                    // cuadrícula de álbumes
	vArtists                   // lista de artistas
	vAlbum                     // un álbum: portada y canciones
	vPlaylists                 // lista de listas .m3u
	vPlaylist                  // las canciones de una lista
	vFolder                    // una carpeta: subcarpetas y canciones
)

// view es una vista de la biblioteca. Cada sección tiene una pila de vistas:
// al abrir un artista, un álbum o una carpeta se apila otra, y B vuelve atrás.
type view struct {
	kind      viewKind
	title     string
	sel       int
	songs     []Song
	albums    []*Album
	album     *Album
	artists   []*Artist
	playlists []*Playlist
	pl        *Playlist // la lista que se ve (vPlaylist), para editarla
	dir       string
	dirs      []string
	scr       scroller // el desplazamiento con el dedo (pointer.go)
}

// count es cuántos elementos se pueden elegir en la vista.
func (v *view) count() int {
	switch v.kind {
	case vAlbums:
		return len(v.albums)
	case vArtists:
		return len(v.artists)
	case vPlaylists:
		return len(v.playlists)
	case vFolder:
		return len(v.dirs) + len(v.songs)
	}
	return len(v.songs)
}

// rootView es la primera vista de cada sección.
func (a *App) rootView(tab int) *view {
	l := a.lib
	switch tab {
	case tabAlbums:
		return &view{kind: vAlbums, title: T("tab_albums"), albums: l.Albums}
	case tabArtists:
		return &view{kind: vArtists, title: T("tab_artists"), artists: l.Artists}
	case tabLists:
		return &view{kind: vPlaylists, title: T("tab_lists"), playlists: l.Playlists}
	case tabFolders:
		if len(l.Roots) == 1 {
			return a.folderView(l.Roots[0])
		}
		return &view{kind: vFolder, title: T("tab_folders"), dirs: l.Roots}
	}
	return &view{kind: vSongs, title: T("tab_songs"), songs: l.Songs}
}

func (a *App) folderView(dir string) *view {
	dirs, songs := a.lib.Folder(dir)
	return &view{kind: vFolder, title: filepath.Base(dir), dir: dir, dirs: dirs, songs: songs}
}

func (a *App) albumView(al *Album) *view {
	return &view{kind: vAlbum, title: al.Title, album: al, songs: al.Songs}
}

// cur es la vista de arriba de la sección actual.
func (a *App) cur() *view {
	st := a.stacks[a.libTab]
	if len(st) == 0 {
		st = []*view{a.rootView(a.libTab)}
		a.stacks[a.libTab] = st
	}
	return st[len(st)-1]
}

// push abre una vista encima de la actual.
func (a *App) push(v *view) {
	a.cur() // asegura que la sección tiene su primera vista
	a.stacks[a.libTab] = append(a.stacks[a.libTab], v)
}

// pop vuelve a la vista anterior; false si ya está en la primera.
func (a *App) pop() bool {
	st := a.stacks[a.libTab]
	if len(st) <= 1 {
		return false
	}
	a.stacks[a.libTab] = st[:len(st)-1]
	return true
}

// resetBrowse vacía las pilas (al cambiar la biblioteca o el idioma).
func (a *App) resetBrowse() {
	for i := range a.stacks {
		a.stacks[i] = nil
	}
}

// openAlbum muestra un álbum en la sección de álbumes (desde un menú o desde
// la pantalla de reproducción).
func (a *App) openAlbum(al *Album) {
	a.screen, a.libTab, a.tabFocus = LibraryScreen, tabAlbums, false
	root := a.rootView(tabAlbums)
	for i, x := range root.albums {
		if x == al {
			root.sel = i
		}
	}
	a.stacks[tabAlbums] = []*view{root, a.albumView(al)}
}

// openArtist muestra los álbumes de un artista.
func (a *App) openArtist(name string) {
	a.screen, a.libTab, a.tabFocus = LibraryScreen, tabArtists, false
	root := a.rootView(tabArtists)
	for i, ar := range root.artists {
		if strings.EqualFold(ar.Name, name) {
			root.sel = i
			a.stacks[tabArtists] = []*view{root, {kind: vAlbums, title: ar.Name, albums: ar.Albums}}
			return
		}
	}
	a.stacks[tabArtists] = []*view{root}
}

// gridLayout reparte el ancho w de la cuadrícula: cuántas columnas de
// portadas caben (de gridMinTile como poco) y de qué tamaño, para que llenen el
// ancho en vez de dejar huecos a los lados.
func gridLayout(w int) (cols, tile int) {
	cols = max(1, (w+gridGap)/(gridMinTile+gridGap))
	tile = min(gridMaxTile, (w-(cols-1)*gridGap)/cols)
	return cols, tile
}

// gridCols es cuántas columnas de portadas caben en la cuadrícula.
func gridCols(w int) int { c, _ := gridLayout(w); return c }

const (
	gridMinTile = 48
	gridMaxTile = 64
	gridGap     = 8
)

// contentX es dónde empieza el contenido de la biblioteca (a la derecha de
// las secciones en pantallas anchas).
func (a *App) contentX() int {
	if a.w >= 320 {
		return margin + 80
	}
	return margin
}

// updateLibrary atiende los botones en la biblioteca.
func (a *App) updateLibrary() {
	in := &a.input
	if a.tabFocus {
		a.updateTabs()
		return
	}
	if a.libTab == tabSearch {
		a.updateSearch()
		return
	}
	v := a.cur()
	n := v.count()
	nested := len(a.stacks[a.libTab]) > 1
	switch {
	case in.Pressed(ActBack):
		if !a.pop() {
			a.tabFocus = true
		}
		return
	case in.Pressed(ActMenu) && n > 0:
		a.openItemMenu(v)
		return
	case in.Pressed(ActMenu) && v.kind == vPlaylists:
		// Sin listas aún: Start crea una.
		a.menu = &menu{title: T("tab_lists"), items: []menuItem{{label: T("pl_new"), icon: "plus", do: a.openNewPlaylist}}}
		return
	case in.Pressed(ActShuffle) && a.shuffleTwice():
		a.playRandomSong() // doble X: una canción al azar de toda la biblioteca
		return
	case in.Pressed(ActShuffle) && n > 0:
		a.shuffleBefore = a.status.Shuffle
		a.playItem(v, true)
		return
	case in.Pressed(ActConfirm) && n > 0:
		a.openItem(v)
		return
	}
	if v.kind == vAlbums {
		cols := gridCols(a.w - a.contentX() - margin)
		switch {
		case in.Repeat(ActRight):
			v.sel = min(n-1, v.sel+1)
		case in.Repeat(ActLeft):
			if v.sel%cols == 0 {
				if nested {
					a.pop()
				} else {
					a.tabFocus = true
				}
			} else {
				v.sel--
			}
		case in.Repeat(ActDown):
			v.sel = min(n-1, v.sel+cols)
		case in.Repeat(ActUp):
			v.sel = max(0, v.sel-cols)
		}
		return
	}
	switch {
	case in.Repeat(ActDown) && n > 0:
		v.sel = min(n-1, v.sel+1)
	case in.Repeat(ActUp):
		v.sel = max(0, v.sel-1)
	case in.Pressed(ActLeft):
		if nested {
			a.pop()
		} else {
			a.tabFocus = true
		}
	case in.Repeat(ActRight) && n > 0:
		v.sel = min(n-1, v.sel+10) // salto de página
	}
}

// updateTabs mueve el foco por las secciones. En pantallas anchas están en
// columna (arriba/abajo), en 1:1 en fila (izquierda/derecha).
func (a *App) updateTabs() {
	in := &a.input
	prev, next, enter := ActUp, ActDown, ActRight
	if a.w < 320 {
		prev, next, enter = ActLeft, ActRight, ActDown
	}
	switch {
	case in.Repeat(next):
		a.libTab = (a.libTab + 1) % tabCount
	case in.Repeat(prev):
		a.libTab = (a.libTab - 1 + tabCount) % tabCount
	case in.Pressed(enter), in.Pressed(ActConfirm):
		a.tabFocus = false
		if a.libTab == tabSearch {
			a.search.typing = true // se entra escribiendo
		}
	case in.Pressed(ActBack):
		a.screen = NowPlaying
	}
}

// openItem es A sobre el elemento elegido: abre colecciones y reproduce canciones.
func (a *App) openItem(v *view) {
	switch v.kind {
	case vAlbums:
		a.push(a.albumView(v.albums[v.sel]))
	case vArtists:
		ar := v.artists[v.sel]
		a.push(&view{kind: vAlbums, title: ar.Name, albums: ar.Albums})
	case vPlaylists:
		pl := v.playlists[v.sel]
		a.push(&view{kind: vPlaylist, title: pl.Name, songs: pl.Songs, pl: pl})
	case vFolder:
		if v.sel < len(v.dirs) {
			a.push(a.folderView(v.dirs[v.sel]))
			return
		}
		a.playSongs(v.songs, v.sel-len(v.dirs), false)
		a.screen = NowPlaying
	default:
		a.playSongs(v.songs, v.sel, false)
		a.screen = NowPlaying
	}
}

// itemSongs son las canciones del elemento elegido (para reproducir o encolar).
func (a *App) itemSongs(v *view) []Song {
	switch v.kind {
	case vAlbums:
		return v.albums[v.sel].Songs
	case vArtists:
		return v.artists[v.sel].AllSongs()
	case vPlaylists:
		return v.playlists[v.sel].Songs
	case vFolder:
		if v.sel < len(v.dirs) {
			_, songs := a.lib.Folder(v.dirs[v.sel])
			return songs
		}
		return []Song{v.songs[v.sel-len(v.dirs)]}
	}
	return []Song{v.songs[v.sel]}
}

// playItem es X: reproduce en aleatorio la colección elegida, o la lista
// entera si lo elegido es una canción.
func (a *App) playItem(v *view, shuffle bool) {
	songs := v.songs
	switch v.kind {
	case vAlbums, vArtists, vPlaylists:
		songs = a.itemSongs(v)
	case vFolder:
		if v.sel < len(v.dirs) {
			songs = a.itemSongs(v)
		}
	}
	if len(songs) == 0 {
		return
	}
	a.playSongs(songs, 0, shuffle)
	a.screen = NowPlaying
}

// drawLibrary pinta la biblioteca: las secciones, la vista actual y lo que suena.
func (a *App) drawLibrary(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	x0, top := a.contentX(), topH+8
	drawTab := func(x, y, tw int, t string, i int) {
		a.tapZone(x-3, y-4, tw, 13, func() {
			a.libTab, a.tabFocus = i, false
			if i == tabSearch {
				a.search.typing = true
			}
		})
		switch {
		case i == a.libTab && a.tabFocus:
			panel(dst, x-3, y-4, tw, 13, th.Accent)
			marching(dst, x-5, y-6, tw+4, 17, a.t, th.Accent)
			drawText(dst, t, SmallFace, x+2, y, th.Bg)
		case i == a.libTab:
			panel(dst, x-3, y-4, tw, 13, mix(th.Bg2, th.Accent, 0.35))
			drawText(dst, t, SmallFace, x+2, y, th.Text)
		default:
			drawText(dst, t, SmallFace, x+2, y, th.Dim)
		}
	}
	if w >= 320 {
		for i, t := range tabs() {
			drawTab(margin, topH+8+i*16, 72, t, i)
		}
		rect(dst, margin+75, topH+5, 1, h-topH-bottomH-44, withAlpha(th.Dim, 0x40))
	} else {
		x := margin - max(0, a.libTab-1)*56
		for i, t := range tabs() {
			tw := textWidth(t, SmallFace) + 6
			if x > -tw && x < w {
				drawTab(x, top, tw, t, i)
			}
			x += tw + 8
		}
		top += 15
	}
	bottom := h - bottomH - 36
	if a.libTab == tabSearch {
		a.drawSearch(dst, x0, top, w-x0-margin, bottom)
		a.drawMiniPlayer(dst)
		a.drawTopBar(dst, a.sourceName()+" · "+tabs()[a.libTab])
		a.drawHints(dst, a.searchHints())
		return
	}
	v := a.cur()
	cw := w - x0 - margin
	if len(a.stacks[a.libTab]) > 1 || v.kind == vAlbum {
		// Ruta de vuelta: "< Título" (tocarla es volver).
		a.tapZone(x0-3, top-4, cw+6, 13, func() { a.input.inject(ActBack) })
		drawText(dst, "<", SmallFace, x0, top, th.Accent2)
		drawTextW(dst, v.title, SmallFace, x0+8, top, cw-10, th.Accent2)
		top += 12
	}
	clip := dst.SubImage(image.Rect(0, top-4, w, bottom)).(*ebiten.Image)
	switch v.kind {
	case vAlbums:
		a.drawGrid(clip, v, x0, top, cw, bottom-top)
	case vAlbum:
		a.drawAlbumView(clip, v, x0, top, cw, bottom-top)
	default:
		a.drawList(clip, v, x0, top, cw, bottom-top)
	}
	a.drawMiniPlayer(dst)
	title := a.sourceName() + " · " + tabs()[a.libTab]
	a.drawTopBar(dst, title)
	hints := []hint{{"A", T("h_open")}, {"X", T("h_shuffle")}, {"START", T("h_more")}, {"SEL", T("h_menu")}}
	if a.tabFocus {
		hints = []hint{{"A", T("h_open")}, {"B", T("now_short")}, {"SEL", T("h_menu")}}
	}
	if v.count() == 0 {
		msg := T("empty")
		if st := a.remoteState(); st != "" {
			msg = st
		}
		drawText(dst, msg, SmallFace, x0+(cw-textWidth(msg, SmallFace))/2, top+30, th.Dim)
	}
	a.drawHints(dst, hints)
}

// listRow es una fila de una lista: icono, texto principal y secundario.
type listRow struct {
	icon, main, sub string
	playing         bool
}

// rowFor describe el elemento i de una vista de lista.
func (a *App) rowFor(v *view, i int) listRow {
	playing := func(s Song) bool { return s.Path == a.playingSong.Path && s.Path != "" }
	switch v.kind {
	case vArtists:
		ar := v.artists[i]
		return listRow{"", ar.Name, N("n_albums", len(ar.Albums)), false}
	case vPlaylists:
		pl := v.playlists[i]
		return listRow{"note", pl.Name, N("n_songs", len(pl.Songs)), false}
	case vFolder:
		if i < len(v.dirs) {
			return listRow{"folder", filepath.Base(v.dirs[i]), "", false}
		}
		s := v.songs[i-len(v.dirs)]
		return listRow{"", s.Title, s.Artist, playing(s)}
	}
	s := v.songs[i]
	return listRow{"", s.Title, s.Artist, playing(s)}
}

// drawList pinta una vista de lista con la fila elegida a la vista.
func (a *App) drawList(dst *ebiten.Image, v *view, x, y, w, h int) {
	th := a.theme
	const rowH = 13
	rows := max(1, h/rowH)
	n := v.count()
	// Con el dedo, la lista se desplaza píxel a píxel (off: lo que se ve
	// cortada la primera fila); con la cruceta, sigue a la elegida.
	first, off := v.scr.place(v.sel, rows, n, rowH)
	a.scrollArea(x-3, y-4, w+6, h, v.scr.by)
	for i := 0; i <= rows && first+i < n; i++ {
		idx := first + i
		r := a.rowFor(v, idx)
		ry := y + i*rowH - off
		mainCol, subCol := th.Text, th.Dim
		if idx == v.sel {
			if a.tabFocus {
				panel(dst, x-3, ry-4, w+6, rowH, withAlpha(th.Bg2, 0xd0))
			} else {
				panel(dst, x-3, ry-4, w+6, rowH, mix(th.Bg2, th.Accent, 0.35))
				rect(dst, x-3, ry-4, 2, rowH, th.Accent)
				subCol = th.Accent
			}
		}
		tx := x + 2
		if r.playing {
			drawIcon(dst, "note", x, ry-1, th.Accent2)
			tx = x + 10
		} else if r.icon != "" {
			drawIcon(dst, r.icon, x, ry-1, th.Accent)
			tx = x + 10
		}
		if r.sub == "" {
			drawTextW(dst, r.main, SmallFace, tx, ry, w-(tx-x), mainCol)
			continue
		}
		mainW := w * 58 / 100
		drawTextW(dst, r.main, SmallFace, tx, ry, mainW-(tx-x)-4, mainCol)
		drawTextW(dst, r.sub, SmallFace, x+mainW, ry, w-mainW, subCol)
	}
	// Tocar una fila la elige y la abre (sin que la vista salte).
	a.dragStep(0, rowH)
	for i := 0; i <= rows && first+i < n; i++ {
		idx := first + i
		a.tapZone(x-3, y+i*rowH-off-4, w+6, rowH, func() {
			a.tabFocus = false
			v.scr.pick(&v.sel, idx)
			a.input.inject(ActConfirm)
		})
	}
	a.scrollBar(dst, x+w+4, y-3, h, first, rows, n)
}

// scrollBar pinta una barrita que dice por dónde va la lista, si no cabe entera.
func (a *App) scrollBar(dst *ebiten.Image, x, y, h, first, rows, n int) {
	if n <= rows || h < 8 {
		return
	}
	th := a.theme
	rect(dst, x, y, 1, h, withAlpha(th.Dim, 0x30))
	bh := max(4, h*rows/n)
	by := y + (h-bh)*first/max(1, n-rows)
	rect(dst, x, by, 1, bh, th.Accent)
}

// drawGrid pinta una cuadrícula de álbumes, con el título del elegido arriba.
func (a *App) drawGrid(dst *ebiten.Image, v *view, x, y, w, h int) {
	th := a.theme
	n := len(v.albums)
	if n == 0 {
		return
	}
	cols, gridTile := gridLayout(w)
	x0 := x + (w-(cols*(gridTile+gridGap)-gridGap))/2
	// El título del álbum elegido.
	sel := v.albums[v.sel]
	a.drawMarquee(dst, sel.Title, x, y+2, w, false)
	drawTextW(dst, sel.Artist+" · "+N("n_songs", len(sel.Songs)), SmallFace, x, y+18, w, th.Accent)
	gy := y + 30
	row := v.sel / cols
	visible := max(1, (h-30)/(gridTile+gridGap))
	// Con el dedo, la cuadrícula se desplaza píxel a píxel; con la cruceta,
	// sigue a la elegida.
	step := gridTile + gridGap
	first, off := v.scr.place(row, visible, (n+cols-1)/cols, step)
	gy -= off
	a.scrollArea(x, y+28, w, h-28, v.scr.by)
	// Se piden ya las portadas de la fila de arriba y la de abajo.
	for i := max(0, (first-1)*cols); i < n && i < (first+visible+2)*cols; i++ {
		a.cover(v.albums[i], gridTile)
	}
	for i := first * cols; i < n && i < (first+visible+1)*cols; i++ {
		if i == v.sel && row >= first && row <= first+visible {
			continue // se dibuja encima, al final
		}
		tx := x0 + (i%cols)*(gridTile+gridGap)
		ty := gy + (i/cols-first)*(gridTile+gridGap)
		rect(dst, tx+2, ty+2, gridTile, gridTile, th.Shadow)
		a.drawCover(dst, v.albums[i], tx, ty, gridTile, 0.62)
	}
	tx := x0 + (v.sel%cols)*(gridTile+gridGap)
	ty := gy + (row-first)*(gridTile+gridGap) - 1
	if row < first || row > first+visible { // el dedo la dejó fuera de la vista
		a.gridZones(v, x0, gy, cols, gridTile, first, visible, n)
		return
	}
	rect(dst, tx+3, ty+3, gridTile, gridTile, th.Shadow)
	if img, alpha := a.cover(sel, gridTile); img != nil && alpha >= 1 {
		drawShine(dst, img, tx, ty, math.Mod(a.t*0.5, 1.4))
	} else {
		a.drawCover(dst, sel, tx, ty, gridTile, 1)
	}
	if !a.tabFocus {
		marching(dst, tx-2, ty-2, gridTile+4, gridTile+4, a.t, th.Accent)
	}
	a.gridZones(v, x0, gy, cols, gridTile, first, visible, n)
}

// gridZones hace tocables las portadas que se ven: tocar una la abre.
func (a *App) gridZones(v *view, x0, gy, cols, gridTile, first, visible, n int) {
	step := gridTile + gridGap
	a.dragStep(step, step)
	for i := first * cols; i < n && i < (first+visible+1)*cols; i++ {
		idx := i
		tx := x0 + (i%cols)*step
		ty := gy + (i/cols-first)*step
		a.tapZone(tx, ty, gridTile, gridTile, func() {
			a.tabFocus = false
			v.scr.pick(&v.sel, idx)
			v.scr.drawn = idx / cols // place sigue la fila, no la portada
			a.input.inject(ActConfirm)
		})
	}
}

// drawAlbumView pinta un álbum: portada y datos arriba, canciones debajo.
func (a *App) drawAlbumView(dst *ebiten.Image, v *view, x, y, w, h int) {
	th := a.theme
	al := v.album
	const size = 40
	rect(dst, x+2, y+2, size, size, th.Shadow)
	a.drawCover(dst, al, x, y, size, 1)
	tx := x + size + 8
	drawTextW(dst, al.Artist, SmallFace, tx, y+4, w-size-8, th.Accent)
	drawText(dst, N("n_songs", len(al.Songs)), SmallFace, tx, y+16, th.Dim)
	if a.playing == al {
		drawIcon(dst, "note", tx, y+27, th.Accent2)
		drawText(dst, T("playing"), SmallFace, tx+10, y+28, th.Accent2)
	}
	ly := y + size + 10
	const rowH = 13
	rows := max(1, (h-size-10)/rowH)
	n := len(v.songs)
	first := max(0, min(v.sel-rows/2, n-rows))
	for i := 0; i < rows && first+i < n; i++ {
		idx := first + i
		s := v.songs[idx]
		ry := ly + i*rowH
		col := th.Text
		if idx == v.sel {
			panel(dst, x-3, ry-4, w+6, rowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x-3, ry-4, 2, rowH, th.Accent)
		}
		num := fmt.Sprintf("%02d", idx+1)
		if s.Track > 0 {
			num = fmt.Sprintf("%02d", s.Track%1000) // los remotos llevan el disco en los miles
		}
		if s.Path == a.playingSong.Path {
			drawIcon(dst, "note", x, ry-1, th.Accent2)
		} else {
			drawText(dst, num, SmallFace, x, ry, th.Dim)
		}
		drawTextW(dst, s.Title, SmallFace, x+14, ry, w-16, col)
	}
	a.scrollBar(dst, x+w+4, ly-3, rows*rowH, first, rows, n)
}
