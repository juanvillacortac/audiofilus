package tray

import "testing"

// monoIcon is a disc with a hole: opaque on the disc, clear in the centre and
// the corners, and only black with alpha (what a macOS template image uses).
func TestMonoIcon(t *testing.T) {
	img := monoIcon(36)
	if a := img.NRGBAAt(18, 18).A; a != 0 {
		t.Errorf("hole alpha = %d, want 0", a)
	}
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0", a)
	}
	if c := img.NRGBAAt(18, 30); c.A != 255 || c.R != 0 {
		t.Errorf("disc = %+v, want opaque black", c)
	}
}
