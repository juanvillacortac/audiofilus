package dlna

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/juanvillacortac/audiofilus/internal/fetch"
)

// El renderer: otros equipos (el móvil con BubbleUPnP, el "Transmitir a" de
// Windows, una app DLNA) mandan una URL y la controlan (Play, Pause, Seek,
// volumen); suena aquí, por el motor del servicio. Cada URL se pone en la
// cola como "dlna://r/<n>"; el servicio la abre con Open (bajándola primero)
// y sus etiquetas salen de los metadatos DIDL que mandó el mando.

// Player es lo que el renderer necesita del motor (en el servicio, el
// engine; en los tests, uno falso).
type Player interface {
	// Load pone path como la cola y, con play, lo toca.
	Load(path string, play bool)
	// Append pone path detrás de lo que suena (SetNextAVTransportURI).
	Append(path string)
	Play()
	Pause()
	// Stop para y vuelve al principio.
	Stop()
	Seek(seconds float64)
	Next()
	Prev()
	// Status es lo que suena: su ruta, si suena, si está en pausa, si aún
	// se carga, la posición y la duración (segundos).
	Status() PlayerStatus
	// Volume y SetVolume, de 0 a 100.
	Volume() int
	SetVolume(v int)
}

// PlayerStatus es el estado del motor.
type PlayerStatus struct {
	Path     string
	Playing  bool
	Paused   bool
	Loading  bool
	Position float64
	Duration float64
}

// Meta son los metadatos de una URL (del DIDL-Lite que manda el mando).
type Meta struct {
	Title, Artist, Album, Art string
	Duration                  float64
	Raw                       string // el DIDL tal cual, para devolverlo
}

// parseMeta lee lo útil de un DIDL-Lite (sin fiarse de los prefijos).
func parseMeta(raw string) Meta {
	m := Meta{Raw: raw}
	if raw == "" {
		return m
	}
	dec := xml.NewDecoder(strings.NewReader(raw))
	dec.Strict = false
	var cur string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			cur = t.Name.Local
			if cur == "res" {
				for _, a := range t.Attr {
					if a.Name.Local == "duration" {
						m.Duration = parseTime(a.Value)
					}
				}
			}
		case xml.CharData:
			v := strings.TrimSpace(string(t))
			if v == "" {
				continue
			}
			switch cur {
			case "title":
				if m.Title == "" {
					m.Title = v
				}
			case "artist", "creator":
				if m.Artist == "" {
					m.Artist = v
				}
			case "album":
				if m.Album == "" {
					m.Album = v
				}
			case "albumArtURI":
				if m.Art == "" {
					m.Art = v
				}
			}
		case xml.EndElement:
			cur = ""
		}
	}
	return m
}

// parseTime lee "H:MM:SS(.fff)" en segundos.
func parseTime(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	t := 0.0
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0
		}
		t = t*60 + v
	}
	return t
}

// fmtTime escribe segundos como "H:MM:SS".
func fmtTime(s float64) string {
	if s < 0 {
		s = 0
	}
	n := int(s)
	return fmt.Sprintf("%d:%02d:%02d", n/3600, n/60%60, n%60)
}

// renderer es el MediaRenderer.
type renderer struct {
	player  Player
	fetch   *fetch.Fetcher[Meta]
	mu      sync.Mutex
	seq     int
	cur     string // la ruta de lo puesto (dlna://r/<n>), o ""
	next    string
	muted   bool
	unmuted int  // el volumen de antes de silenciar
	stopped bool // se pidió Stop (el motor queda en pausa al principio: para UPnP es STOPPED)
	avt, rc *events
	last    string // el último LastChange del AVTransport mandado
	lastRC  string
}

// entry es la URL y los metadatos de una ruta ("" si no es del renderer).
func (r *renderer) entry(p string) (string, Meta) {
	uri, m, _ := r.fetch.Entry(p)
	return uri, m
}

func newRenderer(p Player, f *fetch.Fetcher[Meta]) *renderer {
	r := &renderer{player: p, fetch: f}
	r.avt = newEvents(func() string { return propertySet([2]string{"LastChange", r.avtLastChange()}) })
	r.rc = newEvents(func() string { return propertySet([2]string{"LastChange", r.rcLastChange()}) })
	return r
}

