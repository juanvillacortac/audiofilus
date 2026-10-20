package milkdrop

// Cycler decide cuándo pasar al preset siguiente: cada Interval segundos o,
// con OnBeat, en el primer golpe después de ese tiempo (y como mucho
// beatWait segundos más tarde). Locked lo para.
type Cycler struct {
	Interval float64
	OnBeat   bool
	Locked   bool
	since    float64
}

// beatWait es lo más que se espera a un golpe.
const beatWait = 6

// Tick avanza dt segundos; beat dice si hubo un golpe en este fotograma.
// Devuelve true si toca cambiar (y vuelve a contar).
func (c *Cycler) Tick(dt float64, beat bool) bool {
	if c.Locked || c.Interval <= 0 {
		return false
	}
	c.since += dt
	due := c.since >= c.Interval
	if c.OnBeat {
		due = (due && beat) || c.since >= c.Interval+beatWait
	}
	if due {
		c.since = 0
	}
	return due
}

// Reset vuelve a contar (al cambiar de preset a mano).
func (c *Cycler) Reset() { c.since = 0 }

// Progress es cuánto lleva el preset de su tiempo (0..1).
func (c *Cycler) Progress() float64 {
	if c.Interval <= 0 {
		return 0
	}
	return clamp(c.since/c.Interval, 0, 1)
}
