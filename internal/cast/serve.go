package cast

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/resample"
)

// El servidor HTTP de los archivos: el aparato pide la canción a este equipo.
// Cada archivo se publica con una ficha al azar (/m/<ficha>.<ext>), nunca por
// su ruta, y solo lo publicado se sirve. Los que el aparato sabe tocar van tal
// cual, con Range para saltar; el resto (módulos, música de consola, DSD, un
// trozo de un CUE...) se decodifica y sale como WAV de 16 bits, también con
// Range: el byte pedido se pasa a frame y se salta en el decodificador.

// Server sirve los archivos publicados.
type Server struct {
	ln   net.Listener
	srv  *http.Server
	mu   sync.Mutex
	pubs map[string]*published
}

// published es un archivo publicado.
type published struct {
	path      string
	transcode bool
	created   time.Time
}

// NewServer abre el servidor en addr (":0" o "" para un puerto cualquiera).
func NewServer(addr string) (*Server, error) {
	if addr == "" {
		addr = ":0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, pubs: map[string]*published{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/m/", s.serveMedia)
	mux.HandleFunc("/c/", s.serveCover)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Port es el puerto en que escucha.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Close cierra el servidor.
func (s *Server) Close() error { return s.srv.Close() }

// localIP es la IP de este equipo por la que se llega a host (la interfaz
// que tiene la ruta hacia el aparato).
func localIP(host string) (string, error) {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	c, err := net.Dial("udp", net.JoinHostPort(h, "9"))
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// token es una ficha al azar.
func token() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// directTypes son los tipos que se sirven tal cual, por extensión (los que
// casi todos los aparatos tocan). Lo demás se pasa a WAV.
var directTypes = map[string]string{
	".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".m4b": "audio/mp4", ".mp4": "audio/mp4",
	".aac": "audio/aac", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg",
}

// URLFor publica it para el aparato d y devuelve lo que se le manda: la
// dirección (con la IP por la que el aparato llega a este equipo), su tipo y
// la de la portada. Lo que ya es http(s) pasa tal cual.
func (s *Server) URLFor(d Device, it Item, forceTranscode bool) (media, error) {
	m := media{Title: it.Title, Artist: it.Artist, Album: it.Album, Duration: it.Duration, MIME: it.MIME}
	ip, err := localIP(d.Host)
	if err != nil {
		return m, err
	}
	base := "http://" + net.JoinHostPort(ip, strconv.Itoa(s.Port()))
	switch {
	case strings.HasPrefix(it.Cover, "http://"), strings.HasPrefix(it.Cover, "https://"):
		m.Cover = it.Cover
	case it.Cover != "":
		if _, err := os.Stat(it.Cover); err == nil {
			tok := s.publish(it.Cover, false)
			m.Cover = base + "/c/" + tok + strings.ToLower(filepath.Ext(it.Cover))
		}
	}
	if it.URL != "" {
		if !strings.HasPrefix(it.URL, "http://") && !strings.HasPrefix(it.URL, "https://") {
			return m, fmt.Errorf("cast: can't send %q", it.URL)
		}
		m.URL = it.URL
		if m.MIME == "" {
			m.MIME = orStr(mime.TypeByExtension(strings.ToLower(filepath.Ext(strings.SplitN(it.URL, "?", 2)[0]))), "audio/mpeg")
		}
		return m, nil
	}
	ext := strings.ToLower(filepath.Ext(it.Path))
	typ, direct := directTypes[ext]
	if decode.IsVirtualPath(it.Path) || forceTranscode {
		direct = false
	}
	if direct {
		tok := s.publish(it.Path, false)
		m.URL, m.MIME = base+"/m/"+tok+ext, typ
		return m, nil
	}
	tok := s.publish(it.Path, true)
	m.URL, m.MIME = base+"/m/"+tok+".wav", "audio/wav"
	return m, nil
}

// publish da una ficha a path (y olvida las de hace más de un día).
func (s *Server) publish(path string, transcode bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.pubs {
		if time.Since(p.created) > 24*time.Hour {
			delete(s.pubs, k)
		}
	}
	tok := token()
	s.pubs[tok] = &published{path: path, transcode: transcode, created: time.Now()}
	return tok
}

// lookup busca lo publicado por la ficha del final de la URL.
func (s *Server) lookup(urlPath, prefix string) *published {
	name := strings.TrimPrefix(urlPath, prefix)
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pubs[name]
}

func (s *Server) serveCover(w http.ResponseWriter, r *http.Request) {
	p := s.lookup(r.URL.Path, "/c/")
	if p == nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p.path)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	p := s.lookup(r.URL.Path, "/m/")
	if p == nil {
		http.NotFound(w, r)
		return
	}
	// DLNA: lo piden algunos aparatos para saber que se puede saltar.
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP=01;DLNA.ORG_FLAGS=01700000000000000000000000000000")
	if !p.transcode {
		f, err := os.Open(p.path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if t := directTypes[strings.ToLower(filepath.Ext(p.path))]; t != "" {
			w.Header().Set("Content-Type", t)
		}
		http.ServeContent(w, r, "", st.ModTime(), f) // Range, HEAD y demás
		return
	}
	serveWAV(w, r, p.path)
}

// wavRate es la frecuencia de lo que se pasa a WAV: 44,1 o 48 kHz (las que
// tocan todos los aparatos), según la del archivo.
func wavRate(in int) int {
	if in%11025 == 0 {
		return 44100 // 11 025, 22 050, 44 100, 88 200...
	}
	return 48000
}

// wavHeader es la cabecera de un WAV de 16 bits estéreo con data bytes de
// datos.
func wavHeader(rate int, data uint32) []byte {
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+data)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], data)
	return h
}

