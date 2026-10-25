// Package queue lleva el orden de reproducción: aleatorio, repetición y
// anterior/siguiente.
package queue

import "math/rand/v2"

// Repeat es el modo de repetición.
type Repeat int

const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

func (r Repeat) String() string {
	switch r {
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	}
	return "off"
}

// ParseRepeat acepta "off", "all" u "one".
func ParseRepeat(s string) (Repeat, bool) {
	switch s {
	case "off":
		return RepeatOff, true
	case "all":
		return RepeatAll, true
	case "one":
		return RepeatOne, true
	}
	return RepeatOff, false
}

// Queue es la lista de canciones y la posición en ella. No es segura para uso
// concurrente: el engine la protege con su mutex.
type Queue struct {
	tracks  []string
	order   []int // orden de reproducción: índices en tracks
	linear  []int // orden sin aleatorio (conserva lo insertado con PlayNext)
	cur     int   // índice en order; -1 si está vacía
	shuffle bool
	Repeat  Repeat
	rng     *rand.Rand
}

// New devuelve una cola vacía.
func New() *Queue {
	return &Queue{cur: -1, rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
}

// Set reemplaza las canciones y empieza en start (un índice en tracks).
func (q *Queue) Set(tracks []string, start int) {
	q.tracks = append([]string(nil), tracks...)
	q.order = make([]int, len(tracks))
	for i := range q.order {
		q.order[i] = i
	}
	q.linear = append([]int(nil), q.order...)
	q.cur = -1
	if len(tracks) == 0 {
		return
	}
	if start < 0 || start >= len(tracks) {
		start = 0
	}
	q.cur = start
	if q.shuffle {
		q.reshuffle()
	}
}

// Append añade canciones al final del orden de reproducción.
func (q *Queue) Append(tracks []string) {
	for _, t := range tracks {
		q.order = append(q.order, len(q.tracks))
		q.linear = append(q.linear, len(q.tracks))
		q.tracks = append(q.tracks, t)
	}
	if q.cur < 0 && len(q.tracks) > 0 {
		q.cur = 0
	}
}

// PlayNext inserta una canción justo después de la actual.
func (q *Queue) PlayNext(track string) {
	q.tracks = append(q.tracks, track)
	idx := len(q.tracks) - 1
	at := q.cur + 1
	q.order = append(q.order, 0)
	copy(q.order[at+1:], q.order[at:])
	q.order[at] = idx
	q.linear = insertAfter(q.linear, q.currentTrack(), idx)
	if q.cur < 0 {
		q.cur = 0
	}
}

// Current devuelve la canción actual, o "" si la cola está vacía.
func (q *Queue) Current() string {
	if q.cur < 0 || q.cur >= len(q.order) {
		return ""
	}
	return q.tracks[q.order[q.cur]]
}

// Index es la posición en el orden de reproducción y Len el número de canciones.
func (q *Queue) Index() int { return q.cur }
func (q *Queue) Len() int   { return len(q.order) }

// Shuffled dice si el aleatorio está activo.
func (q *Queue) Shuffled() bool { return q.shuffle }

// SetShuffle activa o desactiva el aleatorio sin cambiar la canción actual.
func (q *Queue) SetShuffle(on bool) {
	if on == q.shuffle {
		return
	}
	q.shuffle = on
	if on {
		q.reshuffle()
		return
	}
	curTrack := q.currentTrack()
	q.order = append(q.order[:0], q.linear...)
	q.cur = -1
	for i, t := range q.order {
		if t == curTrack {
			q.cur = i
		}
	}
	if q.cur < 0 && len(q.order) > 0 {
		q.cur = 0
	}
}

// currentTrack es el índice en tracks de la canción actual, o -1.
func (q *Queue) currentTrack() int {
	if q.cur < 0 || q.cur >= len(q.order) {
		return -1
	}
	return q.order[q.cur]
}

// insertAfter inserta v después del elemento igual a after (o al principio).
func insertAfter(s []int, after, v int) []int {
	at := 0
	for i, x := range s {
		if x == after {
			at = i + 1
			break
		}
	}
	s = append(s, 0)
	copy(s[at+1:], s[at:])
	s[at] = v
	return s
}

// reshuffle desordena y pone la canción actual la primera.
func (q *Queue) reshuffle() {
	curTrack := -1
	if q.cur >= 0 {
		curTrack = q.order[q.cur]
	}
	q.rng.Shuffle(len(q.order), func(i, j int) { q.order[i], q.order[j] = q.order[j], q.order[i] })
	if curTrack >= 0 {
		for i, t := range q.order {
			if t == curTrack {
				q.order[0], q.order[i] = q.order[i], q.order[0]
				break
			}
		}
		q.cur = 0
	}
}

// Next pasa a la siguiente canción, porque la actual terminó (auto) o porque el
// usuario la saltó (!auto). Devuelve false al llegar al final de la cola.
func (q *Queue) Next(auto bool) bool {
	if len(q.order) == 0 {
		return false
	}
	if auto && q.Repeat == RepeatOne {
		return true
	}
	if q.cur+1 < len(q.order) {
		q.cur++
		return true
	}
	if q.Repeat == RepeatOff {
		return false
	}
	if q.shuffle {
		last := q.order[q.cur]
		q.rng.Shuffle(len(q.order), func(i, j int) { q.order[i], q.order[j] = q.order[j], q.order[i] })
		if len(q.order) > 1 && q.order[0] == last {
			q.order[0], q.order[1] = q.order[1], q.order[0]
		}
	}
	q.cur = 0
	return true
}

// Peek es la canción que vendrá después de la actual, sin moverse (para
// bajarla antes si es remota). "" si no hay.
func (q *Queue) Peek() string {
	switch {
	case len(q.order) == 0:
		return ""
	case q.Repeat == RepeatOne:
		return q.tracks[q.order[q.cur]]
	case q.cur+1 < len(q.order):
		return q.tracks[q.order[q.cur+1]]
	case q.Repeat == RepeatAll && !q.shuffle:
		return q.tracks[q.order[0]]
	}
	return ""
}

// Prev vuelve a la canción anterior. Devuelve false al principio de la cola.
func (q *Queue) Prev() bool {
	if len(q.order) == 0 {
		return false
	}
	if q.cur > 0 {
		q.cur--
		return true
	}
	if q.Repeat == RepeatAll {
		q.cur = len(q.order) - 1
		return true
	}
	return false
}

// Jump salta a la posición i del orden de reproducción.
func (q *Queue) Jump(i int) bool {
	if i < 0 || i >= len(q.order) {
		return false
	}
	q.cur = i
	return true
}

// Tracks devuelve las canciones en orden de reproducción.
func (q *Queue) Tracks() []string {
	out := make([]string, len(q.order))
	for i, t := range q.order {
		out[i] = q.tracks[t]
	}
	return out
}

// IDs son los ids de las canciones en orden de reproducción: cada una el
// suyo, que no cambia mientras siga en la cola (su índice en tracks, que no
// se reutiliza hasta un Set). Los usa MPD (songid).
func (q *Queue) IDs() []int { return append([]int(nil), q.order...) }

// Track es la canción de id (de IDs), y si está en la cola.
func (q *Queue) Track(id int) (string, bool) {
	for _, t := range q.order {
		if t == id {
			return q.tracks[t], true
		}
	}
	return "", false
}

// Remove quita la canción de la posición pos del orden de reproducción y dice
// si era la actual (entonces la actual pasa a ser la que la seguía, o la
// anterior si era la última; -1 si la cola queda vacía).
func (q *Queue) Remove(pos int) (wasCurrent bool) {
	if pos < 0 || pos >= len(q.order) {
		return false
	}
	t := q.order[pos]
	q.order = append(q.order[:pos], q.order[pos+1:]...)
	for i, x := range q.linear {
		if x == t {
			q.linear = append(q.linear[:i], q.linear[i+1:]...)
			break
		}
	}
	wasCurrent = pos == q.cur
	switch {
	case len(q.order) == 0:
		q.cur = -1
	case pos < q.cur:
		q.cur--
	case q.cur >= len(q.order):
		q.cur = len(q.order) - 1
	}
	return wasCurrent
}

// Move lleva la canción de la posición from a la posición to del orden de
// reproducción; la actual sigue siendo la misma canción.
func (q *Queue) Move(from, to int) {
	n := len(q.order)
	if from < 0 || from >= n || to < 0 || to >= n || from == to {
		return
	}
	cur := q.currentTrack()
	t := q.order[from]
	q.order = append(q.order[:from], q.order[from+1:]...)
	q.order = append(q.order[:to], append([]int{t}, q.order[to:]...)...)
	if !q.shuffle {
		q.linear = append(q.linear[:0], q.order...)
	}
	for i, x := range q.order {
		if x == cur {
			q.cur = i
		}
	}
}
