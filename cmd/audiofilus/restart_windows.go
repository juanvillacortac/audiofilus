package main

import (
	"os"
	"os/exec"
)

// restartSelf lanza otro servicio igual y deja terminar este (Windows no
// puede reemplazar un proceso por otro).
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, os.Args[1:]...)
	c.SysProcAttr = detachAttr()
	return c.Start()
}
