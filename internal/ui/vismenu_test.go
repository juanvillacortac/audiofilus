package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// Al elegir un visualizador normal desde el menú de MilkDrop, el menú se
// rehace sin las opciones de MilkDrop y con la selección en el elegido.
func TestVisualMenuFollowsVisualizer(t *testing.T) {
	a := &App{}
	a.vis.kind = visMilk
	a.vis.milk = &milkState{}
	a.openVisualMenu()
	hasMilk := func() bool {
		for _, it := range a.menu.items {
			if it.label == T("milk_colors") {
				return true
			}
		}
		return false
	}
	if !hasMilk() {
		t.Fatal("MilkDrop menu without its options")
	}
	for _, it := range a.menu.items {
		if it.label == T(visNames[visBars]) {
			it.do()
			break
		}
	}
	if a.vis.kind != visBars {
		t.Fatalf("visualizer %d, want Bars", a.vis.kind)
	}
	if hasMilk() {
		t.Error("MilkDrop options still in the menu after switching to Bars")
	}
	if got := a.menu.items[a.menu.sel].label; got != T(visNames[visBars]) {
		t.Errorf("selected %q, want Bars", got)
	}
}

// La carpeta elegida para la radio o MilkDrop se usa en vez de la de
// siempre, y vuelve a Ajustes.
func TestPickedDirs(t *testing.T) {
	a := &App{offline: true}
	a.paths.Base = "/base"
	if a.radioDir() != "/base/radio" || a.milkDir() != "/base/milkdrop" {
		t.Fatalf("defaults: %q %q", a.radioDir(), a.milkDir())
	}
	a.pickFor, a.mode = pickRadio, modePicker
	a.pickedDir("/music/radios")
	a.pickFor = pickMilkDrop
	a.pickedDir("/music/milk")
	if a.radioDir() != "/music/radios" || a.milkDir() != "/music/milk" {
		t.Errorf("picked: radio %q, milkdrop %q", a.radioDir(), a.milkDir())
	}
	if a.mode != modeMain || a.pickFor != pickMusic {
		t.Errorf("after picking: mode %v, target %v; want back to the main screen", a.mode, a.pickFor)
	}
}

// Los récords con su nombre viejo (en español) pasan al nuevo, sin pisar
// uno que ya exista con el nuevo.
func TestMigrateGameFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cripta.json", "old crypt")
	write("ritmo.json", "old rhythm")
	write("rhythm.json", "new rhythm")
	migrateGameFiles(dir)
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	if read("crypt.json") != "old crypt" {
		t.Errorf("crypt.json = %q, want the old record moved over", read("crypt.json"))
	}
	if read("rhythm.json") != "new rhythm" {
		t.Errorf("rhythm.json = %q, want the existing one kept", read("rhythm.json"))
	}
	if _, err := os.Stat(filepath.Join(dir, "cripta.json")); err == nil {
		t.Error("cripta.json is still there")
	}
}

// El visualizador elegido se guarda por su nombre y se recupera al abrir.
func TestVisualizerRemembered(t *testing.T) {
	a := &App{offline: true}
	a.setVisualizer(visLake)
	if a.cfg.UI.Visualizer != "lake" {
		t.Errorf("saved %q, want lake", a.cfg.UI.Visualizer)
	}
	if k := visualizerIndex(a.cfg.UI.Visualizer); k != visLake {
		t.Errorf("restored %d, want Lake (%d)", k, visLake)
	}
	if visualizerIndex("gone") != 0 || visualizerIndex("") != 0 {
		t.Error("an unknown name should give the first visualizer")
	}
}

// Con el cambio de botones, A/B y X/Y se intercambian los dos (distribución
// de Nintendo); sin él, cada acción va a su botón de siempre.
func TestSwapButtons(t *testing.T) {
	in := &Input{}
	if got := in.buttons(ActShuffle); got[0] != actionButtons[ActShuffle][0] {
		t.Errorf("unswapped X: %v", got)
	}
	in.swapAB = true
	for a, b := range map[Action]Action{ActConfirm: ActBack, ActBack: ActConfirm, ActShuffle: ActPause, ActPause: ActShuffle} {
		if got := in.buttons(a); got[0] != actionButtons[b][0] {
			t.Errorf("swapped action %d: button %v, want %v", a, got, actionButtons[b])
		}
	}
	if got := in.buttons(ActMenu); got[0] != actionButtons[ActMenu][0] {
		t.Errorf("Start should not change: %v", got)
	}
}
