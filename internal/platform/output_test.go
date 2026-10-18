package platform

import (
	"strings"
	"testing"
)

// TestOutputKind comprueba cómo se clasifica la salida de audio por defecto.
func TestOutputKind(t *testing.T) {
	speaker := `Sink #50
	State: RUNNING
	Name: alsa_output.pci.HiFi__Speaker__sink
	Properties:
		device.bus = "pci"
	Ports:
		[Out] Speaker: Speaker (type: Speaker, priority: 100, availability unknown)
	Active Port: [Out] Speaker
`
	jack := `Sink #0
	Name: alsa_output.platform-codec.analog-stereo
	Properties:
		device.bus = "platform"
	Ports:
		analog-output-speaker: Speakers (type: Speaker, priority: 10000, availability unknown)
		analog-output-headphones: Headphones (type: Headphones, priority: 9900, available)
	Active Port: analog-output-headphones
`
	unplugged := `Sink #0
	Name: alsa_output.platform-codec.analog-stereo
	Ports:
		analog-output-headphones: Headphones (type: Headphones, priority: 9900, not available)
	Active Port: analog-output-headphones
`
	bt := speaker + `Sink #77
	Name: bluez_output.AA_BB_CC_DD_EE_FF.1
	Properties:
		device.bus = "bluetooth"
	Active Port: headset-output
`
	for _, c := range []struct{ name, def, sinks, want string }{
		{"speaker", "alsa_output.pci.HiFi__Speaker__sink", speaker, OutputSpeaker},
		{"wired", "alsa_output.platform-codec.analog-stereo", jack, OutputWired},
		{"unplugged", "alsa_output.platform-codec.analog-stereo", unplugged, OutputSpeaker},
		{"bluetooth", "bluez_output.AA_BB_CC_DD_EE_FF.1", bt, OutputBluetooth},
		{"ucm headphones", "alsa_output.pci.HiFi__Headphones__sink", "Sink #1\n\tName: alsa_output.pci.HiFi__Headphones__sink\n", OutputWired},
		{"no default", "", speaker, ""},
	} {
		if got := outputKind(c.def, c.sinks); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// La RG34XX-SP: PulseAudio no ve el jack; el pin de debugfs sí.
	gpio := `gpiochip0: GPIOs 0-31, parent: platform/pio, pio:
 gpio-259 (                    |Headphone detection ) in  %s
 gpio-260 (                    |usb_id_det          ) in  lo
`
	for state, want := range map[string]bool{"hi": true, "lo": false} {
		plugged, found := jackFromGPIO(strings.Replace(gpio, "%s", state, 1))
		if !found || plugged != want {
			t.Errorf("jackFromGPIO(%s) = %v, %v; want %v, true", state, plugged, found, want)
		}
	}
	if _, found := jackFromGPIO("gpio-1 (x |y ) in  hi\n"); found {
		t.Errorf("jackFromGPIO found a jack pin where there is none")
	}
	if got := defaultSink("Server Name: PulseAudio\nDefault Sink: foo.bar\nDefault Source: x\n"); got != "foo.bar" {
		t.Errorf("defaultSink: got %q", got)
	}
}
