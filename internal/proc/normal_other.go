//go:build !linux

package proc

import "os/exec"

// NormalPriority ejecuta run tal cual fuera de Linux (el problema de los
// ports de Knulli no existe allí).
func NormalPriority(run func() error) error { return run() }

// OwnSession devuelve c tal cual fuera de Linux.
func OwnSession(c *exec.Cmd) *exec.Cmd { return c }
