package mpd

import (
	"cmp"
	"crypto/subtle"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
)

// handler atiende un comando: escribe su respuesta (sin el OK final) en c.w.
type handler func(c *conn, args []string) error

// handlers son los comandos que se entienden.
var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		// Estado y reproducción.
		"ping":             ok,
		"clearerror":       ok,
		"status":           (*conn).status,
		"currentsong":      (*conn).currentSong,
		"stats":            (*conn).stats,
		"play":             (*conn).play,
		"playid":           (*conn).playID,
		"pause":            (*conn).pause,
		"stop":             func(c *conn, _ []string) error { c.p().Stop(); return nil },
		"next":             func(c *conn, _ []string) error { c.p().Next(); return nil },
		"previous":         func(c *conn, _ []string) error { c.p().Prev(); return nil },
		"seek":             (*conn).seek,
		"seekid":           (*conn).seekID,
		"seekcur":          (*conn).seekCur,
		"setvol":           (*conn).setVol,
		"volume":           (*conn).volume,
		"getvol":           (*conn).getVol,
		"random":           (*conn).random,
		"repeat":           (*conn).repeat,
		"single":           (*conn).single,
		"consume":          ok,
		"crossfade":        ok,
		"mixrampdb":        ok,
		"mixrampdelay":     ok,
		"replay_gain_mode": ok,
		"replay_gain_status": func(c *conn, _ []string) error {
			c.w.WriteString("replay_gain_mode: off\n")
			return nil
		},
		// La cola.
		"add":            (*conn).add,
		"addid":          (*conn).addID,
		"addtagid":       (*conn).addTagID,
		"cleartagid":     (*conn).clearTagID,
		"clear":          func(c *conn, _ []string) error { c.p().Clear(); return nil },
		"delete":         (*conn).delete,
		"deleteid":       (*conn).deleteID,
		"move":           (*conn).move,
		"moveid":         (*conn).moveID,
		"swap":           (*conn).swap,
		"swapid":         (*conn).swapID,
		"shuffle":        (*conn).shuffle,
		"playlist":       (*conn).playlist,
		"playlistinfo":   (*conn).playlistInfo,
		"playlistid":     (*conn).playlistID,
		"plchanges":      (*conn).plChanges,
		"plchangesposid": (*conn).plChangesPosID,
		"playlistfind":   func(c *conn, args []string) error { return c.playlistSearch(args, false) },
		"playlistsearch": func(c *conn, args []string) error { return c.playlistSearch(args, true) },
		"prio":           ok,
		"prioid":         ok,
		"rangeid":        ok,
		// La biblioteca.
		"lsinfo":      (*conn).lsinfo,
		"listall":     func(c *conn, args []string) error { return c.listAll(args, false) },
		"listallinfo": func(c *conn, args []string) error { return c.listAll(args, true) },
		"listfiles":   (*conn).listFiles,
		"find":        func(c *conn, args []string) error { return c.find(args, false, false) },
		"search":      func(c *conn, args []string) error { return c.find(args, true, false) },
		"findadd":     func(c *conn, args []string) error { return c.find(args, false, true) },
		"searchadd":   func(c *conn, args []string) error { return c.find(args, true, true) },
		"searchaddpl": (*conn).searchAddPl,
		"list":        (*conn).list,
		"count":       (*conn).count,
		"update":      (*conn).update,
		"rescan":      (*conn).update,
		"albumart":    func(c *conn, args []string) error { return c.picture(args, false) },
		"readpicture": func(c *conn, args []string) error { return c.picture(args, true) },
		// Las listas guardadas.
		"listplaylists":    (*conn).listPlaylists,
		"listplaylist":     func(c *conn, args []string) error { return c.listPlaylist(args, false) },
		"listplaylistinfo": func(c *conn, args []string) error { return c.listPlaylist(args, true) },
		"load":             (*conn).load,
		"save":             (*conn).save,
		"rm":               (*conn).rm,
		"rename":           (*conn).rename,
		"playlistadd":      (*conn).playlistAdd,
		"playlistclear":    (*conn).playlistClear,
		"playlistdelete":   (*conn).playlistDelete,
		"playlistmove":     (*conn).playlistMove,
		// La conexión y lo que hay.
		"password":    (*conn).password,
		"binarylimit": (*conn).binaryLimit,
		"tagtypes":    (*conn).tagTypes,
		"commands":    (*conn).commands,
		"notcommands": ok,
		"urlhandlers": func(c *conn, _ []string) error {
			c.w.WriteString("handler: http://\nhandler: https://\n")
			return nil
		},
		"decoders":      (*conn).decoders,
		"outputs":       (*conn).outputs,
		"enableoutput":  ok,
		"disableoutput": ok,
		"toggleoutput":  ok,
		"channels":      ok,
		"subscribe":     ok,
		"unsubscribe":   ok,
		"readmessages":  ok,
		"sendmessage":   ok,
		"listmounts":    ok,
		"listneighbors": ok,
		"config": func(*conn, []string) error {
			return &ack{ackPermission, "you don't have permission for \"config\""}
		},
	}
}

// ok es un comando que se acepta y no hace nada (crossfade, salidas...).
func ok(*conn, []string) error { return nil }

// open son los comandos que valen sin contraseña.
// queueCmds son los comandos que cambian la cola o lo que suena: quien los
// manda pasa a ser el que le manda música (Server.Receiving).
var queueCmds = map[string]bool{"add": true, "addid": true, "addtagid": true, "cleartagid": true, "clear": true,
	"delete": true, "deleteid": true, "move": true, "moveid": true, "swap": true, "swapid": true, "shuffle": true,
	"load": true, "findadd": true, "searchadd": true, "play": true, "playid": true}

