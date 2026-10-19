package ui

import "testing"

// TestShortPath comprueba que recortar una ruta larga termina siempre, sea
// cual sea el ancho (con uno imposible se colgaba la interfaz en iOS), y que
// cuando cabe se queda con lo del final.
func TestShortPath(t *testing.T) {
	long := "/var/mobile/Containers/Data/Application/8BFC77CF-1E2B-4715-ABC2-07807D08714E/Documents/Music"
	if got := shortPath(long, func(s string) bool { return len(s) <= 30 }); got != ".../Documents/Music" {
		t.Errorf("got %q", got)
	}
	if got := shortPath(long, func(string) bool { return false }); got != ".../Music" {
		t.Errorf("with nothing fitting: got %q, want the last folder", got)
	}
	if got := shortPath("/", func(string) bool { return false }); got != "/" {
		t.Errorf("root: got %q", got)
	}
}
