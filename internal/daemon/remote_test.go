package daemon

import (
	"slices"
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/input"
)

// TestRemote comprueba que, con el desvío puesto, los gestos van a la cola
// de la interfaz (y no al motor, que aquí es nil), y que sin él no.
func TestRemote(t *testing.T) {
	d := &Daemon{paths: config.Paths{Base: t.TempDir(), Data: t.TempDir()}}
	d.cfg.UI.Sounds = "off" // sin sonidos: con el motor nil no hay dónde
	if d.remote("toggle") {
		t.Fatal("remote took a command while off")
	}
	d.setRemote(true)
	for _, a := range []input.Action{input.TogglePause, input.Next, input.SeekBack} {
		d.do(a, false) // con el motor nil, tocarlo sería un pánico
	}
	st := FullStatus{}
	st.RemoteSeq, st.RemoteActs = d.remoteSeq, d.remoteActs
	if want := []string{"toggle", "next", "seek -0"}; st.RemoteSeq != 3 || !slices.Equal(st.RemoteActs, want) {
		t.Errorf("got %d %q, want 3 %q", st.RemoteSeq, st.RemoteActs, want)
	}
	d.setRemote(false)
	if d.remote("next") || len(d.remoteActs) != 0 {
		t.Error("remote still on after setRemote(false)")
	}
}