var open = map[string]bool{"password": true, "ping": true, "commands": true, "notcommands": true, "tagtypes": true, "binarylimit": true}

// run ejecuta un comando.
func (c *conn) run(args []string) error {
	if len(args) == 0 {
		return &ack{ackUnknown, "No command given"}
	}
	name := args[0]
	h := handlers[name]
	if h == nil {
		return &ack{ackUnknown, fmt.Sprintf("unknown command %q", name)}
	}
	if !c.authed && !open[name] {
		return &ack{ackPermission, fmt.Sprintf("you don't have permission for %q", name)}
	}
	if err := h(c, args[1:]); err != nil {
		return err
	}
	if queueCmds[name] {
		c.s.owned(c)
	}
	return nil
}

func (c *conn) p() Player { return c.s.opts.Player }

// field escribe "key: value".
func (c *conn) field(key string, v any) { fmt.Fprintf(c.w, "%s: %v\n", key, v) }

// boolArg lee "0" o "1".
func boolArg(args []string) (bool, error) {
	if len(args) != 1 || (args[0] != "0" && args[0] != "1") {
		return false, errArg("Boolean (0/1) expected")
	}
	return args[0] == "1", nil
}

// intArg lee un número entero.
func intArg(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errArg("Integer expected: %s", s)
	}
	return n, nil
}

// rangeArg lee "N" o "START:END" (END vacío: hasta el final) contra n
// elementos; devuelve [start, end).
func rangeArg(s string, n int) (int, int, error) {
	a, b, isRange := strings.Cut(s, ":")
	start, err := intArg(a)
	if err != nil || start < 0 {
		return 0, 0, errArg("Bad song index: %s", s)
	}
	end := start + 1
	if isRange {
		end = n
		if b != "" {
			if end, err = intArg(b); err != nil {
				return 0, 0, err
			}
		}
	}
	if end > n {
		end = n
	}
	if start > end || (start >= n && !(isRange && start == n)) {
		return 0, 0, errArg("Bad song index")
	}
	return start, end, nil
}

// --- Estado ---

func (c *conn) status(_ []string) error {
	st := c.p().Status()
	n := len(c.p().QueueIDs())
	ids := c.p().QueueIDs()
	c.field("volume", st.Volume)
	c.field("repeat", b01(st.Repeat))
	c.field("random", b01(st.Random))
	c.field("single", b01(st.Single))
	c.field("consume", 0)
	c.field("playlist", st.Version+1)
	c.field("playlistlength", n)
	c.field("mixrampdb", "0.000000")
	c.field("state", st.State)
	if st.Pos >= 0 && st.Pos < len(ids) {
		c.field("song", st.Pos)
		c.field("songid", ids[st.Pos])
		if st.State != "stop" {
			c.field("time", fmt.Sprintf("%d:%d", int(math.Round(st.Elapsed)), int(math.Round(st.Duration))))
			c.field("elapsed", fmt.Sprintf("%.3f", st.Elapsed))
			if st.Duration > 0 {
				c.field("duration", fmt.Sprintf("%.3f", st.Duration))
			}
			c.field("bitrate", 0)
			c.field("audio", "48000:16:2")
		}
		next := st.Pos + 1
		if next >= n && st.Repeat && !st.Single {
			next = 0
		}
		if !st.Random && !st.Single && next < n {
			c.field("nextsong", next)
			c.field("nextsongid", ids[next])
		}
	}
	return nil
}

func b01(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *conn) currentSong(_ []string) error {
	st := c.p().Status()
	q, ids := c.p().Queue(), c.p().QueueIDs()
	if st.Pos < 0 || st.Pos >= len(q) || st.Pos >= len(ids) {
		return nil
	}
	c.queueSong(q[st.Pos], st.Pos, ids[st.Pos], st)
	return nil
}

func (c *conn) stats(_ []string) error {
	idx := c.s.lib.get()
	artists, albums := map[string]bool{}, map[string]bool{}
	for _, s := range idx.songs {
		artists[s.tags.Artist] = true
		albums[s.tags.get("AlbumArtist")+"\x00"+s.tags.Album] = true
	}
	c.field("artists", len(artists))
	c.field("albums", len(albums))
	c.field("songs", len(idx.songs))
	c.field("uptime", int(time.Since(c.s.start).Seconds()))
	c.field("db_playtime", 0)
	c.field("db_update", idx.when.Unix())
	c.field("playtime", 0)
	return nil
}

// --- Reproducción ---

func (c *conn) play(args []string) error {
	if len(args) == 0 {
		c.p().Play()
		return nil
	}
	pos, err := intArg(args[0])
	if err != nil {
		return err
	}
	if pos < 0 || pos >= len(c.p().QueueIDs()) {
		return errArg("Bad song index")
	}
	c.p().Jump(pos)
	return nil
}

// posOfID es la posición del id en la cola.
func (c *conn) posOfID(s string) (int, error) {
	id, err := intArg(s)
	if err != nil {
		return 0, err
	}
	if i := slices.Index(c.p().QueueIDs(), id); i >= 0 {
		return i, nil
	}
	return 0, errNoExist("No such song")
}

func (c *conn) playID(args []string) error {
	if len(args) == 0 {
		c.p().Play()
		return nil
	}
	pos, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	c.p().Jump(pos)
	return nil
}

