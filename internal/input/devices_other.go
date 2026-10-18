//go:build !linux

package input

import (
	"errors"
	"time"
)

// Fuera de Linux no hay evdev: el servicio no lee el mando ni los botones de
// los audífonos por su cuenta (en el escritorio no hace falta: la interfaz
// recibe el teclado y el mando por la ventana). Estas versiones vacías dejan
// compilar lo demás.

// errNoEvdev es el error de todo lo que necesita evdev.
var errNoEvdev = errors.New("input devices are only read on Linux")

// Resolved es el mando elegido y cómo se lee.
type Resolved struct {
	Name   string
	Path   string
	Layout Layout
	Source string
}

// Resolve no encuentra mando fuera de Linux.
func Resolve(path string) (Resolved, error) { return Resolved{}, errNoEvdev }

// Reader es el lector del mando (sin uso fuera de Linux).
type Reader struct{}

// Open falla fuera de Linux.
func Open(r Resolved) (*Reader, error) { return nil, errNoEvdev }

// Read falla fuera de Linux.
func (r *Reader) Read() (Event, error) { return Event{}, errNoEvdev }

// Close no hace nada.
func (r *Reader) Close() error { return nil }

// MediaDevices no encuentra dispositivos fuera de Linux.
func MediaDevices() []string { return nil }

// MediaReader es el lector de botones multimedia (sin uso fuera de Linux).
type MediaReader struct{}

// OpenMedia falla fuera de Linux.
func OpenMedia(path string) (*MediaReader, error) { return nil, errNoEvdev }

// Run no hace nada.
func (m *MediaReader) Run(fn func(key uint16, at time.Time)) {}

// Close no hace nada.
func (m *MediaReader) Close() error { return nil }

// WaitNewDevices espera a que done se cierre: no hay dispositivos que vigilar.
func WaitNewDevices(done <-chan struct{}, fn func()) { <-done }

// PowerButtons no encuentra botones fuera de Linux.
func PowerButtons() []string { return nil }

// PowerWatcher vigila el botón de encendido (sin uso fuera de Linux).
type PowerWatcher struct{}

// WatchPower falla fuera de Linux.
func WatchPower(path string) (*PowerWatcher, error) { return nil, errNoEvdev }

// Run no hace nada.
func (p *PowerWatcher) Run(fn func(held time.Duration)) {}

// Close no hace nada.
func (p *PowerWatcher) Close() error { return nil }
