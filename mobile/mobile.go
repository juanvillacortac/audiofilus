// Package mobile es audiofilus para iOS (y, más adelante, Android): lo que
// ebitenmobile convierte en un framework para la app de ios/ (ver
// docs/ios.md). En el móvil no se pueden lanzar procesos: el servicio de
// audio corre en una goroutine de la misma app y la interfaz le habla por su
// socket, como siempre. La app de Swift llama a Start en cuanto arranca, con
// sus carpetas; hasta entonces la vista se queda en negro.
package mobile

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// ShowKeyboard abre (show) o cierra el teclado del sistema, con el texto
	// que ya hay y si es una contraseña; lo que se escribe vuelve con
	// KeyboardText, Intro con KeyboardDone y bajarlo con KeyboardClosed.
	ShowKeyboard(show bool, text string, secret bool)
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
	// Cuándo fueron el último Update y el último Draw (UnixNano), para el
	// vigilante (watch).
	lastUpdate, lastDraw atomic.Int64
	// La zona segura (en puntos, de SetSafeArea) y lo que ocupa en el
	// lienzo: la vista llena la pantalla (el fondo llega a los bordes, bajo
	// la isla) y la interfaz va dentro, en su propio lienzo (frame).
	safe                   [4]float64 // arriba, izquierda, abajo, derecha
	padT, padL, padB, padR int
	appW, appH             int
	frame                  *ebiten.Image
}

// SetSafeArea pone la zona segura: cuántos puntos tapan la isla o la
// muesca, las esquinas y la raya de inicio, desde arriba, la izquierda,
// abajo y la derecha. La app la llama al empezar y cada vez que cambia
// (al girar el teléfono).
func SetSafeArea(top, left, bottom, right float64) {
	g.mu.Lock()
	g.safe = [4]float64{top, left, bottom, right}
	g.mu.Unlock()
}

// watch vigila que la interfaz siga viva: si pasan 4 s sin Update o sin Draw
// (con la app delante: en segundo plano es normal), guarda en el log lo que
// hace cada goroutine, una vez. Una interfaz que se queda congelada sin
// error está esperando algo, y ahí se ve qué.
func (g *game) watch(dir string) {
	dumped := false
	for range time.Tick(time.Second) {
		now := time.Now().UnixNano()
		u, d := g.lastUpdate.Load(), g.lastDraw.Load()
		if dumped || u == 0 || d == 0 || (now-u < 4e9 && now-d < 4e9) {
			continue
		}
		dumped = true
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		msg := fmt.Sprintf("UI stalled: %.1f s since Update, %.1f s since Draw (normal if the app was in the background)\n%s",
			float64(now-u)/1e9, float64(now-d)/1e9, buf)
		log.Print(msg)
		_ = os.WriteFile(filepath.Join(dir, "stall.txt"), []byte(msg), 0o644)
	}
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
		path   string
		data   []byte
		uiPath string // la portada que mandó la interfaz, y de qué canción
		uiData []byte
		last   *engine.Status // lo último que sonó, para volver a mandarlo con su portada
	}
)

func init() { mobile.SetGame(g) }

func (g *game) current() *ui.App {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.app
}

