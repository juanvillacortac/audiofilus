package dlna

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/radio"
)

// Lo que manda un mando se baja entero a data/cache/dlna antes de sonar (en
// la red de casa una canción tarda un momento, y los decodificadores
// necesitan saltar por el archivo); lo que no tiene fin (una radio por
// DLNA, sin Content-Length) suena en directo, como las emisoras. Solo se
// guarda lo que está puesto (la actual y la siguiente).

// maxDownload es lo más que se baja de una URL.
const maxDownload = 1 << 30

// Los estados de una descarga.
const (
	fetchIdle = iota
	fetchLoading
	fetchDone
	fetchStream
	fetchFailed
)

// fetchEntry es una URL del renderer.
type fetchEntry struct {
	uri    string
	meta   Meta
	state  int
	file   string
	stream *radio.Stream
	err    error
	cancel context.CancelFunc
}

// fetcher baja lo que mandan los mandos.
type fetcher struct {
	dir     string
	client  *http.Client
	mu      sync.Mutex
	entries map[string]*fetchEntry
}

func newFetcher(dir string) *fetcher {
	// Lo de una ejecución anterior sobra.
	_ = os.RemoveAll(dir)
	return &fetcher{dir: dir, client: &http.Client{}, entries: map[string]*fetchEntry{}}
}

// add apunta una ruta de la cola con su URL.
func (f *fetcher) add(p, uri string, meta Meta) {
	f.mu.Lock()
	f.entries[p] = &fetchEntry{uri: uri, meta: meta}
	f.mu.Unlock()
}

// entry es la URL y los metadatos de una ruta ("" si no es del renderer).
func (f *fetcher) entry(p string) (string, Meta) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.entries[p]; e != nil {
		return e.uri, e.meta
	}
	return "", Meta{}
}