// state es el TransportState de UPnP según el motor.
func (r *renderer) state() (state string, st PlayerStatus, mine bool) {
	st = r.player.Status()
	r.mu.Lock()
	cur, next, stopped := r.cur, r.next, r.stopped
	r.mu.Unlock()
	mine = cur != "" && (st.Path == cur || st.Path == next)
	switch {
	case cur == "":
		return "NO_MEDIA_PRESENT", st, mine
	case !mine:
		return "STOPPED", st, mine
	case st.Loading:
		return "TRANSITIONING", st, mine
	case st.Playing:
		return "PLAYING", st, mine
	case stopped:
		return "STOPPED", st, mine
	case st.Paused:
		return "PAUSED_PLAYBACK", st, mine
	}
	return "STOPPED", st, mine
}

// current es la ruta que suena de las del renderer (la siguiente, si ya
// pasó a ella) y su URL y metadatos.
func (r *renderer) current(st PlayerStatus) (path, uri string, meta Meta) {
	r.mu.Lock()
	path = r.cur
	if r.next != "" && st.Path == r.next {
		// El motor ya pasó a la siguiente: ahora es la actual.
		r.cur, r.next = r.next, ""
		path = r.cur
	}
	r.mu.Unlock()
	uri, meta = r.entry(path)
	return
}

// avtLastChange es el LastChange del AVTransport con el estado entero.
func (r *renderer) avtLastChange() string {
	state, st, mine := r.state()
	_, uri, meta := r.current(st)
	dur := meta.Duration
	if mine && st.Duration > 0 {
		dur = st.Duration
	}
	r.mu.Lock()
	next := r.next
	r.mu.Unlock()
	nextURI, nextMeta := r.entry(next)
	tracks := 0
	if uri != "" {
		tracks = 1
	}
	var b strings.Builder
	b.WriteString(`<Event xmlns="urn:schemas-upnp-org:metadata-1-0/AVT/"><InstanceID val="0">`)
	for _, kv := range [][2]string{
		{"TransportState", state},
		{"TransportStatus", "OK"},
		{"TransportPlaySpeed", "1"},
		{"CurrentPlayMode", "NORMAL"},
		{"NumberOfTracks", strconv.Itoa(tracks)},
		{"CurrentTrack", strconv.Itoa(tracks)},
		{"CurrentTrackDuration", fmtTime(dur)},
		{"CurrentMediaDuration", fmtTime(dur)},
		{"AVTransportURI", uri},
		{"CurrentTrackURI", uri},
		{"AVTransportURIMetaData", meta.Raw},
		{"CurrentTrackMetaData", meta.Raw},
		{"NextAVTransportURI", nextURI},
		{"NextAVTransportURIMetaData", nextMeta.Raw},
		{"CurrentTransportActions", transportActions(state)},
		{"PlaybackStorageMedium", "NETWORK"},
	} {
		fmt.Fprintf(&b, `<%s val="%s"/>`, kv[0], xmlEscape(kv[1]))
	}
	b.WriteString("</InstanceID></Event>")
	return b.String()
}

// rcLastChange es el LastChange del RenderingControl.
func (r *renderer) rcLastChange() string {
	vol, muted := r.volume()
	m := "0"
	if muted {
		m = "1"
	}
	return fmt.Sprintf(`<Event xmlns="urn:schemas-upnp-org:metadata-1-0/RCS/"><InstanceID val="0"><Volume channel="Master" val="%d"/><Mute channel="Master" val="%s"/></InstanceID></Event>`, vol, m)
}

// volume es el volumen (el de antes de silenciar, si lo está) y el silencio.
func (r *renderer) volume() (int, bool) {
	r.mu.Lock()
	muted, unmuted := r.muted, r.unmuted
	r.mu.Unlock()
	if muted {
		return unmuted, true
	}
	return r.player.Volume(), false
}

// transportActions son las acciones que tienen sentido en state.
func transportActions(state string) string {
	switch state {
	case "PLAYING":
		return "Pause,Stop,Seek,Next,Previous"
	case "PAUSED_PLAYBACK":
		return "Play,Stop,Seek,Next,Previous"
	case "STOPPED":
		return "Play,Seek"
	}
	return ""
}

