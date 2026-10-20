//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf vuelve a ejecutar el servicio en el mismo proceso (el mismo
// PID y la misma línea de comandos, "audiofilusd daemon"), para aplicar lo
// que solo se lee al arrancar.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
