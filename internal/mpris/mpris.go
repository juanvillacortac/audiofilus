// Package mpris anuncia el servicio como reproductor MPRIS en el bus de sesión
// de D-Bus. Es el estándar de Linux para reproductores: con él, las teclas
// multimedia del teclado y los widgets de música del escritorio (en Omarchy,
// "omarchy-shell media" y la barra) controlan audiofilus sin configurar nada.
//
// En las portátiles no suele haber bus de sesión; entonces Start no hace nada.
package mpris

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/queue"
)

const (
	busName    = "org.mpris.MediaPlayer2.audiofilus"
	objectPath = "/org/mpris/MediaPlayer2"
	ifaceRoot  = "org.mpris.MediaPlayer2"
	ifacePlay  = "org.mpris.MediaPlayer2.Player"
)

// Server es el reproductor publicado en D-Bus.
type Server struct {
	eng   *engine.Engine
	quit  func()
	conn  *dbus.Conn
	props *prop.Properties

	mu      sync.Mutex
	last    engine.Status
	artPath string
	artFor  string // canción de la que salió artPath
	stop    chan struct{}
	closed  bool // Close ya cerró la conexión: Update no publica nada
}

// Start se conecta al bus de sesión y publica el reproductor. quit es lo que
// hace "Quit" (parar el servicio). Si no hay bus de sesión devuelve un error y
// el servicio sigue sin MPRIS.
func Start(eng *engine.Engine, quit func()) (*Server, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" && os.Getenv("XDG_RUNTIME_DIR") == "" {
		return nil, fmt.Errorf("no D-Bus session bus")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("D-Bus name %s is taken", busName)
	}
	s := &Server{eng: eng, quit: quit, conn: conn, stop: make(chan struct{})}
	if err := conn.Export(root{s}, objectPath, ifaceRoot); err != nil {
		return nil, err
	}
	if err := conn.ExportWithMap(player{s}, map[string]string{"SeekBy": "Seek"}, objectPath, ifacePlay); err != nil {
		return nil, err
	}
	s.props, err = prop.Export(conn, objectPath, s.propMap())
	if err != nil {
		return nil, err
	}
	node := &introspect.Node{
		Name: objectPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: ifaceRoot, Methods: introspect.Methods(root{s}), Properties: s.props.Introspection(ifaceRoot)},
			{Name: ifacePlay, Methods: playerMethods(s), Properties: s.props.Introspection(ifacePlay)},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), objectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, err
	}
	go s.loop()
	return s, nil
}

// Close deja de publicar el reproductor.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	close(s.stop)
	s.conn.Close()
	if s.artPath != "" {
		_ = os.Remove(s.artPath)
	}
}

// set publica una propiedad. SetMust (Set no deja tocar las de solo
// lectura) entra en pánico si la conexión con el bus se cayó: entonces el
// cambio se pierde y la música sigue.
func (s *Server) set(iface, name string, v any) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("MPRIS %s: %v", name, r)
		}
	}()
	s.props.SetMust(iface, name, v)
}

