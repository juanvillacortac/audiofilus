//go:build linux

package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/godbus/dbus/v5"
)

// Las portátiles con systemd: ROCKNIX, la familia ArkOS (ArkOS y dArkOS) y
// EmuELEC.
// Comparten casi todo (sysfs para el brillo y el governor, logind), así que
// es una sola plataforma con sus diferencias (sacadas de su código:
// github.com/ROCKNIX/distribution, github.com/christianhaitian/dArkOS y
// github.com/EmuELEC/EmuELEC):
//
//   - ROCKNIX deja la tapa y el botón de encendido a logind
//     (HandleLidSwitch=suspend, HandlePowerKey=suspend): un inhibidor de
//     logind ("block" de sleep, idle, handle-power-key y handle-lid-switch)
//     los para, que es lo que hacen los reproductores en el escritorio. Con su
//     "suspensión falsa" (suspendmode off) su input_sense silencia y congela
//     todo al pulsar el botón, sin forma de saltárselo.
//   - ArkOS/dArkOS ignoran el botón en logind (HandlePowerKey=ignore): lo lleva
//     ogage, que llama a /usr/local/bin/pause.sh (sudo systemctl suspend), y
//     auto_suspend.py suspende por inactividad salvo que
//     /usr/local/bin/processcheck.sh diga "Running" (hoy solo con Kodi o
//     ffplay). Con la app abierta se montan encima copias que no suspenden y
//     que dicen "Running", como en Knulli; se deshacen al cerrar (y tras un
//     cierre inesperado, al volver a arrancar). Los ports corren como el
//     usuario ark: lo que pide root va con sudo -n (ark lo tiene sin
//     contraseña).
//   - EmuELEC (Odroid Go Advance/Super, GameForce Chi y las cajas Amlogic,
//     que no suspenden) también ignora el botón en logind: lo lleva
//     oga_events.py, que como root llama a systemctl suspend. Como root,
//     logind no respeta los inhibidores, así que con la app abierta se deja
//     un sleep.conf con AllowSuspend=no (logind lo relee en cada petición:
//     el suspend falla y el botón queda para el sleep de la app). Está en
//     /storage, que no se borra al apagar: se quita al cerrar y, tras un
//     cierre inesperado, al volver a arrancar. No suspende por inactividad.
//     Todo corre como root.
//   - Brillo: /sys/class/backlight. Volumen: en ROCKNIX, pactl (y su script
//     "volume", que lo guarda); en ArkOS y EmuELEC, ALSA con amixer.
//   - Tapa: el sensor del AXP (hallkey) o, con kernel mainline (ROCKNIX), el
//     interruptor SW_LID por evdev.
//
// Y dos más sin integración tan honda:
//
//   - TrimUI ("trimui": las Smart Pro y Brick con su sistema o con CrossMix,
//     github.com/cizia64/CrossMix-OS): la pantalla se apaga y enciende por
//     /sys/kernel/debug/dispdbg y los LEDs por /sys/class/led_anim, como
//     los lcd_off.sh y lcd_on.sh de CrossMix; al encender vuelve al brillo
//     de sus ajustes (/mnt/UDISK/system.json). El volumen es el control
//     "digital volume" de ALSA, que va al revés (0 es el máximo). El botón
//     de encendido lo lleva el sistema (el MainUI de TrimUI, cerrado) y
//     suspende como siempre: para el bolsillo está el sleep de la app
//     (Start dos veces).
//   - Una portátil cualquiera ("handheld"), cuando el lanzador lo pide con
//     AUDIOFILUS_HANDHELD=1 y no es ninguno de los sistemas que se conocen:
//     el sleep de la app apaga el brillo por /sys/class/backlight y baja el
//     governor, sin tocar la suspensión.

