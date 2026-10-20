package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSysBattery comprueba qué batería se elige entre varias: no la de un
// mando, ni la que no está, ni la que dice 0 % si otra dice algo (como en
// una RG353V con dArkOS); y que sin ninguna no haya lectura.
func TestSysBattery(t *testing.T) {
	dir := t.TempDir()
	supply := func(name string, files map[string]string) {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, v := range files {
			if err := os.WriteFile(filepath.Join(d, f), []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, _, ok := sysBattery(dir); ok {
		t.Fatal("no supplies should give no reading")
	}
	supply("ac", map[string]string{"type": "Mains", "online": "1"})
	supply("a-gauge", map[string]string{"type": "Battery", "capacity": "0", "status": "Unknown"})
	supply("battery", map[string]string{"type": "Battery", "capacity": "78", "status": "Charging", "present": "1"})
	supply("pad", map[string]string{"type": "Battery", "capacity": "100", "scope": "Device"})
	supply("gone", map[string]string{"type": "Battery", "capacity": "95", "present": "0"})
	level, charging, ok := sysBattery(dir)
	if !ok || level != 0.78 || !charging {
		t.Errorf("battery = %v %v %v, want 0.78, charging", level, charging, ok)
	}
}
