package sfx

import (
	"math"
	"strconv"
	"strings"
)

// Los sonidos de Bloques, al estilo de Tetris Effect: campanitas de la escala
// pentatónica de la tonalidad de lo que suena (mayor: 1 2 3 5 6; menor:
// 1 b3 4 5 b7), que no tiene semitonos que choquen con la canción. Mover toca
// la nota de la columna (más aguda a la derecha), girar sube o baja un paso,
// soltar es un golpe grave con un brillo, y borrar líneas es un arpegio que
// sube más cuantas más líneas. Son senoidales con su octava, que se apagan
// solas, como los de Cripta: quedan por debajo de la música.
//
// El nombre lleva la tonalidad detrás de una "@" ("bk_move_3@F#m"); sin ella
// (o si no se sabe), do mayor.

// BlockPrefix es el prefijo de los sonidos de Bloques.
const BlockPrefix = "bk_"

// scale son las notas de una tonalidad: deg da la frecuencia del grado d de
// la pentatónica (0 la tónica, 5 su octava...).
type scale struct {
	tonic float64 // Hz del grado 0, entre sol4 y fa#5
	steps [5]int  // semitonos de cada grado desde la tónica
}

// keyScale es la escala de una tonalidad ("C", "F#m"); do mayor si no se
// entiende.
func keyScale(key string) scale {
	minor := strings.HasSuffix(key, "m")
	root := strings.TrimSuffix(key, "m")
	pc := 0
	for i, n := range [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"} {
		if n == root {
			pc = i
		}
	}
	if pc >= 7 {
		pc -= 12 // de sol a si, una octava abajo: que no queden chillonas
	}
	s := scale{tonic: 523.25 * math.Pow(2, float64(pc)/12), steps: [5]int{0, 2, 4, 7, 9}}
	if minor {
		s.steps = [5]int{0, 3, 5, 7, 10}
	}
	return s
}

func (s scale) deg(d int) float64 {
	return s.tonic * math.Pow(2, float64(s.steps[d%5]+12*(d/5))/12)
}

// bellGain sube todas las campanitas: con el volumen de los sonidos se
// perdían bajo la música.
const bellGain = 2.0

// bell es una campanita de frecuencia f que empieza en at: la nota y, más
// bajo, su octava, que le da el brillo.
func bell(at, f, d, amp float64) []layer {
	amp *= bellGain
	return []layer{{at, f, f, d, false, amp}, {at, 2 * f, 2 * f, d * 0.6, false, amp * 0.35}}
}

// blockLayers son las capas del sonido base de Bloques en la escala s; nil
// si no existe.
func blockLayers(base string, s scale) []layer {
	num := func(prefix string) (int, bool) {
		n, err := strconv.Atoi(strings.TrimPrefix(base, prefix))
		return n, strings.HasPrefix(base, prefix) && err == nil
	}
	drop := []layer{{0, 130, 45, 0.18, false, 0.50}, {0, 4000, 700, 0.07, true, 0.22}}
	if x, ok := num("bk_move_"); ok && x >= 0 && x < 10 {
		return bell(0, s.deg(x), 0.07, 0.10)
	}
	if r, ok := num("bk_rot_"); ok && r >= 0 && r < 4 {
		// Girar: dos notas seguidas de la escala, que cambian con el giro.
		return append(bell(0, s.deg(5+r), 0.06, 0.10), bell(0.03, s.deg(6+r), 0.08, 0.09)...)
	}
	if n, ok := num("bk_clear_"); ok && n >= 1 && n <= 4 {
		// Borrar n líneas: un arpegio de n+2 notas hacia arriba desde la
		// tónica, con un barrido de ruido; un TETRA acaba en un acorde
		// abierto (tónica, quinta y la décima) que se queda sonando.
		var ls []layer
		for i := 0; i < n+2; i++ {
			ls = append(ls, bell(float64(i)*0.045, s.deg(min(i*2, 9)), 0.35, 0.13)...)
		}
		ls = append(ls, layer{0, 6000, 1500, 0.15 + 0.1*float64(n), true, 0.08 * float64(n)})
		if n == 4 {
			ls = append(ls, bell(0.25, s.deg(0), 0.9, 0.12)...)
			ls = append(ls, bell(0.25, s.deg(3), 0.9, 0.10)...)
			ls = append(ls, bell(0.25, s.deg(7), 0.9, 0.08)...)
		}
		return ls
	}
	switch base {
	case "bk_hold":
		return append(bell(0, s.deg(4), 0.06, 0.12), bell(0.05, s.deg(2), 0.10, 0.12)...)
	case "bk_lock":
		return []layer{{0, 150, 90, 0.07, false, 0.30}, {0, 2500, 900, 0.025, true, 0.10}}
	case "bk_drop":
		return append(drop, bell(0.01, s.deg(5), 0.25, 0.12)...)
	case "bk_beat": // soltada a tiempo: un brillo más, dos octavas arriba
		return append(append(drop, bell(0.01, s.deg(5), 0.25, 0.12)...), bell(0.02, s.deg(10), 0.30, 0.10)...)
	case "bk_level":
		return append(append(bell(0, s.deg(3), 0.15, 0.12), bell(0.08, s.deg(5), 0.15, 0.12)...), bell(0.16, s.deg(8), 0.4, 0.12)...)
	case "bk_over":
		low := s.deg(0) / 4
		return append(append(bell(0, s.deg(3), 0.3, 0.13), bell(0.18, s.deg(2), 0.3, 0.13)...),
			append(bell(0.36, s.deg(0), 0.8, 0.13), layer{0.36, low, low / 2, 0.8, false, 0.3})...)
	}
	return nil
}

// block sintetiza un sonido de Bloques ("bk_move_3@F#m"); nil si no existe.
func block(name string) []float32 {
	base, key, _ := strings.Cut(name, "@")
	ls := blockLayers(base, keyScale(key))
	if ls == nil {
		return nil
	}
	return layered(ls)
}
