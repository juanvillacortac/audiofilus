//go:build linux

package platform

import (
	"bytes"
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

// Las portátiles con systemd: ROCKNIX y la familia ArkOS (ArkOS y dArkOS).
// Comparten casi todo (sysfs para el brillo y el governor, logind), así que
// es una sola plataforma con sus diferencias (sacadas de su código:
// github.com/ROCKNIX/distribution y github.com/christianhaitian/dArkOS):
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
//   - Brillo: /sys/class/backlight. Volumen: en ROCKNIX, pactl (y su script
//     "volume", que lo guarda); en ArkOS, ALSA con amixer.
//   - Tapa: el sensor del AXP (hallkey) o, con kernel mainline (ROCKNIX), el
//     interruptor SW_LID por evdev.

const (
	arkPause        = "/usr/local/bin/pause.sh"
	arkProcessCheck = "/usr/local/bin/processcheck.sh"
	arkMounts       = "/tmp/audiofilus-arkos-mounts"
)

// handheld es ROCKNIX o la familia ArkOS.
type handheld struct {
	name string // "rocknix" o "arkos"
	opts Options

	mu            sync.Mutex
	inhibit       *os.File // el inhibidor de logind (se suelta al cerrarlo)
	mounted       []string
	sleeping      bool
	savedBright   map[string]string
	savedGovernor string
	frozenKeys    []int
}

// detectHandheld reconoce ROCKNIX por /etc/os-release y la familia ArkOS por
// su pause.sh y su usuario ark.
func detectHandheld(opts Options) *handheld {
	osr, _ := os.ReadFile("/etc/os-release")
	id := strings.ToLower(string(osr))
	switch {
	case strings.Contains(id, "rocknix"):
		return &handheld{name: "rocknix", opts: opts}
	case fileExists(arkPause) && (fileExists("/home/ark") || strings.Contains(id, "arkos")):
		h := &handheld{name: "arkos", opts: opts}
		h.unmountLeftovers()
		return h
	}
	return nil
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

// SetAwake impide (o deja) la suspensión: el inhibidor de logind y, en
// ArkOS, las copias de pause.sh y processcheck.sh.
func (h *handheld) SetAwake(awake bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if awake && h.inhibit == nil {
		f, err := logindInhibit()
		if err != nil {
			log.Printf("logind inhibit: %v", err)
		}
		h.inhibit = f
		if h.name == "arkos" {
			h.bindLocked(arkPause, "#!/bin/sh\n# audiofilus is open: it handles the power button (its app sleep)\nexit 0\n")
			h.bindLocked(arkProcessCheck, "#!/bin/sh\n# audiofilus is open: no idle suspend\nprintf Running\n")
		}
		return
	}
	if !awake && h.inhibit != nil {
		h.inhibit.Close()
		h.inhibit = nil
		for i := len(h.mounted) - 1; i >= 0; i-- {
			_ = asRoot("umount", "-l", h.mounted[i])
		}
		h.mounted = nil
		_ = os.Remove(arkMounts)
	}
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
		"sleep:idle:handle-power-key:handle-suspend-key:handle-lid-switch", "audiofilus", "Music playing with the app open", "block").Store(&fd)
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
	if screenOff {
		h.savedBright = map[string]string{}
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
	return nil
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

// SystemVolume es el volumen del sistema: pactl en ROCKNIX, amixer en ArkOS.
func (h *handheld) SystemVolume() int {
	if h.name == "rocknix" {
		return pactlVolume()
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
// guarda) o pactl; en ArkOS con amixer.
func (h *handheld) SetSystemVolume(v int) error {
	if h.name == "rocknix" {
		if fileExists("/usr/bin/volume") {
			return exec.Command("/usr/bin/volume", strconv.Itoa(v)).Run()
		}
		return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", strconv.Itoa(v)+"%").Run()
	}
	for _, c := range amixerControls {
		if exec.Command("amixer", "-q", "sset", c, strconv.Itoa(v)+"%").Run() == nil {
			return nil
		}
	}
	return fmt.Errorf("amixer: no volume control")
}

// SetVolumeKeys no hace nada: los botones de volumen los llevan input_sense
// (ROCKNIX) y ogage (ArkOS); el servicio lee el volumen cada pocos segundos.
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
