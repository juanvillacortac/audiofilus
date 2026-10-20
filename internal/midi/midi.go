// Package midi habla MIDI por los puertos crudos de ALSA (/dev/snd/midiC*D*),
// sin cgo: así se ven los teclados y aparatos MIDI por USB tanto en las
// portátiles (Knulli, muOS, ROCKNIX, ArkOS) como en el escritorio Linux. En
// Windows usa winmm.dll y en iOS CoreMIDI; en los demás sistemas no hay
// dispositivos (List devuelve ErrUnsupported).
//
// Uso desde la interfaz:
//
//	devs, _ := midi.List()                       // o midi.Watch para enchufar en caliente
//	port, err := midi.Open(devs[0], func(e midi.Event) {
//		// En la goroutine que lee: hay que volver rápido.
//		switch e.Kind {
//		case midi.Clock:
//			player.ExternalTick(e.Time)
//		case midi.Start:
//			player.ExternalStart()
//		case midi.Continue:
//			player.ExternalContinue()
//		case midi.Stop:
//			player.ExternalStop()
//		case midi.NoteOn:
//			notes <- e // la interfaz la oye o la escribe en la frase (e.TrackerNote(), e.Volume())
//		}
//	})
//	stop := player.ClockOut(func(b byte) { _ = port.Write(b) }) // si el tracker es el maestro
//	...
//	stop(); port.Close()
//
// En Knulli la interfaz corre como root y abre el dispositivo sin más. En
// otros Linux el usuario tiene que estar en el grupo audio (los /dev/snd/*
// son root:audio 0660). En Windows se usan los puertos de winmm.dll (sin cgo)
// y en iOS los de CoreMIDI (con cgo, como el resto de lo de iOS).
package midi

import (
	"errors"
	"io"
	"sync"
	"time"
)

// ErrUnsupported es lo que devuelven List y Open donde no hay MIDI crudo.
var ErrUnsupported = errors.New("midi: not supported on this system")

// Device es un puerto MIDI.
type Device struct {
	Path string // /dev/snd/midiC1D0 (en Windows "winmm:<n>", en iOS "coremidi:<n>")
	Name string // el nombre del aparato, si se sabe
	// En Linux, la tarjeta y el puerto de ALSA. En Windows e iOS, la entrada
	// y la salida del aparato (Dev -1 si no tiene salida con el mismo nombre).
	Card int
	Dev  int
}

// Kind es el tipo de un mensaje.
type Kind int

const (
	Other Kind = iota
	NoteOn
	NoteOff
	Control   // control change: Note es el control, Velocity el valor
	Program   // program change: Note es el programa
	PitchBend // Value, de -8192 a 8191
	Clock     // 0xF8: un pulso (24 por negra)
	Start     // 0xFA
	Continue  // 0xFB
	Stop      // 0xFC
)

// Event es un mensaje MIDI que llegó.
type Event struct {
	Kind     Kind
	Channel  int // 0..15 (en los de canal)
	Note     int // nota (o control, o programa)
	Velocity int // velocidad (o valor del control)
	Value    int // pitch bend
	Time     time.Time
}

// TrackerNote es la nota en la numeración del tracker (1 = C-0; el do
// central, MIDI 60, es C-4 = 49): MIDI 12 es C-0. Por debajo, 0.
func (e Event) TrackerNote() int8 {
	n := e.Note - 11
	if n < 1 || n > 127 {
		return 0
	}
	return int8(n)
}

// Volume es la velocidad como volumen del tracker (0..15; una nota que suena
// tiene al menos 1).
func (e Event) Volume() int8 {
	v := (e.Velocity*15 + 63) / 127
	if e.Kind == NoteOn && v < 1 {
		v = 1
	}
	return int8(min(15, v))
}

// Parser lee un flujo de bytes MIDI: con running status, los mensajes de
// tiempo real metidos en cualquier parte (incluso a mitad de otro mensaje) y
// los sysex saltados.
type Parser struct {
	status byte
	data   [2]byte
	n      int
	sysex  bool
}

// dataLen es cuántos bytes de datos lleva un mensaje de ese status.
func dataLen(status byte) int {
	switch {
	case status >= 0x80 && status < 0xC0, status >= 0xE0 && status < 0xF0:
		return 2
	case status >= 0xC0 && status < 0xE0:
		return 1
	case status == 0xF1, status == 0xF3:
		return 1
	case status == 0xF2:
		return 2
	}
	return 0
}

