package smart

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/history"
)

// now es la hora de referencia de los tests.
var now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)

func daysAgo(d int) time.Time { return now.AddDate(0, 0, -d) }

// lib es una biblioteca de prueba.
var lib = []Track{
	{Path: "/m/Radiohead/OK Computer/01 Airbag.flac", Title: "Airbag", Artist: "Radiohead", Album: "OK Computer", Genre: "Rock", Year: 1997, Track: 1, Duration: 284, Bitrate: 900, Added: daysAgo(400)},
	{Path: "/m/Radiohead/OK Computer/02 Paranoid Android.flac", Title: "Paranoid Android", Artist: "Radiohead", Album: "OK Computer", Genre: "Rock", Year: 1997, Track: 2, Duration: 383, Bitrate: 950, Added: daysAgo(400)},
	{Path: "/m/Daft Punk/Discovery/01 One More Time.mp3", Title: "One More Time", Artist: "Daft Punk", Album: "Discovery", AlbumArtist: "Daft Punk", Genre: "Electronic", Year: 2001, Track: 1, Duration: 320, Bitrate: 320, Added: daysAgo(10)},
	{Path: "/m/Various/Mix/03 Song.ogg", Title: "Song", Artist: "Someone", Album: "Mix", AlbumArtist: "Various Artists", Genre: "Pop", Year: 2015, Track: 3, Duration: 180, Bitrate: 160, Added: daysAgo(2)},
	{Path: "subsonic://casa/abc", Title: "Remote Tune", Artist: "Remote", Album: "Cloud", Format: "opus", Year: 2020},
}

// hist es el historial: Airbag 3 veces (una hace 100 días), Paranoid 6 veces
// hace 120 días, One More Time una vez ayer.
var hist = func() []history.Listen {
	var ls []history.Listen
	add := func(p string, d int) { ls = append(ls, history.Listen{At: daysAgo(d).Unix(), Path: p}) }
	add(lib[0].Path, 100)
	add(lib[0].Path, 5)
	add(lib[0].Path, 3)
	for i := 0; i < 6; i++ {
		add(lib[1].Path, 120)
	}
	add(lib[2].Path, 1)
	return ls
}()

// marks son las marcas: Paranoid me gusta y 5 estrellas, Song 2 estrellas.
type fakeMarks map[string]history.Mark

func (m fakeMarks) Get(p string) history.Mark { return m[p] }

var marks = fakeMarks{
	lib[1].Path: {Fav: true, Rating: 5},
	lib[3].Path: {Rating: 2},
}

func titles(paths []string) []string {
	byPath := map[string]string{}
	for _, t := range lib {
		byPath[t.Path] = t.Title
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = byPath[p]
	}
	return out
}

func eval(d Def) []string { return titles(Evaluate(d, lib, hist, marks, now)) }

func one(field, op, value string) Def {
	return Def{Name: "t", Rules: []Rule{{Field: field, Op: op, Value: value}}}
}

// Cada operación de texto, sin distinguir mayúsculas, en cada campo de texto.
func TestTextOps(t *testing.T) {
	for _, c := range []struct {
		d    Def
		want []string
	}{
		{one("artist", "is", "radiohead"), []string{"Airbag", "Paranoid Android"}},
		{one("artist", "isnot", "Radiohead"), []string{"One More Time", "Song", "Remote Tune"}},
		{one("title", "contains", "AND"), []string{"Paranoid Android"}},
		{one("title", "notcontains", "o"), []string{"Airbag"}},
		{one("album", "startswith", "ok "), []string{"Airbag", "Paranoid Android"}},
		{one("genre", "is", "electronic"), []string{"One More Time"}},
		// Sin artista del álbum, el artista.
		{one("albumartist", "is", "Radiohead"), []string{"Airbag", "Paranoid Android"}},
		{one("albumartist", "is", "Various Artists"), []string{"Song"}},
		{one("folder", "is", "/m/daft punk/discovery"), []string{"One More Time"}},
		{one("path", "startswith", "subsonic://"), []string{"Remote Tune"}},
		{one("format", "is", "flac"), []string{"Airbag", "Paranoid Android"}},
		{one("format", "is", "opus"), []string{"Remote Tune"}},
	} {
		if got := eval(c.d); !slices.Equal(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.d.Rules[0], got, c.want)
		}
	}
}

