// Package mpd es un servidor del protocolo de MPD (Music Player Daemon): los
// clientes de MPD (mpc, ncmpcpp, Cantata, M.A.L.P., Rigelian...) y otro
// audiofilus controlan el que suena aquí, ven su cola y su biblioteca, y le
// mandan canciones por URL. Habla el protocolo de texto de MPD 0.23 por TCP
// (puerto 6600), con contraseña opcional.
package mpd

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/fetch"
)

// protoVersion es la versión del protocolo que se anuncia.
const protoVersion = "0.23.5"

// Player es lo que el servidor necesita del motor (en el servicio, el
// engine; en los tests, uno falso). Las posiciones son las de la cola en
// orden de reproducción; los ids, los de QueueIDs.
type Player interface {
	Status() Status
	Queue() []string
	QueueIDs() []int
	// Load pone paths como la cola desde start y, con play, los toca.
	Load(paths []string, start int, play bool)
	Append(paths []string)
	Play()
	Pause()
	// Stop para y vuelve al principio de la canción.
	Stop()
	Next()
	Prev()
	// Jump toca la canción de la posición pos.
	Jump(pos int)
	SeekTo(seconds float64)
	// SetVolume va de 0 a 100.
	SetVolume(v int)
	SetRandom(on bool)
	// SetRepeat: repeat y single como en MPD (repetir todo, repetir una;
	// single sin repeat, parar al acabar la canción).
	SetRepeat(repeat, single bool)
	Remove(pos int)
	Move(from, to int)
	Clear()
	// Tags son las etiquetas de una ruta de la cola que no está en la
	// biblioteca (una remota, una radio).
	Tags(path string) (Tags, bool)
}

// Status es el estado del motor.
type Status struct {
	State             string // play, pause o stop
	Pos               int    // la canción actual en la cola (-1 si no hay)
	Path              string
	Elapsed, Duration float64
	Volume            int
	Random            bool
	Repeat, Single    bool
	Version           int // sube con cada cambio de la cola
}

// Options configuran el servidor.
type Options struct {
	Addr     string // ":6600"
	Password string // vacía: sin contraseña
	DataDir  string // data/: library.json, playlists/, cache/mpd
	Player   Player
}

// Server es el servidor MPD en marcha.
type Server struct {
	opts  Options
	ln    net.Listener
	lib   *libSource
	fetch *fetch.Fetcher[urlSong]

	mu    sync.Mutex
	conns map[*conn]bool
	urls  int    // el número de la última URL añadida
	last  Status // lo último visto, para saber qué cambió
	lists int64  // para saber si cambiaron las listas guardadas
	poke  chan struct{}
	done  chan struct{}
	start time.Time
	wg    sync.WaitGroup
}

// urlSong es una canción añadida por URL: la URL y sus etiquetas (las de
// addtagid).
type urlSong struct {
	uri  string
	tags Tags
}

// urlPrefix es el de las rutas de las canciones añadidas por URL en la cola
// del motor.
const urlPrefix = "mpd://u/"

// IsPath dice si path es una canción añadida por URL (para el OpenRemote
// del motor).
func IsPath(path string) bool { return strings.HasPrefix(path, urlPrefix) }

// Start abre el puerto y atiende clientes.
func Start(o Options) (*Server, error) {
	if o.Addr == "" {
		o.Addr = ":6600"
	}
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: o, ln: ln, lib: &libSource{dataDir: o.DataDir},
		fetch: fetch.New[urlSong](filepath.Join(o.DataDir, "cache", "mpd"), "audiofilus"),
		conns: map[*conn]bool{}, poke: make(chan struct{}, 1), done: make(chan struct{}), start: time.Now()}
	s.last = o.Player.Status()
	s.wg.Add(2)
	go s.accept()
	go s.watch()
	log.Printf("mpd: listening on %s", ln.Addr())
	return s, nil
}

// Addr es la dirección en la que escucha.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Close cierra el puerto y las conexiones.
func (s *Server) Close() {
	close(s.done)
	s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.nc.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.fetch.Keep()
}

// Changed avisa de que algo del motor cambió (para los clientes en idle).
func (s *Server) Changed() {
	select {
	case s.poke <- struct{}{}:
	default:
	}
}

// Open abre una canción añadida por URL (para el OpenRemote del motor).
func (s *Server) Open(path string) (decode.Source, bool, error) { return s.fetch.Open(path) }

