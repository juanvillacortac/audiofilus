//go:build !linux || android

package platform

// OpenOutputs: fuera de Linux el sistema ya cambia solo de salida al conectar
// un DAC (Windows, macOS, Android, iOS) y oto la sigue; no hay nada que
// elegir.
func OpenOutputs() Outputs { return nil }

// PickALSAOutput no hace nada fuera de Linux.
func PickALSAOutput(string) string { return "" }