// Feed le pasa un byte; ok dice si con él se completó un mensaje.
func (p *Parser) Feed(b byte) (e Event, ok bool) {
	switch {
	case b >= 0xF8: // tiempo real: no toca el running status
		switch b {
		case 0xF8:
			return Event{Kind: Clock}, true
		case 0xFA:
			return Event{Kind: Start}, true
		case 0xFB:
			return Event{Kind: Continue}, true
		case 0xFC:
			return Event{Kind: Stop}, true
		}
		return Event{}, false
	case b == 0xF0:
		p.sysex, p.status, p.n = true, 0, 0
		return Event{}, false
	case b == 0xF7:
		p.sysex = false
		return Event{}, false
	case b >= 0x80:
		p.sysex, p.status, p.n = false, b, 0
		if b >= 0xF0 && dataLen(b) == 0 {
			p.status = 0 // F6 y los indefinidos: sin datos ni running status
		}
		return Event{}, false
	}
	if p.sysex || p.status == 0 {
		return Event{}, false
	}
	p.data[p.n] = b
	p.n++
	if p.n < dataLen(p.status) {
		return Event{}, false
	}
	p.n = 0
	st := p.status
	if st >= 0xF0 {
		p.status = 0 // los mensajes de sistema no dejan running status
		return Event{}, false
	}
	e = Event{Channel: int(st & 0x0F), Note: int(p.data[0]), Velocity: int(p.data[1])}
	switch st & 0xF0 {
	case 0x90:
		e.Kind = NoteOn
		if e.Velocity == 0 {
			e.Kind = NoteOff
		}
	case 0x80:
		e.Kind = NoteOff
	case 0xB0:
		e.Kind = Control
	case 0xC0:
		e.Kind, e.Velocity = Program, 0
	case 0xE0:
		e.Kind = PitchBend
		e.Value = int(p.data[0]) | int(p.data[1])<<7 - 8192
		e.Note, e.Velocity = 0, 0
	default:
		e.Kind = Other
	}
	return e, true
}

// Port es un puerto abierto: sus mensajes llegan a handle (desde la
// goroutine que lee, o desde el hilo del sistema en Windows e iOS) y se
// escribe cuando se pida.
type Port struct {
	Device Device
	write  func([]byte) error // nil: solo lectura
	closer func() error
	handle func(Event)
	parser Parser
	wmu    sync.Mutex
	done   chan struct{}
	err    error
	once   sync.Once
}

// newPort empieza a leer de rw (los puertos crudos de ALSA, o un pipe en los
// tests) y a pasar cada mensaje a handle.
func newPort(d Device, rw io.ReadWriteCloser, canWrite bool, handle func(Event)) *Port {
	p := &Port{Device: d, closer: rw.Close, handle: handle, done: make(chan struct{})}
	if canWrite {
		p.write = func(b []byte) error { _, err := rw.Write(b); return err }
	}
	go p.read(rw)
	return p
}

func (p *Port) read(r io.Reader) {
	defer close(p.done)
	buf := make([]byte, 256)
	for {
		n, err := r.Read(buf)
		p.feed(buf[:n], time.Now())
		if err != nil {
			p.err = err
			return
		}
	}
}

// feed le pasa bytes que llegaron al parser y los mensajes completos a
// handle. Se llama desde un solo hilo a la vez por puerto.
func (p *Port) feed(b []byte, now time.Time) {
	for _, c := range b {
		if e, ok := p.parser.Feed(c); ok && p.handle != nil {
			e.Time = now
			p.handle(e)
		}
	}
}

// Done se cierra cuando el puerto deja de leer (se cerró o se desenchufó).
func (p *Port) Done() <-chan struct{} { return p.done }

// Err es por qué dejó de leer (después de Done).
func (p *Port) Err() error { return p.err }

// Close cierra el puerto.
func (p *Port) Close() error {
	var err error
	p.once.Do(func() { err = p.closer() })
	return err
}

// Write manda bytes MIDI tal cual (uno o varios mensajes completos).
func (p *Port) Write(msg ...byte) error {
	if p.write == nil {
		return errors.New("midi: port opened read-only")
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	return p.write(msg)
}

// split parte bytes MIDI en mensajes completos (sin running status), para
// los sistemas que mandan de uno en uno.
func split(b []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(b); {
		st := b[i]
		n := 1
		if st < 0xF8 {
			n += dataLen(st)
		}
		if st < 0x80 || i+n > len(b) {
			break // un mensaje cortado o sin status: lo demás no se manda
		}
		out = append(out, b[i:i+n])
		i += n
	}
	return out
}

// Clock, Start, Continue y Stop mandan los mensajes de reloj.
func (p *Port) Clock() error    { return p.Write(0xF8) }
func (p *Port) Start() error    { return p.Write(0xFA) }
func (p *Port) Continue() error { return p.Write(0xFB) }
func (p *Port) Stop() error     { return p.Write(0xFC) }

// NoteOn manda una nota (canal 0..15, nota y velocidad 0..127).
func (p *Port) NoteOn(ch, note, vel int) error {
	return p.Write(byte(0x90|ch&0x0F), byte(note&0x7F), byte(vel&0x7F))
}

// NoteOff suelta una nota.
func (p *Port) NoteOff(ch, note int) error {
	return p.Write(byte(0x80|ch&0x0F), byte(note&0x7F), 0)
}

// Watch mira los puertos cada interval y llama a changed (desde su
// goroutine) con la lista cada vez que cambia, también la primera: así un
// teclado enchufado con la app abierta aparece solo. Termina al llamar a la
// función que devuelve (cuando vuelve, changed ya no se llama más).
func Watch(interval time.Duration, changed func([]Device)) (stop func()) {
	done, finished := make(chan struct{}), make(chan struct{})
	var once sync.Once
	go func() {
		defer close(finished)
		var last []Device
		first := true
		for {
			devs, _ := List()
			if first || !same(devs, last) {
				first, last = false, devs
				changed(devs)
			}
			select {
			case <-done:
				return
			case <-time.After(interval):
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}

// same dice si dos listas tienen los mismos puertos.
func same(a, b []Device) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}
