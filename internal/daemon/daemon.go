// Package daemon une el engine, la plataforma, el mando y el socket de control.
//
// Hay dos modos (ver docs/architecture.md):
//
//   - Segundo plano, con la interfaz cerrada: la consola se suspende como
//     siempre y la música se controla con el atajo de FN (input.Combo).
//   - App abierta: la interfaz pide "awake" mientras está abierta, y el daemon
//     impide la suspensión mientras suene música; además puede pedir "sleep"
//     (pantalla apagada y gestos de doble toque).
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/mpris"
	"github.com/juanvillacortac/audiofilus/internal/platform"
	"github.com/juanvillacortac/audiofilus/internal/queue"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"github.com/juanvillacortac/audiofilus/internal/remote"
	"github.com/juanvillacortac/audiofilus/internal/tray"
)

// defaultLease es lo que dura "awake on" si no se dice otra cosa. La interfaz
// lo renueva mientras está abierta; si se cuelga, caduca y la consola vuelve a
// suspenderse sola.
const defaultLease = 60 * time.Second

// Daemon es el servicio en marcha.
type Daemon struct {
	paths config.Paths
	cfg   config.Config
	eng   *engine.Engine
	plat  platform.Platform

	mu         sync.Mutex
	sleeping   bool
	awakeUntil time.Time // hasta cuándo la app pidió mantener despierta la consola
	// El temporizador para parar la música (timer.go): cuándo, y el que lo
	// hace.
	timerAt time.Time
	timer   *time.Timer
	// restarting: el servicio se cierra para volver a arrancar (restart).
	restarting   bool
	awakeApplied bool       // si la plataforma está impidiendo la suspensión ahora
	pad          string     // descripción del mando en uso
	appOpen      bool       // el permiso de la app está vigente (la interfaz está abierta)
	attached     int        // interfaces conectadas con "attach"
	background   bool       // la interfaz pidió seguir sonando en segundo plano
	sysVolume    int        // último volumen del sistema leído (-1 si no se puede)
	sysSetAt     time.Time  // cuándo se pidió cambiar el volumen del sistema
	powerSeq     int        // toques del botón de encendido que piden sleep (FullStatus.PowerSeq)
	sysSet       chan int   // los cambios de volumen del sistema, para applySystemVolume
	sysKnown     bool       // sysVolume ya se leyó alguna vez
	saveMu       sync.Mutex // un guardado de state.json a la vez (comparten el .tmp)
	volumeSeq    int        // sube con cada cambio de volumen, para el indicador
	output       string     // por dónde sale el sonido (platform.Output*)
	outputAt     time.Time  // cuándo se miró output por última vez
	quit         chan struct{}
	quitOnce     sync.Once
}

