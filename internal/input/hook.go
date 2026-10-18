package input

import "time"

// HookCounter cuenta las pulsaciones seguidas del botón único de los
// audífonos de cable (como en Android: una pausa, dos pasa a la siguiente,
// tres a la anterior). Press devuelve cuántas van; tras gap sin otra, el
// llamador actúa con Done.
type HookCounter struct {
	gap   time.Duration
	n     int
	last  time.Time
	timer *time.Timer
}

// NewHookCounter prepara el contador con la separación máxima entre
// pulsaciones.
func NewHookCounter(gap time.Duration) *HookCounter { return &HookCounter{gap: gap} }

// Press apunta una pulsación y llama a done(n) cuando pasa gap sin otra.
func (h *HookCounter) Press(at time.Time, done func(n int)) {
	if at.Sub(h.last) > h.gap {
		h.n = 0
	}
	h.n++
	h.last = at
	if h.timer != nil {
		h.timer.Stop()
	}
	n := h.n
	h.timer = time.AfterFunc(h.gap, func() {
		if h.n == n {
			h.n = 0
			done(n)
		}
	})
}
