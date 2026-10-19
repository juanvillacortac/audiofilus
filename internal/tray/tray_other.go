//go:build !linux && !windows

package tray

import (
	"errors"
	"os/exec"
	"syscall"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Tray no existe fuera de Linux y Windows (aún).
type Tray struct{}

// Start no hace nada fuera de Linux y Windows.
func Start(eng *engine.Engine, opt Options) (*Tray, error) {
	return nil, errors.New("no system tray on this system yet")
}

// Update no hace nada.
func (t *Tray) Update() {}

// Close no hace nada.
func (t *Tray) Close() {}

// detach separa la interfaz del servicio: en su propia sesión.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
