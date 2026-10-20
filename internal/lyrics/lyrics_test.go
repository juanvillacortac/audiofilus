package lyrics

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestParseLRC comprueba las marcas de tiempo (con dos y tres cifras, varias
// por línea), las etiquetas, el offset y las marcas por palabra.
func TestParseLRC(t *testing.T) {
	l := Parse("\ufeff[ar:Someone]\n[offset:+500]\n[00:10.50]Hello\n[00:05.1][01:00.123]Twice <00:05.30>word\n\n[00:20]\n")
	if !l.Synced {
		t.Fatal("LRC not detected as synced")
	}
	want := []Line{
		{4600 * time.Millisecond, "Twice word"},
		{10 * time.Second, "Hello"},
		{19500 * time.Millisecond, ""},
		{59623 * time.Millisecond, "Twice word"},
	}
	if len(l.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(l.Lines), len(want), l.Lines)
	}
	for i, w := range want {
		if l.Lines[i] != w {
			t.Errorf("line %d = %+v, want %+v", i, l.Lines[i], w)
		}
	}
	for pos, idx := range map[time.Duration]int{0: -1, 4600 * time.Millisecond: 0, 15 * time.Second: 1, time.Hour: 3} {
		if got := l.Index(pos); got != idx {
			t.Errorf("Index(%v) = %d, want %d", pos, got, idx)
		}
	}
}

// TestParsePlain comprueba que un texto sin marcas quede como letra sin
// sincronizar, sin blancos de sobra.
func TestParsePlain(t *testing.T) {
	l := Parse("\n\nFirst\r\nSecond\n\n\n\nThird\n\n")
	if l.Synced {
		t.Fatal("plain text detected as synced")
	}
	var got []string
	for _, ln := range l.Lines {
		got = append(got, ln.Text)
	}
	if strings.Join(got, "|") != "First|Second||Third" {
		t.Errorf("lines = %q", got)
	}
	if l.Index(time.Minute) != -1 {
		t.Error("Index on unsynced lyrics should be -1")
	}
	if !Parse(" \n\n").Empty() {
		t.Error("blank text should be empty")
	}
}

// TestFormatRoundTrip comprueba que una letra guardada como LRC se lea igual.
func TestFormatRoundTrip(t *testing.T) {
	l := Parse("[00:01.00]One\n[01:02.34]Two\n")
	if got := Parse(Format(l)); len(got.Lines) != 2 || got.Lines[1] != l.Lines[1] {
		t.Errorf("round trip = %+v, want %+v", got.Lines, l.Lines)
	}
}

// sylt arma un frame SYLT con tiempos en milisegundos y textos en UTF-8.
func sylt(entries ...any) []byte {
	b := []byte{3, 'e', 'n', 'g', 2, 1, 0} // UTF-8, idioma, ms, letra, descriptor vacío
	for i := 0; i < len(entries); i += 2 {
		b = append(b, entries[i].(string)...)
		b = append(b, 0)
		b = binary.BigEndian.AppendUint32(b, uint32(entries[i+1].(int)))
	}
	return b
}

// TestParseSYLT comprueba las dos formas de SYLT: una línea por entrada, y
// una palabra por entrada con "\n" al empezar cada línea.
func TestParseSYLT(t *testing.T) {
	l, err := ParseSYLT(sylt("First line", 1000, "Second line", 2500))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Lines) != 2 || l.Lines[1] != (Line{2500 * time.Millisecond, "Second line"}) {
		t.Errorf("per-line SYLT = %+v", l.Lines)
	}
	l, err = ParseSYLT(sylt("\nHello ", 1000, "world", 1400, "\nBye", 3000))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Lines) != 2 || l.Lines[0].Text != "Hello world" || l.Lines[1].At != 3*time.Second {
		t.Errorf("per-word SYLT = %+v", l.Lines)
	}
	if _, err := ParseSYLT([]byte{3, 'e', 'n', 'g', 1, 1, 0}); err == nil {
		t.Error("SYLT with MPEG-frame timestamps should be rejected")
	}
}