func (c *conn) pause(args []string) error {
	if len(args) == 0 {
		if c.p().Status().State == "play" {
			c.p().Pause()
		} else {
			c.p().Play()
		}
		return nil
	}
	on, err := boolArg(args)
	if err != nil {
		return err
	}
	if on {
		c.p().Pause()
	} else {
		c.p().Play()
	}
	return nil
}

// seekTo va a la posición pos (saltando a esa canción si no es la actual) y
// al segundo t.
func (c *conn) seekTo(pos int, t string) error {
	sec, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return errArg("Float expected: %s", t)
	}
	if pos < 0 || pos >= len(c.p().QueueIDs()) {
		return errArg("Bad song index")
	}
	if c.p().Status().Pos != pos {
		c.p().Jump(pos)
	}
	c.p().SeekTo(math.Max(sec, 0))
	return nil
}

func (c *conn) seek(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"seek\"")
	}
	pos, err := intArg(args[0])
	if err != nil {
		return err
	}
	return c.seekTo(pos, args[1])
}

func (c *conn) seekID(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"seekid\"")
	}
	pos, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	return c.seekTo(pos, args[1])
}

func (c *conn) seekCur(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"seekcur\"")
	}
	sec, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return errArg("Float expected: %s", args[0])
	}
	if strings.HasPrefix(args[0], "+") || strings.HasPrefix(args[0], "-") {
		sec += c.p().Status().Elapsed
	}
	c.p().SeekTo(math.Max(sec, 0))
	return nil
}

func (c *conn) setVol(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"setvol\"")
	}
	v, err := intArg(args[0])
	if err != nil {
		return err
	}
	if v < 0 || v > 100 {
		return errArg("Invalid volume value")
	}
	c.p().SetVolume(v)
	return nil
}

func (c *conn) volume(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"volume\"")
	}
	d, err := intArg(args[0])
	if err != nil {
		return err
	}
	c.p().SetVolume(min(100, max(0, c.p().Status().Volume+d)))
	return nil
}

func (c *conn) getVol(_ []string) error {
	c.field("volume", c.p().Status().Volume)
	return nil
}

func (c *conn) random(args []string) error {
	on, err := boolArg(args)
	if err != nil {
		return err
	}
	c.p().SetRandom(on)
	return nil
}

func (c *conn) repeat(args []string) error {
	on, err := boolArg(args)
	if err != nil {
		return err
	}
	c.p().SetRepeat(on, c.p().Status().Single)
	return nil
}

func (c *conn) single(args []string) error {
	if len(args) == 1 && args[0] == "oneshot" {
		args = []string{"1"}
	}
	on, err := boolArg(args)
	if err != nil {
		return err
	}
	c.p().SetRepeat(c.p().Status().Repeat, on)
	return nil
}

// --- Canciones ---

// info es lo que se sabe de una ruta de la cola: su URI, sus etiquetas y,
// si es de la biblioteca, la canción.
func (c *conn) info(p string) (string, Tags, *song) {
	idx := c.s.lib.get()
	if s := idx.byPath[p]; s != nil {
		return s.uri, s.tags, s
	}
	if IsPath(p) {
		if uri, _, ok := c.s.fetch.Entry(p); ok {
			t, _ := c.s.Tags(p)
			return uri, t, nil
		}
	}
	t, ok := c.p().Tags(p)
	if !ok {
		t.Title = strings.TrimSuffix(filepath.Base(decode.FileOf(p)), filepath.Ext(decode.FileOf(p)))
	}
	return p, t, nil
}

// songBlock escribe una canción: file, Last-Modified, etiquetas y duración
// (si se sabe).
func (c *conn) songBlock(uri string, t Tags, s *song, dur float64) {
	c.field("file", uri)
	if s != nil && !s.mtime.IsZero() {
		c.field("Last-Modified", s.mtime.UTC().Format(time.RFC3339))
	}
	for _, name := range tagNames {
		v := t.get(name)
		if name == "AlbumArtist" {
			v = t.AlbumArtist // sin la de reserva (el Artist)
		}
		if v != "" {
			c.field(name, v)
		}
	}
	if dur > 0 {
		c.field("Time", int(math.Round(dur)))
		c.field("duration", fmt.Sprintf("%.3f", dur))
	}
}

// queueSong escribe una canción de la cola, con su Pos e Id.
func (c *conn) queueSong(p string, pos, id int, st Status) {
	uri, t, s := c.info(p)
	dur := 0.0
	if pos == st.Pos {
		dur = st.Duration
	}
	c.songBlock(uri, t, s, dur)
	c.field("Pos", pos)
	c.field("Id", id)
}

// --- La cola ---

// resolve convierte una URI de un cliente en rutas para la cola: una
// canción o un directorio de la biblioteca, o una URL.
func (c *conn) resolve(uri string) ([]string, error) {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return []string{c.s.addURL(uri)}, nil
	}
	idx := c.s.lib.get()
	uri = strings.TrimSuffix(strings.TrimPrefix(uri, "/"), "/")
	if s := idx.byURI[uri]; s != nil {
		return []string{s.path}, nil
	}
	if d := idx.dirs[uri]; d != nil {
		var out []string
		d.walk(func(s *song) { out = append(out, s.path) })
		return out, nil
	}
	// Una ruta del propio equipo, si es de la biblioteca.
	p := strings.TrimPrefix(uri, "file://")
	if s := idx.byPath[filepath.FromSlash("/"+strings.TrimPrefix(p, "/"))]; s != nil {
		return []string{s.path}, nil
	}
	return nil, errNoExist("No such directory")
}

