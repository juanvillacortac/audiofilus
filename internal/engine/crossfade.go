package engine

import (
	"math"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
	"github.com/juanvillacortac/audiofilus/internal/queue"
	"github.com/juanvillacortac/audiofilus/internal/resample"
)

// El fundido entre canciones (DSP.Crossfade): cuando a la que suena le
// quedan esos segundos, se pasa ya a la siguiente (que se anuncia como
// siempre) y la anterior sigue sonando por debajo, apagándose, hasta su
// final. Se mezclan con potencia constante (seno y coseno), para que a
// medio camino no baje el volumen. Solo al pasar solas de canción: un cambio
// a mano, un salto o una pausa no funden.

// voice es una canción que suena: la que se apaga en un fundido.
type voice struct {
	src  decode.Source
	rs   *resample.Reader
	gain float32
}

// xfChunk es el trozo (en cuadros) en que se lee mientras hay fundido, para
// empezarlo a tiempo.
const xfChunk = 1024

// maybeCrossfadeLocked empieza el fundido si a la canción le queda lo que
// dura, y si toca (ver canCrossfadeLocked).
func (e *Engine) maybeCrossfadeLocked() {
	if e.tail != nil || e.xfLen > 0 || e.src == nil || e.rs == nil || e.scr != nil {
		return
	}
	n := e.src.Length()
	if n <= 0 {
		return // sin duración (una radio): no se sabe cuándo acaba
	}
	rate := float64(e.src.SampleRate())
	left := (float64(n-e.src.Position()) + e.rs.Pending()) / rate
	fade := e.dsp.Crossfade
	// Una canción muy corta (un interludio) no se funde.
	if left > fade || left <= 0.05 || float64(n)/rate < 3*fade {
		return
	}
	if !e.canCrossfadeLocked() {
		return
	}
	old := &voice{src: e.src, rs: e.rs, gain: e.gain}
	e.src, e.rs = nil, nil // que el cambio de canción no la cierre
	if !e.advanceLocked(true) || e.rs == nil {
		if e.src != nil {
			e.src.Close()
		}
		e.src, e.rs = old.src, old.rs
		return
	}
	e.tail, e.xfLen, e.xfPos = old, max(1, int(left*float64(e.rate))), 0
}

// canCrossfadeLocked dice si la siguiente canción se funde con esta: no si
// se repite la misma, si es remota (puede no estar bajada aún) o si es la
// que sigue del mismo disco, en orden.
func (e *Engine) canCrossfadeLocked() bool {
	if e.q.Repeat == queue.RepeatOne || e.stopAfter {
		return false
	}
	next := e.q.Peek()
	if next == "" || strings.Contains(next, "://") || next == e.xfSkip {
		return false
	}
	if !e.q.Shuffled() && e.track.Album != "" && library.ReadTrack(next).Album == e.track.Album {
		e.xfSkip = next // no volver a leer sus etiquetas en cada trozo
		return false
	}
	return true
}

// mixTailLocked mezcla en chunk (lo nuevo, ya leído) lo que se está
// apagando, con las curvas del fundido.
func (e *Engine) mixTailLocked(chunk []float32) {
	if e.xfLen == 0 {
		return
	}
	frames := len(chunk) / 2
	if cap(e.tbuf) < len(chunk) {
		e.tbuf = make([]float32, len(chunk))
	}
	tail := e.tbuf[:len(chunk)]
	got := 0
	if e.tail != nil {
		for got < len(tail) {
			n, err := e.tail.rs.Read(tail[got:])
			got += n
			if err != nil {
				e.tail.src.Close()
				e.tail = nil // se acabó: el resto, silencio
				break
			}
		}
		gainBuf(tail[:got], e.tailGain())
	}
	clear(tail[got:])
	for i := 0; i < frames; i++ {
		t := math.Min(1, float64(e.xfPos+i)/float64(e.xfLen))
		in, out := float32(math.Sin(t*math.Pi/2)), float32(math.Cos(t*math.Pi/2))
		chunk[2*i] = chunk[2*i]*in + tail[2*i]*out
		chunk[2*i+1] = chunk[2*i+1]*in + tail[2*i+1]*out
	}
	e.xfPos += frames
	if e.xfPos >= e.xfLen {
		e.stopCrossfadeLocked()
	}
}

// tailGain es la ganancia de lo que se apaga (0 si ya no está).
func (e *Engine) tailGain() float32 {
	if e.tail == nil {
		return 0
	}
	return e.tail.gain
}

// stopCrossfadeLocked termina el fundido (cerrando lo que se apagaba).
func (e *Engine) stopCrossfadeLocked() {
	if e.tail != nil {
		e.tail.src.Close()
		e.tail = nil
	}
	e.xfLen, e.xfPos = 0, 0
}
