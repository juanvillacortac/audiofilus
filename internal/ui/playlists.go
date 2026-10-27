package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Las listas propias. Las nuevas van en data/playlists/ como .m3u8, junto a la
// app (sin tocar la carpeta de música); las .m3u que ya había en la música se
// leen igual y también se pueden editar. Al guardar, cada canción va con su
// ruta relativa si está dentro de la carpeta de la lista (así la lista sigue
// valiendo en otro equipo) y con la absoluta si no.
//
//   - Start en cualquier canción, álbum, artista, carpeta o resultado: "Añadir
//     a una lista" (una nueva, con nombre del teclado en pantalla, o una que
//     ya exista; lo repetido no se añade).
//   - En el reproductor: añadir lo que suena y guardar la cola como lista.
//   - Dentro de una lista: quitar, subir y bajar canciones.
//   - Sobre una lista: renombrarla y borrarla (con confirmación).

// playlistsDir es donde van las listas nuevas.
func (a *App) playlistsDir() string { return filepath.Join(a.paths.Data, "playlists") }

// savePlaylist escribe la lista en su archivo.
func (a *App) savePlaylist(pl *Playlist) error {
	if err := os.MkdirAll(filepath.Dir(pl.Path), 0o755); err != nil {
		return err
	}
	dir := filepath.Dir(pl.Path)
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, s := range pl.Songs {
		p := s.Path
		if rel, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = filepath.ToSlash(rel)
		}
		fmt.Fprintf(&b, "#EXTINF:-1,%s - %s\n%s\n", s.Artist, s.Title, p)
	}
	tmp := pl.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, pl.Path)
}

// playlistFile es un nombre de archivo para una lista, sin caracteres que no
// valgan en exFAT, y que no exista ya en dir.
func playlistFile(dir, name, ext string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if clean == "" {
		clean = "Lista"
	}
	p := filepath.Join(dir, clean+ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s %d%s", clean, i, ext))
	}
}

// createPlaylist crea una lista con esas canciones y la guarda.
func (a *App) createPlaylist(name string, songs []Song) *Playlist {
	pl := &Playlist{Name: name, Path: playlistFile(a.playlistsDir(), name, ".m3u8"), Songs: slices.Clone(songs)}
	if err := a.savePlaylist(pl); err != nil {
		a.flash(T("pl_error", err))
		return nil
	}
	a.lib.Playlists = append(a.lib.Playlists, pl)
	slices.SortFunc(a.lib.Playlists, func(x, y *Playlist) int { return strings.Compare(fold(x.Name), fold(y.Name)) })
	a.refreshPlaylists()
	a.flash(T("pl_saved", name))
	return pl
}

// addToPlaylist añade a la lista las canciones que aún no tenía.
func (a *App) addToPlaylist(pl *Playlist, songs []Song) {
	have := map[string]bool{}
	for _, s := range pl.Songs {
		have[s.Path] = true
	}
	n := 0
	for _, s := range songs {
		if !have[s.Path] {
			pl.Songs = append(pl.Songs, s)
			have[s.Path] = true
			n++
		}
	}
	if n == 0 {
		a.flash(T("pl_already", pl.Name))
		return
	}
	if err := a.savePlaylist(pl); err != nil {
		a.flash(T("pl_error", err))
		return
	}
	a.refreshPlaylists()
	a.flash(T("pl_added", N("n_songs", n), pl.Name))
}

// refreshPlaylists pone al día las vistas de la biblioteca que muestran
// listas, tras un cambio; quita las de una lista que ya no existe.
func (a *App) refreshPlaylists() {
	for t, stack := range a.stacks {
		for i, v := range stack {
			if v.kind == vPlaylist && v.pl != nil && !slices.Contains(a.lib.Playlists, v.pl) {
				a.stacks[t] = stack[:max(1, i)] // la lista se borró: se sale de ella
				break
			}
			switch v.kind {
			case vPlaylists:
				v.playlists = a.lib.Playlists
			case vPlaylist:
				if v.pl != nil {
					v.songs, v.title = v.pl.Songs, v.pl.Name
				}
			}
			v.sel = max(0, min(v.sel, v.count()-1))
		}
	}
}

