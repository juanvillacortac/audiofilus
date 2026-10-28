package ui

import "testing"

// Preparar los sitios no les pide nada: el calentamiento del arranque dibuja
// la pantalla de los módulos, y sin red eso daba errores de The Mod Archive
// al abrir Audiofilus.
func TestModInitDoesNotFetch(t *testing.T) {
	a := &App{}
	a.paths.Data = t.TempDir()
	a.modInit()
	for i, l := range a.mods.loading {
		if l {
			t.Errorf("site %d started loading on init", i)
		}
	}
	select {
	case r := <-a.mods.results:
		t.Errorf("init fetched %q from site %d", r.what, r.tab)
	default:
	}
}
