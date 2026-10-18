package engine

import (
	"math"
	"math/cmplx"
)

// fft es una FFT radix-2 iterativa de tamaño fijo (potencia de dos), con la
// ventana de Hann y las tablas ya calculadas: para el espectro del medidor.
type fft struct {
	n      int
	window []float64
	twid   []complex128
	rev    []int
	buf    []complex128
	mags   []float64
}

func newFFT(n int) fft {
	f := fft{n: n, window: make([]float64, n), twid: make([]complex128, n/2), rev: make([]int, n),
		buf: make([]complex128, n), mags: make([]float64, n/2+1)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := 0; i < n; i++ {
		f.window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		r := 0
		for b := 0; b < bits; b++ {
			r |= (i >> b & 1) << (bits - 1 - b)
		}
		f.rev[i] = r
	}
	for k := range f.twid {
		f.twid[k] = cmplx.Exp(complex(0, -2*math.Pi*float64(k)/float64(n)))
	}
	return f
}

// magnitudes devuelve |X[k]| normalizado (una senoidal de amplitud 1 da ~1)
// de k = 0 a n/2. El resultado se reutiliza en la siguiente llamada.
func (f *fft) magnitudes(x []float64) []float64 {
	for i := 0; i < f.n; i++ {
		f.buf[f.rev[i]] = complex(x[i]*f.window[i], 0)
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				t := f.twid[k*step] * f.buf[start+k+half]
				u := f.buf[start+k]
				f.buf[start+k], f.buf[start+k+half] = u+t, u-t
			}
		}
	}
	scale := 4 / float64(f.n) // 2 por el lado negativo y 2 por la ventana de Hann
	for k := range f.mags {
		f.mags[k] = cmplx.Abs(f.buf[k]) * scale
	}
	return f.mags
}