// insertAt es la posición de "add URI POS": absoluta, o relativa a la
// actual ("+N" detrás, "-N" delante). -1: al final.
func (c *conn) insertAt(args []string) (int, error) {
	if len(args) == 0 {
		return -1, nil
	}
	s := args[0]
	cur := c.p().Status().Pos
	n, err := intArg(strings.TrimPrefix(s, "+"))
	if err != nil {
		return 0, err
	}
	switch {
	case strings.HasPrefix(s, "+"):
		n = cur + 1 + n
	case strings.HasPrefix(s, "-"):
		n = cur + n // n ya es negativo
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// addPaths pone paths en la cola en at (-1: al final) y devuelve el id de
// la primera.
func (c *conn) addPaths(paths []string, at int) int {
	if len(paths) == 0 {
		return -1
	}
	before := len(c.p().QueueIDs())
	c.p().Append(paths)
	ids := c.p().QueueIDs()
	if at >= 0 && at < before {
		for i := range paths {
			c.p().Move(before+i, at+i)
		}
		ids = c.p().QueueIDs()
		if at < len(ids) {
			return ids[at]
		}
	}
	if before < len(ids) {
		return ids[before]
	}
	return -1
}

func (c *conn) add(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"add\"")
	}
	paths, err := c.resolve(args[0])
	if err != nil {
		return err
	}
	at, err := c.insertAt(args[1:])
	if err != nil {
		return err
	}
	c.addPaths(paths, at)
	return nil
}

func (c *conn) addID(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"addid\"")
	}
	paths, err := c.resolve(args[0])
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return errNoExist("No such song")
	}
	at, err := c.insertAt(args[1:])
	if err != nil {
		return err
	}
	c.field("Id", c.addPaths(paths, at))
	return nil
}

// pathOfID es la ruta de la cola con ese id.
func (c *conn) pathOfID(s string) (string, error) {
	pos, err := c.posOfID(s)
	if err != nil {
		return "", err
	}
	q := c.p().Queue()
	if pos >= len(q) {
		return "", errNoExist("No such song")
	}
	return q[pos], nil
}

func (c *conn) addTagID(args []string) error {
	if len(args) != 3 {
		return errArg("wrong number of arguments for \"addtagid\"")
	}
	p, err := c.pathOfID(args[0])
	if err != nil {
		return err
	}
	tag := canonTag(args[1])
	if tag == "" || tag == "file" || tag == "base" || tag == "any" || tag == "modified-since" {
		return errArg("Unknown tag type: %s", args[1])
	}
	if !c.s.setURLTag(p, tag, args[2], false) {
		return &ack{ackArg, "Song is not a remote URL"}
	}
	c.s.notify("playlist", "player")
	return nil
}

func (c *conn) clearTagID(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"cleartagid\"")
	}
	p, err := c.pathOfID(args[0])
	if err != nil {
		return err
	}
	if !c.s.setURLTag(p, "", "", true) {
		return &ack{ackArg, "Song is not a remote URL"}
	}
	return nil
}

func (c *conn) delete(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"delete\"")
	}
	start, end, err := rangeArg(args[0], len(c.p().QueueIDs()))
	if err != nil {
		return err
	}
	for i := end - 1; i >= start; i-- {
		c.p().Remove(i)
	}
	return nil
}

func (c *conn) deleteID(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"deleteid\"")
	}
	pos, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	c.p().Remove(pos)
	return nil
}

func (c *conn) move(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"move\"")
	}
	n := len(c.p().QueueIDs())
	start, end, err := rangeArg(args[0], n)
	if err != nil {
		return err
	}
	to, err := intArg(args[1])
	if err != nil {
		return err
	}
	if to < 0 || to+(end-start) > n {
		return errArg("Bad song index")
	}
	if to < start {
		for i := 0; i < end-start; i++ {
			c.p().Move(start+i, to+i)
		}
	} else {
		for i := end - start - 1; i >= 0; i-- {
			c.p().Move(start+i, to+i)
		}
	}
	return nil
}

func (c *conn) moveID(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"moveid\"")
	}
	from, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	to, err := intArg(args[1])
	if err != nil {
		return err
	}
	if to < 0 || to >= len(c.p().QueueIDs()) {
		return errArg("Bad song index")
	}
	c.p().Move(from, to)
	return nil
}

// swapPos cambia de sitio dos canciones.
func (c *conn) swapPos(a, b int) error {
	n := len(c.p().QueueIDs())
	if a < 0 || b < 0 || a >= n || b >= n {
		return errArg("Bad song index")
	}
	if a == b {
		return nil
	}
	if a > b {
		a, b = b, a
	}
	c.p().Move(b, a)
	c.p().Move(a+1, b)
	return nil
}

func (c *conn) swap(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"swap\"")
	}
	a, err := intArg(args[0])
	if err != nil {
		return err
	}
	b, err := intArg(args[1])
	if err != nil {
		return err
	}
	return c.swapPos(a, b)
}

func (c *conn) swapID(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"swapid\"")
	}
	a, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	b, err := c.posOfID(args[1])
	if err != nil {
		return err
	}
	return c.swapPos(a, b)
}

func (c *conn) shuffle(args []string) error {
	n := len(c.p().QueueIDs())
	start, end := 0, n
	if len(args) > 0 {
		var err error
		if start, end, err = rangeArg(args[0], n); err != nil {
			return err
		}
	}
	for i := end - 1; i > start; i-- {
		j := start + rand.IntN(i-start+1)
		if j != i {
			c.p().Move(j, i)
		}
	}
	return nil
}

