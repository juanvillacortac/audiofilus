//go:build !linux

package platform

import "time"

// Fuera de Linux no hay Knulli, muOS, ROCKNIX ni ArkOS: siempre escritorio.

func detectKnulli(Options) Platform   { return nil }
func detectMuOS(Options) Platform     { return nil }
func detectHandheld(Options) Platform { return nil }

// AudioOutput no distingue salidas fuera de Linux.
func AudioOutput() string { return "" }

// ExternalDisplay no se sabe fuera de Linux.
func ExternalDisplay() bool { return false }

// WiredJack no se sabe fuera de Linux.
func WiredJack() (plugged, known, cheap bool) { return false, false, false }

// SetWakeAlarm no hace nada fuera de Linux.
func SetWakeAlarm(time.Time) error { return nil }
