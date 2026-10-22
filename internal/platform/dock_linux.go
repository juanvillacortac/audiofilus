package platform

import (
	"errors"
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
		// "1" en los kernels de Android; "HDMI=1" en el de la RG34XX-SP.
		if b, err := os.ReadFile(f); err == nil {
			if v := strings.TrimSpace(string(b)); v == "1" || strings.Contains(strings.ToUpper(v), "HDMI=1") {
				return true
			}
		}
	}
	// muOS (cable.0: "1") y las cajas Amlogic (EmuELEC: hpd_state "1").
	for _, f := range []string{"/sys/class/extcon/hdmi/cable.0/state", "/sys/class/amhdmitx/amhdmitx0/hpd_state"} {
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

// WiredJack dice si hay audífonos de cable enchufados, y si se sabe:
// extcon (HEADPHONE=), el pin de detección de las H700 (debugfs), el archivo
// de muOS o el "Playback Path" de ArkOS. Es más barato que AudioOutput (sin pactl) para mirarlo cada
// segundo mientras suena.
func WiredJack() (plugged, known bool) {
	// extcon (la RG34XX-SP con Knulli: extcon0 "HEADPHONE=0"): lo estándar y
	// lo más barato.
	ext, _ := filepath.Glob("/sys/class/extcon/*/state")
	for _, f := range ext {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Fields(strings.ToUpper(string(b))) {
			if v, ok := strings.CutPrefix(line, "HEADPHONE="); ok {
				return v == "1", true
			}
		}
	}
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

// Archivos del despertador: la hora (Unix) a la que hay que despertar y el
// paso de pm-utils que la vuelve a programar al suspender.
const (
	wakeEpoch = "/var/run/audiofilus-alarm.epoch"
	wakeHook  = "/etc/pm/sleep.d/05-audiofilus-alarm"
)

// wakeHookScript es el paso de pm-utils (Knulli suspende con pm-suspend).
// Al suspender: knulli-suspend borra las alarmas de todos los relojes justo
// antes, así que se vuelve a poner la de audiofilus en los que pueden
// despertar la consola, salvo que ya haya una antes (la de Knulli para
// apagarse tras horas suspendida). Al despertar para la alarma: con la tapa
// cerrada Knulli la vuelve a suspender un segundo después; se le quita el
// cerrojo de knulli-suspend un rato, hasta que audiofilus suena (y desde ahí
// la mantiene despierta él).
const wakeHookScript = `#!/bin/sh
# audiofilus: re-arms the alarm's wake time, which knulli-suspend clears
# before suspending. Removed when the alarm is turned off.
case "$1" in
suspend|hibernate)
	A=$(cat ` + wakeEpoch + ` 2>/dev/null)
	[ -n "$A" ] || exit 0
	left=$(( A - $(date +%s) ))
	[ "$left" -gt 0 ] || exit 0
	for r in /sys/class/rtc/rtc*; do
		[ "$(cat "$r/device/power/wakeup" 2>/dev/null)" = enabled ] || continue
		# Each RTC keeps its own time (the H700's starts in 1970).
		at=$(( $(cat "$r/since_epoch") + left ))
		cur=$(cat "$r/wakealarm" 2>/dev/null)
		if [ -z "$cur" ] || [ "$cur" -gt "$at" ]; then
			echo 0 > "$r/wakealarm"
			echo "$at" > "$r/wakealarm"
		fi
	done
	;;
resume|thaw)
	# Woken for the alarm: Knulli suspends again on resume if the lid is
	# closed (99-postresume-sp runs knulli-suspend a second later). Holding
	# knulli-suspend's lock for a while makes it give up until audiofilus
	# starts playing (from then on it keeps the console awake itself).
	A=$(cat ` + wakeEpoch + ` 2>/dev/null)
	[ -n "$A" ] || exit 0
	d=$(( $(date +%s) - A ))
	if [ "$d" -ge -60 ] && [ "$d" -le 600 ]; then
		( flock -x 9; sleep 90 ) 9>/var/run/knulli-suspend.lock >/dev/null 2>&1 &
	fi
	;;
esac
exit 0
`

// wakeRTCs son los relojes que pueden despertar la consola (los que tienen
// el despertar activado); si ninguno lo dice, rtc0.
func wakeRTCs() []string {
	all, _ := filepath.Glob("/sys/class/rtc/rtc*")
	var out []string
	for _, r := range all {
		if b, err := os.ReadFile(filepath.Join(r, "device", "power", "wakeup")); err == nil && strings.TrimSpace(string(b)) == "enabled" {
			out = append(out, r)
		}
	}
	if len(out) == 0 && fileExists("/sys/class/rtc/rtc0/wakealarm") {
		out = []string{"/sys/class/rtc/rtc0"}
	}
	return out
}

// SetWakeAlarm programa los relojes de la consola (RTC) para que la
// despierten de la suspensión a la hora t (cero: lo quita). En la RG34XX-SP
// el que despierta es el del chip (rtc1, sunxi), no rtc0. Donde la
// suspensión pasa por pm-utils (Knulli), que borra las alarmas antes de
// dormir, se deja además un paso en /etc/pm/sleep.d que la vuelve a poner.
// Pide root: directo si ya lo es, si no con sudo (ArkOS).
func SetWakeAlarm(t time.Time) error {
	rtcs := wakeRTCs()
	for _, r := range rtcs {
		writeSys(filepath.Join(r, "wakealarm"), "0") // si hay una puesta, no deja poner otra
		if t.IsZero() {
			continue
		}
		// Cada reloj lleva su hora (el del H700 cuenta desde 1970 si nadie
		// lo puso en hora): la alarma va en la suya, a lo que falte.
		at := t.Unix()
		if b, err := os.ReadFile(filepath.Join(r, "since_epoch")); err == nil {
			if now, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
				at = now + int64(time.Until(t).Seconds())
			}
		}
		writeSys(filepath.Join(r, "wakealarm"), strconv.FormatInt(at, 10))
	}
	if t.IsZero() {
		_ = os.Remove(wakeEpoch)
		_ = os.Remove(wakeHook)
		return nil
	}
	if err := os.WriteFile(wakeEpoch, []byte(strconv.FormatInt(t.Unix(), 10)), 0o644); err != nil {
		return err
	}
	if fileExists(filepath.Dir(wakeHook)) {
		if b, err := os.ReadFile(wakeHook); err != nil || string(b) != wakeHookScript {
			if err := os.WriteFile(wakeHook, []byte(wakeHookScript), 0o755); err != nil {
				return err
			}
		}
	}
	if len(rtcs) == 0 {
		return errors.New("no RTC can wake the console")
	}
	return nil
}
