package cast

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/juanvillacortac/audiofilus/internal/mdns"
	"github.com/juanvillacortac/audiofilus/internal/mpd"
)

// Mandar la música a un servidor MPD (otro audiofilus con su servidor MPD
// encendido, o un mpd): se le pasa la URL del archivo con addid (la baja y
// la toca) y sus etiquetas con addtagid; la siguiente va detrás en su cola,
// así que la encadena él solo, sin cortes.

// mpdService es el servicio MPD de mDNS.
const mpdService = "_mpd._tcp.local"

// discoverMPD busca servidores MPD hasta que se acabe ctx.
func discoverMPD(ctx context.Context) ([]Device, error) {
	found, err := mdns.Browse(ctx, mpdService)
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, in := range found {
		d := Device{Kind: MPD, ID: "mpd:" + in.Host(), Name: in.Name, Model: "MPD", Host: in.Host(),
			NeedsPassword: in.TXT["password"] == "1"}
		if in.TXT["app"] == "audiofilus" {
			d.Model = "Audiofilus"
		}
		out = append(out, d)
	}
	return out, nil
}

// mpdPlayer controla un servidor MPD.
type mpdPlayer struct {
	dev Device
	mu  sync.Mutex
	c   *mpd.Client
	id  string // el id de la canción que se le mandó
	// loading: a medio Load (entre el clear y el addid la cola está vacía,
	// y eso no es que dejara de recibir).
	loading atomic.Bool
}

func newMPDPlayer(d Device) *mpdPlayer { return &mpdPlayer{dev: d} }

// cmd manda un comando (conectándose, o reconectándose si se cortó).
func (p *mpdPlayer) cmd(args ...string) ([]mpd.Pair, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for try := 0; ; try++ {
		if p.c == nil {
			c, err := mpd.Dial(p.dev.Host, p.dev.Password)
			if err != nil {
				return nil, err
			}
			p.c = c
		}
		pairs, err := p.c.Cmd(args...)
		if _, isAck := err.(*mpd.Error); err == nil || isAck || try > 0 {
			return pairs, err
		}
		// La conexión se cortó: otra vez, con una nueva.
		p.c.Close()
		p.c = nil
	}
}

// add pone m en la cola del servidor (con sus etiquetas) y da su id.
func (p *mpdPlayer) add(m media, pos string) (string, error) {
	args := []string{"addid", m.URL}
	if pos != "" {
		args = append(args, pos)
	}
	pairs, err := p.cmd(args...)
	if err != nil {
		return "", err
	}
	id := mpd.Map(pairs)["Id"]
	if !strings.Contains(m.URL, "://") {
		return id, nil // de su biblioteca: ya tiene sus etiquetas
	}
	for _, t := range [][2]string{{"Title", m.Title}, {"Artist", m.Artist}, {"Album", m.Album}} {
		if t[1] != "" {
			_, _ = p.cmd("addtagid", id, t[0], t[1]) // un mpd puede no dejar
		}
	}
	if m.Cover != "" && p.dev.Model == "Audiofilus" {
		// La portada, con la etiqueta propia de audiofilus (un mpd no la
		// conoce).
		_, _ = p.cmd("addtagid", id, mpd.CoverTag, m.Cover)
	}
	return id, nil
}

func (p *mpdPlayer) Load(_ context.Context, m media, start float64) error {
	p.loading.Store(true)
	defer p.loading.Store(false)
	if _, err := p.cmd("clear"); err != nil {
		return err
	}
	id, err := p.add(m, "")
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.id = id
	p.mu.Unlock()
	if _, err := p.cmd("playid", id); err != nil {
		return err
	}
	if start > 1 {
		_, _ = p.cmd("seekid", id, strconv.FormatFloat(start, 'f', 3, 64))
	}
	return nil
}

func (p *mpdPlayer) Play(context.Context) error  { _, err := p.cmd("pause", "0"); return err }
func (p *mpdPlayer) Pause(context.Context) error { _, err := p.cmd("pause", "1"); return err }
func (p *mpdPlayer) Stop(context.Context) error  { _, err := p.cmd("stop"); return err }
func (p *mpdPlayer) Seek(_ context.Context, sec float64) error {
	_, err := p.cmd("seekcur", strconv.FormatFloat(sec, 'f', 3, 64))
	return err
}

func (p *mpdPlayer) SetVolume(_ context.Context, v float64) error {
	_, err := p.cmd("setvol", strconv.Itoa(int(max(0, min(1, v))*100+0.5)))
	return err
}

func (p *mpdPlayer) Status(context.Context) (Status, error) {
	pairs, err := p.cmd("status")
	if err != nil {
		return Status{}, err
	}
	m := mpd.Map(pairs)
	p.mu.Lock()
	sent := p.id != ""
	p.mu.Unlock()
	if sent && m["playlistlength"] == "0" && !p.loading.Load() {
		return Status{}, ErrReleased
	}
	st := Status{Volume: -1}
	switch m["state"] {
	case "play":
		st.State = Playing
	case "pause":
		st.State = Paused
	default:
		st.State = Stopped
	}
	st.Position, _ = strconv.ParseFloat(m["elapsed"], 64)
	st.Duration, _ = strconv.ParseFloat(m["duration"], 64)
	if v, err := strconv.Atoi(m["volume"]); err == nil && v >= 0 {
		st.Volume = float64(v) / 100
	}
	return st, nil
}

// SetNext pone la siguiente detrás de la que suena: el servidor pasa solo.
func (p *mpdPlayer) SetNext(_ context.Context, m media) error {
	// Lo que hubiera detrás de la actual (otra siguiente de antes) sobra.
	pairs, err := p.cmd("status")
	if err != nil {
		return err
	}
	st := mpd.Map(pairs)
	if song, err := strconv.Atoi(st["song"]); err == nil {
		if n, _ := strconv.Atoi(st["playlistlength"]); n > song+1 {
			_, _ = p.cmd("delete", strconv.Itoa(song+1)+":")
		}
	}
	_, err = p.add(m, "")
	return err
}

func (p *mpdPlayer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.c != nil {
		p.c.Close()
		p.c = nil
	}
	return nil
}

// IsOwnMPD dice si d es el servidor MPD de este mismo equipo en port (para
// no ofrecerse a sí mismo en "Enviar a").
func IsOwnMPD(d Device, port int) bool {
	if d.Kind != MPD {
		return false
	}
	host, p, ok := strings.Cut(d.Host, ":")
	if !ok || p != strconv.Itoa(port) {
		return false
	}
	for _, ip := range mdns.LANIPs() {
		if ip.String() == host {
			return true
		}
	}
	return host == "127.0.0.1"
}
