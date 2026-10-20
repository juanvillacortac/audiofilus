package modsrc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture lee una página guardada de testdata (trozos recortados de las
// páginas reales).
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func init() { politeGap = 0 } // en los tests no hace falta esperar

// La búsqueda de The Mod Archive da el id, el archivo, el formato y el
// título de cada módulo.
func TestParseModArchiveSearch(t *testing.T) {
	items := parseModArchive(fixture(t, "modarchive_search.html"), "")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	it := items[0]
	if it.ID != "137688" || it.Format != "XM" || it.Title != "space debris" || it.Source != "modarchive" {
		t.Errorf("first item = %+v", it)
	}
	if !strings.Contains(it.URL, "moduleid=137688") {
		t.Errorf("url = %q", it.URL)
	}
}

// La lista de un artista lleva su nombre en cada módulo y el título de cada
// uno.
func TestParseModArchiveArtist(t *testing.T) {
	items := parseModArchive(fixture(t, "modarchive_artist.html"), "Purple Motion")
	if len(items) < 2 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].ID != "214141" || items[0].Title != "Puzzix" || items[0].Format != "IT" || items[0].Artist != "Purple Motion" {
		t.Errorf("first item = %+v", items[0])
	}
}

// La lista de éxitos (sin iconos de formato) saca el formato de la
// extensión, y no repite un módulo con varios enlaces.
func TestParseModArchiveTop(t *testing.T) {
	items := parseModArchive(fixture(t, "modarchive_top.html"), "")
	if len(items) == 0 {
		t.Fatal("no items")
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			t.Errorf("repeated %s", it.ID)
		}
		seen[it.ID] = true
		if it.Format == "" || it.Title == "" {
			t.Errorf("incomplete item %+v", it)
		}
	}
}

// La página de un módulo (la de "al azar") da uno solo, con su tamaño.
func TestParseModArchiveRandom(t *testing.T) {
	items := parseModArchive(fixture(t, "modarchive_random.html"), "")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].ID != "189861" || items[0].Format != "MOD" || items[0].Size != 178*1024 {
		t.Errorf("item = %+v", items[0])
	}
}

// Los paquetes de VGMRips traen el título, el zip, el tamaño y los
// compositores.
func TestParseVGMRips(t *testing.T) {
	items := parseVGMRips(fixture(t, "vgmrips_search.html"))
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	it := items[0]
	if it.ID != "sonic-the-hedgehog-2-sega-mega-drive" || it.Title != "Sonic the Hedgehog 2" || !it.Pack || it.Format != "VGM" {
		t.Errorf("item = %+v", it)
	}
	if !strings.HasSuffix(it.URL, ".zip") || it.Artist != "Masato Nakamura" || it.Size < 800*1024 {
		t.Errorf("item = %+v", it)
	}
	// Un paquete de chips que libgme no emula (NES APU y FDS en VGM) no se
	// ofrece: sonaría en silencio.
	if p, ok := parseVGMRipsPack(fixture(t, "vgmrips_pack.html"), "https://vgmrips.net/packs/pack/famicom-grand-prix-ii"); ok {
		t.Errorf("unplayable pack offered: %+v", p)
	}
	// Con los chips de la Mega Drive, sí.
	page := strings.ReplaceAll(strings.ReplaceAll(fixture(t, "vgmrips_pack.html"), "chip/fds", "chip/ym2612"), "chip/nes-apu", "chip/sn76489")
	p, ok := parseVGMRipsPack(page, "https://vgmrips.net/packs/pack/famicom-grand-prix-ii")
	if !ok || p.Title != "Famicom Grand Prix II: 3D Hot Rally" || !strings.HasSuffix(p.URL, ".zip") || !strings.Contains(p.Artist, "Soyo Oka") {
		t.Errorf("pack = %+v (ok %v)", p, ok)
	}
}

// Las tablas de AMP dan el título, el autor, el formato y el tamaño, sea
// cual sea la columna en que empiezan (lo último añadido tiene antes la
// fecha); las filas que no son de módulos (el chat) se ignoran.
func TestParseAMP(t *testing.T) {
	items := parseAMP(fixture(t, "amp_search.html"))
	if len(items) == 0 {
		t.Fatal("no items")
	}
	if it := items[0]; it.ID != "88786" || it.Title != "sPACEMAn" || it.Artist != "Buzz" || it.Format != "XM" || it.Size != 633*1024 {
		t.Errorf("search item = %+v", it)
	}
	c := parseAMP(fixture(t, "amp_composer.html"))
	if len(c) != 2 || c[0].Title != "* kamasutra *" || c[0].Format != "MOD" || c[1].Format != "MED" {
		t.Errorf("composer items = %+v", c)
	}
	a := parseAMP(fixture(t, "amp_added.html"))
	if len(a) == 0 || a[0].Format != "XM" || a[0].Artist == "" {
		t.Errorf("added items = %+v", a)
	}
}

