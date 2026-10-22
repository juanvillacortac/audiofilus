package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/history"
	"github.com/juanvillacortac/audiofilus/internal/lyrics"
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
	"strconv"
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

	// La vista de letras de Now Playing (lyrics.go).
	// Las favoritas y el historial (marks.go): marks se carga la primera
	// vez; hist es el historial leído y histAt la fecha de su archivo
	// entonces; timerChoice, la última opción del temporizador.
	eqEdit       *eqEditor   // el editor del ecualizador abierto (sound.go)
	peqEdit      *peqEditor  // el del paramétrico (peq.go)
	rateDirty    bool        // se cambió la frecuencia de salida: reiniciar al cerrar la elección
	acctCh       chan func() // lo que vuelve de comprobar una cuenta (sound.go)
	meterTracker bool        // el medidor que se mira es el del tracker (meter.go)
	marks        *history.Marks
	hist         []history.Listen
	histAt       time.Time
	timerChoice  int

	lyricsOn bool
	// radioKey es la canción de la radio que suena (lyricsKey) y radioAt,
	// cuándo empezó (cuándo cambió el título): para la letra.
	radioKey  string
	radioAt   float64
	lyr       lyricsView
	lyrCh     chan lyricsResult
	lyrFinder *lyrics.Finder

	mode           mode
	scan           *Scan
	picker         *Picker
	pickFor        pickTarget // para qué es la carpeta del explorador (folders.go)
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
	quitting      bool // se pidió salir: Update termina el bucle
	ptr           pointer
	pendingFolder atomic.Pointer[string] // una carpeta elegida fuera (SetMusicFolder)
	// El gesto Atrás del sistema (Android): backReq lo pide para el próximo
	// fotograma; atRoot dice si ya no hay adónde volver (BackGesture).
	backReq, atRoot atomic.Bool
	ptrOX, ptrOY    int // dónde empieza la interfaz en la pantalla (SetPointerOffset)
	// ExternalBackdrop: el fondo lo pinta quien la contiene, en toda la
	// pantalla (el móvil, con DrawBackdrop): la interfaz no pinta el suyo y
	// no se nota el borde de la zona segura.
	ExternalBackdrop bool
	artSent          string       // la canción cuya portada ya se mandó (sendArt)     // el ratón o el dedo y las zonas tocables (pointer.go)
	drawer           *tabDrawer   // el desplegable de pestañas abierto (touchui.go)
	kbEvents         chan kbEvent // lo que llega del teclado del sistema (keyboard.go)
	kbField          string       // el campo con el teclado del sistema abierto ("" ninguno)
	promptAccept     bool         // Intro en el teclado del sistema: aceptar el nombre
	queueScr         scroller     // el desplazamiento de la cola con el dedo
	runnerDuckY      int          // en Carrera, desde dónde un dedo apoyado agacha
	settingsScr      scroller     // y el de Ajustes (su posición va en settingsScroll)
	splashing        bool         // se está mostrando el splash
	upd              updateState  // las actualizaciones (update.go)
	mods             modState     // los módulos de la red (modules.go)
	meta             metaState    // las herramientas de las etiquetas (tagsui.go)
	cast             castState    // mandar la música a un aparato de la red (castui.go)
	splashT          float64      // segundos de splash

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
	runner  *runnerGame   // Carrera (runner.go)
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
	srvPlCh    chan srvPlDone    // listas de servidor editadas (serverplaylists.go)
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
	themeFor *Album    // el tema se tiñe con este álbum en cuanto se lea su portada
	frames2  frameLog  // los fotogramas lentos (framelog.go)
	books    bookState // los audiolibros (books.go)

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
	h            int // el alto del lienzo: 240 en horizontal, más en vertical (ViewSize)
}

// NewApp prepara la interfaz. Con offline (las capturas y la prueba de
// memoria) no se conecta al servicio y la reproducción se simula.
func NewApp(paths config.Paths, offline bool) (*App, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		return nil, err
	}
	SetLanguage(cfg.Language)
	if !offline {
		migrateGameFiles(paths.Data)
	}
	a := &App{
		libTab: tabSongs, // Buscar es la primera pestaña, pero se entra por Canciones
		paths:  paths, cfg: cfg, offline: offline,
		lyrCh:  make(chan lyricsResult, 4),
		acctCh: make(chan func(), 4),
		covers: newCovers(2), theme: DefaultTheme, battery: -1, now: time.Now(), h: ScreenH,
		kbEvents: make(chan kbEvent, 64),
	}
	a.input.swapAB = cfg.UI.SwapAB
	a.vis.kind = visualizerIndex(cfg.UI.Visualizer)
	if th, ok := fixedTheme(cfg.UI.ThemeColor); ok {
		a.theme = th
	}
	// En el móvil se empieza con el dedo (AUDIOFILUS_TOUCH lo fuerza, para
	// probarlo y para las capturas).
	a.input.touch = StartService != nil || os.Getenv("AUDIOFILUS_TOUCH") != ""
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
		// Volver de segundo plano (el servicio ya sonaba) es volver a la
		// app, no abrirla: sin splash.
		a.splashing = !a.client.WasRunning
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

// PickFolder, si está, abre el selector de carpetas del sistema en vez del
// explorador de la app (en iOS, el de Archivos: cada app solo ve su carpeta
// y lo que el usuario le da). Lo elegido llega con SetMusicFolder.
var PickFolder func()

