// Package mdns busca y anuncia servicios en la red local por mDNS (DNS por
// multicast, RFC 6762/6763), sin bibliotecas: lo justo para encontrar
// Chromecasts y servidores MPD, y para que otros encuentren el MPD de
// audiofilus (_mpd._tcp).
package mdns

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// groupAddr es el grupo multicast de mDNS.
var groupAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Instance es un servicio encontrado.
type Instance struct {
	Name string // el nombre de la instancia, sin el servicio
	IP   net.IP
	Port int
	TXT  map[string]string // claves en minúsculas
}

// Host es "ip:puerto".
func (i Instance) Host() string { return net.JoinHostPort(i.IP.String(), strconv.Itoa(i.Port)) }

// records es lo que se sabe de una instancia mientras llegan respuestas.
type records struct {
	target string
	port   uint16
	txt    map[string]string
	from   net.IP
}

// Browse busca instancias de service ("_mpd._tcp.local") hasta que se acabe
// ctx: una pregunta PTR al grupo desde un puerto cualquiera (así se contesta
// directo, "unicast legado"), y de las respuestas se sacan el nombre (PTR),
// el puerto y el host (SRV), el TXT y la dirección (la de quien contestó, o
// el A).
func Browse(ctx context.Context, service string) ([]Instance, error) {
	if b := native.Load(); b != nil {
		return browseNative(ctx, *b, service)
	}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	q := dnsQuery(service, dnsPTR)
	for range 2 {
		if _, err := conn.WriteTo(q, groupAddr); err != nil {
			return nil, err
		}
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	go func() {
		<-ctx.Done()
		_ = conn.SetReadDeadline(time.Now())
	}()
	recs := map[string]*records{} // por instancia
	addrs := map[string]net.IP{}  // nombre del host → IP
	get := func(name string) *records {
		k := strings.ToLower(name)
		if recs[k] == nil {
			recs[k] = &records{txt: map[string]string{}}
		}
		return recs[k]
	}
	names := map[string]string{} // instancia en minúsculas → como se escribe
	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		rs, err := parseDNS(buf[:n])
		if err != nil && len(rs) == 0 {
			continue
		}
		var ip net.IP // si no mandan el A, sirve la dirección desde la que hablaron
		if ua, ok := from.(*net.UDPAddr); ok {
			ip = ua.IP
		}
		for _, r := range rs {
			switch r.typ {
			case dnsPTR:
				if strings.EqualFold(r.name, service) {
					get(r.ptr).from = ip
					names[strings.ToLower(r.ptr)] = r.ptr
				}
			case dnsSRV:
				m := get(r.name)
				m.target, m.port, m.from = r.target, r.port, ip
				names[strings.ToLower(r.name)] = r.name
			case dnsTXT:
				get(r.name).txt = r.txt
			case dnsA:
				addrs[strings.ToLower(r.name)] = r.ip
			}
		}
	}
	suffix := "." + strings.ToLower(service)
	var out []Instance
	seen := map[string]bool{}
	for k, m := range recs {
		if !strings.HasSuffix(k, suffix) || m.port == 0 {
			continue
		}
		// Mejor la dirección desde la que contestó: el A puede traer las de
		// interfaces que desde aquí no se alcanzan (Docker, una VPN).
		ip := m.from
		if ip == nil {
			ip = addrs[strings.ToLower(m.target)]
		}
		if ip == nil {
			continue
		}
		name := names[k]
		if name == "" {
			name = k
		}
		in := Instance{Name: name[:len(name)-len(suffix)], IP: ip, Port: int(m.port), TXT: m.txt}
		if seen[in.Host()] {
			continue
		}
		seen[in.Host()] = true
		out = append(out, in)
	}
	return out, nil
}

// Responder anuncia una instancia de un servicio y contesta a quien la busca.
type Responder struct {
	conn     *net.UDPConn
	instance string // "<nombre>.<servicio>"
	service  string
	target   string // el host del SRV
	port     uint16
	txt      map[string]string
	stop     chan struct{}
	wg       sync.WaitGroup
	native   func() // retira el anuncio por Bonjour (entonces no hay conn)
}

// Announce anuncia name como instancia de service ("_mpd._tcp.local") en
// port, con su TXT. Varios procesos del mismo equipo pueden anunciarse a la
// vez (y con Avahi): el puerto 5353 se comparte.
func Announce(name, service string, port int, txt map[string]string) (*Responder, error) {
	if b := native.Load(); b != nil {
		return &Responder{native: publishNative(*b, name, service, port, txt)}, nil
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, groupAddr)
	if err != nil {
		return nil, err
	}
	// En el resto de interfaces también (el sistema solo une la suya por
	// defecto).
	for _, ip := range lanIPs() {
		_ = joinGroup(conn, ip)
	}
	if err := setTTL(conn); err != nil {
		log.Printf("mdns: ttl: %v", err)
	}
	// El host del SRV es propio (no el del equipo, que ya anuncia Avahi):
	// "<equipo>-audiofilus-<puerto>.local".
	host, _ := os.Hostname()
	host = strings.Map(func(r rune) rune {
		if r == '.' || r == ' ' {
			return '-'
		}
		return r
	}, host)
	if host == "" {
		host = "pc"
	}
	r := &Responder{conn: conn, instance: strings.ReplaceAll(name, ".", " ") + "." + service, service: service,
		target: fmt.Sprintf("%s-audiofilus-%d.local", host, port), port: uint16(port), txt: txt, stop: make(chan struct{})}
	r.wg.Add(2)
	go r.listen()
	go r.announce()
	return r, nil
}