// Playable sigue a los decodificadores: los módulos y la música de consolas
// sí, el SID y los formatos raros de Amiga no.
func TestPlayable(t *testing.T) {
	for f, want := range map[string]bool{"MOD": true, "xm": true, "IT": true, "SPC": true, "VGZ": true, "FST": true, "SID": false, "AHX": false, "DBM": false} {
		if got := Playable(f); got != want {
			t.Errorf("Playable(%q) = %v, want %v", f, got, want)
		}
	}
}

// zipOf arma un zip en memoria.
func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(data))
	}
	w.Close()
	return buf.Bytes()
}

// gz comprime data.
func gz(data string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write([]byte(data))
	w.Close()
	return buf.Bytes()
}

// server sirve las páginas y archivos de prueba de los cuatro archivos y
// cambia sus direcciones por las suyas.
func server(t *testing.T) *httptest.Server {
	t.Helper()
	index := zipOf(t, map[string]string{"allmods.txt": strings.Join([]string{
		"1000\tProtracker/4-Mat/madness.mod",
		"2000\tProtracker/Purple Motion/sat 1.mod",
		"3000\tFasttracker 2/Purple Motion/second reality.xm",
		"4000\tHVSC/Rob Hubbard/commando.sid",
		"5000\tVideo Game Music/Sega Mega Drive/Yuzo Koshiro/Streets of Rage/sor - 01.vgz",
		"6000\tAd Lib/Screamtracker 3 AdLib/Purple Motion/starport.s3m",
	}, "\n") + "\n"})
	pack := zipOf(t, map[string]string{"02 Stage.vgz": "two", "01 Title.vgz": "one", "../../evil.vgz": "x", "readme.txt": "no"})
	mux := http.NewServeMux()
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fixture(t, name)) }
	}
	mux.HandleFunc("/index.php", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("search_type") {
		case "search_artist":
			io.WriteString(w, fixture(t, "modarchive_artists.html"))
		default:
			if r.URL.Query().Get("request") == "view_artist_modules" {
				io.WriteString(w, fixture(t, "modarchive_artist.html"))
				return
			}
			io.WriteString(w, fixture(t, "modarchive_search.html"))
		}
	})
	mux.HandleFunc("/downloads.php", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="space_debris.xm"`)
		io.WriteString(w, "fake xm")
	})
	mux.HandleFunc("/allmods.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(index) })
	mux.HandleFunc("/pub/modules/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "file "+r.URL.Path) })
	mux.HandleFunc("/packs/search", page("vgmrips_search.html"))
	mux.HandleFunc("/pack.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(pack) })
	mux.HandleFunc("/newresult.php", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("request") == "handle" {
			io.WriteString(w, fixture(t, "amp_handles.html"))
			return
		}
		io.WriteString(w, fixture(t, "amp_search.html"))
	})
	mux.HandleFunc("/detail.php", page("amp_composer.html"))
	mux.HandleFunc("/downmod.php", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("index") == "666" {
			w.Write([]byte("\x00\x00-lh5-rest of an lha archive"))
			return
		}
		w.Write(gz("fake mod"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := []string{modArchiveBase, modArchiveDL, modlandIndex, modlandFiles, vgmRipsBase, ampBase}
	modArchiveBase, modArchiveDL = srv.URL, srv.URL+"/downloads.php?moduleid="
	modlandIndex, modlandFiles = srv.URL+"/allmods.zip", srv.URL+"/pub/modules/"
	vgmRipsBase, ampBase = srv.URL, srv.URL
	t.Cleanup(func() {
		modArchiveBase, modArchiveDL, modlandIndex, modlandFiles, vgmRipsBase, ampBase = old[0], old[1], old[2], old[3], old[4], old[5]
	})
	return srv
}

// The Mod Archive: buscar, buscar por artista y bajar (con el nombre del
// Content-Disposition, y sin volver a bajarlo).
func TestModArchiveFlow(t *testing.T) {
	server(t)
	ctx := context.Background()
	ma := Sources(t.TempDir())[0]
	items, err := ma.Search(ctx, "space debris")
	if err != nil || len(items) != 2 {
		t.Fatalf("search: %v %d", err, len(items))
	}
	byArtist, err := ma.ByArtist(ctx, "Purple Motion")
	if err != nil || len(byArtist) == 0 || byArtist[0].Artist != "Purple Motion" {
		t.Fatalf("by artist: %v %+v", err, byArtist)
	}
	dir := t.TempDir()
	files, err := Fetch(ctx, items[0], dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "space_debris.xm" || filepath.Base(filepath.Dir(files[0])) != "137688" {
		t.Fatalf("files = %v", files)
	}
	again, err := Fetch(ctx, items[0], dir, nil)
	if err != nil || len(again) != 1 || again[0] != files[0] {
		t.Errorf("second fetch = %v %v", again, err)
	}
}

// Modland: el índice se baja una vez y se guarda solo lo que se puede tocar;
// la búsqueda pide todas las palabras; el autor sale de la ruta (en Video
// Game Music y Ad Lib, de su tercera parte).
func TestModlandFlow(t *testing.T) {
	server(t)
	ctx := context.Background()
	cache := t.TempDir()
	ml := Sources(cache)[1]
	items, err := ml.Search(ctx, "purple motion")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Artist != "Purple Motion" || items[0].Format != "MOD" || items[0].Title != "sat 1" || items[2].Artist != "Purple Motion" {
		t.Fatalf("items = %+v", items)
	}
	if got, _ := ml.Search(ctx, "commando"); len(got) != 0 {
		t.Errorf("the SID file wasn't filtered out: %+v", got)
	}
	vgm, _ := ml.Search(ctx, "streets")
	if len(vgm) != 1 || vgm[0].Artist != "Yuzo Koshiro" {
		t.Errorf("vgm = %+v", vgm)
	}
	art, _ := ml.ByArtist(ctx, "purple motion")
	if len(art) != 3 {
		t.Errorf("by artist = %+v", art)
	}
	rnd, err := ml.Browse(ctx, "random")
	if err != nil || len(rnd) != 5 {
		t.Errorf("random = %v %d", err, len(rnd))
	}
	if _, err := ml.Browse(ctx, "top"); err != ErrUnsupported {
		t.Errorf("top: %v", err)
	}
	files, err := Fetch(ctx, items[0], t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(files[0])
	if string(b) != "file /pub/modules/Protracker/Purple Motion/sat 1.mod" {
		t.Errorf("downloaded %q", b)
	}
}

// VGMRips: un paquete se baja y se sacan sus pistas en orden, sin lo que no
// suena y sin que nada salga de la carpeta.
func TestVGMRipsFetch(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	it := Item{Source: "vgmrips", ID: "test-pack", Pack: true, Format: "VGM", URL: srv.URL + "/pack.zip"}
	files, err := Fetch(context.Background(), it, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		if !strings.HasPrefix(f, filepath.Join(dir, "vgmrips", "test-pack")) {
			t.Errorf("%s is outside the pack folder", f)
		}
		names = append(names, filepath.Base(f))
	}
	if strings.Join(names, ",") != "01 Title.vgz,02 Stage.vgz,evil.vgz" {
		t.Errorf("tracks = %v", names)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.vgz")); err == nil {
		t.Error("a file escaped the folder")
	}
}

// AMP: los módulos vienen en gzip y se descomprimen; un LHA se rechaza con un
// error claro.
func TestAMPFetch(t *testing.T) {
	server(t)
	ctx := context.Background()
	a := Sources(t.TempDir())[3]
	items, err := a.Search(ctx, "space")
	if err != nil || len(items) == 0 {
		t.Fatalf("search: %v %d", err, len(items))
	}
	files, err := Fetch(ctx, items[0], t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != "fake mod" || filepath.Ext(files[0]) != ".xm" {
		t.Errorf("file %s = %q", files[0], b)
	}
	bad := Item{Source: "amp", ID: "666", Title: "x", Format: "MOD"}
	if _, err := Fetch(ctx, bad, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "LHA") {
		t.Errorf("LHA: %v", err)
	}
	if got, err := a.ByArtist(ctx, "Jester"); err != nil || len(got) != 2 {
		t.Errorf("by artist: %v %+v", err, got)
	}
}