// Operaciones de números sobre año, pista, duración, bitrate, escuchas y
// valoración.
func TestNumberOps(t *testing.T) {
	between := Def{Rules: []Rule{{Field: "year", Op: "between", Value: "2015", Value2: "1997"}}}
	for _, c := range []struct {
		d    Def
		want []string
	}{
		{one("year", "is", "1997"), []string{"Airbag", "Paranoid Android"}},
		{one("year", "isnot", "1997"), []string{"One More Time", "Song", "Remote Tune"}},
		{one("year", "gt", "2000"), []string{"One More Time", "Song", "Remote Tune"}},
		{one("year", "lt", "2000"), []string{"Airbag", "Paranoid Android"}},
		{between, []string{"Airbag", "Paranoid Android", "One More Time", "Song"}},
		{one("track", "is", "3"), []string{"Song"}},
		{one("duration", "gt", "300"), []string{"Paranoid Android", "One More Time"}},
		{one("bitrate", "lt", "300"), []string{"Song", "Remote Tune"}},
		{one("plays", "is", "0"), []string{"Song", "Remote Tune"}},
		{one("plays", "gt", "4"), []string{"Paranoid Android"}},
		{one("rating", "gt", "1"), []string{"Paranoid Android", "Song"}},
	} {
		if got := eval(c.d); !slices.Equal(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.d.Rules[0], got, c.want)
		}
	}
}

// Fechas: "en los últimos N días" y su contrario (sin fecha cuenta como
// no), y antes/después/entre días.
func TestDateOps(t *testing.T) {
	d := func(day time.Time) string { return day.Format("2006-01-02") }
	for _, c := range []struct {
		d    Def
		want []string
	}{
		{one("added", "inlast", "30"), []string{"One More Time", "Song"}},
		{one("added", "notinlast", "30"), []string{"Airbag", "Paranoid Android", "Remote Tune"}},
		{one("lastplayed", "inlast", "7"), []string{"Airbag", "One More Time"}},
		{one("lastplayed", "notinlast", "7"), []string{"Paranoid Android", "Song", "Remote Tune"}},
		{one("added", "gt", d(daysAgo(5))), []string{"Song"}},
		{one("added", "lt", d(daysAgo(5))), []string{"Airbag", "Paranoid Android", "One More Time"}},
		{Def{Rules: []Rule{{Field: "lastplayed", Op: "between", Value: d(daysAgo(130)), Value2: d(daysAgo(110))}}}, []string{"Paranoid Android"}},
	} {
		if got := eval(c.d); !slices.Equal(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.d.Rules[0], got, c.want)
		}
	}
}

// Me gusta: verdadero o falso.
func TestBoolOps(t *testing.T) {
	if got := eval(one("liked", "true", "")); !slices.Equal(got, []string{"Paranoid Android"}) {
		t.Errorf("liked: %v", got)
	}
	if got := eval(one("liked", "false", "")); len(got) != 4 {
		t.Errorf("not liked: %v", got)
	}
	if got := titles(Evaluate(one("liked", "true", ""), lib, hist, nil, now)); len(got) != 0 {
		t.Errorf("without marks: %v", got)
	}
}

// all exige todas las reglas; any, alguna; sin reglas, todas las canciones.
func TestMatch(t *testing.T) {
	rules := []Rule{{Field: "artist", Op: "is", Value: "Radiohead"}, {Field: "plays", Op: "gt", Value: "4"}}
	if got := eval(Def{Rules: rules}); !slices.Equal(got, []string{"Paranoid Android"}) {
		t.Errorf("all: %v", got)
	}
	if got := eval(Def{Match: "any", Rules: rules}); !slices.Equal(got, []string{"Airbag", "Paranoid Android"}) {
		t.Errorf("any: %v", got)
	}
	if got := eval(Def{}); len(got) != len(lib) {
		t.Errorf("no rules: %v", got)
	}
}

// PlaysDays cuenta solo las escuchas recientes.
func TestPlaysDays(t *testing.T) {
	d := Def{Rules: []Rule{{Field: "plays", Op: "gt", Value: "0"}}, PlaysDays: 30, Sort: Sort{Field: "plays", Desc: true}}
	if got := eval(d); !slices.Equal(got, []string{"Airbag", "One More Time"}) {
		t.Errorf("plays in 30 days: %v", got)
	}
}

// Orden por campo (texto, número, fecha), descendente, límite, y al azar
// estable en el día pero distinto otro día.
func TestSortLimit(t *testing.T) {
	if got := eval(Def{Sort: Sort{Field: "title"}}); !slices.Equal(got, []string{"Airbag", "One More Time", "Paranoid Android", "Remote Tune", "Song"}) {
		t.Errorf("by title: %v", got)
	}
	if got := eval(Def{Sort: Sort{Field: "year", Desc: true}, Limit: 2}); !slices.Equal(got, []string{"Remote Tune", "Song"}) {
		t.Errorf("by year desc, 2: %v", got)
	}
	if got := eval(Def{Sort: Sort{Field: "added", Desc: true}, Limit: 1}); !slices.Equal(got, []string{"Song"}) {
		t.Errorf("by added: %v", got)
	}
	r := Def{Name: "r", Sort: Sort{Field: "random"}}
	a := Evaluate(r, lib, hist, marks, now)
	b := Evaluate(r, lib, hist, marks, now.Add(3*time.Hour))
	if !slices.Equal(a, b) {
		t.Errorf("random changed within the day")
	}
	differs := false
	for d := 1; d <= 10 && !differs; d++ {
		differs = !slices.Equal(a, Evaluate(r, lib, hist, marks, now.AddDate(0, 0, d)))
	}
	if !differs {
		t.Errorf("random is the same on ten other days")
	}
}