// fakeLRCLIB es un LRCLIB de prueba: responde a las búsquedas con results
// (por "track_name" o "q") y cuenta las peticiones.
func fakeLRCLIB(t *testing.T, results map[string][]Found) (*LRCLIB, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.Header.Get("User-Agent"), "audiofilus") {
			t.Errorf("request without the audiofilus User-Agent")
		}
		key := r.URL.Query().Get("track_name")
		if key == "" {
			key = "q:" + r.URL.Query().Get("q")
		}
		_ = json.NewEncoder(w).Encode(orEmpty(results[key]))
	}))
	t.Cleanup(srv.Close)
	return &LRCLIB{Base: srv.URL}, &calls
}

func orEmpty(f []Found) []Found {
	if f == nil {
		return []Found{}
	}
	return f
}

// TestSearchCleansTitleAndRanks comprueba que, sin resultados con el título
// tal cual, se busque sin "(feat. ...)", y que gane la sincronizada con la
// duración de la canción.
func TestSearchCleansTitleAndRanks(t *testing.T) {
	c, _ := fakeLRCLIB(t, map[string][]Found{
		"Song": {
			{ID: 1, Track: "Song", Artist: "Band", Duration: 300, Synced: "[00:01.00]long"},
			{ID: 2, Track: "Song", Artist: "Band", Duration: 181, Plain: "plain"},
			{ID: 3, Track: "Song", Artist: "Band", Duration: 180, Synced: "[00:01.00]right"},
		},
	})
	q := Query{Title: "Song (feat. Guest)", Artist: "Band", Duration: 180}
	found, err := c.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || found[0].ID != 3 {
		t.Fatalf("ranking = %+v, want ID 3 first", found)
	}
	if b, ok := Best(found, q); !ok || b.ID != 3 {
		t.Errorf("Best = %+v, %v; want ID 3", b, ok)
	}
	if _, ok := Best([]Found{{ID: 1, Duration: 300, Synced: "[00:01.00]x"}}, q); ok {
		t.Error("Best accepted a result whose duration is far off")
	}
	if got := CleanTitle("Song - 2011 Remaster"); got != "Song" {
		t.Errorf("CleanTitle = %q", got)
	}
}

// TestLocalFiles comprueba los archivos de letra: el .lrc con el mismo
// nombre (que va primero), el de la carpeta lyrics y otro .lrc de la carpeta.
func TestLocalFiles(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "My Song.mp3")
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(song, "not really an mp3")
	write(filepath.Join(dir, "lyrics", "My Song.txt"), "plain words")
	write(filepath.Join(dir, "My Song.lrc"), "[00:01.00]synced")
	write(filepath.Join(dir, "01 - my song (live).lrc"), "[00:02.00]other")
	opts := Local(song)
	var ids []string
	for _, o := range opts {
		ids = append(ids, filepath.Base(o.ID))
	}
	want := "My Song.lrc|My Song.txt|01 - my song (live).lrc"
	if strings.Join(ids, "|") != want {
		t.Errorf("local options = %q, want %q", ids, want)
	}
	if !opts[0].Synced() || opts[1].Synced() {
		t.Error("synced flags are wrong")
	}
	if Local("subsonic://server/1") != nil {
		t.Error("remote songs have no local lyrics")
	}
}

