package daemon

import (
	"testing"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// TestOutputTarget comprueba adónde se cambia la salida con PipeWire/PulseAudio.
func TestOutputTarget(t *testing.T) {
	codec := platform.Output{ID: "codec"}
	hdmi := platform.Output{ID: "hdmi"}
	dac := platform.Output{ID: "usb-dac", USB: true}
	bt := platform.Output{ID: "bluez"}
	all := []platform.Output{codec, hdmi, dac}
	set := func(ids ...string) map[string]bool {
		m := map[string]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	tests := []struct {
		name      string
		pref      string
		list      []platform.Output
		cur, prev string
		before    map[string]bool
		gone      bool
		want      string
	}{
		{"dac plugged in", "", all, "codec", "", set("codec", "hdmi"), false, "usb-dac"},
		{"dac already there at start", "", all, "codec", "", nil, false, "usb-dac"},
		{"already on the dac", "", all, "usb-dac", "", nil, false, ""},
		{"user moved away from the dac", "", all, "codec", "", set("codec", "hdmi", "usb-dac"), false, ""},
		{"dac unplugged: back to before", "", []platform.Output{codec, hdmi}, "hdmi", "codec", set("codec", "hdmi", "usb-dac"), true, "codec"},
		{"dac unplugged, before gone too", "", []platform.Output{hdmi}, "hdmi", "codec", set("codec", "hdmi", "usb-dac"), true, ""},
		{"bluetooth connects: left alone", "", []platform.Output{codec, bt}, "bluez", "", set("codec"), false, ""},
		{"chosen output", "hdmi", all, "codec", "", nil, false, "hdmi"},
		{"chosen output missing", "bluez", all, "codec", "", nil, false, ""},
	}
	for _, tt := range tests {
		if got := outputTarget(tt.pref, tt.list, tt.cur, tt.prev, tt.before, tt.gone); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
