// Package desktop integra audiofilus con el escritorio Linux (freedesktop):
// la entrada del lanzador (.desktop, con acciones de reproducir, siguiente y
// anterior) y el icono, en las carpetas del usuario. Es lo que hace que la app
// aparezca en el lanzador de Omarchy (y de GNOME, KDE...), que Hyprland la
// reconozca por su clase de ventana y que la barra muestre su icono con lo que
// suena (MPRIS, DesktopEntry "audiofilus").
package desktop

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ID es el nombre de la app para el escritorio: el del .desktop, el del icono,
// la clase de la ventana y la DesktopEntry de MPRIS.
const ID = "audiofilus"

// Icon es el icono (256×256, el CD del logo ampliado ×8 sin suavizar).
//
//go:embed icon.png
var Icon []byte

// dataHome es $XDG_DATA_HOME o ~/.local/share.
func dataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Paths son dónde quedan la entrada y el icono.
func Paths() (entry, icon string, err error) {
	d, err := dataHome()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(d, "applications", ID+".desktop"),
		filepath.Join(d, "icons", "hicolor", "256x256", "apps", ID+".png"), nil
}

// Entry es el contenido del .desktop para la interfaz ui y el servicio cli.
func Entry(ui, cli string) string {
	q := func(p string) string { return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"` }
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Audiofilus
GenericName=Music Player
Comment=Pixel-art music player that keeps playing in the background
Exec=%s
Icon=%s
Terminal=false
Categories=AudioVideo;Audio;Player;
Keywords=music;player;mp3;flac;radio;podcast;subsonic;jellyfin;
StartupWMClass=%s
Actions=PlayPause;Next;Previous;

[Desktop Action PlayPause]
Name=Play/Pause
Exec=%s toggle

[Desktop Action Next]
Name=Next track
Exec=%s next

[Desktop Action Previous]
Name=Previous track
Exec=%s prev
`, q(ui), ID, ID, q(cli), q(cli), q(cli))
}

// Install escribe la entrada y el icono para los binarios de dir (la carpeta
// donde están audiofilus y audiofilus-ui) y avisa a las cachés del escritorio
// si sus herramientas están.
func Install(dir string) (entry, icon string, err error) {
	entry, icon, err = Paths()
	if err != nil {
		return "", "", err
	}
	ui := filepath.Join(dir, "audiofilus-ui")
	if _, err := os.Stat(ui); err != nil {
		return "", "", fmt.Errorf("audiofilus-ui not found next to audiofilus in %s", dir)
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{entry, []byte(Entry(ui, filepath.Join(dir, "audiofilus")))}, {icon, Icon}} {
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
