package ui

import (
	"github.com/juanvillacortac/audiofilus/internal/ytm"
	"image/color"
	"sort"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Buscar: la primera pestaña de la biblioteca. Con el mando se escribe en un
// teclado en pantalla (A escribe, B borra, X espacio, Y limpia, Start o OK
// pasan a los resultados); con el teclado, directamente. Los resultados salen
// mientras se escribe: artistas, álbumes y canciones, sin distinguir
// mayúsculas ni tildes, primero los que empiezan por lo buscado.

// oskKeys es el teclado en pantalla, en QWERTY (lo que todos conocen), con
// los números arriba y la Ñ junto a la L: "_" es espacio, "<" borrar.
var oskKeys = [][]string{
	{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
	{"Q", "W", "E", "R", "T", "Y", "U", "I", "O", "P"},
	{"A", "S", "D", "F", "G", "H", "J", "K", "L", "Ñ"},
	{"Z", "X", "C", "V", "B", "N", "M", "_", "<", "OK"},
}

// Qué es cada resultado.
const (
	resArtist = iota
	resAlbum
	resSong
	resYT // de YouTube Music (ytmusic.go)
)

// searchResult es un resultado de la búsqueda.
type searchResult struct {
	kind int
	main string
	sub  string
	rank int
	ar   *Artist
	al   *Album
	song Song
	yt   ytm.Track
	have bool // de YouTube Music, y probablemente ya está en la biblioteca
}

// searchState es la búsqueda en curso.
type searchState struct {
	query     []rune
	typing    bool // el teclado está a la vista (si no, se navega por los resultados)
	row, col  int  // la tecla elegida
	results   []searchResult
	sel       int
	lastQuery string
	scope     int // dónde se busca (con yt-dlp): todo, la biblioteca o YouTube Music
}

// Dónde se busca. Sin yt-dlp, siempre en la biblioteca.
const (
	scopeAll = iota
	scopeLibrary
	scopeYT
	scopeCount
)

// scopeNames son los nombres de los filtros, para la fila de debajo de la caja.
var scopeNames = [scopeCount]string{"sf_all", "sf_library", "sf_yt"}

// searchScope pasa al filtro anterior (-1) o siguiente (1): L1/R1 en Buscar.
func (a *App) searchScope(d int) {
	s := &a.search
	s.scope = (s.scope + d + scopeCount) % scopeCount
	s.lastQuery = "" // se vuelven a juntar los resultados
	if s.scope != scopeLibrary && !s.typing {
		a.ytSearch(string(s.query))
	}
	a.updateSearchResults()
}

// fold pasa s a mayúsculas sin tildes, para comparar.
func fold(s string) string { return accents.Replace(strings.ToUpper(s)) }

// matchRank dice cómo casa name con q (ya plegada): 0 si empieza por q, 1 si
// alguna palabra empieza por q, 2 si la contiene, -1 si no.
func matchRank(name, q string) int {
	n := fold(name)
	switch {
	case strings.HasPrefix(n, q):
		return 0
	case strings.Contains(n, " "+q):
		return 1
	case strings.Contains(n, q):
		return 2
	}
	return -1
}

// runSearch busca q en la biblioteca.
func (a *App) runSearch(q string) []searchResult {
	q = strings.TrimSpace(fold(q))
	if q == "" || a.lib == nil {
		return nil
	}
	var out []searchResult
	for _, ar := range a.lib.Artists {
		if r := matchRank(ar.Name, q); r >= 0 {
			out = append(out, searchResult{kind: resArtist, main: ar.Name, sub: N("n_albums", len(ar.Albums)), rank: r, ar: ar})
		}
	}
	for _, al := range a.lib.Albums {
		if r := matchRank(al.Title, q); r >= 0 {
			out = append(out, searchResult{kind: resAlbum, main: al.Title, sub: al.Artist, rank: r, al: al})
		}
	}
	for _, s := range a.lib.Songs {
		if r := matchRank(s.Title, q); r >= 0 {
			out = append(out, searchResult{kind: resSong, main: s.Title, sub: s.Artist, rank: r, song: s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].kind < out[j].kind
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// searchTyping dice si se está escribiendo en Buscar (entonces las teclas son
// para el buscador, no para los atajos).
func (a *App) searchTyping() bool {
	return a.screen == LibraryScreen && a.libTab == tabSearch && !a.tabFocus && a.search.typing && a.menu == nil
}

// updateSearch atiende Buscar.
func (a *App) updateSearch() {
	s, in := &a.search, &a.input
	if s.typing {
		a.updateTyping()
	} else {
		switch {
		case in.Repeat(ActDown) && len(s.results) > 0:
			s.sel = min(len(s.results)-1, s.sel+1)
		case in.Repeat(ActUp):
			if s.sel == 0 {
				s.typing = true
			} else {
				s.sel--
			}
		case in.Pressed(ActBack):
			s.typing = true
		case in.Pressed(ActLeft):
			a.tabFocus = true
		case in.Pressed(ActConfirm) && len(s.results) > 0:
			a.openResult(s.results[s.sel])
		case in.Pressed(ActMenu) && len(s.results) > 0:
			a.resultMenu(s.results[s.sel])
		}
	}
	a.updateSearchResults()
}

// updateSearchResults vuelve a buscar si cambió lo escrito.
func (a *App) updateSearchResults() {
	s := &a.search
	if q := string(s.query); q != s.lastQuery {
		var res []searchResult
		if s.scope != scopeYT || a.yt.bin == "" {
			res = a.runSearch(q)
		}
		if s.scope != scopeLibrary {
			res = append(res, a.ytResults(q)...)
		}
		s.lastQuery, s.results = q, res
		s.sel = min(s.sel, max(0, len(s.results)-1))
	}
}

// updateTyping es escribir: con el teclado de verdad, las letras van
// directas; con el mando, se mueve por el teclado en pantalla.
func (a *App) updateTyping() {
	s, in := &a.search, &a.input
	// El teclado de verdad.
	for _, r := range ebiten.AppendInputChars(nil) {
		if unicode.IsPrint(r) && len(s.query) < 40 {
			s.query = append(s.query, r)
		}
	}
	keyRepeat := func(k ebiten.Key) bool {
		d := inpututil.KeyPressDuration(k)
		return d == 1 || (d > 24 && d%4 == 0)
	}
	switch {
	case keyRepeat(ebiten.KeyBackspace):
		a.backspace()
		return
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		a.searchDone()
		return
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		a.tabFocus = true
		return
	}
	if !in.usingPad && len(in.keys) > 0 {
		return // con teclado no se usa el de pantalla (sus letras son atajos)
	}
	// El teclado en pantalla, con el mando.
	rows := len(oskKeys)
	switch {
	case in.Repeat(ActUp):
		s.row = (s.row - 1 + rows) % rows
	case in.Repeat(ActDown):
		s.row = (s.row + 1) % rows
	case in.Repeat(ActLeft):
		s.col = (s.col - 1 + len(oskKeys[s.row])) % len(oskKeys[s.row])
	case in.Repeat(ActRight):
		s.col = (s.col + 1) % len(oskKeys[s.row])
	case in.Pressed(ActConfirm):
		switch k := oskKeys[s.row][s.col]; k {
		case "_":
			s.query = append(s.query, ' ')
		case "<":
			a.backspace()
		case "OK":
			a.searchDone()
		default:
			if len(s.query) < 40 {
				s.query = append(s.query, []rune(k)...)
			}
		}
	case a.yt.bin != "" && in.Pressed(ActPrev):
		a.searchScope(-1)
	case a.yt.bin != "" && in.Pressed(ActNext):
		a.searchScope(1)
	case in.Repeat(ActBack):
		a.backspace()
	case in.Pressed(ActShuffle):
		s.query = append(s.query, ' ')
	case in.Pressed(ActPause):
		s.query = s.query[:0]
	case in.Pressed(ActMenu):
		a.searchDone()
	}
}

// backspace borra la última letra; sin letras, sale a las secciones.
func (a *App) backspace() {
	s := &a.search
	if len(s.query) == 0 {
		a.tabFocus = true
		return
	}
	s.query = s.query[:len(s.query)-1]
}

// searchDone pasa a los resultados (si hay).
func (a *App) searchDone() {
	if a.search.scope != scopeLibrary {
		a.ytSearch(string(a.search.query))
	}
	if len(a.search.results) > 0 || a.yt.bin != "" {
		a.search.typing, a.search.sel = false, 0
	}
}

// openResult abre un resultado: el artista, el álbum o la canción (que suena
// con el resto de su álbum detrás).
func (a *App) openResult(r searchResult) {
	switch r.kind {
	case resYT:
		a.ytMenu(r)
	case resArtist:
		a.openArtist(r.ar.Name)
	case resAlbum:
		a.openAlbum(r.al)
	default:
		if al, i := a.lib.AlbumOf(r.song.Path); al != nil && i >= 0 {
			a.playSongs(al.Songs, i, false)
		} else {
			a.playSongs([]Song{r.song}, 0, false)
		}
		a.screen = NowPlaying
	}
}

// resultMenu es el menú (Y) de un resultado.
func (a *App) resultMenu(r searchResult) {
	switch r.kind {
	case resYT:
		a.ytMenu(r)
	case resArtist:
		a.menu = a.songsMenu(r.ar.Name, r.ar.AllSongs(), nil, "")
	case resAlbum:
		a.menu = a.songsMenu(r.al.Title, r.al.Songs, nil, r.al.Artist)
	default:
		al, _ := a.lib.AlbumOf(r.song.Path)
		a.menu = a.songsMenu(r.song.Title, []Song{r.song}, al, r.song.Artist)
	}
	if r.kind != resYT && len(a.downloadJobs()) > 0 {
		a.menu.items = append(a.menu.items, menuItem{label: T("ytm_jobs"), icon: "download", do: a.openDownloads})
	}
}

// drawSearch pinta Buscar en el hueco de la biblioteca (x, top, w, bottom).
func (a *App) drawSearch(dst *ebiten.Image, x, top, w, bottom int) {
	s, th := &a.search, a.theme
	// La caja de búsqueda, con el cursor parpadeando mientras se escribe.
	box := mix(th.Bg2, th.Accent, 0.15)
	if s.typing && !a.tabFocus {
		box = mix(th.Bg2, th.Accent, 0.3)
	}
	panel(dst, x-3, top-4, w+6, 14, box)
	drawIcon(dst, "search", x, top-1, th.Accent)
	q := string(s.query)
	if q == "" && !s.typing {
		drawText(dst, T("search_hint"), SmallFace, x+11, top, th.Dim)
	}
	tw := textWidth(q, SmallFace)
	maxW := w - 16
	drawTextW(dst, q, SmallFace, x+11, top, maxW, th.Text) // si no cabe, se desvanece
	if s.typing && !a.tabFocus && int(a.t*2)%2 == 0 {
		rect(dst, x+11+min(tw, maxW)+1, top-1, 1, 7, th.Text)
	}
	y := top + 15
	// Los filtros, si hay YouTube Music: todo, la biblioteca o YouTube.
	if a.yt.bin != "" {
		fx := x
		for i, key := range scopeNames {
			label := T(key)
			tx := fx + 4
			if i == scopeYT {
				tx += 11 // el icono de YouTube delante
			}
			lw := tx - fx + textWidth(label, SmallFace) + 4
			bg, fg := withAlpha(th.Bg2, 0xc0), th.Dim
			if i == s.scope {
				bg, fg = th.Accent, th.Bg
			}
			panel(dst, fx, y-2, lw, 10, bg)
			if i == scopeYT {
				drawIcon(dst, "ytm", fx+3, y-1, ytRed)
			}
			drawText(dst, label, SmallFace, tx, y, fg)
			fx += lw + 3
		}
		y += 13
	}

	// El teclado en pantalla, abajo, mientras se escribe con el mando.
	kbTop := bottom
	if s.typing && !a.tabFocus && (a.input.usingPad || a.offline) {
		const keyH = 13
		kbTop = bottom - len(oskKeys)*keyH
		kw := w / 10
		for r, row := range oskKeys {
			for c, k := range row {
				kx, ky := x+c*kw, kbTop+r*keyH
				sel := r == s.row && c == s.col
				bg, fg := withAlpha(th.Bg2, 0xd0), th.Text
				if sel {
					bg, fg = th.Accent, th.Bg
				}
				panel(dst, kx, ky, kw-2, keyH-2, bg)
				drawText(dst, k, SmallFace, kx+(kw-2-textWidth(k, SmallFace))/2, ky+(keyH-2-smallCap)/2, fg)
			}
		}
		kbTop -= 4
	}

	// Los resultados, entre la caja y el teclado.
	const rowH = 13
	rows := max(1, (kbTop-y+4)/rowH)
	n := len(s.results)
	if n == 0 {
		switch {
		case a.yt.pending && s.scope != scopeLibrary:
			drawText(dst, T("ytm_searching"), SmallFace, x, y+2, th.Dim)
		case q != "":
			drawText(dst, T("search_none"), SmallFace, x, y+2, th.Dim)
		}
		return
	}
	first := 0
	if !s.typing {
		first = max(0, min(s.sel-rows/2, n-rows))
	}
	icons := [4]string{"user", "disc", "note", "ytm"}
	for i := 0; i < rows && first+i < n; i++ {
		r := s.results[first+i]
		ry := y + 4 + i*rowH
		subCol := th.Dim
		if !s.typing && first+i == s.sel && !a.tabFocus {
			panel(dst, x-3, ry-4, w+6, rowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x-3, ry-4, 2, rowH, th.Accent)
			subCol = th.Accent
		}
		// Una raya separa los de YouTube Music de los de la biblioteca.
		if r.kind == resYT && (first+i == 0 || s.results[first+i-1].kind != resYT) {
			rect(dst, x, ry-5, w, 1, withAlpha(th.Accent2, 0x70))
		}
		ic := th.Accent
		if r.kind == resYT {
			ic = ytRed
		}
		drawIcon(dst, icons[r.kind], x-1, ry-1, ic)
		mainW := w * 58 / 100
		drawTextW(dst, r.main, SmallFace, x+10, ry, mainW-14, th.Text)
		sx := x + mainW
		if r.have {
			// Probablemente ya está en la biblioteca.
			drawIcon(dst, "check", sx, ry, th.Accent2)
			sx += 8
		}
		drawTextW(dst, r.sub, SmallFace, sx, ry, x+w-sx, subCol)
	}
	a.scrollBar(dst, x+w+4, y, kbTop-y, first, rows, n)
}

// searchHints es la ayuda de abajo en Buscar.
func (a *App) searchHints() []hint {
	switch {
	case a.tabFocus:
		return []hint{{"A", T("h_open")}, {"B", T("now_short")}, {"SEL", T("h_menu")}}
	case a.search.typing:
		return []hint{{"A", T("h_type")}, {"B", T("h_del")}, {"X", T("h_space")}, {"START", T("h_search")}}
	}
	if a.yt.bin != "" {
		return []hint{{"A", T("h_open")}, {"START", T("h_more")}, {"L1/R1", T("h_filter")}, {"B", T("h_keys")}}
	}
	return []hint{{"A", T("h_open")}, {"START", T("h_more")}, {"B", T("h_keys")}}
}

// ytRed es el rojo del icono de YouTube en los resultados y el filtro.
var ytRed = color.RGBA{0xff, 0x33, 0x33, 0xff}
