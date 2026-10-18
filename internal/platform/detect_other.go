//go:build !linux

package platform

// Fuera de Linux no hay Knulli, muOS, ROCKNIX ni ArkOS: siempre escritorio.

func detectKnulli(Options) Platform   { return nil }
func detectMuOS(Options) Platform     { return nil }
func detectHandheld(Options) Platform { return nil }

// AudioOutput no distingue salidas fuera de Linux.
func AudioOutput() string { return "" }