// queueRange escribe las canciones de la cola de [start, end).
func (c *conn) queueRange(start, end int) {
	st := c.p().Status()
	q, ids := c.p().Queue(), c.p().QueueIDs()
	for i := start; i < end && i < len(q) && i < len(ids); i++ {
		c.queueSong(q[i], i, ids[i], st)
	}
}

func (c *conn) playlist(_ []string) error {
	for i, p := range c.p().Queue() {
		uri, _, _ := c.info(p)
		fmt.Fprintf(c.w, "%d:file: %s\n", i, uri)
	}
	return nil
}

func (c *conn) playlistInfo(args []string) error {
	n := len(c.p().QueueIDs())
	start, end := 0, n
	if len(args) > 0 {
		var err error
		if start, end, err = rangeArg(args[0], n); err != nil {
			return err
		}
	}
	c.queueRange(start, end)
	return nil
}

func (c *conn) playlistID(args []string) error {
	if len(args) == 0 {
		return c.playlistInfo(nil)
	}
	pos, err := c.posOfID(args[0])
	if err != nil {
		return err
	}
	c.queueRange(pos, pos+1)
	return nil
}

// changedSince dice si la cola cambió desde la versión v de un cliente (no
// se guarda qué cambió: si cambió, se manda entera).
func (c *conn) changedSince(args []string) (bool, int, int, error) {
	if len(args) < 1 {
		return false, 0, 0, errArg("wrong number of arguments")
	}
	v, err := intArg(args[0])
	if err != nil {
		return false, 0, 0, err
	}
	n := len(c.p().QueueIDs())
	start, end := 0, n
	if len(args) > 1 {
		if start, end, err = rangeArg(args[1], n); err != nil {
			return false, 0, 0, err
		}
	}
	return v != c.p().Status().Version+1, start, end, nil
}

func (c *conn) plChanges(args []string) error {
	changed, start, end, err := c.changedSince(args)
	if err == nil && changed {
		c.queueRange(start, end)
	}
	return err
}

func (c *conn) plChangesPosID(args []string) error {
	changed, start, end, err := c.changedSince(args)
	if err != nil || !changed {
		return err
	}
	ids := c.p().QueueIDs()
	for i := start; i < end && i < len(ids); i++ {
		c.field("cpos", i)
		c.field("Id", ids[i])
	}
	return nil
}

func (c *conn) playlistSearch(args []string, fold bool) error {
	f, _, err := parseFilter(args, fold)
	if err != nil {
		return errArg("%v", err)
	}
	st := c.p().Status()
	q, ids := c.p().Queue(), c.p().QueueIDs()
	for i, p := range q {
		uri, t, _ := c.info(p)
		if i < len(ids) && f(uri, t) {
			c.queueSong(p, i, ids[i], st)
		}
	}
	return nil
}

// --- La biblioteca ---

