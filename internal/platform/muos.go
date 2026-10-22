//go:build linux

package platform

import (
	"fmt"
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

// muOS (MustardOS, también en las Anbernic H700 como la RG34XX-SP). Lo que en
// Knulli son tres trucos, aquí lo dan sus propios mecanismos (de
// github.com/MustardOS/internal):
//
//   - No suspender: mientras exista /run/muos/caffeine, su SLEEP() no suspende
//     por el botón de encendido, la tapa ni la inactividad.
//   - Que no atenúe la pantalla ni silencie el audio por inactividad (con
//     "idle_mute", en el bolsillo se callaría la música): el contador
//     /run/muos/input_activity, que su vigilante (idle.sh) mira cada 5 s; si
//     cambia, inhibe las dos cosas. Es lo que usan los juegos.
//   - Brillo: /opt/muos/script/device/bright.sh (0 apaga y pone la pantalla en
//     negro sin guardarlo; para volver, el brillo guardado en su
//     configuración).
//   - Volumen: /opt/muos/script/device/audio.sh (I lo dice, N lo pone).
//   - Vibración, tapa: las rutas que muOS da para cada consola
//     (/opt/muos/device/config/board/rumble y board/hall).
//
// El audio de muOS es PipeWire sin el protocolo de PulseAudio: oto cae a ALSA,
// cuya salida por defecto va a PipeWire (y necesita XDG_RUNTIME_DIR=/run).

const (
	muosRun      = "/run/muos"
	muosCaffeine = muosRun + "/caffeine"
	muosActivity = muosRun + "/input_activity"
	muosScripts  = "/opt/muos/script/device"
	muosDevice   = "/opt/muos/device/config"
	muosConfig   = "/opt/muos/config"
)

// muos es la plataforma de muOS.
type muos struct {
	opts Options

	mu            sync.Mutex
	awake         bool
	stopActivity  chan struct{}
	sleeping      bool
	screenOff     bool
	savedGovernor string
	frozenKeys    []int
}

// detectMuOS dice si corre en muOS (por sus scripts y su /run/muos).
func detectMuOS(opts Options) *muos {
	if _, err := os.Stat(filepath.Join(muosScripts, "bright.sh")); err != nil {
		return nil
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		os.Setenv("XDG_RUNTIME_DIR", "/run") // donde está el socket de PipeWire
	}
	return &muos{opts: opts}
}

func (m *muos) Name() string { return "muos" }

// muosVar lee una variable de la configuración de muOS (un archivo por
// variable).
func muosVar(base, name string) string {
	b, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetAwake impide (o deja) que muOS suspenda y que atenúe la pantalla o
// silencie el audio por inactividad.
func (m *muos) SetAwake(awake bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if awake == m.awake {
		return
	}
	m.awake = awake
	if awake {
		_ = os.MkdirAll(muosRun, 0o755)
		if err := os.WriteFile(muosCaffeine, nil, 0o644); err != nil {
			log.Printf("muos caffeine: %v", err)
		}
		m.stopActivity = make(chan struct{})
		go activityPulse(m.stopActivity)
		return
	}
	_ = os.Remove(muosCaffeine)
	if m.stopActivity != nil {
		close(m.stopActivity)
		m.stopActivity = nil
	}
}

// activityPulse cambia el contador de actividad cada 3 s (idle.sh lo mira
// cada 5) hasta que se cierre stop.
func activityPulse(stop chan struct{}) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	n := time.Now().UnixNano()
	for {
		n++
		_ = os.WriteFile(muosActivity, []byte(strconv.FormatInt(n, 10)), 0o644)
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

func (m *muos) EnterSleep(screenOff bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sleeping {
		return nil
	}
	m.sleeping = true
	if screenOff {
		run(filepath.Join(muosScripts, "bright.sh"), "0")
		m.screenOff = true
	}
	if g := orDefault(m.opts.Governor, "powersave"); g != "keep" {
		m.savedGovernor = readGovernor()
		setGovernor(g)
	}
	m.frozenKeys = uiKeyMappers()
	for _, pid := range m.frozenKeys {
		_ = syscall.Kill(pid, syscall.SIGSTOP)
	}
	return nil
}

func (m *muos) ExitSleep() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sleeping {
		return nil
	}
	m.sleeping = false
	for _, pid := range m.frozenKeys {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
	m.frozenKeys = nil
	if m.savedGovernor != "" {
		setGovernor(m.savedGovernor)
		m.savedGovernor = ""
	}
	if m.screenOff {
		bright := muosVar(muosConfig, "settings/general/brightness")
		b, err := strconv.Atoi(bright)
		if err != nil || b <= 0 {
			b = 90
		}
		// Dos veces, uno de diferencia y luego el de verdad, como hace muOS al
		// salir de su suspensión: su bright.sh (en Jacaranda) solo escribe la
		// retroiluminación si el valor cambia respecto al guardado, y con el
		// mismo valor quitaba el negro de la pantalla pero la dejaba sin luz.
		nudge := b - 1
		if nudge < 1 {
			nudge = b + 1
		}
		run(filepath.Join(muosScripts, "bright.sh"), strconv.Itoa(nudge))
		run(filepath.Join(muosScripts, "bright.sh"), strconv.Itoa(b))
		m.screenOff = false
	}
	return nil
}

// Rumble vibra por la ruta que muOS da para la consola.
func (m *muos) Rumble(d time.Duration) {
	path := muosVar(muosDevice, "board/rumble")
	if path == "" {
		path = rumbleFile
	}
	if os.WriteFile(path, []byte("1"), 0o644) != nil {
		return
	}
	time.AfterFunc(d, func() { _ = os.WriteFile(path, []byte("0"), 0o644) })
}

// SystemVolume es el volumen de muOS ("audio.sh I" dice "75%").
func (m *muos) SystemVolume() int {
	out, err := exec.Command(filepath.Join(muosScripts, "audio.sh"), "I").Output()
	if err != nil {
		return -1
	}
	v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(string(out)), "%"))
	if err != nil {
		return -1
	}
	return v
}

// SetSystemVolume lo cambia como lo hacen sus botones (y lo guarda).
func (m *muos) SetSystemVolume(v int) error {
	return exec.Command(filepath.Join(muosScripts, "audio.sh"), fmt.Sprint(v)).Run()
}

// SetVolumeKeys no hace nada: en muOS los botones de volumen los lleva su
// muhotkey (con audio.sh), y el servicio lee el volumen cada pocos segundos.
func (m *muos) SetVolumeKeys(bool) {}

// LidClosed lee el sensor de la tapa que muOS da para la consola (0: cerrada).
func (m *muos) LidClosed() bool {
	path := muosVar(muosDevice, "board/hall")
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(b)) == "0"
}

func (m *muos) Cleanup() {
	_ = m.ExitSleep()
	m.SetAwake(false)
}
