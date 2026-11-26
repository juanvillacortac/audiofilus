//go:build windows || (darwin && !ios)

package proc

import (
	"os"
	"os/exec"
)

// Reexec lanza este programa otra vez, con los mismos argumentos, y termina
// este. Windows no puede reemplazar un proceso por otro, y en macOS
// syscall.Exec se queda girando para siempre: antes de ejecutar, Go espera a
// que lleguen todas sus señales de expropiación (runtime_BeforeExec), y con
// hilos de Apple que llaman a Go (los del audio) puede quedar una que no llega
// nunca. Si va bien no vuelve.
func Reexec() error {
	exe, err := Executable()
	if err != nil {
		return err
	}
	if err := exec.Command(exe, os.Args[1:]...).Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
