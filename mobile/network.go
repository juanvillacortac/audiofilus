package mobile

import (
	"github.com/juanvillacortac/audiofilus/internal/daemon"
	"github.com/juanvillacortac/audiofilus/internal/mdns"
)

// Bonjour es el Bonjour del sistema, que la app de iOS hace por audiofilus
// (mdns.Bonjour, ver allí): iOS no deja a la app mandar ni recibir
// multicast por su cuenta (mDNS), pero sí buscar y anunciar servicios con
// Bonjour. Con él, "Enviar a" encuentra Chromecasts y servidores MPD, y
// otros encuentran el MPD del teléfono.
type Bonjour interface {
	// Browse busca service ("_mpd._tcp") durante ms milisegundos y devuelve
	// lo encontrado, una instancia por línea: el nombre, la IPv4, el puerto
	// y el TXT (clave=valor), separados por tabuladores. Se llama desde una
	// goroutine (nunca desde el hilo principal) y bloquea.
	Browse(service string, ms int) string
	// Publish anuncia la instancia id, con el TXT como en Browse (clave=valor
	// separados por tabuladores); Unpublish la retira.
	Publish(id int, name, service string, port int, txt string)
	Unpublish(id int)
}

// SetBonjour hace que la búsqueda y el anuncio por mDNS vayan por el
// Bonjour de la app. Llamarla antes de Start.
func SetBonjour(b Bonjour) {
	if b == nil {
		mdns.SetBonjour(nil)
		return
	}
	mdns.SetBonjour(b)
}

// Multicast es lo que la app de Android hace por audiofilus en la red: el
// Wi-Fi tira los paquetes multicast (las búsquedas por SSDP y mDNS de otros
// aparatos) salvo con un MulticastLock, que gasta batería.
type Multicast interface {
	// Hold toma (on) o suelta el MulticastLock: on mientras DLNA o el
	// servidor MPD están encendidos.
	Hold(on bool)
}

// SetMulticast le da a audiofilus la app que toma el MulticastLock.
// Llamarla antes de Start.
func SetMulticast(m Multicast) {
	if m == nil {
		daemon.MulticastHook = nil
		return
	}
	daemon.MulticastHook = m.Hold
}