func (g *game) Update() error {
	g.lastUpdate.Store(time.Now().UnixNano())
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
	g.lastDraw.Store(time.Now().UnixNano())
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
	a := g.current()
	if a == nil {
		screen.Fill(color.Black)
		return
	}
	if g.padT+g.padL+g.padB+g.padR == 0 {
		a.Draw(screen)
		return
	}
	// El fondo en toda la pantalla y la interfaz encima, en la zona segura.
	if g.frame == nil || g.frame.Bounds().Dx() != g.appW || g.frame.Bounds().Dy() != g.appH {
		if g.frame != nil {
			g.frame.Deallocate()
		}
		g.frame = ebiten.NewImage(g.appW, g.appH)
	}
	g.frame.Clear()
	a.ExternalBackdrop = true
	a.Draw(g.frame)
	if a.Asleep() {
		screen.Fill(color.Black)
	} else {
		a.DrawBackdrop(screen)
		// Arriba y abajo, el oscuro de las barras: llegan hasta el borde.
		a.DrawInsets(screen, g.padT, g.padB)
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(g.padL), float64(g.padT))
	screen.DrawImage(g.frame, op)
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
	g.mu.Lock()
	s := g.safe
	g.mu.Unlock()
	// El lienzo de la interfaz es el de la zona segura; el de la pantalla,
	// ese más los márgenes, a la misma escala.
	aw, ah := w-int(s[1]+s[3]), h-int(s[0]+s[2])
	if aw <= 0 || ah <= 0 {
		aw, ah, s = w, h, [4]float64{}
	}
	if a := g.current(); a != nil {
		aw, ah = a.Layout(aw, ah)
	} else {
		aw, ah = ui.ViewSize(aw, ah)
	}
	scale := float64(aw) / float64(max(1, w-int(s[1]+s[3])))
	px := func(v float64) int { return int(math.Round(v * scale)) }
	g.padT, g.padL, g.padB, g.padR = px(s[0]), px(s[1]), px(s[2]), px(s[3])
	g.appW, g.appH = aw, ah
	if a := g.current(); a != nil {
		a.SetPointerOffset(g.padL, g.padT)
	}
	return aw + g.padL + g.padR, ah + g.padT + g.padB
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
	// La carpeta de la app cambia en cada instalación (otro contenedor): la
	// que da Swift, si es una ruta absoluta que existe; si no, la de este
	// proceso ($HOME/Documents).
	given := home
	if fi, err := os.Stat(home); !filepath.IsAbs(home) || err != nil || !fi.IsDir() {
		home = filepath.Join(os.Getenv("HOME"), "Documents")
	}
	home = filepath.Clean(home)
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
	ui.UpdateDir = filepath.Join(tmp, "update") // el APK nuevo (Android)
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	log.Printf("mobile: home %q (given %q), tmp %q, $HOME %q", home, given, tmp, os.Getenv("HOME"))
	// Sin carpeta de música, o con una de otra instalación (ya no existe):
	// la Music de la app, que se ve en Archivos.
	if cfg, err := config.Load(paths); err == nil {
		ok := false
		for _, d := range cfg.MusicDirs {
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				ok = true
			}
		}
		if !ok {
			cfg.MusicDirs = []string{filepath.Join(home, "Music")}
			_ = os.MkdirAll(cfg.MusicDirs[0], 0o755)
			_ = config.Save(paths, cfg)
		}
	}
	opening.Lock()
	socket = paths.Socket
	pending := opening.paths
	opening.paths = nil
	opening.Unlock()
	if len(pending) > 0 {
		go openWhenReady(pending)
	}
	daemon.NowPlayingHook = nowPlaying
	// La portada que tiene la interfaz (logos de radio, Subsonic...): se
	// guarda para esa canción y se vuelve a mandar lo que suena con ella.
	ui.NowPlayingArt = func(path string, png []byte) {
		art.Lock()
		art.uiPath, art.uiData = path, png
		last := art.last
		art.Unlock()
		if last != nil && last.Track.Path == path {
			nowPlaying(*last)
		}
	}
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
		ui.ShowKeyboard = host.ShowKeyboard
	}
	go g.watch(filepath.Join(home, "data"))
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

// Installer es lo que instala una versión nueva de la app: solo la app de
// Android de GitHub (la de Google Play y la de iOS las actualiza la tienda o
// AltStore). Aparte de Host para no pedírselo a iOS.
type Installer interface {
	// InstallPackage le pasa al sistema el APK bajado (y comprobado) en path.
	InstallPackage(path string)
}

// SetInstaller activa Ajustes → Actualizar con ese instalador. Llamarla
// antes de Start.
func SetInstaller(i Installer) {
	ui.InstallPackage = i.InstallPackage
}

// SetHost le da a audiofilus la app que abre el selector de carpetas.
// Llamarla antes de Start.
func SetHost(h Host) {
	host = h
	if h != nil {
		ui.PickFolder = h.PickFolder
		ui.ShowKeyboard = h.ShowKeyboard
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
	art.Lock()
	art.last = &st
	art.Unlock()
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
	if path == art.uiPath && art.uiData != nil {
		return art.uiData // la que tiene la interfaz (radios, servidores...)
	}
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

// opening son los archivos que llegaron ("Abrir en audiofilus" desde
// Archivos u otra app) antes de que arrancara el servicio.
var opening struct {
	sync.Mutex
	paths []string
}

// OpenFiles hace sonar los archivos abiertos con audiofilus desde otra app
// (las rutas, una por línea): los mismos que "audiofilus <archivos>" en el
// escritorio (el comando open del servicio).
func OpenFiles(paths string) {
	list := strings.Split(strings.TrimSpace(paths), "\n")
	opening.Lock()
	if socket == "" { // la app se abrió por ellos: aún arranca
		opening.paths = append(opening.paths, list...)
		opening.Unlock()
		return
	}
	opening.Unlock()
	go openWhenReady(list)
}

// openWhenReady manda los archivos al servicio en cuanto contesta (al abrir
// la app por ellos, tarda un momento en arrancar).
func openWhenReady(list []string) {
	for i := 0; i < 80; i++ {
		_, err := control.Call(socket, control.Request{Cmd: "open", Args: list})
		if err == nil {
			return
		}
		if !errors.Is(err, control.ErrNotRunning) {
			log.Printf("audiofilus: open: %v", err)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Back es el gesto Atrás del sistema: false si no hay adónde volver en la
// interfaz (la app pasa entonces a segundo plano).
func Back() bool {
	a := g.current()
	return a != nil && a.BackGesture()
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

// KeyboardText es el texto entero del teclado del sistema, tras cada cambio.
func KeyboardText(text string) {
	if a := g.current(); a != nil {
		a.SetKeyboardText(text)
	}
}

// KeyboardDone es Intro en el teclado del sistema.
func KeyboardDone() {
	if a := g.current(); a != nil {
		a.KeyboardDone()
	}
}

// KeyboardClosed es que el usuario bajó el teclado sin Intro.
func KeyboardClosed() {
	if a := g.current(); a != nil {
		a.KeyboardClosed()
	}
}
