package ui

import (
	"context"
	"errors"
	"fmt"
	"github.com/juanvillacortac/audiofilus/internal/podcast"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"github.com/juanvillacortac/audiofilus/internal/tracker"
	"github.com/juanvillacortac/audiofilus/internal/ytm"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
)

// mode es en qué está la interfaz.
type mode int

const (
	modeScan   mode = iota // buscando música
	modePicker             // eligiendo la carpeta de música
	modeMain               // las pantallas normales
)

// App es la interfaz. Lo que suena lo sabe el servicio de audio (Client); la
// interfaz solo manda comandos y pinta su estado.
type App struct {
	paths   config.Paths
	cfg     config.Config
	client  *Client
	offline bool // maqueta sin servicio (capturas y pruebas de memoria)

	mode           mode
	scan           *Scan
	picker         *Picker
	firstRun       bool
	lib            *Library
	covers         *covers
	input          Input
	warm           bool
	screen         Screen
	settingsSel    int
	settingsEdit   bool    // se está eligiendo el valor de la baldosa
	settingsQR     bool    // se ve un QR (el de qrURL)
	qrURL          string  // el repositorio o las donaciones
	settingsScroll float64 // desplazamiento del mosaico, en píxeles
	libTab         int     // sección de la biblioteca
	tabFocus       bool    // el foco está en las secciones, no en la vista
	stacks         [tabCount][]*view
	menu           *menu // menú contextual abierto, o nil
	queueSel       int
	queueMoved     bool // se movió la selección en la cola (si no, sigue a lo que suena)
	queueAt        float64

	// El sleep de la app: pantalla apagada (la apaga el servicio) y la
	// interfaz casi parada.
	quitting  bool    // se pidió salir: Update termina el bucle
	splashing bool    // se está mostrando el splash
	splashT   float64 // segundos de splash

	// El indicador de volumen: aparece un momento al cambiar. volume es el
	// volumen en la escala única (0..2: el sistema hasta 1, el refuerzo de la
	// app por encima).
	volume    float64
	volumeAt  float64 // cuándo cambió
	volumeIn  float64 // cuándo empezó a verse el indicador (para que entre una vez)
	volumeSeq int     // último VolumeSeq del servicio
	powerSeq  int     // último PowerSeq del servicio (botón de encendido: sleep)
	sleeping  bool    // la pantalla está apagada (en sleep)
	sleepSeen bool    // el servicio ya confirmó que está en sleep
	sleepFade float64 // segundos del fundido en curso
	fadingOut bool    // disolviéndose hacia el sleep
	fadingIn  bool    // volviendo del sleep
	frozen    bool    // en sleep, con los fotogramas parados
	// woke lo pone el cliente (desde otro hilo) cuando el servicio sale del
	// sleep: con los fotogramas parados, la interfaz no ve el sleep del servicio
	// y no puede deducirlo por el cambio de estado.
	woke atomic.Bool

	// La barra de progreso: shownPos sigue a position y, en un salto (adelantar,
	// retrasar), llega con una transición en vez de saltar. seekWant y seekAt
	// adelantan el salto en la interfaz mientras el servicio responde.
	shownPos float64
	letter   string // la letra del último salto (L/R en la biblioteca)

	juice   juiceState // las animaciones de la pantalla de reproducción (juice.go)
	beat    beat       // el ritmo de la canción (meter.go)
	steady  steadyGrid // su rejilla suave, para los juegos (meter.go)
	beatHit bool       // en este fotograma hay un golpe

	tetris  *tetris       // Bloques (tetris.go): la partida, que se queda al cambiar de pantalla
	rhythm  *rhythmGame   // Ritmo (rhythm.go), igual
	crypt   *crypt        // Cripta (crypt.go), igual
	tracker *trackerState // el tracker (trackerui.go): la canción y su reproductor
	quiz    *quizGame     // ¿Qué canción es? (quiz.go)
	snake   *snakeGame    // Serpiente (snake.go)
	shooter *shooterGame  // Naves (shooter.go)
	duel    *duelGame     // Duelo (duel.go)
	// La pantalla de Juegos (games.go): la tarjeta elegida y los récords leídos.
	gamesSel    int
	gamesBest   map[string]int
	gamesBestAt float64
	search      searchState  // Buscar (search.go)
	drift       *driftState  // Derrape (drift.go)
	trench      *trenchState // Trinchera (trench.go)
	lake        *lakeState   // Lago (lake.go)
	vis         visState     // los visualizadores (visual.go)
	batch       rectBatch    // rectángulos en un solo dibujo (icons.go)

	dock       dockState         // la barra de pantallas (Select)
	prompt     *promptState      // un nombre que se está escribiendo (playlists.go)
	yt         ytState           // YouTube Music, si hay yt-dlp (ytmusic.go)
	radio      radioState        // la radio por internet (radio.go)
	pod        podState          // los podcasts (podcasts.go)
	snd        soundState        // los sonidos de la interfaz (sounds.go)
	local      *Library          // la biblioteca de la tarjeta (a.lib es la que se ve)
	remotes    []*remoteSource   // los servidores (servers.go)
	source     int               // la que se ve: 0 la tarjeta, i>0 el servidor i-1
	store      *remote.Store     // la caché de lo remoto
	serversCh  chan remoteLoaded // catálogos que llegan
	pingCh     chan pingResult   // pruebas de servidores
	prevScreen Screen            // la pantalla de antes (Select dos veces, B)
	guideAt    float64           // desde cuándo se ve la guía de controles (FN)
	startAt    float64           // cuándo se pulsó Start sin menú (doble Start: sleep)

	// La transición entre pantallas (transition.go).
	lastScreen Screen
	transFrom  Screen
	transAt    float64
	transImg   *ebiten.Image
	transNew   *ebiten.Image
	transReady bool
	transDir   int

	vizBars, vizPeaks []float64 // las barras del visualizador y sus picos
	vizAvg            []float64 // la media reciente de cada barra (para el contraste)

	// Lo que lleva pulsado izquierda o derecha en la pantalla de reproducción
	// (ver tapOrHold).
	leftHold, rightHold holdState

	shuffleAt     float64 // cuándo se pulsó X (aleatorio) por última vez
	shuffleBefore bool    // cómo estaba el aleatorio antes de esa pulsación
	letterAt      float64 // cuándo se saltó
	seekWant      float64
	seekAt        float64
	lastInput     float64 // cuándo se tocó algo por última vez

	// El estado del servicio y lo que se deriva de él.
	status      daemon.FullStatus
	playing     *Album // nil si no suena nada de la biblioteca
	playingSong Song
	playingIdx  int
	isPlaying   bool
	position    float64
	duration    float64
	lastTrack   string
	spin        float64 // ángulo del vinilo
	spinVel     float64
	konami      []Action // las últimas pulsaciones, por si son el código Konami (eggs.go)
	celebrateAt float64  // cuándo empezó la celebración del día especial
	shotDay     int      // en las capturas, el día especial que se finge

	theme    Theme
	themeFor *Album // el tema se tiñe con este álbum en cuanto se lea su portada

	t        float64 // segundos desde que arrancó, para las animaciones
	now      time.Time
	battery  float64 // 0..1; negativo si no hay batería
	charging bool    // enchufada: cargando o ya llena
	batAt    time.Time

	message   string // aviso temporal (errores)
	messageAt float64

	// Capturas y prueba de memoria (modo maqueta, sin servicio).
	ShotsDir string
	// ShotsAlbum elige qué suena en las capturas: el primer álbum cuyo artista
	// o título lo contiene (sin él, el primero con portada y 5 canciones).
	ShotsAlbum string
	warmed     bool // ya se dibujó todo una vez (ver updateShots)
	// FramesDir, con ShotsDir, guarda además una secuencia de la pantalla de
	// reproducción (para hacer un GIF de las animaciones).
	FramesDir  string
	ScrollTest bool
	// BenchVisual, sin servicio, mide un visualizador (bench.go) y termina;
	// BenchProfile es dónde guardar su perfil de CPU.
	BenchVisual  string
	BenchProfile string
	bench        benchState
	frames       int
	shotsErr     error
	peakRSS      int64
	startRSS     int64
	peakHeap     uint64
	w            int
}

// NewApp prepara la interfaz. Con offline (las capturas y la prueba de
// memoria) no se conecta al servicio y la reproducción se simula.
func NewApp(paths config.Paths, offline bool) (*App, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		return nil, err
	}
	SetLanguage(cfg.Language)
	a := &App{
		libTab: tabSongs, // Buscar es la primera pestaña, pero se entra por Canciones
		paths:  paths, cfg: cfg, offline: offline,
		covers: newCovers(2), theme: DefaultTheme, battery: -1, now: time.Now(),
	}
	a.input.swapAB = cfg.UI.SwapAB
	a.splashing = !offline
	if !offline {
		a.client = NewClient(paths)
		a.client.OnWake = func() {
			a.woke.Store(true)
			ebiten.ScheduleFrame()
		}
		if err := a.client.Start(); err != nil {
			return nil, err
		}
	}
	a.initYT()
	a.initServers()
	a.initRadio()
	a.initPodcasts()
	if !offline && (sfx.Gain(cfg.UI.Sounds) > 0 || sfx.Gain(cfg.UI.UISounds) > 0) {
		a.startSounds() // la salida de audio se prepara ya, en segundo plano
	}
	a.search.row = 1 // el teclado en pantalla empieza en la Q, no en los números
	if !hasMusicDir(cfg.MusicDirs) {
		a.firstRun = true
		a.picker = newPicker("")
		a.mode = modePicker
	} else {
		a.startScan()
	}
	return a, nil
}

