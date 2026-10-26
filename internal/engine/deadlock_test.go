package engine

import (
	"io"
	"sync"
	"testing"
	"time"
)

// otoLike imita el candado del player de oto: Seek lo toma y, con él
// tomado, llama al Seek del engine; BufferedSize también lo toma.
type otoLike struct {
	mu  sync.Mutex
	src io.Seeker
}

func (p *otoLike) Play()             {}
func (p *otoLike) Pause()            {}
func (p *otoLike) Volume() float64   { return 1 }
func (p *otoLike) SetVolume(float64) {}
func (p *otoLike) BufferedSize() int { p.mu.Lock(); defer p.mu.Unlock(); return 0 }
func (p *otoLike) Seek(o int64, w int) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	time.Sleep(time.Millisecond) // que Status llegue en medio
	return p.src.Seek(o, w)
}

// Un Next mientras otro pide el estado no traba el servicio (pasaba en la
// consola: oto esperaba a e.mu dentro de Seek y Status, con e.mu, esperaba
// al candado de oto).
func TestNextWhileStatus(t *testing.T) {
	e := testEngine(48000, DSP{})
	e.src = &liveSource{}
	e.player = &otoLike{src: e}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				e.apply(func() {}) // lo que hace Next, sin abrir la salida
			}
		}()
		go func() {
			defer wg.Done()
			for range 500 {
				e.Status()
			}
		}()
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Next and Status deadlocked")
	}
}
