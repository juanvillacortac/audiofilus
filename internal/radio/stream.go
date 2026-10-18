// Package radio es la radio por internet: el directorio libre de
// radio-browser.info (sin clave) para encontrar emisoras, y el flujo de cada
// una como un decode.Source más para el engine. Una emisora viaja como
// "radio://<uuid>"; sus datos (dirección, nombre, logo) los guarda la
// interfaz en data/cache/radio/stations.json para que el servicio los lea.
package radio

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// UserAgent es como se presenta audiofilus (radio-browser pide uno que diga
// quién es).
const UserAgent = "audiofilus/1.0"

// Medidas del búfer: cuánto se guarda de lo que llega, cuánto hace falta para
// empezar a sonar y para volver a sonar tras quedarse sin datos (con un margen,
// para no tartamudear).
const (
	bufMax     = 1 << 20
	startBytes = 48 << 10 // ~3 s a 128 kbps
	resumeAt   = 24 << 10
)

// Stream es una emisora sonando: implementa decode.Source. Una goroutine lee
// de la red (con reconexión) a un búfer; Read decodifica de ahí sin esperar
// nunca: si faltan datos, devuelve silencio.
type Stream struct {
	url   string
	codec string // "mp3" o "aac"; "" hasta saberlo por el Content-Type

	mu        sync.Mutex
	buf       []byte
	dec       decode.StreamDecoder
	rate      int
	ready     bool // ya decodificó algo: se sabe la frecuencia
	buffering bool // se quedó sin datos y espera a tener margen
	title     string
	err       error // el último error de red
	fatal     error // no se puede (códec no soportado...)
	pos       int64
	cancel    context.CancelFunc
	space     *sync.Cond
	pre       []float32 // lo decodificado al arrancar, para saber la frecuencia
}

// Open empieza a conectar con url (codec es la pista del directorio: "MP3",
// "AAC", "AAC+"...). Vuelve enseguida; Ready dice cuándo puede sonar.
func Open(url, codec string) *Stream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Stream{url: url, codec: codecOf(codec, ""), cancel: cancel}
	s.space = sync.NewCond(&s.mu)
	go s.run(ctx)
	return s
}

// codecOf traduce el códec del directorio o el Content-Type a "mp3"/"aac".
func codecOf(hint, contentType string) string {
	h := strings.ToLower(hint + " " + contentType)
	switch {
	case strings.Contains(h, "aac"), strings.Contains(h, "mp4a"):
		return "aac"
	case strings.Contains(h, "mp3"), strings.Contains(h, "mpeg"):
		return "mp3"
	}
	return ""
}

// Ready dice si ya suena (o el error si no se puede).
func (s *Stream) Ready() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatal != nil {
		return false, s.fatal
	}
	if !s.ready && len(s.buf) >= startBytes {
		s.prime()
	}
	return s.ready, nil
}

// prime crea el decodificador y decodifica lo primero, para saber la
// frecuencia (el engine la necesita al abrir). Con s.mu tomado.
func (s *Stream) prime() {
	if s.dec == nil {
		if s.codec == "" {
			s.fatal = fmt.Errorf("radio: unknown codec")
			return
		}
		d, err := decode.NewStreamDecoder(s.codec)
		if err != nil {
			s.fatal = err
			return
		}
		s.dec = d
	}
	tmp := make([]float32, 4096)
	for tries := 0; tries < 64 && s.dec.SampleRate() == 0; tries++ {
		s.feedLocked(16 << 10)
		n, _ := s.dec.Decode(tmp)
		s.pre = append(s.pre, tmp[:n]...)
		if len(s.buf) == 0 && n == 0 {
			break
		}
	}
	if s.rate = s.dec.SampleRate(); s.rate > 0 {
		s.ready = true
	}
}

// feedLocked pasa hasta n bytes del búfer al decodificador.
func (s *Stream) feedLocked(n int) {
	n = min(n, len(s.buf))
	if n == 0 {
		return
	}
	s.dec.Feed(s.buf[:n])
	s.buf = append(s.buf[:0], s.buf[n:]...)
	s.space.Signal()
}

// Title es lo que está sonando según la emisora (los metadatos ICY), si lo
// dice.
func (s *Stream) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// Buffering dice si se quedó sin datos (y suena silencio).
func (s *Stream) Buffering() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffering || !s.ready
}