// Run arranca el daemon y bloquea hasta recibir quit o una señal.
func Run(paths config.Paths) error {
	cfg, err := config.Load(paths)
	if err != nil {
		return fmt.Errorf("config %s: %w", paths.Config, err)
	}
	eng, err := engine.New(cfg.SampleRate, time.Duration(cfg.BufferMS)*time.Millisecond)
	if err != nil {
		return fmt.Errorf("audio output: %w", err)
	}
	if h := OnEngine; h != nil {
		h(eng)
	}
	d := &Daemon{
		paths: paths,
		cfg:   cfg,
		eng:   eng,
		plat:  platform.Detect(platform.Options{Governor: cfg.Sleep.Governor}),
		quit:  make(chan struct{}),
		// Con hueco para uno: volumeStep cambia el que espera por el nuevo.
		sysSet: make(chan int, 1),

		sysVolume: -1,
	}
	log.Printf("audiofilus on %s, home %s", d.plat.Name(), paths.Base)

	// MPRIS: las teclas multimedia y los widgets del escritorio. Sin bus de
	// sesión (en las portátiles) no se publica y no pasa nada.
	mp, err := mpris.Start(eng, func() { d.quitOnce.Do(func() { close(d.quit) }) })
	if err != nil {
		log.Printf("MPRIS disabled: %v", err)
	} else {
		defer mp.Close()
	}
	// La bandeja del sistema, solo en el escritorio (las portátiles no tienen).
	var tr *tray.Tray
	if d.plat.Name() == "desktop" {
		tr, err = tray.Start(eng, tray.Options{
			Home: paths.Base,
			UIOpen: func() bool {
				d.mu.Lock()
				defer d.mu.Unlock()
				return d.attached > 0
			},
			Quit: func() { d.quitOnce.Do(func() { close(d.quit) }) },
			Lang: cfg.Language,
		})
		if err != nil {
			log.Printf("system tray disabled: %v", err)
			tr = nil
		} else {
			defer tr.Close()
		}
	}
	eng.OnPlayingChanged = func(playing bool) {
		d.updateAwake()
		d.saveState()
		if mp != nil {
			mp.Update()
		}
		if tr != nil {
			tr.Update()
		}
		nowPlayingChanged(eng)
	}
	// Lo remoto (Subsonic): se baja a data/cache sin parar el audio, y sus
	// etiquetas salen del índice que escribe la interfaz. Los servidores se
	// releen de audiofilus.json: la interfaz puede añadir uno mientras corre.
	store := remote.NewStore(paths.Data, func() []config.Server {
		if c, err := config.Load(paths); err == nil {
			return c.Servers
		}
		return cfg.Servers
	})
	// Y la radio: cada emisora se conecta al pedirla, con sus datos de
	// data/cache/radio/stations.json (los escribe la interfaz).
	radios := radio.NewManager(radio.NewStations(paths.Data))
	var playingRadio *radio.Stream // la emisora que suena, para su título
	var radioMu sync.Mutex
	eng.OpenRemote = func(path string) (decode.Source, bool, error) {
		if radio.IsRadio(path) {
			s, ready, err := radios.Source(path)
			if ready {
				radioMu.Lock()
				playingRadio = s
				radioMu.Unlock()
			}
			if err != nil || !ready {
				return nil, false, err
			}
			return s, true, nil
		}
		local, ready, err := store.Local(path)
		if err != nil || !ready {
			return nil, false, err
		}
		src, err := decode.Open(local)
		return src, err == nil, err
	}
	eng.Prefetch = store.Prefetch
	library.RemoteTrack = func(path string) (library.Track, bool) {
		if radio.IsRadio(path) {
			st, ok := radios.Station(path)
			if !ok {
				return library.Track{}, false
			}
			// Lo que suena según la emisora; si no lo dice, su nombre.
			t := library.Track{Path: path, Title: st.Name, Artist: st.Name, Album: "Radio"}
			radioMu.Lock()
			if playingRadio != nil {
				if title := playingRadio.Title(); title != "" {
					t.Title = title
				}
			}
			radioMu.Unlock()
			return t, true
		}
		m, ok := store.Track(path)
		return library.Track{Path: path, Title: m.Title, Artist: m.Artist, Album: m.Album}, ok
	}
	eng.OnStopAfter = func() {
		log.Print("timer: stopped at the end of the song")
		control.Notify()
	}
	eng.OnTrackChanged = func(t library.Track) {
		log.Printf("now playing: %s - %s", t.Artist, t.Title)
		if mp != nil {
			mp.Update()
		}
		if tr != nil {
			tr.Update()
		}
		nowPlayingChanged(eng)
	}
	d.restore()

	go d.applySystemVolume()
	ln, err := control.Serve(paths.Socket, d.handle)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	log.Printf("listening on %s", paths.Socket)

	stopInput := d.startInput()
	stopPower := d.startPower()
	stopMedia := d.startMedia()
	stopListens := make(chan struct{})
	go d.trackListens(store, stopListens)
	defer close(stopListens)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	ticks := 0
loop:
	for {
		select {
		case <-sig:
			break loop
		case <-d.quit:
			break loop
		case <-tick.C:
			d.watchProfile()
			d.updateAwake() // por si caducó el permiso de la app
			// Por si cambió desde fuera (el menú de Knulli). En sleep no: nadie
			// lo ve, desde fuera no se puede cambiar, y en Knulli cada lectura
			// es un script de ~100 ms (lo que más gastaba en sleep tras el audio).
			if d.appOpenNow() && !d.inSleep() {
				d.refreshSystemVolume()
			}
			if ticks++; ticks%3 == 0 && eng.Playing() {
				d.saveState()
			}
		}
	}
	// La interfaz que siga abierta se cierra con el servicio (si lo cerraron
	// desde la bandeja o la línea de comandos, no lo vuelve a arrancar); en
	// un reinicio, no: se vuelve a conectar sola.
	d.mu.Lock()
	restarting := d.restarting
	d.mu.Unlock()
	if !restarting {
		control.NotifyQuit()
	}

	stopInput()
	stopPower()
	stopMedia()
	// Un cambio de volumen que aún esperaba se aplica antes de salir, para que
	// Knulli se quede con el último (el que ya se estaba aplicando termina
	// solo: knulli-audio es otro proceso).
	select {
	case v := <-d.sysSet:
		_ = d.plat.SetSystemVolume(v)
	default:
	}
	ln.Close()
	_ = os.Remove(paths.Socket)
	_ = d.setSleep(false, false)
	playing := eng.Playing()
	d.saveState()
	eng.Close()
	d.plat.Cleanup()
	if restarting {
		// Que el nuevo siga sonando si esto sonaba.
		st := config.LoadState(paths)
		st.Resume = playing
		_ = config.SaveState(paths, st)
		log.Print("restarting")
		return ErrRestart
	}
	log.Print("bye")
	return nil
}