// Close deja de anunciar (y avisa con un adiós: ttl 0).
func (r *Responder) Close() {
	if r.native != nil {
		r.native()
		return
	}
	close(r.stop)
	_, _ = r.conn.WriteTo(r.response(0, nil, nil, 0), groupAddr)
	r.conn.Close()
	r.wg.Wait()
}

// announce se anuncia al empezar (dos veces, como pide mDNS).
func (r *Responder) announce() {
	defer r.wg.Done()
	for i := range 3 {
		if _, err := r.conn.WriteTo(r.response(0, nil, nil, 1), groupAddr); err != nil {
			log.Printf("mdns: %v", err)
		}
		select {
		case <-r.stop:
			return
		case <-time.After(time.Duration(1<<i) * time.Second):
		}
	}
}

// listen contesta las preguntas por el servicio, la instancia o su host.
func (r *Responder) listen() {
	defer r.wg.Done()
	buf := make([]byte, 9000)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-r.stop:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		id, qs, err := parseQuestions(buf[:n])
		if err != nil || len(qs) == 0 {
			continue
		}
		var hit []question
		for _, q := range qs {
			switch {
			case strings.EqualFold(q.name, r.service) && (q.typ == dnsPTR || q.typ == dnsANY),
				strings.EqualFold(q.name, r.instance) && (q.typ == dnsSRV || q.typ == dnsTXT || q.typ == dnsANY),
				strings.EqualFold(q.name, r.target) && (q.typ == dnsA || q.typ == dnsANY):
				hit = append(hit, q)
			}
		}
		if len(hit) == 0 {
			continue
		}
		if from.Port != groupAddr.Port {
			// Unicast legado: se contesta directo, con su id y su pregunta.
			_, _ = r.conn.WriteToUDP(r.response(id, hit, from.IP, 1), from)
			continue
		}
		_, _ = r.conn.WriteTo(r.response(0, nil, from.IP, 1), groupAddr)
	}
}

// response arma la respuesta entera (PTR, SRV, TXT y A) para quien pregunta
// desde from (nil: para todos); ttl 0 es el adiós.
func (r *Responder) response(id uint16, qs []question, from net.IP, ttl uint32) []byte {
	b := make([]byte, 12, 512)
	b[0], b[1] = byte(id>>8), byte(id)
	b[2] = 0x84 // respuesta con autoridad
	ips := ipsFor(from)
	b[5] = byte(len(qs))
	b[7] = byte(3 + len(ips))
	for _, q := range qs {
		b = appendName(b, q.name)
		b = append(b, byte(q.typ>>8), byte(q.typ), 0, 1)
	}
	b = appendRR(b, r.service, dnsPTR, false, 4500*ttl, appendName(nil, r.instance))
	b = appendRR(b, r.instance, dnsSRV, true, 120*ttl, srvData(r.port, r.target))
	b = appendRR(b, r.instance, dnsTXT, true, 4500*ttl, txtData(r.txt))
	for _, ip := range ips {
		b = appendRR(b, r.target, dnsA, true, 120*ttl, ip.To4())
	}
	return b
}

// lanNets son las redes IPv4 de las interfaces de red activas: sin la de
// bucle local, las punto a punto (VPN) ni las virtuales (Docker, puentes de
// máquinas virtuales), que desde otro equipo no se alcanzan.
func lanNets() []*net.IPNet {
	ifs, err := net.Interfaces()
	var out []*net.IPNet
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 || ifi.Flags&net.FlagMulticast == 0 ||
			ifi.Flags&net.FlagPointToPoint != 0 || virtualIface(ifi.Name) {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil {
					out = append(out, &net.IPNet{IP: ip4, Mask: n.Mask})
				}
			}
		}
	}
	if len(out) == 0 {
		// Android (desde la 11) no deja leer las interfaces (err): la
		// dirección por la que sale el multicast, con su red supuesta /24.
		if ip := RouteIP(groupAddr.IP); ip != nil {
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(24, 32)})
		} else if err != nil {
			log.Printf("mdns: interfaces: %v", err)
		}
	}
	return out
}

// RouteIP es la dirección propia (IPv4) por la que se llega a remote, sin
// mandar nada: lo que elige el sistema al "conectar" un socket UDP. Nil si
// no hay red, o si es la del bucle local.
func RouteIP(remote net.IP) net.IP {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: remote, Port: 9})
	if err != nil {
		return nil
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return nil
	}
	return ip
}

// virtualIface dice si una interfaz es de Docker o de máquinas virtuales.
func virtualIface(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "cni", "flannel", "podman"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// lanIPs son las direcciones de lanNets.
func lanIPs() []net.IP {
	var out []net.IP
	for _, n := range lanNets() {
		out = append(out, n.IP)
	}
	return out
}

// ipsFor son las direcciones que se anuncian a quien pregunta desde from:
// la de su red si la hay (así no se le da una que no alcanza); si no (una
// pregunta del propio equipo, o un anuncio), todas.
func ipsFor(from net.IP) []net.IP {
	nets := lanNets()
	if from != nil {
		for _, n := range nets {
			if n.Contains(from) {
				return []net.IP{n.IP}
			}
		}
	}
	var out []net.IP
	for _, n := range nets {
		out = append(out, n.IP)
	}
	return out
}

// LANIPs son las direcciones IPv4 del equipo en la red local (sin bucle
// local, VPN ni interfaces virtuales): las que se le pueden dar a otro
// equipo para conectarse.
func LANIPs() []net.IP { return lanIPs() }
