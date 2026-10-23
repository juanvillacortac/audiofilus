package config

import (
	"path/filepath"
	"testing"
)

// Varios guardando el mismo archivo a la vez (la interfaz y el servicio, o el
// servicio viejo y el nuevo) no se pisan el temporal: todos terminan bien y
// no quedan temporales.
func TestWriteJSONConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	errs := make(chan error, 16)
	for i := range 16 {
		go func() { errs <- writeJSON(path, map[string]int{"n": i}) }()
	}
	for range 16 {
		if err := <-errs; err != nil {
			t.Errorf("writeJSON: %v", err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}