// hasMusicDir dice si alguna de las carpetas configuradas existe.
func hasMusicDir(dirs []string) bool {
	for _, d := range dirs {
		if isDir(d) {
			return true
		}
	}
	return false
}

// SetMusicDir usa dir como carpeta de música sin guardarla (para la maqueta).
func (a *App) SetMusicDir(dir string) {
	a.cfg.MusicDirs = []string{dir}
	a.firstRun = false
	a.startScan()
}

// startScan busca la música de la configuración en segundo plano.
func (a *App) startScan() {
	a.scan = StartScan(a.paths, a.cfg.MusicDirs)
	a.mode = modeScan
}

// Close suelta el servicio: la música sigue sonando en segundo plano.
func (a *App) Close() {
	if a.sleeping {
		a.send("sleep", "off")
	}
	if a.client != nil {
		a.client.Close()
	}
}

// send manda un comando al servicio (en la maqueta no hace nada).
func (a *App) send(cmd string, args ...string) {
	a.soundFromSend(cmd, args)
	if a.client != nil {
		a.client.Send(cmd, args...)
	}
}

// flash muestra un aviso unos segundos.
func (a *App) flash(msg string) {
	a.message, a.messageAt = msg, a.t
}

// exit cierra la interfaz. Con background la música sigue sonando en segundo
// plano; si no, se para (y el servicio se cierra). Cerrar la ventana o que la
// interfaz muera también la para: solo se queda sonando si se pide.
func (a *App) exit(background bool) {
	if a.client != nil {
		if background {
			a.client.Background()
		} else {
			a.client.StopService()
		}
	}
	a.quitting = true
}

// Update atiende los botones y sigue al servicio.
func (a *App) Update() error {
	if a.quitting {
		return ebiten.Termination
	}
	a.input.update()
	a.covers.collect(a.t)
	a.updateTheme()
	if a.ShotsDir != "" {
		return a.updateShots()
	}
	defer a.soundFromInput() // el sonido de este fotograma, al final (sounds.go)
	a.t += 1.0 / 60
	a.now = time.Now()
	a.watchKonami()
	a.readBattery()
	if a.splashing && a.updateSplash() {
		return nil // el splash se come los botones; la biblioteca carga detrás
	}

	switch a.mode {
	case modeScan:
		if lib, err, ok := a.scan.Result(); ok {
			if err != nil || len(lib.Songs) == 0 {
				if err != nil {
					a.flash(err.Error())
				} else {
					a.flash(T("no_music"))
				}
				a.picker = newPicker(firstOr(a.cfg.MusicDirs, ""))
				a.mode = modePicker
				break
			}
			a.setLocal(lib) // y lo que suena se vuelve a buscar en la biblioteca nueva
			a.mode = modeMain
			if a.offline {
				a.simulatePlaying()
			}
		}
		return nil
	case modePicker:
		if dir := a.picker.update(&a.input); dir != "" {
			a.cfg.MusicDirs = []string{dir}
			a.firstRun = false
			a.saveConfig()
			a.startScan()
		} else if a.lib != nil && !a.firstRun && a.picker.dir == "" && a.input.Pressed(ActBack) {
			a.mode = modeMain // salir del explorador sin cambiar nada
		}
		return nil
	}

	a.syncStatus()
	a.updateYT()
	a.updateServers()
	a.updateRadioResults()
	a.updatePodResults()
	a.trackPodcast()
	a.updatePings()
	a.watchProfile()
	a.tweenPosition()
	a.beatHit = a.updateBeat() // el ritmo, una vez por fotograma, para todos
	a.updatePump()
	if a.ScrollTest {
		return a.scrollTest()
	}
	if a.BenchVisual != "" {
		return a.benchVisual()
	}
	if a.input.any {
		a.lastInput = a.t
	}
	if !a.sleeping && a.lidClosed() {
		// Cerrar la tapa apaga la pantalla enseguida, sin fundido (ya no se ve).
		a.menu, a.fadingOut, a.fadingIn = nil, false, false
		a.goToSleep()
	}
	if a.sleeping {
		a.updateSleep()
		return nil
	}
	if a.fadingOut {
		a.updateFadeOut()
		return nil
	}
	if a.fadingIn {
		a.sleepFade += 1.0 / 60
		a.fadingIn = a.sleepFade < fadeInLen
	}
	a.updateMain()
	a.maybeSleep()
	return nil
}

// Los fundidos del sleep, con la trama de 8×8.
const (
	fadeOutLen = 0.5
	fadeInLen  = 0.25
)

// updateFadeOut avanza la disolución hacia el sleep; tocar algo la cancela.
func (a *App) updateFadeOut() {
	if a.input.any {
		a.fadingOut = false
		a.fadingIn, a.sleepFade = true, fadeInLen*(1-min(1, a.sleepFade/fadeOutLen))
		a.lastInput = a.t
		return
	}
	a.sleepFade += 1.0 / 60
	if a.sleepFade >= fadeOutLen {
		a.fadingOut = false
		a.goToSleep()
	}
}

// maybeSleep entra en el sleep tras un rato sin tocar nada mientras suena.
func (a *App) maybeSleep() {
	after := a.cfg.UI.SleepAfter
	// Sonando, solo en la pantalla de reproducción (navegando o en Ajustes, no).
	// Sin música, en cualquiera: con la app abierta la consola no se suspende,
	// así que si nadie la usa, al menos se apaga la pantalla. Jugando, nunca.
	if after <= 0 || a.menu != nil || a.screen == Blocks || a.screen == Rhythm || a.screen == Crypt || a.screen == Tracker || a.screen == Quiz || a.screen == Snake || a.screen == Shooter || a.screen == Duel || a.screen == Visual || (a.isPlaying && a.screen != NowPlaying) || a.t-a.lastInput < float64(after) {
		return
	}
	a.enterSleep()
}

// enterSleep empieza a apagar la pantalla: primero se disuelve en negro.
func (a *App) enterSleep() {
	a.menu = nil
	a.fadingOut, a.fadingIn, a.sleepFade = true, false, 0
	a.woke.Store(false)
}

// goToSleep apaga la pantalla de verdad y casi para la interfaz: a 10
// actualizaciones por segundo, pintando negro. En modo "brillo" el servicio
// apaga la retroiluminación; en modo OLED la deja (en una OLED el negro ya es
// apagado). Los gestos de los gatillos los atiende el servicio.
func (a *App) goToSleep() {
	args := []string{"on"}
	if a.cfg.UI.SleepMode == "oled" {
		args = append(args, "oled")
	}
	a.send("sleep", args...)
	a.sleeping, a.sleepSeen = true, false
	ebiten.SetTPS(10)
}

// exitSleep vuelve a la interfaz normal, con la trama al revés.
func (a *App) exitSleep(tellService bool) {
	if tellService {
		a.send("sleep", "off")
	}
	a.sleeping, a.frozen = false, false
	a.fadingIn, a.sleepFade = true, 0
	a.lastInput = a.t
	ebiten.SetFPSMode(ebiten.FPSModeVsyncOn)
	ebiten.SetTPS(ebiten.DefaultTPS)
}

// updateSleep espera a que se despierte: con Start (lo ve el servicio, que sale
// solo del sleep) o con una tecla en el PC. Los botones del mando no
// despiertan: en el bolsillo se pulsan sin querer, y los gestos son del servicio.
func (a *App) updateSleep() {
	a.t += 5.0 / 60 // las animaciones no importan: solo cuenta el tiempo
	if !a.frozen && !a.offline {
		// Ya se pintó el negro: desde aquí Ebitengine no hace ningún fotograma
		// (ni Update ni Draw ni presentar) salvo que llegue un evento o que el
		// servicio avise de que despertó (Client.OnWake). La imagen negra se
		// queda en pantalla.
		a.frozen = true
		ebiten.SetFPSMode(ebiten.FPSModeVsyncOffMinimum)
		// Un Update por fotograma: con TPS fijo, tras un rato quieto Ebitengine
		// hace varios Update seguidos para ponerse al día y el fotograma
		// siguiente se queda sin ninguno (y no ve, p. ej., soltar un botón).
		ebiten.SetTPS(ebiten.SyncWithFPS)
	}
	if a.status.Sleep {
		a.sleepSeen = true
	}
	switch {
	case a.woke.Swap(false), a.sleepSeen && !a.status.Sleep:
		a.exitSleep(false)
	case len(a.input.keys) > 0 && a.status.Platform == "desktop":
		// El teclado despierta solo en el PC. En las portátiles hay "teclas"
		// que no son tocar la pantalla: en la RG34XX-SP la tapa manda Insert al
		// cerrarse (y Delete al abrirse) por el mismo dispositivo del mando.
		// Allí despierta el servicio (Start, ver internal/daemon/pad.go).
		a.exitSleep(true)
	case a.offline && a.t-a.lastInput > 2:
		a.exitSleep(true)
	}
}

