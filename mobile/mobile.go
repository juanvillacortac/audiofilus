// Package mobile es audiofilus para iOS (y, más adelante, Android): lo que
// ebitenmobile convierte en un framework para la app de ios/ (ver
// docs/ios.md). En el móvil no se pueden lanzar procesos: el servicio de
// audio corre en una goroutine de la misma app y la interfaz le habla por su
// socket, como siempre. La app de Swift llama a Start en cuanto arranca, con
// sus carpetas; hasta entonces la vista se queda en negro.
package mobile

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/mobile"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/ui"
)

// Host es lo que la app (Swift) hace por audiofilus: abrir el selector de
// carpetas del sistema (lo elegido vuelve con SetMusicFolder) y mostrar lo
// que suena en la pantalla bloqueada y el Centro de control
// (MPNowPlayingInfoCenter; sus botones vuelven con Toggle, Next...).
type Host interface {
	PickFolder()
	// NowPlaying es lo que suena: la duración y la posición en segundos, si
	// suena o está en pausa, y la portada (JPEG o PNG; vacía si no hay).
	NowPlaying(title, artist, album string, duration, position float64, playing bool, artwork []byte)
}

// game espera a Start: hasta entonces no hay App que correr. Si algo falla
// (un panic, un error de Update), lo muestra en pantalla en vez de dejar la
// imagen congelada (Ebitengine para el bucle sin cerrar la app), y queda en
// el log (data/audiofilus.log, en Archivos).
type game struct {
	mu     sync.Mutex
	app    *ui.App
	failed string
}

// fail apunta el error y lo deja a la vista.
func (g *game) fail(msg string) {
	log.Printf("audiofilus: %s", msg)
	g.mu.Lock()
	if g.failed == "" {
		g.failed = msg
	}
	g.mu.Unlock()
}

// broken es el error que se muestra, si lo hay.
func (g *game) broken() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.failed
}

var (
	g      = &game{}
	host   Host
	once   sync.Once
	socket string // el del servicio, para los botones del sistema
	art    struct {
		sync.Mutex
		path string
		data []byte
	}
)

func init() { mobile.SetGame(g) }

func (g *game) current() *ui.App {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.app
}

func (g *game) Update() error {
	defer func() {
		if r := recover(); r != nil {
			g.fail(fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
		}
	}()
	if a := g.current(); a != nil && g.broken() == "" {
		if err := a.Update(); err != nil && err != ebiten.Termination {
			g.fail(err.Error())
		}
	}
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	defer func() {
		if r := recover(); r != nil {
			g.fail(fmt.Sprintf("panic while drawing: %v\n%s", r, debug.Stack()))
		}
	}()
	if msg := g.broken(); msg != "" {
		screen.Fill(color.RGBA{0x30, 0, 0x10, 0xff})
		ebitenutil.DebugPrint(screen, "audiofilus stopped:\n"+wrap(msg, screen.Bounds().Dx()/6))
		return
	}
	if a := g.current(); a != nil {
		a.Draw(screen)
		return
	}
	screen.Fill(color.Black)
}

// wrap parte msg en líneas de n caracteres como mucho (para DebugPrint).
func wrap(msg string, n int) string {
	var b strings.Builder
	for _, line := range strings.Split(msg, "\n") {
		for len(line) > n && n > 0 {
			b.WriteString(line[:n] + "\n")
			line = line[n:]
		}
		b.WriteString(line + "\n")
	}
	return b.String()
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
			g.fail("starting: " + err.Error())
		}
	})
}

func start(home, tmp, langs string) error {
	debug.SetMemoryLimit(160 << 20)
	if lang, _, _ := strings.Cut(langs, ","); lang != "" {
		os.Setenv("LANG", strings.ReplaceAll(lang, "-", "_")+".UTF-8")
	}
	os.Setenv("AUDIOFILUS_HOME", home)
	// El log, en un archivo que se ve en Archivos (data/audiofilus.log).
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(home, "data", "audiofilus.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			log.SetOutput(f)
		}
	}
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
	socket = paths.Socket
	daemon.NowPlayingHook = nowPlaying
	// Un solo contexto de audio por proceso: la interfaz suena por el del
	// servicio (lo espera hasta 10 s; sin él, sin sonidos).
	engines := make(chan *engine.Engine, 1)
	daemon.OnEngine = func(e *engine.Engine) { engines <- e }
	var shared *engine.Engine
	var sharedOnce sync.Once
	ui.SharedAudio = func() (*oto.Context, func()) {
		sharedOnce.Do(func() {
			select {
			case shared = <-engines:
			case <-time.After(10 * time.Second):
			}
		})
		if shared == nil {
			return nil, nil
		}
		return shared.Context(), shared.Idle
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

// nowPlaying le pasa a la app lo que suena (lo llama el servicio en cada
// cambio).
func nowPlaying(st engine.Status) {
	if host == nil {
		return
	}
	t := st.Track
	title := t.Title
	if title == "" && t.Path != "" {
		title = strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
	}
	host.NowPlaying(title, t.Artist, t.Album, st.Duration, st.Position, st.State == "playing", artwork(t.Path))
}

// artwork es la portada de la canción en path: la que lleva dentro o, si no,
// la imagen de su carpeta (cover.jpg, folder.jpg...). Se recuerda la última.
func artwork(path string) []byte {
	art.Lock()
	defer art.Unlock()
	if path == art.path {
		return art.data
	}
	art.path, art.data = path, nil
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil && m.Picture() != nil {
			art.data = m.Picture().Data
		}
		f.Close()
	}
	if art.data == nil && path != "" {
		for _, name := range []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png", "front.jpg", "Cover.jpg", "Folder.jpg"} {
			if b, err := os.ReadFile(filepath.Join(filepath.Dir(path), name)); err == nil {
				art.data = b
				break
			}
		}
	}
	return art.data
}

// call manda un comando al servicio (los botones del sistema).
func call(cmd string, args ...string) {
	if socket == "" {
		return
	}
	if _, err := control.Call(socket, control.Request{Cmd: cmd, Args: args}); err != nil {
		log.Printf("audiofilus: %s: %v", cmd, err)
	}
}

// Play, Pause, Toggle, Next y Previous son los botones de la pantalla
// bloqueada, el Centro de control y los auriculares.
func Play()     { call("resume") }
func Pause()    { call("pause") }
func Toggle()   { call("toggle") }
func Next()     { call("next") }
func Previous() { call("prev") }

// SeekTo salta a seconds (la barra de la pantalla bloqueada).
func SeekTo(seconds float64) { call("seek", strconv.FormatFloat(seconds, 'f', 1, 64)) }
