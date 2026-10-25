package mpd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePlayer es un motor de mentira: una cola con ids, estado y opciones.
type fakePlayer struct {
	mu      sync.Mutex
	paths   []string
	ids     []int
	nextID  int
	st      Status
	tags    map[string]Tags
	seeked  float64
	changed func()
}

func newFake() *fakePlayer {
	return &fakePlayer{st: Status{State: "stop", Pos: -1, Volume: 50}, tags: map[string]Tags{}}
}

func (f *fakePlayer) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.st
	if st.Pos >= 0 && st.Pos < len(f.paths) {
		st.Path = f.paths[st.Pos]
	}
	return st
}
func (f *fakePlayer) Queue() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.paths)
}
func (f *fakePlayer) QueueIDs() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}
func (f *fakePlayer) Load(paths []string, start int, play bool) {
	f.mu.Lock()
	f.paths, f.ids = nil, nil
	f.mu.Unlock()
	f.Append(paths)
	f.Jump(start)
	if !play {
		f.Pause()
	}
}
func (f *fakePlayer) Append(paths []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range paths {
		f.paths = append(f.paths, p)
		f.ids = append(f.ids, f.nextID)
		f.nextID++
	}
	if f.st.Pos < 0 && len(f.paths) > 0 {
		f.st.Pos = 0
	}
	f.st.Version++
}
func (f *fakePlayer) set(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
}
func (f *fakePlayer) Play()  { f.set(func() { f.st.State = "play" }) }
func (f *fakePlayer) Pause() { f.set(func() { f.st.State = "pause" }) }
func (f *fakePlayer) Stop()  { f.set(func() { f.st.State = "stop" }) }
func (f *fakePlayer) Next() {
	f.set(func() { f.st.Pos = min(f.st.Pos+1, len(f.paths)-1) })
}
func (f *fakePlayer) Prev()        { f.set(func() { f.st.Pos = max(f.st.Pos-1, 0) }) }
func (f *fakePlayer) Jump(pos int) { f.set(func() { f.st.Pos, f.st.State = pos, "play" }) }
func (f *fakePlayer) SeekTo(s float64) {
	f.set(func() { f.seeked, f.st.Elapsed = s, s })
}
func (f *fakePlayer) SetVolume(v int)   { f.set(func() { f.st.Volume = v }) }
func (f *fakePlayer) SetRandom(on bool) { f.set(func() { f.st.Random = on; f.st.Version++ }) }
func (f *fakePlayer) SetRepeat(r, s bool) {
	f.set(func() { f.st.Repeat, f.st.Single = r, s })
}
func (f *fakePlayer) Remove(pos int) {
	f.set(func() {
		f.paths = slices.Delete(f.paths, pos, pos+1)
		f.ids = slices.Delete(f.ids, pos, pos+1)
		if pos < f.st.Pos || f.st.Pos >= len(f.paths) {
			f.st.Pos--
		}
		f.st.Version++
	})
}
func (f *fakePlayer) Move(from, to int) {
	f.set(func() {
		p, id := f.paths[from], f.ids[from]
		f.paths = slices.Insert(slices.Delete(f.paths, from, from+1), to, p)
		f.ids = slices.Insert(slices.Delete(f.ids, from, from+1), to, id)
		f.st.Version++
	})
}
func (f *fakePlayer) Clear() {
	f.set(func() { f.paths, f.ids, f.st.Pos, f.st.State = nil, nil, -1, "stop"; f.st.Version++ })
}
func (f *fakePlayer) Tags(p string) (Tags, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tags[p]
	return t, ok
}

// testLibrary escribe un data/library.json con dos álbumes de dos artistas
// (las canciones no tienen que existir para listarlas) y una portada.
func testLibrary(t *testing.T) (data, music string) {
	t.Helper()
	root := t.TempDir()
	data, music = filepath.Join(root, "data"), filepath.Join(root, "Music")
	os.MkdirAll(filepath.Join(music, "Band", "First"), 0o755)
	os.MkdirAll(data, 0o755)
	os.WriteFile(filepath.Join(music, "Band", "First", "cover.jpg"), []byte("\xff\xd8\xff"+strings.Repeat("x", 300)), 0o644)
	p := func(parts ...string) string { return filepath.Join(append([]string{music}, parts...)...) }
	c := map[string]any{
		"dirs": []string{music},
		"entries": []cacheEntry{
			{Path: p("Band", "First", "01.mp3"), Title: "Uno", Artist: "Band", Album: "First", Track: 1, Year: 2001, Genre: "Rock"},
			{Path: p("Band", "First", "02.mp3"), Title: "Dos", Artist: "Band", Album: "First", Track: 2, Year: 2001, Genre: "Rock"},
			{Path: p("Other", "x.flac"), Title: "Equis", Artist: "Other", AlbumArtist: "Various", Album: "Mix"},
			{Path: p("solo.wav"), Title: "Solo", Artist: "band"},
		},
	}
	b, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(data, "library.json"), b, 0o644)
	return data, music
}

