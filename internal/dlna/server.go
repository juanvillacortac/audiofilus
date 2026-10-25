package dlna

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dhowden/tag"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// El servidor de medios: la biblioteca en el ContentDirectory (Artistas →
// Álbumes → Canciones, Álbumes, Canciones, Listas y Carpetas) y cada canción
// por HTTP con Range. Los formatos que una tele o una app DLNA no conocen
// (DSD, módulos, música de consolas, pistas de un CUE) se sirven
// decodificados, en WAV de 16 bits.

// Los contenedores de la raíz.
var rootContainers = []struct{ id, title string }{
	{"artists", "Artists"},
	{"albums", "Albums"},
	{"songs", "Songs"},
	{"playlists", "Playlists"},
	{"folders", "Folders"},
}

// mediaServer es el ContentDirectory y lo que sirve.
type mediaServer struct {
	src    *libSource
	covers sync.Map // carpeta → portada suelta ("" si no hay)
}

// directMIME son los formatos que se sirven tal cual, con su tipo y su
// perfil DLNA (si lo hay).
var directMIME = map[string][2]string{
	".mp3":  {"audio/mpeg", "MP3"},
	".flac": {"audio/flac", ""},
	".ogg":  {"audio/ogg", ""},
	".oga":  {"audio/ogg", ""},
	".opus": {"audio/ogg", ""},
	".m4a":  {"audio/mp4", "AAC_ISO"},
	".m4b":  {"audio/mp4", "AAC_ISO"},
	".mp4":  {"audio/mp4", "AAC_ISO"},
	".aac":  {"audio/aac", "AAC_ADTS"},
	".wav":  {"audio/wav", ""},
	".aif":  {"audio/aiff", ""},
	".aiff": {"audio/aiff", ""},
	".aifc": {"audio/aiff", ""},
}

// dlnaFlags son las banderas DLNA de lo que se sirve: transferencia por
// streaming, conexión que puede esperar, DLNA 1.5.
const dlnaFlags = "DLNA.ORG_FLAGS=01700000000000000000000000000000"

// mediaOf dice cómo se sirve una canción: su extensión en la URL, su tipo y
// si se decodifica a WAV.
func mediaOf(s *song) (ext, mime, pn string, transcode bool) {
	if decode.IsVirtualPath(s.path) {
		return ".wav", "audio/wav", "", true
	}
	e := strings.ToLower(filepath.Ext(s.path))
	if m, ok := directMIME[e]; ok {
		return e, m[0], m[1], false
	}
	return ".wav", "audio/wav", "", true
}

// protocolInfo es el protocolInfo de un <res>.
func protocolInfo(mime, pn string, transcoded bool) string {
	ci := "0"
	if transcoded {
		ci = "1"
	}
	opts := "DLNA.ORG_OP=01;DLNA.ORG_CI=" + ci + ";" + dlnaFlags
	if pn != "" {
		opts = "DLNA.ORG_PN=" + pn + ";" + opts
	}
	return "http-get:*:" + mime + ":" + opts
}

// browse atiende Browse.
func (m *mediaServer) browse(base string, a *action) ([]arg, error) {
	l := m.src.get()
	start, _ := strconv.Atoi(a.Args["StartingIndex"])
	count, _ := strconv.Atoi(a.Args["RequestedCount"])
	id := a.Args["ObjectID"]
	var objs []string // los fragmentos DIDL
	total := 0
	switch a.Args["BrowseFlag"] {
	case "BrowseMetadata":
		o, err := m.metadata(base, l, id)
		if err != nil {
			return nil, err
		}
		objs, total = []string{o}, 1
	case "BrowseDirectChildren":
		children, err := m.children(base, l, id)
		if err != nil {
			return nil, err
		}
		total = len(children)
		objs = page(children, start, count)
	default:
		return nil, errInvalidArgs
	}
	return []arg{
		{"Result", didl(objs)},
		{"NumberReturned", strconv.Itoa(len(objs))},
		{"TotalMatches", strconv.Itoa(total)},
		{"UpdateID", strconv.Itoa(int(l.update))},
	}, nil
}

