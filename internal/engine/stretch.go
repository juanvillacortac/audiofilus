package engine

import "math"

// La velocidad de reproducción (audiolibros y podcasts): más rápido o más
// lento sin que cambie el tono, con WSOLA. Se cortan trozos solapados de la
// entrada a otro ritmo que el de la salida y se pegan con una ventana; cada
// trozo se corre un poco (hasta stDelta) hacia donde mejor encaja con lo que
// ya sonó, para que la voz no suene a eco ni a robot. Va después del
// remuestreo, a la frecuencia de salida, y solo con una velocidad distinta
// de 1 (la música no lo paga).

// Medidas de WSOLA, en cuadros a 48 kHz: ventanas de 32 ms con la mitad
// solapada y hasta 10 ms de búsqueda (bastan para la voz; la música también
// suena bien).
const (
	stWin   = 1536
	stHop   = stWin / 2
	stDelta = 480
)

// stWindow es la ventana de Hann periódica: con la mitad solapada suma 1.
var stWindow = func() []float32 {
	w := make([]float32, stWin)
	for i := range w {
		s := math.Sin(math.Pi * float64(i) / stWin)
		w[i] = float32(s * s)
	}
	return w
}()

// stretcher cambia la velocidad de lo que lee de read.
type stretcher struct {
	speed float64
	in    []float32 // la entrada que aún hace falta (estéreo intercalado)
	pos   float64   // dónde iría el siguiente trozo, en cuadros de in
	last  int       // dónde empezó el último trozo (-1: ninguno aún)
	ola   []float32 // la suma solapada en curso (stWin cuadros)
	out   []float32 // lo que ya está listo para salir
	eof   bool      // la entrada se acabó (fin de la cola)
	done  bool      // y ya salió lo último de la suma solapada
	// Para la posición: cuadros leídos de la entrada y entregados.
	read, given float64
}

func newStretcher(speed float64) *stretcher {
	return &stretcher{speed: speed, last: -1, ola: make([]float32, stWin*2)}
}

// Pending son los cuadros de la entrada leídos que aún no se oyeron: para
// que la posición no vaya adelantada.
func (s *stretcher) Pending() float64 { return math.Max(0, s.read-s.given*s.speed) }

// Read llena dst (estéreo intercalado) a la velocidad de s, leyendo de src
// lo que haga falta. Devuelve el error de src cuando ya no queda nada.
func (s *stretcher) Read(dst []float32, src func([]float32) (int, error)) (int, error) {
	n := 0
	var err error
	for n < len(dst) {
		if len(s.out) > 0 {
			k := copy(dst[n:], s.out)
			s.out = s.out[k:]
			n += k
			s.given += float64(k / 2)
			continue
		}
		if s.done {
			break
		}
		// Lo que pide el siguiente trozo: hasta su búsqueda más lejana.
		need := (int(s.pos)+stDelta+stWin)*2 - len(s.in)
		for need > 0 && !s.eof {
			var tmp [4096]float32
			got, e := src(tmp[:min(len(tmp), need+need%2)])
			s.in = append(s.in, tmp[:got]...)
			s.read += float64(got / 2)
			need -= got
			if e != nil {
				s.eof, err = true, e
			}
		}
		if s.eof && (int(s.pos)+stWin)*2 > len(s.in) {
			// Lo último: lo que quede en la suma solapada.
			s.out = append(s.out[:0], s.ola[:stHop*2]...)
			clear(s.ola)
			s.done = true
			continue
		}
		s.frame()
	}
	if n > 0 {
		return n, nil
	}
	return 0, err
}

// frame añade un trozo a la salida.
func (s *stretcher) frame() {
	at := int(s.pos)
	frames := len(s.in) / 2
	if s.last >= 0 {
		at = s.bestOffset(at, frames)
	}
	for i := 0; i < stWin && at+i < frames; i++ {
		w := stWindow[i]
		s.ola[i*2] += w * s.in[(at+i)*2]
		s.ola[i*2+1] += w * s.in[(at+i)*2+1]
	}
	s.out = append(s.out[:0], s.ola[:stHop*2]...)
	copy(s.ola, s.ola[stHop*2:])
	clear(s.ola[stHop*2:])
	s.last = at
	s.pos += stHop * s.speed
	// Lo que ya no hará falta: lo anterior a la búsqueda del siguiente y al
	// último trozo (de él sale la continuación que se compara).
	drop := min(s.last, int(s.pos)-stDelta)
	if drop > 0 {
		s.in = s.in[drop*2:]
		s.pos -= float64(drop)
		s.last -= drop
	}
}

// bestOffset busca alrededor de at el trozo que mejor sigue al último: el
// que más se parece (correlación normalizada, en mono y saltando muestras
// para que cueste poco) a lo que venía justo después de él.
func (s *stretcher) bestOffset(at, frames int) int {
	ref := s.last + stHop // la continuación natural del último trozo
	if ref+stHop > frames {
		return at
	}
	mono := func(i int) float32 { return s.in[i*2] + s.in[i*2+1] }
	best, bestScore := at, float32(-math.MaxFloat32)
	for d := -stDelta; d <= stDelta; d += 2 {
		c := at + d
		if c < 0 || c+stHop > frames {
			continue
		}
		var dot, energy float32
		for i := 0; i < stHop; i += 4 {
			x := mono(c + i)
			dot += mono(ref+i) * x
			energy += x * x
		}
		score := dot / float32(math.Sqrt(float64(energy))+1e-6)
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}
