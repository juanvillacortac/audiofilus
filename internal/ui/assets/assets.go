// Package assets guarda lo que va dentro del ejecutable de la interfaz.
package assets

import "embed"

// FS tiene las fuentes (Jersey 10 y Tiny5, licencia OFL; ver los OFL-*.txt).
//
//go:embed fonts/*.ttf
var FS embed.FS