// page es el trozo [start, start+count) (count 0: hasta el final).
func page(all []string, start, count int) []string {
	if start < 0 || start >= len(all) {
		return nil
	}
	end := len(all)
	if count > 0 && start+count < end {
		end = start + count
	}
	return all[start:end]
}

// didl envuelve objetos en un documento DIDL-Lite.
func didl(objs []string) string {
	return `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/">` +
		strings.Join(objs, "") + `</DIDL-Lite>`
}

// container es un contenedor DIDL.
func container(id, parent, title, class string, n int) string {
	return fmt.Sprintf(`<container id="%s" parentID="%s" restricted="1" searchable="1" childCount="%d"><dc:title>%s</dc:title><upnp:class>%s</upnp:class></container>`,
		xmlEscape(id), xmlEscape(parent), n, xmlEscape(title), class)
}

// item es una canción DIDL, con su recurso en base ("http://ip:puerto").
func (m *mediaServer) item(base string, s *song, parent string) string {
	ext, mime, pn, tc := mediaOf(s)
	var b strings.Builder
	fmt.Fprintf(&b, `<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class>`,
		s.id, xmlEscape(parent), xmlEscape(s.title))
	if s.artist != "" {
		fmt.Fprintf(&b, "<dc:creator>%s</dc:creator><upnp:artist>%s</upnp:artist>", xmlEscape(s.artist), xmlEscape(s.artist))
	}
	if s.album != "" {
		fmt.Fprintf(&b, "<upnp:album>%s</upnp:album>", xmlEscape(s.album))
	}
	if s.track > 0 {
		fmt.Fprintf(&b, "<upnp:originalTrackNumber>%d</upnp:originalTrackNumber>", s.track)
	}
	if m.hasCover(s) {
		fmt.Fprintf(&b, `<upnp:albumArtURI dlna:profileID="JPEG_TN">%s/art/%s</upnp:albumArtURI>`, base, s.id)
	}
	size := ""
	if !tc && s.size > 0 {
		size = fmt.Sprintf(` size="%d"`, s.size)
	}
	fmt.Fprintf(&b, `<res protocolInfo="%s"%s>%s/media/%s%s</res></item>`, xmlEscape(protocolInfo(mime, pn, tc)), size, base, s.id, ext)
	return b.String()
}

// metadata es el propio objeto id (BrowseMetadata).
func (m *mediaServer) metadata(base string, l *library, id string) (string, error) {
	if id == "0" {
		return container("0", "-1", "Audiofilus", "object.container", len(rootContainers)), nil
	}
	for _, c := range rootContainers {
		if c.id == id {
			kids, _ := m.children(base, l, id)
			return container(id, "0", c.title, "object.container", len(kids)), nil
		}
	}
	switch o := l.byID[id].(type) {
	case *song:
		return m.item(base, o, "songs"), nil
	case *album:
		return albumContainer(base, o, "albums"), nil
	case *artistEntry:
		return container(o.id, "artists", o.name, "object.container.person.musicArtist", len(o.albums)), nil
	case *playlist:
		return container(o.id, "playlists", o.name, "object.container.playlistContainer", len(o.songs)), nil
	case *folder:
		return container(o.id, "folders", o.name, "object.container.storageFolder", len(o.subs)+len(o.songs)), nil
	}
	return "", errNoSuchObject
}

// albumContainer es un álbum DIDL, con la portada de su primera canción.
func albumContainer(base string, al *album, parent string) string {
	art := ""
	if len(al.songs) > 0 {
		art = fmt.Sprintf(`<upnp:albumArtURI dlna:profileID="JPEG_TN">%s/art/%s</upnp:albumArtURI>`, base, al.songs[0].id)
	}
	artist := ""
	if al.artist != "" {
		artist = fmt.Sprintf("<upnp:artist>%s</upnp:artist><dc:creator>%s</dc:creator>", xmlEscape(al.artist), xmlEscape(al.artist))
	}
	return fmt.Sprintf(`<container id="%s" parentID="%s" restricted="1" searchable="1" childCount="%d"><dc:title>%s</dc:title><upnp:class>object.container.album.musicAlbum</upnp:class>%s%s</container>`,
		al.id, xmlEscape(parent), len(al.songs), xmlEscape(al.title), artist, art)
}

