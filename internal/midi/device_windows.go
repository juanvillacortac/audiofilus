package midi

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// En Windows, MIDI va por winmm.dll (sin cgo): las entradas y las salidas
// son listas aparte, y un aparato con las dos aparece en ambas con el mismo
// nombre. Device.Card es la entrada y Device.Dev la salida con su nombre (-1
// si no tiene). Los mensajes llegan por una función de vuelta (callback) en
// un hilo de winmm, de uno en uno y completos.

var (
	winmm              = windows.NewLazySystemDLL("winmm.dll")
	midiInGetNumDevs   = winmm.NewProc("midiInGetNumDevs")
	midiInGetDevCapsW  = winmm.NewProc("midiInGetDevCapsW")
	midiInOpen         = winmm.NewProc("midiInOpen")
	midiInStart        = winmm.NewProc("midiInStart")
	midiInStop         = winmm.NewProc("midiInStop")
	midiInReset        = winmm.NewProc("midiInReset")
	midiInClose        = winmm.NewProc("midiInClose")
	midiOutGetNumDevs  = winmm.NewProc("midiOutGetNumDevs")
	midiOutGetDevCapsW = winmm.NewProc("midiOutGetDevCapsW")
	midiOutOpen        = winmm.NewProc("midiOutOpen")
	midiOutShortMsg    = winmm.NewProc("midiOutShortMsg")
	midiOutReset       = winmm.NewProc("midiOutReset")
	midiOutClose       = winmm.NewProc("midiOutClose")
)

const (
	callbackFunction = 0x30000 // CALLBACK_FUNCTION
	mimData          = 0x3C3   // MIM_DATA: un mensaje corto
)

// midiInCaps es MIDIINCAPSW.
type midiInCaps struct {
	Mid, Pid      uint16
	DriverVersion uint32
	Pname         [32]uint16
	Support       uint32
}

// midiOutCaps es MIDIOUTCAPSW.
type midiOutCaps struct {
	Mid, Pid                               uint16
	DriverVersion                          uint32
	Pname                                  [32]uint16
	Technology, Voices, Notes, ChannelMask uint16
	Support                                uint32
}

// mmErr pasa un MMRESULT a error.
func mmErr(what string, r uintptr) error {
	if r == 0 {
		return nil
	}
	return fmt.Errorf("midi: %s: MMRESULT %d", what, r)
}

// outNames son los nombres de las salidas.
func outNames() []string {
	n, _, _ := midiOutGetNumDevs.Call()
	names := make([]string, n)
	for i := range names {
		var c midiOutCaps
		if r, _, _ := midiOutGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c)); r == 0 {
			names[i] = windows.UTF16ToString(c.Pname[:])
		}
	}
	return names
}

// List son las entradas MIDI, cada una con la salida del mismo nombre.
func List() ([]Device, error) {
	if err := winmm.Load(); err != nil {
		return nil, ErrUnsupported
	}
	outs := outNames()
	n, _, _ := midiInGetNumDevs.Call()
	var devs []Device
	for i := 0; i < int(n); i++ {
		var c midiInCaps
		if r, _, _ := midiInGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c)); r != 0 {
			continue
		}
		name := windows.UTF16ToString(c.Pname[:])
		out := -1
		for j, o := range outs {
			if o == name {
				out = j
				break
			}
		}
		devs = append(devs, Device{Path: fmt.Sprintf("winmm:%d", i), Name: name, Card: i, Dev: out})
	}
	return devs, nil
}

// Los puertos abiertos, por su número: la función de vuelta de winmm es una
// sola (syscall.NewCallback tiene un límite) y encuentra su puerto por el
// dwInstance.
var (
	portsMu  sync.Mutex
	ports    = map[uintptr]*Port{}
	nextPort uintptr
	inProc   = syscall.NewCallback(midiInProc)
)

// midiInProc es la función de vuelta de midiInOpen: con MIM_DATA, dwParam1
// trae el mensaje empaquetado (status | dato1<<8 | dato2<<16).
func midiInProc(h, msg, instance, p1, p2 uintptr) uintptr {
	if msg != mimData {
		return 0
	}
	portsMu.Lock()
	p := ports[instance]
	portsMu.Unlock()
	if p == nil {
		return 0
	}
	st := byte(p1)
	b := []byte{st}
	if st < 0xF8 {
		for i := 0; i < dataLen(st); i++ {
			b = append(b, byte(p1>>(8*(i+1))))
		}
	}
	p.feed(b, time.Now())
	return 0
}

// Open abre la entrada del aparato (y su salida, si tiene) y empieza a pasar
// sus mensajes a handle, desde el hilo de winmm: handle tiene que volver
// rápido.
func Open(d Device, handle func(Event)) (*Port, error) {
	if err := winmm.Load(); err != nil {
		return nil, ErrUnsupported
	}
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
	var in uintptr
	r, _, _ := midiInOpen.Call(uintptr(unsafe.Pointer(&in)), uintptr(d.Card), inProc, id, callbackFunction)
	if err := mmErr("midiInOpen", r); err != nil {
		forget()
		return nil, err
	}
	if r, _, _ := midiInStart.Call(in); r != 0 {
		midiInClose.Call(in)
		forget()
		return nil, mmErr("midiInStart", r)
	}
	var out uintptr
	if d.Dev >= 0 {
		if r, _, _ := midiOutOpen.Call(uintptr(unsafe.Pointer(&out)), uintptr(d.Dev), 0, 0, 0); r != 0 {
			out = 0 // sin salida: el puerto queda de solo lectura
		}
	}
	if out != 0 {
		p.write = func(b []byte) error {
			for _, m := range split(b) {
				var packed uintptr
				for i, c := range m {
					packed |= uintptr(c) << (8 * i)
				}
				if r, _, _ := midiOutShortMsg.Call(out, packed); r != 0 {
					return mmErr("midiOutShortMsg", r)
				}
			}
			return nil
		}
	}
	p.closer = func() error {
		midiInStop.Call(in)
		midiInReset.Call(in)
		r, _, _ := midiInClose.Call(in)
		if out != 0 {
			midiOutReset.Call(out)
			midiOutClose.Call(out)
		}
		forget()
		p.err = errors.New("midi: port closed") // en Windows el puerto termina al cerrarlo
		close(p.done)
		if r != 0 {
			return mmErr("midiInClose", r)
		}
		return nil
	}
	return p, nil
}
