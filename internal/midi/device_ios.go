//go:build ios && cgo

package midi

// En iOS, MIDI va por CoreMIDI: los aparatos por USB (con el adaptador) y los
// Bluetooth ya emparejados aparecen como fuentes (entradas) y destinos
// (salidas). Device.Card es la fuente y Device.Dev el destino del mismo
// nombre (-1 si no tiene). Los mensajes llegan en un hilo de CoreMIDI, en
// paquetes que se le pasan a Go tal cual (goMIDIRead). La lista se vuelve a
// leer con Watch (sondeando): las notificaciones de CoreMIDI necesitan un run
// loop en el hilo que creó el cliente, y las goroutines no tienen.

/*
#cgo LDFLAGS: -framework CoreMIDI -framework CoreFoundation
#include <stdint.h>
#include <CoreMIDI/CoreMIDI.h>
#include <CoreFoundation/CoreFoundation.h>

extern void goMIDIRead(int id, unsigned char *data, int n);

// afRead le pasa a Go cada paquete que llega a un puerto de entrada.
static void afRead(const MIDIPacketList *list, void *refCon, void *srcConnRefCon) {
	const MIDIPacket *p = &list->packet[0];
	for (UInt32 i = 0; i < list->numPackets; i++) {
		goMIDIRead((int)(intptr_t)refCon, (unsigned char *)p->data, (int)p->length);
		p = MIDIPacketNext(p);
	}
}

static OSStatus afClient(MIDIClientRef *c) {
	return MIDIClientCreate(CFSTR("audiofilus"), NULL, NULL, c);
}

static OSStatus afInPort(MIDIClientRef c, int id, MIDIPortRef *p) {
	return MIDIInputPortCreate(c, CFSTR("audiofilus in"), afRead, (void *)(intptr_t)id, p);
}

static OSStatus afOutPort(MIDIClientRef c, MIDIPortRef *p) {
	return MIDIOutputPortCreate(c, CFSTR("audiofilus out"), p);
}

// afSend manda n bytes (mensajes completos) a un destino.
static OSStatus afSend(MIDIPortRef port, MIDIEndpointRef dest, const Byte *data, int n) {
	Byte buf[1024];
	MIDIPacketList *list = (MIDIPacketList *)buf;
	MIDIPacket *p = MIDIPacketListInit(list);
	p = MIDIPacketListAdd(list, sizeof(buf), p, 0, (ByteCount)n, data);
	if (p == NULL) {
		return -1;
	}
	return MIDISend(port, dest, list);
}

// afName escribe en buf el nombre de un punto (fuente o destino).
static int afName(MIDIEndpointRef e, char *buf, int n) {
	CFStringRef s = NULL;
	if (MIDIObjectGetStringProperty(e, kMIDIPropertyDisplayName, &s) != noErr || s == NULL) {
		return 0;
	}
	Boolean ok = CFStringGetCString(s, buf, n, kCFStringEncodingUTF8);
	CFRelease(s);
	return ok ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// endpointName es el nombre de una fuente o un destino.
func endpointName(e C.MIDIEndpointRef) string {
	var buf [256]C.char
	if C.afName(e, &buf[0], C.int(len(buf))) == 0 {
		return ""
	}
	return C.GoString(&buf[0])
}

// List son las fuentes MIDI, cada una con el destino del mismo nombre.
func List() ([]Device, error) {
	var outs []string
	for j := 0; j < int(C.MIDIGetNumberOfDestinations()); j++ {
		outs = append(outs, endpointName(C.MIDIGetDestination(C.ItemCount(j))))
	}
	var devs []Device
	for i := 0; i < int(C.MIDIGetNumberOfSources()); i++ {
		name := endpointName(C.MIDIGetSource(C.ItemCount(i)))
		out := -1
		for j, o := range outs {
			if o == name {
				out = j
				break
			}
		}
		devs = append(devs, Device{Path: fmt.Sprintf("coremidi:%d", i), Name: name, Card: i, Dev: out})
	}
	return devs, nil
}

// El cliente de CoreMIDI (uno por app) y los puertos abiertos, por su número
// (el refCon de la entrada).
var (
	clientOnce sync.Once
	client     C.MIDIClientRef
	clientErr  error
	portsMu    sync.Mutex
	ports      = map[int]*Port{}
	nextPort   int
)

//export goMIDIRead
func goMIDIRead(id C.int, data *C.uchar, n C.int) {
	portsMu.Lock()
	p := ports[int(id)]
	portsMu.Unlock()
	if p == nil || n <= 0 {
		return
	}
	p.feed(C.GoBytes(unsafe.Pointer(data), n), time.Now())
}

// Open conecta la fuente del aparato (y su destino, si tiene) y empieza a
// pasar sus mensajes a handle, desde el hilo de CoreMIDI: handle tiene que
// volver rápido.
func Open(d Device, handle func(Event)) (*Port, error) {
	clientOnce.Do(func() {
		if st := C.afClient(&client); st != 0 {
			clientErr = fmt.Errorf("midi: MIDIClientCreate: OSStatus %d", st)
		}
	})
	if clientErr != nil {
		return nil, clientErr
	}
	if d.Card >= int(C.MIDIGetNumberOfSources()) {
		return nil, fmt.Errorf("midi: %s is gone", d.Name)
	}
	src := C.MIDIGetSource(C.ItemCount(d.Card))
	p := &Port{Device: d, handle: handle, done: make(chan struct{})}
	portsMu.Lock()
	nextPort++
	id := nextPort
	ports[id] = p
	portsMu.Unlock()
	forget := func() {
		portsMu.Lock()
		delete(ports, id)
		portsMu.Unlock()
	}
	var in C.MIDIPortRef
	if st := C.afInPort(client, C.int(id), &in); st != 0 {
		forget()
		return nil, fmt.Errorf("midi: MIDIInputPortCreate: OSStatus %d", st)
	}
	if st := C.MIDIPortConnectSource(in, src, nil); st != 0 {
		C.MIDIPortDispose(in)
		forget()
		return nil, fmt.Errorf("midi: MIDIPortConnectSource: OSStatus %d", st)
	}
	var out C.MIDIPortRef
	if d.Dev >= 0 && d.Dev < int(C.MIDIGetNumberOfDestinations()) {
		dest := C.MIDIGetDestination(C.ItemCount(d.Dev))
		if C.afOutPort(client, &out) == 0 {
			p.write = func(b []byte) error {
				if len(b) == 0 {
					return nil
				}
				if st := C.afSend(out, dest, (*C.Byte)(unsafe.Pointer(&b[0])), C.int(len(b))); st != 0 {
					return fmt.Errorf("midi: MIDISend: OSStatus %d", st)
				}
				return nil
			}
		}
	}
	p.closer = func() error {
		C.MIDIPortDisconnectSource(in, src)
		C.MIDIPortDispose(in)
		if out != 0 {
			C.MIDIPortDispose(out)
		}
		forget()
		p.err = fmt.Errorf("midi: port closed")
		close(p.done)
		return nil
	}
	return p, nil
}