// children son los hijos de un contenedor (BrowseDirectChildren).
func (m *mediaServer) children(base string, l *library, id string) ([]string, error) {
	var out []string
	switch id {
	case "0":
		for _, c := range rootContainers {
			kids, _ := m.children(base, l, c.id)
			out = append(out, container(c.id, "0", c.title, "object.container", len(kids)))
		}
		return out, nil
	case "artists":
		for _, ar := range l.artists {
			out = append(out, container(ar.id, id, ar.name, "object.container.person.musicArtist", len(ar.albums)))
		}
		return out, nil
	case "albums":
		for _, al := range l.albums {
			out = append(out, albumContainer(base, al, id))
		}
		return out, nil
	case "songs":
		for _, s := range l.songs {
			out = append(out, m.item(base, s, id))
		}
		return out, nil
	case "playlists":
		for _, p := range l.playlists {
			out = append(out, container(p.id, id, p.name, "object.container.playlistContainer", len(p.songs)))
		}
		return out, nil
	case "folders":
		for _, f := range l.folders {
			out = append(out, container(f.id, id, f.name, "object.container.storageFolder", len(f.subs)+len(f.songs)))
		}
		return out, nil
	}
	switch o := l.byID[id].(type) {
	case *artistEntry:
		for _, al := range o.albums {
			out = append(out, albumContainer(base, al, id))
		}
	case *album:
		for _, s := range o.songs {
			out = append(out, m.item(base, s, id))
		}
	case *playlist:
		for _, s := range o.songs {
			out = append(out, m.item(base, s, id))
		}
	case *folder:
		for _, f := range o.subs {
			out = append(out, container(f.id, id, f.name, "object.container.storageFolder", len(f.subs)+len(f.songs)))
		}
		for _, s := range o.songs {
			out = append(out, m.item(base, s, id))
		}
	case *song:
		return nil, errNoSuchObject // una canción no tiene hijos
	default:
		return nil, errNoSuchObject
	}
	return out, nil
}

// quotedRe saca los valores entre comillas de un SearchCriteria.
var quotedRe = regexp.MustCompile(`(dc:title|upnp:artist|upnp:album|dc:creator|upnp:genre)\s+(?:contains|=)\s+"((?:[^"\\]|\\.)*)"`)

// search atiende Search: lo justo para los clientes habituales (título,
// artista y álbum que contienen algo; álbumes o artistas según la clase).
func (m *mediaServer) search(base string, a *action) ([]arg, error) {
	l := m.src.get()
	crit := a.Args["SearchCriteria"]
	start, _ := strconv.Atoi(a.Args["StartingIndex"])
	count, _ := strconv.Atoi(a.Args["RequestedCount"])
	conds := quotedRe.FindAllStringSubmatch(crit, -1)
	match := func(field, v string) bool {
		for _, c := range conds {
			if c[1] == field || (field == "upnp:artist" && c[1] == "dc:creator") {
				if !strings.Contains(strings.ToLower(v), strings.ToLower(strings.ReplaceAll(c[2], `\"`, `"`))) {
					return false
				}
			}
		}
		return true
	}
	var out []string
	switch {
	case strings.Contains(crit, "object.container.album"):
		for _, al := range l.albums {
			if match("dc:title", al.title) && match("upnp:artist", al.artist) && match("upnp:album", al.title) {
				out = append(out, albumContainer(base, al, "albums"))
			}
		}
	case strings.Contains(crit, "object.container.person"):
		for _, ar := range l.artists {
			if match("dc:title", ar.name) && match("upnp:artist", ar.name) {
				out = append(out, container(ar.id, "artists", ar.name, "object.container.person.musicArtist", len(ar.albums)))
			}
		}
	default:
		for _, s := range l.songs {
			if match("dc:title", s.title) && match("upnp:artist", s.artist) && match("upnp:album", s.album) {
				out = append(out, m.item(base, s, "songs"))
			}
		}
	}
	objs := page(out, start, count)
	return []arg{
		{"Result", didl(objs)},
		{"NumberReturned", strconv.Itoa(len(objs))},
		{"TotalMatches", strconv.Itoa(len(out))},
		{"UpdateID", strconv.Itoa(int(l.update))},
	}, nil
}

