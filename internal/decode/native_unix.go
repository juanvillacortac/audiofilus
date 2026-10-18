//go:build !windows

package decode

import "github.com/ebitengine/purego"

// loadLib abre una biblioteca compartida (.so) con dlopen.
func loadLib(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// libSym busca un símbolo en la biblioteca.
func libSym(lib uintptr, name string) (uintptr, error) { return purego.Dlsym(lib, name) }