// updateMain son los botones de las pantallas normales.
func (a *App) updateMain() {
	if a.input.Pressed(ActMenu) && a.menu == nil {
		a.startAt = a.t // para el doble Start (updateMenu)
	}
	if a.updateGuide() {
		return // con FN mantenido solo se mira la guía (Knulli usa FN+botón)
	}
	if a.prompt != nil {
		a.updatePrompt()
		return
	}
	if a.updateDock() {
		return
	}
	if a.searchTyping() {
		a.updateSearch() // escribiendo: las teclas son del buscador, no atajos
		return
	}
	if a.screen == Blocks && a.menu == nil {
		if a.tetris == nil {
			a.tetris = a.newTetris()
		}
		a.updateTetris()
		return
	}
	if a.screen == Rhythm && a.menu == nil {
		if a.rhythm == nil {
			a.rhythm = a.newRhythm()
		}
		a.updateRhythm()
		return
	}
	if a.screen == Crypt && a.menu == nil {
		if a.crypt == nil {
			a.crypt = a.newCrypt()
		}
		a.updateCrypt()
		return
	}
	a.trackerStopIfAway()
	if a.screen == Tracker && a.menu == nil {
		if a.tracker == nil {
			a.tracker = a.newTracker()
		}
		a.updateTracker()
		return
	}
	if a.screen == Quiz && a.menu == nil {
		if a.quiz == nil {
			a.quiz = a.newQuiz()
			a.quizRound()
		}
		a.updateQuiz()
		return
	}
	if a.screen == Snake && a.menu == nil {
		if a.snake == nil {
			a.snake = a.newSnake()
		}
		a.updateSnake()
		return
	}
	if a.screen == Shooter && a.menu == nil {
		if a.shooter == nil {
			a.shooter = a.newShooter()
		}
		a.updateShooter()
		return
	}
	if a.screen == Duel && a.menu == nil {
		if a.duel == nil {
			a.duel = a.newDuel()
		}
		a.updateDuel()
		return
	}
	if a.screen == Games && a.menu == nil {
		a.updateGames()
		return
	}
	if a.screen == Visual && a.menu == nil {
		a.updateVisual()
		return
	}
	in := &a.input
	if a.menu != nil {
		a.updateMenu()
		return
	}
	// Los que valen en todas las pantallas.
	switch {
	case in.Pressed(ActPause):
		a.send("toggle")
	case a.screen == LibraryScreen && a.libTab == tabSearch && !a.tabFocus && a.yt.bin != "" && in.Pressed(ActPrev):
		a.searchScope(-1) // en Buscar, L y R cambian el filtro
	case a.screen == LibraryScreen && a.libTab == tabSearch && !a.tabFocus && a.yt.bin != "" && in.Pressed(ActNext):
		a.searchScope(1)
	case a.screen == LibraryScreen && in.Repeat(ActPrev):
		a.jumpLetter(-1) // en la biblioteca, L y R saltan de letra en letra
	case a.screen == LibraryScreen && in.Repeat(ActNext):
		a.jumpLetter(1)
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	case in.Repeat(ActSeekBack):
		a.seekBy(-10)
	case in.Repeat(ActSeekFwd):
		a.seekBy(+10)
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	}
	switch a.screen {
	case NowPlaying:
		switch {
		case in.Pressed(ActConfirm):
			a.send("toggle")
		case in.Pressed(ActShuffle) && a.shuffleTwice():
			a.playRandomSong() // doble X: una canción al azar de toda la biblioteca
		case in.Pressed(ActShuffle):
			a.shuffleBefore = a.status.Shuffle
			a.send("shuffle", onOffArg(!a.status.Shuffle))
		case in.Pressed(ActMenu):
			a.openNowMenu()
		case in.Pressed(ActBack):
			a.screen = LibraryScreen
		case in.Pressed(ActUp):
			a.scratch(-1) // huevo de pascua: scratch con el vinilo (eggs.go)
		case in.Pressed(ActDown):
			a.scratch(1)
		}
		// Izquierda/derecha: un toque cambia de canción y mantenerlo busca.
		// Así se puede todo sin gatillos, que no todas las consolas tienen.
		a.tapOrHold(&a.leftHold, in.duration(ActLeft), "prev", -10)
		a.tapOrHold(&a.rightHold, in.duration(ActRight), "next", +10)
	case LibraryScreen:
		a.updateLibrary()
	case Queue:
		a.updateQueue()
	case Downloads:
		a.updateDownloads()
	case Radio:
		a.updateRadio()
	case Podcasts:
		a.updatePodcasts()
	case Settings:
		if in.Pressed(ActBack) && !a.settingsEdit && !a.settingsQR {
			a.goBack()
			return
		}
		a.updateSettings()
	}
}

// playSongs manda al servicio una cola y en qué canción empezar.
func (a *App) playSongs(songs []Song, start int, shuffle bool) {
	args := make([]string, 0, len(songs)+1)
	args = append(args, fmt.Sprint(start))
	for _, s := range songs {
		args = append(args, s.Path)
	}
	a.send("load", args...)
	a.send("shuffle", onOffArg(shuffle))
}

// El volumen va en una escala de 0 a 200: el del sistema hasta 100 y, por
// encima, el refuerzo de audiofilus (que amplifica, con un limitador de picos). El servicio
// decide qué sube o baja (volstep), igual que con los botones de la consola.
const (
	volumeStep = 5
	volumeMax  = 2.0
)

// changeVolume sube o baja delta puntos de la escala.
func (a *App) changeVolume(delta int) {
	a.send("volstep", fmt.Sprintf("%+d", delta))
	// Se ve enseguida, sin esperar al servicio (que lo confirma luego).
	a.volume = min(volumeMax, max(0, a.volume+float64(delta)/100))
	a.showVolume()
}

// syncStatus copia el estado del servicio y deriva qué álbum suena.
func (a *App) syncStatus() {
	if a.client == nil {
		return
	}
	st, err := a.client.Status()
	if err != nil && a.t-a.messageAt > 4 {
		a.flash(T("service_error", err))
	}
	// Cada cambio de volumen (desde aquí, los botones de la consola o la CLI)
	// muestra el indicador; al conectar por primera vez, no.
	if st.Platform != "" {
		if st.VolumeSeq != a.volumeSeq && a.status.Platform != "" {
			a.showVolume()
			// Los botones de volumen de la consola no pasan por la interfaz
			// (llegan al servicio con volstep), pero también son tocar algo:
			// reinician la cuenta para el sleep y cancelan el fundido.
			a.lastInput = a.t
			if a.fadingOut {
				a.fadingOut = false
				a.fadingIn, a.sleepFade = true, fadeInLen*(1-min(1, a.sleepFade/fadeOutLen))
			}
		}
		a.volumeSeq = st.VolumeSeq
		// El botón de encendido (lo lee el servicio): a dormir, con fundido.
		if st.PowerSeq != a.powerSeq && a.status.Platform != "" && !a.sleeping && !a.fadingOut {
			a.enterSleep()
		}
		a.powerSeq = st.PowerSeq
		a.volume = float64(st.VolumeTotal) / 100
	}
	prevIndex, wasPlaying := a.status.Index, a.isPlaying
	a.status = st
	a.isPlaying = st.State == "playing"
	if a.isPlaying != wasPlaying && a.lastTrack != "" {
		a.juiceToggle()
	}
	a.position, a.duration = st.Position, st.Duration
	if since := a.t - a.seekAt; since < 0.8 && math.Abs(st.Position-a.seekWant) > 1.5 {
		// El servicio todavía no ha contestado al salto: se da por hecho.
		a.position = a.seekWant
		if a.isPlaying {
			a.position += since
		}
	}
	if st.Track.Path == a.lastTrack {
		return
	}
	a.shownPos, a.seekAt = st.Position, -1 // canción nueva: la barra no viaja hacia atrás
	a.resetBeat()                          // y el ritmo empieza de cero
	if a.lastTrack != "" {
		dir := 1
		if st.Index < prevIndex {
			dir = -1
		}
		a.juiceTrack(dir)
	}
	a.lastTrack = st.Track.Path
	a.playing = nil
	a.playingSong = Song{}
	if al, i := a.albumOf(st.Track.Path); al != nil {
		a.playing, a.playingIdx, a.playingSong = al, max(i, 0), al.Songs[max(i, 0)]
		a.themeFor = al
	} else if st.Track.Path != "" {
		// Suena algo que no está en la biblioteca (por ejemplo, desde la CLI).
		a.playingSong = Song{Path: st.Track.Path, Title: st.Track.Title, Artist: st.Track.Artist, Album: st.Track.Album}
		// Con su portada al lado (lo de YouTube Music que se escucha sin
		// guardar), un álbum de paso: portada, vinilo y colores como siempre.
		cover := st.Track.Path + ".jpg"
		if radio.IsRadio(st.Track.Path) {
			// Una emisora: su logo, si lo tiene (se baja la primera vez).
			cover = a.radioLogo(strings.TrimPrefix(st.Track.Path, radio.Scheme))
			a.fetchRadioLogo(st.Track.Path)
		}
		if _, err := os.Stat(cover); err == nil {
			al := &Album{Title: st.Track.Album, Artist: st.Track.Artist, Songs: []Song{a.playingSong}, coverPath: cover, stray: true, scaled: map[int]*coverImage{}}
			a.playing, a.playingIdx, a.themeFor = al, 0, al
		}
	}
}

