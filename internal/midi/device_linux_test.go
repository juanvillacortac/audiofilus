package midi

import (
	"os"
	"path/filepath"
	"testing"
)

// List encuentra los puertos crudos de ALSA, en orden, con el nombre del
// aparato (de /proc/asound/card<N>/midi<D>, o el id de la tarjeta).
func TestListALSA(t *testing.T) {
	dev, proc := t.TempDir(), t.TempDir()
	for _, n := range []string{"midiC2D0", "midiC1D1", "midiC1D0", "pcmC0D0p", "controlC1"} {
		os.WriteFile(filepath.Join(dev, n), nil, 0o644)
	}
	os.MkdirAll(filepath.Join(proc, "card1"), 0o755)
	os.WriteFile(filepath.Join(proc, "card1", "midi0"), []byte("Keystation 49\n\nOutput 0\n"), 0o644)
	os.WriteFile(filepath.Join(proc, "card1", "id"), []byte("Keystation\n"), 0o644)
	oldDev, oldProc := devDir, procDir
	devDir, procDir = dev, proc
	defer func() { devDir, procDir = oldDev, oldProc }()
	devs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := []Device{
		{filepath.Join(dev, "midiC1D0"), "Keystation 49", 1, 0},
		{filepath.Join(dev, "midiC1D1"), "Keystation", 1, 1},
		{filepath.Join(dev, "midiC2D0"), "MIDI 2", 2, 0},
	}
	if len(devs) != len(want) {
		t.Fatalf("got %+v", devs)
	}
	for i := range want {
		if devs[i] != want[i] {
			t.Errorf("device %d = %+v, want %+v", i, devs[i], want[i])
		}
	}
}
