package input

import "time"

// Event es una pulsación o suelta de un botón lógico.
type Event struct {
	Button  Button
	Pressed bool
	Time    time.Time
}
