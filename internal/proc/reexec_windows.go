package proc

import (
	"os"
	"os/exec"
)

// Reexec lanza este programa otra vez, con los mismos argumentos, y termina
// este: Windows no puede reemplazar un proceso por otro. Si va bien no vuelve.
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
