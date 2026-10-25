package daemon

import (
	"log"

	"github.com/juanvillacortac/audiofilus/internal/control"
)

// Mientras la interfaz manda la música a otro aparato (Chromecast, DLNA,
// MPD), el motor de aquí está en pausa y la sesión vive en la interfaz. Los
// controles que lee el servicio (los gestos del sleep con la pantalla
// apagada, el atajo de FN, los audífonos, MPRIS) no tocan el motor: se dejan
// en una cola corta que la interfaz recoge con el estado (RemoteSeq y
// RemoteActs) y manda al aparato. La interfaz lo pide con "remote on" y lo
// retira con "remote off"; si se cierra, se retira solo ("detached").

// remoteKeep es cuántas órdenes se guardan: la interfaz las recoge en cada
// estado, así que con unas pocas basta aunque lleguen seguidas.
const remoteKeep = 16

// remote deja cmd para la interfaz si está mandando la música a otro aparato.
// Dice si lo hizo (y entonces no hay que tocar el motor).
func (d *Daemon) remote(cmd string) bool {
	d.mu.Lock()
	if !d.remoteOn {
		d.mu.Unlock()
		return false
	}
	d.remoteSeq++
	d.remoteActs = append(d.remoteActs, cmd)
	if len(d.remoteActs) > remoteKeep {
		d.remoteActs = d.remoteActs[len(d.remoteActs)-remoteKeep:]
	}
	d.mu.Unlock()
	log.Printf("remote: %s (to the cast device)", cmd)
	control.Notify()
	return true
}

// remoteActive dice si el desvío está puesto.
func (d *Daemon) remoteActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.remoteOn
}

// setRemote enciende o apaga el desvío.
func (d *Daemon) setRemote(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.remoteOn != on {
		log.Printf("remote control: %v", on)
	}
	d.remoteOn = on
	if !on {
		d.remoteActs = nil
	}
}