// contentDirectory atiende las acciones del ContentDirectory.
func (m *mediaServer) contentDirectory(r *http.Request) handler {
	return func(a *action) ([]arg, error) {
		base := "http://" + r.Host
		switch a.Name {
		case "Browse":
			return m.browse(base, a)
		case "Search":
			return m.search(base, a)
		case "GetSearchCapabilities":
			return []arg{{"SearchCaps", "dc:title,dc:creator,upnp:artist,upnp:album,upnp:class"}}, nil
		case "GetSortCapabilities":
			return []arg{{"SortCaps", ""}}, nil
		case "GetSystemUpdateID":
			return []arg{{"Id", strconv.Itoa(int(m.src.get().update))}}, nil
		}
		return nil, errInvalidAction
	}
}

// sourceProtocols es lo que sirve el servidor (GetProtocolInfo).
func sourceProtocols() string {
	seen := map[string]bool{}
	var out []string
	for _, m := range directMIME {
		p := "http-get:*:" + m[0] + ":*"
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// serveMedia sirve una canción (/media/<id>.<ext>): el archivo tal cual con
// Range, o decodificado en WAV.
func (m *mediaServer) serveMedia(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/media/")
	id := strings.TrimSuffix(name, filepath.Ext(name))
	s, ok := m.src.get().byID[id].(*song)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, mime, pn, tc := mediaOf(s)
	// Con su nombre tal cual (sin canonizar): alguna tele lo compara así.
	w.Header()["transferMode.dlna.org"] = []string{"Streaming"}
	w.Header()["contentFeatures.dlna.org"] = []string{strings.TrimPrefix(protocolInfo(mime, pn, tc), "http-get:*:"+mime+":")}
	w.Header().Set("Content-Type", mime)
	if tc {
		serveWAV(w, r, s.path)
		return
	}
	f, err := os.Open(s.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(s.path), st.ModTime(), f)
}

// wavHeader es la cabecera de un WAV de 16 bits estéreo de frames frames
// (frames < 0: sin largo, con los tamaños al máximo).
func wavHeader(rate int, frames int64) []byte {
	data := uint32(math.MaxUint32 - 44)
	if frames >= 0 {
		data = uint32(frames * 4)
	}
	var h bytes.Buffer
	h.WriteString("RIFF")
	_ = binary.Write(&h, binary.LittleEndian, data+36)
	h.WriteString("WAVEfmt ")
	_ = binary.Write(&h, binary.LittleEndian, uint32(16))
	_ = binary.Write(&h, binary.LittleEndian, uint16(1))
	_ = binary.Write(&h, binary.LittleEndian, uint16(2))
	_ = binary.Write(&h, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&h, binary.LittleEndian, uint32(rate*4))
	_ = binary.Write(&h, binary.LittleEndian, uint16(4))
	_ = binary.Write(&h, binary.LittleEndian, uint16(16))
	h.WriteString("data")
	_ = binary.Write(&h, binary.LittleEndian, data)
	return h.Bytes()
}

// rangeStart lee el principio de un "Range: bytes=N-" (o -1 si no hay).
func rangeStart(r *http.Request) int64 {
	v := r.Header.Get("Range")
	if !strings.HasPrefix(v, "bytes=") {
		return -1
	}
	v = strings.TrimPrefix(v, "bytes=")
	if i := strings.IndexAny(v, "-,"); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// serveWAV decodifica path y lo manda como WAV de 16 bits. Si se sabe su
// largo, admite Range (cada frame son 4 bytes: se salta al suyo).
func serveWAV(w http.ResponseWriter, r *http.Request, path string) {
	src, err := decode.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	defer src.Close()
	frames := src.Length()
	if frames <= 0 {
		frames = -1
	}
	hdr := wavHeader(src.SampleRate(), frames)
	total := int64(-1)
	if frames >= 0 {
		total = int64(len(hdr)) + frames*4
		w.Header().Set("Accept-Ranges", "bytes")
	}
	from := rangeStart(r)
	if from > 0 && total >= 0 && from < total {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, total-1, total))
		w.Header().Set("Content-Length", strconv.FormatInt(total-from, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		from = 0
		if total >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		}
		w.WriteHeader(http.StatusOK)
	}
	if r.Method == http.MethodHead {
		return
	}
	skip := 0 // bytes del primer frame que no se mandan (un Range a mitad de frame)
	if from < int64(len(hdr)) {
		if _, err := w.Write(hdr[from:]); err != nil {
			return
		}
	} else {
		off := from - int64(len(hdr))
		if err := src.SeekFrame(off / 4); err != nil {
			return
		}
		skip = int(off % 4)
	}
	buf := make([]float32, 4096*2)
	out := make([]byte, len(buf)*2)
	for {
		n, err := src.Read(buf)
		for i := 0; i < n; i++ {
			v := math.Max(-1, math.Min(1, float64(buf[i])))
			binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(math.Round(v*32767))))
		}
		chunk := out[:2*n]
		if skip > 0 {
			chunk = chunk[min(skip, len(chunk)):]
			skip = 0
		}
		if len(chunk) > 0 {
			if _, werr := w.Write(chunk); werr != nil {
				return
			}
		}
		if err != nil || n == 0 {
			return
		}
	}
}

