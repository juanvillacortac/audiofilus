// Package resample lleva un decode.Source a la frecuencia de salida.
//
// Usa interpolación lineal: es barata en CPU, y en una portátil con batería eso
// pesa más que la pequeña pérdida en las frecuencias más altas.
package resample

import (
	"io"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Reader lee frames estéreo float32 de src a outRate.
type Reader struct {
	src     decode.Source
	inRate  int
	outRate int
	step    float64 // frames de origen que avanza cada frame de salida

	buf    []float32 // frames de origen, estéreo intercalado
	frames int       // frames válidos en buf
	t      float64   // posición de lectura en buf, en frames
	eof    bool
}

// New envuelve src para que Read devuelva frames a outRate.
func New(src decode.Source, outRate int) *Reader {
	r := &Reader{src: src, inRate: src.SampleRate(), outRate: outRate}
	r.step = float64(r.inRate) / float64(outRate)
	r.buf = make([]float32, 4096*2)
	return r
}

// Passthrough dice si no hace falta convertir (misma frecuencia).
func (r *Reader) Passthrough() bool { return r.inRate == r.outRate }

// Pending son los frames de origen que ya se leyeron de src pero aún no han
// salido (los que esperan en el búfer para interpolar). La posición de lo que
// suena es la de src menos estos: si no, avanza a saltos del tamaño del
// búfer (hasta 0,19 s con un WAV de 22 kHz) y la barra de la interfaz
// retrocede al corregirse.
func (r *Reader) Pending() float64 {
	if r.Passthrough() {
		return 0
	}
	return max(0, float64(r.frames)-r.t)
}

// Reset descarta lo que hay en el búfer; se llama después de saltar en el origen.
func (r *Reader) Reset() {
	r.frames = 0
	r.t = 0
	r.eof = false
}

// Read llena dst con muestras estéreo intercaladas a la frecuencia de salida.
func (r *Reader) Read(dst []float32) (int, error) {
	if r.Passthrough() {
		return r.src.Read(dst)
	}
	n := 0
	for n+1 < len(dst) {
		i := int(r.t)
		if i+1 >= r.frames {
			if r.eof {
				break
			}
			r.refill()
			continue
		}
		frac := float32(r.t - float64(i))
		a, b := r.buf[i*2:], r.buf[i*2+2:]
		dst[n] = a[0] + (b[0]-a[0])*frac
		dst[n+1] = a[1] + (b[1]-a[1])*frac
		n += 2
		r.t += r.step
	}
	if n == 0 && r.eof {
		return 0, io.EOF
	}
	return n, nil
}

// refill conserva los frames que aún hacen falta para interpolar y lee más.
func (r *Reader) refill() {
	keep := int(r.t)
	if keep > r.frames {
		keep = r.frames
	}
	copy(r.buf, r.buf[keep*2:r.frames*2])
	r.frames -= keep
	r.t -= float64(keep)

	got, err := r.src.Read(r.buf[r.frames*2:])
	r.frames += got / 2
	if err != nil || got == 0 {
		r.eof = true
	}
}
