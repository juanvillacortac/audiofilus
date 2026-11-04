package esdata

import (
	"os"
	"testing"
)

// Con AUDIOFILUS_KNULLI_ROOT (una copia de los archivos de una consola), lee
// todo de verdad: los sistemas, sus juegos y las opciones de cada uno.
func TestSampleRoot(t *testing.T) {
	dir := os.Getenv("AUDIOFILUS_KNULLI_ROOT")
	if dir == "" {
		t.Skip("AUDIOFILUS_KNULLI_ROOT not set")
	}
	r := Root(dir)
	systems, err := r.LoadSystems()
	if err != nil {
		t.Fatal(err)
	}
	f, err := r.LoadFeatures()
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.LoadConf()
	if err != nil {
		t.Fatal(err)
	}
	for i := range systems {
		s := &systems[i]
		games := r.LoadGames(s)
		if len(games) == 0 {
			continue
		}
		opts := r.Options(f, c, Scope{System: s})
		emu, core := CurrentEmulator(c, Scope{System: s})
		t.Logf("%-12s %4d games  %-24s %3d options", s.Name, len(games), emu+"/"+core, len(opts))
	}
	t.Logf("global options: %d", len(r.Options(f, c, Scope{})))
}
