package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Un perfil bajado se guarda en eq/, y "Perfil de EQ" pasa por todos los de
// eq/ (y por ninguno) poniendo su ecualización.
func TestEQProfiles(t *testing.T) {
	a := &App{offline: true}
	a.paths.Base = t.TempDir()
	if _, ok := a.eqProfileItem(); ok {
		t.Fatal("the item shows with no profiles")
	}
	a.saveEQProfile("Sony WH-1000XM4 (ANC on)", []engine.PEQBand{{Type: "peak", Freq: 100, Gain: 3, Q: 1}}, -3)
	os.WriteFile(filepath.Join(a.eqDir(), "HD 650 ParametricEQ.txt"), []byte("Preamp: -6 dB\nFilter 1: ON LSC Fc 105 Hz Gain 5.5 dB Q 0.70\n"), 0o644)
	it, ok := a.eqProfileItem()
	if !ok {
		t.Fatal("no item with two profiles")
	}
	for _, want := range []string{"HD 650", "Sony WH-1000XM4 (ANC on)", ""} {
		it.do()
		if a.cfg.UI.EQProfile != want {
			t.Fatalf("profile %q, want %q", a.cfg.UI.EQProfile, want)
		}
	}
	it.do() // otra vez el primero: su ecualización
	if a.status.DSP.EQMode != "parametric" || a.status.DSP.PEQPreamp != -6 || len(a.status.DSP.PEQ) != 1 || a.status.DSP.PEQ[0].Type != "lowshelf" {
		t.Errorf("HD 650 not applied: %+v", a.status.DSP)
	}
}

// Borrar el perfil puesto (Y sobre él en AutoEQ) lo quita de eq/ y deja el
// ecualizador plano.
func TestEQProfileDelete(t *testing.T) {
	a := &App{offline: true}
	a.paths.Base = t.TempDir()
	a.saveEQProfile("HD 650", []engine.PEQBand{{Type: "peak", Freq: 100, Gain: 3, Q: 1}}, -3)
	if err := a.useEQProfile("HD 650"); err != nil {
		t.Fatal(err)
	}
	a.openAutoEQ()
	a.menu.items[1].del() // el perfil
	a.menu.items[1].do()  // "Borrar"
	if len(a.eqProfiles()) != 0 {
		t.Errorf("profiles left: %v", a.eqProfiles())
	}
	if a.cfg.UI.EQProfile != "" || len(a.status.DSP.PEQ) != 0 {
		t.Errorf("still applied: %q %+v", a.cfg.UI.EQProfile, a.status.DSP.PEQ)
	}
}

// edit simula pulsar act en un fotograma del editor abierto (el gráfico o el
// paramétrico).
func edit(a *App, act Action) {
	a.input = Input{}
	a.input.inject(act)
	if a.eqEdit != nil {
		a.updateEQ()
	} else {
		a.updatePEQ()
	}
}

// B vuelve siempre un paso: de los editores al menú del que se abrieron
// (Ecualizador, o AutoEQ al elegir un perfil), y del teclado de Buscar sin
// haber escrito, a AutoEQ. Y sobre un perfil pregunta si
// borrarlo: "Conservar" vuelve a AutoEQ, y borrar también, ya sin el perfil.
func TestEQBackOneStep(t *testing.T) {
	a := &App{offline: true, w: 320, h: 240}
	a.paths.Base = t.TempDir()
	os.MkdirAll(a.eqDir(), 0o755)
	os.WriteFile(filepath.Join(a.eqDir(), "HD 650.txt"), []byte("Preamp: -6 dB\nFilter 1: ON PK Fc 100 Hz Gain 3 dB Q 1.0\n"), 0o644)
	a.openEQMenu()
	eq := a.menu
	for sel := range 2 { // gráfico y paramétrico
		eq.sel = sel
		press(a, ActConfirm)
		if a.eqEdit == nil && a.peqEdit == nil {
			t.Fatalf("editor %d didn't open", sel)
		}
		edit(a, ActBack)
		if a.menu != eq || a.eqEdit != nil || a.peqEdit != nil {
			t.Fatalf("back from editor %d: menu %v", sel, a.menu)
		}
	}
	eq.sel = 2
	press(a, ActConfirm) // AutoEQ
	aeq := a.menu
	if aeq.title != "AutoEQ" || aeq.parent != eq {
		t.Fatalf("AutoEQ: %q, parent %v", aeq.title, aeq.parent)
	}
	press(a, ActConfirm) // Buscar: el teclado
	if a.prompt == nil {
		t.Fatal("search didn't open the keyboard")
	}
	a.input = Input{}
	a.input.inject(ActBack) // sin haber escrito: cancela
	a.updatePrompt()
	if a.prompt != nil || a.menu != aeq {
		t.Fatalf("back from the search keyboard: %v", a.menu)
	}
	aeq.sel = 1 // el perfil
	press(a, ActConfirm)
	if a.peqEdit == nil {
		t.Fatal("profile didn't open the editor")
	}
	edit(a, ActBack)
	if a.menu != aeq {
		t.Fatalf("back from the profile: %v", a.menu)
	}
	if len(aeq.items) != 2 {
		t.Fatalf("AutoEQ items: %d (search and the profile, no Delete)", len(aeq.items))
	}
	press(a, ActPause)   // Y sobre HD 650
	press(a, ActConfirm) // Conservar
	if a.menu != aeq {
		t.Fatalf("keep: %v", a.menu)
	}
	press(a, ActPause)
	press(a, ActBack) // B en la pregunta: también a AutoEQ
	if a.menu != aeq {
		t.Fatalf("back from the question: %v", a.menu)
	}
	press(a, ActPause)
	a.menu.sel = 1 // Borrar
	press(a, ActConfirm)
	if _, err := os.Stat(filepath.Join(a.eqDir(), "HD 650.txt")); err == nil {
		t.Fatal("not deleted")
	}
	if a.menu == nil || a.menu.title != "AutoEQ" || len(a.menu.items) != 1 || a.menu.parent != eq {
		t.Fatalf("after deleting: %+v", a.menu)
	}
}
