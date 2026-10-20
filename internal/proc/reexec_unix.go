//go:build !windows

package proc

import (
	"os"
	"syscall"
)

// Reexec vuelve a ejecutar este programa en el mismo proceso (el mismo PID y
// los mismos argumentos): tras una actualización, para correr el binario
// nuevo. Si va bien no vuelve.
func Reexec() error {
	exe, err := Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
