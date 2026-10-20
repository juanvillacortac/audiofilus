//go:build !linux && !windows && !(ios && cgo)

package midi

// List no encuentra nada fuera de Linux.
func List() ([]Device, error) { return nil, ErrUnsupported }

// Open no abre nada fuera de Linux.
func Open(Device, func(Event)) (*Port, error) { return nil, ErrUnsupported }
