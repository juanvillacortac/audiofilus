package daemon

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/dlna"
	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/mdns"
	"github.com/juanvillacortac/audiofilus/internal/mpd"
	"github.com/juanvillacortac/audiofilus/internal/queue"
)

// MPD en el servicio (internal/mpd): el servidor del protocolo de MPD,
// anunciado por mDNS como _mpd._tcp, según audiofilus.json ("mpd": {"on",
// "port", "password", "name"}). Apagado por defecto. La interfaz cambia la
// configuración y manda "mpd apply"; desde la línea de comandos, "mpd on" y
// compañía la cambian y la aplican a la vez.

// mpdService es el servicio MPD de mDNS.
const mpdService = "_mpd._tcp.local"

// installMPD engancha el servidor al motor (abrir lo que llega por URL, sus
// etiquetas, avisar de los cambios) y lo arranca si está encendido. Va
// después de installDLNA, cuyos ganchos envuelve.
func (d *Daemon) installMPD() {
	open := d.eng.OpenRemote
	d.eng.OpenRemote = func(path string) (decode.Source, bool, error) {
		if mpd.IsPath(path) {
			if s := d.mpdServer(); s != nil {
				return s.Open(path)
			}
			return nil, false, errors.New("mpd server is off")
		}
		return open(path)
	}
	track := library.RemoteTrack
	library.RemoteTrack = func(path string) (library.Track, bool) {
		if mpd.IsPath(path) {
			if s := d.mpdServer(); s != nil {
				if t, ok := s.Tags(path); ok {
					return library.Track{Path: path, Title: t.Title, Artist: t.Artist, Album: t.Album}, true
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
		if s := d.mpdServer(); s != nil {
			s.Changed()
		}
	}
	changed := d.eng.OnTrackChanged
	d.eng.OnTrackChanged = func(t library.Track) {
		if changed != nil {
			changed(t)
		}
		if s := d.mpdServer(); s != nil {
			s.Changed()
		}
	}
	d.applyMPD(d.cfg.MPD)
}

// mpdServer es el servidor encendido (nil si no).
func (d *Daemon) mpdServer() *mpd.Server {
	d.mpdMu.Lock()
	defer d.mpdMu.Unlock()
	return d.mpd
}

// mpdName es el nombre con el que se anuncia.
func mpdName(c config.MPD) string {
	if c.Name != "" {
		return c.Name
	}
	n := dlna.DefaultName()
	if p := c.MPDPort(); p != 6600 {
		// Otro audiofilus del mismo equipo: que no se llamen igual.
		n += fmt.Sprintf(" :%d", p)
	}
	return n
}

// applyMPD enciende, apaga o reinicia el servidor según c.
func (d *Daemon) applyMPD(c config.MPD) {
	d.mpdMu.Lock()
	defer d.mpdMu.Unlock()
	if d.mpd != nil {
		if d.mpdCfg == c {
			return
		}
		d.closeMPDLocked()
	}
	d.mpdCfg = c
	if !c.On {
		return
	}
	s, err := mpd.Start(mpd.Options{Addr: ":" + strconv.Itoa(c.MPDPort()), Password: c.Password,
		DataDir: d.paths.Data, Player: mpdPlayer{d.eng}})
	if err != nil {
		log.Printf("mpd: %v", err)
		d.mpdErr = err.Error()
		return
	}
	d.mpdErr = ""
	d.mpd = s
	txt := map[string]string{"app": "audiofilus"}
	if c.Password != "" {
		txt["password"] = "1"
	}
	if r, err := mdns.Announce(mpdName(c), mpdService, c.MPDPort(), txt); err != nil {
		log.Printf("mpd: mdns: %v", err)
	} else {
		d.mpdAnnounce = r
	}
}

// closeMPDLocked apaga el servidor y su anuncio (con d.mpdMu tomado).
func (d *Daemon) closeMPDLocked() {
	if d.mpdAnnounce != nil {
		d.mpdAnnounce.Close()
		d.mpdAnnounce = nil
	}
	if d.mpd != nil {
		d.mpd.Close()
		d.mpd = nil
	}
}

// stopMPD apaga el servidor (al cerrar el servicio).
func (d *Daemon) stopMPD() {
	d.mpdMu.Lock()
	defer d.mpdMu.Unlock()
	d.closeMPDLocked()
}

// MPDStatus es la respuesta de "mpd".
type MPDStatus struct {
	On       bool   `json:"on"`
	Port     int    `json:"port"`
	Name     string `json:"name"`
	Password bool   `json:"password,omitempty"`
	Error    string `json:"error,omitempty"`
}

// mpdCommand atiende "mpd": sin argumentos (o "status"), el estado;
// "apply", relee audiofilus.json; "on|off", "port <n>", "password <clave>"
// (vacía: sin clave) y "name <nombre>" lo cambian, lo guardan y lo aplican.
func (d *Daemon) mpdCommand(args []string) (any, error) {
	if len(args) > 0 && args[0] != "status" {
		cfg, err := config.Load(d.paths)
		if err != nil {
			return nil, err
		}
		switch args[0] {
		case "apply":
		case "on", "off", "toggle":
			on, err := onOff(args, !cfg.MPD.On)
			if err != nil {
				return nil, err
			}
			cfg.MPD.On = on
		case "port":
			if len(args) < 2 {
				return nil, errors.New("usage: mpd port <port>")
			}
			p, err := strconv.Atoi(args[1])
			if err != nil || p <= 0 || p > 65535 {
				return nil, errors.New("bad port")
			}
			cfg.MPD.Port = p
		case "password":
			cfg.MPD.Password = ""
			if len(args) > 1 {
				cfg.MPD.Password = args[1]
			}
		case "name":
			if len(args) < 2 {
				return nil, errors.New("usage: mpd name <name>")
			}
			cfg.MPD.Name = args[1]
		default:
			return nil, errors.New("usage: mpd [status|apply|on|off|port <port>|password [<password>]|name <name>]")
		}
		if args[0] != "apply" {
			if err := config.Save(d.paths, cfg); err != nil {
				return nil, err
			}
		}
		d.applyMPD(cfg.MPD)
	}
	d.mpdMu.Lock()
	defer d.mpdMu.Unlock()
	return MPDStatus{On: d.mpd != nil, Port: d.mpdCfg.MPDPort(), Name: mpdName(d.mpdCfg),
		Password: d.mpdCfg.Password != "", Error: d.mpdErr}, nil
}

// mpdPlayer es el motor visto por el servidor MPD.
type mpdPlayer struct{ e *engine.Engine }

func (p mpdPlayer) Status() mpd.Status {
	st := p.e.Status()
	out := mpd.Status{Pos: st.Index, Path: st.Track.Path, Elapsed: st.Position, Duration: st.Duration,
		Volume: int(math.Round(math.Min(st.Volume, 1) * 100)), Random: st.Shuffle, Version: st.QueueVersion}
	switch st.State {
	case "playing":
		out.State = "play"
	case "paused":
		out.State = "pause"
	default:
		out.State = "stop"
	}
	if st.Count == 0 {
		out.Pos = -1
	}
	switch st.Repeat {
	case "all":
		out.Repeat = true
	case "one":
		out.Repeat, out.Single = true, true
	default:
		out.Single = st.StopAfter
	}
	return out
}

func (p mpdPlayer) Queue() []string    { return p.e.Queue() }
func (p mpdPlayer) QueueIDs() []int    { return p.e.QueueIDs() }
func (p mpdPlayer) Append(ps []string) { p.e.Append(ps) }
func (p mpdPlayer) Play()              { p.e.Play() }
func (p mpdPlayer) Pause()             { p.e.Pause() }
func (p mpdPlayer) Stop()              { p.e.Pause(); p.e.SeekTo(0) }
func (p mpdPlayer) Next()              { p.e.Next() }
func (p mpdPlayer) Prev()              { p.e.Prev() }
func (p mpdPlayer) Jump(pos int)       { p.e.Jump(pos) }
func (p mpdPlayer) SeekTo(s float64)   { p.e.SeekTo(s) }
func (p mpdPlayer) SetVolume(v int)    { p.e.SetVolume(float64(v) / 100) }
func (p mpdPlayer) SetRandom(on bool)  { p.e.SetShuffle(on) }
func (p mpdPlayer) Remove(pos int)     { p.e.Remove(pos) }
func (p mpdPlayer) Move(from, to int)  { p.e.Move(from, to) }
func (p mpdPlayer) Clear()             { p.e.Clear() }

func (p mpdPlayer) Load(paths []string, start int, play bool) { p.e.Load(paths, start, 0, play) }

// SetRepeat: repeat con single es repetir una; sin single, todas; single
// solo es parar al acabar la que suena.
func (p mpdPlayer) SetRepeat(repeat, single bool) {
	switch {
	case repeat && single:
		p.e.SetRepeat(queue.RepeatOne)
	case repeat:
		p.e.SetRepeat(queue.RepeatAll)
	default:
		p.e.SetRepeat(queue.RepeatOff)
	}
	p.e.SetStopAfter(single && !repeat)
}

// Tags son las etiquetas de una ruta que no está en la biblioteca.
func (p mpdPlayer) Tags(path string) (mpd.Tags, bool) {
	t, ok := library.Track{}, false
	if library.RemoteTrack != nil {
		t, ok = library.RemoteTrack(path)
	}
	if !ok {
		t = library.ReadTrack(path)
	}
	return mpd.Tags{Title: t.Title, Artist: t.Artist, Album: t.Album}, true
}
