package engine

import "math"

// El crossfeed para audífonos: con altavoces, cada oído oye también el otro
// lado, un poco más tarde y sin los agudos (la cabeza los tapa); con
// audífonos no, y los discos con instrumentos solo a un lado (los de los 60)
// cansan. Se manda a cada canal algo del otro, pasado por un paso bajo (que
// además lo retrasa un poco), como bs2b. Tres niveles, de los de bs2b.

// crossfeedLevels son los niveles: corte del paso bajo (Hz) y cuánto se
// manda (dB por debajo del canal directo).
var crossfeedLevels = map[string]struct{ cut, feed float64 }{
	"light":  {700, 9.5}, // el de Jan Meier: sutil
	"medium": {700, 6},   // el de Chu Moy
	"strong": {650, 4.5}, // el de bs2b por defecto: el más marcado
}

// CrossfeedLevels son los niveles en el orden en que se ofrecen ("" apagado).
var CrossfeedLevels = []string{"", "light", "medium", "strong"}

// crossfeed es el filtro en marcha.
type crossfeed struct {
	on     bool
	a, g   float32 // coeficiente del paso bajo de un polo y ganancia del cruce
	norm   float32 // para que los graves (que suman) no suban de nivel
	lpL    float32 // estado del paso bajo de cada canal
	lpR    float32
	level  string
	rateAt int
}

// set ajusta el filtro al nivel y la frecuencia de salida.
func (c *crossfeed) set(level string, rate int) {
	l, ok := crossfeedLevels[level]
	c.on, c.level, c.rateAt = ok, level, rate
	if !ok {
		return
	}
	c.a = float32(1 - math.Exp(-2*math.Pi*l.cut/float64(rate)))
	c.g = float32(math.Pow(10, -l.feed/20))
	c.norm = 1 / (1 + c.g)
}

// process mezcla los canales de buf (estéreo intercalado).
func (c *crossfeed) process(buf []float32) {
	if !c.on {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		l, r := buf[i], buf[i+1]
		c.lpL += c.a * (l - c.lpL)
		c.lpR += c.a * (r - c.lpR)
		buf[i] = (l + c.g*c.lpR) * c.norm
		buf[i+1] = (r + c.g*c.lpL) * c.norm
	}
}
