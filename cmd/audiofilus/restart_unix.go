//go:build !windows

package main

import (
	"github.com/juanvillacortac/audiofilus/internal/proc"
	"os"
	"syscall"
)

// restartSelf vuelve a ejecutar el servicio en el mismo proceso (el mismo
// PID y la misma línea de comandos, "audiofilusd daemon"), para aplicar lo
// que solo se lee al arrancar.
func restartSelf() error {
	exe, err := proc.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
