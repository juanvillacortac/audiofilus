package daemon

// MulticastHook, si está, se llama cuando el servicio empieza o deja de
// necesitar el multicast de la red (DLNA o MPD encendidos: contestar a
// quien los busca por SSDP o mDNS). En Android el Wi-Fi tira los paquetes
// multicast salvo con un MulticastLock, que gasta batería: la app lo toma
// solo mientras on (mobile/).
var MulticastHook func(on bool)

// multicastChanged avisa a MulticastHook si cambió lo que hace falta.
func (d *Daemon) multicastChanged() {
	h := MulticastHook
	if h == nil {
		return
	}
	d.multicastMu.Lock()
	defer d.multicastMu.Unlock()
	on := d.dlnaOn.Load() || d.mpdOn.Load()
	if on != d.multicastOn {
		d.multicastOn = on
		h(on)
	}
}
