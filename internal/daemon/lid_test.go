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

// rumblePlat anota las vibraciones que se piden.
type rumblePlat struct {
	platform.Platform
	got []time.Duration
}

func (p *rumblePlat) Rumble(d time.Duration) { p.got = append(p.got, d) }

// La fuerza de la vibración de Ajustes alarga, acorta o apaga cada una.
func TestRumbleScale(t *testing.T) {
	p := &rumblePlat{Platform: platform.Detect(platform.Options{})}
	d := &Daemon{plat: p}
	for _, v := range []string{"low", "", "high", "off"} {
		d.cfg.UI.Vibration = v
		d.rumble(100 * time.Millisecond)
	}
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 160 * time.Millisecond}
	if len(p.got) != len(want) {
		t.Fatalf("rumbles %v, want %v (none when off)", p.got, want)
	}
	for i := range want {
		if p.got[i] != want[i] {
			t.Errorf("rumble %d = %v, want %v", i, p.got[i], want[i])
		}
	}
}