// changed manda los eventos si cambió algo (lo llama el servicio al
// cambiar lo que suena, y el renderer tras cada acción).
func (r *renderer) changed() {
	if r.avt.count() > 0 {
		lc := r.avtLastChange()
		r.mu.Lock()
		send := lc != r.last
		r.last = lc
		r.mu.Unlock()
		if send {
			r.avt.notify(propertySet([2]string{"LastChange", lc}))
		}
	}
	if r.rc.count() > 0 {
		lc := r.rcLastChange()
		r.mu.Lock()
		send := lc != r.lastRC
		r.lastRC = lc
		r.mu.Unlock()
		if send {
			r.rc.notify(propertySet([2]string{"LastChange", lc}))
		}
	}
}

// newPath es la ruta de la cola para una URL nueva.
func (r *renderer) newPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return fmt.Sprintf("dlna://r/%d", r.seq)
}

// checkInstance comprueba el InstanceID (solo hay el 0).
func checkInstance(a *action) error {
	if v := a.Args["InstanceID"]; v != "" && v != "0" {
		return errBadInstance
	}
	return nil
}

// avTransport atiende las acciones del AVTransport.
func (r *renderer) avTransport(a *action) ([]arg, error) {
	if err := checkInstance(a); err != nil {
		return nil, err
	}
	defer func() { go r.changed() }()
	switch a.Name {
	case "SetAVTransportURI", "Play", "Pause":
		r.mu.Lock()
		r.stopped = false
		r.mu.Unlock()
	case "Stop":
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
	}
	switch a.Name {
	case "SetAVTransportURI":
		uri := strings.TrimSpace(a.Args["CurrentURI"])
		if uri == "" {
			return nil, errInvalidArgs
		}
		state, _, _ := r.state()
		path := r.newPath()
		r.fetch.Add(path, uri, parseMeta(a.Args["CurrentURIMetaData"]))
		r.mu.Lock()
		r.cur, r.next = path, ""
		r.mu.Unlock()
		r.fetch.Keep(path)
		// Si sonaba algo del renderer, la nueva sigue sonando (así cambian
		// de canción la mayoría de los mandos); si no, espera a Play.
		r.player.Load(path, state == "PLAYING" || state == "TRANSITIONING")
		return nil, nil
	case "SetNextAVTransportURI":
		uri := strings.TrimSpace(a.Args["NextURI"])
		path := r.newPath()
		r.mu.Lock()
		cur := r.cur
		if uri == "" {
			r.next = ""
			r.mu.Unlock()
			return nil, nil
		}
		r.next = path
		r.mu.Unlock()
		r.fetch.Add(path, uri, parseMeta(a.Args["NextURIMetaData"]))
		r.fetch.Keep(cur, path)
		r.player.Append(path)
		return nil, nil
	case "Play":
		r.mu.Lock()
		cur := r.cur
		r.mu.Unlock()
		if cur == "" {
			return nil, errTransition
		}
		if st := r.player.Status(); st.Path != cur {
			r.player.Load(cur, true) // sonaba otra cosa (de la interfaz): vuelve lo del renderer
			return nil, nil
		}
		r.player.Play()
		return nil, nil
	case "Pause":
		r.player.Pause()
		return nil, nil
	case "Stop":
		if _, _, mine := r.state(); mine {
			r.player.Stop()
		}
		return nil, nil
	case "Seek":
		switch a.Args["Unit"] {
		case "REL_TIME", "ABS_TIME":
			t := parseTime(a.Args["Target"])
			if t < 0 {
				return nil, errSeekTarget
			}
			r.player.Seek(t)
		default:
			return nil, errSeekMode
		}
		return nil, nil
	case "Next":
		r.player.Next()
		return nil, nil
	case "Previous":
		r.player.Prev()
		return nil, nil
	case "GetTransportInfo":
		state, _, _ := r.state()
		return []arg{{"CurrentTransportState", state}, {"CurrentTransportStatus", "OK"}, {"CurrentSpeed", "1"}}, nil
	case "GetPositionInfo":
		_, st, mine := r.state()
		_, uri, meta := r.current(st)
		pos, dur := 0.0, meta.Duration
		if mine {
			pos = st.Position
			if st.Duration > 0 {
				dur = st.Duration
			}
		}
		track := "0"
		if uri != "" {
			track = "1"
		}
		return []arg{{"Track", track}, {"TrackDuration", fmtTime(dur)}, {"TrackMetaData", meta.Raw}, {"TrackURI", uri},
			{"RelTime", fmtTime(pos)}, {"AbsTime", fmtTime(pos)}, {"RelCount", "2147483647"}, {"AbsCount", "2147483647"}}, nil
	case "GetMediaInfo":
		_, st, _ := r.state()
		_, uri, meta := r.current(st)
		r.mu.Lock()
		next := r.next
		r.mu.Unlock()
		nextURI, nextMeta := r.entry(next)
		n := "0"
		if uri != "" {
			n = "1"
		}
		return []arg{{"NrTracks", n}, {"MediaDuration", fmtTime(meta.Duration)}, {"CurrentURI", uri}, {"CurrentURIMetaData", meta.Raw},
			{"NextURI", nextURI}, {"NextURIMetaData", nextMeta.Raw}, {"PlayMedium", "NETWORK"}, {"RecordMedium", "NOT_IMPLEMENTED"}, {"WriteStatus", "NOT_IMPLEMENTED"}}, nil
	case "GetDeviceCapabilities":
		return []arg{{"PlayMedia", "NETWORK"}, {"RecMedia", "NOT_IMPLEMENTED"}, {"RecQualityModes", "NOT_IMPLEMENTED"}}, nil
	case "GetTransportSettings":
		return []arg{{"PlayMode", "NORMAL"}, {"RecQualityMode", "NOT_IMPLEMENTED"}}, nil
	case "GetCurrentTransportActions":
		state, _, _ := r.state()
		return []arg{{"Actions", transportActions(state)}}, nil
	}
	return nil, errInvalidAction
}

