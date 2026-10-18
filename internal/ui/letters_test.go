package ui

import "testing"

// TestLetterOf comprueba cómo se agrupa por letras: mayúsculas, sin tilde, y
// los números y símbolos en "#".
func TestLetterOf(t *testing.T) {
	for in, want := range map[string]string{
		"chevelle": "C", "Ángel": "A", "ñu": "N", "  (hed) p.e.": "H",
		"311": "#", "": "#", "...": "#", "Ölüm": "O",
	} {
		if got := letterOf(in); got != want {
			t.Errorf("letterOf(%q) = %q, want %q", in, got, want)
		}
	}
}
