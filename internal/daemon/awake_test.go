package daemon

import (
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// awakePlat anota si se impide la suspensión y si los botones de volumen
// pasan por el servicio.
type awakePlat struct {
	platform.Platform
	awake, volumeKeys bool
}

func (p *awakePlat) SetAwake(on bool)      { p.awake = on }
func (p *awakePlat) SetVolumeKeys(on bool) { p.volumeKeys = on }
func (p *awakePlat) SystemVolume() int     { return 100 }

// En el modo juego (awake open) la app cuenta como abierta: los botones de
// volumen pasan por el servicio (y se ve el indicador), pero la consola se
// suspende como siempre. Al volver a la app normal se impide otra vez, y al
// cerrarla se suelta todo.
func TestAwakeOpen(t *testing.T) {
	p := &awakePlat{Platform: platform.Detect(platform.Options{})}
	d := &Daemon{plat: p}
	d.setAwake("on", time.Minute)
	if !p.awake || !p.volumeKeys {
		t.Fatalf("awake on: blocking %v, volume keys %v; want both", p.awake, p.volumeKeys)
	}
	d.setAwake("open", time.Minute)
	if p.awake || !p.volumeKeys {
		t.Errorf("awake open: blocking %v, volume keys %v; want only the keys", p.awake, p.volumeKeys)
	}
	d.setAwake("on", time.Minute)
	if !p.awake {
		t.Errorf("back to awake on: suspend not blocked")
	}
	d.setAwake("off", 0)
	if p.awake || p.volumeKeys {
		t.Errorf("awake off: blocking %v, volume keys %v; want neither", p.awake, p.volumeKeys)
	}
}
