package mpris

import (
	"fmt"
	"log"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// El mismo reproductor, pero para los audífonos bluetooth: se publica en el
// bus del sistema y se registra en BlueZ (org.bluez.Media1.RegisterPlayer).
// Con eso BlueZ le cuenta a los audífonos si audiofilus suena (AVRCP) y les
// pasa sus botones como llamadas a Play, Pause, Next... en vez de teclas. Sin
// esto los audífonos adivinaban: los que mandan PLAY o PAUSE según lo que
// creen (el doble toque de los Huawei FreeBuds, la detección de puesta que
// pausa al quitárselos y reanuda al ponérselos) se desincronizaban.

const (
	bluezName   = "org.bluez"
	bluezMedia  = "org.bluez.Media1"
	bluezPlayer = dbus.ObjectPath("/org/audiofilus/player")
)

// BlueZ es el reproductor registrado en BlueZ.
type BlueZ struct {
	*Server
	mu         sync.Mutex
	registered map[dbus.ObjectPath]bool // los adaptadores donde está registrado
}

// StartBlueZ publica el reproductor en el bus del sistema y lo registra en
// cada adaptador bluetooth, también en los que aparezcan después (al
// encender el bluetooth) y si bluetoothd se reinicia. Sin bus del sistema o
// sin BlueZ devuelve un error y los botones siguen llegando como teclas.
func StartBlueZ(eng *engine.Engine, quit func()) (*BlueZ, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, bluezName).Store(&owner); err != nil {
		conn.Close()
		return nil, fmt.Errorf("no BlueZ on the system bus")
	}
	s, err := publish(conn, bluezPlayer, eng, quit)
	if err != nil {
		conn.Close()
		return nil, err
	}
	b := &BlueZ{Server: s, registered: map[dbus.ObjectPath]bool{}}
	// Adaptadores nuevos y bluetoothd que vuelve: se registra otra vez.
	_ = conn.AddMatchSignal(dbus.WithMatchSender(bluezName), dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"), dbus.WithMatchMember("InterfacesAdded"))
	_ = conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, bluezName))
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	go b.watch(signals)
	b.registerAll()
	return b, nil
}

// Registered dice si está registrado en algún adaptador: entonces los
// audífonos saben si suena y PLAY y PAUSE se pueden tomar al pie de la letra.
func (b *BlueZ) Registered() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.registered) > 0
}

// watch atiende las señales de BlueZ hasta que se cierra.
func (b *BlueZ) watch(signals chan *dbus.Signal) {
	for {
		select {
		case <-b.stop:
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			switch sig.Name {
			case "org.freedesktop.DBus.NameOwnerChanged":
				// bluetoothd se fue (o volvió): sus registros se perdieron.
				b.mu.Lock()
				b.registered = map[dbus.ObjectPath]bool{}
				b.mu.Unlock()
				if len(sig.Body) == 3 && sig.Body[2] != "" {
					b.registerAll()
				}
			case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
				if len(sig.Body) == 2 {
					if ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant); ok {
						if _, ok := ifaces[bluezMedia]; ok {
							if p, ok := sig.Body[0].(dbus.ObjectPath); ok {
								b.register(p)
							}
						}
					}
				}
			}
		}
	}
}

// registerAll se registra en cada adaptador que ya tenga Media1.
func (b *BlueZ) registerAll() {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := b.conn.Object(bluezName, "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs)
	if err != nil {
		log.Printf("bluetooth player: %v", err)
		return
	}
	for p, ifaces := range objs {
		if _, ok := ifaces[bluezMedia]; ok {
			b.register(p)
		}
	}
}

// register se registra en el adaptador adapter, con el estado de ahora.
func (b *BlueZ) register(adapter dbus.ObjectPath) {
	b.mu.Lock()
	done := b.registered[adapter]
	b.mu.Unlock()
	if done {
		return
	}
	props, derr := b.props.GetAll(ifacePlay)
	if derr != nil {
		log.Printf("bluetooth player: %v", derr)
		return
	}
	err := b.conn.Object(bluezName, adapter).Call(bluezMedia+".RegisterPlayer", 0, bluezPlayer, props).Err
	if err != nil {
		if e, ok := err.(dbus.Error); !ok || e.Name != "org.bluez.Error.AlreadyExists" {
			log.Printf("bluetooth player on %s: %v", adapter, err)
			return
		}
	}
	log.Printf("bluetooth player registered on %s", adapter)
	b.mu.Lock()
	b.registered[adapter] = true
	b.mu.Unlock()
}

// Close se quita de BlueZ y deja de publicar el reproductor.
func (b *BlueZ) Close() {
	b.mu.Lock()
	adapters := b.registered
	b.registered = map[dbus.ObjectPath]bool{}
	b.mu.Unlock()
	for a := range adapters {
		_ = b.conn.Object(bluezName, a).Call(bluezMedia+".UnregisterPlayer", 0, bluezPlayer).Err
	}
	b.Server.Close()
}
