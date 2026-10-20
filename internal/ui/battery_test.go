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

// TestBatteryEstimate comprueba el porcentaje cuando la batería dice 0 %
// (el medidor no lo calcula): de la carga si la da, si no de la tensión, y
// sin nada se oculta.
func TestBatteryEstimate(t *testing.T) {
	dir := t.TempDir()
	supply := func(name string, files map[string]string) {
		d := filepath.Join(dir, name)
		_ = os.MkdirAll(d, 0o755)
		for f, v := range files {
			_ = os.WriteFile(filepath.Join(d, f), []byte(v+"\n"), 0o644)
		}
	}
	supply("battery", map[string]string{"type": "Battery", "capacity": "0", "status": "Discharging", "voltage_now": "3800000"})
	if level, _, ok := sysBattery(dir); !ok || level != 0.5 {
		t.Errorf("3.8 V gives %v (%v), want 0.5", level, ok)
	}
	supply("battery", map[string]string{"charge_now": "1500000", "charge_full": "3000000"})
	if level, _, ok := sysBattery(dir); !ok || level != 0.5 {
		t.Errorf("half the charge gives %v (%v), want 0.5", level, ok)
	}
	os.RemoveAll(filepath.Join(dir, "battery"))
	supply("battery", map[string]string{"type": "Battery", "capacity": "0"})
	if _, _, ok := sysBattery(dir); ok {
		t.Error("0 % with nothing to estimate from should hide the battery")
	}
}
