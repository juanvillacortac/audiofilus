package ui

import (
	"os/exec"
	"syscall"
)

// dieWithParent hace que el proceso hijo muera si muere la interfaz (el
// ffmpeg de los vídeos del modo juego no debe quedar suelto si la
// interfaz se cierra para lanzar un juego).
func dieWithParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// detach pone el proceso hijo en su propia sesión, sin heredar la entrada
// ni la salida de la interfaz (para knulli-shutdown, ver gmPowerOff).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
}
