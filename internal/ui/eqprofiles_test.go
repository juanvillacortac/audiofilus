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

// Borrar el perfil puesto lo quita de eq/ y deja el ecualizador plano.
func TestEQProfileDelete(t *testing.T) {
	a := &App{offline: true}
	a.paths.Base = t.TempDir()
	a.saveEQProfile("HD 650", []engine.PEQBand{{Type: "peak", Freq: 100, Gain: 3, Q: 1}}, -3)
	if err := a.useEQProfile("HD 650"); err != nil {
		t.Fatal(err)
	}
	a.openEQDelete()
	a.menu.items[0].do() // el perfil
	a.menu.items[1].do() // "Borrar"
	if len(a.eqProfiles()) != 0 {
		t.Errorf("profiles left: %v", a.eqProfiles())
	}
	if a.cfg.UI.EQProfile != "" || len(a.status.DSP.PEQ) != 0 {
		t.Errorf("still applied: %q %+v", a.cfg.UI.EQProfile, a.status.DSP.PEQ)
	}
}
