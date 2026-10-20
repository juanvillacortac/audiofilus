package desktop

import (
	"fmt"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// En Windows, "Abrir con" sale del registro del usuario (HKCU, sin pedir
// administrador): un ProgID de audiofilus con su orden de abrir, que cada
// extensión que entiende lista en OpenWithProgids, la entrada de
// Applications (para "Elegir otra aplicación") y, en las carpetas, "Play with
// Audiofilus". El explorador lanza un proceso por archivo elegido: el
// servicio junta los que llegan seguidos en la cola (ver el comando open).

const (
	progID    = "Audiofilus.Audio"
	folderKey = `Directory\shell\Audiofilus`
	classes   = `Software\Classes\`
)

// Extensions son las extensiones que se asocian: el audio que se entiende,
// las listas y las hojas CUE.
func Extensions() []string {
	return append(append([]string{}, decode.Extensions...), ".m3u", ".m3u8", ".pls", ".cue")
}

// openCommand es la orden de abrir con exe.
func openCommand(exe string) string { return fmt.Sprintf(`"%s" "%%1"`, exe) }

// setValue escribe un valor de texto en HKCU\Software\Classes\path.
func setValue(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, classes+path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

// Install registra exe para abrir audio, listas y carpetas.
func Install(exe string) (entry, icon string, err error) {
	cmd, ico := openCommand(exe), fmt.Sprintf(`"%s",0`, exe)
	app := `Applications\` + filepath.Base(exe)
	steps := [][3]string{
		{progID, "", "Audio (Audiofilus)"},
		{progID + `\DefaultIcon`, "", ico},
		{progID + `\shell\open`, "FriendlyAppName", "Audiofilus"},
		{progID + `\shell\open\command`, "", cmd},
		{app, "FriendlyAppName", "Audiofilus"},
		{app + `\DefaultIcon`, "", ico},
		{app + `\shell\open\command`, "", cmd},
		{folderKey, "", "Play with Audiofilus"},
		{folderKey, "Icon", ico},
		{folderKey + `\command`, "", cmd},
	}
	for _, ext := range Extensions() {
		steps = append(steps, [3]string{ext + `\OpenWithProgids`, progID, ""}, [3]string{app + `\SupportedTypes`, ext, ""})
	}
	for _, s := range steps {
		if err := setValue(s[0], s[1], s[2]); err != nil {
			return "", "", fmt.Errorf("registry %s: %w", s[0], err)
		}
	}
	changed()
	return `HKCU\` + classes + progID, `HKCU\` + classes + folderKey, nil
}

// Uninstall quita lo que puso Install.
func Uninstall() error {
	exe := "audiofilus.exe"
	for _, p := range []string{progID, `Applications\` + exe, folderKey} {
		if err := deleteTree(classes + p); err != nil {
			return err
		}
	}
	for _, ext := range Extensions() {
		if k, err := registry.OpenKey(registry.CURRENT_USER, classes+ext+`\OpenWithProgids`, registry.SET_VALUE); err == nil {
			_ = k.DeleteValue(progID)
			k.Close()
		}
	}
	changed()
	return nil
}

// deleteTree borra una clave con todo lo que tiene debajo.
func deleteTree(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if err == registry.ErrNotExist {
		return nil
	}
	if err != nil {
		return err
	}
	subs, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, s := range subs {
		if err := deleteTree(path + `\` + s); err != nil {
			return err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// EnsureRegistered registra exe si no lo estaba, o si estaba otro (se movió
// la carpeta): se llama al abrir la interfaz, porque en Windows audiofilus
// se usa desde su zip, sin instalador.
func EnsureRegistered(exe string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, classes+progID+`\shell\open\command`, registry.QUERY_VALUE)
	if err == nil {
		cur, _, _ := k.GetStringValue("")
		k.Close()
		if cur == openCommand(exe) {
			return
		}
	}
	_, _, _ = Install(exe)
}

// changed avisa al explorador de que cambiaron las asociaciones.
func changed() {
	const shcneAssocChanged = 0x08000000
	proc := syscall.NewLazyDLL("shell32.dll").NewProc("SHChangeNotify")
	if proc.Find() == nil {
		_, _, _ = proc.Call(shcneAssocChanged, 0, 0, 0)
	}
}