// openAddToPlaylist es el menú de "Añadir a una lista": una nueva o una de
// las que hay.
func (a *App) openAddToPlaylist(songs []Song) {
	if a.srvAddToPlaylist(songs) {
		return // canciones de un servidor: a sus listas (serverplaylists.go)
	}
	m := &menu{title: T("t_addlist"), subtitle: N("n_songs", len(songs))}
	m.items = append(m.items, menuItem{label: T("pl_new"), icon: "plus", do: func() {
		a.openPrompt(T("pl_name"), "", func(name string) { a.createPlaylist(name, songs) })
	}})
	for _, pl := range a.lib.Playlists {
		m.items = append(m.items, menuItem{label: pl.Name, icon: "list", do: func() { a.addToPlaylist(pl, songs) }})
	}
	a.menu = m
}

// openNewPlaylist pide el nombre de una lista vacía.
func (a *App) openNewPlaylist() {
	a.openPrompt(T("pl_name"), "", func(name string) { a.createPlaylist(name, nil) })
}

// playlistItems son las opciones propias de una lista (Start sobre ella).
func (a *App) playlistItems(pl *Playlist) []menuItem {
	return []menuItem{
		{label: T("pl_rename"), icon: "pencil", do: func() {
			a.openPrompt(T("pl_name"), pl.Name, func(name string) { a.renamePlaylist(pl, name) })
		}},
		{label: T("pl_new"), icon: "plus", do: a.openNewPlaylist},
		{label: T("pl_delete"), icon: "trash", danger: true, do: func() { a.confirmDeletePlaylist(pl) }},
	}
}

// renamePlaylist cambia el nombre de la lista y de su archivo.
func (a *App) renamePlaylist(pl *Playlist, name string) {
	to := playlistFile(filepath.Dir(pl.Path), name, filepath.Ext(pl.Path))
	if err := os.Rename(pl.Path, to); err != nil {
		a.flash(T("pl_error", err))
		return
	}
	pl.Name, pl.Path = name, to
	a.refreshPlaylists()
}

// confirmDeletePlaylist pregunta antes de borrar la lista (y su archivo).
func (a *App) confirmDeletePlaylist(pl *Playlist) {
	a.menu = &menu{title: T("pl_delete_q", pl.Name), subtitle: N("n_songs", len(pl.Songs)), items: []menuItem{
		{label: T("pl_keep"), icon: "arrow_l", do: func() {}},
		{label: T("pl_delete_yes"), icon: "trash", danger: true, do: func() {
			if err := os.Remove(pl.Path); err != nil && !os.IsNotExist(err) {
				a.flash(T("pl_error", err))
				return
			}
			a.lib.Playlists = slices.DeleteFunc(a.lib.Playlists, func(p *Playlist) bool { return p == pl })
			a.refreshPlaylists()
			a.flash(T("pl_deleted", pl.Name))
		}},
	}}
}

// playlistSongItems son las opciones de una canción dentro de una lista.
func (a *App) playlistSongItems(pl *Playlist, i int) []menuItem {
	move := func(d int) func() {
		return func() {
			j := i + d
			if j < 0 || j >= len(pl.Songs) {
				return
			}
			pl.Songs[i], pl.Songs[j] = pl.Songs[j], pl.Songs[i]
			if a.savePlaylist(pl) == nil {
				if v := a.cur(); v.pl == pl {
					v.sel = j
				}
				a.refreshPlaylists()
			}
		}
	}
	return []menuItem{
		{label: T("pl_up"), icon: "arrow_u", do: move(-1)},
		{label: T("pl_down"), icon: "arrow_d", do: move(1)},
		{label: T("pl_remove"), icon: "trash", danger: true, do: func() {
			pl.Songs = slices.Delete(slices.Clone(pl.Songs), i, i+1)
			if err := a.savePlaylist(pl); err != nil {
				a.flash(T("pl_error", err))
				return
			}
			a.refreshPlaylists()
		}},
	}
}

// queueSongs son las canciones de la cola, con sus etiquetas si están en la
// biblioteca.
func (a *App) queueSongs() []Song {
	var out []Song
	for _, p := range a.queue() {
		if s, ok := a.lib.song[p]; ok {
			out = append(out, s)
		} else {
			out = append(out, Song{Path: p, Title: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))})
		}
	}
	return out
}

