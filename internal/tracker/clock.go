package tracker

import (
	"sync"
	"time"
)

// El reloj MIDI: el tracker va a 24 ticks por tiempo, lo mismo que el reloj
// MIDI (24 pulsos por negra), así que un pulso es un tick.
//
//   - Esclavo (SetExternalClock): los ticks los marca otro aparato. Cada pulso
//     que llega (0xF8) avanza un tick (ExternalTick); Start (0xFA) toca la
//     canción desde la fila 0, Continue (0xFB) sigue donde iba y Stop (0xFC)
//     para. El audio se sigue generando entre pulsos (las voces suenan), solo
//     que el secuenciador espera al pulso siguiente. El pulso se aplica al
//     siguiente bloque de audio que se genera, así que llega con el retraso
//     del búfer de salida (unos 50–100 ms en la interfaz), siempre el mismo.
//   - Maestro (ClockOut): una goroutine manda el reloj a otro aparato al
//     tempo de la canción (también los cambios de T) mientras se toca, con
//     Start al empezar y Stop al parar. Va en tiempo real, no en el hilo del
//     audio (que genera por adelantado): el otro aparato va por delante del
//     sonido lo que tarda el búfer de salida en oírse.

// extClock es el estado del reloj de fuera.
type extClock struct {
	on       bool
	last     time.Time     // el último pulso
	interval time.Duration // la media entre pulsos (0: aún no se sabe)
}

// tickDur es lo que dura un tick: con el reloj de fuera, lo que miden los
// pulsos; si no, lo del tempo.
func (p *Player) tickDur() float64 {
	if p.ext.on && p.ext.interval > 0 {
		return p.ext.interval.Seconds()
	}
	return 1 / p.tickRate()
}

// SetExternalClock pone o quita el reloj de fuera (modo esclavo).
func (p *Player) SetExternalClock(on bool) {
	p.mu.Lock()
	p.ext = extClock{on: on}
	p.tickAcc = 0
	p.mu.Unlock()
}

// ExternalClock dice si los ticks vienen de fuera.
func (p *Player) ExternalClock() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ext.on
}

// ExternalTick es un pulso de reloj (0xF8) que llegó en el momento at: mide
// el tempo y, tocando, avanza un tick. Sin el reloj de fuera no hace nada.
func (p *Player) ExternalTick(at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := &p.ext
	if !e.on {
		return
	}
	if !e.last.IsZero() {
		d := at.Sub(e.last)
		// Un hueco largo (el otro aparato estaba parado) no cuenta.
		if d > 0 && d < 500*time.Millisecond {
			if e.interval == 0 {
				e.interval = d
			} else {
				// Media móvil: el reloj por USB llega con algo de vaivén.
				e.interval += (d - e.interval) / 8
			}
		}
	}
	e.last = at
	if p.playing && p.song != nil {
		p.tick()
	}
}

// ExternalBPM es el tempo que marcan los pulsos de fuera (0 si aún no se
// sabe).
func (p *Player) ExternalBPM() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ext.interval <= 0 {
		return 0
	}
	return 60 / (p.ext.interval.Seconds() * TicksPerBeat)
}

// ExternalStart es Start (0xFA): la canción desde la fila 0.
func (p *Player) ExternalStart() {
	p.start(PlaySong, 0, 0, false)
	p.mu.Lock()
	p.tickAcc = 0 // el primer tick lo trae el primer pulso
	p.mu.Unlock()
}

// ExternalContinue es Continue (0xFB): sigue donde se paró (si no había nada
// que seguir, empieza la canción).
func (p *Player) ExternalContinue() {
	p.mu.Lock()
	resume := !p.playing && p.song != nil && p.chans[0].started
	if resume {
		p.playing = true
	}
	p.mu.Unlock()
	if !resume {
		p.ExternalStart()
	}
}

// ExternalStop es Stop (0xFC).
func (p *Player) ExternalStop() { p.Stop() }

// Tempo es el tempo con el que se toca ahora (el de la canción con sus T).
func (p *Player) Tempo() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bpm
}

// Los mensajes de tiempo real del reloj MIDI.
const (
	midiClock    = 0xF8
	midiStart    = 0xFA
	midiContinue = 0xFB
	midiStop     = 0xFC
)

// ClockOut manda el reloj MIDI por send (modo maestro) hasta que se llame a
// la función que devuelve: Start al empezar a tocar, un pulso cada 1/24 de
// tiempo al tempo de la canción y Stop al parar. Con el reloj de fuera no
// manda nada (el tempo es del otro). send se llama desde otra goroutine;
// cuando stop vuelve, ya no se llama más.
func (p *Player) ClockOut(send func(b byte)) (stop func()) {
	done, finished := make(chan struct{}), make(chan struct{})
	var once sync.Once
	go func() {
		defer close(finished)
		was := false
		var next time.Time
		for {
			p.mu.Lock()
			playing, ext, bpm := p.playing, p.ext.on, p.bpm
			p.mu.Unlock()
			now := time.Now()
			switch {
			case ext:
				was = false
			case playing && !was:
				send(midiStart)
				send(midiClock)
				was, next = true, now.Add(pulse(bpm))
			case !playing && was:
				send(midiStop)
				was = false
			case playing:
				// Los pulsos que tocan hasta ahora (si la goroutine se
				// retrasó, se ponen al día sin acumular un retraso).
				for !next.After(now) {
					send(midiClock)
					next = next.Add(pulse(bpm))
				}
			}
			wait := 2 * time.Millisecond
			if was {
				wait = min(wait, max(0, time.Until(next)))
			}
			select {
			case <-done:
				if was {
					send(midiStop)
				}
				return
			case <-time.After(wait):
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}

// pulse es lo que dura un pulso de reloj a bpm.
func pulse(bpm int) time.Duration {
	return time.Duration(float64(time.Minute) / float64(max(1, bpm)*TicksPerBeat))
}
