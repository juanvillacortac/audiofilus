package mdns

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Bonjour es el Bonjour del sistema, para donde la app no puede hablar mDNS
// por su cuenta: en iOS, mandar o recibir multicast pide un permiso de Apple
// (com.apple.developer.networking.multicast), y solo Bonjour queda libre. Lo
// pone mobile (lo implementa la app de Swift), así que habla con tipos que
// gomobile entiende: service va sin ".local" ("_mpd._tcp"), y las
// instancias y el TXT van en texto.
type Bonjour interface {
	// Browse busca service durante ms milisegundos y devuelve lo
	// encontrado, una instancia por línea: el nombre, la IPv4, el puerto y
	// el TXT (clave=valor), separados por tabuladores. Bloquea.
	Browse(service string, ms int) string
	// Publish anuncia la instancia id, con el TXT como en Browse (clave=valor
	// separados por tabuladores); Unpublish la retira.
	Publish(id int, name, service string, port int, txt string)
	Unpublish(id int)
}

// native es el Bonjour puesto con SetBonjour (nil: mDNS propio).
var native atomic.Pointer[Bonjour]

// nativeIDs numera los anuncios por Bonjour.
var nativeIDs atomic.Int64

// SetBonjour hace que Browse y Announce vayan por b (nil: por mDNS propio).
func SetBonjour(b Bonjour) {
	if b == nil {
		native.Store(nil)
		return
	}
	native.Store(&b)
}

// browseNative es Browse por Bonjour: busca hasta el plazo de ctx (3 s sin
// plazo).
func browseNative(ctx context.Context, b Bonjour, service string) ([]Instance, error) {
	ms := 3000
	if d, ok := ctx.Deadline(); ok {
		ms = int(time.Until(d) / time.Millisecond)
	}
	if ms <= 0 {
		return nil, ctx.Err()
	}
	var out []Instance
	for _, line := range strings.Split(b.Browse(strings.TrimSuffix(service, ".local"), ms), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		ip := net.ParseIP(f[1])
		port, _ := strconv.Atoi(f[2])
		if ip == nil || port <= 0 || port > 65535 {
			continue
		}
		in := Instance{Name: f[0], IP: ip, Port: port, TXT: map[string]string{}}
		for _, kv := range f[3:] {
			k, v, _ := strings.Cut(kv, "=")
			in.TXT[strings.ToLower(k)] = v
		}
		out = append(out, in)
	}
	return out, nil
}

// publishNative anuncia por Bonjour; lo que devuelve retira el anuncio.
func publishNative(b Bonjour, name, service string, port int, txt map[string]string) func() {
	id := int(nativeIDs.Add(1))
	var kvs []string
	for k, v := range txt {
		kvs = append(kvs, k+"="+v)
	}
	b.Publish(id, name, strings.TrimSuffix(service, ".local"), port, strings.Join(kvs, "\t"))
	return func() { b.Unpublish(id) }
}
