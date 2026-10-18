// Package proc ajusta los procesos que lanza audiofilus según el sistema.
package proc

import "runtime"

// Exe es el nombre de un ejecutable en este sistema: con ".exe" en Windows.
func Exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
