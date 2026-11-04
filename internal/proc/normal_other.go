//go:build !linux

package proc

// NormalPriority ejecuta run tal cual fuera de Linux (el problema de los
// ports de Knulli no existe allí).
func NormalPriority(run func() error) error { return run() }
