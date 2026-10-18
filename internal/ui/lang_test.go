package ui

import "testing"

// TestLanguagesComplete comprueba que cada texto exista en todos los idiomas
// (si falta en uno, la interfaz enseñaría el de otro idioma o nada).
func TestLanguagesComplete(t *testing.T) {
	for l, m := range strs {
		for other, om := range strs {
			for k := range m {
				if _, ok := om[k]; !ok {
					t.Errorf("%q is in %s but missing in %s", k, l, other)
				}
			}
		}
	}
}