// ErrRestart es lo que devuelve Run cuando le pidieron reiniciarse
// (restart): quien lo lanzó tiene que volver a ejecutarlo.
var ErrRestart = errors.New("restart requested")

// restore recupera la cola, la posición y las opciones de la última vez,
// en pausa.
func (d *Daemon) restore() {
	st := config.LoadState(d.paths)
	vol := d.cfg.Volume
	if st.Volume > 0 {
		vol = st.Volume
	}
	d.eng.SetVolume(vol)
	d.eng.SetShuffle(st.Shuffle)
	if r, ok := queue.ParseRepeat(st.Repeat); ok {
		d.eng.SetRepeat(r)
	}
	var dsp engine.DSP
	if len(st.DSP) > 0 {
		_ = json.Unmarshal(st.DSP, &dsp)
	}
	d.eng.SetDSP(dsp)
	if len(st.Queue) > 0 {
		d.eng.Load(st.Queue, st.Index, st.Position, st.Resume)
	}
	if st.Resume { // solo para el arranque tras un reinicio
		st.Resume = false
		_ = config.SaveState(d.paths, st)
	}
}

// saveState guarda data/state.json.
func (d *Daemon) saveState() {
	d.saveMu.Lock()
	defer d.saveMu.Unlock()
	s := d.eng.Status()
	st := config.State{
		Queue:    d.eng.Queue(),
		Index:    s.Index,
		Position: s.Position,
		Shuffle:  s.Shuffle,
		Repeat:   s.Repeat,
		Volume:   s.Volume,
	}
	st.DSP, _ = json.Marshal(d.eng.DSPConfig())
	if err := config.SaveState(d.paths, st); err != nil {
		log.Printf("saving state: %v", err)
	}
}

// updateAwake impide la suspensión mientras la app esté abierta (su permiso no
// caducó), suene o no; en segundo plano la consola se suspende como siempre.
// Mientras la app está abierta, además, los botones de volumen pasan por el
// servicio (volstep).
func (d *Daemon) updateAwake() {
	d.mu.Lock()
	open := time.Now().Before(d.awakeUntil)
	// Con la app abierta se impide siempre, suene o no: cerrar la tapa o
	// pausar en el bolsillo no debe suspender de verdad, porque entonces los
	// gatillos ya no harían nada y habría que sacar la consola para seguir.
	// La interfaz apaga la pantalla sola (su sleep) cuando no se usa.
	want := open
	changed := want != d.awakeApplied
	openChanged := open != d.appOpen
	d.awakeApplied, d.appOpen = want, open
	d.mu.Unlock()
	if changed {
		log.Printf("blocking suspend: %v", want)
		d.plat.SetAwake(want)
	}
	if openChanged {
		d.updateSpectrum()
		log.Printf("volume keys through audiofilus: %v", open)
		d.plat.SetVolumeKeys(open)
		d.refreshSystemVolume()
	}
}

// Volumen: una sola escala de 0 a 200. Hasta 100 es el volumen del sistema;
// por encima, el refuerzo de audiofilus (el volumen del engine, de 1 a 2). El
// refuerzo solo pasa de 1 con el sistema al 100 %, así nunca hay saltos.

// totalVolume es el volumen en esa escala.
func (d *Daemon) totalVolume() int {
	boost := d.eng.Status().Volume
	d.mu.Lock()
	sys := d.sysVolume
	d.mu.Unlock()
	if sys < 0 {
		return int(boost*100 + 0.5) // sin volumen del sistema: solo el de la app
	}
	if sys < 100 {
		return sys
	}
	return 100 + int((boost-1)*100+0.5)
}

