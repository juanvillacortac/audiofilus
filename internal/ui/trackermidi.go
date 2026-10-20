package ui

import (
	"log"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/midi"
	"github.com/juanvillacortac/audiofilus/internal/tracker"
)

// MIDI en el tracker: mientras se está en él, se vigilan los aparatos MIDI
// (un teclado por USB enchufado en caliente aparece solo) y se conecta el
// primero que haya, o el que se elija en Proyecto. Las notas que llegan
// suenan con el último instrumento y, en la frase, se escriben en el paso del
// cursor (que baja uno). La sincronía elige quién lleva el tiempo: el propio
// tracker, el reloj MIDI que llega (24 pulsos por tiempo, uno por tick) o el
// tracker mandando su reloj a los demás.

// Los modos de sincronía.
const (
	syncInternal = iota // el tempo de la canción
	syncExternal        // el reloj MIDI que llega
	syncSend            // el tempo de la canción, y se manda el reloj
	syncModes
)

// midiState es el MIDI del tracker.
type midiState struct {
	mu    sync.Mutex
	devs  []midi.Device // los que hay (los pone Watch, desde su goroutine)
	fresh bool          // cambiaron desde el último fotograma

	stopWatch func()
	port      *midi.Port
	open      string // la ruta del aparato abierto
	choice    string // la ruta elegida ("" el primero que haya, "-" ninguno)
	sync      int
	stopClock func()
	notes     chan midi.Event // las notas, de la goroutine que lee a la interfaz
}

// midiWatch empieza a vigilar los aparatos (al entrar en el tracker).
func (a *App) midiWatch() {
	m := &a.tracker.midi
	if m.stopWatch != nil || a.offline {
		return
	}
	if m.notes == nil {
		m.notes = make(chan midi.Event, 64)
	}
	m.stopWatch = midi.Watch(2*time.Second, func(devs []midi.Device) {
		m.mu.Lock()
		m.devs, m.fresh = devs, true
		m.mu.Unlock()
		ebiten.ScheduleFrame()
	})
}

// midiStop deja de vigilar y cierra el aparato (al salir del tracker).
func (a *App) midiStop() {
	m := &a.tracker.midi
	if m.stopWatch != nil {
		m.stopWatch()
		m.stopWatch = nil
	}
	a.midiClose()
}

// midiClose cierra el aparato abierto.
func (a *App) midiClose() {
	m := &a.tracker.midi
	if m.stopClock != nil {
		m.stopClock()
		m.stopClock = nil
	}
	if m.port != nil {
		m.port.Close()
		m.port = nil
	}
	m.open = ""
	a.tracker.player.SetExternalClock(false)
}

// midiDevices son los aparatos que hay ahora.
func (a *App) midiDevices() []midi.Device {
	m := &a.tracker.midi
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.devs
}

// pollMIDI atiende el MIDI en cada fotograma: abre el aparato que toca si
// cambiaron los que hay o la elección, y atiende las notas que llegaron.
func (a *App) pollMIDI() {
	t := a.tracker
	m := &t.midi
	m.mu.Lock()
	fresh := m.fresh
	m.fresh = false
	devs := m.devs
	m.mu.Unlock()
	if fresh {
		a.midiConnect(devs)
	}
	if m.port != nil {
		select {
		case <-m.port.Done(): // lo desenchufaron
			log.Printf("midi: %s closed", m.open)
			a.midiClose()
		default:
		}
	}
	for {
		select {
		case e := <-m.notes:
			a.midiNote(e)
		default:
			a.midiClockOut()
			return
		}
	}
}

