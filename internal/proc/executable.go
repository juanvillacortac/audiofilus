package proc

import (
	"os"
	"strings"
)

// Executable es la ruta de este programa. Tras una actualización en Windows
// el ejecutable en uso quedó renombrado a .old (no se puede pisar uno que
// corre): la ruta buena es la de sin .old, donde está el nuevo.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(exe, ".old"), nil
}