// TestFinder comprueba el orden al elegir solo: el .lrc gana a internet; sin
// él, la de internet se guarda y la segunda vez no se pide; elegir "none"
// apaga la letra; y una búsqueda sin nada no se repite enseguida.
func TestFinder(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "Song.flac")
	if err := os.WriteFile(song, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	web, calls := fakeLRCLIB(t, map[string][]Found{
		"Song": {{ID: 7, Track: "Song", Artist: "Band", Duration: 200, Synced: "[00:03.00]from the web"}},
	})
	fd := &Finder{Store: NewStore(filepath.Join(dir, "data")), Web: web}
	q := Query{Title: "Song", Artist: "Band", Duration: 200}
	ctx := context.Background()

	o := fd.Find(ctx, song, q)
	if o.ID != "lrclib:7" || !o.Synced() {
		t.Fatalf("first Find = %q, want lrclib:7", o.ID)
	}
	n := calls.Load()
	if o = fd.Find(ctx, song, q); o.ID != "lrclib:7" || calls.Load() != n {
		t.Errorf("second Find = %q with %d new requests; want the cached one", o.ID, calls.Load()-n)
	}

	lrc := filepath.Join(dir, "Song.lrc")
	if err := os.WriteFile(lrc, []byte("[00:01.00]local"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o = fd.Find(ctx, song, q); o.ID != "file:"+lrc {
		t.Errorf("with a .lrc, Find = %q; the local file should win over the automatic one", o.ID)
	}
	// Elegida a mano, gana la de internet aunque haya .lrc.
	if err := fd.Store.Update(song, func(c *Choice) { c.Source, c.Auto = "lrclib:7", false }); err != nil {
		t.Fatal(err)
	}
	if o = fd.Find(ctx, song, q); o.ID != "lrclib:7" {
		t.Errorf("chosen by hand, Find = %q", o.ID)
	}
	_ = fd.Store.Update(song, func(c *Choice) { c.Source = "none" })
	if o = fd.Find(ctx, song, q); o.ID != "none" {
		t.Errorf("with none chosen, Find = %q", o.ID)
	}

	missing := filepath.Join(dir, "Missing.flac")
	_ = os.WriteFile(missing, []byte("x"), 0o644)
	mq := Query{Title: "Missing", Artist: "Band"}
	if o = fd.Find(ctx, missing, mq); o.ID != "none" {
		t.Errorf("Find for an unknown song = %q", o.ID)
	}
	n = calls.Load()
	fd.Find(ctx, missing, mq)
	if calls.Load() != n {
		t.Error("a miss was looked up again right away")
	}
}

// TestEmbedded comprueba las letras de las etiquetas: en FLAC, LYRICS (un
// LRC) y UNSYNCEDLYRICS; en MP3, SYLT (que va primero) y USLT.
func TestEmbedded(t *testing.T) {
	abs := func(p string) string { a, _ := filepath.Abs(p); return a }
	opts := Embedded(abs("testdata/tags.flac"))
	if len(opts) != 2 || !opts[0].Synced() || opts[0].Lyrics.Lines[0].Text != "Tagged line" || opts[1].Lyrics.Lines[0].Text != "Plain tag" {
		t.Errorf("FLAC tags = %+v", opts)
	}
	opts = Embedded(abs("testdata/tags.mp3"))
	if len(opts) != 2 || opts[0].ID != "tag:sylt" || opts[0].Lyrics.Lines[1] != (Line{2500 * time.Millisecond, "Second line"}) ||
		opts[1].ID != "tag" || opts[1].Lyrics.Lines[0].Text != "Unsynced words" {
		t.Errorf("MP3 tags = %+v", opts)
	}
}

// TestWithBreaks comprueba los huecos instrumentales: la intro larga, un
// silencio largo entre dos líneas y no en uno corto, y que una línea vacía
// del LRC no se duplique.
func TestWithBreaks(t *testing.T) {
	l := Parse("[00:10.00]One two three\n[00:13.00]Short gap\n[00:30.00]After a long gap\n[00:32.00]\n[00:45.00]Back\n").WithBreaks()
	var got []string
	for _, ln := range l.Lines {
		got = append(got, fmt.Sprintf("%v %s", ln.At, ln.Text))
	}
	want := []string{"0s ", "10s One two three", "13s Short gap", "15s ", "30s After a long gap", "32s ", "45s Back"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("WithBreaks = %q\nwant %q", got, want)
	}
	if p := Parse("plain").WithBreaks(); len(p.Lines) != 1 {
		t.Error("unsynced lyrics should be left alone")
	}
}

// TestInstrumental comprueba que una instrumental de LRCLIB (con su
// duración) salga como tal cuando no hay letra, y que no se vea vacía.
func TestInstrumental(t *testing.T) {
	q := Query{Title: "Intro", Duration: 90}
	f, ok := Best([]Found{{ID: 1, Duration: 300, Instrumental: true}, {ID: 2, Duration: 91, Instrumental: true}}, q)
	if !ok || f.ID != 2 {
		t.Fatalf("Best = %+v, %v; want the instrumental with the right duration", f, ok)
	}
	o, ok := OptionOf(f)
	if !ok || !o.Lyrics.Instrumental || o.Lyrics.Empty() {
		t.Errorf("OptionOf(instrumental) = %+v, %v", o, ok)
	}
	if f, _ := Best([]Found{{ID: 3, Duration: 91, Instrumental: true}, {ID: 4, Duration: 90, Synced: "[00:01.00]x"}}, q); f.ID != 4 {
		t.Errorf("lyrics should win over an instrumental entry, got %d", f.ID)
	}
}
