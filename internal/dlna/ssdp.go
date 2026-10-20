package dlna

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SSDP es cómo se encuentran los equipos UPnP: cada uno anuncia por
// multidifusión (239.255.255.250:1900) que está (NOTIFY ssdp:alive), cada
// cierto tiempo y al irse (ssdp:byebye), y contesta a las búsquedas
// (M-SEARCH) de quien busca con su dirección (LOCATION: la URL de su
// descripción). El oyente es un socket bloqueado en la lectura: no gasta
// nada mientras nadie pregunta.

var ssdpAddr = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

// maxAge es cuánto vale un anuncio (CACHE-CONTROL); se repite a la mitad.
const maxAge = 1800

// ssdpDevice es un dispositivo que se anuncia.
type ssdpDevice struct {
	uuid     string
	devType  string
	services []string
	desc     string // la ruta de su descripción ("/server/desc.xml")
}

// targets son los NT de un dispositivo, con su USN.
func (d ssdpDevice) targets() [][2]string {
	out := [][2]string{
		{"upnp:rootdevice", "uuid:" + d.uuid + "::upnp:rootdevice"},
		{"uuid:" + d.uuid, "uuid:" + d.uuid},
		{d.devType, "uuid:" + d.uuid + "::" + d.devType},
	}
	for _, s := range d.services {
		out = append(out, [2]string{s, "uuid:" + d.uuid + "::" + s})
	}
	return out
}

// ssdp anuncia y contesta por los dispositivos que haya.
type ssdp struct {
	port    int // el del servidor HTTP
	devices func() []ssdpDevice
	conn    *net.UDPConn
	stop    chan struct{}
	done    sync.WaitGroup
}

// startSSDP empieza a escuchar y a anunciarse.
func startSSDP(port int, devices func() []ssdpDevice) (*ssdp, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, ssdpAddr)
	if err != nil {
		return nil, err
	}
	// En el resto de interfaces también (el sistema solo une la suya por
	// defecto): en un PC con varias redes, la de la tele puede ser otra.
	for _, ip := range lanIPs() {
		_ = joinGroup(conn, ip)
	}
	s := &ssdp{port: port, devices: devices, conn: conn, stop: make(chan struct{})}
	s.done.Add(2)
	go s.listen()
	go s.announce()
	return s, nil
}

// close se despide (byebye) y deja de escuchar.
func (s *ssdp) close() {
	close(s.stop)
	s.notifyAll("ssdp:byebye")
	s.conn.Close()
	s.done.Wait()
}

// announce manda ssdp:alive al empezar (dos veces: es UDP) y cada maxAge/2.
func (s *ssdp) announce() {
	defer s.done.Done()
	s.notifyAll("ssdp:alive")
	t := time.NewTimer(2 * time.Second)
	for {
		select {
		case <-s.stop:
			t.Stop()
			return
		case <-t.C:
			s.notifyAll("ssdp:alive")
			t.Reset(maxAge / 2 * time.Second)
		}
	}
}

// notifyAll manda un NOTIFY por cada interfaz, con su LOCATION en ella.
func (s *ssdp) notifyAll(nts string) {
	for _, ip := range lanIPs() {
		c, err := net.DialUDP("udp4", &net.UDPAddr{IP: ip}, ssdpAddr)
		if err != nil {
			continue
		}
		for _, d := range s.devices() {
			for _, t := range d.targets() {
				var b bytes.Buffer
				b.WriteString("NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\n")
				if nts == "ssdp:alive" {
					fmt.Fprintf(&b, "CACHE-CONTROL: max-age=%d\r\nLOCATION: %s\r\nSERVER: %s\r\n", maxAge, s.location(ip, d), serverHeader)
				}
				fmt.Fprintf(&b, "NT: %s\r\nNTS: %s\r\nUSN: %s\r\n\r\n", t[0], nts, t[1])
				_, _ = c.Write(b.Bytes())
			}
		}
		c.Close()
	}
}

// location es la URL de la descripción de d, en la dirección ip.
func (s *ssdp) location(ip net.IP, d ssdpDevice) string {
	return fmt.Sprintf("http://%s:%d%s", ip, s.port, d.desc)
}

// listen contesta a los M-SEARCH.
func (s *ssdp) listen() {
	defer s.done.Done()
	buf := make([]byte, 2048)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
			}
			log.Printf("dlna: ssdp read: %v", err)
			time.Sleep(time.Second)
			continue
		}
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf[:n])))
		if err != nil || req.Method != "M-SEARCH" || req.Header.Get("MAN") != `"ssdp:discover"` {
			continue
		}
		st := req.Header.Get("ST")
		mx := 1
		fmt.Sscan(req.Header.Get("MX"), &mx)
		go s.reply(from, st, min(max(mx, 1), 5))
	}
}

// reply contesta a una búsqueda de st (tras una espera al azar de hasta mx
// segundos, como pide SSDP para no saturar a quien busca).
func (s *ssdp) reply(to *net.UDPAddr, st string, mx int) {
	ip := localIPFor(to.IP)
	if ip == nil {
		return
	}
	delay := time.Duration(rand.Int64N(int64(mx) * int64(time.Second) / 2))
	select {
	case <-time.After(delay):
	case <-s.stop:
		return
	}
	for _, d := range s.devices() {
		for _, t := range d.targets() {
			if st != "ssdp:all" && st != t[0] {
				continue
			}
			msg := ssdpResponse(t[0], t[1], s.location(ip, d))
			_, _ = s.conn.WriteToUDP([]byte(msg), to)
		}
	}
}

// ssdpResponse es la respuesta a una búsqueda.
func ssdpResponse(st, usn, location string) string {
	return fmt.Sprintf("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=%d\r\nDATE: %s\r\nEXT:\r\nLOCATION: %s\r\nSERVER: %s\r\nST: %s\r\nUSN: %s\r\n\r\n",
		maxAge, time.Now().UTC().Format(http.TimeFormat), location, serverHeader, st, usn)
}

// serverHeader es el SERVER de los anuncios y las respuestas.
const serverHeader = "Linux/1.0 UPnP/1.0 audiofilus/1.0"

// lanIPs son las direcciones IPv4 de las interfaces de red activas (sin la
// de bucle local).
func lanIPs() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil {
					out = append(out, ip4)
				}
			}
		}
	}
	return out
}

// localIPFor es la dirección propia por la que se llega a remote (la de la
// interfaz de su red).
func localIPFor(remote net.IP) net.IP {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: remote, Port: 1900})
	if err != nil {
		return nil
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP
}

// hostIP es la IP propia que se ve desde quien hizo r (para las URL que van
// en las respuestas): la del Host de la petición.
func hostIP(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	if strings.TrimSpace(h) == "" {
		if ip := localIPFor(net.ParseIP(strings.Split(r.RemoteAddr, ":")[0])); ip != nil {
			return ip.String()
		}
	}
	return h
}
