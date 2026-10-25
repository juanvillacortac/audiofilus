package ui

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"

	"github.com/juanvillacortac/audiofilus/internal/sfx"
)

// Los sonidos de la interfaz (internal/sfx): mover, aceptar, volver, abrir y
// cerrar menús, cambiar de pantalla, escribir, y los de la música cuando se
// manejan desde aquí (pausa, siguiente, anterior, aleatorio, al azar). La
// interfaz tiene su propia salida de audio, con un búfer corto (~50 ms) para
// que el sonido vaya con el botón (la del servicio tiene 250 ms); se abre con
// el primer sonido y se suspende tras 2 s sin ninguno, así en reposo no
// gasta. Los de los atajos y gestos que no pasan por aquí los pone el
// servicio. En cada fotograma suena como mucho uno, el más importante
// (soundAt): así pulsar A en el reproductor suena a pausa y no además a
// "aceptar".

// soundState es la salida de audio de la interfaz y el sonido de este
// fotograma.
type soundState struct {
	once    sync.Once
	reqs    chan soundReq // los pedidos a la goroutine del audio
	pending string        // el sonido de este fotograma
	prio    int
	// Para los sonidos que salen de cambios de estado.
	hadMenu, hadDock, hadPrompt bool
}

// sound pide name para este fotograma, si es más importante que el que ya
// hay (prio: 3 música, 2 abrir/cerrar, 1 aceptar/volver, 0 mover).
func (a *App) sound(name string, prio int) {
	if a.snd.pending == "" || prio > a.snd.prio {
		a.snd.pending, a.snd.prio = name, prio
	}
}

// soundNone es un sonido que no suena: pedirlo calla los de los botones de
// ese fotograma.
const soundNone = "-"

// soundFromSend pone el sonido de un comando de la música que se manda al
// servicio desde aquí.
func (a *App) soundFromSend(cmd string, args []string) {
	switch cmd {
	case "toggle":
		if a.isPlaying {
			a.sound(sfx.Pause, 3)
		} else {
			a.sound(sfx.Play, 3)
		}
	case "next":
		a.sound(sfx.Next, 3)
	case "prev":
		a.sound(sfx.Prev, 3)
	case "seek":
		if len(args) > 0 && len(args[0]) > 0 && args[0][0] == '-' {
			a.sound(sfx.Prev, 3)
		} else {
			a.sound(sfx.Next, 3)
		}
	case "shuffle":
		if len(args) > 0 && args[0] == "on" {
			a.sound(sfx.ShuffleOn, 3)
		} else {
			a.sound(sfx.Ordered, 3)
		}
	case "load":
		a.sound(sfx.Confirm, 2)
	}
}

// soundFromInput pone, si no hay otro, el sonido de los botones de este
// fotograma y de lo que cambió (menús, barra, escritura). Va al final de
// Update.
func (a *App) soundFromInput() {
	s, in := &a.snd, &a.input
	menu, dock, prompt := a.menu != nil, a.dock.open, a.prompt != nil
	switch {
	case menu && !s.hadMenu, dock && !s.hadDock:
		a.sound(sfx.Open, 2)
	case !menu && s.hadMenu, !dock && s.hadDock:
		a.sound(sfx.Close, 2)
	}
	s.hadMenu, s.hadDock, s.hadPrompt = menu, dock, prompt
	// En los juegos, sin menú, los botones son del juego: sin sonidos.
	inGame := (a.screen == Blocks || a.screen == Rhythm || a.screen == Crypt || a.screen == Tracker || a.screen == Quiz || a.screen == Snake || a.screen == Shooter || a.screen == Duel || a.screen == Runner) && !menu && !dock && !prompt
	typing := prompt || a.searchTyping()
	switch {
	case inGame || a.sleeping || a.guideAt > 0:
	case typing && (in.Pressed(ActConfirm) || in.Pressed(ActShuffle)):
		a.sound(sfx.Type, 1)
	case in.Pressed(ActConfirm):
		a.sound(sfx.Confirm, 1)
	case in.Pressed(ActBack):
		a.sound(sfx.Back, 1)
	case in.Repeat(ActUp), in.Repeat(ActDown), in.Repeat(ActLeft), in.Repeat(ActRight):
		a.sound(sfx.Move, 0)
	}
	if s.pending != "" && s.pending != soundNone {
		a.playSound(s.pending)
	}
	s.pending, s.prio = "", -1
}

// musicSounds son los sonidos de la música (pausa, siguiente...), con su
// propio volumen; los demás son los de la interfaz.
var musicSounds = map[string]bool{sfx.Play: true, sfx.Pause: true, sfx.Next: true, sfx.Prev: true,
	sfx.ShuffleOn: true, sfx.Ordered: true, sfx.Random: true,
	// Los de Cripta: son del juego, con el volumen de los de la música.
	"cr_step": true, "cr_bump": true, "cr_hit": true, "cr_kill": true, "cr_coin": true,
	"cr_hurt": true, "cr_dead": true, "cr_miss": true, "cr_stairs": true,
	"gb_ding": true, "scratch": true}

