package ui

import (
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// El menú (Y) es un panel que sube desde abajo: una cabecera con la portada y
// el título de lo elegido, y un mosaico de botones con icono. Los que activan o
// desactivan algo (aleatorio, repetir) se iluminan cuando están activos,
// enseñan su valor y dejan el menú abierto para ver el cambio.

// menuItem es una opción del menú.
type menuItem struct {
	label string
	icon  string
	do    func()
	// value, si está, es el valor que se ve debajo (p. ej. "Todo" en Repetir);
	// active, si está, ilumina el botón cuando devuelve true. Las opciones con
	// alguno de los dos no cierran el menú.
	value  func() string
	active func() bool
	danger bool   // Salir: en otro color
	close  bool   // cierra el menú aunque tenga active (p. ej. el de pantallas)
	note   string // un texto debajo, sin más (a diferencia de value, cierra el menú)
	cover  *Album // si está, su portada va en lugar del icono (un álbum)
	del    func() // si está, Y la borra (un perfil de AutoEQ); la ayuda lo dice
}

// menu es el menú abierto.
type menu struct {
	title    string
	subtitle string
	album    *Album // su portada va en la cabecera
	items    []menuItem
	sel      int
	openedAt float64
	// cols, si está, es cuántas columnas: menos y más anchas para nombres
	// largos (la lista de letras).
	cols int
	// parent es el menú desde el que se abrió este (B vuelve a él); nil si
	// se abrió desde una pantalla.
	parent *menu
}

// Medidas del menú.
const (
	menuTileH   = 38
	menuGap     = 4
	menuHeadH   = 34
	menuSlideIn = 0.16 // segundos que tarda en subir
)

// menuLayout coloca los botones del menú m para una pantalla de w×h: el
// panel y sus baldosas (en coordenadas de pantalla, ya subido).
func menuLayout(m *menu, w, h int) (px, py, pw, ph int, tiles []tileRect) {
	pw = min(w-2*margin+8, 340)
	cols := 3
	if pw >= 280 {
		cols = 4
	}
	if m.cols > 0 {
		cols = m.cols
		if pw < 280 {
			cols = max(1, cols-1)
		}
	}
	rows := (len(m.items) + cols - 1) / cols
	tw := (pw - 12 - (cols-1)*menuGap) / cols
	// Si no caben todas las filas (menús largos en pantallas cuadradas), se
	// ven las que quepan y se desplazan para que la elegida se vea.
	fit := max(1, (h-topH-bottomH-8-menuHeadH-6)/(menuTileH+menuGap))
	shown := min(rows, fit)
	first := 0
	if rows > shown {
		first = max(0, min(m.sel/cols-shown/2, rows-shown))
	}
	ph = menuHeadH + shown*(menuTileH+menuGap) + 6
	px, py = (w-pw)/2, h-bottomH-ph-4
	for i := range m.items {
		c, r := i%cols, i/cols-first
		tiles = append(tiles, tileRect{
			x: px + 6 + c*(tw+menuGap), y: py + menuHeadH + r*(menuTileH+menuGap),
			w: tw, h: menuTileH,
		})
	}
	return px, py, pw, ph, tiles
}

// updateMenu atiende los botones con el menú abierto.
func (a *App) updateMenu() {
	in, m := &a.input, a.menu
	if m.openedAt == 0 {
		m.openedAt = a.t
	}
	_, _, _, _, tiles := menuLayout(m, a.w, max(a.h, ScreenH))
	for _, d := range []struct {
		act    Action
		dx, dy int
	}{{ActUp, 0, -1}, {ActDown, 0, 1}, {ActLeft, -1, 0}, {ActRight, 1, 0}} {
		if in.Repeat(d.act) {
			m.sel = nearestTile(tiles, m.sel, d.dx, d.dy)
			return
		}
	}
	switch {
	case in.Pressed(ActConfirm):
		it := m.items[m.sel]
		if it.close || (it.value == nil && it.active == nil) {
			a.menu = nil
		}
		it.do()
		a.linkMenu(m)
	case in.Pressed(ActPause) && m.items[m.sel].del != nil:
		m.items[m.sel].del()
		a.linkMenu(m)
	case in.Pressed(ActMenu) && a.t-a.startAt < doubleStart:
		// Start dos veces seguidas: a dormir (la primera abrió el menú).
		a.menu = nil
		a.enterSleep()
	case in.Pressed(ActBack) && m.parent != nil:
		a.menu = m.parent // un menú abierto desde otro: se vuelve a ese
	case in.Pressed(ActBack), in.Pressed(ActMenu):
		a.menu = nil
	}
}

// linkMenu apunta de dónde viene el menú que abrió una opción de from: B
// vuelve a from (Ecualizador → AutoEQ → B, al Ecualizador) en vez de cerrar
// todo. Si el nuevo es uno que ya estaba abierto más arriba (se vuelve a
// abrir para ponerse al día, como la alarma tras elegir qué suena), ocupa su
// sitio en la cadena.
func (a *App) linkMenu(from *menu) {
	m := a.menu
	if m == nil || m == from || m.parent != nil {
		return
	}
	m.parent = from
	for p := from; p != nil; p = p.parent {
		if p.title == m.title {
			m.parent = p.parent
			return
		}
	}
}

// doubleStart es el plazo para el segundo Start del doble Start (sleep).
const doubleStart = 0.35

// paths son las rutas de unas canciones, para mandarlas al servicio.
func paths(songs []Song) []string {
	out := make([]string, len(songs))
	for i, s := range songs {
		out[i] = s.Path
	}
	return out
}

// songsMenu son las opciones para unas canciones (una, un álbum, una lista...).
func (a *App) songsMenu(title string, songs []Song, al *Album, artist string) *menu {
	m := &menu{title: title, subtitle: artist, album: al}
	if m.album == nil && len(songs) > 0 {
		m.album, _ = a.lib.AlbumOf(songs[0].Path)
	}
	if m.subtitle == "" {
		m.subtitle = N("n_songs", len(songs))
	}
	if len(songs) > 1 {
		m.items = append(m.items,
			menuItem{label: T("t_play"), icon: "play", do: func() { a.playSongs(songs, 0, false); a.screen = NowPlaying }},
			menuItem{label: T("t_shuffle"), icon: "shuffle", do: func() { a.playShuffled(songs); a.screen = NowPlaying }})
	}
	m.items = append(m.items,
		menuItem{label: T("t_next"), icon: "playnext", do: func() { a.send("playnext", paths(songs)...); a.flash(T("queued_next")) }},
		menuItem{label: T("t_add"), icon: "plus", do: func() { a.send("add", paths(songs)...); a.flash(T("queued_end")) }},
	)
	if isRemoteSongs(songs) {
		// De un servidor: se guardan en la tarjeta (las listas propias son
		// de la tarjeta).
		m.items = append(m.items, menuItem{label: T("ytm_download"), icon: "download", do: func() { a.downloadRemote(songs) }})
	} else {
		m.items = append(m.items, menuItem{label: T("t_addlist"), icon: "list", do: func() { a.openAddToPlaylist(songs) }})
	}
	if al != nil {
		m.items = append(m.items, menuItem{label: T("t_album"), icon: "disc", do: func() { a.openAlbum(al) }})
	}
	if artist != "" {
		m.items = append(m.items, menuItem{label: T("t_artist"), icon: "user", do: func() { a.openArtist(artist) }})
	}
	return m
}

// openItemMenu abre el menú del elemento elegido en la biblioteca.
func (a *App) openItemMenu(v *view) {
	defer a.addMetaItems(v) // las herramientas de las etiquetas, al final (tagsui.go)
	switch v.kind {
	case vAlbums:
		al := v.albums[v.sel]
		a.menu = a.songsMenu(al.Title, al.Songs, nil, al.Artist)
	case vArtists:
		ar := v.artists[v.sel]
		a.menu = a.songsMenu(ar.Name, ar.AllSongs(), nil, "")
	case vPlaylists:
		pl := v.playlists[v.sel]
		a.smartFill(pl)
		a.menu = a.songsMenu(pl.Name, pl.Songs, nil, "")
		if !pl.Remote {
			a.menu.items = append(a.menu.items, a.playlistItems(pl)...)
		}
		a.menu.items = append(a.menu.items, a.srvPlaylistItems(pl)...) // de un servidor (serverplaylists.go)
		if pl.smart > 0 {
			a.menu.items = append(a.menu.items, a.smartItems(pl)...)
		}
		if a.source == 0 {
			a.menu.items = append(a.menu.items, menuItem{label: T("sm_new"), icon: "bolt", do: a.smartNew})
		}
	default:
		songs := a.itemSongs(v)
		if len(songs) == 0 {
			return
		}
		if len(songs) > 1 { // una carpeta
			a.menu = a.songsMenu(v.title, songs, nil, "")
			return
		}
		s := songs[0]
		al, _ := a.lib.AlbumOf(s.Path)
		if v.kind == vAlbum {
			al = nil // ya se está en el álbum
		}
		artist := s.Artist
		if al != nil {
			artist = al.Artist
		}
		a.menu = a.songsMenu(s.Title, songs, al, artist)
		a.menu.items = append(a.menu.items, a.markItems(s)...)
		if v.kind == vPlaylist && v.pl != nil && !v.pl.Remote {
			a.menu.items = append(a.menu.items, a.playlistSongItems(v.pl, v.sel)...)
		}
		if v.kind == vPlaylist && v.pl != nil && v.pl.srvID != "" {
			a.menu.items = append(a.menu.items, a.srvPlaylistSongItems(v.pl, v.sel)...) // serverplaylists.go
		}
	}
}

// openNowMenu es el menú de la pantalla de reproducción.
func (a *App) openNowMenu() {
	m := &menu{title: orDefault(a.playingSong.Title, T("now")), subtitle: a.playingSong.Artist, album: a.playing}
	if a.playing != nil && !a.playing.stray {
		al := a.playing
		m.items = append(m.items,
			menuItem{label: T("t_album"), icon: "disc", do: func() { a.openAlbum(al) }},
			menuItem{label: T("t_artist"), icon: "user", do: func() { a.openArtist(al.Artist) }})
	}
	if t, ok := a.streamSavable(); ok {
		m.items = append(m.items, menuItem{label: T("ytm_download"), icon: "download", do: func() { a.ytDownload(t) }})
	}
	if len(a.downloadJobs()) > 0 {
		m.items = append(m.items, menuItem{label: T("ytm_jobs"), icon: "download", do: a.openDownloads})
	}
	if a.playingSong.Path != "" {
		m.items = append(m.items, menuItem{label: T("lyr_title"), icon: "lyrics", do: a.openLyrics})
		m.items = append(m.items, a.markItems(a.playingSong)...)
	}
	m.items = append(m.items, a.castItems()...) // enviar a un aparato (castui.go)
	if a.spoken() {
		m.items = append(m.items, a.bookMenuItems()...) // capítulos, velocidad (books.go)
	}
	m.items = append(m.items, a.timerItem())
	if it, ok := a.eqProfileItem(); ok {
		m.items = append(m.items, it) // los perfiles de EQ de los audífonos
	}
	// El visualizador que se dejó puesto, de fondo (atenuado).
	m.items = append(m.items, menuItem{label: T("vis_bg"), icon: visIcons[a.vis.kind],
		do: func() {
			a.cfg.UI.VisualBG = !a.cfg.UI.VisualBG
			a.saveConfig()
		},
		active: func() bool { return a.cfg.UI.VisualBG },
		value: func() string {
			if !a.cfg.UI.VisualBG {
				return T("off")
			}
			return T(visNames[a.vis.kind])
		}})
	if song := a.playingSong; song.Path != "" {
		if remote.IsRemote(song.Path) {
			m.items = append(m.items, menuItem{label: T("ytm_download"), icon: "download", do: func() { a.downloadRemote([]Song{song}) }})
		} else {
			m.items = append(m.items, menuItem{label: T("t_addlist"), icon: "list", do: func() { a.openAddToPlaylist([]Song{song}) }})
		}
	}
	m.items = append(m.items,
		menuItem{label: T("pl_save_queue"), icon: "plus", do: func() {
			songs := a.queueSongs()
			a.openPrompt(T("pl_name"), "", func(name string) { a.createPlaylist(name, songs) })
		}},
		menuItem{label: T("t_queue"), icon: "list", do: func() { a.screen = Queue }},
		menuItem{label: T("set_shuffle"), icon: "shuffle",
			do:     func() { a.send("shuffle", onOffArg(!a.status.Shuffle)); a.status.Shuffle = !a.status.Shuffle },
			active: func() bool { return a.status.Shuffle },
			value:  func() string { return onOffLabel(a.status.Shuffle) }},
		menuItem{label: T("set_repeat"), icon: "repeat",
			do: func() {
				next := map[string]string{"off": "all", "all": "one", "one": "off"}[orDefault(a.status.Repeat, "off")]
				a.send("repeat", next)
				a.status.Repeat = next // se ve ya; el servicio lo confirma
			},
			active: func() bool { return orDefault(a.status.Repeat, "off") != "off" },
			value:  func() string { return T("repeat_" + orDefault(a.status.Repeat, "off")) }},
	)
	// Apagar la pantalla, solo en las portátiles; segundo plano, no en el
	// móvil (ahí la música ya sigue al salir de la app).
	if a.canSleep() {
		m.items = append(m.items, menuItem{label: T("t_sleep"), icon: "moon", do: func() { a.enterSleep() }})
	}
	if !a.mobile() {
		m.items = append(m.items, menuItem{label: T("t_bg"), icon: "bg", do: func() { a.exit(true) }})
	}
	m.items = append(m.items, menuItem{label: T("t_quit"), icon: "power", do: func() { a.exit(false) }, danger: true})
	a.menu = m
}

func onOffLabel(b bool) string {
	if b {
		return T("on")
	}
	return T("off")
}

// drawMenu pinta el menú encima de la pantalla: el fondo oscurecido y el
// panel, que sube desde abajo al abrirse.
func (a *App) drawMenu(dst *ebiten.Image) {
	if a.menu == nil {
		return
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	m := a.menu
	if m.openedAt == 0 && !a.offline {
		// La entrada empieza al dibujarse por primera vez (no al primer
		// Update del menú: mientras se mantiene Select, no hay, y al soltar
		// volvía a entrar).
		m.openedAt = a.t
	}
	px, py, pw, ph, tiles := menuLayout(m, w, h)
	// Sube con un rebote corto, en píxeles enteros.
	dy := 0
	if p := (a.t - m.openedAt) / menuSlideIn; m.openedAt > 0 && p < 1 {
		dy = int(math.Round(float64(ph+bottomH) * (1 - easeOutBack(math.Max(0, p)))))
	}
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xa0))
	a.tapZone(0, 0, w, h, func() { a.input.inject(ActBack) }) // tocar fuera lo cierra
	py += dy
	rect(dst, px+3, py+3, pw, ph, th.Shadow)
	panel(dst, px, py, pw, ph, th.Bg2)
	rect(dst, px+1, py, pw-2, 1, th.Accent) // el filo de arriba, en color

	// Cabecera: portada, título y subtítulo.
	const cs = 24
	tx := px + 8
	if m.album != nil {
		a.drawCover(dst, m.album, px+6, py+5, cs, 1)
		tx = px + 6 + cs + 6
	}
	drawTextW(dst, m.title, SmallFace, tx, py+8, px+pw-tx-6, th.Text)
	drawTextW(dst, m.subtitle, SmallFace, tx, py+18, px+pw-tx-6, th.Accent)

	// Si no caben todas, una barrita a la derecha dice cuántas quedan.
	if n := len(tiles); n > 0 {
		rowH := menuTileH + menuGap
		total := (tiles[n-1].y-tiles[0].y)/rowH + 1
		shown := (ph - menuHeadH - 6) / rowH
		if total > shown {
			first := (py - dy + menuHeadH - tiles[0].y) / rowH
			a.scrollBar(dst, px+pw-4, py+menuHeadH, ph-menuHeadH-6, first, shown, total)
		}
	}
	for i, t := range tiles {
		if t.y < py-dy+menuHeadH-2 || t.y+t.h > py-dy+ph {
			continue // fuera de lo que se ve (el menú se desplaza)
		}
		it := m.items[i]
		t.y += dy
		idx := i
		a.tapZone(t.x, t.y, t.w, t.h, func() { m.sel = idx; a.input.inject(ActConfirm) })
		sel := i == m.sel
		on := it.active != nil && it.active()
		bg, fg := withAlpha(th.Bg, 0xc0), th.Text
		iconCol := th.Dim
		switch {
		case sel && it.danger:
			bg, fg, iconCol = mix(th.Bg2, th.Accent2, 0.7), th.Text, th.Text
		case sel:
			bg, fg, iconCol = th.Accent, th.Bg, th.Bg
		case on:
			bg, iconCol = mix(th.Bg2, th.Accent, 0.35), th.Accent
		case it.danger:
			iconCol = th.Accent2
		}
		panel(dst, t.x, t.y, t.w, t.h, bg)
		if sel {
			marching(dst, t.x-2, t.y-2, t.w+4, t.h+4, a.t, th.Accent)
		}
		// Icono (al doble), nombre y, si hay, valor: el conjunto, centrado en
		// vertical por la altura real de las letras (smallCap), no por la línea.
		sub := it.value != nil || it.note != ""
		content := 14 + 5 + smallCap
		if sub {
			content = 14 + 4 + smallCap + 4 + smallCap
		}
		top := t.y + (t.h-content)/2
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(2, 2)
		op.GeoM.Translate(float64(t.x+t.w/2-7), float64(top))
		op.ColorScale.ScaleWithColor(iconCol)
		if it.cover != nil && !it.cover.missing {
			a.drawCover(dst, it.cover, t.x+t.w/2-7, top, 14, 1)
		} else {
			dst.DrawImage(icon(it.icon), op)
		}
		label := it.label
		ly := top + 14 + 5
		if sub {
			ly = top + 14 + 4
		}
		vy := ly + smallCap + 4
		lw := textWidth(label, SmallFace)
		drawTextW(dst, label, SmallFace, t.x+max(2, (t.w-lw)/2), ly, t.w-4, fg)
		if it.note != "" {
			vc := th.Dim
			if sel {
				vc = th.Bg
			}
			drawTextW(dst, it.note, SmallFace, t.x+max(2, (t.w-textWidth(it.note, SmallFace))/2), vy, t.w-4, vc)
		}
		if it.value != nil {
			v := it.value()
			vc := th.Dim
			if sel {
				vc = th.Bg
			} else if on {
				vc = th.Accent
			}
			// Dentro de la baldosa (un perfil de EQ con un nombre largo se
			// salía de ella): recortado y, en la elegida, de lado a lado para
			// leerlo entero (perfiles que solo se distinguen al final, "ANC").
			vx := t.x + max(2, (t.w-textWidth(v, SmallFace))/2)
			if sel {
				a.scrollText(dst, v, SmallFace, vx, vy, t.w-4, vc, nil)
			} else {
				drawTextW(dst, v, SmallFace, vx, vy, t.w-4, vc)
			}
		}
	}
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg) // tapa la ayuda de la pantalla de debajo
	hints := []hint{{"A", T("h_ok")}}
	if m.sel < len(m.items) && m.items[m.sel].del != nil {
		hints = append(hints, hint{"Y", T("h_delete")})
	}
	a.drawHints(dst, append(hints, hint{"B", T("h_close")}))
}
