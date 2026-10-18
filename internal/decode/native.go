package decode

import (
	"errors"
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// Decodificadores en C cargados en tiempo de ejecución con purego (sin cgo):
// libmpg123 (MP3), libopusfile (Opus), libxmp (módulos tracker) y libfdk-aac
// (AAC). Knulli los trae, y casi todos los Linux de escritorio también. Si una
// biblioteca falta, ese formato no se puede abrir (o, en MP3, se usa go-mp3).

// nativeDisabled dice si AUDIOFILUS_DECODER=go pide no usar bibliotecas en C
// (para comparar con los decodificadores en Go puro, o en los tests).
func nativeDisabled() bool { return os.Getenv("AUDIOFILUS_DECODER") == "go" }

// libFunc es una función de una biblioteca: dónde guardarla y su símbolo.
type libFunc struct {
	ptr  any
	name string
}

// openLib abre la primera de names que exista y registra fns en ella.
func openLib(names []string, fns []libFunc) error {
	var lib uintptr
	for _, name := range names {
		if l, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			lib = l
			break
		}
	}
	if lib == 0 {
		return fmt.Errorf("%s not found", names[0])
	}
	for _, f := range fns {
		if _, err := purego.Dlsym(lib, f.name); err != nil {
			return fmt.Errorf("%s: %s missing", names[0], f.name)
		}
	}
	for _, f := range fns {
		purego.RegisterLibFunc(f.ptr, lib, f.name)
	}
	return nil
}

// errNoLib es el error de un formato cuya biblioteca no está: se informa como
// ErrUnsupported (con el motivo) para que el resto lo trate igual.
func errNoLib(err error) error {
	return fmt.Errorf("%w: %v", ErrUnsupported, err)
}

// cString es s terminada en cero, para pasarla a C.
func cString(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

var errDisabled = errors.New("native decoders disabled by AUDIOFILUS_DECODER=go")
