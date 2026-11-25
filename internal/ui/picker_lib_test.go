package ui

import (
	"slices"
	"testing"
)

// TestPickerToggle comprueba las varias carpetas de la biblioteca: agregar,
// quitar, que la última no se quite, que lo de dentro de una carpeta ya
// cuente y que al agregar la de arriba sobren las de dentro.
func TestPickerToggle(t *testing.T) {
	p := &Picker{lib: []string{"/roms/music"}}
	p.toggle("/roms/music")
	if !slices.Equal(p.lib, []string{"/roms/music"}) {
		t.Fatalf("removed the last folder: %v", p.lib)
	}
	p.toggle("/roms2/music")
	if !slices.Equal(p.lib, []string{"/roms/music", "/roms2/music"}) {
		t.Fatalf("add: %v", p.lib)
	}
	if s := p.libState("/roms/music/Album"); s != libInside {
		t.Errorf("inside = %d", s)
	}
	if s := p.libState("/roms/musicals"); s != libOut {
		t.Errorf("sibling with the same prefix = %d", s)
	}
	p.toggle("/roms/music/Album") // ya está: no cambia nada
	if len(p.lib) != 2 {
		t.Fatalf("toggled a folder inside: %v", p.lib)
	}
	p.toggle("/roms")
	if !slices.Equal(p.lib, []string{"/roms2/music", "/roms"}) {
		t.Fatalf("parent should replace its child: %v", p.lib)
	}
	p.toggle("/roms2/music")
	if !slices.Equal(p.lib, []string{"/roms"}) {
		t.Fatalf("remove: %v", p.lib)
	}
}