// midiConnect abre el aparato elegido (o el primero) si no es el que está
// abierto.
func (a *App) midiConnect(devs []midi.Device) {
	t := a.tracker
	m := &t.midi
	want := ""
	for _, d := range devs {
		if m.choice == "" || d.Path == m.choice {
			want = d.Path
			break
		}
	}
	if m.choice == "-" {
		want = ""
	}
	if want == m.open && (want == "" || m.port != nil) {
		return
	}
	a.midiClose()
	if want == "" {
		return
	}
	for _, d := range devs {
		if d.Path != want {
			continue
		}
		p := t.player
		notes := m.notes
		port, err := midi.Open(d, func(e midi.Event) {
			// En la goroutine que lee: el reloj va directo al reproductor
			// (es seguro entre hilos); las notas, a la interfaz.
			switch e.Kind {
			case midi.Clock:
				if p.ExternalClock() {
					p.ExternalTick(e.Time)
				}
			case midi.Start:
				if p.ExternalClock() {
					p.ExternalStart()
				}
			case midi.Continue:
				if p.ExternalClock() {
					p.ExternalContinue()
				}
			case midi.Stop:
				if p.ExternalClock() {
					p.ExternalStop()
				}
			case midi.NoteOn:
				select {
				case notes <- e:
					ebiten.ScheduleFrame()
				default:
				}
			}
		})
		if err != nil {
			log.Printf("midi: %v", err)
			a.flash(T("tk_midi_failed", err))
			return
		}
		m.port, m.open = port, d.Path
		p.SetExternalClock(m.sync == syncExternal)
		a.flash(T("tk_midi_on", midiName(d)))
	}
}

// midiName es el nombre de un aparato para la interfaz.
func midiName(d midi.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Path
}

// midiNote atiende una nota que llegó: suena en el canal en que se trabaja
// con el último instrumento y, en la frase (con la canción parada o no), se
// escribe en el paso del cursor, que baja uno.
func (a *App) midiNote(e midi.Event) {
	t := a.tracker
	n := e.TrackerNote()
	if n < 1 || n > tracker.NoteMax {
		return
	}
	if t.view == tvPhrase {
		a.tkUndoPoint()
		t.editing = false // cada nota se deshace sola
		p := a.phraseGet()
		st := &p[t.phRow]
		st.Note = n
		if st.Instr == tracker.Empty {
			st.Instr = int16(t.lastInstr)
		}
		if v := e.Volume(); v < 15 {
			st.Vol = v
		}
		t.lastNote = n
		a.trackerChanged()
		t.phRow = (t.phRow + 1) % tracker.Steps
		t.rollNote = int(n)
	}
	a.tkPreview(n, tracker.Empty)
}

// midiClockOut manda el reloj mientras el tracker toca, si así se eligió.
func (a *App) midiClockOut() {
	t := a.tracker
	m := &t.midi
	want := t.playing && m.sync == syncSend && m.port != nil
	switch {
	case want && m.stopClock == nil:
		port := m.port
		m.stopClock = t.player.ClockOut(func(b byte) { _ = port.Write(b) })
	case !want && m.stopClock != nil:
		m.stopClock()
		m.stopClock = nil
	}
}

// midiParams son las filas MIDI de la vista de proyecto: el aparato y la
// sincronía.
func (a *App) midiParams() []tkParam {
	t := a.tracker
	m := &t.midi
	devs := a.midiDevices()
	// Las opciones: ninguno y cada aparato.
	idx, show := 0, T("tk_midi_none")
	for i, d := range devs {
		if d.Path == m.open || (m.open == "" && d.Path == m.choice) {
			idx, show = i+1, midiName(d)
		}
	}
	if len(devs) == 0 {
		show = T("tk_midi_nodev")
	}
	syncNames := [syncModes]string{T("tk_sync_int"), T("tk_sync_ext"), T("tk_sync_send")}
	return []tkParam{
		{label: "MIDI", show: show, field: tkField{key: "midi", val: idx, emptyVal: -1000, max: len(devs), big: 1,
			set: func(v int) {
				if v == 0 {
					m.choice = "-"
				} else {
					m.choice = devs[v-1].Path
				}
				a.midiConnect(devs)
			}}},
		{label: T("tk_p_sync"), show: syncNames[m.sync], field: tkField{key: "sync", val: m.sync, emptyVal: -1000, max: syncModes - 1, big: 1,
			set: func(v int) {
				m.sync = v
				t.player.SetExternalClock(v == syncExternal && m.port != nil)
			}}},
	}
}
