package daemon

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// lidPlat es la plataforma de prueba con una tapa que se abre y se cierra.
type lidPlat struct {
	platform.Platform
	closed atomic.Bool
}

func (p *lidPlat) LidClosed() bool { return p.closed.Load() }

// asleep lee d.sleeping con su candado.
func asleep(d *Daemon) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sleeping
}

// Un sleep que empieza con la tapa cerrada termina al abrirla; uno que
// empieza con la tapa abierta no depende de ella.
func TestWakeOnLidOpen(t *testing.T) {
	p := &lidPlat{Platform: platform.Detect(platform.Options{})}
	d := &Daemon{plat: p}
	p.closed.Store(true)
	if err := d.setSleep(true, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * lidEvery)
	if !asleep(d) {
		t.Fatal("woke up with the lid still closed")
	}
	p.closed.Store(false)
	time.Sleep(3 * lidEvery)
	if asleep(d) {
		t.Error("still asleep after opening the lid")
	}
	d.setSleep(true, true) // tapa abierta: no la vigila
	time.Sleep(2 * lidEvery)
	if !asleep(d) {
		t.Error("a sleep started with the lid open ended by itself")
	}
}