// promptState es un nombre que se está escribiendo (el de una lista), con el
// teclado en pantalla o con el del PC.
type promptState struct {
	title    string
	text     []rune
	row, col int
	typed    bool // se escribió tal cual (teclado del PC, minúsculas o símbolos): sin "Tipo Título"
	raw      bool // una dirección, un usuario...: nunca "Tipo Título"
	secret   bool // una contraseña: se ve con asteriscos
	layer    int  // la capa del teclado en pantalla (promptKeys)
	done     func(string)
	back     *menu // si está, cancelar vuelve a este menú (el que lo abrió)
}

// cancelPrompt deja de escribir sin aceptar: al menú de donde se abrió, si
// lo dijo (AutoEQ → Buscar → B, a AutoEQ), o a nada.
func (a *App) cancelPrompt() {
	if a.prompt != nil && a.prompt.back != nil {
		a.menu = a.prompt.back
	}
	a.prompt = nil
}

// promptRows son las capas del teclado en pantalla de openPrompt:
// mayúsculas, minúsculas y símbolos, más una fila fija para direcciones y
// para cambiar de capa ("aA" mayúsculas/minúsculas, "#+=" símbolos).
var promptRows = [3][][]string{
	oskKeys,
	{
		{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
		{"q", "w", "e", "r", "t", "y", "u", "i", "o", "p"},
		{"a", "s", "d", "f", "g", "h", "j", "k", "l", "ñ"},
		{"z", "x", "c", "v", "b", "n", "m", "_", "<", "OK"},
	},
	{
		{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
		{"!", "#", "$", "%", "&", "*", "+", ",", ";", "~"},
		{"(", ")", "[", "]", "{", "}", "'", "\"", "|", "^"},
		{"á", "é", "í", "ó", "ú", "ü", "¿", "_", "<", "OK"},
	},
}

// promptExtra es la fila fija de abajo.
var promptExtra = []string{"aA", "#+=", ".", "/", ":", "@", "-", "~", "?", "="}

// promptKeys son las filas de la capa actual.
func (p *promptState) keys() [][]string {
	rows := append([][]string{}, promptRows[p.layer]...)
	return append(rows, promptExtra)
}

// openPrompt pide un texto; done lo recibe al aceptar (no si se cancela).
func (a *App) openPrompt(title, initial string, done func(string)) {
	a.menu = nil
	a.prompt = &promptState{title: title, text: []rune(initial), done: done, typed: initial != "", row: 1}
}

// openRawPrompt pide un texto tal cual (una dirección, un usuario, una
// contraseña con secret), empezando en minúsculas.
func (a *App) openRawPrompt(title, initial string, secret bool, done func(string)) {
	a.menu = nil
	a.prompt = &promptState{title: title, text: []rune(initial), done: done, typed: true, raw: true, secret: secret, layer: 1, row: 1}
}

// updatePrompt atiende la escritura: A escribe la tecla, B borra (sin letras,
// cancela), X espacio, Y limpia, Start acepta.
func (a *App) updatePrompt() {
	p, in := a.prompt, &a.input
	accept := func() {
		name := string(p.text)
		if !p.secret {
			name = strings.TrimSpace(name)
		}
		if name == "" {
			a.cancelPrompt()
			return
		}
		a.prompt = nil
		if !p.typed && !p.raw {
			name = titleCase(name)
		}
		p.done(name)
	}
	back := func() {
		if len(p.text) == 0 {
			a.cancelPrompt()
			return
		}
		p.text = p.text[:len(p.text)-1]
	}
	if a.promptAccept { // Intro en el teclado del sistema (keyboard.go)
		a.promptAccept = false
		accept()
		return
	}
	// Con el teclado del PC se escribe directamente.
	if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
		for _, r := range chars {
			if unicode.IsPrint(r) && len(p.text) < 120 {
				p.text = append(p.text, r)
			}
		}
		p.typed = true
		return
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		accept()
		return
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		a.cancelPrompt()
		return
	case inpututil.IsKeyJustPressed(ebiten.KeyBackspace):
		back()
		return
	}
	// Con el teclado del PC no se usa el de pantalla (como en Buscar): sus
	// letras son atajos, y el carácter puede llegar un fotograma después de
	// la pulsación, así que espacio (Pausa, la Y) borraba todo, Z elegía una
	// tecla, S ponía otro espacio y M aceptaba.
	if !in.usingPad && len(in.keys) > 0 {
		return
	}
	keys := p.keys()
	rows := len(keys)
	switch {
	case in.Repeat(ActUp):
		p.row = (p.row - 1 + rows) % rows
	case in.Repeat(ActDown):
		p.row = (p.row + 1) % rows
	case in.Repeat(ActLeft):
		p.col = (p.col - 1 + len(keys[p.row])) % len(keys[p.row])
	case in.Repeat(ActRight):
		p.col = (p.col + 1) % len(keys[p.row])
	case in.Pressed(ActConfirm):
		switch k := keys[p.row][p.col]; k {
		case "aA":
			p.layer = map[int]int{0: 1, 1: 0, 2: 1}[p.layer]
		case "#+=":
			p.layer = map[int]int{0: 2, 1: 2, 2: 0}[p.layer]
		case "_":
			p.text = append(p.text, ' ')
		case "<":
			back()
		case "OK":
			accept()
		default:
			if len(p.text) < 120 {
				p.text = append(p.text, []rune(k)...)
				if p.layer != 0 || len(k) == 1 && !unicode.IsLetter([]rune(k)[0]) && !unicode.IsDigit([]rune(k)[0]) {
					p.typed = true // con minúsculas o símbolos, tal cual
				}
			}
		}
	case in.Repeat(ActBack):
		back()
	case in.Pressed(ActShuffle):
		p.text = append(p.text, ' ')
	case in.Pressed(ActPause):
		p.text = p.text[:0]
	case in.Pressed(ActMenu):
		accept()
	}
}

// titleCase pasa "MI LISTA DEL GYM" (el teclado en pantalla solo tiene
// mayúsculas) a "Mi Lista Del Gym".
func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// drawPrompt pinta la escritura: el título, la caja con el texto y el
// teclado en pantalla.
func (a *App) drawPrompt(dst *ebiten.Image) {
	p := a.prompt
	if p == nil {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xe0))
	x := margin + 4
	bw := w - 2*x
	y := topH + 8
	drawText(dst, p.title, SmallFace, x, y, th.Accent)
	y += 12
	panel(dst, x-3, y-4, bw+6, 15, mix(th.Bg2, th.Accent, 0.3))
	txt := string(p.text)
	shown := txt
	if p.secret {
		shown = strings.Repeat("*", len(p.text))
	} else if !p.typed && !p.raw {
		shown = titleCase(txt)
		if strings.HasSuffix(txt, " ") {
			shown += " "
		}
	}
	drawTextW(dst, shown, SmallFace, x, y, bw-4, th.Text)
	if int(a.t*2)%2 == 0 {
		rect(dst, x+min(textWidth(shown, SmallFace), bw-4)+1, y-1, 1, 7, th.Text)
	}
	const keyH = 14
	keys := p.keys()
	kbTop := h - bottomH - 6 - len(keys)*keyH
	kw := bw / 10
	if a.nativeKeyboard() {
		keys = nil // se escribe con el teclado del sistema (keyboard.go)
	}
	for r, row := range keys {
		for c, k := range row {
			kx, ky := x+c*kw, kbTop+r*keyH
			sel := r == p.row && c == p.col
			bg, fg := withAlpha(th.Bg2, 0xe0), th.Text
			if sel {
				bg, fg = th.Accent, th.Bg
			}
			panel(dst, kx, ky, kw-2, keyH-2, bg)
			drawText(dst, k, SmallFace, kx+(kw-2-textWidth(k, SmallFace))/2, ky+(keyH-2-smallCap)/2, fg)
			a.tapZone(kx, ky, kw, keyH, func() { p.row, p.col = r, c; a.input.inject(ActConfirm) })
		}
	}
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg)
	if a.nativeKeyboard() {
		a.drawHints(dst, []hint{{"START", T("h_ok")}})
		return
	}
	a.drawHints(dst, []hint{{"A", T("h_type")}, {"B", T("h_del")}, {"X", T("h_space")}, {"START", T("h_ok")}})
}
