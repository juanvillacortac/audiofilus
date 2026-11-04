//go:build !linux

package ui

import "os/exec"

// dieWithParent no hace nada fuera de Linux (allí los vídeos se paran al
// salir de la ficha).
func dieWithParent(*exec.Cmd) {}

// detach no hace nada fuera de Linux.
func detach(*exec.Cmd) {}
