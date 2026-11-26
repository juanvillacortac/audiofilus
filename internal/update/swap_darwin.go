//go:build darwin && !ios

package update

import "golang.org/x/sys/unix"

// swap intercambia las carpetas a y b de una vez (renamex_np con
// RENAME_SWAP): no hay un instante sin app, ni aunque se corte la luz.
// shortcut: solo en APFS y HFS+; en un disco sin RENAME_SWAP (exFAT, una
// carpeta de red) falla sin tocar nada, y si alguien tiene la app ahí, dos
// renombres.
func swap(a, b string) error { return unix.RenamexNp(a, b, unix.RENAME_SWAP) }
