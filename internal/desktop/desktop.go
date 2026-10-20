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

// MimeTypes son los tipos que abre audiofilus desde el explorador
// ("Abrir con"): el audio que entiende, las listas, las hojas CUE, la música
// de consola y las carpetas (Exec recibe %F: uno o varios archivos o
// carpetas).
var MimeTypes = []string{
	"audio/mpeg", "audio/mp3", "audio/x-mp3", "audio/flac", "audio/x-flac",
	"audio/ogg", "audio/x-vorbis+ogg", "audio/vorbis", "audio/opus", "audio/x-opus+ogg",
	"audio/wav", "audio/x-wav", "audio/vnd.wave", "audio/aac", "audio/mp4", "audio/x-m4a",
	"audio/m4a", "audio/aiff", "audio/x-aiff", "audio/x-aifc", "audio/x-dsf", "audio/x-dff",
	"audio/x-mod", "audio/x-xm", "audio/x-it", "audio/x-s3m",
	"audio/x-nsf", "audio/x-nsfe", "audio/x-spc", "audio/x-gbs", "audio/x-vgm", "audio/x-vgz",
	"audio/x-gym", "audio/x-hes", "audio/x-kss", "audio/x-ay", "audio/x-sap",
	"audio/x-mpegurl", "audio/mpegurl", "application/vnd.apple.mpegurl", "audio/x-scpls",
	"application/x-cue", "inode/directory",
}

// Entry es el contenido del .desktop para el binario bin (la interfaz, o lo
// que se abra con ella; las acciones, sus comandos).
func Entry(bin string) string {
	q := func(p string) string { return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"` }
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Audiofilus
GenericName=Music Player
Comment=Pixel-art music player that keeps playing in the background
Exec=%s %%F
Icon=%s
Terminal=false
Categories=AudioVideo;Audio;Player;
MimeType=%s;
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
`, q(bin), ID, strings.Join(MimeTypes, ";"), ID, q(bin), q(bin), q(bin))
}