const (
	// crossMixVersion es el archivo con la versión de CrossMix y trimUIBin
	// la carpeta de los programas de TrimUI (con su sistema o con CrossMix).
	crossMixVersion = "/mnt/SDCARD/System/usr/trimui/crossmix-version.txt"
	trimUIBin       = "/usr/trimui/bin"
	// trimUIDisp es la interfaz de depuración del driver de pantalla de las
	// Allwinner, por la que TrimUI cambia el brillo; trimUILeds, el brillo
	// máximo de los LEDs de los sticks; trimUISettings, sus ajustes.
	trimUIDisp      = "/sys/kernel/debug/dispdbg"
	trimUILeds      = "/sys/class/led_anim/max_scale"
	trimUISettings  = "/mnt/UDISK/system.json"
	arkPause        = "/usr/local/bin/pause.sh"
	arkProcessCheck = "/usr/local/bin/processcheck.sh"
	arkMounts       = "/tmp/audiofilus-arkos-mounts"
	// eeNoSleep es el sleep.conf que pone EmuELEC sin suspensión (va detrás
	// del suyo, sleep.conf, así que manda).
	eeNoSleep = "/storage/.config/sleep.conf.d/zz-audiofilus.conf"
)

// handheld es ROCKNIX, la familia ArkOS, EmuELEC, TrimUI o una portátil
// cualquiera.
type handheld struct {
	name string // "rocknix", "arkos", "emuelec", "trimui" o "handheld"
	opts Options

	mu            sync.Mutex
	awake         bool     // SetAwake(true) puesto (con o sin inhibidor)
	inhibit       *os.File // el inhibidor de logind (se suelta al cerrarlo)
	mounted       []string
	sleeping      bool
	savedBright   map[string]string
	savedGovernor string
	frozenKeys    []int
	trimUIOff     bool   // la pantalla de TrimUI, apagada por trimUIScreen
	savedLeds     string // el brillo de los LEDs antes de apagarlos
}

// detectHandheld reconoce ROCKNIX y EmuELEC por /etc/os-release, la familia
// ArkOS por su pause.sh y su usuario ark, y TrimUI por sus programas.
func detectHandheld(opts Options) *handheld {
	osr, _ := os.ReadFile("/etc/os-release")
	id := strings.ToLower(string(osr))
	switch {
	case strings.Contains(id, "rocknix"):
		return &handheld{name: "rocknix", opts: opts}
	case osReleaseID(id) == "emuelec":
		// Si quedó de una vez anterior que terminó mal, la consola no
		// suspendería nunca.
		_ = os.Remove(eeNoSleep)
		return &handheld{name: "emuelec", opts: opts}
	case fileExists(arkPause) && (fileExists("/home/ark") || strings.Contains(id, "arkos")):
		h := &handheld{name: "arkos", opts: opts}
		h.unmountLeftovers()
		return h
	case fileExists(crossMixVersion) || fileExists(trimUIBin):
		return &handheld{name: "trimui", opts: opts}
	case os.Getenv("AUDIOFILUS_HANDHELD") != "":
		return &handheld{name: "handheld", opts: opts}
	}
	return nil
}

