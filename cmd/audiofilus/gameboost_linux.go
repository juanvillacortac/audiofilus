package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Mientras dura un juego lanzado desde el modo juego, se le deja la consola
// lo más libre posible: Syncthing se congela (al sincronizar recorre y
// calcula el hash de los archivos: picos de CPU, de disco y de calor, que en
// la RG34XX-SP frena la CPU desde los 60 °C) y el grupo del juego va con más
// prioridad. Todo se deshace al volver, y nada queda escrito en la
// configuración de nadie.

// gameNice es la prioridad del grupo del juego (el de la sesión de
// EmulationStation, en que también corre este proceso) mientras juega.
const gameNice = -5

// frozenPath guarda quién congeló (el pid de este `launch`, en la primera
// línea) y los procesos congelados mientras dura el juego: si Audiofilus se
// cae a medias, el siguiente arranque los descongela (thawLeftovers). En
// /tmp: con un reinicio ya no hace falta.
const frozenPath = "/tmp/audiofilus-frozen"

// frozenComms son los procesos que se congelan durante un juego.
var frozenComms = map[string]bool{"syncthing": true}

// boostGame prepara la consola para el juego y devuelve lo que la deja
// como estaba (llamarlo más de una vez no hace nada).
func boostGame() (restore func()) {
	pids := freezeBackground(func(_ int, comm string) bool { return frozenComms[comm] })
	prev, hadGroup := autogroupNice()
	if hadGroup {
		if err := setAutogroupNice(gameNice); err != nil {
			log.Printf("launch: game priority: %v", err)
			hadGroup = false
		}
	}
	log.Printf("launch: froze %d background processes, game group nice %d", len(pids), gameNice)
	done := false
	return func() {
		if done {
			return // una sola vez (launch la llama al volver y al salir)
		}
		done = true
		if hadGroup {
			if err := setAutogroupNice(prev); err != nil {
				log.Printf("launch: restoring priority: %v", err)
			}
		}
		thaw(pids)
	}
}

// freezeBackground congela los procesos que elige match (por su pid y su
// nombre) y los apunta en frozenPath.
func freezeBackground(match func(pid int, comm string) bool) []int {
	var pids []int
	dirs, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, d := range dirs {
		b, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(d)))
		if err != nil || pid == os.Getpid() || !match(pid, strings.TrimSpace(string(b))) {
			continue
		}
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		return nil
	}
	// Primero se apuntan: si algo se cae entre medias, se descongelan igual.
	list := []string{"launcher " + strconv.Itoa(os.Getpid())}
	for _, p := range pids {
		list = append(list, strconv.Itoa(p))
	}
	if err := os.WriteFile(frozenPath, []byte(strings.Join(list, "\n")+"\n"), 0o644); err != nil {
		log.Printf("launch: %v (not freezing)", err)
		return nil
	}
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGSTOP)
	}
	return pids
}

// thaw descongela pids y borra frozenPath.
func thaw(pids []int) {
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGCONT)
	}
	os.Remove(frozenPath)
}

// thawLeftovers descongela lo que quedó congelado si un juego anterior no
// terminó bien (Audiofilus se cayó durante el juego): solo si el `launch`
// que lo congeló ya no está (si sigue, el juego sigue: cualquier comando,
// como `audiofilus status`, pasa por aquí). Solo los procesos que siguen
// siendo de frozenComms (por su nombre).
func thawLeftovers() {
	b, err := os.ReadFile(frozenPath)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 0 {
		if pid, ok := strings.CutPrefix(lines[0], "launcher "); ok {
			if n, err := strconv.Atoi(pid); err == nil && n != os.Getpid() && launcherAlive(n) {
				return // el juego sigue
			}
			lines = lines[1:]
		}
	}
	var pids []int
	for _, f := range lines {
		p, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			continue
		}
		if c, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", p)); err == nil && frozenComms[strings.TrimSpace(string(c))] {
			pids = append(pids, p)
		}
	}
	thaw(pids)
}

// autogroupNice es la prioridad del grupo de este proceso (su sesión, con
// SCHED_AUTOGROUP); false si el kernel no agrupa.
func autogroupNice() (int, bool) {
	if b, err := os.ReadFile("/proc/sys/kernel/sched_autogroup_enabled"); err != nil || strings.TrimSpace(string(b)) != "1" {
		return 0, false
	}
	b, err := os.ReadFile("/proc/self/autogroup")
	if err != nil {
		return 0, false
	}
	// "/autogroup-12 nice 0"
	f := strings.Fields(string(b))
	if len(f) != 3 || f[1] != "nice" {
		return 0, false
	}
	n, err := strconv.Atoi(f[2])
	return n, err == nil
}

// setAutogroupNice pone la prioridad del grupo de este proceso (y de sus
// hijos: el juego).
func setAutogroupNice(n int) error {
	return os.WriteFile("/proc/self/autogroup", []byte(strconv.Itoa(n)), 0o644)
}

// launcherAlive dice si pid sigue siendo un `audiofilus launch` (no otro
// proceso que heredó el número).
func launcherAlive(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	// "audiofilus launch ..." (o, con un intérprete delante, un argumento
	// más allá).
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	for i := 1; i < len(args) && i <= 2; i++ {
		if args[i] == "launch" {
			return true
		}
	}
	return false
}
