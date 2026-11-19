package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
	"github.com/juanvillacortac/audiofilus/internal/proc"
	"github.com/juanvillacortac/audiofilus/internal/sdlmap"
)

// runLaunch atiende `audiofilus launch [petición.json]`: lanza el juego que
// dejó el modo juego de la interfaz (esdata.RequestPath), como lo haría
// EmulationStation: los mandos que ve SDL en %CONTROLLERSCONFIG%, el entorno
// de Knulli (sin el de PortMaster) y los scripts de usuario game-start y
// game-end. Espera a que termine el juego. Lo llama el lanzador del port,
// con la interfaz ya cerrada: así el juego tiene la pantalla para él.
func runLaunch(args []string) int {
	file := esdata.RequestPath
	if len(args) > 0 {
		file = args[0]
	}
	q, err := esdata.LoadRequest(file)
	os.Remove(file) // una sola vez, aunque falle
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus launch:", err)
		return 1
	}
	var argv []string
	for _, a := range q.Args {
		if a == esdata.ControllersToken {
			argv = append(argv, controllerArgs()...)
			continue
		}
		argv = append(argv, a)
	}
	log.Printf("launch: %q", argv)
	base := strings.TrimSuffix(path.Base(q.Rom), path.Ext(q.Rom))
	runESScripts("game-start", q.Rom, base, q.Name)
	restore := boostGame() // Syncthing quieto y el juego primero (gameboost_linux.go)
	defer restore()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = knulliEnv()
	cmd.Dir = "/userdata"
	if f, err := os.Create("/tmp/audiofilus-game.log"); err == nil {
		defer f.Close()
		cmd.Stdout, cmd.Stderr = f, f
	}
	start := time.Now()
	// Con prioridad normal, como lo lanza ES: emulatorlauncher ya le pone
	// nice -4 al juego, y sobre el -4 que este proceso hereda del port
	// quedaba en -8. El ahorro agresivo de Knulli busca exactamente -4.
	if err := proc.NormalPriority(cmd.Run); err != nil {
		log.Printf("launch: %v", err)
	}
	played := time.Since(start)
	restore() // ya, antes de escribir el gamelist (y otra vez, sin efecto, al salir)
	log.Printf("launch: back after %s", played.Round(time.Second))
	// La partida, en su gamelist.xml (como hace ES): una más, la fecha y el
	// tiempo jugado.
	if q.Dir != "" {
		if err := esdata.Root("/").RecordPlay(q.Dir, q.Rom, q.Name, played, time.Now()); err != nil {
			log.Printf("launch: recording the play: %v", err)
		}
	}
	runESScripts("game-end")
	return 0
}

// controllerArgs son los argumentos de los mandos para emulatorlauncher, como
// los arma EmulationStation (InputManager::configureEmulators): un jugador
// por mando, en el orden de SDL.
func controllerArgs() []string {
	joys, err := sdlmap.Joysticks()
	if err != nil {
		log.Printf("launch: controllers: %v", err)
		return nil
	}
	var out []string
	for i, j := range joys {
		if i >= 8 {
			break
		}
		p := fmt.Sprintf("-p%d", i+1)
		out = append(out,
			p+"index", fmt.Sprint(j.Index),
			p+"guid", j.GUID,
			p+"name", j.Name,
			p+"devicepath", j.Path,
			p+"nbbuttons", fmt.Sprint(j.Buttons),
			p+"nbhats", fmt.Sprint(j.Hats),
			p+"nbaxes", fmt.Sprint(j.Axes),
		)
	}
	return out
}

// knulliEnv es el entorno para el juego: el de ahora sin lo que pone
// PortMaster para Audiofilus (su mapeo de SDL, el backend de la interfaz) y
// con lo que pone EmulationStation.
func knulliEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "SDL_GAMECONTROLLERCONFIG", k == "SDL_GAMECONTROLLERCONFIG_FILE",
			strings.HasPrefix(k, "EBITENGINE_"), strings.HasPrefix(k, "AUDIOFILUS_"),
			k == "HOME", k == "XDG_CONFIG_HOME", k == "XDG_RUNTIME_DIR":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME=/userdata/system", "XDG_CONFIG_HOME=/userdata/system/configs",
		"XDG_RUNTIME_DIR=/var/run", "SDL_NOMOUSE=1")
}

// runESScripts ejecuta los scripts de usuario del evento de
// EmulationStation (scripts/<evento>/ en su carpeta), con sus argumentos.
func runESScripts(event string, args ...string) {
	for _, dir := range []string{"/usr/share/emulationstation/scripts", "/userdata/system/configs/emulationstation/scripts"} {
		files, _ := filepath.Glob(filepath.Join(dir, event, "*"))
		for _, f := range files {
			if st, err := os.Stat(f); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
				continue
			}
			if err := proc.NormalPriority(exec.Command(f, args...).Run); err != nil {
				log.Printf("launch: %s: %v", f, err)
			}
		}
	}
}