// simulatePlaying hace que "suene" un álbum, para la maqueta sin servicio.
func (a *App) simulatePlaying() {
	sel := -1
	for i, al := range a.lib.Albums {
		want := a.ShotsAlbum == "" || strings.Contains(strings.ToLower(al.Artist+" "+al.Title), strings.ToLower(a.ShotsAlbum))
		if want && al.Title != "Sin álbum" && len(al.Songs) >= 5 && al.HasCover() {
			sel = i
			break
		}
	}
	sel = max(sel, 0)
	al := a.lib.Albums[sel]
	a.playing, a.playingIdx, a.playingSong = al, 0, al.Songs[0]
	a.isPlaying, a.position, a.duration = true, 64, 231
	a.status.Shuffle, a.status.Repeat, a.status.Volume = true, "off", 1
	a.status.Output = "bluetooth"
	a.volume = 1
	a.themeFor = al
	root := a.rootView(tabAlbums)
	root.sel = sel
	a.stacks[tabAlbums] = []*view{root}
}

// readBattery lee la batería y si está cargando cada cinco segundos (en el
// PC, la del portátil): así el rayo de carga sale poco después de enchufarla.
func (a *App) readBattery() {
	if time.Since(a.batAt) < 5*time.Second {
		return
	}
	a.batAt = time.Now()
	a.battery = -1
	if level, charging, ok := systemBattery(); ok {
		a.battery, a.charging = level, charging
		return
	}
	caps, _ := filepath.Glob("/sys/class/power_supply/*/capacity")
	for _, c := range caps {
		if t, err := os.ReadFile(filepath.Join(filepath.Dir(c), "type")); err != nil || strings.TrimSpace(string(t)) != "Battery" {
			continue
		}
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var v int
		if _, err := fmt.Sscan(string(b), &v); err == nil {
			a.battery = float64(v) / 100
			st, _ := os.ReadFile(filepath.Join(filepath.Dir(c), "status"))
			switch strings.TrimSpace(string(st)) {
			case "Charging", "Full":
				a.charging = true
			default:
				a.charging = false
			}
			return
		}
	}
}

func firstOr(s []string, d string) string {
	if len(s) > 0 {
		return s[0]
	}
	return d
}

// Draw pinta la pantalla actual.
func (a *App) Draw(screen *ebiten.Image) {
	if a.BenchVisual != "" {
		return // la prueba pinta en su lienzo; pintar aquí también falsearía la medida
	}
	if a.splashing {
		a.drawSplash(screen)
		return
	}
	if !a.warm && a.mode == modeMain {
		a.warmUp()
	}
	a.watchScreen() // aquí, después de Update: así no asoma la nueva sin cortina
	a.drawScreen(screen, a.screen)
	a.drawTransition(screen)
	if a.mode == modeMain {
		a.drawCelebration(screen)
	}
	if a.cfg.UI.GameBoy {
		applyGameBoy(screen)
	}
}

// warmUp estrena los shaders y pide las portadas de todas las pantallas en el
// primer frame, para que entrar a una pantalla nueva no dé un tirón.
func (a *App) warmUp() {
	a.warm = true
	tmp := ebiten.NewImage(16, 16)
	defer tmp.Deallocate()
	src := ebiten.NewImage(8, 8)
	defer src.Deallocate()
	drawBackground(tmp, a.theme, 0)
	drawVinyl(tmp, 8, 8, 6, 0, a.theme.Accent)
	drawShine(tmp, src, 0, 0, 0.5)
	for s := Screen(0); s < screenCount; s++ {
		a.drawScreen(tmp, s)
	}
}

// updateTheme tiñe la interfaz con el disco que suena cuando ya se conocen sus
// colores (se leen con la portada, en segundo plano).
func (a *App) updateTheme() {
	if a.themeFor == nil {
		return
	}
	c1, c2, ok := a.themeFor.Accent()
	if !ok {
		a.cover(a.themeFor, 16) // pide la portada para leer sus colores
		return
	}
	a.theme = ThemeFor(c1, c2)
	a.themeFor = nil
}

func (a *App) drawScreen(dst *ebiten.Image, s Screen) {
	if a.sleeping {
		dst.Fill(color.Black) // apagada: negro (en una OLED, píxeles apagados)
		return
	}
	drawBackground(dst, a.theme, a.t)
	switch a.mode {
	case modeScan:
		a.drawScan(dst)
	case modePicker:
		a.drawPicker(dst, a.picker, a.firstRun)
	default:
		switch s {
		case Blocks:
			if a.tetris == nil {
				a.tetris = a.newTetris()
			}
			a.drawTetris(dst)
			a.drawTopBar(dst, T("tet_title"))
		case Rhythm:
			if a.rhythm == nil {
				a.rhythm = a.newRhythm()
			}
			a.drawRhythmGame(dst)
			a.drawTopBar(dst, T("rh_title"))
		case Crypt:
			if a.crypt == nil {
				a.crypt = a.newCrypt()
			}
			a.drawCrypt(dst)
			a.drawTopBar(dst, T("cr_title"))
		case Tracker:
			if a.tracker == nil {
				a.tracker = a.newTracker()
			}
			a.drawTracker(dst)
			a.drawTopBar(dst, T("tk_title"))
		case Quiz:
			if a.quiz == nil {
				a.quiz = a.newQuiz()
			}
			a.drawQuiz(dst)
			a.drawTopBar(dst, T("qz_title"))
		case Snake:
			if a.snake == nil {
				a.snake = a.newSnake()
			}
			a.drawSnake(dst)
			a.drawTopBar(dst, T("sn_title"))
		case Shooter:
			if a.shooter == nil {
				a.shooter = a.newShooter()
			}
			a.drawShooter(dst)
			a.drawTopBar(dst, T("sh_title"))
		case Duel:
			if a.duel == nil {
				a.duel = a.newDuel()
			}
			a.drawDuel(dst)
			a.drawTopBar(dst, T("du_title"))
		case Games:
			a.drawGames(dst)
			a.drawTopBar(dst, T("gm_title"))
		case Visual:
			a.drawVisual(dst)
			a.drawTopBar(dst, T("vis_title")+" · "+T(visNames[a.vis.kind]))
		case NowPlaying:
			a.drawNowPlaying(dst)
			a.drawTopBar(dst, T("now"))
		case LibraryScreen:
			a.drawLibrary(dst)
			a.drawLetter(dst)
		case Queue:
			a.drawQueue(dst)
		case Downloads:
			a.drawDownloads(dst)
		case Radio:
			a.drawRadio(dst)
		case Podcasts:
			a.drawPodcasts(dst)
		case Settings:
			a.drawSettings(dst)
		}
		a.drawYTBar(dst)
		a.drawMenu(dst)
		a.drawPrompt(dst)
		a.drawDock(dst)
		a.drawGuide(dst)
		a.drawVolume(dst)
	}
	a.drawMessage(dst)
	switch {
	case a.fadingOut:
		drawDissolve(dst, a.sleepFade/fadeOutLen)
	case a.fadingIn:
		drawDissolve(dst, 1-a.sleepFade/fadeInLen)
	}
}

// drawScan es la pantalla mientras se busca la música.
func (a *App) drawScan(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	title := "audiofilus"
	drawTextShadow(dst, title, BigFace, (w-textWidth(title, BigFace))/2, h/2-30, th.Text, th.Shadow)
	msg := T("scanning")
	drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2-8, th.Accent)
	done, total := a.scan.Done.Load(), a.scan.Total.Load()
	bw := min(160, w-2*margin)
	bx := (w - bw) / 2
	rect(dst, bx, h/2+6, bw, 3, th.Bg2)
	if total > 0 {
		rect(dst, bx, h/2+6, int(float64(bw)*float64(done)/float64(total)), 3, th.Accent)
		c := T("scan_count", done, total)
		drawText(dst, c, SmallFace, (w-textWidth(c, SmallFace))/2, h/2+16, th.Dim)
	} else {
		// Aún contando archivos: un bloque que va y viene.
		x := int(a.t*60) % (bw * 2)
		if x > bw {
			x = 2*bw - x
		}
		rect(dst, bx+max(0, x-20), h/2+6, min(20, bw-max(0, x-20)), 3, th.Accent)
	}
	a.drawTopBar(dst, "")
}

// drawMessage pinta el aviso temporal, si lo hay.
func (a *App) drawMessage(dst *ebiten.Image) {
	if a.message == "" || a.t-a.messageAt > 4 {
		return
	}
	w := dst.Bounds().Dx()
	th := a.theme
	msg := fit(a.message, SmallFace, w-2*margin-8)
	y := topH + 4
	panel(dst, margin-4, y, w-2*margin+8, 13, th.Accent2)
	drawText(dst, msg, SmallFace, margin, y+4, th.Bg)
}

// Layout da el lienzo lógico: 240 de alto y el ancho que pida la ventana.
func (a *App) Layout(w, h int) (int, int) {
	a.w = ViewW(w, h)
	return a.w, ScreenH
}

