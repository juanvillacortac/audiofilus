// Package version dice qué versión de audiofilus es este binario.
package version

// Version es la etiqueta de la release ("v0.9.0"), puesta al compilar con
// -ldflags "-X .../internal/version.Version=v0.9.0". Sin ella (go run, una
// compilación a mano) es "dev", y no se ofrecen actualizaciones.
var Version = "dev"
