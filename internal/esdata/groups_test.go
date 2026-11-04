package esdata

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHiddenSystems comprueba que se leen los sistemas ocultos de
// es_settings.cfg (sin elemento raíz, separados por ";").
func TestHiddenSystems(t *testing.T) {
	root := Root(t.TempDir())
	dir := root.Path(userESDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `<?xml version="1.0"?>
<bool name="DrawFramerate" value="false" />
<string name="HiddenSystems" value="pygame;mame; " />
<string name="Language" value="es_ES" />
`
	if err := os.WriteFile(filepath.Join(dir, "es_settings.cfg"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	h := root.HiddenSystems()
	if len(h) != 2 || !h["pygame"] || !h["mame"] {
		t.Errorf("hidden = %v, want pygame and mame", h)
	}
	if h := Root(t.TempDir()).HiddenSystems(); len(h) != 0 {
		t.Errorf("without es_settings.cfg, hidden = %v", h)
	}
}

// TestGrouped comprueba que un sistema con grupo va en él salvo con
// <grupo>.ungroup=1 en knulli.conf.
func TestGrouped(t *testing.T) {
	s := System{Name: "prboom", Group: "ports"}
	c := &Conf{values: map[string]string{}}
	if !s.Grouped(c) {
		t.Errorf("prboom not grouped by default")
	}
	c.values["ports.ungroup"] = "1"
	if s.Grouped(c) {
		t.Errorf("prboom grouped with ports.ungroup=1")
	}
	if (&System{Name: "gba"}).Grouped(c) {
		t.Errorf("a system without group is grouped")
	}
}