// DrawFinalScreen pone la pantalla lógica en la ventana con FilterPixelated:
// a cualquier escala, también no entera (pantalla completa en el PC), los
// píxeles se ven duros y del mismo tamaño, sin el suavizado del filtro por
// defecto y sin bandas negras de una escala entera (como Trancazo).
func (a *App) DrawFinalScreen(screen ebiten.FinalScreen, offscreen *ebiten.Image, geoM ebiten.GeoM) {
	op := &ebiten.DrawImageOptions{}
	op.GeoM = geoM
	op.Filter = ebiten.FilterPixelated
	screen.DrawImage(offscreen, op)
}

// Err es el error de las capturas, si hubo.
func (a *App) Err() error { return a.shotsErr }

// updateShots espera a la biblioteca, las portadas y el tema, y guarda las capturas.
func (a *App) updateShots() error {
	a.frames++
	if a.mode == modeScan {
		if lib, err, ok := a.scan.Result(); ok {
			if err != nil {
				a.shotsErr = err
				return ebiten.Termination
			}
			a.local, a.lib, a.mode = lib, lib, modeMain
			a.simulatePlaying()
		}
		return nil
	}
	if !a.warmed {
		// Calentamiento, en un fotograma aparte: el primer uso de un shader en
		// el proceso a veces no llega a la imagen (se vio con el del vinilo); se
		// dibuja todo una vez y se tira.
		warm := ebiten.NewImage(320, ScreenH)
		a.drawScreen(warm, NowPlaying)
		drawPortCover(warm)
		warm.Deallocate()
		a.warmed = true
		return nil
	}
	if a.frames > 2 && len(a.covers.pending) == 0 && a.themeFor == nil {
		a.shotsErr = a.writeShots()
		return ebiten.Termination
	}
	if a.frames > 1200 {
		a.shotsErr = fmt.Errorf("covers did not load in time")
		return ebiten.Termination
	}
	a.requestShotCovers()
	return nil
}

// scrollTest avanza un álbum cada dos frames por toda la cuadrícula de la
// biblioteca y va anotando la memoria del proceso; al final la escribe y termina.
func (a *App) scrollTest() error {
	a.screen, a.libTab, a.tabFocus = LibraryScreen, tabAlbums, false
	v := a.cur()
	a.frames++
	if a.frames == 1 {
		a.startRSS = rssKB()
	}
	if a.frames%2 == 0 {
		v.sel = min(v.sel+1, len(v.albums)-1)
	}
	if rss := rssKB(); rss > a.peakRSS {
		a.peakRSS = rss
	}
	var ms runtime.MemStats
	if a.frames%30 == 0 {
		runtime.ReadMemStats(&ms)
		a.peakHeap = max(a.peakHeap, ms.HeapInuse)
	}
	if v.sel >= len(v.albums)-1 && len(a.covers.pending) == 0 {
		runtime.ReadMemStats(&ms)
		fmt.Printf("álbumes: %d  RSS: al arrancar %d MB, pico %d MB, final %d MB  |  heap de Go: pico %d MB, final %d MB\n",
			len(v.albums), a.startRSS/1024, a.peakRSS/1024, rssKB()/1024, a.peakHeap>>20, ms.HeapInuse>>20)
		return ebiten.Termination
	}
	return nil
}

// rssKB es la memoria residente del proceso, de /proc/self/status.
func rssKB() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int64
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "VmRSS:")), "%d", &kb)
			return kb
		}
	}
	return 0
}

// requestShotCovers dibuja (sin mostrar) cada pantalla en cada formato, para
// que se pidan todas las portadas que saldrán en las capturas.
func (a *App) requestShotCovers() {
	for _, asp := range Aspects {
		a.w = asp.W
		tmp := ebiten.NewImage(asp.W, ScreenH)
		for s := Screen(0); s < screenCount; s++ {
			a.drawScreen(tmp, s)
		}
		for tab := 0; tab < tabCount; tab++ {
			a.libTab = tab
			a.drawScreen(tmp, LibraryScreen)
		}
		tmp.Deallocate()
	}
	a.libTab = tabSongs
}

