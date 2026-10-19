package proc

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// gameSlice es donde ROCKNIX mete cada juego (y cada port): runemu.sh lo
// lanza con systemd-run --scope --slice=games.slice y, al salir el juego,
// hace systemctl stop de ese scope, que mata todo lo que quede dentro. El
// servicio, aunque se separe con setsid, sigue en el cgroup de la interfaz.
const gameSlice = "/games.slice/"

// LeaveGameScope saca este proceso del scope del juego que lo lanzó (si lo
// hay) a uno propio, para que siga sonando cuando la interfaz se cierra. En
// sistemas sin systemd, o fuera de un scope de juego, no hace nada.
func LeaveGameScope() {
	cg, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || !strings.Contains(string(cg), gameSlice) || !isDir("/run/systemd/system") {
		return
	}
	if err := moveToScope(os.Getpid()); err != nil {
		log.Printf("leaving the game scope: %v", err)
		return
	}
	// systemd mueve el proceso al arrancar la unidad; se espera a verlo.
	for i := 0; i < 40; i++ {
		if cg, err := os.ReadFile("/proc/self/cgroup"); err == nil && !strings.Contains(string(cg), gameSlice) {
			log.Printf("left the game scope: %s", strings.TrimSpace(string(cg)))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("leaving the game scope: still in %s", strings.TrimSpace(string(cg)))
}

// moveToScope crea un scope de systemd con solo pid, como hace systemd-run
// --scope, en system.slice: fuera de games.slice, que ROCKNIX para al salir
// del juego y congela al suspender.
func moveToScope(pid int) error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	type prop struct {
		Name  string
		Value dbus.Variant
	}
	type aux struct {
		Name  string
		Props []prop
	}
	props := []prop{
		{"Description", dbus.MakeVariant("audiofilus music service")},
		{"PIDs", dbus.MakeVariant([]uint32{uint32(pid)})},
		{"CollectMode", dbus.MakeVariant("inactive-or-failed")},
	}
	var job dbus.ObjectPath
	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	err = obj.Call("org.freedesktop.systemd1.Manager.StartTransientUnit", 0,
		fmt.Sprintf("audiofilusd-%d.scope", pid), "fail", props, []aux{}).Store(&job)
	return err
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
