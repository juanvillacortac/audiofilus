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
	if fileExists(crossMixVersion) || fileExists(arkPause) {
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