// writeShots guarda cada pantalla en cada formato, ampliada ×3, más el
// explorador de carpetas de la primera vez.
func (a *App) writeShots() error {
	if err := os.MkdirAll(a.ShotsDir, 0o755); err != nil {
		return err
	}
	a.t = 3.2 // un instante fijo, para que las capturas no cambien entre ejecuciones
	a.now = time.Date(2026, 10, 17, 21, 20, 0, 0, time.Local)
	a.battery = 0.72
	a.charging = true
	shot := func(name string) error {
		for _, asp := range Aspects {
			a.w = asp.W
			if err := a.renderShot(filepath.Join(a.ShotsDir, fmt.Sprintf("%s-%s.png", name, asp.Name)), asp.W); err != nil {
				return err
			}
		}
		return nil
	}
	a.screen = NowPlaying
	if err := shot("reproduciendo"); err != nil {
		return err
	}
	a.screen = LibraryScreen
	for tab, name := range []string{tabSongs: "canciones", tabAlbums: "albumes", tabArtists: "artistas", tabLists: "listas", tabFolders: "carpetas"} {
		if name == "" {
			continue
		}
		a.libTab = tab
		if err := shot(name); err != nil {
			return err
		}
	}
	// Buscar: con el teclado en pantalla y escribiendo, y en los resultados.
	a.libTab, a.tabFocus = tabSearch, false
	a.search = searchState{query: []rune("CHE"), typing: true, row: 1, col: 4}
	a.updateSearchResults()
	if err := shot("buscar"); err != nil {
		return err
	}
	a.search.typing, a.search.sel = false, 2
	if err := shot("buscar-resultados"); err != nil {
		return err
	}
	a.search = searchState{}
	// El salto por letras (R en los álbumes), con su letra en el centro.
	a.libTab = tabAlbums
	sel := a.cur().sel
	a.jumpLetter(1)
	if err := shot("letra"); err != nil {
		return err
	}
	a.cur().sel, a.letter = sel, ""
	a.openAlbum(a.playing)
	if err := shot("album"); err != nil {
		return err
	}
	a.screen = Queue
	if err := shot("cola"); err != nil {
		return err
	}
	a.screen = Settings
	if err := shot("ajustes"); err != nil {
		return err
	}
	a.settingsQR, a.qrURL = true, projectURL
	if err := shot("ajustes-qr"); err != nil {
		return err
	}
	a.settingsQR = false
	a.screen = NowPlaying
	a.openNowMenu()
	if err := shot("menu"); err != nil {
		return err
	}
	a.menu = nil
	// Buscar con resultados de YouTube Music (inventados: la maqueta no usa
	// la red ni yt-dlp).
	a.screen, a.libTab, a.tabFocus = LibraryScreen, tabSearch, false
	a.yt.bin, a.yt.query, a.yt.ids = "mock", "Chevelle", map[string]bool{}
	a.yt.tracks = []ytm.Track{
		{ID: "a", Title: "The Red", Artists: []string{"Chevelle"}, Album: "Wonder What's Next", Duration: "3:59"},
		{ID: "b", Title: "Send the Pain Below", Artists: []string{"Chevelle"}, Album: "Wonder What's Next", Duration: "4:13"},
		{ID: "c", Title: "Rabbit Hole (Cowards, Pt. 1)", Artists: []string{"Chevelle"}, Album: "Bright as Blasphemy", Duration: "4:10"},
		{ID: "d", Title: "Hats off to the Bull", Artists: []string{"Chevelle"}, Album: "Hats off to the Bull", Duration: "3:42"},
	}
	a.search.query, a.search.typing, a.search.lastQuery, a.search.sel = []rune("Chevelle"), false, "", 2
	a.updateSearchResults()
	tr := a.yt.tracks
	a.yt.jobs = []*ytJob{
		{track: tr[3], state: jobDone, path: "x"},
		{track: tr[2], state: jobError, err: errors.New("yt-dlp: ERROR: HTTP Error 403")},
		{track: tr[1], state: jobDownload, done: 45, total: 100},
		{track: tr[3], state: jobWaiting},
		{track: tr[0], listen: true, state: jobResolve},
	}
	if err := shot("buscar-youtube"); err != nil {
		return err
	}
	a.screen, a.yt.sel = Downloads, 1
	if err := shot("descargas"); err != nil {
		return err
	}
	// Reproduciendo algo de YouTube Music sin guardar (la portada es la del
	// disco que suena en la maqueta).
	keepSong, keepAl := a.playingSong, a.playing
	a.playingSong = Song{Path: filepath.Join(ytCacheDir(), "z1ndFL1v34E.m4a"), Title: "The Red", Artist: "Chevelle", Album: "Wonder What's Next"}
	a.screen = NowPlaying
	if err := shot("reproduciendo-streaming"); err != nil {
		return err
	}
	// Una radio: sin duración, la barra es "en vivo".
	keepDur, keepPos := a.duration, a.position
	a.playingSong = Song{Path: "radio://prueba", Title: "Adolescent's Orquesta - Exitos 2", Artist: "Caracas. Salsa Romántica", Album: "Radio"}
	a.playing, a.duration, a.position = nil, 0, 127
	if err := shot("reproduciendo-radio"); err != nil {
		return err
	}
	a.duration, a.position = keepDur, keepPos
	a.playingSong, a.playing = keepSong, keepAl
	a.yt = ytState{}
	a.search = searchState{}
	a.screen = NowPlaying
	// Con AUDIOFILUS_SHOT_SERVER=nombre,url,usuario,contraseña[,tipo], también la
	// biblioteca de ese servidor (para probar Subsonic en el PC).
	if env := os.Getenv("AUDIOFILUS_SHOT_SERVER"); env != "" {
		if parts := strings.SplitN(env, ",", 5); len(parts) >= 4 {
			srv := config.Server{Name: parts[0], URL: parts[1], User: parts[2], Password: parts[3]}
			if len(parts) == 5 {
				srv.Type = parts[4] // "jellyfin"
			}
			a.cfg.Servers = []config.Server{srv}
			a.initServers()
			for i := 0; i < 300 && a.remotes[0].loading; i++ {
				time.Sleep(50 * time.Millisecond)
				a.updateServers()
			}
			a.screen, a.libTab, a.tabFocus = LibraryScreen, tabAlbums, false
			a.setSource(1)
			for i := 0; i < 60; i++ { // las portadas, del servidor (ya sin fundido)
				a.covers.collect(a.t - 10)
				time.Sleep(50 * time.Millisecond)
				_ = a.renderShot(filepath.Join(os.TempDir(), "audiofilus-warm.png"), 320)
			}
			if err := shot("servidor"); err != nil {
				return err
			}
			a.openDock()
			a.dock.at, a.dock.sel = a.t-1, 2
			if err := shot("servidor-pantallas"); err != nil {
				return err
			}
			a.dock.open = false
			a.screen = Settings
			a.openServersMenu()
			if err := shot("servidor-ajustes"); err != nil {
				return err
			}
			a.menu = nil
			a.openRawPrompt(T("srv_url"), "http://navidrome.local:4533", false, func(string) {})
			a.prompt.row, a.prompt.col = 4, 4
			if err := shot("servidor-teclado"); err != nil {
				return err
			}
			a.prompt = nil
			a.setSource(0)
		}
	}
	// Con AUDIOFILUS_NET_TESTS, la radio con emisoras de verdad.
	if os.Getenv("AUDIOFILUS_NET_TESTS") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		top, err := radio.Top(ctx, "VE")
		cancel()
		if err == nil {
			a.radio.loaded = true
			a.radio.lists[rtTop], a.radio.tab, a.radio.sel[rtTop] = top, rtTop, 1
			a.screen = Radio
			if err := shot("radio"); err != nil {
				return err
			}
			a.openCountryMenu()
			if err := shot("radio-pais"); err != nil {
				return err
			}
			a.menu = nil
		}
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		ptop, err := podcast.Top(ctx, "VE")
		cancel()
		if err == nil && len(ptop) > 0 {
			a.pod.loaded = true
			a.pod.lists[ptTop], a.pod.tab = ptop, ptTop
			a.screen = Podcasts
			if err := shot("podcasts"); err != nil {
				return err
			}
			ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			show, eps, ferr := podcast.Fetch(ctx, ptop[0].Feed)
			cancel()
			if ferr == nil {
				a.pod.open = &subscription{Show: show, Episodes: eps}
				if len(eps) > 1 {
					a.pod.progress[a.epPath(show, eps[1])] = epProgress{Pos: 754, Dur: 3000}
				}
				if err := shot("podcast-episodios"); err != nil {
					return err
				}
				a.pod.open = nil
			}
		}
	}
	a.screen = NowPlaying
	a.openAddToPlaylist([]Song{a.playingSong})
	if err := shot("lista-anadir"); err != nil {
		return err
	}
	a.prompt = &promptState{title: T("pl_name"), text: []rune("MI LISTA DEL GYM"), row: 1, col: 3, done: func(string) {}}
	if err := shot("lista-nombre"); err != nil {
		return err
	}
	a.prompt, a.menu = nil, nil
	a.openDock()
	a.dock.at = a.t - 1
	if err := shot("pantallas"); err != nil {
		return err
	}
	a.dock.open = false
	a.guideAt = a.t - 1
	if err := shot("guia"); err != nil {
		return err
	}
	a.guideAt = 0
	a.volume, a.volumeAt, a.volumeIn = 1.35, a.t-0.5, a.t-0.5
	if err := shot("volumen"); err != nil {
		return err
	}
	a.volumeAt = 0
	if a.FramesDir != "" {
		if err := a.writeFrames(); err != nil {
			return err
		}
	}
	a.mode, a.firstRun = modePicker, true
	a.picker = newPicker(filepath.Dir(firstOr(a.cfg.MusicDirs, "/")))
	for _, asp := range Aspects {
		if err := a.renderShot(filepath.Join(a.ShotsDir, fmt.Sprintf("carpeta-%s.png", asp.Name)), asp.W); err != nil {
			return err
		}
	}
	a.mode = modeMain
	// Bloques, a media partida.
	a.screen, a.tetris = Blocks, a.newTetris()
	for y := 14; y < tetRows; y++ {
		for x := 0; x < tetW; x++ {
			if (x*7+y*3)%11 != 0 {
				a.tetris.board[y][x] = int8(1 + (x+y)%7)
			}
		}
	}
	a.tetris.py, a.tetris.score, a.tetris.lines, a.tetris.hold = 6, 4820, 23, 5
	if err := shot("bloques"); err != nil {
		return err
	}
	a.openTetrisPause()
	a.menu.openedAt = 0
	if err := shot("bloques-pausa"); err != nil {
		return err
	}
	a.menu = nil
	a.tetris = nil
	// Ritmo, a media canción (con el ritmo de la maqueta, a 120 BPM).
	a.screen, a.rhythm = Rhythm, a.newRhythm()
	a.beat = beat{}
	for i := 0; i < 3; i++ {
		a.updateRhythmFrame()
	}
	a.rhythm.score, a.rhythm.combo, a.rhythm.max = 18450, 37, 52
	a.rhythm.judged = [4]int{61, 14, 3, 2}
	a.rhythm.judge, a.rhythm.judgeAt = "PERFECT", a.t-0.1
	if err := shot("ritmo"); err != nil {
		return err
	}
	a.rhythm = nil
	// ¿Qué canción es?, a media ronda; Serpiente y Naves jugadas por sus bots.
	a.screen, a.quiz = Quiz, a.newQuiz()
	a.quizRound()
	if a.quiz != nil && len(a.quiz.pool) >= 4 {
		a.quiz.round, a.quiz.score, a.quiz.streak, a.quiz.sel = 3, 430, 3, 1
		a.quiz.startAt = a.t - 3.5
		if err := shot("adivina"); err != nil {
			return err
		}
	}
	a.quiz = nil
	a.screen, a.snake = Snake, nil
	a.w = 360
	a.snake = a.newSnake()
	for i := 0; i < 60*14; i++ {
		a.t += 1.0 / 60
		a.botSnake()
		a.updateSnake()
	}
	if err := shot("serpiente"); err != nil {
		return err
	}
	a.snake = nil
	a.screen, a.shooter = Shooter, a.newShooter()
	for i := 0; i < 60*10; i++ {
		a.t += 1.0 / 60
		a.beatHit = a.updateBeat()
		a.botShooter()
		a.updateShooter()
	}
	if err := shot("naves"); err != nil {
		return err
	}
	a.shooter = nil
	// Duelo: un rato de batalla con su bot, en su turno de esquivar.
	a.screen, a.duel = Duel, a.newDuel()
	for i := 0; i < 60*12; i++ {
		a.t += 1.0 / 60
		a.beatHit = a.updateBeat()
		a.botDuel()
		a.updateDuel()
	}
	if err := shot("duelo"); err != nil {
		return err
	}
	a.duel.level = 2 // el metrónomo, con su péndulo
	a.duelFoe(a.duel)
	a.duel.phase, a.duel.cursor = duAttack, 0.42
	if err := shot("duelo-metronomo"); err != nil {
		return err
	}
	a.duel = nil
	// Los huevos de pascua: el modo Game Boy y el 26 de octubre.
	a.screen = NowPlaying
	a.cfg.UI.GameBoy = true
	if err := shot("gameboy"); err != nil {
		return err
	}
	a.cfg.UI.GameBoy = false
	a.shotDay, a.celebrateAt = dayWifeBirthday, a.t-2
	if err := shot("cumple"); err != nil {
		return err
	}
	a.shotDay = dayNone
	// Juegos y una partida de Cripta jugada por el bot unos segundos.
	a.screen, a.gamesSel = Games, 6 // el tracker
	if err := shot("juegos"); err != nil {
		return err
	}
	a.screen, a.crypt = Crypt, a.newCrypt()
	for i := 0; i < 60*9; i++ {
		a.t += 1.0 / 60
		a.beatHit = a.updateBeat()
		a.botStepCrypt()
	}
	if err := shot("cripta"); err != nil {
		return err
	}
	a.crypt = nil
	// El tracker: la canción de ejemplo sonando (adelantada al paso 6) y el
	// cursor en el volumen del pulso 2; después, la vista de canción.
	a.screen, a.tracker = Tracker, a.newTracker()
	a.tracker.song = tracker.NewSong()
	a.tracker.player.SetSong(a.tracker.song)
	a.tracker.player.PlayPattern(0)
	_, _ = a.tracker.player.Read(make([]byte, 8*sfx.Rate*60/a.tracker.song.BPM/4*6+8))
	a.tracker.row, a.tracker.col = 4, 4
	if err := shot("tracker"); err != nil {
		return err
	}
	a.tracker.songView, a.tracker.ord = true, 0
	if err := shot("tracker-cancion"); err != nil {
		return err
	}
	a.tracker = nil
	if err := a.writePortCover(filepath.Join(a.ShotsDir, "portada-portmaster.png")); err != nil {
		return err
	}
	if err := a.writeLogo(filepath.Join(a.ShotsDir, "logo.png")); err != nil {
		return err
	}
	if err := a.writeIcon(filepath.Join(a.ShotsDir, "icono.png")); err != nil {
		return err
	}
	return nil
}

