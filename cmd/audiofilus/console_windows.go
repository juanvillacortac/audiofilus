package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess es ATTACH_PARENT_PROCESS: la consola del proceso padre.
const attachParentProcess = ^uint32(0)

// En Windows, audiofilus.exe es una aplicación de ventana (-H windowsgui): la
// interfaz no abre una consola detrás. La línea de comandos sí escribe: si no
// le llega una salida (redirigida a un archivo o a un pipe), se engancha a la
// consola desde la que se lanzó, si la hay.
func init() {
	if len(os.Args) < 2 || os.Args[1][0] == '-' && os.Args[1] != "-h" && os.Args[1] != "--help" {
		return // la interfaz
	}
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return // ya tiene adónde escribir
	}
	// AttachConsole(ATTACH_PARENT_PROCESS), que x/sys no trae.
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if ok, _, _ := attach.Call(uintptr(attachParentProcess)); ok == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}
