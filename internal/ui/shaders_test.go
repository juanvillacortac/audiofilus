package ui

import "testing"

// TestShadersCompile comprueba que todos los shaders de la interfaz (la
// cinta del casete entre ellos) son Kage válido: si no, un error solo se
// vería al arrancar el programa.
func TestShadersCompile(t *testing.T) {
	if err := LoadShaders(); err != nil {
		t.Fatal(err)
	}
}
