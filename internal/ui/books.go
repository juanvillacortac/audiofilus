package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Los audiolibros. Un libro es un .m4b, lo que tenga el género de audiolibro
// o lo que esté en una carpeta de audiolibros; si va en varios archivos, el
// libro es su carpeta y cada archivo un capítulo. Con un libro sonando:
//
//   - se recuerda por dónde va (el archivo y el segundo) en data/books.json,
//     aunque luego suene otra cosa; al volver a ponerlo desde el principio,
//     sigue donde se dejó (si se elige otro capítulo, se respeta);
//   - L1/R1 cambian de capítulo (los del archivo, si los lleva, o de
//     archivo) y ←/→ saltan 30 s;
//   - va a su velocidad (cfg.UI.BookSpeed, también la de los podcasts), sin
//     cambiar el tono; la música, siempre a la normal.

// bookGenres son los géneros de audiolibro (en minúsculas, por partes).
var bookGenres = []string{"audiobook", "audio book", "hörbuch", "horbuch", "audiolibro", "livre audio", "audiolivro", "audiolibri", "luisterboek", "аудиокнига", "sesli kitap"}

// bookDirs son los nombres de carpeta de audiolibros (en minúsculas).
var bookDirs = []string{"audiobooks", "audiobook", "audiolibros", "audiolibro", "hörbücher", "hörbuch", "horbucher", "livres audio", "audiolivros", "audiolibri", "luisterboeken", "аудиокниги", "sesli kitaplar"}

// bookSpeeds son las velocidades que se ofrecen.
var bookSpeeds = []float64{0.75, 1, 1.25, 1.5, 1.75, 2}

// bookSkip es lo que saltan ←/→ en un libro (segundos).
const bookSkip = 30

// bookMark es por dónde va un libro.
type bookMark struct {
	Path string  `json:"path"` // el archivo (el capítulo, si va en varios)
	Pos  float64 `json:"pos"`
	At   int64   `json:"at"` // cuándo se escuchó (Unix)
}

// bookState es lo de los libros en la interfaz.
type bookState struct {
	loaded   bool
	marks    map[string]bookMark // por libro (bookKey)
	dirty    bool
	savedAt  float64
	handled  string            // la pista ya atendida al empezar (reanudar, velocidad)
	chapters []decode.Chapter  // los del archivo que suena
	chFor    string            // de qué archivo son
	chCh     chan bookChapters // los que llegan de leerlos en segundo plano
}

type bookChapters struct {
	path string
	ch   []decode.Chapter
}

// isBook dice si path es (parte de) un audiolibro.
func (a *App) isBook(path string) bool {
	if path == "" || strings.Contains(path, "://") {
		return false
	}
	if strings.EqualFold(filepath.Ext(path), ".m4b") {
		return true
	}
	if a.local != nil {
		if g := strings.ToLower(a.local.meta[path].genre); g != "" {
			for _, b := range bookGenres {
				if strings.Contains(g, b) {
					return true
				}
			}
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		if slices.Contains(bookDirs, strings.ToLower(part)) {
			return true
		}
	}
	return false
}

// spoken dice si lo que suena se escucha hablado (un libro o un podcast):
// su velocidad y sus saltos de 30 s.
func (a *App) spoken() bool {
	p := a.playingSong.Path
	return a.isBook(p) || p != "" && strings.HasPrefix(p, a.podDir())
}

// bookKey es el libro de path: el propio archivo si es un .m4b (un libro
// entero), si no su carpeta.
func bookKey(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".m4b") {
		return path
	}
	return filepath.Dir(path)
}

func (a *App) booksFile() string { return filepath.Join(a.paths.Data, "books.json") }

// loadBooks lee data/books.json la primera vez.
func (a *App) loadBooks() {
	b := &a.books
	if b.loaded {
		return
	}
	b.loaded, b.marks, b.chCh = true, map[string]bookMark{}, make(chan bookChapters, 4)
	if data, err := os.ReadFile(a.booksFile()); err == nil {
		_ = json.Unmarshal(data, &b.marks)
	}
}

// saveBooks guarda las marcas.
func (a *App) saveBooks() {
	b := &a.books
	if !b.dirty || a.offline {
		return
	}
	b.dirty, b.savedAt = false, a.t
	if data, err := json.Marshal(b.marks); err == nil {
		_ = os.MkdirAll(a.paths.Data, 0o755)
		_ = os.WriteFile(a.booksFile(), data, 0o644)
	}
}

// trackBooks va en cada Update: al empezar una pista pone su velocidad y, si
// es un libro, lo lleva a donde se dejó y pide sus capítulos; mientras
// suena, apunta por dónde va.
func (a *App) trackBooks() {
	a.loadBooks()
	b := &a.books
	select {
	case c := <-b.chCh:
		if c.path == a.playingSong.Path {
			b.chapters, b.chFor = c.ch, c.path
		}
	default:
	}
	path := a.playingSong.Path
	if path == "" {
		return
	}
	if b.handled != path {
		b.handled = path
		a.startedTrack(path)
	}
	if !a.isBook(path) || a.duration <= 0 {
		return
	}
	key := bookKey(path)
	m := b.marks[key]
	if m.Path != path || int(m.Pos) != int(a.position) {
		b.marks[key] = bookMark{Path: path, Pos: a.position, At: time.Now().Unix()}
		b.dirty = true
	}
	if b.dirty && a.t-b.savedAt > 10 {
		a.saveBooks()
	}
}