// SetMusicFolder pone dir como la carpeta de música y la vuelve a leer. Se
// puede llamar desde otro hilo (el selector del sistema): se aplica en el
// siguiente Update.
func (a *App) SetMusicFolder(dir string) { a.pendingFolder.Store(&dir) }

// BackGesture atiende el gesto Atrás del sistema (en Android; se llama desde
// otro hilo). En Reproduciendo, sin menús ni teclado, no hay adónde volver:
// devuelve false y la app pasa a segundo plano. Si no, cuenta como B en el
// próximo fotograma.
func (a *App) BackGesture() bool {
	if a.atRoot.Load() {
		return false
	}
	a.backReq.Store(true)
	ebiten.ScheduleFrame()
	return true
}

// startScan busca la música de la configuración en segundo plano.
func (a *App) startScan() {
	a.scan = StartScan(a.paths, a.cfg.MusicDirs)
	a.mode = modeScan
}

// Close suelta el servicio: la música sigue sonando en segundo plano.
func (a *App) Close() {
	a.saveBooks()
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
	if a.castSend(cmd, args) { // mandando a un aparato: la orden va allí (castui.go)
		return
	}
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
	if StartService != nil {
		// En el móvil una app no se cierra a sí misma (iOS no lo permite):
		// "Salir" para la música; "Segundo plano" no hace falta, la música
		// ya sigue al salir de la app.
		if !background {
			a.send("pause")
		}
		return
	}
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
	defer a.frameStart()() // los fotogramas lentos, al log (framelog.go)
	if dir := a.pendingFolder.Swap(nil); dir != nil {
		a.cfg.MusicDirs = []string{*dir}
		a.saveConfig()
		a.startScan()
	}
	if a.client != nil && a.client.QuitRequested() && StartService == nil {
		a.quitting = true // cerraron el servicio desde fuera: la interfaz también (no en el móvil)
	}
	if a.quitting {
		return ebiten.Termination
	}
	pollFonts(a.ShotsDir != "" || a.FramesDir != "") // las capturas, con todas las letras
	a.input.update()
	if a.backReq.Swap(false) {
		a.input.inject(ActBack)
	}
	defer func() {
		a.atRoot.Store(a.screen == NowPlaying && a.menu == nil && !a.lyricsOn && !a.sleeping)
	}()
	a.updatePointer()
	a.setBottomH()
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
	// Los botones cuentan también en el explorador de carpetas y mientras
	// se escanea: si no, al llegar a la biblioteca el sleep creía que nadie
	// había tocado nada desde el arranque y apagaba la pantalla al momento.
	if a.input.any {
		a.lastInput = a.t
	}
	if a.splashing && a.updateSplash() {
		return nil // el splash se come los botones; la biblioteca carga detrás
	}

	switch a.mode {
	case modeScan:
		if lib, err, ok := a.scan.Result(); ok {
			// Sin música en la carpeta (o sin poder leerla) se entra igual:
			// los servidores, la radio, los podcasts y YouTube Music no la
			// necesitan, y la carpeta se cambia en Ajustes. Antes se quedaba
			// en el explorador, sin forma de llegar a ellos.
			switch {
			case err != nil:
				a.flash(err.Error())
			case lib == nil || len(lib.Songs) == 0:
				a.flash(T("no_music"))
			}
			if lib == nil {
				lib = &Library{}
			}
			a.setLocal(lib) // y lo que suena se vuelve a buscar en la biblioteca nueva
			a.mode = modeMain
			a.lastInput = a.t // el escaneo pudo ser largo: el sleep cuenta desde aquí
			if a.offline {
				a.simulatePlaying()
			}
		}
		return nil
	case modePicker:
		if dir := a.picker.update(&a.input); dir != "" && a.pickFor != pickMusic {
			a.pickedDir(dir) // la de MilkDrop o la de radio
		} else if dir != "" {
			a.cfg.MusicDirs = []string{dir}
			a.firstRun = false
			a.saveConfig()
			a.startScan()
		} else if a.lib != nil && !a.firstRun && a.picker.dir == "" && a.input.Pressed(ActBack) {
			a.mode, a.pickFor = modeMain, pickMusic // salir del explorador sin cambiar nada
		}
		return nil
	}

	a.syncStatus()
	a.pollCast() // mandando a un aparato: su canción y su posición (castui.go)
	a.sendArt()
	a.updateLyricsFetch()
	a.updateAccounts()
	a.updateKeyboard()
	if a.drawer != nil {
		a.updateDrawer()
		return nil
	}
	a.updateYT()
	a.updateServers()
	a.pollSrvPlaylists() // las listas de servidor editadas (serverplaylists.go)
	a.updateRadioResults()
	a.updatePodResults()
	a.pollMeta() // lo que vuelve de las herramientas de las etiquetas (tagsui.go)
	a.pollModules()
	a.trackPodcast()
	a.trackBooks() // los audiolibros: dónde iban, capítulos, velocidad (books.go)
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
	a.pollUpdate()
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
	// así que si nadie la usa, al menos se apaga la pantalla. Jugando, nunca;
	// leyendo la letra, tampoco.
	if after <= 0 || a.menu != nil || a.screen == Blocks || a.screen == Rhythm || a.screen == Crypt || a.screen == Tracker || a.screen == Quiz || a.screen == Snake || a.screen == Shooter || a.screen == Duel || a.screen == Runner || a.screen == Visual || (a.screen == NowPlaying && a.lyricsOn) || (a.isPlaying && a.screen != NowPlaying) || a.t-a.lastInput < float64(after) {
		return
	}
	a.enterSleep()
}