// Las listas hechas dan lo esperado.
func TestPresets(t *testing.T) {
	want := map[string][]string{
		"Most played (30 days)": {"Airbag", "One More Time"},
		"Recently added":        {"Song", "One More Time"},
		"Liked":                 {"Paranoid Android"},
		"Top rated":             {"Paranoid Android"},
		"Forgotten favorites":   {"Paranoid Android"},
	}
	for _, d := range Presets() {
		if err := Validate(d); err != nil {
			t.Errorf("%s: %v", d.Name, err)
		}
		got := eval(d)
		switch d.Name {
		case "Never played":
			slices.Sort(got)
			if !slices.Equal(got, []string{"Remote Tune", "Song"}) {
				t.Errorf("never played: %v", got)
			}
		case "Random mix (today)":
			if len(got) != len(lib) {
				t.Errorf("random mix: %v", got)
			}
		default:
			if w, ok := want[d.Name]; !ok || !slices.Equal(got, w) {
				t.Errorf("%s: %v, want %v", d.Name, got, w)
			}
		}
	}
}

// Validate rechaza campos, operaciones y valores que no se entienden.
func TestValidate(t *testing.T) {
	for _, d := range []Def{
		one("nope", "is", "x"),
		one("year", "contains", "19"),
		one("year", "gt", "abc"),
		one("added", "inlast", "soon"),
		one("added", "gt", "20/10/2026"),
		{Match: "some"},
		{Sort: Sort{Field: "nope"}},
		{Limit: -1},
	} {
		if Validate(d) == nil {
			t.Errorf("accepted %+v", d)
		}
	}
	for _, f := range Fields() {
		if len(f.Ops) == 0 {
			t.Errorf("%s has no ops", f.Name)
		}
	}
}

// Load sin archivo da los presets; Save y Load devuelven lo mismo.
func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	defs, err := Load(dir)
	if err != nil || len(defs) != len(Presets()) {
		t.Fatalf("load empty: %v %d", err, len(defs))
	}
	mine := []Def{{Name: "Rock 90s", Rules: []Rule{{Field: "genre", Op: "is", Value: "Rock"}, {Field: "year", Op: "between", Value: "1990", Value2: "1999"}}, Live: true}}
	if err := Save(dir, mine); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || len(got) != 1 || got[0].Name != "Rock 90s" || len(got[0].Rules) != 2 || got[0].Rules[1].Value2 != "1999" {
		t.Fatalf("roundtrip: %v %+v", err, got)
	}
}

// Una biblioteca de 50 000 canciones con 100 000 escuchas.
func bigLibrary() ([]Track, []history.Listen) {
	tracks := make([]Track, 50000)
	for i := range tracks {
		tracks[i] = Track{Path: fmt.Sprintf("/m/artist%d/album%d/%d.flac", i%800, i%4000, i), Title: fmt.Sprintf("Song %d", i),
			Artist: fmt.Sprintf("Artist %d", i%800), Album: fmt.Sprintf("Album %d", i%4000), Genre: []string{"Rock", "Pop", "Jazz"}[i%3],
			Year: 1960 + i%60, Duration: float64(120 + i%300), Added: daysAgo(i % 900)}
	}
	ls := make([]history.Listen, 100000)
	for i := range ls {
		ls[i] = history.Listen{At: daysAgo(i % 365).Unix(), Path: tracks[(i*7919)%len(tracks)].Path}
	}
	return tracks, ls
}

// BenchmarkEvaluate mide una lista con varias reglas y orden sobre 50 000
// canciones.
func BenchmarkEvaluate(b *testing.B) {
	tracks, ls := bigLibrary()
	d := Def{Name: "b", Rules: []Rule{{Field: "genre", Op: "is", Value: "rock"}, {Field: "artist", Op: "contains", Value: "1"},
		{Field: "lastplayed", Op: "notinlast", Value: "30"}}, Sort: Sort{Field: "plays", Desc: true}, Limit: 200}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Evaluate(d, tracks, ls, fakeMarks{}, now)
	}
}
