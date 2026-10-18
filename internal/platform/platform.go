// Package platform adapta audiofilus al sistema donde corre. En el escritorio
// todo es un no-op; en Knulli mantiene despierta la portátil mientras la app
// está abierta e implementa el modo bolsillo.
package platform

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Platform es la integración con el sistema que usa el daemon.
type Platform interface {
	Name() string
	// SetAwake impide (true) o devuelve (false) la suspensión por inactividad,
	// por cerrar la tapa y por la pulsación corta del botón de encendido. El
	// daemon lo pide mientras la app está abierta, suene o no; en segundo plano
	// la consola se suspende como siempre.
	SetAwake(awake bool)
	// EnterSleep es el sleep de la app: ahorra energía y, con screenOff, apaga
	// la retroiluminación (sin él, la interfaz solo pinta negro: modo OLED).
	EnterSleep(screenOff bool) error
	// ExitSleep deshace EnterSleep.
	ExitSleep() error
	Rumble(d time.Duration)
	// SystemVolume es el volumen del sistema en porcentaje (0-100), o -1 si no
	// se puede leer; SetSystemVolume lo cambia.
	SystemVolume() int
	SetSystemVolume(v int) error
	// SetVolumeKeys hace (true) que los botones de volumen de la consola pasen
	// por el servicio (comando volstep) mientras la app está abierta.
	SetVolumeKeys(on bool)
	// LidClosed dice si la tapa está cerrada (false si no hay tapa o no se
	// sabe): con la tapa cerrada, el botón de encendido no despierta.
	LidClosed() bool
	// Cleanup lo deshace todo; se llama al salir.
	Cleanup()
}

// Options ajustan la implementación de Knulli.
type Options struct {
	// Governor es el governor de CPU durante el sleep: vacío es "powersave";
	// "keep" no lo toca.
	Governor string
}

// Detect devuelve la implementación del sistema en el que corre.
func Detect(opts Options) Platform {
	if k := detectKnulli(opts); k != nil {
		return k
	}
	if m := detectMuOS(opts); m != nil {
		return m
	}
	if h := detectHandheld(opts); h != nil {
		return h
	}
	return desktop{}
}

// desktop es la plataforma del PC: solo deja constancia en el log.
type desktop struct{}

func (desktop) Name() string { return "desktop" }
func (desktop) SetAwake(awake bool) {
	log.Printf("keep awake: %v (desktop: no-op)", awake)
}
func (desktop) EnterSleep(bool) error {
	log.Print("sleep (desktop: screen left alone)")
	return nil
}
func (desktop) ExitSleep() error       { log.Print("sleep ended"); return nil }
func (desktop) Rumble(d time.Duration) { log.Printf("rumble %v", d) }
func (desktop) Cleanup()               {}
func (desktop) SystemVolume() int      { return pactlVolume() }
func (desktop) LidClosed() bool        { return false }
func (desktop) SetVolumeKeys(bool)     {}
func (desktop) SetSystemVolume(v int) error {
	return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", strconv.Itoa(v)+"%").Run()
}

// pactlVolume lee el volumen de la salida por defecto con pactl.
func pactlVolume() int {
	out, err := exec.Command("pactl", "get-sink-volume", "@DEFAULT_SINK@").Output()
	if err != nil {
		return -1
	}
	// "Volume: front-left: 26214 /  40% / -23.88 dB, ..."
	for _, f := range strings.Fields(string(out)) {
		if strings.HasSuffix(f, "%") {
			if v, err := strconv.Atoi(strings.TrimSuffix(f, "%")); err == nil {
				return v
			}
		}
	}
	return -1
}
