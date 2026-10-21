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
