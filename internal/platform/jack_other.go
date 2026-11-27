//go:build !linux && (!darwin || ios)

package platform

// WiredJack no se sabe fuera de Linux y macOS.
func WiredJack() (plugged, known, cheap bool) { return false, false, false }