// refreshSystemVolume vuelve a leer el volumen del sistema. Si alguien lo bajó
// por fuera (el menú de Knulli) con refuerzo puesto, el refuerzo vuelve a 1.
func (d *Daemon) refreshSystemVolume() {
	d.mu.Lock()
	recent := time.Since(d.sysSetAt) < 2*time.Second
	d.mu.Unlock()
	if recent {
		return // aún se está aplicando un cambio nuestro: leería el valor viejo
	}
	v := d.plat.SystemVolume()
	d.mu.Lock()
	changed := v != d.sysVolume
	d.sysVolume, d.sysKnown = v, true
	if changed {
		d.volumeSeq++
	}
	d.mu.Unlock()
	if v >= 0 && v < 100 && d.eng.Status().Volume > 1 {
		d.eng.SetVolume(1)
	}
}

// volumeStep sube o baja delta puntos en la escala de 0 a 200: el sistema
// hasta 100 y el refuerzo por encima (al bajar, primero el refuerzo).
//
// Responde enseguida: usa el último volumen del sistema conocido y lo cambia en
// segundo plano (applySystemVolume), porque knulli-audio tarda ~100 ms en cada
// lectura y en cada cambio.
func (d *Daemon) volumeStep(delta int) {
	d.mu.Lock()
	sys, known := d.sysVolume, d.sysKnown
	d.mu.Unlock()
	if !known {
		sys = d.plat.SystemVolume()
	}
	before := sys
	totalBefore := d.totalVolume()
	boost := d.eng.Status().Volume
	switch {
	case sys < 0: // no hay volumen del sistema: todo es de la app
		boost = min(2, max(0, boost+float64(delta)/100))
	case delta > 0 && sys < 100:
		sys = min(100, sys+delta)
	case delta > 0:
		boost = min(2, max(1, boost)+float64(delta)/100)
	case boost > 1:
		boost = max(1, boost+float64(delta)/100)
	default:
		sys = max(0, sys+delta)
	}
	d.eng.SetVolume(boost)
	d.mu.Lock()
	d.sysVolume, d.sysKnown = sys, true
	d.volumeSeq++
	if sys != before {
		d.sysSetAt = time.Now()
	}
	d.mu.Unlock()
	// Una vibración corta al llegar a 0, a 100 (el tope del sistema) o a 200:
	// se notan los límites sin mirar la pantalla (en el bolsillo, en sleep).
	if total := d.totalVolume(); total != totalBefore && (total == 0 || total == 100 || total == 200) {
		d.plat.Rumble(volumeMarkRumble)
	}
	if sys != before {
		// Solo vale el último: si hay uno esperando, se cambia por este.
		select {
		case <-d.sysSet:
		default:
		}
		d.sysSet <- sys
	}
	control.Notify()
}

// volumeMarkRumble es la vibración al llegar a 0, 100 o 200 % de volumen.
const volumeMarkRumble = 50 * time.Millisecond

// applySystemVolume aplica los cambios de volumen del sistema que pide
// volumeStep, uno detrás de otro y sin bloquear a quien los pidió.
func (d *Daemon) applySystemVolume() {
	for v := range d.sysSet {
		if err := d.plat.SetSystemVolume(v); err != nil {
			log.Printf("system volume %d: %v", v, err)
		}
		d.mu.Lock()
		d.sysSetAt = time.Now() // el valor ya está puesto: cuenta desde aquí
		d.mu.Unlock()
	}
}

// setAwake da o quita el permiso de la app para mantener despierta la consola.
func (d *Daemon) setAwake(on bool, lease time.Duration) {
	d.mu.Lock()
	if on {
		d.awakeUntil = time.Now().Add(lease)
	} else {
		d.awakeUntil = time.Time{}
	}
	d.mu.Unlock()
	d.updateAwake()
}

// setSleep entra o sale del sleep de la app; screenOff apaga la retroiluminación.
func (d *Daemon) setSleep(on, screenOff bool) error {
	d.mu.Lock()
	if on == d.sleeping {
		d.mu.Unlock()
		return nil
	}
	d.sleeping = on
	d.mu.Unlock()
	log.Printf("sleep: %v (backlight off: %v)", on, screenOff && on)
	defer control.Notify() // la interfaz despierta enseguida si fue por un gesto
	defer d.updateSpectrum()
	if on {
		return d.plat.EnterSleep(screenOff)
	}
	return d.plat.ExitSleep()
}

