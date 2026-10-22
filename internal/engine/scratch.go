package engine

import (
	"io"
	"log"
	"math"
)

// El scratch: la mano en el disco. El engine guarda lo último que generó
// (histSeconds) y, al pedir un scratch, vacía el búfer de la salida y toca ese
// trozo de la canción con la velocidad de una mano que echa el disco atrás y
// lo suelta hacia delante (un "baby scratch"), muy rápido o casi parado. Con
// la velocidad cambia el tono, como en un plato de verdad; casi parado no
// suena, y despacio suena apagado (como la aguja en el surco). Al terminar
// sigue la canción justo donde se oía: lo que se había generado y quedó sin
// sonar al vaciar el búfer suena primero, desde la historia.

// histSeconds es cuánto se guarda de lo que sonó.
const histSeconds = 3

// scratchMove es un vaivén de la mano: echa el disco atrás amp segundos de
// música y lo devuelve, en dur segundos.
type scratchMove struct{ dur, amp float64 }

// scratchMoves son los movimientos de cada scratch: hacia arriba, uno largo;
// hacia abajo, dos cortos seguidos (un "chirp").
var scratchMoves = map[int][]scratchMove{
	1:  {{0.30, 0.22}},
	-1: {{0.15, 0.10}, {0.15, 0.10}},
}

// scratch es un scratch en curso.
type scratch struct {
	moves []scratchMove
	start float64 // el punto de la historia (en cuadros) donde está la aguja al empezar
	i     int     // cuántos cuadros lleva el movimiento actual
	tone  [2]float64
	// replay es por dónde va la historia después del scratch, hasta
	// alcanzar lo último que se generó; -1 mientras dura el scratch.
	replay int64
}

// histWrite guarda en la historia los cuadros de buf (estéreo).
func (e *Engine) histWrite(buf []float32) {
	if e.hist == nil {
		e.hist = make([]float32, histSeconds*e.rate*2)
	}
	size := int64(len(e.hist) / 2)
	for i := 0; i+1 < len(buf); i += 2 {
		j := (e.histN % size) * 2
		e.hist[j], e.hist[j+1] = buf[i], buf[i+1]
		e.histN++
	}
}

// histReset olvida la historia (un salto o un cambio de canción a mano: lo
// de antes ya no es lo que suena) y corta el scratch.
func (e *Engine) histReset() {
	if e.st != nil {
		e.st = newStretcher(e.speed) // un salto: lo que esperaba ya no vale
	}
	e.histFrom = e.histN
	e.scr = nil
}

// histAt es el cuadro de la historia en pos (cuadros, con decimales), con
// interpolación lineal.
func (e *Engine) histAt(pos float64) (l, r float32) {
	size := int64(len(e.hist) / 2)
	i := int64(math.Floor(pos))
	f := float32(pos - float64(i))
	a := (i % size) * 2
	b := ((i + 1) % size) * 2
	return e.hist[a] + (e.hist[b]-e.hist[a])*f, e.hist[a+1] + (e.hist[b+1]-e.hist[a+1])*f
}

// Scratch hace un scratch con lo que está sonando (dir 1: uno largo; -1: dos
// cortos). No hace nada si no está sonando o si aún no hay bastante música
// guardada.
func (e *Engine) Scratch(dir int) {
	moves, ok := scratchMoves[dir]
	if !ok {
		return
	}
	e.mu.Lock()
	ready := e.playing && e.scr == nil && e.hist != nil
	e.mu.Unlock()
	if !ready {
		return
	}
	e.mu.Lock()
	e.pending = func() {
		// Lo que se oía: lo último generado menos lo que había en el búfer
		// (que se acaba de tirar).
		start := float64(e.histN) - e.latency.Seconds()*float64(e.rate)
		maxBack := 0.0
		for _, m := range moves {
			maxBack = math.Max(maxBack, m.amp)
		}
		if start-maxBack*float64(e.rate) <= float64(max(e.histFrom, e.histN-int64(len(e.hist)/2)+1)) {
			return // aún no hay bastante
		}
		e.scr = &scratch{moves: moves, start: start, replay: -1}
	}
	e.mu.Unlock()
	if _, err := e.player.Seek(0, io.SeekStart); err != nil {
		log.Printf("player seek: %v", err)
	}
	e.player.Play()
}

// scratchRead llena buf con el scratch en curso (y después con lo que quedó
// sin sonar en la historia); devuelve cuántas muestras llenó. Al acabar,
// deja e.scr en nil y el resto lo llena la canción.
func (e *Engine) scratchRead(buf []float32) int {
	s := e.scr
	n := 0
	for n+1 < len(buf) {
		if s.replay >= 0 {
			if s.replay >= e.histN {
				e.scr = nil
				return n
			}
			buf[n], buf[n+1] = e.histAt(float64(s.replay))
			s.replay++
			n += 2
			continue
		}
		m := s.moves[0]
		frames := int(math.Round(m.dur * float64(e.rate)))
		u := float64(s.i) / float64(frames)
		// La aguja: atrás y de vuelta, con la forma de un seno; la velocidad
		// (1 = la normal) es su derivada.
		pos := s.start - m.amp*float64(e.rate)*math.Sin(math.Pi*u)
		v := math.Abs(m.amp / m.dur * math.Pi * math.Cos(math.Pi*u))
		l, r := e.histAt(pos)
		// Despacio suena apagado (un filtro de un polo que se abre con la
		// velocidad) y casi parado, nada.
		k := math.Min(1, 0.08+0.45*v)
		s.tone[0] += (float64(l) - s.tone[0]) * k
		s.tone[1] += (float64(r) - s.tone[1]) * k
		g := math.Min(1, v*0.9)
		buf[n], buf[n+1] = float32(s.tone[0]*g), float32(s.tone[1]*g)
		n += 2
		s.i++
		if s.i >= frames {
			s.i = 0
			s.moves = s.moves[1:]
			if len(s.moves) == 0 {
				s.replay = int64(math.Round(s.start))
			}
		}
	}
	return n
}
