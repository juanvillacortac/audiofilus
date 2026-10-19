package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/juanvillacortac/audiofilus/internal/desktop"
)

// runDesktop atiende `audiofilus desktop install|uninstall`: la entrada del
// lanzador y el icono en el escritorio Linux (Omarchy, GNOME, KDE...).
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
		fmt.Println("removed the launcher entry and icon")
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
	entry, icon, err := desktop.Install(filepath.Dir(exe))
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	fmt.Printf("installed %s\n          %s\n", entry, icon)
	return 0
}
