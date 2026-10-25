package platform

import (
	"os"
	"strings"
)

// distroHosts son los nombres de equipo que traen de fábrica los sistemas de
// las portátiles: no dicen qué consola es, así que en su lugar va el modelo.
var distroHosts = map[string]bool{
	"knulli": true, "batocera": true, "rocknix": true, "jelos": true, "emuelec": true,
	"muos": true, "arkos": true, "darkos": true, "amberelec": true, "localhost": true,
}

// modelFiles son de donde se saca el modelo de la consola, en orden: el de
// muOS ("rg34xx-sp"), el de Knulli y el del device tree ("Anbernic RG34XX SP").
var modelFiles = []string{
	"/opt/muos/device/config/board/name",
	"/boot/boot/batocera.board",
	"/proc/device-tree/model",
}

// deviceName es el nombre que da la app del teléfono (SetDeviceName).
var deviceName string

// SetDeviceName pone el nombre del aparato cuando el sistema lo sabe mejor
// que el hostname: en Android y en iOS el hostname es "localhost" o nada
// útil, así que la app pasa el nombre que el usuario le dio al teléfono (o
// su modelo).
func SetDeviceName(name string) { deviceName = strings.TrimSpace(name) }

// DeviceName es el nombre con el que el equipo se presenta en la red (MPD,
// DLNA): el nombre del equipo tal cual; en una portátil que conserva el de
// su sistema ("KNULLI", "ROCKNIX"), el modelo de la consola: el que dice
// PortMaster (su DEVICE_NAME, que el lanzador pasa en AUDIOFILUS_DEVICE) o,
// si no, el de modelFiles.
func DeviceName() string {
	if deviceName != "" {
		return deviceName
	}
	h, _ := os.Hostname()
	h = strings.TrimSpace(h)
	if h != "" && !distroHosts[strings.ToLower(h)] {
		return h
	}
	if m := cleanModel(strings.TrimSpace(os.Getenv("AUDIOFILUS_DEVICE"))); m != "" {
		return m
	}
	for _, f := range modelFiles {
		if m := readModel(f); m != "" {
			return m
		}
	}
	if h != "" {
		return h
	}
	return "Audiofilus"
}

// readModel lee un modelo de f, o "" si no hay o es el del chip (los
// kernels de fábrica de las portátiles dicen "sun50iw9" o "Rockchip
// RK3326 EVB"). Los códigos en minúsculas ("rg34xx-sp") van en mayúsculas.
func readModel(f string) string {
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	m := strings.TrimSpace(strings.Trim(string(b), "\x00"))
	return cleanModel(m)
}

// cleanModel descarta los modelos genéricos y pone en mayúsculas los códigos.
func cleanModel(m string) string {
	l := strings.ToLower(m)
	for _, g := range []string{"sun50i", "sun8i", "allwinner", "rockchip", "amlogic", "evb", "generic", "board"} {
		if strings.Contains(l, g) {
			return ""
		}
	}
	if m == l {
		return strings.ToUpper(m)
	}
	return m
}