// Tags son las etiquetas de una canción añadida por URL.
func (s *Server) Tags(path string) (Tags, bool) {
	uri, u, ok := s.fetch.Entry(path)
	if !ok {
		return Tags{}, false
	}
	t := u.tags
	if t.Title == "" {
		t.Title = fetch.TitleFromURL(uri)
	}
	return t, true
}

// accept atiende las conexiones nuevas.
func (s *Server) accept() {
	defer s.wg.Done()
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("mpd: %v", err)
			return
		}
		c := &conn{s: s, nc: nc, w: bufio.NewWriter(nc), authed: s.opts.Password == "",
			pending: map[string]bool{}, wake: make(chan struct{}, 1), binLimit: 8192}
		s.mu.Lock()
		s.conns[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			c.serve()
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
			nc.Close()
		}()
	}
}

// watch mira qué cambió en el motor y en la biblioteca y se lo dice a los
// clientes (idle). Solo pregunta mientras hay alguien conectado.
func (s *Server) watch() {
	defer s.wg.Done()
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	var dbWhen time.Time
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		case <-s.poke:
		}
		s.mu.Lock()
		n := len(s.conns)
		s.mu.Unlock()
		if n == 0 {
			continue
		}
		st := s.opts.Player.Status()
		var ev []string
		s.mu.Lock()
		old := s.last
		s.last = st
		s.mu.Unlock()
		if st.State != old.State || st.Path != old.Path || st.Pos != old.Pos {
			ev = append(ev, "player")
		}
		if st.Volume != old.Volume {
			ev = append(ev, "mixer")
		}
		if st.Random != old.Random || st.Repeat != old.Repeat || st.Single != old.Single {
			ev = append(ev, "options")
		}
		if st.Version != old.Version {
			ev = append(ev, "playlist")
			s.keepURLs()
		}
		idx := s.lib.get()
		if !dbWhen.IsZero() && !idx.when.Equal(dbWhen) {
			ev = append(ev, "database")
		}
		dbWhen = idx.when
		if len(ev) > 0 {
			s.notify(ev...)
		}
	}
}

// keepURLs olvida (y borra) las canciones bajadas que ya no están en la cola.
func (s *Server) keepURLs() {
	var keep []string
	for _, p := range s.opts.Player.Queue() {
		if IsPath(p) {
			keep = append(keep, p)
		}
	}
	s.fetch.Keep(keep...)
}

// notify apunta los subsistemas que cambiaron en cada cliente.
func (s *Server) notify(subsystems ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.mu.Lock()
		for _, e := range subsystems {
			c.pending[e] = true
		}
		c.mu.Unlock()
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

// addURL pone una URL en la cola del motor como una ruta propia.
func (s *Server) addURL(uri string) string {
	s.mu.Lock()
	s.urls++
	p := urlPrefix + strconv.Itoa(s.urls)
	s.mu.Unlock()
	s.fetch.Add(p, uri, urlSong{uri: uri})
	return p
}

// setURLTag pone una etiqueta a una canción añadida por URL.
func (s *Server) setURLTag(path, tag, value string, clear bool) bool {
	uri, u, ok := s.fetch.Entry(path)
	if !ok {
		return false
	}
	if clear {
		u.tags = Tags{}
	} else {
		u.tags.set(tag, value)
	}
	s.fetch.Add(path, uri, u)
	return true
}

// ack es un error del protocolo ("ACK [code@n] {cmd} msg").
type ack struct {
	code int
	msg  string
}

func (a *ack) Error() string { return a.msg }

// Los códigos de error de MPD.
const (
	ackNotList    = 1
	ackArg        = 2
	ackPassword   = 3
	ackPermission = 4
	ackUnknown    = 5
	ackNoExist    = 50
	ackSystem     = 52
	ackExist      = 56
)

func errArg(format string, a ...any) error { return &ack{ackArg, fmt.Sprintf(format, a...)} }
func errNoExist(msg string) error          { return &ack{ackNoExist, msg} }

// conn es un cliente conectado.
type conn struct {
	s        *Server
	nc       net.Conn
	w        *bufio.Writer
	authed   bool
	binLimit int
	// coverKey y cover son la última portada pedida (llega a trozos).
	coverKey string
	cover    []byte

	mu      sync.Mutex
	pending map[string]bool // los subsistemas que cambiaron desde el último idle
	wake    chan struct{}
}

// maxLine es lo más largo que puede ser un comando.
const maxLine = 64 << 10

// serve atiende al cliente hasta que se va.
func (c *conn) serve() {
	fmt.Fprintf(c.w, "OK MPD %s\n", protoVersion)
	c.w.Flush()
	lines := make(chan string)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		defer close(lines)
		r := bufio.NewReaderSize(c.nc, 4096)
		for {
			line, err := r.ReadString('\n')
			if len(line) > maxLine {
				return
			}
			if err != nil {
				return
			}
			select {
			case lines <- strings.TrimRight(line, "\r\n"):
			case <-quit:
				return
			}
		}
	}()
	var list []string
	inList, listOK := false, false
	for {
		var line string
		var ok bool
		select {
		case line, ok = <-lines:
		case <-c.s.done:
			return
		}
		if !ok {
			return
		}
		switch {
		case inList && line == "command_list_end":
			c.runList(list, listOK)
			list, inList = nil, false
		case inList:
			list = append(list, line)
		case line == "command_list_begin" || line == "command_list_ok_begin":
			inList, listOK = true, line == "command_list_ok_begin"
		default:
			args, err := splitArgs(line)
			if err != nil {
				c.ack(&ack{ackArg, err.Error()}, 0, "")
				break
			}
			if len(args) > 0 && args[0] == "idle" {
				if !c.idle(args[1:], lines) {
					return
				}
				break
			}
			if len(args) > 0 && args[0] == "close" {
				return
			}
			if err := c.run(args); err != nil {
				c.ack(err, 0, cmdName(args))
			} else {
				c.w.WriteString("OK\n")
			}
		}
		if err := c.w.Flush(); err != nil {
			return
		}
	}
}

