package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Un libro es un .m4b, algo con el género de audiolibro o algo dentro de una
// carpeta de audiolibros; lo demás (y lo remoto) no.
func TestIsBook(t *testing.T) {
	a := &App{local: &Library{meta: map[string]songMeta{
		filepath.Join("m", "a.mp3"): {genre: "Audiobook"},
		filepath.Join("m", "b.mp3"): {genre: "Rock"},
	}}}
	for path, want := range map[string]bool{
		filepath.Join("m", "x.m4b"):                    true,
		filepath.Join("m", "a.mp3"):                    true,
		filepath.Join("m", "b.mp3"):                    false,
		filepath.Join("Audiolibros", "Dune", "01.mp3"): true,
		filepath.Join("Music", "Hörbücher", "x.opus"):  true,
		filepath.Join("Music", "Band", "song.flac"):    false,
		"radio://abc": false,
	} {
		if got := a.isBook(path); got != want {
			t.Errorf("isBook(%q) = %v, want %v", path, got, want)
		}
	}
	if bookKey(filepath.Join("b", "x.m4b")) != filepath.Join("b", "x.m4b") || bookKey(filepath.Join("b", "01.mp3")) != "b" {
		t.Error("bookKey: a .m4b is its own book; split books are their folder")
	}
}

// Mientras suena un libro se apunta por dónde va (el archivo y el segundo),
// y no lo que no es un libro.
func TestTrackBooksMarks(t *testing.T) {
	a := &App{offline: true}
	book := filepath.Join("Audiobooks", "Dune", "02.mp3")
	a.playingSong.Path, a.duration, a.position = book, 3600, 754
	a.trackBooks()
	m := a.books.marks[filepath.Join("Audiobooks", "Dune")]
	if m.Path != book || int(m.Pos) != 754 {
		t.Errorf("mark %+v, want %s at 754", m, book)
	}
	a.playingSong.Path = filepath.Join("Music", "song.mp3")
	a.trackBooks()
	if len(a.books.marks) != 1 {
		t.Errorf("a song was marked as a book: %v", a.books.marks)
	}
}

// Al volver a abrir la interfaz con un libro sonando de fondo no se salta a
// la marca guardada (la de cuando se cerró): el servicio sabe por dónde va.
// Un libro que empieza después sí sigue donde se dejó.
func TestTrackBooksReopen(t *testing.T) {
	a := &App{offline: true}
	a.books.loaded = true
	a.books.marks = map[string]bookMark{"a.m4b": {Path: "a.m4b", Pos: 754}, "b.m4b": {Path: "b.m4b", Pos: 300}}
	a.status.Platform = "muos"
	a.playingSong.Path, a.duration, a.position = "a.m4b", 3600, 0
	a.trackBooks()
	if a.message != "" {
		t.Errorf("reopening resumed the book: %q", a.message)
	}
	a.playingSong.Path, a.position = "b.m4b", 0
	a.trackBooks()
	if a.message != T("bk_resume", clock(300)) {
		t.Errorf("a book that starts later didn't resume: %q", a.message)
	}
}

// El capítulo que suena y su línea; sin capítulos, ninguna.
func TestChapterLine(t *testing.T) {
	a := &App{}
	a.playingSong.Path = "x.m4b"
	if a.chapterLine() != "" {
		t.Error("no chapters should give no line")
	}
	a.books.chapters = []decode.Chapter{{Start: 0, Title: "Intro"}, {Start: 60, Title: "One"}, {Start: 120}}
	a.books.chFor = "x.m4b"
	a.position = 90
	if i := a.chapterAt(a.position); i != 1 {
		t.Errorf("chapter at 90 s = %d, want 1", i)
	}
	if got, want := a.chapterLine(), T("bk_chapter", 2, 3)+" · One"; got != want {
		t.Errorf("line %q, want %q", got, want)
	}
	if clock(3725) != "1:02:05" || clock(65) != "1:05" {
		t.Errorf("clock: %s %s", clock(3725), clock(65))
	}
}

// La hora de la alarma se escribe como sea: "7:30", "07:30", "730", "7.30".
func TestParseClock(t *testing.T) {
	for in, want := range map[string][2]int{"7:30": {7, 30}, "07:30": {7, 30}, "730": {7, 30}, "7.30": {7, 30}, "2359": {23, 59}} {
		h, m, ok := parseClock(in)
		if !ok || h != want[0] || m != want[1] {
			t.Errorf("parseClock(%q) = %d:%d %v", in, h, m, ok)
		}
	}
	for _, bad := range []string{"25:00", "7:61", "abc", "7"} {
		if _, _, ok := parseClock(bad); ok {
			t.Errorf("parseClock(%q) should fail", bad)
		}
	}
}

