package main

import (
	"os"
	"os/exec"

	"github.com/juanvillacortac/audiofilus/internal/proc"
)

// restartSelf lanza otro servicio igual y deja terminar este, como en
// Windows: en macOS syscall.Exec se queda girando (ver proc.Reexec). El nuevo
// va en su propia sesión, con el mismo nombre ("audiofilusd") y la misma
// salida (el log); espera a que este suelte el candado del servicio.
func restartSelf() error {
	exe, err := proc.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, os.Args[1:]...)
	c.Args[0] = os.Args[0]
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	c.SysProcAttr = detachAttr()
	return c.Start()
}
