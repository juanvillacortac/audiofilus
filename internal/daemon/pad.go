package daemon

import (
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/sfx"

	"github.com/juanvillacortac/audiofilus/internal/input"
)

// armedRumble es la vibración que confirma que el atajo de FN quedó armado.
const armedRumble = 60 * time.Millisecond

// startInput lee el mando en segundo plano: los atajos de FN siempre, y los
// gestos de doble toque mientras dura el sleep de la app. Devuelve la función
// que lo para.
func (d *Daemon) startInput() (stop func()) {
	var mu sync.Mutex
	var cur *input.Reader
	done := make(chan struct{})
	go func() {
		failures := 0
		for {
			select {
			case <-done:
				return
			default:
			}
			res, err := input.Resolve(d.cfg.Input.Device)
			var r *input.Reader
			if err == nil {
				r, err = input.Open(res)
			}
			if err != nil {
				if failures == 0 {
					log.Printf("no gamepad (retrying every 30 s): %v", err)
				}
				failures++
				select {
				case <-done:
					return
				case <-time.After(30 * time.Second):
				}
				continue
			}
			failures = 0
			desc := res.Name + " (" + res.Path + ", mapping " + res.Source + ")"
			log.Printf("gamepad: %s: %s", desc, res.Layout.Describe())
			d.mu.Lock()
			d.pad = desc
			d.mu.Unlock()
			mu.Lock()
			cur = r
			mu.Unlock()
			d.readPad(r)
			r.Close()
			select {
			case <-done:
				return
			case <-time.After(2 * time.Second): // se desconectó: se vuelve a buscar
			}
		}
	}()
	return func() {
		close(done)
		mu.Lock()
		if cur != nil {
			cur.Close()
		}
		mu.Unlock()
	}
}

// readPad procesa los botones hasta que falle la lectura.
func (d *Daemon) readPad(r *input.Reader) {
	s, h := d.cfg.Sleep, d.cfg.Hotkeys
	gestures := input.NewDetector(ms(s.DoubleTapMS), ms(s.ChordMS))
	combo := input.NewCombo(h.Taps, ms(h.TapMS), ms(h.WindowMS))
	for {
		ev, err := r.Read()
		if err != nil {
			return
		}
		if h.Enabled {
			switch a := combo.Event(ev); a {
			case input.NoAction:
			case input.Armed:
				log.Print("hotkey armed (FN)")
				d.plat.Rumble(armedRumble)
			default:
				log.Printf("hotkey: %v", a)
				d.act(a)
			}
		}
		if d.inSleep() {
			if a := gestures.Event(ev); a != input.NoAction {
				log.Printf("gesture: %v", a)
				d.act(a)
			}
		}
	}
}

// act ejecuta una acción de un gesto o de un atajo, con su vibración y su
// sonido (los de la interfaz los pone ella; estos son los que no pasan por
// ella: el atajo de FN y los gestos del sleep).
func (d *Daemon) act(a input.Action) {
	defer d.actSound(a)
	rumble := ms(d.cfg.Sleep.RumbleMS)
	seek := float64(d.cfg.Hotkeys.SeekSec)
	switch a {
	case input.Prev:
		d.eng.Prev()
	case input.Next:
		d.eng.Next()
	case input.SeekBack:
		d.eng.SeekBy(-seek)
	case input.SeekForward:
		d.eng.SeekBy(seek)
	case input.TogglePause:
		if d.eng.Playing() {
			d.eng.Pause()
			rumble = ms(d.cfg.Sleep.PauseRumbleMS)
		} else {
			d.eng.Play()
		}
	case input.Random:
		d.playRandom()
	case input.Wake:
		_ = d.setSleep(false, false)
		return
	}
	d.plat.Rumble(rumble)
}

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// playRandom pone una canción al azar de toda la biblioteca (las carpetas de
// audiofilus.json), con el resto detrás; el aleatorio queda como estaba.
func (d *Daemon) playRandom() {
	cfg, err := config.Load(d.paths) // la carpeta pudo cambiar desde la interfaz
	if err != nil {
		cfg = d.cfg
	}
	tracks, err := library.Expand(cfg.MusicDirs)
	if err != nil || len(tracks) == 0 {
		log.Printf("random song: no music (%v)", err)
		return
	}
	i := rand.IntN(len(tracks))
	log.Printf("random song: %s", tracks[i])
	d.eng.Load(tracks, i, 0, true)
}

// powerShort es lo que puede durar un toque del botón de encendido: más es
// mantenerlo, que en Knulli apaga.
const powerShort = 1500 * time.Millisecond

