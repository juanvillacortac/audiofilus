package main

import "syscall"

// detachAttr separa el daemon de la consola de quien lo lanza y no le abre
// ventana: sigue vivo al cerrar la interfaz o la terminal.
func detachAttr() *syscall.SysProcAttr {
	const detachedProcess, newProcessGroup = 0x00000008, 0x00000200
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup, HideWindow: true}
}
