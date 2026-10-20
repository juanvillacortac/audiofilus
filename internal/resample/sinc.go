package resample

import (
	"io"
	"math"
	"sync"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// El modo Quality: interpolación de banda limitada (la de Julius O. Smith):
// cada muestra de salida es la suma de las de origen que la rodean por un
// sinc con ventana de Kaiser, centrado en el instante exacto que le toca.
// El sinc corta en la frecuencia de Nyquist de la más baja de las dos
// frecuencias (al bajar, se ensancha en el tiempo: más muestras de origen),
// así que es plano hasta el 45 % de ella y rechaza unos 100 dB desde el 55 %
// (lo que se cuela entre el 50 y el 55 % cae por encima de 20 kHz).
//
// Los coeficientes se calculan una vez por par de frecuencias. Si la razón
// es una fracción pequeña (44,1 → 48 kHz es 160/147), hay una fila exacta por
// cada fase; si no, 512 filas y se interpola entre las dos más cercanas.

const (
	sincHalf      = 32   // muestras de origen a cada lado (64 en total) al subir
	sincBeta      = 10   // la ventana de Kaiser: ~100 dB de rechazo
	sincRows      = 512  // filas de la tabla para razones que no son fracciones pequeñas
	sincMaxPhases = 1024 // hasta cuántas fases se guardan exactas
	sincChunk     = 4096 // frames de origen que se leen de una vez
)

// sincTable son los coeficientes de un par de frecuencias.
type sincTable struct {
	half  int       // muestras de origen a cada lado
	taps  int       // 2*half
	rows  int       // filas (fases)
	exact bool      // una fila por fase (si no, se interpola entre filas)
	l, m  int       // con exact: cada salida avanza m/l frames de origen
	coef  []float32 // rows (+1 si no es exacta) × taps
}

var (
	tablesMu sync.Mutex
	tables   = map[[2]int]*sincTable{}
)

// tableFor es la tabla de in → out (calculada la primera vez).
func tableFor(in, out int) *sincTable {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	if t, ok := tables[[2]int{in, out}]; ok {
		return t
	}
	t := newSincTable(in, out)
	tables[[2]int{in, out}] = t
	return t
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// besselI0 es la función de Bessel modificada de orden 0 (para Kaiser).
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / 2) / float64(k)
		sum += term * term
		if term*term < sum*1e-17 {
			break
		}
	}
	return sum
}

// newSincTable calcula los coeficientes de in → out.
func newSincTable(in, out int) *sincTable {
	scale := math.Min(1, float64(out)/float64(in)) // < 1 al bajar de frecuencia
	half := int(math.Ceil(sincHalf / scale))
	t := &sincTable{half: half, taps: 2 * half}
	g := gcd(in, out)
	t.l, t.m = out/g, in/g
	if t.l <= sincMaxPhases {
		t.exact, t.rows = true, t.l
	} else {
		t.rows = sincRows
	}
	n := t.rows
	if !t.exact {
		n++ // la fila de fracción 1, para interpolar en la última
	}
	t.coef = make([]float32, n*t.taps)
	i0b := besselI0(sincBeta)
	row := make([]float64, t.taps)
	for r := 0; r < n; r++ {
		frac := float64(r) / float64(t.rows)
		sum := 0.0
		for j := range row {
			x := float64(j-half+1) - frac // la distancia al instante de salida
			v := scale
			if u := scale * x; u != 0 {
				v = scale * math.Sin(math.Pi*u) / (math.Pi * u)
			}
			if q := x / float64(half); q > -1 && q < 1 {
				v *= besselI0(sincBeta*math.Sqrt(1-q*q)) / i0b
			} else {
				v = 0
			}
			row[j] = v
			sum += v
		}
		// Cada fila suma 1: la continua pasa igual en todas las fases.
		for j, v := range row {
			t.coef[r*t.taps+j] = float32(v / sum)
		}
	}
	return t
}