// start arranca un servidor con el motor falso y le conecta un cliente.
func start(t *testing.T, password string) (*Server, *fakePlayer, *Client, string) {
	t.Helper()
	data, music := testLibrary(t)
	f := newFake()
	s, err := Start(Options{Addr: "127.0.0.1:0", Password: password, DataDir: data, Player: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c, err := Dial(s.Addr().String(), password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return s, f, c, music
}

func must(t *testing.T, c *Client, args ...string) []Pair {
	t.Helper()
	p, err := c.Cmd(args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return p
}

// Las canciones se añaden por URI de la biblioteca (una o un directorio
// entero) y la cola se ve, se mueve, se toca y se borra como en MPD.
func TestQueue(t *testing.T) {
	_, f, c, music := start(t, "")
	must(t, c, "add", "Band")
	id := Map(must(t, c, "addid", "solo.wav", "0"))["Id"]
	q := Songs(must(t, c, "playlistinfo"))
	if len(q) != 3 || q[0].File != "solo.wav" || q[1].File != "Band/First/01.mp3" || fmt.Sprint(q[0].ID) != id {
		t.Fatalf("queue: %+v (id %s)", q, id)
	}
	if q[1].Tags.Title != "Uno" || q[1].Tags.Track != "1" || q[1].Tags.Date != "2001" {
		t.Errorf("tags: %+v", q[1].Tags)
	}
	if f.Queue()[0] != filepath.Join(music, "solo.wav") {
		t.Errorf("engine path: %v", f.Queue())
	}
	must(t, c, "play", "1")
	st := Map(must(t, c, "status"))
	if st["state"] != "play" || st["song"] != "1" || st["playlistlength"] != "3" || st["nextsong"] != "2" {
		t.Errorf("status: %v", st)
	}
	if cur := Songs(must(t, c, "currentsong")); len(cur) != 1 || cur[0].Tags.Title != "Uno" {
		t.Errorf("currentsong: %+v", cur)
	}
	must(t, c, "seekcur", "42.5")
	must(t, c, "pause", "1")
	must(t, c, "setvol", "80")
	must(t, c, "repeat", "1")
	must(t, c, "single", "1")
	st = Map(must(t, c, "status"))
	if f.seeked != 42.5 || st["state"] != "pause" || st["volume"] != "80" || st["repeat"] != "1" || st["single"] != "1" {
		t.Errorf("status after changes: %v (seek %v)", st, f.seeked)
	}
	must(t, c, "move", "0", "2")
	if q := Songs(must(t, c, "playlistinfo")); q[2].File != "solo.wav" {
		t.Errorf("after move: %+v", q)
	}
	must(t, c, "deleteid", id)
	must(t, c, "delete", "0:1")
	if q := Songs(must(t, c, "playlistinfo")); len(q) != 1 || q[0].File != "Band/First/02.mp3" {
		t.Errorf("after delete: %+v", q)
	}
	if _, err := c.Cmd("add", "nope"); !IsNoExist(err) {
		t.Errorf("adding a missing URI: %v", err)
	}
	must(t, c, "clear")
	if q := must(t, c, "playlistinfo"); len(q) != 0 {
		t.Errorf("after clear: %v", q)
	}
}

// La biblioteca se recorre por directorios y se busca y agrupa por
// etiquetas, con la sintaxis vieja y con expresiones.
func TestLibrary(t *testing.T) {
	_, _, c, _ := start(t, "")
	ls := must(t, c, "lsinfo")
	var dirs, files []string
	for _, p := range ls {
		switch p.Key {
		case "directory":
			dirs = append(dirs, p.Value)
		case "file":
			files = append(files, p.Value)
		}
	}
	if fmt.Sprint(dirs) != "[Band Other]" || fmt.Sprint(files) != "[solo.wav]" {
		t.Errorf("lsinfo: dirs %v files %v", dirs, files)
	}
	if s := Songs(must(t, c, "lsinfo", "Band/First")); len(s) != 2 {
		t.Errorf("lsinfo Band/First: %+v", s)
	}
	if all := must(t, c, "listall"); len(Songs(all)) != 4 {
		t.Errorf("listall: %v", all)
	}
	if s := Songs(must(t, c, "find", "artist", "Band")); len(s) != 2 {
		t.Errorf("find artist Band (case-sensitive): %+v", s)
	}
	if s := Songs(must(t, c, "search", "artist", "band")); len(s) != 3 {
		t.Errorf("search artist band: %+v", s)
	}
	if s := Songs(must(t, c, "find", "((Album == 'First') AND (Track == '2'))")); len(s) != 1 || s[0].Tags.Title != "Dos" {
		t.Errorf("find expression: %+v", s)
	}
	if s := Songs(must(t, c, "find", "(!(Artist == 'Band'))")); len(s) != 2 {
		t.Errorf("find not: %+v", s)
	}
	if s := Songs(must(t, c, "find", "(base 'Band')", "sort", "-Track", "window", "0:1")); len(s) != 1 || s[0].Tags.Title != "Dos" {
		t.Errorf("find base sort window: %+v", s)
	}
	got := must(t, c, "list", "album", "group", "albumartist")
	want := []Pair{{"AlbumArtist", "Band"}, {"Album", "First"}, {"AlbumArtist", "Various"}, {"Album", "Mix"}}
	if !slices.Equal(got, want) {
		t.Errorf("list album group albumartist:\n got %v\nwant %v", got, want)
	}
	if got := must(t, c, "list", "album", "Band"); !slices.Equal(got, []Pair{{"Album", "First"}}) {
		t.Errorf("list album <artist>: %v", got)
	}
	if m := Map(must(t, c, "count", "genre", "Rock")); m["songs"] != "2" {
		t.Errorf("count: %v", m)
	}
	if m := Map(must(t, c, "stats")); m["songs"] != "4" || m["artists"] != "3" {
		t.Errorf("stats: %v", m)
	}
	must(t, c, "findadd", "album", "First")
	if q := Songs(must(t, c, "playlistinfo")); len(q) != 2 {
		t.Errorf("findadd: %+v", q)
	}
}

// La portada suelta de la carpeta se manda a trozos (binarylimit).
func TestAlbumArt(t *testing.T) {
	_, _, c, music := start(t, "")
	want, _ := os.ReadFile(filepath.Join(music, "Band", "First", "cover.jpg"))
	must(t, c, "binarylimit", "128")
	got, err := c.Picture("Band/First/01.mp3")
	if err != nil || string(got) != string(want) {
		t.Fatalf("albumart: %d bytes, %v (want %d)", len(got), err, len(want))
	}
	if got, err := c.Picture("solo.wav"); err != nil || got != nil {
		t.Errorf("no cover: %d bytes, %v", len(got), err)
	}
}

// Con contraseña, solo se puede hacer algo después de darla.
func TestPassword(t *testing.T) {
	s, _, _, _ := start(t, "secret")
	c, err := Dial(s.Addr().String(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Cmd("status"); err == nil || err.(*Error).Code != ackPermission {
		t.Errorf("status without password: %v", err)
	}
	if _, err := c.Cmd("password", "wrong"); err == nil {
		t.Error("a wrong password was accepted")
	}
	must(t, c, "password", "secret")
	must(t, c, "status")
}

// idle espera hasta que algo cambia y dice qué; noidle lo corta.
func TestIdle(t *testing.T) {
	s, f, _, _ := start(t, "")
	nc, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	r := bufio.NewReader(nc)
	r.ReadString('\n')
	fmt.Fprintf(nc, "idle player mixer\n")
	time.Sleep(100 * time.Millisecond)
	f.SetVolume(10)
	s.Changed()
	nc.SetReadDeadline(time.Now().Add(3 * time.Second))
	l1, _ := r.ReadString('\n')
	l2, _ := r.ReadString('\n')
	if l1 != "changed: mixer\n" || l2 != "OK\n" {
		t.Errorf("idle: %q %q", l1, l2)
	}
	fmt.Fprintf(nc, "idle\n")
	time.Sleep(100 * time.Millisecond)
	fmt.Fprintf(nc, "noidle\n")
	if l, _ := r.ReadString('\n'); l != "OK\n" {
		t.Errorf("noidle: %q", l)
	}
}

// Las listas de comandos van todas juntas; con ok_begin, cada una dice
// list_OK; un error para la lista y dice en cuál.
func TestCommandList(t *testing.T) {
	s, _, _, _ := start(t, "")
	nc, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	r := bufio.NewReader(nc)
	r.ReadString('\n')
	fmt.Fprint(nc, "command_list_ok_begin\nadd solo.wav\nsetvol 70\ncommand_list_end\n")
	var got []string
	for i := 0; i < 3; i++ {
		l, _ := r.ReadString('\n')
		got = append(got, strings.TrimSpace(l))
	}
	if fmt.Sprint(got) != "[list_OK list_OK OK]" {
		t.Errorf("ok list: %v", got)
	}
	fmt.Fprint(nc, "command_list_begin\nsetvol 20\nadd \"no such\"\nsetvol 30\ncommand_list_end\n")
	if l, _ := r.ReadString('\n'); !strings.HasPrefix(l, "ACK [50@1] {add}") {
		t.Errorf("failing list: %q", l)
	}
}

// Las listas guardadas se crean desde la cola, se editan, se cargan y se
// borran; van a data/playlists.
func TestStoredPlaylists(t *testing.T) {
	_, _, c, _ := start(t, "")
	must(t, c, "add", "Band")
	must(t, c, "save", "Mine")
	if _, err := c.Cmd("save", "Mine"); err == nil {
		t.Error("saving over an existing list worked")
	}
	must(t, c, "playlistadd", "Mine", "solo.wav")
	must(t, c, "playlistdelete", "Mine", "0")
	got := must(t, c, "listplaylist", "Mine")
	if !slices.Equal(got, []Pair{{"file", "Band/First/02.mp3"}, {"file", "solo.wav"}}) {
		t.Errorf("listplaylist: %v", got)
	}
	if m := Map(must(t, c, "listplaylists")); m["playlist"] != "Mine" {
		t.Errorf("listplaylists: %v", m)
	}
	must(t, c, "clear")
	must(t, c, "load", "Mine")
	if q := Songs(must(t, c, "playlistinfo")); len(q) != 2 || q[1].File != "solo.wav" {
		t.Errorf("load: %+v", q)
	}
	must(t, c, "rename", "Mine", "Ours")
	must(t, c, "rm", "Ours")
	if l := must(t, c, "listplaylists"); len(l) != 0 {
		t.Errorf("after rm: %v", l)
	}
}

// Una URL se pone en la cola con una ruta propia; sus etiquetas son las de
// addtagid, y la ve el motor (Server.Tags).
func TestAddURL(t *testing.T) {
	s, f, c, _ := start(t, "")
	id := Map(must(t, c, "addid", "http://example.invalid/music/song.mp3"))["Id"]
	must(t, c, "addtagid", id, "title", "Remote")
	must(t, c, "addtagid", id, "artist", "Someone")
	q := Songs(must(t, c, "playlistinfo"))
	if len(q) != 1 || q[0].File != "http://example.invalid/music/song.mp3" || q[0].Tags.Title != "Remote" || q[0].Tags.Artist != "Someone" {
		t.Errorf("playlistinfo: %+v", q)
	}
	p := f.Queue()[0]
	if !IsPath(p) {
		t.Fatalf("engine path %q", p)
	}
	if tg, ok := s.Tags(p); !ok || tg.Title != "Remote" {
		t.Errorf("Server.Tags: %+v %v", tg, ok)
	}
	if _, err := c.Cmd("addtagid", Map(must(t, c, "addid", "solo.wav"))["Id"], "title", "x"); err == nil {
		t.Error("addtagid on a library song worked")
	}
}

// La etiqueta propia AudiofilusCover pone la URL de la portada de una
// canción por URL: Server.Cover la baja (y no es una etiqueta más).
func TestAddURLCover(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(png) }))
	defer srv.Close()
	s, f, c, _ := start(t, "")
	id := Map(must(t, c, "addid", srv.URL+"/song.mp3"))["Id"]
	must(t, c, "addtagid", id, "audiofiluscover", srv.URL+"/cover.png")
	p := f.Queue()[0]
	var cover string
	for i := 0; i < 200 && cover == ""; i++ {
		cover = s.Cover(p)
		time.Sleep(10 * time.Millisecond)
	}
	if b, err := os.ReadFile(cover); err != nil || !bytes.Equal(b, png) {
		t.Errorf("cover %q: %q %v", cover, b, err)
	}
	if tg, _ := s.Tags(p); tg != (Tags{Title: "song"}) {
		t.Errorf("the cover became a tag: %+v", tg)
	}
}

// splitArgs separa palabras y comillas con escapes.
func TestSplitArgs(t *testing.T) {
	got, err := splitArgs(`find "(Artist == \"AC/DC\")" sort  Title`)
	if err != nil || !slices.Equal(got, []string{`find`, `(Artist == "AC/DC")`, "sort", "Title"}) {
		t.Errorf("%q %v", got, err)
	}
	if _, err := splitArgs(`add "unterminated`); err == nil {
		t.Error("unterminated quote accepted")
	}
}
