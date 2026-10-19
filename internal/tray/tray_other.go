//go:build !linux

package tray

import (
	"errors"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Options es lo que la bandeja necesita del servicio.
type Options struct {
	Home   string
	UIOpen func() bool
	Quit   func()
	Lang   string
}

// Tray no existe fuera de Linux (aún).
type Tray struct{}

// Start no hace nada fuera de Linux.
func Start(eng *engine.Engine, opt Options) (*Tray, error) {
	return nil, errors.New("the system tray is only on Linux for now")
}

// Update no hace nada.
func (t *Tray) Update() {}

// Close no hace nada.
func (t *Tray) Close() {}