// keep olvida (y borra) todo lo que no sea paths.
func (f *fetcher) keep(paths ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for p, e := range f.entries {
		if containsStr(paths, p) {
			continue
		}
		if e.cancel != nil {
			e.cancel()
		}
		if e.file != "" {
			_ = os.Remove(e.file)
		}
		delete(f.entries, p)
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Open abre una ruta del renderer para el motor: ready es false mientras se
// baja (el motor vuelve a preguntar). Se llama con el motor bloqueado: no
// espera nada.
func (f *fetcher) Open(p string) (decode.Source, bool, error) {
	f.mu.Lock()
	e := f.entries[p]
	if e == nil {
		f.mu.Unlock()
		return nil, false, fmt.Errorf("dlna: %s is gone", p)
	}
	switch e.state {
	case fetchIdle:
		e.state = fetchLoading
		ctx, cancel := context.WithCancel(context.Background())
		e.cancel = cancel
		go f.download(ctx, p, e.uri)
		f.mu.Unlock()
		return nil, false, nil
	case fetchLoading:
		f.mu.Unlock()
		return nil, false, nil
	case fetchFailed:
		err := e.err
		f.mu.Unlock()
		return nil, false, err
	case fetchStream:
		s := e.stream
		f.mu.Unlock()
		ready, err := s.Ready()
		if err != nil {
			return nil, false, err
		}
		return s, ready, nil
	}
	file := e.file
	f.mu.Unlock()
	src, err := decode.Open(file)
	return src, err == nil, err
}

// Track son las etiquetas de una ruta del renderer (las del mando).
func (f *fetcher) Track(p string) (Meta, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entries[p]
	if e == nil {
		return Meta{}, false
	}
	m := e.meta
	if m.Title == "" {
		m.Title = titleFromURL(e.uri)
	}
	return m, true
}

// titleFromURL es un título de reserva: el nombre del archivo de la URL.
func titleFromURL(u string) string {
	if pu, err := url.Parse(u); err == nil {
		if b := path.Base(pu.Path); b != "" && b != "/" && b != "." {
			if s, err := url.PathUnescape(b); err == nil {
				b = s
			}
			return strings.TrimSuffix(b, path.Ext(b))
		}
	}
	return u
}

// extFor es la extensión de un tipo de audio (para que el decodificador lo
// reconozca), o la de la URL.
func extFor(contentType, u string) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(mt) {
	case "audio/mpeg", "audio/mp3", "audio/mpeg3":
		return ".mp3"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/ogg", "audio/x-ogg", "application/ogg", "audio/vorbis":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/mp4", "audio/x-m4a", "audio/m4a":
		return ".m4a"
	case "audio/aac", "audio/x-aac", "audio/aacp":
		return ".aac"
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave", "audio/l16":
		return ".wav"
	case "audio/aiff", "audio/x-aiff":
		return ".aiff"
	case "audio/dsf", "audio/x-dsf":
		return ".dsf"
	}
	if pu, err := url.Parse(u); err == nil {
		if e := strings.ToLower(path.Ext(pu.Path)); e != "" && decode.IsAudioFile("x"+e) {
			return e
		}
	}
	return ".bin"
}

// download baja uri (o, si no tiene fin, la deja como emisora).
func (f *fetcher) download(ctx context.Context, p, uri string) {
	fail := func(err error) {
		log.Printf("dlna: %s: %v", uri, err)
		f.mu.Lock()
		if e := f.entries[p]; e != nil {
			e.state, e.err = fetchFailed, err
		}
		f.mu.Unlock()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		fail(err)
		return
	}
	req.Header.Set("User-Agent", serverHeader)
	req.Header.Set("Icy-MetaData", "0")
	resp, err := f.client.Do(req)
	if err != nil {
		fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		fail(fmt.Errorf("http %s", resp.Status))
		return
	}
	ct := resp.Header.Get("Content-Type")
	if resp.ContentLength <= 0 || resp.ContentLength > maxDownload || resp.Header.Get("icy-name") != "" {
		// Sin largo: una emisora en directo.
		resp.Body.Close()
		s := radio.Open(uri, ct)
		f.mu.Lock()
		if e := f.entries[p]; e != nil {
			e.state, e.stream = fetchStream, s
		} else {
			s.Close()
		}
		f.mu.Unlock()
		return
	}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		fail(err)
		return
	}
	sum := sha1.Sum([]byte(p + uri))
	file := filepath.Join(f.dir, hex.EncodeToString(sum[:8])+extFor(ct, uri))
	tmp := file + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		fail(err)
		return
	}
	body := io.Reader(resp.Body)
	if mt, params, _ := mime.ParseMediaType(ct); strings.EqualFold(mt, "audio/L16") {
		// PCM crudo (big endian): se guarda como WAV.
		rate, _ := strconv.Atoi(params["rate"])
		ch, _ := strconv.Atoi(params["channels"])
		if rate <= 0 {
			rate = 44100
		}
		if ch <= 0 {
			ch = 2
		}
		_, _ = out.Write(l16Header(rate, ch, resp.ContentLength))
		body = &swap16{r: resp.Body}
	}
	start := time.Now()
	n, err := io.Copy(out, io.LimitReader(body, maxDownload))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		if !errors.Is(err, context.Canceled) {
			fail(err)
		}
		return
	}
	if err := os.Rename(tmp, file); err != nil {
		fail(err)
		return
	}
	log.Printf("dlna: got %s (%d KB in %v)", uri, n/1024, time.Since(start).Round(time.Millisecond))
	f.mu.Lock()
	e := f.entries[p]
	if e == nil {
		_ = os.Remove(file) // lo olvidaron mientras se bajaba
	} else {
		e.state, e.file = fetchDone, file
	}
	f.mu.Unlock()
}

// l16Header es la cabecera WAV de PCM de 16 bits de size bytes.
func l16Header(rate, ch int, size int64) []byte {
	var h bytes.Buffer
	h.WriteString("RIFF")
	_ = binary.Write(&h, binary.LittleEndian, uint32(size+36))
	h.WriteString("WAVEfmt ")
	_ = binary.Write(&h, binary.LittleEndian, uint32(16))
	_ = binary.Write(&h, binary.LittleEndian, uint16(1))
	_ = binary.Write(&h, binary.LittleEndian, uint16(ch))
	_ = binary.Write(&h, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&h, binary.LittleEndian, uint32(rate*ch*2))
	_ = binary.Write(&h, binary.LittleEndian, uint16(ch*2))
	_ = binary.Write(&h, binary.LittleEndian, uint16(16))
	h.WriteString("data")
	_ = binary.Write(&h, binary.LittleEndian, uint32(size))
	return h.Bytes()
}

// swap16 da la vuelta a cada muestra de 16 bits (big endian → little).
type swap16 struct {
	r    io.Reader
	odd  bool
	keep byte
}

func (s *swap16) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	out := p[:0]
	for i := 0; i < n; i++ {
		b := p[i]
		if !s.odd {
			s.keep, s.odd = b, true
			continue
		}
		out = append(out, b, s.keep)
		s.odd = false
	}
	return len(out), err
}
