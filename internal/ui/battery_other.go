//go:build !windows

package ui

// systemBattery no se usa fuera de Windows: en Linux la batería sale de sysfs.
func systemBattery() (level float64, charging, ok bool) { return 0, false, false }