func (c *conn) lsinfo(args []string) error {
	idx := c.s.lib.get()
	uri := ""
	if len(args) > 0 {
		uri = strings.Trim(args[0], "/")
	}
	if s := idx.byURI[uri]; s != nil {
		c.songBlock(s.uri, s.tags, s, 0)
		return nil
	}
	d := idx.dirs[uri]
	if d == nil {
		return errNoExist("No such directory")
	}
	for _, sub := range d.subs {
		c.field("directory", sub.uri)
		if !sub.mtime.IsZero() {
			c.field("Last-Modified", sub.mtime.UTC().Format(time.RFC3339))
		}
	}
	for _, s := range d.songs {
		c.songBlock(s.uri, s.tags, s, 0)
	}
	if uri == "" {
		for _, l := range idx.lists {
			c.field("playlist", l.name)
			c.field("Last-Modified", l.mtime.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

func (c *conn) listAll(args []string, info bool) error {
	idx := c.s.lib.get()
	uri := ""
	if len(args) > 0 {
		uri = strings.Trim(args[0], "/")
	}
	if s := idx.byURI[uri]; s != nil {
		if info {
			c.songBlock(s.uri, s.tags, s, 0)
		} else {
			c.field("file", s.uri)
		}
		return nil
	}
	d := idx.dirs[uri]
	if d == nil {
		return errNoExist("No such directory")
	}
	var walk func(d *dir)
	walk = func(d *dir) {
		for _, s := range d.songs {
			if info {
				c.songBlock(s.uri, s.tags, s, 0)
			} else {
				c.field("file", s.uri)
			}
		}
		for _, sub := range d.subs {
			c.field("directory", sub.uri)
			walk(sub)
		}
	}
	walk(d)
	return nil
}

func (c *conn) listFiles(args []string) error {
	idx := c.s.lib.get()
	uri := ""
	if len(args) > 0 {
		uri = strings.Trim(args[0], "/")
	}
	d := idx.dirs[uri]
	if d == nil {
		return errNoExist("No such directory")
	}
	for _, sub := range d.subs {
		c.field("directory", path.Base(sub.uri))
	}
	seen := map[string]bool{}
	for _, s := range d.songs {
		name := path.Base(filepath.ToSlash(decode.FileOf(s.path)))
		if !seen[name] {
			seen[name] = true
			c.field("file", name)
		}
	}
	return nil
}

// matches son las canciones de la biblioteca que cumplen un filtro, con
// sort y window aplicados.
func (c *conn) matches(args []string, fold bool) ([]*song, error) {
	f, rest, err := parseFilter(args, fold)
	if err != nil {
		return nil, errArg("%v", err)
	}
	var out []*song
	for _, s := range c.s.lib.get().songs {
		if f(s.uri, s.tags) {
			out = append(out, s)
		}
	}
	for i := 0; i+1 < len(rest); i += 2 {
		switch strings.ToLower(rest[i]) {
		case "sort":
			key, desc := rest[i+1], false
			if strings.HasPrefix(key, "-") {
				key, desc = key[1:], true
			}
			tag := canonTag(key)
			slices.SortStableFunc(out, func(a, b *song) int {
				var r int
				if tag == "Track" {
					x, _ := strconv.Atoi(a.tags.Track)
					y, _ := strconv.Atoi(b.tags.Track)
					r = cmp.Compare(x, y)
				} else {
					r = strings.Compare(strings.ToLower(valueOf(tag, a.uri, a.tags)[0]), strings.ToLower(valueOf(tag, b.uri, b.tags)[0]))
				}
				if desc {
					r = -r
				}
				return r
			})
		case "window":
			start, end, err := rangeArg(rest[i+1], len(out))
			if err != nil {
				if len(out) == 0 {
					continue
				}
				return nil, err
			}
			out = out[start:end]
		}
	}
	return out, nil
}

func (c *conn) find(args []string, fold, add bool) error {
	songs, err := c.matches(args, fold)
	if err != nil {
		return err
	}
	if add {
		var paths []string
		for _, s := range songs {
			paths = append(paths, s.path)
		}
		c.addPaths(paths, -1)
		return nil
	}
	for _, s := range songs {
		c.songBlock(s.uri, s.tags, s, 0)
	}
	return nil
}

func (c *conn) searchAddPl(args []string) error {
	if len(args) < 2 {
		return errArg("wrong number of arguments for \"searchaddpl\"")
	}
	songs, err := c.matches(args[1:], true)
	if err != nil {
		return err
	}
	var paths []string
	for _, s := range songs {
		paths = append(paths, s.path)
	}
	return c.s.appendToList(args[0], paths)
}

func (c *conn) list(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"list\"")
	}
	tag := canonTag(args[0])
	if tag == "" || tag == "any" || tag == "base" || tag == "modified-since" {
		return errArg("Unknown tag type: %s", args[0])
	}
	rest := args[1:]
	// El "list album <artista>" de los clientes viejos.
	if tag == "Album" && len(rest) == 1 && !strings.HasPrefix(rest[0], "(") {
		rest = []string{"artist", rest[0]}
	}
	f, extra, err := parseFilter(rest, false)
	if err != nil {
		return errArg("%v", err)
	}
	var groups []string
	for i := 0; i+1 < len(extra); i += 2 {
		if strings.EqualFold(extra[i], "group") {
			g := canonTag(extra[i+1])
			if g == "" || g == "any" || g == "base" {
				return errArg("Unknown tag type: %s", extra[i+1])
			}
			groups = append(groups, g)
		}
	}
	type row []string
	seen := map[string]bool{}
	var rows []row
	for _, s := range c.s.lib.get().songs {
		if !f(s.uri, s.tags) {
			continue
		}
		r := make(row, 0, len(groups)+1)
		for _, g := range groups {
			r = append(r, valueOf(g, s.uri, s.tags)[0])
		}
		v := valueOf(tag, s.uri, s.tags)[0]
		if v == "" {
			continue
		}
		r = append(r, v)
		k := strings.Join(r, "\x00")
		if !seen[k] {
			seen[k] = true
			rows = append(rows, r)
		}
	}
	slices.SortFunc(rows, func(a, b row) int { return slices.Compare(a, b) })
	var prev row
	for _, r := range rows {
		for i, g := range groups {
			if prev == nil || slices.Compare(prev[:i+1], r[:i+1]) != 0 {
				c.field(g, r[i])
			}
		}
		c.field(tag, r[len(r)-1])
		prev = r
	}
	return nil
}

func (c *conn) count(args []string) error {
	f, extra, err := parseFilter(args, false)
	if err != nil {
		return errArg("%v", err)
	}
	group := ""
	if len(extra) >= 2 && strings.EqualFold(extra[0], "group") {
		group = canonTag(extra[1])
	}
	counts := map[string]int{}
	var keys []string
	for _, s := range c.s.lib.get().songs {
		if !f(s.uri, s.tags) {
			continue
		}
		k := ""
		if group != "" {
			k = valueOf(group, s.uri, s.tags)[0]
		}
		if _, ok := counts[k]; !ok {
			keys = append(keys, k)
		}
		counts[k]++
	}
	if group == "" {
		c.field("songs", counts[""])
		c.field("playtime", 0)
		return nil
	}
	slices.Sort(keys)
	for _, k := range keys {
		c.field(group, k)
		c.field("songs", counts[k])
		c.field("playtime", 0)
	}
	return nil
}

// update no escanea (lo hace la interfaz): relee data/library.json.
func (c *conn) update(_ []string) error {
	c.field("updating_db", 1)
	c.s.Changed()
	return nil
}

// coverNames son las portadas sueltas que se buscan en la carpeta.
var coverNames = []string{"cover.jpg", "folder.jpg", "front.jpg", "cover.png", "folder.png", "front.png", "Cover.jpg", "Folder.jpg", "AlbumArt.jpg"}

// picture atiende albumart (la portada suelta: <archivo>.jpg o la de la
// carpeta, y si no, la incrustada) y readpicture (la incrustada), a trozos
// desde offset.
func (c *conn) picture(args []string, embedded bool) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments")
	}
	off, err := intArg(args[1])
	if err != nil || off < 0 {
		return errArg("Bad offset")
	}
	data := c.coverData(args[0], embedded)
	if data == nil {
		if embedded {
			return nil
		}
		return errNoExist("No file exists")
	}
	if off > len(data) {
		return errArg("Bad file offset")
	}
	chunk := data[off:min(len(data), off+c.binLimit)]
	c.field("size", len(data))
	if embedded {
		c.field("type", http.DetectContentType(data))
	}
	c.field("binary", len(chunk))
	c.w.Write(chunk)
	c.w.WriteString("\n")
	return nil
}