// serveWAV decodifica path y lo manda como WAV. Si se sabe el largo, el WAV
// tiene su tamaño exacto y admite Range (el byte se pasa a frame y se salta
// en el decodificador); si no (una canción de libgme sin duración), sale
// entero, sin Range.
func serveWAV(w http.ResponseWriter, r *http.Request, path string) {
	src, err := decode.Open(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	defer src.Close()
	rate := wavRate(src.SampleRate())
	frames := int64(-1)
	if l := src.Length(); l > 0 {
		frames = int64(math.Round(float64(l) * float64(rate) / float64(src.SampleRate())))
	}
	w.Header().Set("Content-Type", "audio/wav")
	if frames < 0 {
		w.Header().Set("Accept-Ranges", "none")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(wavHeader(rate, 0x7ffff000))
		streamPCM(w, resample.New(src, rate), -1)
		return
	}
	size := 44 + frames*4
	w.Header().Set("Accept-Ranges", "bytes")
	start, end, ok := parseRange(r.Header.Get("Range"), size)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method == http.MethodHead {
		return
	}
	hdr := wavHeader(rate, uint32(frames*4))
	pos := start
	if pos < 44 {
		n := min(int64(44), end+1)
		_, _ = w.Write(hdr[pos:n])
		pos = n
	}
	if pos > end {
		return
	}
	// El primer frame entero desde pos; si pos cae a mitad de un frame, se
	// mandan los bytes que faltan de él.
	off := pos - 44
	frame := off / 4
	skip := off % 4
	inFrame := int64(math.Floor(float64(frame) * float64(src.SampleRate()) / float64(rate)))
	if err := src.SeekFrame(inFrame); err != nil {
		return
	}
	rs := resample.New(src, rate)
	streamPCM(&skipWriter{w: w, skip: skip}, rs, end-pos+1)
}

// skipWriter se salta los primeros bytes de lo que le escriben.
type skipWriter struct {
	w    io.Writer
	skip int64
}

func (s *skipWriter) Write(b []byte) (int, error) {
	n := len(b)
	if s.skip > 0 {
		k := min(s.skip, int64(len(b)))
		b, s.skip = b[k:], s.skip-k
	}
	if len(b) == 0 {
		return n, nil
	}
	_, err := s.w.Write(b)
	return n, err
}

// streamPCM escribe lo decodificado como PCM de 16 bits little endian, hasta
// limit bytes (-1: hasta el final; si se acaba antes, completa con silencio
// para que el tamaño anunciado se cumpla).
func streamPCM(w io.Writer, r *resample.Reader, limit int64) {
	buf := make([]float32, 4096*2)
	out := make([]byte, len(buf)*2)
	sent := int64(0)
	// Lo que se escribe de más por el salto de un frame a medias lo descuenta
	// skipWriter; aquí se cuenta lo que pide el que llama más ese salto.
	if sw, ok := w.(*skipWriter); ok && limit >= 0 {
		limit += sw.skip
	}
	for limit < 0 || sent < limit {
		n, err := r.Read(buf)
		for i := 0; i < n; i++ {
			v := int16(math.Max(-1, math.Min(1, float64(buf[i]))) * 32767)
			binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
		}
		chunk := out[:2*n]
		if limit >= 0 && sent+int64(len(chunk)) > limit {
			chunk = chunk[:limit-sent]
		}
		if len(chunk) > 0 {
			if _, werr := w.Write(chunk); werr != nil {
				return
			}
			sent += int64(len(chunk))
		}
		if err != nil || n == 0 {
			break
		}
	}
	// Si el decodificador dio menos de lo anunciado, silencio hasta el final.
	for limit >= 0 && sent < limit {
		z := make([]byte, min(int64(len(out)), limit-sent))
		if _, err := w.Write(z); err != nil {
			return
		}
		sent += int64(len(z))
	}
}

// parseRange lee "bytes=a-b" (una sola parte) para un recurso de size bytes.
func parseRange(h string, size int64) (start, end int64, ok bool) {
	if h == "" {
		return 0, size - 1, true
	}
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	a, b, _ := strings.Cut(spec, "-")
	var err error
	switch {
	case a == "": // los últimos b bytes
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		return max(0, size-n), size - 1, true
	default:
		if start, err = strconv.ParseInt(a, 10, 64); err != nil {
			return 0, 0, false
		}
		end = size - 1
		if b != "" {
			if end, err = strconv.ParseInt(b, 10, 64); err != nil {
				return 0, 0, false
			}
			end = min(end, size-1)
		}
	}
	if start < 0 || start > end || start >= size {
		return 0, 0, false
	}
	return start, end, true
}
