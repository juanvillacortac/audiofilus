package input

import "time"

// Action es lo que pide un gesto o un atajo.
type Action int

const (
	NoAction    Action = iota
	Prev               // canción anterior
	Next               // canción siguiente
	SeekBack           // retroceder
	SeekForward        // adelantar
	TogglePause        // pausar o reanudar
	Wake               // salir del sleep
	Armed              // el atajo de segundo plano está listo para recibir un botón
	Random             // una canción al azar de toda la biblioteca
)

func (a Action) String() string {
	return [...]string{"none", "prev", "next", "seek-back", "seek-forward", "toggle-pause", "wake", "armed", "random"}[a]
}

// Detector reconoce los gestos del sleep de la app: doble toque de L1/R1/L2/R2
// y dobles acordes: L1+L2 pausa y R1+R2 pone una canción al azar. Un toque o
// un acorde sueltos no hacen nada, para que el bolsillo no cambie la música.
// Start despierta.
type Detector struct {
	Tap   time.Duration // separación máxima entre los dos toques
	Chord time.Duration // separación máxima entre los dos acordes

	held    map[Button]bool
	last    map[Button]time.Time
	chordAt map[Action]time.Time // el primer acorde de cada doble acorde
}

// chords son los acordes de dos gatillos y lo que hace su doble acorde.
var chords = []struct {
	a, b   Button
	action Action
}{{L1, L2, TogglePause}, {R1, R2, Random}}

// NewDetector devuelve un detector con esas ventanas de tiempo.
func NewDetector(tap, chord time.Duration) *Detector {
	return &Detector{Tap: tap, Chord: chord, held: map[Button]bool{}, last: map[Button]time.Time{}, chordAt: map[Action]time.Time{}}
}

// doubleTap es la acción del doble toque de cada gatillo.
var doubleTap = map[Button]Action{L1: Prev, R1: Next, L2: SeekBack, R2: SeekForward}

// Event recibe un cambio de botón y devuelve la acción reconocida, si la hay.
func (d *Detector) Event(e Event) Action {
	if !e.Pressed {
		d.held[e.Button] = false
		return NoAction
	}
	d.held[e.Button] = true
	if e.Button == Start {
		return Wake
	}
	for _, c := range chords {
		if (e.Button != c.a || !d.held[c.b]) && (e.Button != c.b || !d.held[c.a]) {
			continue
		}
		// Un acorde nunca cuenta como toques de sus botones.
		clear(d.last)
		if at, ok := d.chordAt[c.action]; ok && e.Time.Sub(at) <= d.Chord {
			delete(d.chordAt, c.action)
			return c.action
		}
		d.chordAt[c.action] = e.Time
		return NoAction
	}
	action, ok := doubleTap[e.Button]
	if !ok {
		return NoAction
	}
	if prev, seen := d.last[e.Button]; seen && e.Time.Sub(prev) <= d.Tap {
		delete(d.last, e.Button)
		return action
	}
	d.last[e.Button] = e.Time
	return NoAction
}

// Combo reconoce el atajo de segundo plano: FN pulsado Taps veces seguidas
// (cada toque a menos de Tap del anterior) y, dentro de Window, un botón:
// L1 anterior, R1 siguiente, L2 retroceder, R2 adelantar, Select pausar. Tres
// toques de FN no los usa ningún juego ni Knulli, así que el atajo no choca con
// lo que esté abierto.
type Combo struct {
	Taps   int
	Tap    time.Duration
	Window time.Duration

	count   int
	lastFN  time.Time
	armedAt time.Time
}

// NewCombo devuelve un detector de atajos.
func NewCombo(taps int, tap, window time.Duration) *Combo {
	return &Combo{Taps: taps, Tap: tap, Window: window}
}

// comboActions es la acción de cada botón una vez armado el atajo.
var comboActions = map[Button]Action{L1: Prev, R1: Next, L2: SeekBack, R2: SeekForward, Select: TogglePause}

// Event recibe un cambio de botón. Devuelve Armed al completar los toques de FN
// y la acción del botón que llega después.
func (c *Combo) Event(e Event) Action {
	if !e.Pressed {
		return NoAction
	}
	if !c.armedAt.IsZero() {
		armed := e.Time.Sub(c.armedAt) <= c.Window
		c.armedAt = time.Time{}
		if armed {
			if a, ok := comboActions[e.Button]; ok {
				c.count = 0
				return a
			}
		}
	}
	if e.Button != FN {
		c.count = 0
		return NoAction
	}
	if c.count > 0 && e.Time.Sub(c.lastFN) > c.Tap {
		c.count = 0
	}
	c.count++
	c.lastFN = e.Time
	if c.count >= c.Taps {
		c.count = 0
		c.armedAt = e.Time
		return Armed
	}
	return NoAction
}