// coverData es la portada de uri (con la última guardada, porque los
// clientes la piden a trozos).
func (c *conn) coverData(uri string, embedded bool) []byte {
	key := fmt.Sprint(embedded) + uri
	if c.coverKey == key {
		return c.cover
	}
	s := c.s.lib.get().byURI[strings.Trim(uri, "/")]
	if s == nil {
		return nil
	}
	file := decode.FileOf(s.path)
	var data []byte
	if !embedded {
		for _, p := range append([]string{file + ".jpg"}, coverNames...) {
			if !strings.HasPrefix(p, file) {
				p = filepath.Join(filepath.Dir(file), p)
			}
			if b, err := os.ReadFile(p); err == nil {
				data = b
				break
			}
		}
	}
	if data == nil && s.pic {
		if f, err := os.Open(file); err == nil {
			if md, err := tag.ReadFrom(f); err == nil && md.Picture() != nil {
				data = md.Picture().Data
			}
			f.Close()
		}
	}
	c.coverKey, c.cover = key, data
	return data
}

// --- Las listas guardadas ---

// listPaths son las rutas de una lista guardada.
func listPaths(file string) []string {
	paths, _ := library.Expand([]string{file})
	return paths
}

func (c *conn) listPlaylists(_ []string) error {
	for _, l := range c.s.lib.get().lists {
		c.field("playlist", l.name)
		c.field("Last-Modified", l.mtime.UTC().Format(time.RFC3339))
	}
	return nil
}

// storedList es la lista name, o el error de que no existe.
func (c *conn) storedList(name string) (storedList, error) {
	l, ok := c.s.lib.get().list(name)
	if !ok {
		return l, errNoExist("No such playlist")
	}
	return l, nil
}

func (c *conn) listPlaylist(args []string, info bool) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments")
	}
	l, err := c.storedList(args[0])
	if err != nil {
		return err
	}
	paths := listPaths(l.file)
	start, end := 0, len(paths)
	if len(args) > 1 {
		if start, end, err = rangeArg(args[1], len(paths)); err != nil {
			return err
		}
	}
	for _, p := range paths[start:end] {
		uri, t, s := c.info(p)
		if info {
			c.songBlock(uri, t, s, 0)
		} else {
			c.field("file", uri)
		}
	}
	return nil
}

func (c *conn) load(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"load\"")
	}
	l, err := c.storedList(args[0])
	if err != nil {
		return err
	}
	paths := listPaths(l.file)
	if len(args) > 1 {
		start, end, err := rangeArg(args[1], len(paths))
		if err != nil {
			return err
		}
		paths = paths[start:end]
	}
	at, err := c.insertAt(args[min(2, len(args)):])
	if err != nil {
		return err
	}
	c.addPaths(paths, at)
	return nil
}

// listFile es el archivo de una lista nueva, sin caracteres que no valgan
// en exFAT.
func (s *Server) listFile(name string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	return filepath.Join(s.lib.listsDir(), clean+".m3u8")
}

// writeList escribe una lista (.m3u8), con rutas relativas a su carpeta si
// están dentro, como la interfaz.
func (s *Server) writeList(file string, paths []string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	idx := s.lib.get()
	dir := filepath.Dir(file)
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, p := range paths {
		var t Tags
		if so := idx.byPath[p]; so != nil {
			t = so.tags
		}
		w := p
		if rel, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(rel, "..") {
			w = filepath.ToSlash(rel)
		}
		fmt.Fprintf(&b, "#EXTINF:-1,%s - %s\n%s\n", t.Artist, t.Title, w)
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		return err
	}
	s.lib.reloadLists()
	s.notify("stored_playlist")
	return nil
}

// appendToList añade paths a la lista name (creándola).
func (s *Server) appendToList(name string, paths []string) error {
	file := s.listFile(name)
	var old []string
	if l, ok := s.lib.get().list(name); ok {
		file, old = l.file, listPaths(l.file)
	}
	return s.writeList(file, append(old, paths...))
}

// localQueue son las rutas de la cola que se pueden guardar en una lista
// (no las bajadas por URL, que se borran).
func (c *conn) localQueue() []string {
	var out []string
	for _, p := range c.p().Queue() {
		if !IsPath(p) {
			out = append(out, p)
		}
	}
	return out
}

func (c *conn) save(args []string) error {
	if len(args) < 1 {
		return errArg("wrong number of arguments for \"save\"")
	}
	mode := "create"
	if len(args) > 1 {
		mode = args[1]
	}
	l, exists := c.s.lib.get().list(args[0])
	switch {
	case mode == "create" && exists:
		return &ack{ackExist, "Playlist already exists"}
	case mode == "append":
		return c.s.appendToList(args[0], c.localQueue())
	case mode == "replace" && exists:
		return c.s.writeList(l.file, c.localQueue())
	case mode != "create" && mode != "replace":
		return errArg("Unrecognized save mode: %s", mode)
	}
	return c.s.writeList(c.s.listFile(args[0]), c.localQueue())
}

