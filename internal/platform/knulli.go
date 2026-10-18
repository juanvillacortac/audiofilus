package platform

import (
	"bufio"
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	knulliCapability = "/boot/boot/knulli.board.capability"
	knulliBrightness = "/usr/bin/knulli-brightness"
	powerButton      = "/usr/bin/power-button"
	idleInit         = "/etc/init.d/S96idlewatcher-daemon"
	batterySaverDir  = "/var/run/battery-saver"
	rumbleFile       = "/sys/class/power_supply/axp2202-battery/moto"
	governorGlob     = "/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor"

	// Archivos en /tmp: es tmpfs, así que desaparecen al reiniciar junto con los
	// montajes.
	knulliAudio     = "/usr/bin/knulli-audio"
	audioOriginal   = "/tmp/audiofilus-knulli-audio"         // copia del original, para llamarlo con el wrapper puesto
	audioWrapper    = "/tmp/audiofilus-knulli-audio-wrapper" // lo que se monta encima
	capabilityCopy  = "/tmp/audiofilus-capability"
	powerButtonCopy = "/tmp/audiofilus-power-button"
	mountsRecord    = "/tmp/audiofilus-mounts"
	pauseFile       = batterySaverDir + "/audiofilus.pause"
)

// knulli es la plataforma de Knulli (probada en la RG34XX-SP).
type knulli struct {
	opts Options

	mu            sync.Mutex
	mounted       []string
	volumeKeys    bool // el wrapper de knulli-audio está montado
	sleeping      bool
	savedGovernor string
	stoppedIdle   bool  // se paró idlewatcher al entrar en sleep
	frozenKeys    []int // los gptokeyb de la interfaz, congelados en sleep
	screenOff     bool  // se apagó la retroiluminación al entrar en sleep
}

func detectKnulli(opts Options) *knulli {
	for _, p := range []string{knulliCapability, knulliBrightness} {
		if _, err := os.Stat(p); err != nil {
			return nil
		}
	}
	k := &knulli{opts: opts}
	k.unmountRecorded() // restos de un cierre inesperado
	return k
}

func (k *knulli) Name() string { return "knulli" }

func (k *knulli) SetAwake(awake bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if awake {
		k.holdAwakeLocked()
	} else {
		k.releaseLocked()
	}
}

// holdAwakeLocked impide la suspensión por inactividad, por la tapa y por la
// pulsación corta del botón de encendido.
func (k *knulli) holdAwakeLocked() {
	if err := os.MkdirAll(batterySaverDir, 0o755); err == nil {
		_ = os.WriteFile(pauseFile, nil, 0o644)
	}
	if len(k.mounted) > 0 {
		return
	}
	// Tapa: se oculta la capacidad "lid" para que lid-control ignore el cierre.
	if b, err := os.ReadFile(knulliCapability); err == nil && hasWord(string(b), "lid") {
		if err := os.WriteFile(capabilityCopy, []byte(removeWord(string(b), "lid")), 0o644); err == nil {
			k.bindLocked(capabilityCopy, knulliCapability)
		}
	}
	// Botón de encendido: una copia del script de Knulli sin la línea que
	// suspende; mantenerlo pulsado sigue apagando.
	if b, err := os.ReadFile(powerButton); err == nil {
		patched := bytes.Replace(b, []byte("    knulli-suspend &\n"), []byte("    : # suspend disabled while audiofilus plays\n"), 1)
		if !bytes.Equal(patched, b) && os.WriteFile(powerButtonCopy, patched, 0o755) == nil {
			k.bindLocked(powerButtonCopy, powerButton)
		}
	}
	k.recordLocked()
}

// releaseLocked deshace holdAwakeLocked.
func (k *knulli) releaseLocked() {
	_ = os.Remove(pauseFile)
	for i := len(k.mounted) - 1; i >= 0; i-- {
		unmountAll(k.mounted[i])
	}
	k.mounted = nil
	_ = os.Remove(capabilityCopy)
	_ = os.Remove(powerButtonCopy)
	k.recordLocked()
}

// bindLocked monta src sobre target, salvo que otra app ya lo haya tapado.
func (k *knulli) bindLocked(src, target string) {
	if isMountpoint(target) {
		log.Printf("%s is already overridden (another app?); leaving it", target)
		return
	}
	if out, err := exec.Command("mount", "--bind", src, target).CombinedOutput(); err != nil {
		log.Printf("bind %s: %v %s", target, err, out)
		return
	}
	k.mounted = append(k.mounted, target)
}

