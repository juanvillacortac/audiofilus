package ui

import "testing"

// TestShadersCompile checks that every interface shader (the cassette's
// tape among them) is valid Kage: a mistake would otherwise only show when
// the program starts.
func TestShadersCompile(t *testing.T) {
	if err := LoadShaders(); err != nil {
		t.Fatal(err)
	}
}
