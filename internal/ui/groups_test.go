package ui

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// TestArrangeSystems comprueba que los sistemas de un grupo van juntos en
// una entrada (con el nombre del sistema del grupo aunque no tenga juegos),
// en el sitio del primero, que cada juego sigue siendo de su sistema, y que
// el único con juegos de un grupo va solo.
func TestArrangeSystems(t *testing.T) {
	sys := func(name, group string, games ...string) *gmSystem {
		s := &gmSystem{sys: esdata.System{Name: name, FullName: name, Group: group}}
		for _, g := range games {
			s.games = append(s.games, esdata.Game{Name: g, Path: name + "/" + g})
		}
		return s
	}
	base := []*gmSystem{
		sys("gba", "", "Advance Wars"),
		sys("odcommander", "tools", "OD Commander"),
		sys("prboom", "ports", "Doom"),
		sys("ports", "ports", "Celeste", "Balatro"),
		sys("vaixterm", "tools", "Terminal"),
		sys("sega32x", "megadrive", "Knuckles Chaotix"),
	}
	faces := map[string]esdata.System{"tools": {Name: "tools", FullName: "Tools"}}
	got := arrangeSystems(base, faces, nil)
	var names []string
	for _, s := range got {
		names = append(names, s.sys.Name)
	}
	want := []string{"gba", "tools", "ports", "sega32x"}
	if len(names) != len(want) {
		t.Fatalf("carousel = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("carousel = %v, want %v", names, want)
		}
	}
	tools, ports := got[1], got[2]
	if !tools.group || tools.isCollection() || tools.sys.FullName != "Tools" || len(tools.games) != 2 {
		t.Errorf("tools group = %+v", tools.sys)
	}
	if len(ports.games) != 3 || ports.games[0].Name != "Balatro" || ports.games[2].Name != "Doom" {
		t.Errorf("ports games not merged by name: %v", ports.games)
	}
	if rs, gm := ports.at(2); rs.sys.Name != "prboom" || gm != &base[2].games[0] {
		t.Errorf("Doom resolves to %s, want its own system prboom", rs.sys.Name)
	}
	if got[3].group {
		t.Errorf("the only megadrive-group system with games was wrapped in a group")
	}
}

// TestCarouselWraps comprueba que el carrusel sigue a la elegida dando
// varias vueltas en los dos sentidos, a pulsaciones sueltas y con el botón
// apretado: la elegida nunca queda a más de una posición del centro (dando
// vueltas a la izquierda llegaba a quedar fuera de la pantalla).
func TestCarouselWraps(t *testing.T) {
	const n = 7
	for _, step := range []int{-1, 1} {
		for _, every := range []int{5, 60} { // apretado (12 por segundo) y suelto
			pos, sel := 0.0, 0
			for f := 0; f < 3*n*every; f++ {
				if f%every == 0 {
					sel = ((sel+step)%n + n) % n
				}
				pos = gmCarStep(pos, sel, n, 1.0/60)
				if pos < 0 || pos >= n {
					t.Fatalf("step %d every %d frames: position %v out of 0..%d", step, every, pos, n)
				}
				if rel := gmWrap(float64(sel)-pos, n); math.Abs(rel) > 1.0001 {
					t.Fatalf("step %d every %d frames, frame %d: selected %v slots from the centre", step, every, f, rel)
				}
			}
			if every == 60 && pos != float64(sel) {
				t.Errorf("step %d: carousel stopped at %v, want %d", step, pos, sel)
			}
		}
	}
}

// TestGMIdleLimit comprueba que el modo juego apaga la pantalla antes que el
// idlewatcher de Knulli (que con el ahorro agresivo congela la interfaz), o
// antes si Ajustes lo pide, y que fuera de Knulli vale solo lo de Ajustes.
func TestGMIdleLimit(t *testing.T) {
	conf := func(text string) *esdata.Conf {
		dir := t.TempDir()
		path := filepath.Join(dir, "userdata/system/knulli.conf")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := esdata.Root(dir).LoadConf()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	cases := []struct {
		name     string
		platform string
		after    int
		conf     string
		want     float64
	}{
		{"before knulli", "knulli", 600, "system.idlewatcher.timer=300\nsystem.batterysaver.mode=dim\n", 285},
		{"settings first", "knulli", 60, "system.idlewatcher.timer=300\n", 60},
		{"settings off", "knulli", 0, "system.idlewatcher.timer=120\n", 105},
		{"knulli default", "knulli", 0, "", 285},
		{"knulli idle off", "knulli", 0, "system.batterysaver.mode=none\n", 0},
		{"not knulli", "desktop", 30, "system.idlewatcher.timer=300\n", 30},
	}
	for _, c := range cases {
		a := &App{}
		a.cfg.UI.SleepAfter = c.after
		a.status.Platform = c.platform
		a.gm.conf = conf(c.conf)
		if got := a.gmIdleLimit(); got != c.want {
			t.Errorf("%s: limit %v, want %v", c.name, got, c.want)
		}
	}
}
