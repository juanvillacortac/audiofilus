package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/proc"
	"github.com/juanvillacortac/audiofilus/internal/update"
	"github.com/juanvillacortac/audiofilus/internal/version"
)

// runUpdate atiende `audiofilus update [-check]`: mira la última release y,
// si es más nueva, la instala (como Ajustes → Actualizar) y reinicia el
// servicio si estaba corriendo.
func runUpdate(paths config.Paths, args []string) int {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only say whether there is a newer version")
	_ = fs.Parse(args)
	exe, err := proc.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rel, err := update.Latest(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	if !update.Newer(version.Version, rel.Tag) {
		fmt.Printf("audiofilus %s is up to date (latest: %s)\n", version.Version, rel.Tag)
		return 0
	}
	fmt.Printf("audiofilus %s → %s\n", version.Version, rel.Tag)
	if *check {
		return 0
	}
	if update.Detect(exe) == update.None {
		fmt.Fprintln(os.Stderr, "audiofilus: this installation can't update itself; download it from", rel.URL)
		return 1
	}
	last := -1
	err = update.Install(context.Background(), rel, exe, func(d, t int64) {
		if t > 0 && int(d*100/t)/10 != last {
			last = int(d*100/t) / 10
			fmt.Printf("\rdownloading %d%%", last*10)
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	// El servicio, si corre, vuelve a arrancar con el binario nuevo.
	if _, err := control.Call(paths.Socket, control.Request{Cmd: "restart"}); err == nil {
		fmt.Println("service restarted")
	}
	fmt.Println("updated to", rel.Tag)
	return 0
}