// writeFrames guarda 8 s de la pantalla de reproducción en 4:3 a 20 frames por
// segundo: lo que tarda el título en ir y volver.
func (a *App) writeFrames() error {
	if err := os.MkdirAll(a.FramesDir, 0o755); err != nil {
		return err
	}
	if err := a.writeLogoFrames(filepath.Join(a.FramesDir, "logo")); err != nil {
		return err
	}
	// El splash para la web, a 30 cuadros por segundo y hasta antes de su
	// salida (la web hace su propio fundido), en dos capas: el fondo, que la
	// página estira hasta cubrir cualquier pantalla, y lo de encima, sobre
	// transparente, que centra entero.
	for _, v := range []struct {
		dir    string
		bg, fg bool
	}{{"splash-bg", true, false}, {"splash-fg", false, true}} {
		dir := filepath.Join(a.FramesDir, v.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for i := 0; float64(i)/30 < splashLen-splashOut; i++ {
			a.splashT = float64(i) / 30
			err := a.render(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), 427, func(dst *ebiten.Image) {
				a.drawSplashLayers(dst, v.bg, v.fg)
			})
			if err != nil {
				return err
			}
		}
	}
	// El splash, entero, en su propia carpeta.
	splashDir := filepath.Join(a.FramesDir, "splash")
	if err := os.MkdirAll(splashDir, 0o755); err != nil {
		return err
	}
	for i := 0; float64(i)/20 < splashLen; i++ {
		a.splashT = float64(i) / 20
		if err := a.render(filepath.Join(splashDir, fmt.Sprintf("f%04d.png", i)), 320, a.drawSplash); err != nil {
			return err
		}
	}
	// La pantalla apagándose y volviendo.
	sleepDir := filepath.Join(a.FramesDir, "sleep")
	if err := os.MkdirAll(sleepDir, 0o755); err != nil {
		return err
	}
	a.screen, a.menu, a.volumeAt = NowPlaying, nil, 0
	// A 20 cuadros por segundo: 10 disolviéndose, 4 en negro y 5 volviendo.
	for i := 0; i < 24; i++ {
		a.t = 4 + float64(i)/20
		a.fadingOut, a.fadingIn = i < 10, i >= 14
		a.sleepFade = float64(i) / 20
		if i >= 14 {
			a.sleepFade = float64(i-14) / 20
		}
		if i >= 10 && i < 14 {
			a.fadingOut, a.sleeping = false, true
		}
		if err := a.renderShot(filepath.Join(sleepDir, fmt.Sprintf("f%04d.png", i)), 320); err != nil {
			return err
		}
		a.sleeping = false
	}
	a.fadingOut, a.fadingIn = false, false
	// Bloques: una partida de verdad, con el jugador automático (tetbot.go)
	// soltando las piezas al ritmo de la maqueta. Empieza con una pila de
	// cinco filas con un pozo a la derecha, para que se vean líneas, combos y
	// un TETRA.
	// Los colores de cuatro discos distintos (colorshow.go).
	keepAl, keepSong, keepTheme, keepDur := a.playing, a.playingSong, a.theme, a.duration
	if err := a.writeColorShow(filepath.Join(a.FramesDir, "colores")); err != nil {
		return err
	}
	a.playing, a.playingSong, a.theme, a.duration = keepAl, keepSong, keepTheme, keepDur
	// El tracker tocando Pocket Pop 12 s desde la estrofa, siguiendo la
	// canción (el sintetizador avanza lo que dura cada cuadro).
	tkDir := filepath.Join(a.FramesDir, "tracker")
	if err := os.MkdirAll(tkDir, 0o755); err != nil {
		return err
	}
	a.screen, a.tracker = Tracker, a.newTracker()
	ex := tracker.Examples()[0]
	a.tracker.song, a.tracker.file = ex, "example.json"
	a.tracker.player.SetSong(ex)
	a.tracker.player.PlaySong(1)
	a.tracker.playing, a.tracker.playSong = true, true
	a.tracker.row, a.tracker.col = 0, 3
	chunk := make([]byte, 8*sfx.Rate/20)
	for i := 0; i < 240; i++ {
		a.t = 30 + float64(i)/20
		_, _ = a.tracker.player.Read(chunk)
		a.trackerFollow()
		if _, step, _, _ := a.tracker.player.Position(); i%4 == 0 {
			a.tracker.row = step // el cursor acompaña un poco
		}
		if err := a.renderShot(filepath.Join(tkDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
			return err
		}
	}
	a.tracker = nil
	// Los días especiales (eggs.go): la celebración entera sobre el
	// reproductor, fingiendo la fecha.
	for _, day := range []struct {
		dir  string
		kind int
	}{{"dia-anio-nuevo", dayNewYear}, {"dia-audiofilus", dayAppBirthday}, {"dia-26-octubre", dayWifeBirthday}} {
		dir := filepath.Join(a.FramesDir, day.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		a.screen, a.w = NowPlaying, 360
		start := a.t + 1
		a.shotDay, a.celebrateAt = day.kind, start
		for i := 0; float64(i)/20 < celebrateFor; i++ {
			a.t = start + float64(i)/20
			a.position = 64 + float64(i)/20
			if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
				return err
			}
		}
		a.shotDay = dayNone
	}
	// Serpiente y Naves: sus bots, 12 s cada uno.
	for _, gm := range []struct {
		dir  string
		init func()
		step func()
	}{
		{"serpiente", func() { a.screen, a.snake = Snake, a.newSnake() }, func() { a.botSnake(); a.updateSnake() }},
		{"naves", func() { a.screen, a.shooter = Shooter, a.newShooter() }, func() { a.beatHit = a.updateBeat(); a.botShooter(); a.updateShooter() }},
		{"duelo", func() { a.screen, a.duel = Duel, a.newDuel() }, func() { a.beatHit = a.updateBeat(); a.botDuel(); a.updateDuel() }},
	} {
		dir := filepath.Join(a.FramesDir, gm.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		a.w = 360
		gm.init()
		for i := 0; i < 240; i++ {
			for k := 0; k < 3; k++ {
				a.t += 1.0 / 60
				gm.step()
			}
			if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
				return err
			}
		}
	}
	a.snake, a.shooter, a.duel = nil, nil, nil
	// Cripta: el bot jugando 12 s al ritmo de la maqueta.
	crDir := filepath.Join(a.FramesDir, "cripta")
	if err := os.MkdirAll(crDir, 0o755); err != nil {
		return err
	}
	a.w = 360
	a.screen, a.crypt = Crypt, a.newCrypt()
	for i := 0; i < 240; i++ {
		for k := 0; k < 3; k++ {
			a.t = 20 + float64(i*3+k)/60
			a.beatHit = a.updateBeat()
			a.botStepCrypt()
		}
		if err := a.renderShot(filepath.Join(crDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
			return err
		}
	}
	a.crypt = nil
	tetDir := filepath.Join(a.FramesDir, "bloques")
	if err := os.MkdirAll(tetDir, 0o755); err != nil {
		return err
	}
	a.screen, a.tetris = Blocks, a.newTetris()
	g := a.tetris
	g.queue = []int{2, 5, 3, 1, 6, 4, 0, 5, 2, 3, 1, 6, 0, 4, 5, 3, 2, 6, 1, 0, 4, 5, 3, 2, 1, 6, 0, 4, 5, 2}
	g.bag = nil
	g.spawn(g.take())
	for y := tetRows - 5; y < tetRows; y++ {
		for x := 0; x < tetW-1; x++ {
			g.board[y][x] = int8(1 + (x*3+y)%7)
		}
	}
	for x := 0; x < 4; x++ {
		g.board[tetRows-6][x] = int8(1 + x%7)
	}
	var plan tetPlan
	var readyAt float64
	planned := false
	for i := 0; i < 240; i++ {
		for k := 0; k < 3; k++ { // a 20 cuadros por segundo, 3 pasos de 60 Hz
			a.t = 10 + float64(i*3+k)/60
			a.beatHit = a.updateBeat()
			if !planned && len(a.tetris.clearing) == 0 && !a.tetris.over {
				plan, planned = a.tetris.botPlan(), true
			}
			if a.botStep(i*3+k, &plan, &readyAt) {
				planned = false
			}
			a.updateTetris()
		}
		if err := a.renderShot(filepath.Join(tetDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
			return err
		}
	}
	a.tetris = nil
	// La transición de Reproduciendo a Biblioteca, a 50 cuadros por segundo.
	trDir := filepath.Join(a.FramesDir, "transicion")
	if err := os.MkdirAll(trDir, 0o755); err != nil {
		return err
	}
	a.screen, a.lastScreen, a.t = LibraryScreen, NowPlaying, 30
	a.watchScreen()
	for i := 0; i < 10; i++ {
		a.t = 30 + float64(i)/50
		err := a.render(filepath.Join(trDir, fmt.Sprintf("f%04d.png", i)), 320, func(dst *ebiten.Image) {
			a.drawScreen(dst, a.screen)
			a.drawTransition(dst)
		})
		if err != nil {
			return err
		}
	}
	a.transAt = 0
	// Los visualizadores, unos segundos de cada uno (con el ritmo de la
	// maqueta); el de Derrape, más largo y con las tres cámaras.
	for kind := 0; kind < visCount; kind++ {
		dir := filepath.Join(a.FramesDir, "visual", visNames[kind][4:])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		a.screen, a.vis.kind, a.vis.switchAt = Visual, kind, 0
		frames := 60
		if kind == visDrift {
			a.drift = newDrift()
			frames = 180
		}
		if kind == visTrench || kind == visLake {
			frames = 120
		}
		if kind == visLake {
			a.lake = newLake()
		}
		for i := 0; i < frames; i++ {
			a.t = 40 + float64(i)/20
			if kind == visDrift {
				a.drift.cam = i / 60
			}
			for k := 0; k < 3; k++ {
				a.beatHit = a.updateBeat()
				a.updatePump()
				switch kind {
				case visDrift:
					a.stepDrift(1.0 / 60)
				case visTrench:
					if a.trench == nil {
						a.trench = newTrench()
					}
					a.stepTrench(1.0 / 60)
				case visLake:
					if a.lake == nil {
						a.lake = newLake()
					}
					a.stepLake(1.0 / 60)
				}
			}
			if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), 320); err != nil {
				return err
			}
		}
	}
	// Ritmo jugando solo: cada nota se toca al llegar a la línea.
	rhDir := filepath.Join(a.FramesDir, "ritmo")
	if err := os.MkdirAll(rhDir, 0o755); err != nil {
		return err
	}
	a.screen, a.rhythm, a.beat = Rhythm, a.newRhythm(), beat{}
	for i := 0; i < 80; i++ {
		a.t = 20 + float64(i)/20
		for k := 0; k < 3; k++ {
			a.updateRhythmFrame()
			a.autoPlay()
		}
		if err := a.renderShot(filepath.Join(rhDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
			return err
		}
	}
	a.rhythm = nil
	a.w = 360
	a.screen = NowPlaying
	// El reproductor, en 3:2 (el de la consola): 8 s sonando, con el reloj
	// siempre hacia delante (las secuencias de antes lo dejaron más allá: si
	// volviera atrás, la portada saldría a medio fundir) y solo un salto como
	// muestra del juice (un "cambio de canción" sin canción nueva destellaba
	// la portada en blanco).
	base := a.t + 1
	for i := 0; i < 160; i++ {
		a.t = base + float64(i)/20
		a.position = 64 + float64(i)/20
		for k := 0; k < 3; k++ { // el fondo sigue el ritmo de la maqueta
			a.beatHit = a.updateBeat()
			a.updatePump()
		}
		if i == 100 {
			a.juiceSeek(1)
		}
		if err := a.renderShot(filepath.Join(a.FramesDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
			return err
		}
	}
	return nil
}

// renderShot dibuja la pantalla actual a w×240 y la guarda ampliada ×3.
func (a *App) renderShot(path string, w int) error {
	return a.render(path, w, func(dst *ebiten.Image) {
		a.drawScreen(dst, a.screen)
		a.drawCelebration(dst)
		if a.cfg.UI.GameBoy {
			applyGameBoy(dst)
		}
	})
}

// render dibuja con draw a w×240 y lo guarda ampliado ×3.
func (a *App) render(path string, w int, draw func(*ebiten.Image)) error {
	a.shownPos = a.position // en las capturas no hay transición
	canvas := ebiten.NewImage(w, ScreenH)
	defer canvas.Deallocate()
	draw(canvas)
	pix := make([]byte, 4*w*ScreenH)
	canvas.ReadPixels(pix)
	const k = 3
	out := image.NewRGBA(image.Rect(0, 0, w*k, ScreenH*k))
	for y := 0; y < ScreenH*k; y++ {
		for x := 0; x < w*k; x++ {
			si := ((y/k)*w + x/k) * 4
			copy(out.Pix[y*out.Stride+x*4:], pix[si:si+4])
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

// seekBy adelanta o retrasa d segundos.
func (a *App) seekBy(d float64) {
	if a.duration <= 0 {
		return // en vivo (una radio): no se puede adelantar ni retroceder
	}
	a.send("seek", fmt.Sprintf("%+d", int(d)))
	a.seekWant = max(0, min(a.position+d, a.duration))
	a.seekAt = a.t
	a.position = a.seekWant
	if d > 0 {
		a.juiceSeek(1)
	} else {
		a.juiceSeek(-1)
	}
}

// tweenPosition acerca la posición que se pinta a la real: al sonar avanzan
// juntas; en un salto, la barra recorre la distancia en unos 0,25 s con
// frenada (cada fotograma, un 22 % de lo que falta).
func (a *App) tweenPosition() {
	d := a.position - a.shownPos
	if math.Abs(d) < 0.3 {
		a.shownPos = a.position
		return
	}
	a.shownPos += d * 0.22
	// El vinilo acompaña al salto: gira hacia ese lado lo que giraría
	// sonando esos segundos (1,4 radianes por segundo), como si se arrastrara.
	a.spin += d * 0.22 * vinylSpin
}

// vinylSpin es lo que gira el vinilo por segundo de canción (radianes).
const vinylSpin = 1.4

// lidClosed dice si se acaba de cerrar la tapa. En las Anbernic con tapa
// (RG34XX-SP, RG35XXSP) la tapa manda Insert al cerrarse y Delete al abrirse
// por el mismo dispositivo del mando (Knulli lo atiende con triggerhappy:
// lid-control). En el PC, Insert es solo una tecla.
func (a *App) lidClosed() bool {
	if a.status.Platform == "" || a.status.Platform == "desktop" {
		return false
	}
	// Knulli manda Insert al cerrarla (por el mando); en muOS, y también en
	// Knulli, el servicio lee el sensor de la tapa.
	for _, k := range a.input.keys {
		if k == ebiten.KeyInsert {
			return true
		}
	}
	return a.status.Lid
}

// shuffleTap es la separación máxima entre las dos pulsaciones de X del doble
// toque.
const shuffleTap = 0.4

// shuffleTwice registra una pulsación de X y dice si es la segunda de un doble
// toque.
func (a *App) shuffleTwice() bool {
	twice := a.shuffleAt > 0 && a.t-a.shuffleAt <= shuffleTap
	a.shuffleAt = a.t
	if twice {
		a.shuffleAt = 0 // un triple toque no cuenta como dos dobles
	}
	return twice
}

// playRandomSong pone una canción al azar de toda la biblioteca, con el resto
// detrás, y deshace lo que hizo la primera pulsación de X: el aleatorio queda
// como estaba.
func (a *App) playRandomSong() {
	a.sound(sfx.Random, 4)
	if a.lib == nil || len(a.lib.Songs) == 0 {
		return
	}
	i := rand.IntN(len(a.lib.Songs))
	a.playSongs(a.lib.Songs, i, a.shuffleBefore)
	a.flash(T("random_song", a.lib.Songs[i].Title))
}

// holdState es una flecha en la pantalla de reproducción: cuánto lleva
// pulsada y si ya buscó (entonces, al soltarla, no cambia de canción).
type holdState struct {
	frames int
	sought bool
}

// Mantener una flecha busca tras holdFrames, y luego cada seekEvery frames.
const (
	holdFrames = 18 // 0,3 s
	seekEvery  = 12 // 0,2 s: 50 s de canción por segundo
)

// tapOrHold atiende una flecha: soltada antes de holdFrames, manda cmd
// (anterior o siguiente); mantenida, busca seek segundos cada seekEvery frames.
func (a *App) tapOrHold(h *holdState, d int, cmd string, seek float64) {
	if d > 0 {
		h.frames = d
		if d >= holdFrames && (d-holdFrames)%seekEvery == 0 {
			a.seekBy(seek)
			h.sought = true
		}
		return
	}
	if h.frames > 0 && !h.sought {
		a.send(cmd)
	}
	*h = holdState{}
}

// watchProfile graba un perfil de CPU de 20 s si aparece data/cpuprofile (se
// borra al empezar): para medir en la consola sin relanzar la interfaz.
// Queda en data/cpu.prof. Se mira cada 2 s.
func (a *App) watchProfile() {
	if a.offline || int(a.t*60)%120 != 0 {
		return
	}
	req := filepath.Join(a.paths.Data, "cpuprofile")
	if _, err := os.Stat(req); err != nil {
		return
	}
	_ = os.Remove(req)
	f, err := os.Create(filepath.Join(a.paths.Data, "cpu.prof"))
	if err != nil {
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return
	}
	log.Printf("cpu profile: recording 20 s")
	go func() {
		time.Sleep(20 * time.Second)
		pprof.StopCPUProfile()
		f.Close()
		log.Printf("cpu profile: written")
	}()
}