// renderingControl atiende las acciones del RenderingControl.
func (r *renderer) renderingControl(a *action) ([]arg, error) {
	if err := checkInstance(a); err != nil {
		return nil, err
	}
	switch a.Name {
	case "GetVolume":
		v, _ := r.volume()
		return []arg{{"CurrentVolume", strconv.Itoa(v)}}, nil
	case "SetVolume":
		v, err := strconv.Atoi(a.Args["DesiredVolume"])
		if err != nil || v < 0 || v > 100 {
			return nil, errInvalidArgs
		}
		r.mu.Lock()
		muted := r.muted
		if muted {
			r.unmuted = v
		}
		r.mu.Unlock()
		if !muted {
			r.player.SetVolume(v)
		}
		go r.changed()
		return nil, nil
	case "GetMute":
		_, m := r.volume()
		return []arg{{"CurrentMute", boolStr(m)}}, nil
	case "SetMute":
		on := a.Args["DesiredMute"] == "1" || strings.EqualFold(a.Args["DesiredMute"], "true")
		r.mu.Lock()
		was := r.muted
		r.muted = on
		if on && !was {
			r.unmuted = r.player.Volume()
		}
		vol := r.unmuted
		r.mu.Unlock()
		switch {
		case on && !was:
			r.player.SetVolume(0)
		case !on && was:
			r.player.SetVolume(vol)
		}
		go r.changed()
		return nil, nil
	case "ListPresets":
		return []arg{{"CurrentPresetNameList", "FactoryDefaults"}}, nil
	case "SelectPreset":
		return nil, nil
	}
	return nil, errInvalidAction
}

// boolStr es "1" o "0".
func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// sinkProtocols es lo que el renderer sabe tocar (GetProtocolInfo).
func sinkProtocols() string {
	mimes := []string{"audio/mpeg", "audio/mp3", "audio/flac", "audio/x-flac", "audio/ogg", "audio/x-ogg", "audio/opus",
		"audio/mp4", "audio/x-m4a", "audio/aac", "audio/x-aac", "audio/wav", "audio/x-wav", "audio/wave",
		"audio/aiff", "audio/x-aiff", "audio/L16", "audio/dsf", "audio/x-dsf"}
	out := make([]string, len(mimes))
	for i, m := range mimes {
		out[i] = "http-get:*:" + m + ":*"
	}
	return strings.Join(out, ",")
}
