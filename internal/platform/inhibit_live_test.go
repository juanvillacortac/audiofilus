package platform

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestLogindInhibitLive pide el inhibidor de logind y comprueba que sale en
// la lista (solo con AUDIOFILUS_LOGIND_TESTS=1: necesita systemd).
func TestLogindInhibitLive(t *testing.T) {
	if os.Getenv("AUDIOFILUS_LOGIND_TESTS") == "" {
		t.Skip("set AUDIOFILUS_LOGIND_TESTS=1")
	}
	f, err := logindInhibit()
	if err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("systemd-inhibit", "--list").Output()
	if !strings.Contains(string(out), "audiofilus") {
		t.Errorf("inhibitor not listed:\n%s", out)
	}
	t.Logf("%s", out)
	f.Close()
	out, _ = exec.Command("systemd-inhibit", "--list").Output()
	if strings.Contains(string(out), "audiofilus") {
		t.Error("inhibitor still listed after closing")
	}
}