// osReleaseID es el ID= de un /etc/os-release (en minúsculas): EmuELEC trae
// también los campos de CoreELEC y LibreELEC, así que se mira el ID.
func osReleaseID(osr string) string {
	for _, l := range strings.Split(osr, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "id="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (h *handheld) Name() string { return h.name }

// asRoot ejecuta un comando como root: directo si ya lo es, si no con
// sudo -n (sin pedir contraseña).
func asRoot(name string, args ...string) error {
	if os.Geteuid() != 0 {
		args = append([]string{"-n", name}, args...)
		name = "sudo"
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v %s", name, args, err, bytes.TrimSpace(out))
	}
	return nil
}

// writeSys escribe en un archivo de sysfs; si no hay permiso, con sudo.
func writeSys(path, value string) {
	if os.WriteFile(path, []byte(value), 0o644) == nil {
		return
	}
	if err := asRoot("sh", "-c", `printf %s "$1" > "$2"`, "sh", value, path); err != nil {
		log.Printf("write %s: %v", path, err)
	}
}

// SetAwake impide (o deja) la suspensión: el inhibidor de logind, en ArkOS
// las copias de pause.sh y processcheck.sh, y en EmuELEC su sleep.conf.
func (h *handheld) SetAwake(awake bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Se mira awake, no el inhibidor: logind puede negarlo (en dArkOS los
	// ports corren como ark, y pedirlo exige autorización), y entonces no
	// se quitaba nunca la copia de pause.sh y el botón de encendido no
	// suspendía con la música en segundo plano.
	if awake && !h.awake {
		h.awake = true
		// En ArkOS sin root, logind no lo da (y no hace falta: el botón lo
		// lleva ogage, y la copia de pause.sh ya no suspende).
		// TrimUI no tiene logind, y la portátil cualquiera se deja como
		// está.
		if h.name == "trimui" || h.name == "handheld" {
			return
		}
		if h.name == "emuelec" {
			h.noSleepLocked(true)
		} else if h.name != "arkos" || os.Geteuid() == 0 {
			f, err := logindInhibit()
			if err != nil {
				log.Printf("logind inhibit: %v", err)
			}
			h.inhibit = f
		}
		if h.name == "arkos" {
			h.bindLocked(arkPause, "#!/bin/sh\n# audiofilus is open: it handles the power button (its app sleep)\nexit 0\n")
			h.bindLocked(arkProcessCheck, "#!/bin/sh\n# audiofilus is open: no idle suspend\nprintf Running\n")
		}
		return
	}
	if !awake && h.awake {
		h.awake = false
		if h.name == "emuelec" {
			h.noSleepLocked(false)
		}
		if h.inhibit != nil {
			h.inhibit.Close()
			h.inhibit = nil
		}
		for i := len(h.mounted) - 1; i >= 0; i-- {
			_ = asRoot("umount", "-l", h.mounted[i])
		}
		h.mounted = nil
		_ = os.Remove(arkMounts)
	}
}

// noSleepLocked pone (o quita) el sleep.conf de EmuELEC que no deja
// suspender.
func (h *handheld) noSleepLocked(on bool) {
	if !on {
		if err := os.Remove(eeNoSleep); err != nil && !os.IsNotExist(err) {
			log.Printf("emuelec sleep.conf: %v", err)
		}
		return
	}
	conf := "# audiofilus is open: it handles the power button (its app sleep).\n# Removed when it closes.\n[Sleep]\nAllowSuspend=no\n"
	if err := os.MkdirAll(filepath.Dir(eeNoSleep), 0o755); err == nil {
		err = os.WriteFile(eeNoSleep, []byte(conf), 0o644)
		if err == nil {
			return
		}
	}
	log.Printf("emuelec sleep.conf: could not write %s", eeNoSleep)
}

// logindInhibit pide a logind que no suspenda (ni por el botón, la tapa o la
// inactividad) mientras el archivo que devuelve siga abierto.
func logindInhibit() (*os.File, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	var fd dbus.UnixFD
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	err = obj.Call("org.freedesktop.login1.Manager.Inhibit", 0,
		"sleep:idle:handle-power-key:handle-suspend-key:handle-lid-switch", "Audiofilus", "Music playing with the app open", "block").Store(&fd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "logind-inhibit"), nil
}

// bindLocked monta encima de target una copia con content (y lo apunta, para
// deshacerlo tras un cierre inesperado).
func (h *handheld) bindLocked(target, content string) {
	if !fileExists(target) {
		return
	}
	src := filepath.Join(os.TempDir(), "audiofilus-"+filepath.Base(target))
	if err := os.WriteFile(src, []byte(content), 0o755); err != nil {
		log.Printf("bind %s: %v", target, err)
		return
	}
	if err := asRoot("mount", "--bind", src, target); err != nil {
		log.Printf("bind %s: %v", target, err)
		return
	}
	h.mounted = append(h.mounted, target)
	_ = os.WriteFile(arkMounts, []byte(strings.Join(h.mounted, "\n")), 0o644)
}

// unmountLeftovers deshace los montajes de una vez anterior que terminó mal.
func (h *handheld) unmountLeftovers() {
	b, err := os.ReadFile(arkMounts)
	if err != nil {
		return
	}
	for _, t := range strings.Fields(string(b)) {
		_ = asRoot("umount", "-l", t)
	}
	_ = os.Remove(arkMounts)
}

// backlights son los brillos de las pantallas.
func backlights() []string {
	m, _ := filepath.Glob("/sys/class/backlight/*/brightness")
	return m
}

func (h *handheld) EnterSleep(screenOff bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sleeping {
		return nil
	}
	h.sleeping = true
	if screenOff && h.name == "trimui" {
		h.trimUIScreen(false)
	} else if screenOff {
		h.savedBright = map[string]string{}
		if len(backlights()) == 0 {
			log.Print("sleep: no backlight in /sys/class/backlight")
		}
		for _, b := range backlights() {
			if v, err := os.ReadFile(b); err == nil {
				h.savedBright[b] = strings.TrimSpace(string(v))
				writeSys(b, "0")
			}
		}
	}
	if g := orDefault(h.opts.Governor, "powersave"); g != "keep" {
		h.savedGovernor = readGovernor()
		setGovernor(g)
	}
	h.frozenKeys = uiKeyMappers()
	for _, pid := range h.frozenKeys {
		if syscall.Kill(pid, syscall.SIGSTOP) != nil {
			_ = asRoot("kill", "-STOP", strconv.Itoa(pid))
		}
	}
	return nil
}

func (h *handheld) ExitSleep() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.sleeping {
		return nil
	}
	h.sleeping = false
	for _, pid := range h.frozenKeys {
		if syscall.Kill(pid, syscall.SIGCONT) != nil {
			_ = asRoot("kill", "-CONT", strconv.Itoa(pid))
		}
	}
	h.frozenKeys = nil
	if h.savedGovernor != "" {
		setGovernor(h.savedGovernor)
		h.savedGovernor = ""
	}
	for b, v := range h.savedBright {
		if v == "0" {
			v = "100"
		}
		writeSys(b, v)
	}
	h.savedBright = nil
	if h.name == "trimui" {
		h.trimUIScreen(true)
	}
	return nil
}

// trimUIScreen apaga o enciende la pantalla y los LEDs de una TrimUI, como
// los lcd_off.sh y lcd_on.sh de CrossMix. Encender solo hace falta si se
// apagó: con el sleep en modo OLED la pantalla quedó encendida.
func (h *handheld) trimUIScreen(on bool) {
	if on == !h.trimUIOff {
		return
	}
	if !on {
		if b, err := os.ReadFile(trimUILeds); err == nil {
			h.savedLeds = strings.TrimSpace(string(b))
		}
		trimUIBacklight(0)
		writeSys(trimUILeds, "0")
		h.trimUIOff = true
		log.Printf("trimui: screen off (leds were %q)", h.savedLeds)
		return
	}
	v := trimUIBrightness()
	trimUIBacklight(v)
	if h.savedLeds != "" && h.savedLeds != "0" {
		writeSys(trimUILeds, h.savedLeds)
	}
	h.trimUIOff = false
	log.Printf("trimui: screen on (backlight %d)", v)
}

// trimUIBacklight pone el brillo de la pantalla (0 a 255) por dispdbg.
func trimUIBacklight(v int) {
	for _, w := range [][2]string{{"name", "lcd0"}, {"command", "setbl"}, {"param", strconv.Itoa(v)}, {"start", "1"}} {
		writeSys(filepath.Join(trimUIDisp, w[0]), w[1])
	}
}

// trimUIBrightness es el brillo de los ajustes de TrimUI (0 a 10) en la
// escala de la pantalla, como lo calcula lcd_on.sh: por 23, y al menos 4 para
// que el 0 se vea.
func trimUIBrightness() int {
	var s struct {
		Brightness *int `json:"brightness"`
	}
	b, _ := os.ReadFile(trimUISettings)
	if json.Unmarshal(b, &s) != nil || s.Brightness == nil {
		return 160
	}
	return max(*s.Brightness*23, 4)
}

// Rumble vibra si la consola tiene el motor del AXP (las H700); si no, nada.
func (h *handheld) Rumble(d time.Duration) {
	if !fileExists(rumbleFile) {
		return
	}
	writeSys(rumbleFile, "1")
	time.AfterFunc(d, func() { writeSys(rumbleFile, "0") })
}

// amixerControls son los controles de volumen de ALSA que se prueban (ArkOS
// usa "Playback" en las RK3326 y "Master" en otras).
var amixerControls = []string{"Playback", "Master", "Headphone", "PCM"}

var amixerPct = regexp.MustCompile(`\[(\d+)%\]`)

// SystemVolume es el volumen del sistema: pactl en ROCKNIX, amixer en ArkOS y
// EmuELEC.
func (h *handheld) SystemVolume() int {
	if h.name == "rocknix" {
		return pactlVolume()
	}
	if h.name == "trimui" {
		return trimUIVolume()
	}
	for _, c := range amixerControls {
		out, err := exec.Command("amixer", "sget", c).Output()
		if err != nil {
			continue
		}
		if m := amixerPct.FindSubmatch(out); m != nil {
			v, _ := strconv.Atoi(string(m[1]))
			return v
		}
	}
	return -1
}

// SetSystemVolume lo cambia: en ROCKNIX con su script "volume" (que lo
// guarda) o pactl; en ArkOS y EmuELEC con amixer.
func (h *handheld) SetSystemVolume(v int) error {
	if h.name == "rocknix" {
		if fileExists("/usr/bin/volume") {
			return exec.Command("/usr/bin/volume", strconv.Itoa(v)).Run()
		}
		return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", strconv.Itoa(v)+"%").Run()
	}
	if h.name == "trimui" {
		return exec.Command("amixer", "-q", "-c", "0", "sset", trimUIVolumeControl, strconv.Itoa(trimUIVolumeRaw(v))).Run()
	}
	for _, c := range amixerControls {
		if exec.Command("amixer", "-q", "sset", c, strconv.Itoa(v)+"%").Run() == nil {
			return nil
		}
	}
	return fmt.Errorf("amixer: no volume control")
}

// trimUIVolumeControl es el volumen de TrimUI en ALSA. Es una atenuación: 0
// es el máximo, 40 lo más bajo que usa su sistema y 63 silencio.
const (
	trimUIVolumeControl = "digital volume"
	trimUIVolumeMin     = 40
	trimUIVolumeMute    = 63
)

// amixerRaw es el valor crudo de un control en la salida de amixer sget
// ("Mono: 20 [68%]" o "Front Left: Playback 20 [68%]").
var amixerRaw = regexp.MustCompile(`:\s+(?:Playback\s+)?(\d+)\s+\[`)

// trimUIVolumeRaw pasa un volumen de 0 a 100 al valor de "digital volume",
// como el preload.sh de CrossMix.
func trimUIVolumeRaw(v int) int {
	if v <= 0 {
		return trimUIVolumeMute
	}
	return trimUIVolumeMin - min(v, 100)*trimUIVolumeMin/100
}

// trimUIVolumeFromRaw es lo contrario.
func trimUIVolumeFromRaw(raw int) int {
	if raw >= trimUIVolumeMin {
		return 0
	}
	return (trimUIVolumeMin - raw) * 100 / trimUIVolumeMin
}

// trimUIVolume lee el volumen de TrimUI (0 a 100), o -1.
func trimUIVolume() int {
	out, err := exec.Command("amixer", "-c", "0", "sget", trimUIVolumeControl).Output()
	if err != nil {
		return -1
	}
	m := amixerRaw.FindSubmatch(out)
	if m == nil {
		return -1
	}
	raw, _ := strconv.Atoi(string(m[1]))
	return trimUIVolumeFromRaw(raw)
}

// SetVolumeKeys no hace nada: los botones de volumen los llevan input_sense
// (ROCKNIX), ogage (ArkOS) y oga_events.py (EmuELEC); el servicio lee el volumen cada pocos segundos.
func (h *handheld) SetVolumeKeys(bool) {}

// LidClosed: el sensor del AXP (hallkey) o el interruptor SW_LID por evdev.
func (h *handheld) LidClosed() bool {
	if files, _ := filepath.Glob("/sys/class/power_supply/*/hallkey"); len(files) > 0 {
		b, err := os.ReadFile(files[0])
		return err == nil && strings.TrimSpace(string(b)) == "0"
	}
	return lidSwitchClosed()
}

func (h *handheld) Cleanup() {
	_ = h.ExitSleep()
	h.SetAwake(false)
}

// swLid es SW_LID de linux/input-event-codes.h.
const swLid = 0

// lidSwitchClosed busca un dispositivo con el interruptor de la tapa y lee
// su estado con EVIOCGSW.
func lidSwitchClosed() bool {
	b, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return false
	}
	for _, block := range strings.Split(string(b), "\n\n") {
		if !strings.Contains(block, "B: SW=") {
			continue
		}
		var handler string
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "H: Handlers="); ok {
				for _, f := range strings.Fields(v) {
					if strings.HasPrefix(f, "event") {
						handler = f
					}
				}
			}
		}
		if handler == "" {
			continue
		}
		f, err := os.Open("/dev/input/" + handler)
		if err != nil {
			continue
		}
		var sw [8]byte
		const evIOCGSW = 0x8000451b | (8 << 16) // _IOR('E', 0x1b, len)
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), evIOCGSW, uintptr(unsafe.Pointer(&sw[0])))
		f.Close()
		if errno == 0 && sw[swLid/8]&(1<<(swLid%8)) != 0 {
			return true
		}
	}
	return false
}