// El quiz no pregunta por audiolibros: solo las canciones entran en la
// partida.
func TestQuizSkipsBooks(t *testing.T) {
	a := &App{offline: true, local: &Library{Songs: []Song{
		{Title: "Song", Path: filepath.Join("Music", "song.mp3")},
		{Title: "Dune", Path: filepath.Join("Music", "dune.m4b")},
		{Title: "Ch 1", Path: filepath.Join("Audiobooks", "Dune", "01.mp3")},
	}}}
	g := a.newQuiz()
	if len(g.pool) != 1 || g.pool[0].Title != "Song" {
		t.Errorf("quiz pool %v, want only the song", g.pool)
	}
}

// Las canciones sin álbum van a un disco con el nombre en el idioma de la
// interfaz, que cambia al cambiar el idioma.
func TestNoAlbumLanguage(t *testing.T) {
	defer SetLanguage(lang)
	SetLanguage("en")
	lib := buildLibrary([]entry{{Path: "a.mp3", Title: "A"}})
	if len(lib.Albums) != 1 || lib.Albums[0].Title != "No album" || lib.Songs[0].Album != "No album" {
		t.Fatalf("album %q, song album %q, want No album", lib.Albums[0].Title, lib.Songs[0].Album)
	}
	SetLanguage("de")
	lib.relabel()
	if lib.Albums[0].Title != "Kein Album" || lib.Songs[0].Album != "Kein Album" || lib.song["a.mp3"].Album != "Kein Album" {
		t.Errorf("after relabel: %q %q", lib.Albums[0].Title, lib.Songs[0].Album)
	}
}

// Poner en aleatorio un álbum (o una lista, o un artista) empieza por una
// canción al azar, no siempre por la primera.
func TestShuffleStartsAnywhere(t *testing.T) {
	a := &App{offline: true}
	songs := []Song{{Path: "1.mp3"}, {Path: "2.mp3"}, {Path: "3.mp3"}, {Path: "4.mp3"}, {Path: "5.mp3"}}
	starts := map[string]bool{}
	sendHook = func(cmd string, args []string) {
		if cmd == "load" {
			starts[args[0]] = true
		}
	}
	defer func() { sendHook = nil }()
	for range 40 {
		a.playShuffled(songs)
	}
	if len(starts) < 2 {
		t.Errorf("always started at %v", starts)
	}
}

// Reproduciendo pide la cola al servicio para preparar las portadas de lo que
// viene (antes solo se pedía en la pantalla de la cola, y no había qué
// preparar); con la cola al día no la vuelve a pedir.
func TestPrefetchAsksForQueue(t *testing.T) {
	a := &App{offline: true, t: 10}
	a.status.Count = 3
	a.playingSong.Path = "a.mp3"
	asked := 0
	sendHook = func(cmd string, args []string) {
		if cmd == "queue" {
			asked++
		}
	}
	defer func() { sendHook = nil }()
	a.prefetchCovers(nil, 64)
	a.prefetchCovers(nil, 64) // en el mismo segundo: no otra vez
	if asked != 1 {
		t.Errorf("asked for the queue %d times, want 1", asked)
	}
}

// Las emisoras sin logo usan el icono de audiofilus: radioDefaultLogo lo
// deja una vez junto a los logos, como un PNG que se puede leer.
func TestRadioDefaultLogo(t *testing.T) {
	a := &App{offline: true}
	a.paths.Data = t.TempDir()
	p := a.radioDefaultLogo()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Errorf("not a PNG: %v", err)
	}
	if a.radioDefaultLogo() != p {
		t.Error("a second call gave another file")
	}
}

// press simula pulsar act en un fotograma del menú abierto.
func press(a *App, act Action) {
	a.input = Input{}
	a.input.inject(act)
	a.updateMenu()
}

// Un menú abierto desde otro vuelve a él con B (Ecualizador → AutoEQ → B, al
// Ecualizador); el de arriba se cierra. Uno que se vuelve a abrir a sí mismo
// para ponerse al día no deja un bucle.
func TestMenuBackGoesUp(t *testing.T) {
	a := &App{offline: true, w: 320, h: 240}
	var eq, aeq *menu
	aeq = &menu{title: "AutoEQ", items: []menuItem{{label: "x", do: func() {}}}}
	eq = &menu{title: "EQ", items: []menuItem{{label: "AutoEQ", do: func() { a.menu = aeq }}}}
	a.menu = eq
	press(a, ActConfirm)
	if a.menu != aeq {
		t.Fatal("AutoEQ didn't open")
	}
	press(a, ActBack)
	if a.menu != eq {
		t.Fatalf("B went to %v, want the EQ menu", a.menu)
	}
	press(a, ActBack)
	if a.menu != nil {
		t.Error("B on the top menu didn't close it")
	}
	// Una opción que vuelve a abrir su propio menú (al día).
	var self func()
	self = func() { a.menu = &menu{title: "Alarm", items: []menuItem{{label: "x", value: func() string { return "" }, do: self}}} }
	self()
	press(a, ActConfirm)
	press(a, ActBack)
	if a.menu != nil {
		t.Error("a menu reopening itself made B loop")
	}
}