func (d *Daemon) appOpenNow() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.appOpen
}

func (d *Daemon) inSleep() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sleeping
}

// musicPaths resuelve las rutas de la petición, o las carpetas de música de la
// configuración.
func (d *Daemon) musicPaths(args []string) []string {
	if len(args) == 0 {
		args = d.cfg.MusicDirs
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = d.paths.Resolve(a)
	}
	return out
}

// handle ejecuta un comando del socket de control.
func (d *Daemon) handle(req control.Request) (any, error) {
	e := d.eng
	switch req.Cmd {
	case "status":
	case "play":
		if len(req.Args) == 0 && e.Status().Count > 0 {
			e.Play()
			break
		}
		files, err := library.Expand(d.musicPaths(req.Args))
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, errors.New("no playable files found")
		}
		e.Load(files, 0, 0, true)
	case "load":
		// load <inicio> <archivos...>: la interfaz manda la cola ya armada (un
		// álbum, la lista de canciones) y en qué canción empezar.
		if len(req.Args) < 2 {
			return nil, errors.New("usage: load <start> <files...>")
		}
		start, err := strconv.Atoi(req.Args[0])
		if err != nil {
			return nil, errors.New("usage: load <start> <files...>")
		}
		e.Load(req.Args[1:], start, 0, true)
	case "playnext":
		// playnext <archivos...>: suenan justo después de la canción actual, en orden.
		if len(req.Args) == 0 {
			return nil, errors.New("usage: playnext <files...>")
		}
		for i := len(req.Args) - 1; i >= 0; i-- {
			e.PlayNext(req.Args[i])
		}
	case "add":
		files, err := library.Expand(d.musicPaths(req.Args))
		if err != nil {
			return nil, err
		}
		e.Append(files)
	case "toggle":
		e.Toggle()
	case "pause":
		e.Pause()
	case "resume":
		e.Play()
	case "next":
		e.Next()
	case "prev":
		e.Prev()
	case "jump":
		i, err := intArg(req.Args)
		if err != nil {
			return nil, err
		}
		e.Jump(i)
	case "scratch":
		// La mano en el disco: 1, un vaivén largo; -1, dos cortos.
		dir := 1
		if len(req.Args) > 0 && strings.HasPrefix(req.Args[0], "-") {
			dir = -1
		}
		e.Scratch(dir)
	case "seek":
		if len(req.Args) != 1 {
			return nil, errors.New("usage: seek <seconds>|+<seconds>|-<seconds>")
		}
		v, err := strconv.ParseFloat(req.Args[0], 64)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(req.Args[0], "+") || strings.HasPrefix(req.Args[0], "-") {
			e.SeekBy(v)
		} else {
			e.SeekTo(v)
		}
		nowPlayingChanged(e)
	case "shuffle":
		on, err := onOff(req.Args, !e.Status().Shuffle)
		if err != nil {
			return nil, err
		}
		e.SetShuffle(on)
	case "timer":
		if err := d.setTimer(req.Args); err != nil {
			return nil, err
		}
	case "replaygain", "preamp", "eq", "crossfade":
		dsp, err := dspCommand(e.DSPConfig(), req.Cmd, req.Args)
		if err != nil {
			return nil, err
		}
		e.SetDSP(dsp)
		go d.saveState()
	case "repeat":
		if len(req.Args) != 1 {
			return nil, errors.New("usage: repeat off|all|one")
		}
		r, ok := queue.ParseRepeat(req.Args[0])
		if !ok {
			return nil, errors.New("usage: repeat off|all|one")
		}
		e.SetRepeat(r)
	case "volume":
		if len(req.Args) != 1 {
			return nil, errors.New("usage: volume <0..2>")
		}
		v, err := strconv.ParseFloat(req.Args[0], 64)
		if err != nil {
			return nil, err
		}
		e.SetVolume(v)
	case "queue":
		return e.Queue(), nil
	case "sleep":
		// sleep on|off|toggle [oled]: con "oled" no se apaga la retroiluminación
		// (la interfaz pinta negro, que en una OLED ya es apagado).
		args := req.Args
		screenOff := true
		if len(args) > 1 && args[1] == "oled" {
			screenOff = false
			args = args[:1]
		}
		on, err := onOff(args, !d.inSleep())
		if err != nil {
			return nil, err
		}
		if err := d.setSleep(on, screenOff); err != nil {
			return nil, err
		}
	case "attach":
		// Una interfaz se conecta y mantiene la conexión abierta mientras vive.
		d.mu.Lock()
		d.attached++
		d.background = false
		d.mu.Unlock()
		return nil, nil
	case "background":
		// La interfaz pide seguir sonando cuando se cierre (al salir, o desde
		// que se conecta si así está en Ajustes); "background off" lo retira.
		on := len(req.Args) == 0 || req.Args[0] != "off"
		d.mu.Lock()
		d.background = on
		d.mu.Unlock()
		if on {
			log.Print("UI going to background: music keeps playing")
		}
	case "detached":
		// Se cerró la conexión de una interfaz (al salir o porque murió). Si no
		// pidió segundo plano, la música no se queda sonando sola: se pausa,
		// se guarda dónde iba y el servicio se cierra.
		d.mu.Lock()
		d.attached--
		stop := d.attached <= 0 && !d.background
		d.mu.Unlock()
		if stop {
			log.Print("UI closed without going to background: stopping")
			e.Pause()
			d.quitOnce.Do(func() { close(d.quit) })
		}
		return nil, nil
	case "volstep":
		// volstep <+N|-N>: los botones de volumen (y + y - de la interfaz).
		if len(req.Args) != 1 {
			return nil, errors.New("usage: volstep +N|-N")
		}
		n, err := strconv.Atoi(req.Args[0])
		if err != nil {
			return nil, errors.New("usage: volstep +N|-N")
		}
		d.volumeStep(n)
	case "awake":
		if len(req.Args) == 0 || (req.Args[0] != "on" && req.Args[0] != "off") {
			return nil, errors.New("usage: awake on [seconds] | awake off")
		}
		lease := defaultLease
		if len(req.Args) > 1 {
			s, err := strconv.Atoi(req.Args[1])
			if err != nil || s <= 0 {
				return nil, errors.New("usage: awake on [seconds] | awake off")
			}
			lease = time.Duration(s) * time.Second
		}
		d.setAwake(req.Args[0] == "on", lease)
	case "quit":
		d.quitOnce.Do(func() { close(d.quit) })
		return nil, nil
	case "restart":
		// Para aplicar lo que solo se lee al arrancar (la frecuencia de
		// salida): Run termina con ErrRestart, sin cerrar la interfaz, y quien
		// lo lanzó vuelve a ejecutarlo; si sonaba, sigue sonando.
		d.mu.Lock()
		d.restarting = true
		d.mu.Unlock()
		d.quitOnce.Do(func() { close(d.quit) })
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown command %q", req.Cmd)
	}
	return d.status(), nil
}