// startedTrack atiende una pista que empieza: la velocidad (la de los libros
// y podcasts, o la normal) y, en un libro, seguir donde se dejó.
func (a *App) startedTrack(path string) {
	b := &a.books
	speed := 1.0
	if a.spoken() {
		speed = orOne(a.cfg.UI.BookSpeed)
	}
	if speed != orOne(a.status.Speed) {
		a.send("speed", fmt.Sprint(speed))
	}
	b.chapters, b.chFor = nil, ""
	if !a.isBook(path) {
		return
	}
	go func() {
		b.chCh <- bookChapters{path, decode.Chapters(path)}
	}()
	m, ok := b.marks[bookKey(path)]
	if !ok || a.position > 5 {
		return
	}
	switch {
	case m.Path == path && m.Pos > 10 && (a.duration <= 0 || m.Pos < a.duration-10):
		a.send("seek", fmt.Sprint(int(m.Pos)-3)) // un poco antes, para situarse
		a.flash(T("bk_resume", clock(m.Pos)))
	case m.Path != path && a.firstOfBook(path):
		// Se puso el libro desde el principio: al capítulo en que iba (que
		// al empezar seguirá desde su segundo).
		if i := slices.Index(a.queue(), m.Path); i >= 0 {
			a.send("jump", fmt.Sprint(i))
		}
	}
}

// firstOfBook dice si path es el primer archivo de su libro en la cola.
func (a *App) firstOfBook(path string) bool {
	key := bookKey(path)
	for _, p := range a.queue() {
		if bookKey(p) == key {
			return p == path
		}
	}
	return false
}

// bookFromStart vuelve al principio del libro y olvida por dónde iba.
func (a *App) bookFromStart() {
	path := a.playingSong.Path
	key := bookKey(path)
	delete(a.books.marks, key)
	a.books.dirty = true
	for i, p := range a.queue() {
		if bookKey(p) == key {
			if p == path {
				a.send("seek", "0")
			} else {
				a.send("jump", fmt.Sprint(i))
			}
			return
		}
	}
}

// chapterStep pasa al capítulo d (-1 anterior, 1 siguiente). Con los
// capítulos del archivo, salta dentro de él (el anterior, si van más de 3 s
// del actual, vuelve a su principio); sin ellos, cambia de archivo.
func (a *App) chapterStep(d int) {
	ch := a.books.chapters
	if len(ch) < 2 || a.books.chFor != a.playingSong.Path {
		if d < 0 {
			a.send("prev")
		} else {
			a.send("next")
		}
		return
	}
	cur := a.chapterAt(a.position)
	target := cur + d
	if d < 0 && a.position-ch[cur].Start > 3 {
		target = cur
	}
	switch {
	case target < 0:
		target = 0
	case target >= len(ch):
		a.send("next") // tras el último, el archivo siguiente
		return
	}
	a.seekTo(ch[target].Start)
}

// chapterAt es el capítulo que suena en pos (índice en books.chapters).
func (a *App) chapterAt(pos float64) int {
	i := 0
	for k, c := range a.books.chapters {
		if c.Start <= pos+0.5 {
			i = k
		}
	}
	return i
}

// seekTo salta a un segundo (a la vista enseguida, como seekBy).
func (a *App) seekTo(sec float64) {
	a.send("seek", fmt.Sprint(int(sec)))
	a.seekWant, a.seekAt, a.position = sec, a.t, sec
}

// chapterLine es la línea del capítulo que suena ("Capítulo 3/12 · Título"),
// o "" si el archivo no los lleva.
func (a *App) chapterLine() string {
	ch := a.books.chapters
	if len(ch) < 2 || a.books.chFor != a.playingSong.Path {
		return ""
	}
	i := a.chapterAt(a.position)
	s := T("bk_chapter", i+1, len(ch))
	if t := strings.TrimSpace(ch[i].Title); t != "" {
		s += " · " + t
	}
	return s
}

// bookMenuItems son las opciones de un libro (o podcast) en el menú del
// reproductor: los capítulos, la velocidad y volver al principio.
func (a *App) bookMenuItems() []menuItem {
	var items []menuItem
	if ch := a.books.chapters; len(ch) > 1 && a.books.chFor == a.playingSong.Path {
		items = append(items, menuItem{label: T("bk_chapters"), icon: "list", do: a.openChapters})
	}
	items = append(items, menuItem{label: T("bk_speed"), icon: "speed",
		value: func() string { return speedLabel(orOne(a.cfg.UI.BookSpeed)) },
		do: func() {
			cur := slices.Index(bookSpeeds, orOne(a.cfg.UI.BookSpeed))
			v := bookSpeeds[(cur+1)%len(bookSpeeds)]
			a.cfg.UI.BookSpeed = v
			a.saveConfig()
			a.send("speed", fmt.Sprint(v))
		}})
	if a.isBook(a.playingSong.Path) {
		items = append(items, menuItem{label: T("bk_from_start"), icon: "prev", close: true, do: a.bookFromStart})
	}
	return items
}

// openChapters es la lista de capítulos; elegir uno salta a él.
func (a *App) openChapters() {
	cur := a.chapterAt(a.position)
	var items []menuItem
	for i, c := range a.books.chapters {
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = T("bk_chapter", i+1, len(a.books.chapters))
		}
		items = append(items, menuItem{label: title, icon: "note", note: clock(c.Start), close: true,
			active: func() bool { return i == cur }, do: func() { a.seekTo(c.Start) }})
	}
	a.menu = &menu{title: T("bk_chapters"), subtitle: a.playingSong.Title, items: items, cols: 1}
	a.menu.sel = cur
}

// speedLabel es una velocidad a la vista: "1×", "1,25×".
func speedLabel(v float64) string {
	return decimal(strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.2f", v), "0"), ".0")) + "×"
}

// orOne es v, o 1 si no se puso.
func orOne(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return v
}

// clock es un tiempo a la vista: "4:05", y con horas (un audiolibro)
// "1:02:03".
func clock(sec float64) string {
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