// SharedAudio, si está, da la salida de audio que la interfaz debe usar en
// vez de crear la suya, y lo que hay que hacer en reposo (dormirla si no
// suena música). Esperar a que exista es cosa suya. Lo pone el móvil, donde
// el servicio va en el mismo proceso (ver mobile).
var SharedAudio func() (*oto.Context, func())

// soundReq es un pedido para la goroutine del audio: tocar name con gain, o
// cortar el jingle del splash (stop).
type soundReq struct {
	name   string
	gain   float64
	stop   bool
	stream io.Reader // un flujo continuo (el tracker): suena hasta streamOff
	off    bool      // para el flujo
}

// playSound pide que suene name, con el volumen de Ajustes. No espera nunca:
// lo hace la goroutine del audio (soundLoop). Crear la salida de audio puede
// tardar segundos (PulseAudio), y hacerlo desde aquí congelaba la interfaz.
func (a *App) playSound(name string) {
	level := a.cfg.UI.UISounds
	if musicSounds[name] {
		level = a.cfg.UI.Sounds
	}
	g := sfx.Gain(level)
	if g == 0 || a.offline {
		return
	}
	a.startSounds()
	select {
	case a.snd.reqs <- soundReq{name: name, gain: g}:
	default: // si se acumulan, se pierde alguno
	}
}

// stopSplashSound corta el jingle del splash (al saltarlo).
func (a *App) stopSplashSound() {
	if a.snd.reqs != nil {
		select {
		case a.snd.reqs <- soundReq{stop: true}:
		default:
		}
	}
}

// startSounds arranca la goroutine del audio la primera vez.
func (a *App) startSounds() {
	a.snd.once.Do(func() {
		a.snd.reqs = make(chan soundReq, 8)
		go a.soundLoop()
	})
}

// soundLoop es la goroutine del audio: crea la salida (búfer de 50 ms), toca
// lo que se pide y la suspende tras 2 s sin sonidos.
func (a *App) soundLoop() {
	var ctx *oto.Context
	idleFn := func() { _ = ctx.Suspend() } // en reposo no gasta
	if SharedAudio != nil {
		// En el móvil, la salida del servicio (oto admite un solo contexto
		// por proceso); dormirla es cosa suya: solo si no suena música.
		ctx, idleFn = SharedAudio()
		if ctx == nil {
			return
		}
	} else {
		usePulseSocket()
		c, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate: sfx.Rate, ChannelCount: 2, Format: oto.FormatFloat32LE,
			BufferSize: 50 * time.Millisecond, ApplicationName: "Audiofilus",
		})
		if err != nil {
			return
		}
		<-ready
		ctx = c
	}
	var splash, stream *oto.Player
	var streamSrc io.Reader
	idle := time.NewTimer(time.Hour)
	for {
		select {
		case r := <-a.snd.reqs:
			if r.stream != nil {
				// El tracker: un reproductor que lee del sintetizador, con un
				// búfer corto para que lo que se edita suene enseguida.
				_ = ctx.Resume()
				if r.stream != streamSrc { // otro reproductor (otra canción)
					if stream != nil {
						_ = stream.Close()
					}
					stream, streamSrc = ctx.NewPlayer(r.stream), r.stream
					stream.SetBufferSize(sfx.Rate * 8 / 20) // 50 ms
				}
				stream.Play()
				idle.Stop()
				continue
			}
			if r.off {
				if stream != nil {
					stream.Pause()
				}
				idle.Reset(2 * time.Second)
				continue
			}
			if r.stop {
				if splash != nil {
					splash.Pause()
				}
				continue
			}
			_ = ctx.Resume()
			p := ctx.NewPlayer(bytes.NewReader(sfx.Bytes(r.name, r.gain)))
			p.Play()
			if r.name == sfx.Splash {
				splash = p
			}
			d := time.Duration(len(sfx.Samples(r.name))/2) * time.Second / sfx.Rate
			time.AfterFunc(d+200*time.Millisecond, func() { _ = p.Close() })
			if stream == nil || !stream.IsPlaying() {
				idle.Reset(d + 2*time.Second)
			}
		case <-idle.C:
			if stream == nil || !stream.IsPlaying() {
				idleFn()
			}
		}
	}
}

// usePulseSocket hace que la salida encuentre PulseAudio en Knulli, que lo
// corre en modo sistema (como el servicio, ver engine).
func usePulseSocket() {
	// En muOS el audio es PipeWire (por ALSA), y su socket está en /run.
	if _, err := os.Stat("/run/muos"); err == nil && os.Getenv("XDG_RUNTIME_DIR") == "" {
		os.Setenv("XDG_RUNTIME_DIR", "/run")
	}
	if os.Getenv("PULSE_SERVER") != "" {
		return
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		if _, err := os.Stat(dir + "/pulse/native"); err == nil {
			return
		}
	}
	for _, p := range []string{"/var/run/pulse/native", "/run/pulse/native"} {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("PULSE_SERVER", "unix:"+p)
			return
		}
	}
}

// playStream hace sonar r (el tracker) por la salida de la interfaz, hasta
// stopStream. No espera.
func (a *App) playStream(r io.Reader) {
	a.startSounds()
	a.snd.reqs <- soundReq{stream: r}
}

// stopStream para el flujo.
func (a *App) stopStream() {
	if a.snd.reqs != nil {
		a.snd.reqs <- soundReq{off: true}
	}
}
