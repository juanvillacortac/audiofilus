package proc

// LowPriority no hace nada en Windows (allí no hace falta).
func LowPriority(pid int) {}
