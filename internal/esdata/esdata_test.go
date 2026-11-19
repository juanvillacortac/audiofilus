package esdata

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testFeatures es un es_features.cfg pequeño con lo que importa: una opción
// compartida, las globales, un emulador con núcleos, opciones por sistema
// dentro de un núcleo, submenú, grupo y orden.
const testFeatures = `<?xml version="1.0"?>
<features>
  <sharedFeatures>
    <feature name="GAME ASPECT RATIO" value="ratio" description="Force a ratio.">
      <choice name="4/3" value="4/3" />
      <choice name="Full" value="full" />
    </feature>
    <feature name="REWIND" value="rewind" preset="switchauto" />
  </sharedFeatures>
  <globalFeatures>
    <sharedFeature value="ratio" />
  </globalFeatures>
  <emulator name="libretro" features="netplay">
    <cores>
      <core name="mgba" features="cheevos, rewind">
        <sharedFeature value="ratio" group="VIDEO" order="2" />
        <feature name="COLOR CORRECTION" value="mgba_color" group="VIDEO" order="1">
          <choice name="Off" />
          <choice name="On" value="enabled" />
        </feature>
        <systems>
          <system name="gb">
            <feature name="GB MODEL" value="gb_model" submenu="MODEL">
              <choice name="DMG" value="dmg" />
            </feature>
          </system>
        </systems>
      </core>
      <core name="gpsp">
        <feature name="SKIP BIOS" value="gpsp_skip" preset="switch" />
      </core>
    </cores>
  </emulator>
</features>`

// Las opciones de un sistema son las del emulador y del núcleo elegido, más
// las suyas para ese sistema, ordenadas; las compartidas se copian.
func TestFeaturesFor(t *testing.T) {
	f, err := ParseFeatures([]byte(testFeatures))
	if err != nil {
		t.Fatal(err)
	}
	keys := func(fs []Feature) []string {
		var out []string
		for _, x := range fs {
			out = append(out, x.Key)
		}
		return out
	}
	if got, want := keys(f.For("gba", "libretro", "mgba")), []string{"rewind", "mgba_color", "ratio"}; !reflect.DeepEqual(got, want) {
		t.Errorf("gba/mgba = %v, want %v", got, want)
	}
	if got, want := keys(f.For("gb", "libretro", "mgba")), []string{"rewind", "gb_model", "mgba_color", "ratio"}; !reflect.DeepEqual(got, want) {
		t.Errorf("gb/mgba = %v, want %v", got, want)
	}
	if got := keys(f.For("gba", "lr-gpsp", "gpsp")); !reflect.DeepEqual(got, []string{"gpsp_skip"}) {
		t.Errorf("lr- emulators should be libretro: %v", got)
	}
	if !f.Supports("gba", "libretro", "mgba", FlagCheevos) || !f.Supports("gba", "libretro", "gpsp", FlagNetplay) {
		t.Error("flags not merged from core and emulator")
	}
	if len(f.Global) != 1 || f.Global[0].Key != "ratio" {
		t.Errorf("global = %v", keys(f.Global))
	}
	// Un <choice> sin value toma su nombre.
	for _, x := range f.For("gba", "libretro", "mgba") {
		if x.Key == "mgba_color" && x.Choices[0].Value != "Off" {
			t.Errorf("choice without value = %q", x.Choices[0].Value)
		}
	}
}

// Guardar toca solo las claves cambiadas: reemplaza su línea (también una
// comentada), quita las que se ponen en AUTO y añade las nuevas al final,
// sin tocar comentarios ni el resto.
func TestConfSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "userdata/system/knulli.conf")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# comment\n#snes.ratio=4/3\ngba.core=mgba\nwifi.enabled=1\nnes.rewind=1\n"), 0o644)
	c, err := Root(dir).LoadConf()
	if err != nil {
		t.Fatal(err)
	}
	if c.Get("gba.core") != "mgba" || c.Get("snes.ratio") != "" {
		t.Fatalf("read wrong values")
	}
	c.Set("snes.ratio", "full")
	c.Set("nes.rewind", "auto")
	c.Set(`gba["a b.zip"].core`, "gpsp")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := "# comment\nsnes.ratio=full\ngba.core=mgba\nwifi.enabled=1\ngba[\"a b.zip\"].core=gpsp\n"
	if string(b) != want {
		t.Errorf("saved:\n%s\nwant:\n%s", b, want)
	}
}