func (s *Server) propMap() prop.Map {
	st := s.eng.Status()
	return prop.Map{
		ifaceRoot: {
			"CanQuit":             {Value: true, Emit: prop.EmitConst},
			"CanRaise":            {Value: false, Emit: prop.EmitConst},
			"HasTrackList":        {Value: false, Emit: prop.EmitConst},
			"Identity":            {Value: "Audiofilus", Emit: prop.EmitConst},
			"DesktopEntry":        {Value: "audiofilus", Emit: prop.EmitConst}, // audiofilus.desktop: su nombre e icono
			"SupportedUriSchemes": {Value: []string{"file"}, Emit: prop.EmitConst},
			"SupportedMimeTypes":  {Value: []string{"audio/mpeg", "audio/ogg", "audio/flac", "audio/wav"}, Emit: prop.EmitConst},
		},
		ifacePlay: {
			"PlaybackStatus": {Value: playbackStatus(st), Emit: prop.EmitTrue},
			"LoopStatus":     {Value: loopStatus(st.Repeat), Writable: true, Emit: prop.EmitTrue, Callback: s.setLoop},
			"Rate":           {Value: 1.0, Writable: true, Emit: prop.EmitTrue, Callback: func(*prop.Change) *dbus.Error { return nil }},
			"Shuffle":        {Value: st.Shuffle, Writable: true, Emit: prop.EmitTrue, Callback: s.setShuffle},
			"Metadata":       {Value: s.metadata(st), Emit: prop.EmitTrue},
			"Volume":         {Value: st.Volume, Writable: true, Emit: prop.EmitTrue, Callback: s.setVolume},
			"Position":       {Value: micros(st.Position), Emit: prop.EmitFalse},
			"MinimumRate":    {Value: 1.0, Emit: prop.EmitConst},
			"MaximumRate":    {Value: 1.0, Emit: prop.EmitConst},
			"CanGoNext":      {Value: true, Emit: prop.EmitTrue},
			"CanGoPrevious":  {Value: true, Emit: prop.EmitTrue},
			"CanPlay":        {Value: true, Emit: prop.EmitTrue},
			"CanPause":       {Value: true, Emit: prop.EmitTrue},
			"CanSeek":        {Value: true, Emit: prop.EmitTrue},
			"CanControl":     {Value: true, Emit: prop.EmitConst},
		},
	}
}

// loop actualiza las propiedades: los clientes de MPRIS no preguntan, esperan
// la señal PropertiesChanged. Una vez por segundo basta (y la posición la
// interpolan ellos).
func (s *Server) loop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.Update()
		}
	}
}

// Update publica los cambios del estado. El daemon la llama también al cambiar
// de canción o al pausar, para que el escritorio se entere enseguida.
func (s *Server) Update() {
	st := s.eng.Status()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return // un aviso que llegó mientras el servicio se cerraba
	}
	last := s.last
	s.last = st
	s.set(ifacePlay, "Position", micros(st.Position))
	if st.State != last.State {
		s.set(ifacePlay, "PlaybackStatus", playbackStatus(st))
	}
	if st.Track.Path != last.Track.Path || st.Duration != last.Duration {
		s.set(ifacePlay, "Metadata", s.metadata(st))
	}
	if st.Shuffle != last.Shuffle {
		s.set(ifacePlay, "Shuffle", st.Shuffle)
	}
	if st.Repeat != last.Repeat {
		s.set(ifacePlay, "LoopStatus", loopStatus(st.Repeat))
	}
	if st.Volume != last.Volume {
		s.set(ifacePlay, "Volume", st.Volume)
	}
	// Un salto grande de posición (seek) se anuncia con la señal Seeked.
	if st.Track.Path == last.Track.Path && math.Abs(st.Position-last.Position) > 3 {
		_ = s.conn.Emit(objectPath, ifacePlay+".Seeked", micros(st.Position))
	}
}

// metadata son los datos de la canción en el formato de MPRIS.
func (s *Server) metadata(st engine.Status) map[string]dbus.Variant {
	t := st.Track
	if t.Path == "" {
		return map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"))}
	}
	sum := sha256.Sum256([]byte(t.Path))
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/audiofilus/track/" + hex.EncodeToString(sum[:8]))),
		"xesam:title":   dbus.MakeVariant(t.Title),
		"xesam:url":     dbus.MakeVariant("file://" + t.Path),
	}
	if t.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{t.Artist})
	}
	if t.Album != "" {
		m["xesam:album"] = dbus.MakeVariant(t.Album)
	}
	if st.Duration > 0 {
		m["mpris:length"] = dbus.MakeVariant(micros(st.Duration))
	}
	if art := s.art(t.Path); art != "" {
		m["mpris:artUrl"] = dbus.MakeVariant("file://" + art)
	}
	return m
}

