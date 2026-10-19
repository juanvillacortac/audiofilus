//go:build tools

package mobile

// El código que genera gomobile para el framework importa su paquete seq:
// con esto, go.mod lo lleva (go mod tidy mira todas las etiquetas), aunque
// ninguna compilación normal lo use.
import _ "github.com/ebitengine/gomobile/bind/seq"
