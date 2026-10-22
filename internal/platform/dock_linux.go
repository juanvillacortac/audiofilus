package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Las pantallas externas (HDMI, DisplayPort) y los audífonos de cable, leídos
// de sysfs y debugfs: lo barato, para mirarlo a menudo (daemon).

// ExternalDisplay dice si hay una pantalla externa conectada (la consola en
// su base o con un cable HDMI a la tele). Con ella la tapa no apaga nada: se
// juega con mando y la tapa cerrada. Se mira lo que exponen los kernels de
// las portátiles: DRM (card*-HDMI-A-1/status = connected), extcon
// (state con "HDMI=1") y el switch de Android de los kernels de fabricante
// (switch/hdmi/state = 1).
func ExternalDisplay() bool {
	// Primero lo que solo lee un valor (extcon, switch); el estado de DRM al
	// final: en algunos kernels leerlo vuelve a sondear la conexión.
	ext, _ := filepath.Glob("/sys/class/extcon/*/state")
	for _, f := range ext {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(strings.ToUpper(string(b)), "HDMI=1") {
			return true
		}
	}
	for _, f := range []string{"/sys/class/switch/hdmi/state", "/sys/devices/virtual/switch/hdmi/state"} {
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == "1" {
			return true
		}
	}
	drm, _ := filepath.Glob("/sys/class/drm/card*-*/status")
	for _, f := range drm {
		name := strings.ToUpper(filepath.Base(filepath.Dir(f)))
		if !strings.Contains(name, "HDMI") && !strings.Contains(name, "DP-") {
			continue // el panel de la consola (DSI) siempre está conectado
		}
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == "connected" {
			return true
		}
	}
	return false
}

// WiredJack dice si hay audífonos de cable enchufados, y si se sabe: el pin
// de detección de las H700 (debugfs), el archivo de muOS o el "Playback Path"
// de ArkOS. Es más barato que AudioOutput (sin pactl) para mirarlo cada
// segundo mientras suena.
func WiredJack() (plugged, known bool) {
	if _, err := os.Stat(muosRun); err == nil {
		_, err := os.Stat(muosRun + "/headphones")
		return err == nil, true
	}
	if b, err := os.ReadFile(gpioDebug); err == nil {
		if plugged, found := jackFromGPIO(string(b)); found {
			return plugged, true
		}
	}
	if fileExists(arkPause) {
		switch out := AudioOutput(); out {
		case OutputWired:
			return true, true
		case OutputSpeaker:
			return false, true
		}
	}
	return false, false
}

// SetWakeAlarm programa el reloj de la consola (RTC) para que la despierte
// de la suspensión a la hora t (cero: lo quita). Pide root: directo si ya
// lo es, si no con sudo (ArkOS).
func SetWakeAlarm(t time.Time) error {
	const f = "/sys/class/rtc/rtc0/wakealarm"
	if _, err := os.Stat(f); err != nil {
		return nil // sin reloj que despierte: nada que hacer
	}
	// Primero se borra (si hay una puesta, no deja poner otra).
	writeSys(f, "0")
	if t.IsZero() {
		return nil
	}
	writeSys(f, strconv.FormatInt(t.Unix(), 10))
	return nil
}
