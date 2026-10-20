// Package assets guarda lo que va dentro del ejecutable de la interfaz.
package assets

import "embed"

// FS tiene las fuentes (Jersey 10, Tiny5 y Fusion Pixel, comprimida; licencia
// OFL, ver los OFL-*.txt).
//
//go:embed fonts/*.ttf fonts/*.ttf.gz
var FS embed.FS