// recordLocked anota los montajes en /tmp para deshacerlos tras un cierre inesperado.
func (k *knulli) recordLocked() {
	targets := append([]string(nil), k.mounted...)
	if k.volumeKeys {
		targets = append(targets, knulliAudio)
	}
	if len(targets) == 0 {
		_ = os.Remove(mountsRecord)
		return
	}
	_ = os.WriteFile(mountsRecord, []byte(strings.Join(targets, "\n")+"\n"), 0o644)
}

// audioBin es el knulli-audio de verdad: la copia si el wrapper está puesto.
func (k *knulli) audioBin() string {
	if k.volumeKeys {
		return audioOriginal
	}
	return knulliAudio
}

// SystemVolume lee el volumen como lo hace Knulli: knulli-audio getSystemVolume.
func (k *knulli) SystemVolume() int {
	k.mu.Lock()
	bin := k.audioBin()
	k.mu.Unlock()
	out, err := exec.Command(bin, "getSystemVolume").Output()
	if err != nil {
		return pactlVolume()
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return v
}

// SetSystemVolume cambia el volumen con knulli-audio, como los botones.
func (k *knulli) SetSystemVolume(v int) error {
	k.mu.Lock()
	bin := k.audioBin()
	k.mu.Unlock()
	return exec.Command(bin, "setSystemVolume", strconv.Itoa(v)).Run()
}

// SetVolumeKeys monta (o quita) un wrapper sobre knulli-audio, como hacía
// Songo: los botones de volumen ejecutan "knulli-audio setSystemVolume +5" (o
// -5), y el wrapper se lo pasa al servicio (audiofilus volstep), que decide si
// sube el sistema o el refuerzo de la app. Lo demás va al original. Si el
// servicio no responde, el wrapper llama al original: los botones nunca dejan
// de funcionar.
func (k *knulli) SetVolumeKeys(on bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if on == k.volumeKeys {
		return
	}
	if !on {
		unmountAll(knulliAudio)
		k.volumeKeys = false
		_ = os.Remove(audioWrapper)
		_ = os.Remove(audioOriginal)
		k.recordLocked()
		return
	}
	if isMountpoint(knulliAudio) {
		log.Printf("%s is already overridden (another app?); leaving it", knulliAudio)
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	orig, err := os.ReadFile(knulliAudio)
	if err != nil || os.WriteFile(audioOriginal, orig, 0o755) != nil {
		return
	}
	wrapper := "#!/bin/sh\n" +
		"# audiofilus: los botones de volumen pasan por audiofilus mientras su app está abierta.\n" +
		"if [ \"$1\" = setSystemVolume ]; then\n" +
		"  case \"$2\" in\n" +
		"    +[0-9]*|-[0-9]*) \"" + self + "\" volstep \"$2\" >/dev/null 2>&1 && exit 0 ;;\n" +
		"  esac\n" +
		"fi\n" +
		"exec " + audioOriginal + " \"$@\"\n"
	if os.WriteFile(audioWrapper, []byte(wrapper), 0o755) != nil {
		return
	}
	if out, err := exec.Command("mount", "--bind", audioWrapper, knulliAudio).CombinedOutput(); err != nil {
		log.Printf("bind %s: %v %s", knulliAudio, err, out)
		return
	}
	k.volumeKeys = true
	k.recordLocked()
}

// unmountRecorded deshace los montajes anotados por una ejecución anterior.
func (k *knulli) unmountRecorded() {
	b, err := os.ReadFile(mountsRecord)
	if err != nil {
		return
	}
	for _, t := range strings.Fields(string(b)) {
		unmountAll(t)
	}
	_ = os.Remove(mountsRecord)
	_ = os.Remove(pauseFile)
}

// EnterSleep para idlewatcher (su hook "active" no mira los *.pause y, al
// detectar un botón, restaura el brillo y el governor) y, con screenOff, apaga
// la retroiluminación; sin él (modo OLED) la interfaz solo pinta negro.
// EmulationStation no se congela: con un port abierto ya está en espera.
func (k *knulli) EnterSleep(screenOff bool) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.sleeping {
		return nil
	}
	k.sleeping = true
	if processRunning("idlewatcher") {
		run(idleInit, "stop")
		k.stoppedIdle = true
	}
	if screenOff {
		run(knulliBrightness, "dispoff")
		k.screenOff = true
	}
	if g := orDefault(k.opts.Governor, "powersave"); g != "keep" {
		k.savedGovernor = readGovernor()
		setGovernor(g)
	}
	// El gptokeyb que lanza PortMaster junto a la interfaz solo da el atajo
	// de salir (Select+Start), pero lee el mando sin parar: ~5 % de CPU. En
	// sleep no hace falta; se congela y se reanuda al despertar.
	k.frozenKeys = uiKeyMappers()
	for _, pid := range k.frozenKeys {
		_ = syscall.Kill(pid, syscall.SIGSTOP)
	}
	return nil
}

// uiKeyMappers son los gptokeyb lanzados para la interfaz de audiofilus (los
// de otros ports no se tocan).
func uiKeyMappers() []int {
	var pids []int
	dirs, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, d := range dirs {
		b, err := os.ReadFile(d)
		if err != nil || strings.TrimSpace(string(b)) != "gptokeyb" {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join(filepath.Dir(d), "cmdline"))
		if err != nil || !bytes.Contains(cmd, []byte("audiofilus-ui")) {
			continue
		}
		if pid, err := strconv.Atoi(filepath.Base(filepath.Dir(d))); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// ExitSleep deshace EnterSleep.
func (k *knulli) ExitSleep() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.sleeping {
		return nil
	}
	k.sleeping = false
	for _, pid := range k.frozenKeys {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
	k.frozenKeys = nil
	if k.savedGovernor != "" {
		setGovernor(k.savedGovernor)
		k.savedGovernor = ""
	}
	if k.screenOff {
		run(knulliBrightness, "dispon")
		k.screenOff = false
	}
	if k.stoppedIdle {
		run(idleInit, "start")
		k.stoppedIdle = false
	}
	return nil
}

func (k *knulli) Rumble(d time.Duration) {
	if os.WriteFile(rumbleFile, []byte("1"), 0o644) != nil {
		return
	}
	time.AfterFunc(d, func() { _ = os.WriteFile(rumbleFile, []byte("0"), 0o644) })
}

func (k *knulli) Cleanup() {
	_ = k.ExitSleep()
	k.SetVolumeKeys(false)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.releaseLocked()
}

// LidClosed lee el sensor de la tapa del cargador AXP (hallkey: 1 abierta, 0
// cerrada). Sin ese archivo, la consola no tiene tapa o no se sabe: abierta.
func (k *knulli) LidClosed() bool {
	files, _ := filepath.Glob("/sys/class/power_supply/*/hallkey")
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == "0" {
			return true
		}
	}
	return false
}

// processRunning dice si hay un proceso llamado name.
func processRunning(name string) bool {
	dirs, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, d := range dirs {
		if b, err := os.ReadFile(d); err == nil && strings.TrimSpace(string(b)) == name {
			return true
		}
	}
	return false
}

// run ejecuta un comando del sistema y deja en el log si falla.
func run(name string, args ...string) {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		log.Printf("%s %v: %v %s", name, args, err, bytes.TrimSpace(out))
	}
}

// unmountAll quita los montajes apilados en target. Knulli no trae `mountpoint`.
func unmountAll(target string) {
	for i := 0; i < 8 && isMountpoint(target); i++ {
		if err := exec.Command("umount", "-l", target).Run(); err != nil {
			log.Printf("umount %s: %v", target, err)
			return
		}
	}
}

// isMountpoint dice si target aparece como punto de montaje en /proc/mounts.
func isMountpoint(target string) bool {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) > 1 && fields[1] == target {
			return true
		}
	}
	return false
}

// hasWord dice si w es una de las palabras de s.
func hasWord(s, w string) bool {
	for _, f := range strings.Fields(s) {
		if f == w {
			return true
		}
	}
	return false
}

// removeWord quita la palabra w de s.
func removeWord(s, w string) string {
	var out []string
	for _, f := range strings.Fields(s) {
		if f != w {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ") + "\n"
}

// readGovernor devuelve el governor actual de la CPU.
func readGovernor() string {
	paths, _ := filepath.Glob(governorGlob)
	if len(paths) == 0 {
		return ""
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// setGovernor pone el governor g en todos los núcleos.
func setGovernor(g string) {
	paths, _ := filepath.Glob(governorGlob)
	for _, p := range paths {
		writeSys(p, g) // en ArkOS los ports no son root: con sudo
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