// startPower lee el botón de encendido (en las portátiles, Knulli y muOS; en
// el PC la tecla de encendido es del escritorio). Devuelve la función que lo
// para.
func (d *Daemon) startPower() (stop func()) {
	if d.plat.Name() == "desktop" {
		return func() {}
	}
	var watchers []*input.PowerWatcher
	for _, path := range input.PowerButtons() {
		w, err := input.WatchPower(path)
		if err != nil {
			log.Printf("power button %s: %v", path, err)
			continue
		}
		log.Printf("power button: %s", path)
		watchers = append(watchers, w)
		go w.Run(d.powerPressed)
	}
	return func() {
		for _, w := range watchers {
			w.Close()
		}
	}
}

// powerPressed atiende un toque del botón de encendido con la app abierta:
// despierta, entra en sleep pidiéndoselo a la interfaz (para que haga su
// fundido); en sleep, despierta, salvo con la tapa cerrada (en el bolsillo se
// pulsa sin querer). Con la app cerrada no hace nada: Knulli suspende.
func (d *Daemon) powerPressed(held time.Duration) {
	if held > powerShort {
		return
	}
	d.mu.Lock()
	open, sleeping := d.appOpen, d.sleeping
	d.mu.Unlock()
	switch {
	case !open:
	case sleeping && d.plat.LidClosed():
		log.Print("power button: lid closed, staying asleep")
	case sleeping:
		log.Print("power button: wake")
		_ = d.setSleep(false, false)
	default:
		log.Print("power button: sleep")
		d.mu.Lock()
		d.powerSeq++
		d.mu.Unlock()
		control.Notify()
	}
}

// actSound suena el efecto de una acción, con el nivel de Sonidos de
// audiofilus.json (se relee: la interfaz puede cambiarlo).
func (d *Daemon) actSound(a input.Action) {
	name := ""
	switch a {
	case input.Prev, input.SeekBack:
		name = sfx.Prev
	case input.Next, input.SeekForward:
		name = sfx.Next
	case input.TogglePause:
		name = sfx.Pause
		if d.eng.Playing() {
			name = sfx.Play
		}
	case input.Random:
		name = sfx.Random
	}
	if name == "" {
		return
	}
	level := d.cfg.UI.Sounds
	if c, err := config.Load(d.paths); err == nil {
		level = c.UI.Sounds
	}
	if g := sfx.Gain(level); g > 0 {
		s := sfx.Samples(name)
		d.eng.PlaySFX(sfx.Bytes(name, g), time.Duration(len(s)/2)*time.Second/sfx.Rate)
	}
}

// startMedia lee los botones multimedia de los audífonos (bluetooth o de
// cable) en las portátiles; en el PC eso llega por MPRIS. Busca dispositivos
// al arrancar y cada vez que aparece uno en /dev/input (inotify: los de
// bluetooth aparecen al conectarse); entre medias no gasta nada, todo está
// bloqueado esperando. Devuelve la función que lo para.
func (d *Daemon) startMedia() (stop func()) {
	if d.plat.Name() == "desktop" {
		return func() {}
	}
	done := make(chan struct{})
	var mu sync.Mutex
	open := map[string]*input.MediaReader{}
	hook := input.NewHookCounter(400 * time.Millisecond)
	scan := func() {
		for _, path := range input.MediaDevices() {
			mu.Lock()
			_, ok := open[path]
			mu.Unlock()
			if ok {
				continue
			}
			r, err := input.OpenMedia(path)
			if err != nil {
				continue
			}
			log.Printf("media keys: %s", path)
			mu.Lock()
			open[path] = r
			mu.Unlock()
			go func() {
				r.Run(func(key uint16, at time.Time) { d.mediaKey(key, at, hook) })
				r.Close()
				mu.Lock()
				delete(open, path)
				mu.Unlock()
			}()
		}
	}
	scan()
	go input.WaitNewDevices(done, scan)
	return func() {
		close(done)
		mu.Lock()
		for _, r := range open {
			r.Close()
		}
		mu.Unlock()
	}
}

// mediaKey hace lo que pide una tecla multimedia.
func (d *Daemon) mediaKey(key uint16, at time.Time, hook *input.HookCounter) {
	log.Printf("media key: %d", key)
	switch key {
	case input.KeyMedia:
		// El botón único de los audífonos de cable: una pausa, dos pasa a la
		// siguiente, tres a la anterior.
		hook.Press(at, func(n int) {
			switch n {
			case 1:
				d.act(input.TogglePause)
			case 2:
				d.act(input.Next)
			default:
				d.act(input.Prev)
			}
		})
	case input.KeyPlayPause:
		d.act(input.TogglePause)
	case input.KeyPlayCD:
		if !d.eng.Playing() {
			d.act(input.TogglePause)
		}
	case input.KeyPauseCD, input.KeyStopCD, input.KeyStop:
		if d.eng.Playing() {
			d.act(input.TogglePause)
		}
	case input.KeyNextSong:
		d.act(input.Next)
	case input.KeyPreviousSong:
		d.act(input.Prev)
	case input.KeyFastForward:
		d.act(input.SeekForward)
	case input.KeyRewind:
		d.act(input.SeekBack)
	}
}
