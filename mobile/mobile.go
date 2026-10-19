// Package mobile es audiofilus para iOS (y, más adelante, Android): lo que
// ebitenmobile convierte en un framework para la app de ios/ (ver
// docs/ios.md). En el móvil no se pueden lanzar procesos: el servicio de
// audio corre en una goroutine de la misma app y la interfaz le habla por su
// socket, como siempre. La app de Swift llama a Start en cuanto arranca, con
// sus carpetas; hasta entonces la vista se queda en negro.
package mobile

import (
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/mobile"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/ui"
)

// Host es lo que la app (Swift) hace por audiofilus: abrir el selector de
// carpetas del sistema. Lo elegido vuelve con SetMusicFolder.
type Host interface {
	PickFolder()
}

// game espera a Start: hasta entonces no hay App que correr.
type game struct {
	mu  sync.Mutex
	app *ui.App
}

var (
	g    = &game{}
	host Host
	once sync.Once
)

func init() { mobile.SetGame(g) }

func (g *game) current() *ui.App {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.app
}

func (g *game) Update() error {
	if a := g.current(); a != nil {
		return a.Update()
	}
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	if a := g.current(); a != nil {
		a.Draw(screen)
		return
	}
	screen.Fill(color.Black)
}

func (g *game) Layout(w, h int) (int, int) {
	if a := g.current(); a != nil {
		return a.Layout(w, h)
	}
	return ui.ViewSize(w, h)
}

// Start arranca audiofilus: home es la carpeta de sus datos (la de
// Documentos de la app, que se ve en Archivos), tmp una carpeta temporal
// (para el socket, con una ruta corta) y langs los idiomas del sistema
// ("es-VE,en-US"). Sin carpeta de música elegida, usa home: lo que el usuario
// copie ahí desde Archivos suena.
func Start(home, tmp, langs string) {
	once.Do(func() {
		if err := start(home, tmp, langs); err != nil {
			log.Printf("audiofilus: %v", err)
		}
	})
}

func start(home, tmp, langs string) error {
	debug.SetMemoryLimit(160 << 20)
	if lang, _, _ := strings.Cut(langs, ","); lang != "" {
		os.Setenv("LANG", strings.ReplaceAll(lang, "-", "_")+".UTF-8")
	}
	os.Setenv("AUDIOFILUS_HOME", home)
	// Los sockets de Unix no admiten rutas de más de 104 bytes y las de una
	// app de iOS se acercan: relativa a la carpeta temporal.
	if err := os.Chdir(tmp); err != nil {
		return err
	}
	os.Setenv("AUDIOFILUS_SOCKET", "audiofilus.sock")
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	if cfg, err := config.Load(paths); err == nil && len(cfg.MusicDirs) == 0 {
		cfg.MusicDirs = []string{filepath.Join(home, "Music")}
		_ = os.MkdirAll(cfg.MusicDirs[0], 0o755)
		_ = config.Save(paths, cfg)
	}
	ui.StartService = func(p config.Paths) error {
		go func() {
			if err := daemon.Run(p); err != nil {
				log.Printf("audio service: %v", err)
			}
		}()
		return nil
	}
	if host != nil {
		ui.PickFolder = host.PickFolder
	}
	if err := ui.LoadFonts(); err != nil {
		return err
	}
	if err := ui.LoadShaders(); err != nil {
		return err
	}
	app, err := ui.NewApp(paths, false)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.app = app
	g.mu.Unlock()
	return nil
}

// SetHost le da a audiofilus la app que abre el selector de carpetas.
// Llamarla antes de Start.
func SetHost(h Host) {
	host = h
	if h != nil {
		ui.PickFolder = h.PickFolder
	}
}

// SetMusicFolder pone la carpeta que el usuario eligió en el selector del
// sistema (con su permiso ya abierto por la app) y vuelve a leer la música.
func SetMusicFolder(path string) {
	if a := g.current(); a != nil {
		a.SetMusicFolder(path)
	}
}