// OnEngine, si está, recibe el engine en cuanto se crea: en el móvil, la
// interfaz (en el mismo proceso) suena por su salida de audio (mobile/).
var OnEngine func(*engine.Engine)

// NowPlayingHook, si está, se llama (en su goroutine) cada vez que cambia
// lo que suena: otra canción, pausa o play, un salto. Lo usa el móvil para la
// pantalla bloqueada y el Centro de control (mobile/).
var NowPlayingHook func(engine.Status)

// nowPlayingChanged avisa a NowPlayingHook.
func nowPlayingChanged(e *engine.Engine) {
	if h := NowPlayingHook; h != nil {
		// Un salto o un cambio de canción se aplican al vaciar el búfer, un
		// momento después: el estado, ya con ellos.
		go func() {
			time.Sleep(150 * time.Millisecond)
			h(e.Status())
		}()
	}
}

// FullStatus es el estado del engine más el del daemon.
type FullStatus struct {
	engine.Status
	Sleep    bool   `json:"sleep"`
	Awake    bool   `json:"awake"` // impidiendo la suspensión ahora mismo
	Platform string `json:"platform"`
	Pad      string `json:"pad"`
	// VolumeTotal es el volumen en la escala de 0 a 200 (sistema hasta 100,
	// refuerzo de la app por encima); VolumeSeq sube con cada cambio.
	VolumeTotal int `json:"volume_total"`
	VolumeSeq   int `json:"volume_seq"`
	// PowerSeq sube con cada toque del botón de encendido con la app abierta y
	// despierta: la interfaz entra en sleep (con su fundido).
	PowerSeq int `json:"power_seq"`
	// Lid: la tapa está cerrada (el sensor de la consola, en Knulli y muOS).
	Lid bool `json:"lid,omitempty"`
	// Output es por dónde sale el sonido: speaker, wired, bluetooth o "" si
	// no se sabe.
	Output string `json:"output"`
	// Meter son el nivel y los graves de lo último que se oye y lo que se va a
	// oír (engine/meter.go): para el visualizador y el tetris.
	Meter []engine.MeterPoint `json:"meter,omitempty"`
	// TimerLeft son los segundos que le quedan al temporizador (0: no hay;
	// el de "al acabar la canción" es StopAfter).
	TimerLeft float64 `json:"timer_left,omitempty"`
}

