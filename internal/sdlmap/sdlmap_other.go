//go:build !linux

// Package sdlmap le pregunta a SDL2 cómo está mapeado el mando. Solo se usa en
// Linux, donde el servicio lee el mando por evdev; en otros sistemas la
// interfaz recibe el mando por la ventana.
package sdlmap

import "errors"

// Controller es un mando que SDL reconoce como gamepad.
type Controller struct {
	Name    string
	Path    string
	Mapping string
}

// Controllers no encuentra mandos fuera de Linux.
func Controllers() ([]Controller, error) {
	return nil, errors.New("sdlmap is only used on Linux")
}
