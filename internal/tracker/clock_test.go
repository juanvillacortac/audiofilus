package tracker

import (
	"math"
	"testing"
	"time"
)

// Con el reloj de fuera, el secuenciador solo avanza con los pulsos: el
// audio que se genere entre medias no mueve la canción, y cada pulso es un
// tick (6 pulsos, un paso con el groove 6/6).
func TestExternalClockTicks(t *testing.T) {
	p := NewPlayer(rate)
	p.SetSong(song1(ModeGB))
	p.SetExternalClock(true)
	p.ExternalStart()
	run(p, rate) // un segundo de audio sin pulsos
	if h := p.Hits()[PU1]; h != 0 {
		t.Fatalf("hits without clock = %d, want 0", h)
	}
	at := time.Unix(0, 0)
	for i := 0; i < 6; i++ {
		p.ExternalTick(at)
		at = at.Add(20833 * time.Microsecond)
	}
	if _, _, step, _ := p.Position(PU1); step != 0 {
		t.Fatalf("after 6 pulses: step %d, want 0", step)
	}
	p.ExternalTick(at)
	if _, _, step, _ := p.Position(PU1); step != 1 {
		t.Fatalf("after 7 pulses: step %d, want 1", step)
	}
	if h := p.Hits()[PU1]; h != 1 {
		t.Fatalf("hits = %d, want 1", h)
	}
	p.ExternalStop()
	p.ExternalTick(at)
	if p.Playing() {
		t.Fatal("still playing after stop")
	}
	p.ExternalContinue()
	if _, _, step, playing := p.Position(PU1); !playing || step != 1 {
		t.Fatalf("continue: step %d playing %v, want 1 and playing", step, playing)
	}
}

// El tempo de fuera se mide de los pulsos: a 20,833 ms por pulso, 120 BPM.
func TestExternalBPM(t *testing.T) {
	p := NewPlayer(rate)
	p.SetSong(song1(ModeGB))
	p.SetExternalClock(true)
	at := time.Unix(0, 0)
	for i := 0; i < 48; i++ {
		p.ExternalTick(at)
		at = at.Add(time.Minute / (120 * TicksPerBeat))
	}
	if bpm := p.ExternalBPM(); math.Abs(bpm-120) > 0.5 {
		t.Fatalf("ExternalBPM = %v, want 120", bpm)
	}
	// Un hueco largo (el otro aparato estuvo parado) no cambia la medida.
	p.ExternalTick(at.Add(3 * time.Second))
	if bpm := p.ExternalBPM(); math.Abs(bpm-120) > 0.5 {
		t.Fatalf("after a gap ExternalBPM = %v, want 120", bpm)
	}
}

// Como maestro, manda Start, pulsos a 24 por tiempo al tempo de la canción y
// Stop al parar.
func TestClockOut(t *testing.T) {
	p := NewPlayer(rate)
	s := song1(ModeGB)
	s.BPM = 300 // 120 pulsos por segundo: la prueba es corta
	p.SetSong(s)
	got := make(chan byte, 1024)
	stop := p.ClockOut(func(b byte) { got <- b })
	p.Play(PlaySong, 0, 0)
	time.Sleep(500 * time.Millisecond)
	p.Stop()
	time.Sleep(50 * time.Millisecond)
	stop()
	close(got)
	var bytes []byte
	for b := range got {
		bytes = append(bytes, b)
	}
	if len(bytes) < 3 || bytes[0] != midiStart || bytes[len(bytes)-1] != midiStop {
		t.Fatalf("clock out = % x", bytes)
	}
	pulses := 0
	for _, b := range bytes {
		if b == midiClock {
			pulses++
		}
	}
	// Medio segundo a 120 pulsos por segundo: unos 60.
	if pulses < 45 || pulses > 75 {
		t.Fatalf("%d pulses in 0.5 s at 300 BPM, want about 60", pulses)
	}
}
