//go:build linux && !android

package platform

import (
	"reflect"
	"testing"
)

// TestParseALSACards comprueba que de /proc/asound/cards solo salen los DAC USB.
func TestParseALSACards(t *testing.T) {
	cards := ` 0 [audiocodec     ]: audiocodec - audiocodec
                      audiocodec
 1 [allwinnerhdmi  ]: allwinner-hdmi - allwinner-hdmi
                      allwinner-hdmi
 2 [Audio          ]: USB-Audio - USB Audio Device
                      Generic USB Audio Device at usb-xhci-hcd.0.auto-1, full speed
`
	want := []Output{{ID: "alsa:Audio", Name: "USB Audio Device", USB: true}}
	if got := parseALSACards(cards); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := alsaPCM("alsa:Audio"); got != "plughw:CARD=Audio" {
		t.Errorf("alsaPCM: got %q", got)
	}
}

// TestNameDuplicates comprueba que las salidas con el mismo nombre se
// distinguen por su tarjeta, y las demás no cambian.
func TestNameDuplicates(t *testing.T) {
	list := []Output{
		{ID: "hdmi", Name: "Built-in Audio Stereo", card: "ahubhdmi"},
		{ID: "codec", Name: "Built-in Audio Stereo", card: "audiocodec"},
		{ID: "dac", Name: "USB DAC", card: "Audio"},
	}
	nameDuplicates(list)
	want := []string{"Built-in Audio Stereo (ahubhdmi)", "Built-in Audio Stereo (audiocodec)", "USB DAC"}
	for i, o := range list {
		if o.Name != want[i] {
			t.Errorf("%s: got %q, want %q", o.ID, o.Name, want[i])
		}
	}
}
