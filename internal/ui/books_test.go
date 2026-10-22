package ui

import (
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