// Read decodifica lo que haya; si no hay bastante, completa con silencio.
func (s *Stream) Read(dst []float32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(dst) &^ 1
	if s.fatal != nil {
		return 0, s.fatal
	}
	got := 0
	if len(s.pre) > 0 {
		got = copy(dst[:n], s.pre)
		s.pre = s.pre[got:]
	}
	if s.buffering && len(s.buf) >= resumeAt {
		s.buffering = false
	}
	for got < n && !s.buffering && s.dec != nil {
		k, _ := s.dec.Decode(dst[got:n])
		got += k
		if k == 0 {
			if len(s.buf) == 0 {
				s.buffering = true // sin datos: silencio hasta tener margen
				break
			}
			s.feedLocked(8 << 10)
		}
	}
	clear(dst[got:n])
	s.pos += int64(n / 2)
	return n, nil
}

func (s *Stream) SampleRate() int       { return s.rate }
func (s *Stream) Length() int64         { return -1 }
func (s *Stream) Position() int64       { return s.pos }
func (s *Stream) SeekFrame(int64) error { return errors.New("radio: cannot seek a live stream") }

// Close corta la conexión.
func (s *Stream) Close() error {
	s.cancel()
	s.mu.Lock()
	s.space.Broadcast()
	if s.dec != nil {
		s.dec.Close()
		s.dec = nil
	}
	s.fatal = errors.New("radio: closed")
	s.mu.Unlock()
	return nil
}

// client es el cliente HTTP de las emisoras: acepta la respuesta de los
// servidores Shoutcast viejos ("ICY 200 OK" en vez de "HTTP/1.0 200 OK").
var client = &http.Client{Transport: &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &icyConn{Conn: c}, nil
	},
	ResponseHeaderTimeout: 20 * time.Second,
}}

// icyConn cambia "ICY " por "HTTP/1.0 " al principio de la respuesta.
type icyConn struct {
	net.Conn
	checked bool
	extra   []byte
}

func (c *icyConn) Read(p []byte) (int, error) {
	if len(c.extra) > 0 {
		n := copy(p, c.extra)
		c.extra = c.extra[n:]
		return n, nil
	}
	n, err := c.Conn.Read(p)
	if !c.checked && n > 0 {
		c.checked = true
		if strings.HasPrefix(string(p[:n]), "ICY ") {
			fixed := append([]byte("HTTP/1.0 "), p[4:n]...)
			k := copy(p, fixed)
			c.extra = append(c.extra, fixed[k:]...)
			return k, err
		}
	}
	return n, err
}

// run conecta y lee mientras dure, reconectando si se corta.
func (s *Stream) run(ctx context.Context) {
	wait := time.Second
	for ctx.Err() == nil {
		err := s.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.err = err
		fatal := s.fatal != nil
		s.mu.Unlock()
		if fatal {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 10*time.Second)
	}
}

// connect lee el flujo hasta que se corte: los bytes de audio al búfer y,
// cada icy-metaint bytes, los metadatos (el título).
func (s *Stream) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("radio: %s", resp.Status)
	}
	s.mu.Lock()
	if c := codecOf("", resp.Header.Get("Content-Type")); c != "" && s.codec == "" {
		s.codec = c
	}
	if s.codec == "" {
		s.fatal = fmt.Errorf("%w (radio content type %q)", decode.ErrUnsupported, resp.Header.Get("Content-Type"))
		s.mu.Unlock()
		return s.fatal
	}
	s.mu.Unlock()
	metaint, _ := strconv.Atoi(resp.Header.Get("Icy-Metaint"))
	r := bufio.NewReaderSize(resp.Body, 32<<10)
	chunk := make([]byte, 16<<10)
	left := metaint
	for {
		want := len(chunk)
		if metaint > 0 {
			want = min(want, left)
		}
		n, err := r.Read(chunk[:want])
		if n > 0 {
			s.push(ctx, chunk[:n])
			left -= n
		}
		if err != nil {
			return err
		}
		if metaint > 0 && left == 0 {
			if err := s.readMeta(r); err != nil {
				return err
			}
			left = metaint
		}
	}
}

// push guarda bytes de audio, esperando si el búfer está lleno.
func (s *Stream) push(ctx context.Context, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf)+len(b) > bufMax && ctx.Err() == nil && s.fatal == nil {
		s.space.Wait()
	}
	s.buf = append(s.buf, b...)
}

// readMeta lee un bloque de metadatos ICY y se queda con StreamTitle.
func (s *Stream) readMeta(r *bufio.Reader) error {
	l, err := r.ReadByte()
	if err != nil || l == 0 {
		return err
	}
	meta := make([]byte, int(l)*16)
	if _, err := io.ReadFull(r, meta); err != nil {
		return err
	}
	m := strings.TrimRight(string(meta), "\x00")
	if i := strings.Index(m, "StreamTitle='"); i >= 0 {
		t := m[i+len("StreamTitle='"):]
		if j := strings.Index(t, "';"); j >= 0 {
			t = t[:j]
		}
		s.mu.Lock()
		s.title = strings.TrimSpace(t)
		s.mu.Unlock()
	}
	return nil
}