// art guarda la portada de la canción en un archivo temporal, para el widget.
func (s *Server) art(path string) string {
	if path == s.artFor {
		return s.artPath
	}
	s.artFor = path
	if s.artPath != "" {
		_ = os.Remove(s.artPath)
		s.artPath = ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil || m.Picture() == nil {
		return ""
	}
	ext := m.Picture().Ext
	if ext == "" {
		ext = "jpg"
	}
	sum := sha256.Sum256([]byte(path))
	out := filepath.Join(os.TempDir(), "audiofilus-art-"+hex.EncodeToString(sum[:6])+"."+ext)
	if os.WriteFile(out, bytes.Clone(m.Picture().Data), 0o644) != nil {
		return ""
	}
	s.artPath = out
	return out
}

func (s *Server) setLoop(c *prop.Change) *dbus.Error {
	v, _ := c.Value.(string)
	r := queue.RepeatOff
	switch v {
	case "Track":
		r = queue.RepeatOne
	case "Playlist":
		r = queue.RepeatAll
	}
	s.eng.SetRepeat(r)
	return nil
}

func (s *Server) setShuffle(c *prop.Change) *dbus.Error {
	v, _ := c.Value.(bool)
	s.eng.SetShuffle(v)
	return nil
}

func (s *Server) setVolume(c *prop.Change) *dbus.Error {
	v, _ := c.Value.(float64)
	s.eng.SetVolume(v)
	return nil
}

func playbackStatus(st engine.Status) string {
	switch st.State {
	case "playing":
		return "Playing"
	case "paused":
		return "Paused"
	}
	return "Stopped"
}

func loopStatus(r string) string {
	switch r {
	case "one":
		return "Track"
	case "all":
		return "Playlist"
	}
	return "None"
}

func micros(sec float64) int64 { return int64(sec * 1e6) }

// playerMethods es la introspección de player, con SeekBy publicado como Seek.
func playerMethods(s *Server) []introspect.Method {
	ms := introspect.Methods(player{s})
	for i := range ms {
		if ms[i].Name == "SeekBy" {
			ms[i].Name = "Seek"
		}
	}
	return ms
}

// root es org.mpris.MediaPlayer2.
type root struct{ s *Server }

func (r root) Raise() *dbus.Error { return nil }
func (r root) Quit() *dbus.Error  { go r.s.quit(); return nil }

// player es org.mpris.MediaPlayer2.Player. Tras cada orden se publica el estado.
type player struct{ s *Server }

func (p player) after() *dbus.Error { go p.s.Update(); return nil }

func (p player) Next() *dbus.Error      { p.s.eng.Next(); return p.after() }
func (p player) Previous() *dbus.Error  { p.s.eng.Prev(); return p.after() }
func (p player) Pause() *dbus.Error     { p.s.eng.Pause(); return p.after() }
func (p player) Play() *dbus.Error      { p.s.eng.Play(); return p.after() }
func (p player) PlayPause() *dbus.Error { p.s.eng.Toggle(); return p.after() }
func (p player) Stop() *dbus.Error      { p.s.eng.Pause(); return p.after() }

// SeekBy es "Seek" de MPRIS (se exporta con ese nombre: en Go, Seek choca con
// io.Seeker): mueve offset microsegundos desde la posición actual.
func (p player) SeekBy(offset int64) *dbus.Error {
	p.s.eng.SeekBy(float64(offset) / 1e6)
	return p.after()
}

// SetPosition salta a una posición absoluta de la canción actual.
func (p player) SetPosition(track dbus.ObjectPath, pos int64) *dbus.Error {
	p.s.eng.SeekTo(float64(pos) / 1e6)
	return p.after()
}

// OpenUri no se usa: la música se elige en la interfaz.
func (p player) OpenUri(uri string) *dbus.Error { return nil }
