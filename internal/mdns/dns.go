package mdns

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
)

// El formato de los mensajes DNS (RFC 1035), lo justo para mDNS: preguntas,
// y registros A, PTR, SRV y TXT, de ida y de vuelta.

// Tipos de registro DNS que se usan.
const (
	dnsA   = 1
	dnsPTR = 12
	dnsTXT = 16
	dnsSRV = 33
	dnsANY = 255
)

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

var errDNS = errors.New("mdns: bad dns message")

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

// question es una pregunta de un mensaje DNS.
type question struct {
	name string
	typ  uint16
}

// parseQuestions lee el id y las preguntas de un mensaje (nada si es una
// respuesta).
func parseQuestions(b []byte) (uint16, []question, error) {
	if len(b) < 12 {
		return 0, nil, errDNS
	}
	if b[2]&0x80 != 0 {
		return 0, nil, nil // una respuesta
	}
	id := binary.BigEndian.Uint16(b)
	qd := int(binary.BigEndian.Uint16(b[4:]))
	off := 12
	var out []question
	for range qd {
		name, n, err := readName(b, off)
		if err != nil || n+4 > len(b) {
			return id, out, errDNS
		}
		out = append(out, question{name: name, typ: binary.BigEndian.Uint16(b[n:])})
		off = n + 4
	}
	return id, out, nil
}

// appendRR escribe un registro: nombre, tipo, clase IN (con el bit de
// "cache flush" si flush), ttl en segundos y datos.
func appendRR(b []byte, name string, typ uint16, flush bool, ttl uint32, rdata []byte) []byte {
	b = appendName(b, name)
	b = binary.BigEndian.AppendUint16(b, typ)
	class := uint16(1)
	if flush {
		class |= 0x8000
	}
	b = binary.BigEndian.AppendUint16(b, class)
	b = binary.BigEndian.AppendUint32(b, ttl)
	b = binary.BigEndian.AppendUint16(b, uint16(len(rdata)))
	return append(b, rdata...)
}

// txtData son los datos de un TXT: cada "clave=valor" con su largo.
func txtData(txt map[string]string) []byte {
	var b []byte
	for k, v := range txt {
		kv := k + "=" + v
		if len(kv) > 255 {
			continue
		}
		b = append(b, byte(len(kv)))
		b = append(b, kv...)
	}
	if len(b) == 0 {
		b = []byte{0}
	}
	return b
}

// srvData son los datos de un SRV: prioridad, peso, puerto y destino.
func srvData(port uint16, target string) []byte {
	b := []byte{0, 0, 0, 0}
	b = binary.BigEndian.AppendUint16(b, port)
	return appendName(b, target)
}
