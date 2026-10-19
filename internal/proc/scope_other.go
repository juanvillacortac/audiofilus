//go:build !linux

package proc

// LeaveGameScope no hace nada fuera de Linux (los scopes son de systemd).
func LeaveGameScope() {}
