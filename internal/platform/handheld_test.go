//go:build linux

package platform

import (
	"strings"
	"testing"
)

func lower(s string) string { return strings.ToLower(s) }

// EmuELEC se reconoce por el ID de os-release aunque traiga también los
// campos de CoreELEC y LibreELEC.
func TestOSReleaseID(t *testing.T) {
	cases := map[string]string{
		"NAME=\"EmuELEC\"\nID=\"emuelec\"\nVERSION_ID=\"4.8\"\nLIBREELEC_ARCH=\"Amlogic-ng.aarch64\"\nCOREELEC_DEVICE=\"Amlogic-ng\"\n": "emuelec",
		"NAME=ROCKNIX\nID=rocknix\n":                                "rocknix",
		"PRETTY_NAME=\"Ubuntu 19.10\"\nID=ubuntu\nID_LIKE=debian\n": "ubuntu",
		"": "",
	}
	for in, want := range cases {
		if got := osReleaseID(lower(in)); got != want {
			t.Errorf("osReleaseID(%q) = %q, want %q", in, got, want)
		}
	}
}

// Con AUDIOFILUS_HANDHELD (lo ponen los lanzadores), un sistema que no se
// conoce es una portátil cualquiera; sin ella, nada (el PC).
func TestDetectGenericHandheld(t *testing.T) {
	if fileExists(crossMixVersion) || fileExists(trimUIBin) || fileExists(arkPause) {
		t.Skip("running on a known handheld")
	}
	t.Setenv("AUDIOFILUS_HANDHELD", "")
	if h := detectHandheld(Options{}); h != nil && h.name == "handheld" {
		t.Errorf("without the variable: %q", h.name)
	}
	t.Setenv("AUDIOFILUS_HANDHELD", "1")
	if h := detectHandheld(Options{}); h == nil || h.name != "handheld" && h.name != "rocknix" && h.name != "emuelec" {
		t.Errorf("with the variable: %v", h)
	}
}

// El volumen de TrimUI va al revés en ALSA ("digital volume": 0 es el
// máximo, 40 lo más bajo y 63 silencio): subir de 0 a 100 baja el valor
// crudo, y leerlo de vuelta da el mismo volumen (o casi, por la escala).
func TestTrimUIVolume(t *testing.T) {
	if r := trimUIVolumeRaw(0); r != trimUIVolumeMute {
		t.Errorf("volume 0 -> raw %d, want mute %d", r, trimUIVolumeMute)
	}
	if r := trimUIVolumeRaw(100); r != 0 {
		t.Errorf("volume 100 -> raw %d, want 0", r)
	}
	prev := trimUIVolumeRaw(1)
	for v := 2; v <= 100; v++ {
		r := trimUIVolumeRaw(v)
		if r > prev {
			t.Fatalf("volume %d -> raw %d, louder than %d", v, r, prev)
		}
		prev = r
		if back := trimUIVolumeFromRaw(r); back < v-3 || back > v {
			t.Errorf("volume %d -> raw %d -> %d", v, r, back)
		}
	}
	if v := trimUIVolumeFromRaw(trimUIVolumeMute); v != 0 {
		t.Errorf("mute reads %d", v)
	}
	if m := amixerRaw.FindSubmatch([]byte("Simple mixer control 'digital volume',0\n  Mono: 20 [32%]\n")); m == nil || string(m[1]) != "20" {
		t.Errorf("amixer output not parsed: %q", m)
	}
}