// canSleep dice si hay sleep de la app (apagar la pantalla y controlar con
// gestos desde el bolsillo): en las portátiles, no en el escritorio ni en el
// móvil (ahí lo hace el sistema).
func (a *App) canSleep() bool {
	return a.offline || !a.mobile() && a.status.Platform != "desktop"
}

// mobile dice si es la app del móvil (el servicio va en el mismo proceso).
// AUDIOFILUS_MOBILE lo simula en las capturas, para verlas como en el móvil.
func (a *App) mobile() bool { return StartService != nil || previewMobile }

// previewMobile: las capturas, como en el móvil (AUDIOFILUS_MOBILE).
var previewMobile = os.Getenv("AUDIOFILUS_MOBILE") != ""

// enterSleep empieza a apagar la pantalla: primero se disuelve en negro.
func (a *App) enterSleep() {
	if !a.canSleep() {
		return
	}
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
	if a.eqEdit != nil {
		a.updateEQ()
		return
	}
	if a.peqEdit != nil { // el ecualizador paramétrico (peq.go)
		a.updatePEQ()
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
	if a.screen == Runner && a.menu == nil {
		if a.runner == nil {
			a.runner = a.newRunner()
		}
		a.updateRunner()
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
	if a.visualBGOn() {
		a.stepVisual() // el visualizador de fondo del reproductor
	}
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
	case a.screen == NowPlaying && a.isBook(a.playingSong.Path) && in.Pressed(ActPrev):
		a.chapterStep(-1) // en un libro, L1/R1 cambian de capítulo (books.go)
	case a.screen == NowPlaying && a.isBook(a.playingSong.Path) && in.Pressed(ActNext):
		a.chapterStep(1)
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
		case a.lyricsOn && a.playingSong.Path != "" && (in.Pressed(ActBack) || in.Pressed(ActMenu) || in.Repeat(ActUp) || in.Repeat(ActDown)):
			a.updateLyrics()
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
			a.screen = a.playingHome()
		case in.Pressed(ActUp):
			a.scratch(-1) // huevo de pascua: scratch con el vinilo (eggs.go)
		case in.Pressed(ActDown):
			a.scratch(1)
		}
		// Izquierda/derecha: un toque cambia de canción y mantenerlo busca.
		// Así se puede todo sin gatillos, que no todas las consolas tienen.
		if a.spoken() {
			// Un libro o un podcast: un toque salta 30 s (books.go).
			a.tapOrHold(&a.leftHold, in.duration(ActLeft), "-30", -10)
			a.tapOrHold(&a.rightHold, in.duration(ActRight), "+30", +10)
		} else {
			a.tapOrHold(&a.leftHold, in.duration(ActLeft), "prev", -10)
			a.tapOrHold(&a.rightHold, in.duration(ActRight), "next", +10)
		}
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
	case Modules:
		a.updateModules()
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
		// Al cerrar, seguir sonando: el valor por defecto depende de la
		// plataforma, que solo se sabe con el primer estado.
		defer func() { a.client.SetKeepPlaying(a.keepPlaying()) }()
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
		// La misma pista, pero una emisora anuncia otra canción (su título
		// ICY): lo que suena se pone al día sin tratarla como pista nueva.
		if radio.IsRadio(st.Track.Path) && st.Track.Title != "" &&
			(st.Track.Title != a.playingSong.Title || st.Track.Artist != a.playingSong.Artist) {
			a.playingSong.Title, a.playingSong.Artist = st.Track.Title, st.Track.Artist
			if a.playing != nil && a.playing.stray && len(a.playing.Songs) > 0 {
				a.playing.Songs[0] = a.playingSong
			}
		}
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
	if a.lib == nil || len(a.lib.Albums) == 0 {
		return // sin música: no suena nada (así se ve la app vacía)
	}
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
	// Con AUDIOFILUS_SHOT_LYRICS=título, la canción con ese título (para la
	// secuencia de la letra, ver lyricsFrames).
	if name := strings.ToLower(os.Getenv("AUDIOFILUS_SHOT_LYRICS")); name != "" {
		for _, b := range a.lib.Albums {
			for i, s := range b.Songs {
				if strings.Contains(strings.ToLower(s.Title), name) {
					al = b
					a.playing, a.playingIdx, a.playingSong = b, i, s
				}
			}
		}
	}
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
	if level, charging, ok := sysBattery("/sys/class/power_supply"); ok {
		a.battery, a.charging = level, charging
	}
}

// sysBattery lee la batería de la consola en sysfs (dir es
// /sys/class/power_supply). Puede haber varias del tipo "Battery": la del
// sistema, la de un mando (scope "Device"), una que no está (present 0) o
// la de un medidor que no informa (capacidad 0, visto en las RK3566 con
// dArkOS). Se queda con la del sistema que esté y diga algo; a igualdad, la
// de más capacidad, y la primera por nombre.
func sysBattery(dir string) (level float64, charging, ok bool) {
	read := func(sup, f string) string {
		b, _ := os.ReadFile(filepath.Join(dir, sup, f))
		return strings.TrimSpace(string(b))
	}
	ents, _ := os.ReadDir(dir)
	best := -1
	var bestCharging bool
	for _, e := range ents {
		sup := e.Name()
		if read(sup, "type") != "Battery" || read(sup, "scope") == "Device" || read(sup, "present") == "0" {
			continue
		}
		var v int
		if _, err := fmt.Sscan(read(sup, "capacity"), &v); err != nil || v < 0 || v > 100 {
			v = -1
		}
		// El contador de carga manda sobre la capacidad: hay medidores que
		// dan un porcentaje que no es cero pero tampoco es el real (el RK817
		// de la RG353V con dArkOS marca 24 % casi llena), mientras que
		// charge_now/charge_full sí sigue a la carga.
		if r := batteryChargeRatio(func(f string) string { return read(sup, f) }); r >= 0 {
			v = r
		} else if v <= 0 {
			// 0 % con la consola encendida no es real: el medidor no calcula
			// el porcentaje (el mismo RK817). Se saca de la tensión.
			v = batteryEstimate(func(f string) string { return read(sup, f) })
		}
		if v < 0 {
			continue
		}
		if v > best {
			best = v
			switch read(sup, "status") {
			case "Charging", "Full":
				bestCharging = true
			default:
				bestCharging = false
			}
		}
	}
	if best < 0 {
		return 0, false, false
	}
	return float64(best) / 100, bestCharging, true
}

// liIon es la curva de descarga de una celda de litio: tensión (V) y
// porcentaje, de más a menos.
var liIon = []struct{ v, pct float64 }{
	{4.20, 100}, {4.10, 90}, {4.00, 78}, {3.90, 65}, {3.80, 50}, {3.75, 40},
	{3.70, 30}, {3.65, 20}, {3.60, 12}, {3.50, 5}, {3.40, 0},
}

// batteryChargeRatio saca el porcentaje del contador de carga
// (charge_now/charge_full o energy_now/energy_full), o -1 si el medidor no
// lo lleva.
func batteryChargeRatio(read func(f string) string) int {
	num := func(f string) float64 {
		var v float64
		if _, err := fmt.Sscan(read(f), &v); err != nil {
			return -1
		}
		return v
	}
	for _, pair := range [][2]string{{"charge_now", "charge_full"}, {"energy_now", "energy_full"}} {
		if now, full := num(pair[0]), num(pair[1]); now >= 0 && full > 0 {
			return int(math.Round(math.Min(100, now/full*100)))
		}
	}
	return -1
}

// batteryEstimate calcula el porcentaje cuando la batería no lo da: de la
// carga (charge_now/charge_full o energy_now/energy_full) o, si no, de la
// tensión con la curva de una celda de litio. -1 si no hay con qué.
func batteryEstimate(read func(f string) string) int {
	if r := batteryChargeRatio(read); r >= 0 {
		return r
	}
	num := func(f string) float64 {
		var v float64
		if _, err := fmt.Sscan(read(f), &v); err != nil {
			return -1
		}
		return v
	}
	uv := num("voltage_now")
	if uv <= 0 {
		uv = num("voltage_avg")
	}
	if uv <= 0 {
		return -1
	}
	v := uv / 1e6 // en microvoltios
	if v >= liIon[0].v {
		return 100
	}
	for i := 1; i < len(liIon); i++ {
		if v >= liIon[i].v {
			hi, lo := liIon[i-1], liIon[i]
			return int(math.Round(lo.pct + (v-lo.v)/(hi.v-lo.v)*(hi.pct-lo.pct)))
		}
	}
	return 0
}

func firstOr(s []string, d string) string {
	if len(s) > 0 {
		return s[0]
	}
	return d
}

// Draw pinta la pantalla actual.
func (a *App) Draw(screen *ebiten.Image) {
	defer a.drawStart()()
	if a.BenchVisual != "" {
		return // la prueba pinta en su lienzo; pintar aquí también falsearía la medida
	}
	a.beginZones()
	defer a.endZones()
	a.setBottomH()
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
	a.drawDrawer(screen)
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
	// Con un color fijo (Ajustes → Color), los discos no lo cambian.
	if th, ok := fixedTheme(a.cfg.UI.ThemeColor); ok {
		a.theme, a.themeFor = th, nil
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
	if !a.ExternalBackdrop {
		drawBackground(dst, a.theme, a.t)
	}
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
		case Runner:
			if a.runner == nil {
				a.runner = a.newRunner()
			}
			a.drawRunner(dst)
			a.drawTopBar(dst, T("ru_title"))
		case Games:
			a.drawGames(dst)
			a.drawTopBar(dst, T("gm_title"))
		case Visual:
			a.drawVisual(dst)
			a.drawTopBar(dst, T("vis_title")+" · "+T(visNames[a.vis.kind]))
		case NowPlaying:
			if a.visualBGOn() && a.playingSong.Path != "" {
				a.drawVisualBG(dst)
			}
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
		case Modules:
			a.drawModules(dst)
		case Settings:
			a.drawSettings(dst)
		}
		a.drawYTBar(dst)
		a.drawMenu(dst)
		a.drawEQ(dst)
		a.drawPEQ(dst)
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

// Layout da el lienzo lógico según la orientación de la ventana (ViewSize).
func (a *App) Layout(w, h int) (int, int) {
	a.w, a.h = ViewSize(w, h)
	return a.w, a.h
}

// NowPlayingArt, si está, recibe la portada de lo que suena (PNG) en cuanto
// la interfaz la tiene cargada, con la ruta de la canción: en el móvil, para
// la pantalla bloqueada cuando la portada no está en un archivo local (el
// logo de una radio, Subsonic, YouTube Music).
var NowPlayingArt func(path string, png []byte)

// sendArt le pasa a NowPlayingArt la portada de lo que suena, una vez por
// canción, cuando ya está cargada.
func (a *App) sendArt() {
	if NowPlayingArt == nil || a.playing == nil || a.playingSong.Path == "" || a.artSent == a.playingSong.Path {
		return
	}
	img, alpha := a.cover(a.playing, 160)
	if img == nil || alpha < 1 {
		return // aún cargando (se vuelve a mirar en el siguiente fotograma)
	}
	a.artSent = a.playingSong.Path
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	img.ReadPixels(rgba.Pix)
	var buf bytes.Buffer
	if png.Encode(&buf, rgba) == nil {
		go NowPlayingArt(a.playingSong.Path, buf.Bytes())
	}
}

// Asleep dice si la pantalla está "apagada" (el sleep de la app: negro).
func (a *App) Asleep() bool { return a.sleeping }

// DrawBackdrop pinta el fondo animado de la interfaz en dst (en el móvil,
// en toda la pantalla: también bajo la barra de estado y la muesca, donde
// no va contenido).
func (a *App) DrawBackdrop(dst *ebiten.Image) { drawBackground(dst, a.theme, a.t) }

// DrawInsets pinta las franjas de la zona segura de arriba (top) y abajo
// (bottom) del oscuro de las barras de la interfaz (la de arriba y la de la
// ayuda), sobre el fondo: las barras parecen llegar hasta el borde, detrás
// de la hora y la isla y bajo la raya de inicio.
func (a *App) DrawInsets(dst *ebiten.Image, top, bottom int) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	bar := withAlpha(a.theme.Shadow, 0x99) // el de drawTopBar y drawHints
	if a.sleeping {
		bar = color.RGBA{0, 0, 0, 0xff}
	}
	rect(dst, 0, 0, w, top, bar)
	rect(dst, 0, h-bottom, w, bottom, bar)
}

// SetPointerOffset dice dónde empieza la interfaz dentro de la pantalla (en
// el móvil, tras la zona segura): se resta a los toques.
func (a *App) SetPointerOffset(x, y int) { a.ptrOX, a.ptrOY = x, y }

// portrait dice si la pantalla está en vertical (más alta que ancha).
func (a *App) portrait() bool { return a.h > ScreenH }

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
		a.w, a.h = asp.W, asp.H
		tmp := ebiten.NewImage(asp.W, asp.H)
		for s := Screen(0); s < screenCount; s++ {
			a.drawScreen(tmp, s)
		}
		for tab := 0; tab < tabCount; tab++ {
			a.libTab = tab
			a.drawScreen(tmp, LibraryScreen)
		}
		tmp.Deallocate()
	}
	a.h = ScreenH
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
			a.w, a.h = asp.W, asp.H
			if err := a.renderShot(filepath.Join(a.ShotsDir, fmt.Sprintf("%s-%s.png", name, asp.Name)), asp.W); err != nil {
				return err
			}
		}
		a.h = ScreenH
		return nil
	}
	a.screen = NowPlaying
	if err := shot("reproduciendo"); err != nil {
		return err
	}
	// Con el temporizador puesto (y el corazón, si la canción es favorita en
	// la maqueta).
	a.status.TimerLeft = 25 * 60
	if err := shot("reproduciendo-temporizador"); err != nil {
		return err
	}
	a.status.TimerLeft = 0
	// Las estadísticas, si la maqueta tiene historial.
	if len(a.listens()) > 0 {
		a.screen, a.libTab, a.tabFocus = LibraryScreen, tabLists, false
		a.push(a.statsView())
		if err := shot("estadisticas"); err != nil {
			return err
		}
		a.pop()
		a.screen = NowPlaying
	}
	// La letra sincronizada (inventada) y la lista para elegir otra.
	if a.playingSong.Path != "" {
		a.lyricsOn = true
		a.lyr = lyricsView{path: a.playingSong.Path, opt: shotLyrics(a.shownPos-13.5, 3)}
		if err := shot("letras"); err != nil {
			return err
		}
		a.openLyricsPicker(lyricsResult{path: a.playingSong.Path, opts: []lyrics.Option{
			{ID: "file:/m/song.lrc", Label: a.playingSong.Title + ".lrc", Lyrics: lyrics.Lyrics{Synced: true}},
			a.lyr.opt,
			{ID: "lrclib:2", Label: a.playingSong.Artist + " - " + a.playingSong.Title + " (Live)", Detail: "Live 2004 · 4:51", Lyrics: lyrics.Lyrics{Synced: true}},
			{ID: "tag", Lyrics: lyrics.Lyrics{Lines: []lyrics.Line{{Text: "x"}}}},
		}})
		if err := shot("letras-elegir"); err != nil {
			return err
		}
		a.menu, a.lyricsOn, a.lyr = nil, false, lyricsView{}
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
	if a.playing != nil { // sin música no hay álbum que abrir
		a.openAlbum(a.playing)
		if err := shot("album"); err != nil {
			return err
		}
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
	// El editor del ecualizador, con el preajuste Rock.
	a.eqEdit = &eqEditor{gains: engine.PresetGains("rock"), preset: "rock", sel: 6}
	if err := shot("ecualizador"); err != nil {
		return err
	}
	a.eqEdit = nil
	a.peqEdit = &peqEditor{bands: []engine.PEQBand{{Type: "lowshelf", Freq: 90, Gain: 4, Q: 0.7}, {Type: "peak", Freq: 2500, Gain: -3, Q: 2},
		{Type: "highshelf", Freq: 9000, Gain: 2.5, Q: 0.7}, {Type: "highpass", Freq: 30, Q: 0.7}}, preamp: -4, row: 2, col: 1}
	if err := shot("ecualizador-parametrico"); err != nil {
		return err
	}
	a.peqEdit = nil
	// Ajustes en la sección Sonido, y el menú de las cuentas.
	if tiles, _, _ := settingsLayout(a.w, a.sections()); len(tiles) > 0 {
		for i, t := range tiles {
			if t.tile.id == setReplayGain {
				a.settingsSel = i
			}
		}
	}
	if err := shot("ajustes-sonido"); err != nil {
		return err
	}
	a.openScrobbleMenu()
	if err := shot("cuentas"); err != nil {
		return err
	}
	a.menu, a.settingsSel, a.settingsScroll = nil, 0, 0
	a.screen = NowPlaying
	a.openNowMenu()
	if err := shot("menu"); err != nil {
		return err
	}
	a.menu = nil
	if err := a.shotsNew(shot); err != nil {
		return err
	}
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
	// Mis listas de la radio, si la carpeta radio/ de la maqueta tiene alguna.
	if list := radio.LoadPlaylists(a.radioDir()); len(list) > 0 {
		a.radio.loaded = true
		a.radio.lists[rtLists], a.radio.tab, a.radio.sel[rtLists], a.radio.tabFocus = list, rtLists, 0, false
		a.screen = Radio
		if err := shot("radio-listas"); err != nil {
			return err
		}
		a.screen = NowPlaying
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
		// Los módulos: lo más bajado de The Mod Archive (con red).
		a.modInit()
		for i := 0; i < 300 && a.mods.loading[0]; i++ {
			time.Sleep(100 * time.Millisecond)
			a.pollModules()
		}
		if len(a.mods.lists[0]) > 0 {
			a.screen, a.mods.tabFocus, a.mods.sel[0] = Modules, false, 2
			if err := shot("modulos"); err != nil {
				return err
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
		a.w, a.h = asp.W, asp.H
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
	// Carrera: unos compases con su bot.
	a.screen, a.runner = Runner, a.newRunner()
	for i := 0; i < 60*12; i++ {
		a.t += 1.0 / 60
		a.beatHit = a.updateBeat()
		a.botRunner()
		a.stepRunner(a.runner)
	}
	if err := shot("carrera"); err != nil {
		return err
	}
	a.runner = nil
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
	// El tracker: la canción de ejemplo sonando (adelantada un compás y
	// medio), en la frase del pulso 2 con el cursor en su comando; después,
	// la canción, el piano roll y el instrumento.
	a.screen, a.tracker = Tracker, a.newTracker()
	ex := tracker.Examples()[0]
	a.tracker.song = ex
	a.tracker.player.SetSong(ex)
	a.tracker.player.Play(tracker.PlaySong, 1, 0)
	a.tracker.playing = true
	_, _ = a.tracker.player.Read(make([]byte, 8*sfx.Rate*60/ex.BPM*6+8))
	a.tracker.view, a.tracker.ch, a.tracker.songRow = tvPhrase, 1, 1
	a.tracker.chain = int(ex.Rows[1][1])
	a.tracker.phrase = int(ex.Chains[a.tracker.chain][0].Phrase)
	a.tracker.phRow, a.tracker.phCol = 4, 3
	if err := shot("tracker"); err != nil {
		return err
	}
	a.tracker.view = tvSong
	if err := shot("tracker-cancion"); err != nil {
		return err
	}
	a.tracker.view, a.tracker.roll, a.tracker.rollNote = tvPhrase, true, 65
	if err := shot("tracker-piano"); err != nil {
		return err
	}
	a.tracker.view, a.tracker.instr = tvInstr, 1
	if err := shot("tracker-instrumento"); err != nil {
		return err
	}
	a.tracker.view = tvProject
	if err := shot("tracker-proyecto"); err != nil {
		return err
	}
	// La guía de FN en el tracker: la lista más larga, en varias columnas.
	a.tracker.view = tvSong
	a.guideAt = a.t - 1
	if err := shot("guia-tracker"); err != nil {
		return err
	}
	a.guideAt = 0
	a.tracker.player.Stop()
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
	a.tracker.player.Play(tracker.PlaySong, 1, 0)
	a.tracker.playing, a.tracker.view, a.tracker.songRow = true, tvSong, 1
	chunk := make([]byte, 8*sfx.Rate/20)
	for i := 0; i < 240; i++ {
		a.t = 30 + float64(i)/20
		_, _ = a.tracker.player.Read(chunk)
		a.trackerFollow()
		// La primera mitad, la frase que suena en PU2 (siguiéndola); la
		// segunda, la canción.
		if i < 120 {
			a.tracker.view, a.tracker.ch = tvPhrase, 1
			if _, ph := a.tracker.player.Now(1); ph != tracker.Empty {
				a.tracker.phrase = ph
			}
			if _, _, step, _ := a.tracker.player.Position(1); i%2 == 0 {
				a.tracker.phRow, a.tracker.phCol = step, 0
			}
		} else {
			a.tracker.view = tvSong
			if row, _, _, _ := a.tracker.player.Position(0); i%4 == 0 {
				a.tracker.songRow = row // el cursor acompaña un poco
			}
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
		{"carrera", func() { a.screen, a.runner = Runner, a.newRunner() }, func() { a.beatHit = a.updateBeat(); a.botRunner(); a.stepRunner(a.runner) }},
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
	a.snake, a.shooter, a.duel, a.runner = nil, nil, nil, nil
	// El desplegable de pestañas del móvil (en vertical y con el dedo): se
	// abre, se elige Álbumes y se cierra; se vuelve a abrir y se cierra
	// tocando fuera.
	if err := a.drawerFrames(filepath.Join(a.FramesDir, "pestanas")); err != nil {
		return err
	}
	// La letra (inventada) sonando: el karaoke, sus chispas y el cambio de línea.
	if err := a.lyricsFrames(filepath.Join(a.FramesDir, "letras")); err != nil {
		return err
	}
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
		if kind == visMilk {
			if err := a.milkFrames(dir); err != nil {
				return err
			}
			continue
		}
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
	// Un audiolibro sonando: el casete con los carretes girando (6 s).
	bookDir := filepath.Join(a.FramesDir, "audiolibro")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		return err
	}
	restore := a.shotBook()
	defer restore()
	start := a.position
	base = a.t + 1
	for i := 0; i < 120; i++ {
		a.t = base + float64(i)/20
		a.position = start + float64(i)/20*1.25
		a.shownPos = a.position
		a.spin += 2 * a.spinSpeed() // a 20 fps, lo que giraría en tres fotogramas de 60 (el tercero, al dibujar)
		for k := 0; k < 3; k++ {
			a.beatHit = a.updateBeat()
			a.updatePump()
		}
		if err := a.renderShot(filepath.Join(bookDir, fmt.Sprintf("f%04d.png", i)), 360); err != nil {
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
		a.drawDrawer(dst)
		if a.cfg.UI.GameBoy {
			applyGameBoy(dst)
		}
	})
}

// shotSafeArea es la zona segura simulada de las capturas verticales, en
// píxeles del lienzo (arriba, abajo): AUDIOFILUS_SAFE="36,21" (la de un
// iPhone 15: 59 y 34 puntos de 393 de ancho).
func shotSafeArea() (top, bot int) {
	if s := os.Getenv("AUDIOFILUS_SAFE"); s != "" {
		fmt.Sscanf(s, "%d,%d", &top, &bot)
	}
	return top, bot
}

// render dibuja con draw a w×a.h (240, salvo en las capturas verticales) y
// lo guarda ampliado ×3.
func (a *App) render(path string, w int, draw func(*ebiten.Image)) error {
	a.shownPos = a.position // en las capturas no hay transición
	h := max(a.h, ScreenH)
	canvas := ebiten.NewImage(w, h)
	defer canvas.Deallocate()
	if top, bot := shotSafeArea(); top+bot > 0 && h > ScreenH {
		// Como en un iPhone: el fondo en toda la pantalla, la interfaz en la
		// zona segura y una barra de estado de muestra encima.
		full := a.h
		a.h = h - top - bot
		inner := ebiten.NewImage(w, a.h)
		a.ExternalBackdrop = true
		draw(inner)
		a.ExternalBackdrop = false
		drawBackground(canvas, a.theme, a.t)
		a.DrawInsets(canvas, top, bot)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(0, float64(top))
		canvas.DrawImage(inner, op)
		inner.Deallocate()
		a.h = full
		white := color.RGBA{0xff, 0xff, 0xff, 0xff}
		drawText(canvas, "9:41", SmallFace, 18, top/2-2, white)
		frame(canvas, w-30, top/2-3, 15, 8, white)
		rect(canvas, w-28, top/2-1, 9, 4, white)
		rect(canvas, w-15, top/2-1, 1, 4, white)
		panel(canvas, w/2-28, top/2-7, 56, 16, color.RGBA{0, 0, 0, 0xff}) // la isla
		rect(canvas, w/2-36, h-bot/2-1, 72, 2, white)                     // la raya de inicio
	} else {
		draw(canvas)
	}
	pix := make([]byte, 4*w*h)
	canvas.ReadPixels(pix)
	const k = 3
	out := image.NewRGBA(image.Rect(0, 0, w*k, h*k))
	for y := 0; y < h*k; y++ {
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

// playingHome es a dónde lleva Atrás desde el reproductor: la pantalla de
// lo que suena (Radio, Podcasts) y, si no, la biblioteca. Así no se cambia
// de contexto sin querer al volver.
func (a *App) playingHome() Screen {
	switch p := a.playingSong.Path; {
	case radio.IsRadio(p):
		return Radio
	case p != "" && strings.HasPrefix(p, a.podDir()):
		return Podcasts
	}
	return LibraryScreen
}

// lidClosed dice si se acaba de cerrar la tapa. En las Anbernic con tapa
// (RG34XX-SP, RG35XXSP) la tapa manda Insert al cerrarse y Delete al abrirse
// por el mismo dispositivo del mando (Knulli lo atiende con triggerhappy:
// lid-control). En el PC, Insert es solo una tecla.
func (a *App) lidClosed() bool {
	if a.status.Platform == "" || a.status.Platform == "desktop" {
		return false
	}
	// En la base o con HDMI a la tele, la tapa cerrada es lo normal (se
	// juega con mando): no apaga la pantalla.
	if a.status.Docked {
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
		switch cmd {
		case "-30":
			a.seekBy(-bookSkip)
		case "+30":
			a.seekBy(bookSkip)
		default:
			a.send(cmd)
		}
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

// drawerFrames guarda en dir la secuencia del desplegable de pestañas, en un
// iPhone (240×520) y con el dedo, a 20 cuadros por segundo.
func (a *App) drawerFrames(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w0, h0, touch0, screen0, tab0 := a.w, a.h, a.input.touch, a.screen, a.libTab
	defer func() { a.w, a.h, a.input.touch, a.screen, a.libTab = w0, h0, touch0, screen0, tab0 }()
	a.w, a.h, a.input.touch = 240, 520, true
	a.setBottomH()
	a.screen, a.libTab, a.tabFocus = LibraryScreen, tabSongs, false
	pick := func(i int) { a.libTab, a.tabFocus = i, false }
	for i := 0; i < 90; i++ {
		a.t += 1.0 / 20
		switch i {
		case 12, 56: // tocar el botón
			a.drawer = &tabDrawer{names: tabs(), sel: a.libTab, pick: pick, at: a.t}
		case 28: // el dedo baja a Álbumes...
			a.drawer.sel = tabAlbums
		case 32: // ...y lo toca
			a.closeDrawer(tabAlbums)
		case 74: // tocar fuera
			a.closeDrawer(-1)
		}
		if a.drawer != nil {
			a.updateDrawer()
		}
		if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), a.w); err != nil {
			return err
		}
	}
	a.drawer = nil
	return nil
}

// lyricsFrames guarda en dir 10 s de la vista de letras sonando (la letra
// inventada de shotLyrics: una intro instrumental y luego las líneas), en
// horizontal a 20 cuadros por segundo, con un golpe de ritmo cada medio
// segundo.
func (a *App) lyricsFrames(dir string) error {
	if a.playingSong.Path == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w0, screen0, playing0, pos0 := a.w, a.screen, a.isPlaying, a.position
	al0, idx0, song0, theme0 := a.playing, a.playingIdx, a.playingSong, a.themeFor
	defer func() {
		a.w, a.screen, a.isPlaying, a.position, a.shownPos = w0, screen0, playing0, pos0, pos0
		a.playing, a.playingIdx, a.playingSong, a.themeFor = al0, idx0, song0, theme0
		a.lyricsOn, a.lyr = false, lyricsView{}
	}()
	a.w, a.screen, a.isPlaying, a.lyricsOn = 360, NowPlaying, true, true
	opt := shotLyrics(a.position+2.5, 2.2)
	// Con AUDIOFILUS_SHOT_LYRICS=título, la canción que suena es esa (ver
	// simulatePlaying), con su letra
	// de verdad (de sus archivos, etiquetas o LRCLIB), desde el segundo
	// AUDIOFILUS_SHOT_AT: para verla a mano, no para la web.
	if os.Getenv("AUDIOFILUS_SHOT_LYRICS") != "" { // la eligió simulatePlaying
		tmp, err := os.MkdirTemp("", "audiofilus-lyrics")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fd := &lyrics.Finder{Store: lyrics.NewStore(tmp), Web: &lyrics.LRCLIB{}}
		q := lyrics.Query{Title: a.playingSong.Title, Artist: a.playingSong.Artist, Album: a.playingSong.Album}
		opt = fd.Find(context.Background(), a.playingSong.Path, q)
		at, _ := strconv.ParseFloat(os.Getenv("AUDIOFILUS_SHOT_AT"), 64)
		a.position, a.shownPos = at, at
	}
	a.lyr = lyricsView{path: a.playingSong.Path, opt: opt}
	for i := 0; i < 200; i++ {
		a.t += 1.0 / 20
		a.position += 1.0 / 20
		a.shownPos = a.position
		if i%10 == 0 {
			a.beat.at = a.beatClock()
		}
		if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), a.w); err != nil {
			return err
		}
	}
	return nil
}

// shotLyrics es una letra inventada para las capturas, cuya primera línea
// empieza en el segundo start y cada una dura every segundos.
func shotLyrics(start, every float64) lyrics.Option {
	words := []string{
		"Static on the radio tonight", "Every light in town is fading out",
		"I keep the volume low", "So the neighbors never know",
		"Pixel hearts in a cathode glow", "Spinning records, nowhere to go",
		"Turn it up, turn it up", "Until the speakers start to shake",
		"", "We were never meant to sleep", "Counting beats instead of sheep",
	}
	var lines []lyrics.Line
	for i, w := range words {
		lines = append(lines, lyrics.Line{At: time.Duration((start + float64(i)*every) * float64(time.Second)), Text: w})
	}
	return lyrics.Option{ID: "lrclib:1", Label: "audiofilus - Demo", Detail: "Demo · 3:30", Lyrics: lyrics.Lyrics{Lines: lines, Synced: true}}
}
