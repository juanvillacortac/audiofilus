package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/juanvillacortac/audiofilus/internal/desktop"
)

// runDesktop atiende `audiofilus desktop install|uninstall`: en Linux, la
// entrada del lanzador (con los tipos que abre) y el icono (Omarchy, GNOME,
// KDE...); en Windows, "Abrir con" en el registro del usuario.
func runDesktop(args []string) int {
	if len(args) != 1 || (args[0] != "install" && args[0] != "uninstall") {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if args[0] == "uninstall" {
		if err := desktop.Uninstall(); err != nil {
			fmt.Fprintln(os.Stderr, "audiofilus:", err)
			return 1
		}
		fmt.Println("removed the launcher entry and file associations")
		return 0
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	entry, icon, err := desktop.Install(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	fmt.Printf("installed %s\n          %s\n", entry, icon)
	return 0
}