// status devuelve el estado completo.
func (d *Daemon) status() FullStatus {
	s := FullStatus{Status: d.eng.Status(), Platform: d.plat.Name(), VolumeTotal: d.totalVolume()}
	d.mu.Lock()
	s.Sleep, s.Awake, s.Pad, s.VolumeSeq, s.PowerSeq = d.sleeping, d.awakeApplied, d.pad, d.volumeSeq, d.powerSeq
	if !d.timerAt.IsZero() {
		s.TimerLeft = math.Max(0, time.Until(d.timerAt).Seconds())
	}
	d.mu.Unlock()
	s.Lid = d.plat.LidClosed()
	if !s.Sleep {
		s.Output = d.audioOutput() // en sleep no se ve: no se gasta en pactl
		if s.State == "playing" {
			s.Meter = d.eng.Meter()
		}
	}
	return s
}

// outputEvery es cada cuánto se vuelve a mirar la salida de audio. Solo se
// mira cuando alguien pide el estado (la interfaz abierta), así que en segundo
// plano no cuesta nada.
const outputEvery = 2 * time.Second

// audioOutput es la salida de audio, mirada como mucho cada outputEvery.
func (d *Daemon) audioOutput() string {
	d.mu.Lock()
	if time.Since(d.outputAt) < outputEvery {
		defer d.mu.Unlock()
		return d.output
	}
	d.outputAt = time.Now() // otro estado a la vez no la vuelve a mirar
	d.mu.Unlock()
	out := platform.AudioOutput()
	d.mu.Lock()
	if out != d.output {
		log.Printf("audio output: %q", out)
	}
	d.output = out
	d.mu.Unlock()
	return out
}

// intArg lee un único argumento entero.
func intArg(args []string) (int, error) {
	if len(args) != 1 {
		return 0, errors.New("expected one number")
	}
	return strconv.Atoi(args[0])
}

// onOff interpreta on/off/toggle; sin argumento o con toggle devuelve toggled.
func onOff(args []string, toggled bool) (bool, error) {
	if len(args) == 0 {
		return toggled, nil
	}
	switch args[0] {
	case "on":
		return true, nil
	case "off":
		return false, nil
	case "toggle":
		return toggled, nil
	}
	return false, errors.New("expected on, off or toggle")
}

// updateSpectrum calcula el espectro solo con la app abierta y despierta: es
// para el visualizador, y en segundo plano o en sleep nadie lo ve.
func (d *Daemon) updateSpectrum() {
	d.mu.Lock()
	on := d.appOpen && !d.sleeping
	d.mu.Unlock()
	d.eng.SetSpectrum(on)
}

// watchProfile graba un perfil de CPU de 20 s en data/daemon-cpu.prof si
// alguien creó data/daemon-cpuprofile: para ver en la consola en qué se va la
// CPU (como el de la interfaz, que en sleep no corre y no sirve para esto).
func (d *Daemon) watchProfile() {
	req := filepath.Join(d.paths.Data, "daemon-cpuprofile")
	if _, err := os.Stat(req); err != nil {
		return
	}
	_ = os.Remove(req)
	f, err := os.Create(filepath.Join(d.paths.Data, "daemon-cpu.prof"))
	if err != nil {
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return
	}
	log.Printf("cpu profile: recording 20 s")
	time.AfterFunc(20*time.Second, func() {
		pprof.StopCPUProfile()
		f.Close()
		log.Printf("cpu profile: written")
	})
}