// sincState es un Reader en modo Quality.
type sincState struct {
	src  decode.Source
	t    *sincTable
	step float64 // frames de origen por frame de salida

	buf    []float32 // frames de origen, estéreo intercalado
	frames int       // frames válidos en buf (incluidos los ceros del principio y del final)
	pos    int       // la muestra de origen en que está la salida (en buf)
	phase  int       // con tabla exacta: la fracción es phase/l
	frac   float64   // con tabla interpolada: la fracción
	eof    bool
	end    int // con eof: dónde acaban las muestras de verdad en buf
}

func newSinc(src decode.Source, in, out int) *sincState {
	s := &sincState{src: src, t: tableFor(in, out), step: float64(in) / float64(out)}
	s.buf = make([]float32, (sincChunk+3*s.t.taps)*2)
	s.reset()
	return s
}

// reset vacía el búfer (tras saltar en el origen): delante quedan ceros, la
// historia que necesita la primera salida.
func (s *sincState) reset() {
	h := s.t.half
	clear(s.buf[:(h-1)*2])
	s.frames, s.pos = h-1, h-1
	s.phase, s.frac = 0, 0
	s.eof, s.end = false, 0
}

// pending son los frames de origen leídos que aún no han salido.
func (s *sincState) pending() float64 {
	real := s.frames
	if s.eof {
		real = s.end
	}
	at := float64(s.pos) + s.frac
	if s.t.exact {
		at = float64(s.pos) + float64(s.phase)/float64(s.t.l)
	}
	return math.Max(0, float64(real)-at)
}

// refill conserva la historia que hace falta y lee más del origen; al
// acabarse, pone detrás los ceros que necesitan las últimas salidas.
func (s *sincState) refill() {
	h := s.t.half
	if start := s.pos - h + 1; start > 0 {
		copy(s.buf, s.buf[start*2:s.frames*2])
		s.frames -= start
		s.pos -= start
		if s.eof {
			s.end -= start
		}
	}
	if s.eof {
		return
	}
	room := len(s.buf)/2 - h - 1 // deja sitio para los ceros del final
	got, err := s.src.Read(s.buf[s.frames*2 : room*2])
	s.frames += got / 2
	if err != nil || got == 0 {
		s.eof, s.end = true, s.frames
		clear(s.buf[s.frames*2 : (s.frames+h)*2])
		s.frames += h
	}
}

// read llena dst con muestras estéreo intercaladas a la frecuencia de salida.
func (s *sincState) read(dst []float32) (int, error) {
	t := s.t
	h, taps := t.half, t.taps
	n := 0
	for n+1 < len(dst) {
		if s.eof && s.pos >= s.end {
			break
		}
		if s.pos+h >= s.frames {
			if s.eof {
				break
			}
			s.refill()
			continue
		}
		base := (s.pos - h + 1) * 2
		x := s.buf[base : base+taps*2]
		var l, r float32
		if t.exact {
			c := t.coef[s.phase*taps : (s.phase+1)*taps]
			for j, cj := range c {
				l += cj * x[2*j]
				r += cj * x[2*j+1]
			}
			s.phase += t.m
			s.pos += s.phase / t.l
			s.phase %= t.l
		} else {
			f := s.frac * float64(t.rows)
			row := int(f)
			w := float32(f - float64(row))
			c0 := t.coef[row*taps : (row+1)*taps]
			c1 := t.coef[(row+1)*taps : (row+2)*taps]
			var l1, r1 float32
			for j := range c0 {
				a, b := x[2*j], x[2*j+1]
				l += c0[j] * a
				r += c0[j] * b
				l1 += c1[j] * a
				r1 += c1[j] * b
			}
			l += (l1 - l) * w
			r += (r1 - r) * w
			s.frac += s.step
			adv := int(s.frac)
			s.pos += adv
			s.frac -= float64(adv)
		}
		dst[n], dst[n+1] = l, r
		n += 2
	}
	if n == 0 && s.eof {
		return 0, io.EOF
	}
	return n, nil
}
