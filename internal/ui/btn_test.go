package ui

import "testing"

// Con el teclado, la ayuda nombra las teclas también en los compuestos.
func TestBtnKeyboardNames(t *testing.T) {
	a := &App{}
	for in, want := range map[string]string{
		"X":           "S",
		"X X":         "S S",
		"SEL SEL":     "TAB TAB",
		"A/Y":         "ENTER/SPACE",
		"L1/R1":       "Q/E",
		"B+A":         "ESC+ENTER",
		"FN x3 L1/R1": "F1 x3 Q/E",
		"<> ...":      "<> ...",
		"=Z/X":        "Z/X", // ya es del teclado: tal cual
	} {
		if got := a.btn(in); got != want {
			t.Errorf("btn(%q) = %q, want %q", in, got, want)
		}
	}
	a.input.usingPad = true
	if got := a.btn("X X"); got != "X X" {
		t.Errorf("with a gamepad btn(X X) = %q", got)
	}
}

// Las direcciones (cruceta, stick o flechas del teclado) son siempre una
// cruceta: las seguidas, una sola con esos brazos pulsados, también tras "+"
// o "/"; las letras quedan como texto.
func TestBtnParts(t *testing.T) {
	for in, want := range map[string][]string{
		"<>":     {"<>"},
		"<>^v":   {"<>^v"},
		"A+^v":   {"A+", "^v"},
		"^/v":    {"^", "/", "v"},
		"< v B":  {"<", " ", "v", " B"},
		"L1/R1":  {"L1/R1"},
		"<> ...": {"<>", " ..."},
		"v":      {"v"},
	} {
		got := btnParts(in)
		if len(got) != len(want) {
			t.Errorf("btnParts(%q) = %q, want %q", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("btnParts(%q) = %q, want %q", in, got, want)
				break
			}
		}
	}
	if padIcon("><") != padIcon("<>") || padIcon("<>^v") != "padurdl" {
		t.Errorf("pad icons: %q %q", padIcon("<>"), padIcon("<>^v"))
	}
}