// cmdName es el nombre del comando (para los errores).
func cmdName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// runList ejecuta una lista de comandos; para en el primer error.
func (c *conn) runList(list []string, listOK bool) {
	for i, line := range list {
		args, err := splitArgs(line)
		if err == nil {
			err = c.run(args)
		}
		if err != nil {
			c.ack(err, i, cmdName(args))
			return
		}
		if listOK {
			c.w.WriteString("list_OK\n")
		}
	}
	c.w.WriteString("OK\n")
}

// ack escribe un error.
func (c *conn) ack(err error, n int, cmd string) {
	code := ackSystem
	var a *ack
	if errors.As(err, &a) {
		code = a.code
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	fmt.Fprintf(c.w, "ACK [%d@%d] {%s} %s\n", code, n, cmd, msg)
}

// idle espera a que cambie algo de subsystems (todo si está vacío) o a
// que llegue noidle. Devuelve false si el cliente se fue.
func (c *conn) idle(subsystems []string, lines <-chan string) bool {
	if !c.authed {
		c.ack(&ack{ackPermission, "you don't have permission for \"idle\""}, 0, "idle")
		return true
	}
	want := func(e string) bool {
		if len(subsystems) == 0 {
			return true
		}
		for _, s := range subsystems {
			if strings.EqualFold(s, e) {
				return true
			}
		}
		return false
	}
	// take saca lo pendiente que interesa.
	take := func() []string {
		c.mu.Lock()
		defer c.mu.Unlock()
		var out []string
		for e := range c.pending {
			if want(e) {
				out = append(out, e)
				delete(c.pending, e)
			}
		}
		return out
	}
	reply := func(ev []string) bool {
		for _, e := range ev {
			fmt.Fprintf(c.w, "changed: %s\n", e)
		}
		c.w.WriteString("OK\n")
		return c.w.Flush() == nil
	}
	for {
		if ev := take(); len(ev) > 0 {
			return reply(ev)
		}
		select {
		case <-c.wake:
		case line, ok := <-lines:
			if !ok {
				return false
			}
			if strings.TrimSpace(line) != "noidle" {
				// Durante idle solo vale noidle; MPD cierra la conexión.
				return false
			}
			return reply(take())
		case <-c.s.done:
			return false
		}
	}
}

// splitArgs separa una línea en argumentos: palabras, o texto entre
// comillas dobles con \ de escape.
func splitArgs(line string) ([]string, error) {
	var out []string
	i := 0
	for {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			return out, nil
		}
		if line[i] == '"' {
			i++
			var b strings.Builder
			closed := false
			for i < len(line) {
				ch := line[i]
				i++
				if ch == '\\' && i < len(line) {
					b.WriteByte(line[i])
					i++
					continue
				}
				if ch == '"' {
					closed = true
					break
				}
				b.WriteByte(ch)
			}
			if !closed {
				return nil, errors.New("missing closing '\"'")
			}
			out = append(out, b.String())
			continue
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		out = append(out, line[start:i])
	}
}

// quote escribe un argumento para mandarlo (lo usa el cliente).
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
