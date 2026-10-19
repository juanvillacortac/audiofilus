package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall comprueba que la entrada apunte al binario (y sus acciones a sus
// comandos), con su clase de ventana, y que Uninstall la quite.
func TestInstall(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "con espacio")
	os.MkdirAll(dir, 0o755)
	bin := filepath.Join(dir, "audiofilus")
	entry, icon, err := Install(bin)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(entry)
	for _, want := range []string{`Exec="` + bin + `"` + "\n", "StartupWMClass=audiofilus", `Exec="` + bin + `" toggle`, "Icon=audiofilus"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("entry lacks %q:\n%s", want, b)
		}
	}
	if fi, err := os.Stat(icon); err != nil || fi.Size() != int64(len(Icon)) {
		t.Errorf("icon not written: %v", err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("entry still there after Uninstall")
	}
}
