package proc

import (
	"os/exec"
	"runtime"
	"syscall"
)

// NormalPriority ejecuta run (que lanza procesos) con la prioridad normal
// (nice 0) para sus hijos. En Knulli los ports corren con nice -4 (la
// interfaz y el servicio lo heredan), y su ahorro de batería agresivo, tras
// un rato sin tocar nada, congela (SIGSTOP) todo lo que tenga -4, como si
// fuera un juego: un connmand o un idlewatcher lanzados desde aquí
// heredaban ese -4 y quedaban congelados (sin red; sin nadie que viera los
// botones para despertar). En Linux la prioridad es de cada hilo y un hijo
// hereda la del hilo que lo lanza: se fija la goroutine a su hilo, se le
// pone 0, se lanza y se le devuelve la suya.
func NormalPriority(run func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	raw, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
	if err != nil {
		return run()
	}
	prev := 20 - raw // el kernel devuelve 20 - nice
	if prev == 0 || syscall.Setpriority(syscall.PRIO_PROCESS, 0, 0) != nil {
		return run()
	}
	defer syscall.Setpriority(syscall.PRIO_PROCESS, 0, prev)
	return run()
}

// OwnSession pone c en una sesión nueva (setsid) y lo devuelve. Para los
// servicios del sistema que se lanzan desde la interfaz (bluetoothd,
// connmand): si no, heredan el grupo de procesos de EmulationStation (la
// interfaz es un port), y el ahorro agresivo de Knulli congela ese grupo
// entero (S31emulationstation suspend) tras un rato sin tocar nada.
func OwnSession(c *exec.Cmd) *exec.Cmd {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
	return c
}
