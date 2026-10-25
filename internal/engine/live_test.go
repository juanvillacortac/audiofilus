package engine

import (
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// liveSource es una emisora de mentira: silencio sin duración.
type liveSource struct{ pos int64 }

func (s *liveSource) Read(dst []float32) (int, error) {
	for i := range dst {
		dst[i] = 0
	}
	s.pos += int64(len(dst) / 2)
	return len(dst), nil
}
func (s *liveSource) SampleRate() int       { return 48000 }
func (s *liveSource) Length() int64         { return 0 }
func (s *liveSource) Position() int64       { return s.pos }
func (s *liveSource) SeekFrame(int64) error { return nil }
func (s *liveSource) Close() error          { return nil }

var _ decode.Source = (*liveSource)(nil)

// Con emisoras, siguiente tras la última vuelve a la primera y anterior va
// a la de antes aunque lleve rato sonando (no la reinicia), y antes de la
// primera, a la última.
func TestLiveNextPrevWrap(t *testing.T) {
	e := testEngine(48000, DSP{})
	e.OpenRemote = func(path string) (decode.Source, bool, error) { return &liveSource{}, true, nil }
	stations := []string{"radio://a", "radio://b", "radio://c"}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.q.Set(stations, 2)
	e.openLocked()
	e.playing = true
	e.nextLocked()
	if got := e.q.Current(); got != "radio://a" {
		t.Fatalf("next after the last station: %s, want radio://a", got)
	}
	e.prevLocked(120) // dos minutos sonando
	if got := e.q.Current(); got != "radio://c" {
		t.Fatalf("prev before the first station: %s, want radio://c", got)
	}
	e.prevLocked(120)
	if got := e.q.Current(); got != "radio://b" {
		t.Fatalf("prev after two minutes on a station: %s, want radio://b", got)
	}
}

// Un salto pedido mientras una canción remota aún se baja no se pierde:
// cuando llega, empieza en ese segundo (lo que hace MPD con seekid, y
// reanudar una canción de un servidor).
func TestSeekWhileLoading(t *testing.T) {
	e := testEngine(48000, DSP{})
	ready := false
	src := &liveSource{}
	var sought int64 = -1
	seekSrc := &seekRecorder{liveSource: src, sought: &sought}
	e.OpenRemote = func(path string) (decode.Source, bool, error) {
		if !ready {
			return nil, false, nil
		}
		return seekSrc, true, nil
	}
	e.mu.Lock()
	e.q.Set([]string{"mpd://u/1"}, 0)
	e.openLocked()
	e.playing = true
	e.seekLocked(5)
	e.waitAt = time.Time{}
	e.mu.Unlock()
	ready = true
	e.Read(make([]byte, 1024*bytesPerFrame))
	if sought != 5*48000 {
		t.Errorf("sought to frame %d, want %d", sought, 5*48000)
	}
}

// seekRecorder apunta a qué cuadro se saltó.
type seekRecorder struct {
	*liveSource
	sought *int64
}

func (s *seekRecorder) SeekFrame(f int64) error { *s.sought = f; return nil }
