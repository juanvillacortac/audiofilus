//go:build !windows

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Install escribe la entrada y el icono para el binario bin (audiofilus) y
// avisa a las cachés del escritorio si sus herramientas están.
func Install(bin string) (entry, icon string, err error) {
	entry, icon, err = Paths()
	if err != nil {
		return "", "", err
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{entry, []byte(Entry(bin))}, {icon, Icon}} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return "", "", err
		}
	}
	refresh(filepath.Dir(entry), filepath.Dir(filepath.Dir(filepath.Dir(icon))))
	return entry, icon, nil
}

// Uninstall quita la entrada y el icono.
func Uninstall() error {
	entry, icon, err := Paths()
	if err != nil {
		return err
	}
	for _, p := range []string{entry, icon} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	refresh(filepath.Dir(entry), filepath.Dir(filepath.Dir(filepath.Dir(icon))))
	return nil
}

// refresh actualiza las cachés de entradas e iconos (si las herramientas no
// están, el escritorio las relee solo).
func refresh(apps, icons string) {
	if p, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(p, apps).Run()
	}
	if p, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		_ = exec.Command(p, "-q", "-t", icons).Run()
	}
}

// EnsureRegistered no hace nada fuera de Windows: en Linux la entrada la
// pone "audiofilus desktop install" (o el instalador).
func EnsureRegistered(string) {}
