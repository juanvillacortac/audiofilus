package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
)

// runYTM prueba YouTube Music desde la línea de comandos (lo mismo que la
// interfaz, para diagnosticar el yt-dlp de una consola):
//
//	ytm search <texto>     las canciones que encuentra
//	ytm fetch <texto>      baja la primera a la caché, para escucharla
//	ytm download <texto>   la guarda en la primera carpeta de música
func runYTM(args []string) int {
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	paths, err := config.Locate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := ytm.Search(ctx, strings.Join(args[1:], " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	fmt.Printf("search: %d results in %v\n", len(res), time.Since(start).Round(time.Millisecond))
	if args[0] == "search" {
		for _, t := range res {
			fmt.Printf("%s  %s — %s (%s) %s\n", t.ID, t.Artist(), t.Title, t.Album, t.Duration)
		}
		return 0
	}
	if len(res) == 0 {
		return 1
	}
	bin := ytm.Find(paths.Base)
	if bin == "" {
		fmt.Fprintln(os.Stderr, "audiofilus: no yt-dlp next to the binary (put it in yt-dlp/ or on the PATH)")
		return 1
	}
	t := res[0]
	fmt.Printf("yt-dlp: %s\n%s — %s\n", bin, t.Artist(), t.Title)
	start = time.Now()
	last := ""
	progress := func(p ytm.Progress) {
		line := p.Stage
		if p.Stage == "download" && p.Total > 0 {
			line = fmt.Sprintf("download %d%%", 100*p.Done/p.Total)
		}
		if line != last {
			fmt.Printf("  %5.1fs %s\n", time.Since(start).Seconds(), line)
			last = line
		}
	}
	var path string
	switch args[0] {
	case "fetch":
		path, err = ytm.Fetch(ctx, bin, t, filepath.Join(os.TempDir(), "audiofilus-ytm"), 15, progress)
	case "download":
		cfg, cerr := config.Load(paths)
		if cerr != nil || len(cfg.MusicDirs) == 0 {
			fmt.Fprintln(os.Stderr, "audiofilus: no music_dirs in audiofilus.json")
			return 1
		}
		path, err = ytm.Download(ctx, bin, t, cfg.MusicDirs[0], filepath.Join(os.TempDir(), "audiofilus-ytm"), progress)
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	fmt.Printf("%s in %v\n", path, time.Since(start).Round(time.Millisecond))
	return 0
}