// coverNames son las portadas sueltas que se buscan en la carpeta.
var coverNames = []string{"cover.jpg", "folder.jpg", "front.jpg", "cover.png", "folder.png", "front.png", "Cover.jpg", "Folder.jpg", "AlbumArt.jpg"}

// folderCover es la portada suelta de una carpeta ("" si no hay).
func (m *mediaServer) folderCover(dir string) string {
	if v, ok := m.covers.Load(dir); ok {
		return v.(string)
	}
	found := ""
	for _, n := range coverNames {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			found = filepath.Join(dir, n)
			break
		}
	}
	m.covers.Store(dir, found)
	return found
}

// hasCover dice si una canción tiene portada que servir.
func (m *mediaServer) hasCover(s *song) bool {
	if s.pic {
		return true
	}
	file := decode.FileOf(s.path)
	if _, err := os.Stat(file + ".jpg"); err == nil {
		return true
	}
	return m.folderCover(filepath.Dir(file)) != ""
}

// serveArt sirve la portada de una canción (/art/<id>): la incrustada, la
// de al lado (<archivo>.jpg) o la de su carpeta.
func (m *mediaServer) serveArt(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/art/")
	s, ok := m.src.get().byID[id].(*song)
	if !ok {
		http.NotFound(w, r)
		return
	}
	file := decode.FileOf(s.path)
	if s.pic {
		if f, err := os.Open(file); err == nil {
			md, terr := tag.ReadFrom(f)
			f.Close()
			if terr == nil && md.Picture() != nil {
				mime := md.Picture().MIMEType
				if mime == "" {
					mime = http.DetectContentType(md.Picture().Data)
				}
				w.Header().Set("Content-Type", mime)
				_, _ = w.Write(md.Picture().Data)
				return
			}
		}
	}
	for _, p := range []string{file + ".jpg", m.folderCover(filepath.Dir(file))} {
		if p == "" {
			continue
		}
		if f, err := os.Open(p); err == nil {
			defer f.Close()
			if strings.HasSuffix(strings.ToLower(p), ".png") {
				w.Header().Set("Content-Type", "image/png")
			} else {
				w.Header().Set("Content-Type", "image/jpeg")
			}
			_, _ = io.Copy(w, f)
			return
		}
	}
	http.NotFound(w, r)
}
