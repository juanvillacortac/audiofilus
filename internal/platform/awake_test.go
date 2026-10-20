//go:build linux

package platform

import "testing"

// TestAwakeWithoutInhibitor comprueba que SetAwake(false) deshaga lo de
// SetAwake(true) aunque logind no haya dado el inhibidor (en dArkOS, como el
// usuario ark, lo niega): antes, sin inhibidor, no se deshacía nada y la
// copia de pause.sh se quedaba puesta en segundo plano.
func TestAwakeWithoutInhibitor(t *testing.T) {
	h := &handheld{name: "rocknix"}
	h.SetAwake(true)
	if !h.awake {
		t.Fatal("SetAwake(true) did not mark it awake")
	}
	h.SetAwake(false)
	if h.awake || h.inhibit != nil {
		t.Errorf("after SetAwake(false): awake %v, inhibitor %v; want both released", h.awake, h.inhibit != nil)
	}
	// Y con el inhibidor fallido a propósito: lo mismo.
	h.awake, h.inhibit = true, nil
	h.SetAwake(false)
	if h.awake {
		t.Error("SetAwake(false) without an inhibitor left it awake")
	}
}
