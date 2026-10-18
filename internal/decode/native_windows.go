package decode

import "syscall"

// loadLib abre una DLL. Windows la busca primero en la carpeta del programa:
// basta con dejar ahí las de MSYS2 (libmpg123-0.dll, libopusfile-0.dll...).
func loadLib(name string) (uintptr, error) {
	h, err := syscall.LoadLibrary(name)
	return uintptr(h), err
}

// libSym busca un símbolo en la DLL.
func libSym(lib uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(lib), name)
}
