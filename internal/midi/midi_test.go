package midi

import (
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// parse pasa bytes por un Parser y devuelve los mensajes.
func parse(b ...byte) []Event {
	var p Parser
	var out []Event
	for _, c := range b {
		if e, ok := p.Feed(c); ok {
			out = append(out, e)
		}
	}
	return out
}

// Una nota con running status: la segunda nota y el note on con velocidad 0
// (que es un note off) llegan sin repetir el status.
func TestParserRunningStatus(t *testing.T) {
	ev := parse(0x91, 60, 100, 64, 90, 60, 0)
	if len(ev) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(ev), ev)
	}
	if ev[0].Kind != NoteOn || ev[0].Channel != 1 || ev[0].Note != 60 || ev[0].Velocity != 100 {
		t.Errorf("first = %+v", ev[0])
	}
	if ev[1].Kind != NoteOn || ev[1].Note != 64 {
		t.Errorf("second = %+v", ev[1])
	}
	if ev[2].Kind != NoteOff || ev[2].Note != 60 {
		t.Errorf("third = %+v, want note off", ev[2])
	}
}

// Los mensajes de tiempo real pueden caer en mitad de otro sin romperlo.
func TestParserRealtimeInterleaved(t *testing.T) {
	ev := parse(0x90, 0xF8, 60, 0xFA, 100, 0xF8)
	kinds := []Kind{Clock, Start, NoteOn, Clock}
	if len(ev) != len(kinds) {
		t.Fatalf("got %+v", ev)
	}
	for i, k := range kinds {
		if ev[i].Kind != k {
			t.Errorf("event %d = %v, want %v", i, ev[i].Kind, k)
		}
	}
	if ev[2].Note != 60 || ev[2].Velocity != 100 {
		t.Errorf("note = %+v", ev[2])
	}
}

// Un sysex se salta entero (con un reloj dentro, que sí cuenta) y después
// sigue el running status nuevo.
func TestParserSysex(t *testing.T) {
	ev := parse(0xF0, 0x7E, 0x10, 0xF8, 0x22, 0xF7, 0x80, 60, 0, 0xB0, 7, 100, 0xC2, 5, 0xE0, 0, 0x40)
	kinds := []Kind{Clock, NoteOff, Control, Program, PitchBend}
	if len(ev) != len(kinds) {
		t.Fatalf("got %+v", ev)
	}
	for i, k := range kinds {
		if ev[i].Kind != k {
			t.Errorf("event %d = %v, want %v", i, ev[i].Kind, k)
		}
	}
	if ev[3].Note != 5 || ev[3].Channel != 2 {
		t.Errorf("program = %+v", ev[3])
	}
	if ev[4].Value != 0 {
		t.Errorf("pitch bend center = %d, want 0", ev[4].Value)
	}
}

// Los datos sin status (al empezar a leer a mitad de un mensaje) se ignoran.
func TestParserNoStatus(t *testing.T) {
	if ev := parse(60, 100, 0xF8); len(ev) != 1 || ev[0].Kind != Clock {
		t.Fatalf("got %+v", ev)
	}
}

// La nota MIDI pasa a la numeración del tracker (do central, 60, es C-4 =
// 49) y la velocidad a volumen.
func TestTrackerNote(t *testing.T) {
	if n := (Event{Note: 60}).TrackerNote(); n != 49 {
		t.Errorf("MIDI 60 = %d, want 49", n)
	}
	if n := (Event{Note: 12}).TrackerNote(); n != 1 {
		t.Errorf("MIDI 12 = %d, want 1", n)
	}
	if n := (Event{Note: 5}).TrackerNote(); n != 0 {
		t.Errorf("MIDI 5 = %d, want 0", n)
	}
	if v := (Event{Kind: NoteOn, Velocity: 127}).Volume(); v != 15 {
		t.Errorf("velocity 127 = %d", v)
	}
	if v := (Event{Kind: NoteOn, Velocity: 1}).Volume(); v != 1 {
		t.Errorf("velocity 1 = %d, want 1", v)
	}
}

// pipeRW junta los dos extremos de dos pipes en un io.ReadWriteCloser.
type pipeRW struct {
	r *os.File
	w *os.File
}

func (p pipeRW) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRW) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRW) Close() error                { p.w.Close(); return p.r.Close() }

// Un puerto lee los mensajes de su dispositivo (aquí un pipe), escribe en él,
// y termina cuando el dispositivo se va.
func TestPortPipe(t *testing.T) {
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	var mu sync.Mutex
	var got []Event
	p := newPort(Device{Name: "fake"}, pipeRW{inR, outW}, true, func(e Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})
	inW.Write([]byte{0xFA, 0x90, 60, 100, 0xF8})
	if err := p.NoteOn(0, 64, 90); err != nil {
		t.Fatal(err)
	}
	p.Clock()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(outR, buf); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x90, 64, 90, 0xF8}; string(buf) != string(want) {
		t.Errorf("wrote % x, want % x", buf, want)
	}
	inW.Close() // desenchufado
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("port did not stop when the device went away")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || got[0].Kind != Start || got[1].Kind != NoteOn || got[2].Kind != Clock {
		t.Errorf("events = %+v", got)
	}
	if got[1].Time.IsZero() {
		t.Error("event without time")
	}
	p.Close()
}

// split parte bytes en mensajes completos, como los manda winmm.
func TestSplit(t *testing.T) {
	m := split([]byte{0xF8, 0x90, 60, 100, 0xC0, 3, 0xFA, 0x80, 60})
	if len(m) != 4 {
		t.Fatalf("got %d messages: % x", len(m), m)
	}
}