// writeRoot arma una raíz de Knulli de prueba con un sistema, su gamelist y
// knulli.conf.
func writeRoot(t *testing.T) Root {
	dir := t.TempDir()
	put := func(p, s string) {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("usr/share/emulationstation/es_systems.cfg", `<systemList><system>
  <fullname>Game Boy Advance</fullname><name>gba</name><path>/userdata/roms/gba</path>
  <extension>.gba .zip</extension>
  <command>emulatorlauncher %CONTROLLERSCONFIG% -system %SYSTEM% -rom %ROM% -gameinfoxml %GAMEINFOXML% -systemname %SYSTEMNAME%</command>
  <emulators><emulator name="libretro"><cores><core>gpsp</core><core default="true">mgba</core></cores></emulator></emulators>
</system></systemList>`)
	put("userdata/system/configs/emulationstation/es_systems_gba.cfg", `<systemList><system><name>gba</name>
  <emulators><emulator name="libretro"><cores><core default="true">mgba</core><core>vba-m</core></cores></emulator></emulators>
</system></systemList>`)
	put("usr/share/emulationstation/es_features.cfg", testFeatures)
	put("userdata/roms/gba/gamelist.xml", `<gameList>
  <game><path>./b game.zip</path><name>Beta</name><thumbnail>./images/b.png</thumbnail><playcount>3</playcount><releasedate>20010611T000000</releasedate></game>
  <game><path>./a.gba</path><name>alpha</name></game>
  <game><path>./h.gba</path><name>Hidden</name><hidden>true</hidden></game>
</gameList>`)
	put("userdata/system/knulli.conf", "global.ratio=full\ngba.core=gpsp\ngba.emulator=libretro\n")
	put("usr/share/knulli/shaders/configs/zfast/rendering-defaults.yml", "")
	return Root(dir)
}

// Los sistemas salen de es_systems.cfg con los emuladores que cambia un
// es_systems_*.cfg del usuario; los juegos, del gamelist, ordenados y sin
// los ocultos, con las rutas absolutas.
func TestSystemsAndGames(t *testing.T) {
	r := writeRoot(t)
	systems, err := r.LoadSystems()
	if err != nil || len(systems) != 1 {
		t.Fatalf("systems = %v, %v", systems, err)
	}
	s := &systems[0]
	if e, c := s.DefaultEmulator(); e != "libretro" || c != "mgba" {
		t.Errorf("default = %s/%s", e, c)
	}
	if len(s.Emulators[0].Cores) != 2 || s.Emulators[0].Cores[1].Name != "vba-m" {
		t.Errorf("user override not applied: %+v", s.Emulators)
	}
	games := r.LoadGames(s)
	if len(games) != 2 || games[0].Name != "alpha" || games[1].Name != "Beta" {
		t.Fatalf("games = %+v", games)
	}
	g := games[1]
	if g.Path != "/userdata/roms/gba/b game.zip" || g.Thumbnail != "/userdata/roms/gba/images/b.png" || g.PlayCount != 3 || g.Year() != "2001" {
		t.Errorf("game = %+v", g)
	}
}

// Las opciones de un juego: el emulador primero, luego las del núcleo que
// usa el sistema (gpsp, por knulli.conf) y lo heredado del global.
func TestOptions(t *testing.T) {
	r := writeRoot(t)
	systems, _ := r.LoadSystems()
	f, _ := r.LoadFeatures()
	c, _ := r.LoadConf()
	s := &systems[0]
	games := r.LoadGames(s)
	scope := Scope{System: s, Game: &games[1]}
	if got := scope.ConfigName(); got != `gba["b game.zip"]` {
		t.Errorf("config name = %s", got)
	}
	opts := r.Options(f, c, scope)
	if len(opts) != 2 || opts[0].Kind != OptEmulator || opts[1].Key != `gba["b game.zip"].gpsp_skip` {
		t.Fatalf("options = %+v", opts)
	}
	// Cambiar el emulador del juego a mgba cambia las filas.
	emu := &opts[0]
	for i, v := range emu.Values {
		if v.Value == "libretro/mgba" {
			emu.Set(c, i)
		}
	}
	opts = r.Options(f, c, scope)
	var ratio *Option
	for i := range opts {
		if strings.HasSuffix(opts[i].Key, ".ratio") {
			ratio = &opts[i]
		}
	}
	if ratio == nil || ratio.Inherited != "Full" || ratio.Group != "VIDEO" {
		t.Fatalf("ratio option = %+v", ratio)
	}
	ratio.Set(c, 1) // 4/3
	if c.Get(`gba["b game.zip"].ratio`) != "4/3" || ratio.Current(c) != 1 {
		t.Error("set/current mismatch")
	}
	// Los shaders salen de las carpetas.
	sh := r.option(Feature{Key: "shaderset", Preset: "shaderset"}, "gba", c, "")
	if len(sh.Values) != 3 || sh.Values[2].Value != "zfast" {
		t.Errorf("shaders = %+v", sh.Values)
	}
}

// La orden de lanzar: los marcadores puestos, los mandos donde van, y sin el
// -gameinfoxml vacío; una ruta con espacios es un solo argumento.
func TestLaunchArgs(t *testing.T) {
	s := &System{Name: "gba", FullName: "Game Boy Advance",
		Command: "emulatorlauncher %CONTROLLERSCONFIG% -system %SYSTEM% -rom %ROM% -gameinfoxml %GAMEINFOXML% -systemname %SYSTEMNAME%"}
	g := &Game{Path: "/userdata/roms/gba/b game.zip", Name: "Beta"}
	got := LaunchArgs(s, g, "libretro", "mgba", []string{"-p1index", "0"})
	want := []string{"emulatorlauncher", "-p1index", "0", "-system", "gba", "-rom", "/userdata/roms/gba/b game.zip", "-systemname", "Game Boy Advance"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q\nwant %q", got, want)
	}
}

// Un deslizador va de "desde" a "hasta" con su paso; sin AUTO, el primero es
// el valor por defecto.
func TestSliderValues(t *testing.T) {
	v := sliderValues("10 120 5 100 %", false)
	if v[0].Name != "100%" || v[0].Value != "" || v[1].Name != "10%" || v[len(v)-1].Name != "120%" {
		t.Errorf("slider = %v", v)
	}
	if a := sliderValues("0 2 1 x", true); a[0].Name != ValAuto || a[1].Name != "0x" || len(a) != 4 {
		t.Errorf("sliderauto = %v", a)
	}
}

// TestSaveStates comprueba que se encuentran las partidas de un juego (la
// ranura 0 sin número, las numeradas y la automática, con su captura) y no
// las de otro juego con un nombre que empieza igual.
func TestSaveStates(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.Path("/userdata/saves/gb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for _, f := range []string{"Zelda (USA).state", "Zelda (USA).state2", "Zelda (USA).state2.png", "Zelda (USA).state.auto", "Zelda (USA) DX.state1", "Zelda (USA).srm", "Zelda (USA).stateX"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "Zelda (USA).state"), old, old); err != nil {
		t.Fatal(err)
	}
	sys := &System{Name: "gb"}
	g := &Game{Path: "/userdata/roms/gb/Zelda (USA).zip"}
	got := root.SaveStates(sys, g)
	if len(got) != 3 {
		t.Fatalf("got %d states, want 3: %+v", len(got), got)
	}
	if last := got[2]; last.Slot != 0 || last.Path != "/userdata/saves/gb/Zelda (USA).state" {
		t.Errorf("oldest state = %+v, want slot 0", last)
	}
	slots := map[int]SaveState{}
	for _, s := range got {
		slots[s.Slot] = s
	}
	if s, ok := slots[2]; !ok || s.Image == "" {
		t.Errorf("slot 2 = %+v, want it with its screenshot", s)
	}
	if _, ok := slots[AutoSlot]; !ok {
		t.Errorf("auto state missing")
	}
	args := StateArgs(slots[AutoSlot])
	if strings.Join(args, " ") != "-state_slot -1 -state_filename /userdata/saves/gb/Zelda (USA).state.auto" {
		t.Errorf("StateArgs = %q", args)
	}
}

// TestGameCache comprueba que los juegos salen del caché mientras el
// gamelist.xml no cambia, que se releen si cambia (o si cambia la
// definición del sistema) y que el caché guardado se vuelve a leer igual.
func TestGameCache(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.Path("/userdata/roms/gb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(xml string, when time.Time) {
		p := filepath.Join(dir, "gamelist.xml")
		if err := os.WriteFile(p, []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour)
	write(`<gameList><game><path>./a.gb</path><name>Alpha</name></game></gameList>`, t0)
	sys := &System{Name: "gb", Path: "/userdata/roms/gb", Extensions: []string{".gb"}}
	cachePath := filepath.Join(t.TempDir(), "cache.gob")

	c := LoadGameCache(cachePath)
	if g, hit := root.LoadGamesCached(sys, c); hit || len(g) != 1 {
		t.Fatalf("first load: hit=%v games=%d, want a miss with 1 game", hit, len(g))
	}
	if err := c.Save(cachePath); err != nil {
		t.Fatal(err)
	}
	c = LoadGameCache(cachePath)
	if g, hit := root.LoadGamesCached(sys, c); !hit || len(g) != 1 || g[0].Name != "Alpha" {
		t.Fatalf("second load: hit=%v games=%+v, want Alpha from the cache", hit, g)
	}
	write(`<gameList><game><path>./a.gb</path><name>Alpha</name></game><game><path>./b.gb</path><name>Beta</name></game></gameList>`, t0.Add(time.Minute))
	if g, hit := root.LoadGamesCached(sys, c); hit || len(g) != 2 {
		t.Fatalf("after editing gamelist: hit=%v games=%d, want a miss with 2 games", hit, len(g))
	}
	sys.Extensions = append(sys.Extensions, ".zip")
	if _, hit := root.LoadGamesCached(sys, c); hit {
		t.Errorf("after changing the extensions: got a cache hit")
	}
	c.Prune(map[string]bool{})
	if len(c.Systems) != 0 || !c.Dirty() {
		t.Errorf("Prune left %d systems (dirty=%v)", len(c.Systems), c.Dirty())
	}
}

// TestSetFavorite comprueba que marcar y desmarcar un favorito cambia solo
// su entrada del gamelist.xml (con su sangría), que se vuelve a leer igual, y
// que un juego que no estaba en él se añade.
func TestSetFavorite(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.Path("/userdata/roms/gb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "<?xml version=\"1.0\"?>\n<gameList>\n\t<game>\n\t\t<path>./a.gb</path>\n\t\t<name>Alpha &amp; Co</name>\n\t\t<rating>0.8</rating>\n\t</game>\n" +
		"\t<game>\n\t\t<path>./b.gb</path>\n\t\t<name>Beta</name>\n\t\t<favorite>true</favorite>\n\t</game>\n</gameList>\n"
	file := filepath.Join(dir, "gamelist.xml")
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := &System{Name: "gb", Path: "/userdata/roms/gb"}
	games := root.LoadGames(sys)
	if len(games) != 2 || games[0].Favorite || !games[1].Favorite {
		t.Fatalf("games = %+v", games)
	}
	if err := root.SetFavorite(sys, &games[0], true); err != nil {
		t.Fatal(err)
	}
	if err := root.SetFavorite(sys, &games[1], false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	want := "<?xml version=\"1.0\"?>\n<gameList>\n\t<game>\n\t\t<path>./a.gb</path>\n\t\t<name>Alpha &amp; Co</name>\n\t\t<rating>0.8</rating>\n\t\t<favorite>true</favorite>\n\t</game>\n" +
		"\t<game>\n\t\t<path>./b.gb</path>\n\t\t<name>Beta</name>\n\t</game>\n</gameList>\n"
	if string(b) != want {
		t.Errorf("gamelist =\n%s\nwant\n%s", b, want)
	}
	extra := &Game{Path: "/userdata/roms/gb/sub/c.gb", Name: "Gamma"}
	if err := root.SetFavorite(sys, extra, true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if !strings.Contains(string(b), "<path>./sub/c.gb</path>") || !strings.HasSuffix(string(b), "</game>\n</gameList>\n") {
		t.Errorf("new entry not added:\n%s", b)
	}
	games = root.LoadGames(sys)
	favs := 0
	for _, g := range games {
		if g.Favorite {
			favs++
		}
	}
	if favs != 2 {
		t.Errorf("after reloading, %d favorites, want 2: %+v", favs, games)
	}
}

// buildMO arma un catálogo .mo (little endian) con los pares dados, para
// las pruebas.
func buildMO(pairs [][2]string) []byte {
	n := len(pairs)
	head := 28
	origT, transT := head, head+8*n
	data := transT + 8*n
	var strs []byte
	tab := make([]byte, 16*n)
	for i, p := range pairs {
		binary.LittleEndian.PutUint32(tab[8*i:], uint32(len(p[0])))
		binary.LittleEndian.PutUint32(tab[8*i+4:], uint32(data+len(strs)))
		strs = append(strs, p[0]...)
		strs = append(strs, 0)
	}
	for i, p := range pairs {
		binary.LittleEndian.PutUint32(tab[8*n+8*i:], uint32(len(p[1])))
		binary.LittleEndian.PutUint32(tab[8*n+8*i+4:], uint32(data+len(strs)))
		strs = append(strs, p[1]...)
		strs = append(strs, 0)
	}
	h := make([]byte, head)
	binary.LittleEndian.PutUint32(h, 0x950412de)
	binary.LittleEndian.PutUint32(h[8:], uint32(n))
	binary.LittleEndian.PutUint32(h[12:], uint32(origT))
	binary.LittleEndian.PutUint32(h[16:], uint32(transT))
	return append(append(h, tab...), strs...)
}

// TestCatalog comprueba que se lee un catálogo .mo (con contexto y
// plurales) y que se busca en la carpeta de locale del idioma.
func TestCatalog(t *testing.T) {
	mo := buildMO([][2]string{
		{"VIDEO MODE", "MODO DE VÍDEO"},
		{"menu\x04SHADERS", "SHADERS (menú)"},
		{"%d GAME\x00%d GAMES", "%d JUEGO\x00%d JUEGOS"},
	})
	c, err := ParseMO(mo)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{"VIDEO MODE": "MODO DE VÍDEO", "SHADERS": "SHADERS (menú)", "%d GAME": "%d JUEGO", "OTHER": "OTHER"} {
		if got := c.Tr(in); got != want {
			t.Errorf("Tr(%q) = %q, want %q", in, got, want)
		}
	}
	root := Root(t.TempDir())
	dir := root.Path("/usr/share/locale/pt_BR/LC_MESSAGES")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "emulationstation2.mo"), mo, 0o644); err != nil {
		t.Fatal(err)
	}
	if root.LoadCatalog("pt").Tr("VIDEO MODE") != "MODO DE VÍDEO" {
		t.Errorf("pt catalog not found in pt_BR")
	}
	if root.LoadCatalog("en") != nil {
		t.Errorf("English should have no catalog")
	}
}

// TestRecordPlay comprueba que al volver de un juego se suma una partida y
// su tiempo, y se apunta la fecha, en su entrada del gamelist.xml (y se
// crea si no estaba).
func TestRecordPlay(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.Path("/userdata/roms/gba")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "gamelist.xml")
	orig := "<gameList>\n\t<game>\n\t\t<path>./a.gba</path>\n\t\t<name>Alpha</name>\n\t\t<playcount>4</playcount>\n\t\t<gametime>100</gametime>\n\t</game>\n</gameList>\n"
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 0, 30, 15, 0, time.Local)
	if err := root.RecordPlay("/userdata/roms/gba", "/userdata/roms/gba/a.gba", "Alpha", 90*time.Second, now); err != nil {
		t.Fatal(err)
	}
	if err := root.RecordPlay("/userdata/roms/gba", "/userdata/roms/gba/b.gba", "Beta", 5*time.Second, now); err != nil {
		t.Fatal(err)
	}
	games := root.LoadGames(&System{Name: "gba", Path: "/userdata/roms/gba"})
	if len(games) != 2 {
		t.Fatalf("got %d games", len(games))
	}
	a, b := games[0], games[1]
	if a.PlayCount != 5 || !a.LastPlayed.Equal(esTime("20261104T003015")) || b.PlayCount != 1 {
		t.Errorf("after recording: %+v / %+v", a, b)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "<gametime>190</gametime>") || !strings.Contains(string(data), "<gametime>5</gametime>") {
		t.Errorf("gametime not added up:\n%s", data)
	}
}

// El lanzador del port se reconoce por su contenido (el de verdad, para que
// no deje de reconocerse si cambia) y otros puertos no.
func TestIsOwnLauncher(t *testing.T) {
	if !IsOwnLauncher(filepath.Join("..", "..", "portmaster", "Audiofilus.sh")) {
		t.Error("portmaster/Audiofilus.sh is not recognized")
	}
	dir := t.TempDir()
	other := filepath.Join(dir, "Halo.sh")
	os.WriteFile(other, []byte("#!/bin/bash\n./halo\n"), 0o644)
	if IsOwnLauncher(other) {
		t.Error("another port is recognized as Audiofilus")
	}
	renamed := filepath.Join(dir, "Musica.SH")
	os.WriteFile(renamed, []byte("export AUDIOFILUS_LAUNCHER=1\n"), 0o644)
	if !IsOwnLauncher(renamed) {
		t.Error("a renamed launcher is not recognized")
	}
	notScript := filepath.Join(dir, "notes.txt")
	os.WriteFile(notScript, []byte("export AUDIOFILUS_LAUNCHER=1\n"), 0o644)
	if IsOwnLauncher(notScript) || IsOwnLauncher(filepath.Join(dir, "missing.sh")) {
		t.Error("a file that is not a .sh, or is missing, is recognized")
	}
}
