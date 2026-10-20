package daemon

import (
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/dlna"
	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/library"
)

// DLNA en el servicio (internal/dlna): el servidor de la biblioteca y el
// renderer, según audiofilus.json ("dlna": {"server", "renderer", "name"}).
// Apagado por defecto. La interfaz cambia la configuración y manda
// "dlna apply"; desde la línea de comandos, "dlna server on" y compañía la
// cambian y la aplican a la vez.

// installDLNA engancha el renderer al motor (abrir lo que mandan los mandos,
// sus etiquetas, avisar de los cambios) y arranca lo que esté encendido. Va
// después de poner los ganchos del resto (Run), a los que envuelve.
func (d *Daemon) installDLNA() {
	open := d.eng.OpenRemote
	d.eng.OpenRemote = func(path string) (decode.Source, bool, error) {
		if dlna.IsRendererPath(path) {
			if s := d.dlnaService(); s != nil {
				return s.Open(path)
			}
			return nil, false, errors.New("dlna renderer is off")
		}
		return open(path)
	}
	track := library.RemoteTrack
	library.RemoteTrack = func(path string) (library.Track, bool) {
		if dlna.IsRendererPath(path) {
			if s := d.dlnaService(); s != nil {
				if m, ok := s.Track(path); ok {
					return library.Track{Path: path, Title: m.Title, Artist: m.Artist, Album: m.Album}, true
				}
			}
			return library.Track{}, false
		}
		if track != nil {
			return track(path)
		}
		return library.Track{}, false
	}
	playing := d.eng.OnPlayingChanged
	d.eng.OnPlayingChanged = func(p bool) {
		if playing != nil {
			playing(p)
		}
		if s := d.dlnaService(); s != nil {
			s.Changed()
		}
	}
	changed := d.eng.OnTrackChanged
	d.eng.OnTrackChanged = func(t library.Track) {
		if changed != nil {
			changed(t)
		}
		if s := d.dlnaService(); s != nil {
			s.Changed()
		}
	}
	d.applyDLNA(d.cfg.DLNA)
}

// dlnaService es lo que está encendido (nil si nada).
func (d *Daemon) dlnaService() *dlna.Service {
	d.dlnaMu.Lock()
	defer d.dlnaMu.Unlock()
	return d.dlna
}

// applyDLNA enciende, apaga o reinicia DLNA según c.
func (d *Daemon) applyDLNA(c config.DLNA) {
	d.dlnaMu.Lock()
	defer d.dlnaMu.Unlock()
	if d.dlna != nil {
		if d.dlnaCfg == c {
			return
		}
		d.dlna.Close()
		d.dlna = nil
	}
	d.dlnaCfg = c
	if !c.Server && !c.Renderer {
		return
	}
	s, err := dlna.Start(dlna.Options{Name: c.Name, Server: c.Server, Renderer: c.Renderer,
		DataDir: d.paths.Data, Player: enginePlayer{d.eng}})
	if err != nil {
		log.Printf("dlna: %v", err)
		d.dlnaErr = err.Error()
		return
	}
	d.dlnaErr = ""
	d.dlna = s
}

// stopDLNA apaga DLNA (al cerrar el servicio).
func (d *Daemon) stopDLNA() {
	d.dlnaMu.Lock()
	defer d.dlnaMu.Unlock()
	if d.dlna != nil {
		d.dlna.Close()
		d.dlna = nil
	}
}

// DLNAStatus es la respuesta de "dlna".
type DLNAStatus struct {
	dlna.Status
	Error string `json:"error,omitempty"`
}

// dlnaCommand atiende "dlna": sin argumentos (o "status"), el estado;
// "apply", relee audiofilus.json; "server|renderer on|off" y "name <nombre>"
// lo cambian, lo guardan y lo aplican.
func (d *Daemon) dlnaCommand(args []string) (any, error) {
	if len(args) > 0 && args[0] != "status" {
		cfg, err := config.Load(d.paths)
		if err != nil {
			return nil, err
		}
		switch args[0] {
		case "apply":
		case "server", "renderer":
			on, err := onOff(args[1:], true)
			if err != nil {
				return nil, err
			}
			if args[0] == "server" {
				cfg.DLNA.Server = on
			} else {
				cfg.DLNA.Renderer = on
			}
			if err := config.Save(d.paths, cfg); err != nil {
				return nil, err
			}
		case "name":
			if len(args) < 2 {
				return nil, errors.New("usage: dlna name <name>")
			}
			cfg.DLNA.Name = fmt.Sprint(args[1])
			if err := config.Save(d.paths, cfg); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("usage: dlna [status|apply|server on|off|renderer on|off|name <name>]")
		}
		d.applyDLNA(cfg.DLNA)
	}
	d.dlnaMu.Lock()
	defer d.dlnaMu.Unlock()
	st := DLNAStatus{Error: d.dlnaErr}
	st.Name = d.dlnaCfg.Name
	if st.Name == "" {
		st.Name = dlna.DefaultName()
	}
	if d.dlna != nil {
		st.Status = d.dlna.Status()
	}
	return st, nil
}

// enginePlayer es el motor visto por el renderer.
type enginePlayer struct{ e *engine.Engine }

func (p enginePlayer) Load(path string, play bool) { p.e.Load([]string{path}, 0, 0, play) }
func (p enginePlayer) Append(path string)          { p.e.Append([]string{path}) }
func (p enginePlayer) Play()                       { p.e.Play() }
func (p enginePlayer) Pause()                      { p.e.Pause() }
func (p enginePlayer) Stop()                       { p.e.Pause(); p.e.SeekTo(0) }
func (p enginePlayer) Seek(s float64)              { p.e.SeekTo(s) }
func (p enginePlayer) Next()                       { p.e.Next() }
func (p enginePlayer) Prev()                       { p.e.Prev() }

func (p enginePlayer) Status() dlna.PlayerStatus {
	st := p.e.Status()
	return dlna.PlayerStatus{Path: st.Track.Path, Playing: st.State == "playing", Paused: st.State == "paused",
		Loading: st.Loading, Position: st.Position, Duration: st.Duration}
}

// Volume es el volumen del motor en 0..100 (por encima de 1, con el
// refuerzo, cuenta como 100).
func (p enginePlayer) Volume() int {
	return int(math.Round(math.Min(p.e.Status().Volume, 1) * 100))
}

func (p enginePlayer) SetVolume(v int) { p.e.SetVolume(float64(v) / 100) }
