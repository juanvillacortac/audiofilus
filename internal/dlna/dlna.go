// Package dlna hace de audiofilus un servidor y un renderer DLNA/UPnP en la
// red local: el servidor deja ver y tocar la biblioteca desde otros equipos
// (teles, apps DLNA, el reproductor de Windows); el renderer recibe la música
// que mandan otros (BubbleUPnP en el móvil, "Transmitir a" de Windows) y la
// toca aquí. Corre en el servicio, apagado por defecto: encendido, escucha
// por HTTP en un puerto libre y por SSDP en el 1900 (un socket bloqueado en
// la lectura, que no gasta mientras nadie pregunta). Nunca impide que la
// consola se suspenda.
package dlna

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Options es lo que se enciende y con qué nombre.
type Options struct {
	Name     string // como se ve en la red; vacío: "audiofilus (<equipo>)"
	Server   bool
	Renderer bool
	DataDir  string // data/: los UUID, la biblioteca y la caché del renderer
	Player   Player // el motor (necesario con Renderer)
}

// Status es lo que la interfaz muestra.
type Status struct {
	Server      bool   `json:"server"`
	Renderer    bool   `json:"renderer"`
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`         // la dirección del servidor HTTP
	Subscribers int    `json:"subscribers,omitempty"` // mandos suscritos al renderer
}

// Service es DLNA en marcha.
type Service struct {
	opts     Options
	ln       net.Listener
	srv      *http.Server
	ssdp     *ssdp
	media    *mediaServer
	rend     *renderer
	fetch    *fetcher
	serverID string
	rendID   string
	mu       sync.Mutex
}

// DefaultName es el nombre de un equipo: "audiofilus (<equipo>)".
func DefaultName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "audiofilus"
	}
	return fmt.Sprintf("audiofilus (%s)", h)
}

// Start enciende lo que diga opts (al menos una de las dos cosas).
func Start(opts Options) (*Service, error) {
	if !opts.Server && !opts.Renderer {
		return nil, errors.New("dlna: nothing to start")
	}
	if opts.Renderer && opts.Player == nil {
		return nil, errors.New("dlna: the renderer needs a player")
	}
	if opts.Name == "" {
		opts.Name = DefaultName()
	}
	s := &Service{opts: opts}
	s.serverID = persistentUUID(opts.DataDir, "dlna-server.uuid")
	s.rendID = persistentUUID(opts.DataDir, "dlna-renderer.uuid")
	s.fetch = newFetcher(filepath.Join(opts.DataDir, "cache", "dlna"))
	if opts.Server {
		s.media = &mediaServer{src: &libSource{dataDir: opts.DataDir}}
	}
	if opts.Renderer {
		s.rend = newRenderer(opts.Player, s.fetch)
	}
	ln, err := net.Listen("tcp4", ":0")
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.mux(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("dlna: http: %v", err)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	s.ssdp, err = startSSDP(port, s.devices)
	if err != nil {
		s.srv.Close()
		return nil, fmt.Errorf("ssdp: %w", err)
	}
	log.Printf("dlna: %q on port %d (server %v, renderer %v)", opts.Name, port, opts.Server, opts.Renderer)
	return s, nil
}

// Close se despide de la red y para.
func (s *Service) Close() {
	s.ssdp.close()
	_ = s.srv.Close()
	s.fetch.keep()
}

// Status es el estado para la interfaz.
func (s *Service) Status() Status {
	st := Status{Server: s.opts.Server, Renderer: s.opts.Renderer, Name: s.opts.Name}
	if ips := lanIPs(); len(ips) > 0 {
		st.URL = fmt.Sprintf("http://%s:%d/", ips[0], s.ln.Addr().(*net.TCPAddr).Port)
	}
	if s.rend != nil {
		st.Subscribers = s.rend.avt.count()
	}
	return st
}

// Changed avisa de que cambió lo que suena (para los eventos del renderer).
func (s *Service) Changed() {
	if s.rend != nil {
		go s.rend.changed()
	}
}

// IsRendererPath dice si path es una canción que mandó un mando.
func IsRendererPath(path string) bool { return strings.HasPrefix(path, "dlna://r/") }

// Open abre una canción del renderer (para el OpenRemote del motor).
func (s *Service) Open(path string) (decode.Source, bool, error) { return s.fetch.Open(path) }

// Track son las etiquetas de una canción del renderer.
func (s *Service) Track(path string) (Meta, bool) { return s.fetch.Track(path) }

// devices son los dispositivos que se anuncian.
func (s *Service) devices() []ssdpDevice {
	var out []ssdpDevice
	if s.opts.Server {
		out = append(out, ssdpDevice{uuid: s.serverID, devType: typeServer, services: []string{svcCD, svcCM}, desc: "/server/desc.xml"})
	}
	if s.opts.Renderer {
		out = append(out, ssdpDevice{uuid: s.rendID, devType: typeRenderer, services: []string{svcAVT, svcRC, svcCM}, desc: "/renderer/desc.xml"})
	}
	return out
}

// mux son las rutas HTTP.
func (s *Service) mux() http.Handler {
	m := http.NewServeMux()
	xmlDoc := func(doc string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
			_, _ = w.Write([]byte(doc))
		}
	}
	icon := iconPNG()
	serveIcon := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(icon)
	}
	cmEvents := newEvents(func() string {
		return propertySet([2]string{"SourceProtocolInfo", ""}, [2]string{"SinkProtocolInfo", ""}, [2]string{"CurrentConnectionIDs", "0"})
	})
	cm := func(source, sink string) handler {
		return func(a *action) ([]arg, error) {
			switch a.Name {
			case "GetProtocolInfo":
				return []arg{{"Source", source}, {"Sink", sink}}, nil
			case "GetCurrentConnectionIDs":
				return []arg{{"ConnectionIDs", "0"}}, nil
			case "GetCurrentConnectionInfo":
				dir := "Output"
				if sink != "" {
					dir = "Input"
				}
				return []arg{{"RcsID", "0"}, {"AVTransportID", "0"}, {"ProtocolInfo", ""}, {"PeerConnectionManager", ""}, {"PeerConnectionID", "-1"}, {"Direction", dir}, {"Status", "OK"}}, nil
			}
			return nil, errInvalidAction
		}
	}
	if s.media != nil {
		desc := deviceDesc(typeServer, s.opts.Name, s.serverID, "server", serverServices)
		m.HandleFunc("/server/desc.xml", xmlDoc(desc))
		m.HandleFunc("/server/icon.png", serveIcon)
		m.HandleFunc("/server/cd/scpd.xml", xmlDoc(scpdCD))
		m.HandleFunc("/server/cm/scpd.xml", xmlDoc(scpdCM))
		m.HandleFunc("/server/cd/control", func(w http.ResponseWriter, r *http.Request) {
			serveControl(svcCD, s.media.contentDirectory(r))(w, r)
		})
		m.HandleFunc("/server/cm/control", serveControl(svcCM, cm(sourceProtocols(), "")))
		cdEvents := newEvents(func() string {
			return propertySet([2]string{"SystemUpdateID", fmt.Sprint(s.media.src.get().update)})
		})
		m.HandleFunc("/server/cd/event", cdEvents.serve)
		m.HandleFunc("/server/cm/event", cmEvents.serve)
		m.HandleFunc("/media/", s.media.serveMedia)
		m.HandleFunc("/art/", s.media.serveArt)
	}
	if s.rend != nil {
		desc := deviceDesc(typeRenderer, s.opts.Name, s.rendID, "renderer", rendererServices)
		m.HandleFunc("/renderer/desc.xml", xmlDoc(desc))
		m.HandleFunc("/renderer/icon.png", serveIcon)
		m.HandleFunc("/renderer/avt/scpd.xml", xmlDoc(scpdAVT))
		m.HandleFunc("/renderer/rc/scpd.xml", xmlDoc(scpdRC))
		m.HandleFunc("/renderer/cm/scpd.xml", xmlDoc(scpdCM))
		m.HandleFunc("/renderer/avt/control", serveControl(svcAVT, s.rend.avTransport))
		m.HandleFunc("/renderer/rc/control", serveControl(svcRC, s.rend.renderingControl))
		m.HandleFunc("/renderer/cm/control", serveControl(svcCM, cm("", sinkProtocols())))
		m.HandleFunc("/renderer/avt/event", s.rend.avt.serve)
		m.HandleFunc("/renderer/rc/event", s.rend.rc.serve)
		m.HandleFunc("/renderer/cm/event", cmEvents.serve)
	}
	return m
}

// persistentUUID es el UUID guardado en data/<name> (lo crea la primera vez):
// el mismo equipo tiene que verse siempre igual en la red.
// RendererUDN es el UDN ("uuid:...") del renderer de este audiofilus, el
// guardado en dataDir ("" si nunca se encendió): así "Enviar a" no se ofrece
// a sí mismo.
func RendererUDN(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "dlna-renderer.uuid"))
	if id := strings.TrimSpace(string(b)); err == nil && len(id) == 36 {
		return "uuid:" + id
	}
	return ""
}

func persistentUUID(dataDir, name string) string {
	p := filepath.Join(dataDir, name)
	if b, err := os.ReadFile(p); err == nil {
		if id := strings.TrimSpace(string(b)); len(id) == 36 {
			return id
		}
	}
	id := newUUID()
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.WriteFile(p, []byte(id+"\n"), 0o644)
	return id
}

// iconPNG es el icono del dispositivo: un disco de 48×48 en los colores de
// audiofilus.
func iconPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			dx, dy := float64(x)-23.5, float64(y)-23.5
			d := dx*dx + dy*dy
			switch {
			case d < 16:
				img.Set(x, y, color.RGBA{0x0d, 0x0b, 0x17, 0xff})
			case d < 64:
				img.Set(x, y, color.RGBA{0xff, 0x55, 0x77, 0xff})
			case d < 22*22:
				img.Set(x, y, color.RGBA{0x2a, 0x22, 0x40, 0xff})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