func (c *conn) rm(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"rm\"")
	}
	l, err := c.storedList(args[0])
	if err != nil {
		return err
	}
	if err := os.Remove(l.file); err != nil {
		return &ack{ackSystem, err.Error()}
	}
	c.s.lib.reloadLists()
	c.s.notify("stored_playlist")
	return nil
}

func (c *conn) rename(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"rename\"")
	}
	l, err := c.storedList(args[0])
	if err != nil {
		return err
	}
	if _, exists := c.s.lib.get().list(args[1]); exists {
		return &ack{ackExist, "Playlist already exists"}
	}
	to := filepath.Join(filepath.Dir(l.file), filepath.Base(c.s.listFile(args[1])))
	if strings.EqualFold(filepath.Ext(l.file), ".m3u") {
		to = strings.TrimSuffix(to, ".m3u8") + ".m3u"
	}
	if err := os.Rename(l.file, to); err != nil {
		return &ack{ackSystem, err.Error()}
	}
	c.s.lib.reloadLists()
	c.s.notify("stored_playlist")
	return nil
}

// editList cambia las rutas de una lista guardada.
func (c *conn) editList(name string, edit func([]string) ([]string, error)) error {
	l, err := c.storedList(name)
	if err != nil {
		return err
	}
	paths, err := edit(listPaths(l.file))
	if err != nil {
		return err
	}
	return c.s.writeList(l.file, paths)
}

func (c *conn) playlistAdd(args []string) error {
	if len(args) < 2 {
		return errArg("wrong number of arguments for \"playlistadd\"")
	}
	if strings.HasPrefix(args[1], "http://") || strings.HasPrefix(args[1], "https://") {
		return errArg("URLs can't be saved in a playlist here")
	}
	paths, err := c.resolve(args[1])
	if err != nil {
		return err
	}
	if _, ok := c.s.lib.get().list(args[0]); !ok {
		return c.s.writeList(c.s.listFile(args[0]), paths)
	}
	return c.editList(args[0], func(old []string) ([]string, error) {
		at := len(old)
		if len(args) > 2 {
			n, err := intArg(args[2])
			if err != nil || n < 0 || n > len(old) {
				return nil, errArg("Bad position")
			}
			at = n
		}
		return slices.Insert(old, at, paths...), nil
	})
}

func (c *conn) playlistClear(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"playlistclear\"")
	}
	return c.editList(args[0], func([]string) ([]string, error) { return nil, nil })
}

func (c *conn) playlistDelete(args []string) error {
	if len(args) != 2 {
		return errArg("wrong number of arguments for \"playlistdelete\"")
	}
	return c.editList(args[0], func(old []string) ([]string, error) {
		start, end, err := rangeArg(args[1], len(old))
		if err != nil {
			return nil, err
		}
		return slices.Delete(old, start, end), nil
	})
}

func (c *conn) playlistMove(args []string) error {
	if len(args) != 3 {
		return errArg("wrong number of arguments for \"playlistmove\"")
	}
	return c.editList(args[0], func(old []string) ([]string, error) {
		from, err := intArg(args[1])
		if err != nil {
			return nil, err
		}
		to, err := intArg(args[2])
		if err != nil {
			return nil, err
		}
		if from < 0 || from >= len(old) || to < 0 || to >= len(old) {
			return nil, errArg("Bad song index")
		}
		p := old[from]
		old = slices.Delete(old, from, from+1)
		return slices.Insert(old, to, p), nil
	})
}

// --- La conexión ---

func (c *conn) password(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"password\"")
	}
	if subtle.ConstantTimeCompare([]byte(args[0]), []byte(c.s.opts.Password)) != 1 {
		return &ack{ackPassword, "incorrect password"}
	}
	c.authed = true
	return nil
}

func (c *conn) binaryLimit(args []string) error {
	if len(args) != 1 {
		return errArg("wrong number of arguments for \"binarylimit\"")
	}
	n, err := intArg(args[0])
	if err != nil || n < 64 {
		return errArg("Value too small")
	}
	c.binLimit = min(n, 1<<20)
	return nil
}

func (c *conn) tagTypes(args []string) error {
	if len(args) > 0 {
		return nil // clear, all, enable, disable: se mandan siempre todas
	}
	for _, t := range tagNames {
		c.field("tagtype", t)
	}
	return nil
}

func (c *conn) commands(_ []string) error {
	names := make([]string, 0, len(handlers)+3)
	for n := range handlers {
		if c.authed || open[n] {
			names = append(names, n)
		}
	}
	names = append(names, "close", "idle", "noidle")
	slices.Sort(names)
	for _, n := range names {
		c.field("command", n)
	}
	return nil
}

func (c *conn) decoders(_ []string) error {
	c.field("plugin", "audiofilus")
	for _, s := range []string{"mp3", "flac", "ogg", "oga", "opus", "m4a", "m4b", "aac", "wav", "aiff", "aif", "mod", "xm", "s3m", "it"} {
		c.field("suffix", s)
	}
	return nil
}

func (c *conn) outputs(_ []string) error {
	c.field("outputid", 0)
	c.field("outputname", "audiofilus")
	c.field("plugin", "audiofilus")
	c.field("outputenabled", 1)
	return nil
}
