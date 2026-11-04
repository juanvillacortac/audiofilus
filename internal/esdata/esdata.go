// Package esdata lee y escribe lo mismo que EmulationStation en Knulli: los
// sistemas (es_systems.cfg), los juegos de cada uno (gamelist.xml), las
// opciones de cada emulador y núcleo (es_features.cfg) y los ajustes que
// guarda en knulli.conf. Es lo que necesita el modo juego de la interfaz
// para mostrar los sistemas, sus juegos y los mismos menús de opciones por
// sistema y por juego que ES, sin depender de ES.
//
// Todo cuelga de una raíz: "/" en la consola, y en el PC una copia de sus
// archivos (AUDIOFILUS_KNULLI_ROOT), para probarlo sin la consola.
package esdata

import (
	"os"
	"path/filepath"
	"strings"
)

// Root es la raíz del sistema de archivos de Knulli: "/" en la consola, o la
// carpeta con una copia de sus archivos.
type Root string

// Path es la ruta absoluta p (como la ve Knulli) dentro de la raíz.
func (r Root) Path(p string) string {
	if r == "" || r == "/" {
		return p
	}
	return filepath.Join(string(r), strings.TrimPrefix(p, "/"))
}

// Live dice si es la consola de verdad (y no una copia): solo entonces se
// mira si los juegos existen y se lanzan.
func (r Root) Live() bool { return r == "" || r == "/" }

// Las rutas de Knulli que se leen, como las ve la consola.
const (
	systemDir   = "/usr/share/emulationstation"
	userESDir   = "/userdata/system/configs/emulationstation"
	confFile    = "/userdata/system/knulli.conf"
	oldConfFile = "/userdata/system/batocera.conf"
)

// firstExisting es la primera de las rutas (de Knulli) que existe en la raíz,
// ya dentro de ella; o "" si no hay ninguna.
func (r Root) firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(r.Path(p)); err == nil {
			return r.Path(p)
		}
	}
	return ""
}

// ConfPath es el archivo de ajustes de Knulli (knulli.conf; batocera.conf en
// las versiones viejas).
func (r Root) ConfPath() string {
	if p := r.firstExisting(confFile, oldConfFile); p != "" {
		return p
	}
	return r.Path(confFile)
}
