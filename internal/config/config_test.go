package config

import "testing"

// TestHomeFor comprueba dónde queda la configuración según la carpeta del
// binario: en el port de PortMaster, dos carpetas arriba de bin/<arquitectura>.
func TestHomeFor(t *testing.T) {
	for dir, want := range map[string]string{
		"/roms/ports/audiofilus/bin/aarch64": "/roms/ports/audiofilus",
		"/roms/ports/audiofilus/bin/x86_64":  "/roms/ports/audiofilus",
		"/home/u/audiofilus":                 "/home/u/audiofilus",
		"/opt/aarch64":                       "/opt/aarch64",
	} {
		if got := homeFor(dir); got != want {
			t.Errorf("homeFor(%q) = %q, want %q", dir, got, want)
		}
	}
}
