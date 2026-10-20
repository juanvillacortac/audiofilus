// Command audiofilus es todo audiofilus en un binario: la interfaz (sin
// argumentos, ver ui.go), el servicio de audio y su cliente de línea de
// comandos. Uno solo en vez de dos ahorra espacio en las portátiles.
//
//	audiofilus                    la interfaz
//	audiofilus daemon [-detach]   el servicio (en primer plano por defecto)
//	audiofilus play [rutas...]    reproduce archivos, carpetas o .m3u (arranca el servicio si hace falta)
//	audiofilus <comando> [args]   status, toggle, pause, resume, next, prev, seek,
//	                              shuffle, repeat, volume, queue, jump, add, sleep, awake, quit, bench
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/daemon/statusfmt"
	"github.com/juanvillacortac/audiofilus/internal/proc"
)

const usage = `usage:
  audiofilus                      open the player (the interface; starts the service)
  audiofilus daemon [-detach]     run the audio service
  audiofilus play [paths...]      play files, folders or .m3u (default: music_dirs)
  audiofilus add <paths...>       append to the queue
  audiofilus status [-json]       show what is playing
  audiofilus toggle|pause|resume|next|prev
  audiofilus seek <s>|+<s>|-<s>   jump to or move by seconds
  audiofilus jump <n>             play position n of the queue
  audiofilus shuffle [on|off]     (no argument toggles)
  audiofilus repeat off|all|one
  audiofilus volume <0..2>
  audiofilus queue
  audiofilus sleep [on|off]       app sleep: screen off + double-tap gestures
  audiofilus awake on [s] | off   keep the console awake while playing (lease, default 60 s)
  audiofilus quit                 stop the service
  audiofilus pad                  show the gamepad and its SDL mapping
  audiofilus ytm search|fetch|download <text>
                                  YouTube Music: search, get the first result to listen
                                  or save it to the library (needs yt-dlp next to the binary)
  audiofilus desktop install|uninstall
                                  add audiofilus to the Linux app launcher (Omarchy, GNOME, KDE...)

Config lives next to the binary: audiofilus.json and data/.
`

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	// Archivos o carpetas abiertos desde el explorador (open.go).
	if files, ok := openArgs(os.Args[1:]); ok {
		runOpen(files)
		return
	}
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") && os.Args[1] != "-h" && os.Args[1] != "--help" {
		runUI(os.Args[1:])
		return
	}
	paths, err := config.Locate()
	if err != nil {
		log.Fatal(err)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "daemon":
		runDaemon(paths, args)
	case "bench":
		os.Exit(runBench(args))
	case "pad":
		os.Exit(runPad())
	case "ytm":
		os.Exit(runYTM(args))
	case "desktop":
		os.Exit(runDesktop(args))
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		os.Exit(runClient(paths, cmd, args))
	}
}

// runDaemon atiende `audiofilus daemon`.
func runDaemon(paths config.Paths, args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	detach := fs.Bool("detach", false, "run in the background, logging to data/audiofilus.log")
	_ = fs.Parse(args)
	if *detach {
		if err := startDetached(paths); err != nil {
			log.Fatal(err)
		}
		return
	}
	// En ROCKNIX la interfaz corre en el scope del port, que se mata entero
	// al cerrarla: el servicio se muda a uno propio.
	proc.LeaveGameScope()
	err := daemon.Run(paths)
	switch {
	case errors.Is(err, daemon.ErrRestart):
		err = restartSelf() // restart_*.go
	case errors.Is(err, daemon.ErrAlreadyRunning):
		log.Print(err)
		return
	}
	if err != nil {
		log.Fatal(err)
	}
}

// startDetached vuelve a lanzar el daemon en una sesión nueva (setsid) para que
// sobreviva al lanzador: en Knulli se mata el grupo de procesos del port al salir.
func startDetached(paths config.Paths) error {
	if err := os.MkdirAll(paths.Data, 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, "daemon")
	// Con otro nombre en su línea de comandos: el servicio y la interfaz son
	// el mismo binario, y el atajo de salida de PortMaster (gptokeyb) cierra
	// la interfaz con pkill -f <binario>; así no se lleva la música.
	c.Args[0] = "audiofilusd"
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = detachAttr()
	c.Env = append(os.Environ(), "AUDIOFILUS_HOME="+paths.Base)
	if err := c.Start(); err != nil {
		return err
	}
	// Se espera a que responda el socket para que el siguiente comando lo encuentre.
	for i := 0; i < 50; i++ {
		if _, err := control.Call(paths.Socket, control.Request{Cmd: "status"}); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start; see %s", paths.Log)
}

// runClient envía un comando al daemon y pinta la respuesta.
func runClient(paths config.Paths, cmd string, args []string) int {
	asJSON := false
	if cmd == "status" && len(args) > 0 && args[0] == "-json" {
		asJSON, args = true, args[1:]
	}
	// Las rutas las resuelve el cliente, para que las relativas sean respecto a la
	// carpeta actual y no a la del daemon.
	if cmd == "play" || cmd == "add" {
		for i, a := range args {
			if strings.Contains(a, "://") {
				continue // remota (subsonic://...): tal cual
			}
			if abs, err := filepath.Abs(a); err == nil {
				args[i] = abs
			}
		}
	}
	req := control.Request{Cmd: cmd, Args: args}
	resp, err := control.Call(paths.Socket, req)
	if errors.Is(err, control.ErrNotRunning) && cmd == "play" {
		if err = startDetached(paths); err == nil {
			resp, err = control.Call(paths.Socket, req)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		return 1
	}
	if len(resp.Data) == 0 {
		return 0
	}
	if asJSON {
		os.Stdout.Write(resp.Data)
		fmt.Println()
		return 0
	}
	print(os.Stdout, cmd, resp.Data)
	return 0
}

// print pinta la respuesta de cmd en la terminal.
func print(w io.Writer, cmd string, data json.RawMessage) {
	if cmd == "queue" {
		var q []string
		if json.Unmarshal(data, &q) == nil {
			for i, p := range q {
				fmt.Fprintf(w, "%3d  %s\n", i, filepath.Base(p))
			}
		}
		return
	}
	var s daemon.FullStatus
	if err := json.Unmarshal(data, &s); err != nil {
		fmt.Fprintln(w, strings.TrimSpace(string(data)))
		return
	}
	fmt.Fprint(w, statusfmt.Format(s.Status, s.Sleep, s.Awake, s.Platform, s.Pad))
}
