package cast

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
)

// Búsqueda de Chromecasts por mDNS (DNS por multicast, sin bibliotecas): una
// pregunta PTR por _googlecast._tcp.local a 224.0.0.251:5353 desde un puerto
// cualquiera (así los aparatos contestan directo, "unicast legado"), y de las
// respuestas se sacan el nombre (TXT fn=), el modelo (md=), el id y la
// dirección (SRV y A).

const (
	mdnsAddr    = "224.0.0.251:5353"
	castService = "_googlecast._tcp.local"
)

// Tipos de registro DNS que se usan.
const (
	dnsA   = 1
	dnsPTR = 12
	dnsTXT = 16
	dnsSRV = 33
)

// discoverChromecast busca Chromecasts hasta que se acabe ctx.
func discoverChromecast(ctx context.Context) ([]Device, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", mdnsAddr)
	if err != nil {
		return nil, err
	}
	q := dnsQuery(castService, dnsPTR)
	for range 2 {
		if _, err := conn.WriteTo(q, dst); err != nil {
			return nil, err
		}
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	recs := map[string]*mdnsRecords{} // por instancia
	addrs := map[string]net.IP{}      // nombre del host → IP
	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		rs, err := parseDNS(buf[:n])
		if err != nil {
			continue
		}
		var ip net.IP // si el aparato no manda el A, sirve la dirección desde la que habló
		if ua, ok := from.(*net.UDPAddr); ok {
			ip = ua.IP
		}
		for _, r := range rs {
			switch r.typ {
			case dnsPTR:
				if strings.EqualFold(r.name, castService) {
					inst(recs, r.ptr).from = ip
				}
			case dnsSRV:
				m := inst(recs, r.name)
				m.target, m.port, m.from = r.target, r.port, ip
			case dnsTXT:
				inst(recs, r.name).txt = r.txt
			case dnsA:
				addrs[strings.ToLower(r.name)] = r.ip
			}
		}
	}
	var out []Device
	for name, m := range recs {
		if !strings.HasSuffix(strings.ToLower(name), "."+castService) || m.port == 0 {
			continue
		}
		ip := addrs[strings.ToLower(m.target)]
		if ip == nil {
			ip = m.from
		}
		if ip == nil {
			continue
		}
		d := Device{Kind: Chromecast, ID: m.txt["id"], Name: m.txt["fn"], Model: m.txt["md"],
			Host: net.JoinHostPort(ip.String(), strconv.Itoa(int(m.port)))}
		if d.Name == "" {
			d.Name = strings.TrimSuffix(name, "."+castService)
		}
		out = append(out, d)
	}
	return dedupe(out), nil
}

// mdnsRecords es lo que se sabe de una instancia.
type mdnsRecords struct {
	target string
	port   uint16
	txt    map[string]string
	from   net.IP
}

func inst(m map[string]*mdnsRecords, name string) *mdnsRecords {
	if m[name] == nil {
		m[name] = &mdnsRecords{txt: map[string]string{}}
	}
	return m[name]
}

// dnsQuery arma una pregunta DNS de un registro.
func dnsQuery(name string, typ uint16) []byte {
	b := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(b[4:], 1) // una pregunta
	b = appendName(b, name)
	b = binary.BigEndian.AppendUint16(b, typ)
	b = binary.BigEndian.AppendUint16(b, 1) // IN
	return b
}

// appendName escribe un nombre DNS (sin compresión).
func appendName(b []byte, name string) []byte {
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0)
}

// dnsRecord es un registro de una respuesta.
type dnsRecord struct {
	name   string
	typ    uint16
	ptr    string
	target string
	port   uint16
	txt    map[string]string
	ip     net.IP
}

var errDNS = errors.New("cast: bad dns message")

// parseDNS lee las respuestas y los adicionales de un mensaje DNS.
func parseDNS(b []byte) ([]dnsRecord, error) {
	if len(b) < 12 {
		return nil, errDNS
	}
	qd := int(binary.BigEndian.Uint16(b[4:]))
	total := int(binary.BigEndian.Uint16(b[6:])) + int(binary.BigEndian.Uint16(b[8:])) + int(binary.BigEndian.Uint16(b[10:]))
	off := 12
	for range qd {
		_, n, err := readName(b, off)
		if err != nil {
			return nil, err
		}
		off = n + 4
	}
	var out []dnsRecord
	for range total {
		name, n, err := readName(b, off)
		if err != nil {
			return out, err
		}
		if n+10 > len(b) {
			return out, errDNS
		}
		r := dnsRecord{name: name, typ: binary.BigEndian.Uint16(b[n:])}
		rdlen := int(binary.BigEndian.Uint16(b[n+8:]))
		rd := n + 10
		if rd+rdlen > len(b) {
			return out, errDNS
		}
		switch r.typ {
		case dnsPTR:
			r.ptr, _, err = readName(b, rd)
		case dnsSRV:
			if rdlen >= 6 {
				r.port = binary.BigEndian.Uint16(b[rd+4:])
				r.target, _, err = readName(b, rd+6)
			}
		case dnsTXT:
			r.txt = map[string]string{}
			for i := rd; i < rd+rdlen; {
				l := int(b[i])
				i++
				if i+l > rd+rdlen {
					break
				}
				kv := string(b[i : i+l])
				if k, v, ok := strings.Cut(kv, "="); ok {
					r.txt[strings.ToLower(k)] = v
				}
				i += l
			}
		case dnsA:
			if rdlen == 4 {
				r.ip = net.IP(append([]byte(nil), b[rd:rd+4]...))
			}
		}
		if err != nil {
			return out, err
		}
		out = append(out, r)
		off = rd + rdlen
	}
	return out, nil
}

// readName lee un nombre DNS en off (con compresión) y dice dónde sigue.
func readName(b []byte, off int) (string, int, error) {
	var labels []string
	next := -1
	for jumps := 0; ; {
		if off >= len(b) {
			return "", 0, errDNS
		}
		l := int(b[off])
		switch {
		case l == 0:
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, "."), next, nil
		case l&0xc0 == 0xc0:
			if off+1 >= len(b) {
				return "", 0, errDNS
			}
			if next < 0 {
				next = off + 2
			}
			off = int(binary.BigEndian.Uint16(b[off:]) & 0x3fff)
			if jumps++; jumps > 32 {
				return "", 0, errDNS
			}
		default:
			if off+1+l > len(b) {
				return "", 0, errDNS
			}
			labels = append(labels, string(b[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
