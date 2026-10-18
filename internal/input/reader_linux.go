package input

import (
	"encoding/binary"
	"io"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	evKey          = 0x01
	evAbs          = 0x03
	inputEventSize = 24 // struct input_event en Linux de 64 bits
)

// Reader lee el mando por evdev sin acapararlo (sin EVIOCGRAB): los juegos y
// el frontend siguen recibiendo los botones. Read bloquea, así que mientras no
// se pulse nada no gasta CPU.
type Reader struct {
	f       *os.File
	layout  Layout
	half    map[uint16]int32 // umbral de cada gatillo analógico (mitad del recorrido)
	pressed map[Button]bool
}

// Open abre el dispositivo resuelto.
func Open(r Resolved) (*Reader, error) {
	f, err := os.Open(r.Path)
	if err != nil {
		return nil, err
	}
	rd := &Reader{f: f, layout: r.Layout, half: map[uint16]int32{}, pressed: map[Button]bool{}}
	for code := range r.Layout.Axes {
		rd.half[code] = axisMidpoint(f, code)
	}
	return rd, nil
}

// absInfo es struct input_absinfo.
type absInfo struct{ value, min, max, fuzz, flat, resolution int32 }

// axisMidpoint pide el recorrido de un eje con EVIOCGABS.
func axisMidpoint(f *os.File, code uint16) int32 {
	var ai absInfo
	req := uintptr(0x80184540 + uint32(code)) // _IOR('E', 0x40+code, struct input_absinfo)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&ai))); errno != 0 || ai.max <= ai.min {
		return 128
	}
	return ai.min + (ai.max-ai.min)/2
}

// Read espera al siguiente cambio de un botón lógico. Los gatillos analógicos
// cuentan como pulsados al pasar de la mitad del recorrido.
func (r *Reader) Read() (Event, error) {
	var buf [inputEventSize]byte
	for {
		if _, err := io.ReadFull(r.f, buf[:]); err != nil {
			return Event{}, err
		}
		typ := binary.LittleEndian.Uint16(buf[16:])
		code := binary.LittleEndian.Uint16(buf[18:])
		value := int32(binary.LittleEndian.Uint32(buf[20:]))
		var b Button
		var down bool
		switch typ {
		case evKey:
			if value == 2 { // autorrepetición
				continue
			}
			b, down = r.layout.Keys[code], value == 1
		case evAbs:
			var ok bool
			if b, ok = r.layout.Axes[code]; ok {
				down = value > r.half[code]
			}
		default:
			continue
		}
		if b == None || r.pressed[b] == down {
			continue
		}
		r.pressed[b] = down
		sec := int64(binary.LittleEndian.Uint64(buf[0:]))
		usec := int64(binary.LittleEndian.Uint64(buf[8:]))
		return Event{Button: b, Pressed: down, Time: time.Unix(sec, usec*1000)}, nil
	}
}

// Close deja de leer; un Read en espera devuelve un error.
func (r *Reader) Close() error { return r.f.Close() }
