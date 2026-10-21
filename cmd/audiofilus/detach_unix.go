//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
)

// detachAttr pone el daemon en una sesión nueva (setsid).
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// closeInherited marca para cerrar al lanzar el daemon los descriptores que
// este proceso heredó abiertos (Go solo marca los suyos). Si el daemon se los
// queda, quien lanzó el port puede quedarse esperando a que se cierren: en
// EmuELEC, EmulationStation lee la salida del port por una tubería (y con el
// fd 3) y no vuelve hasta que nadie la tiene abierta.
func closeInherited() {
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return
	}
	for _, e := range ents {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
}
