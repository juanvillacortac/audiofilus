package daemon

import (
	"errors"
	"log"

	"github.com/juanvillacortac/audiofilus/internal/control"
	"strconv"
	"time"
)

// El temporizador para parar la música: a los N minutos (con el fundido de
// la pausa) o al acabar la canción que suena. Lo lleva el servicio, así
// funciona con la interfaz cerrada y la consola en el bolsillo.
//
//	timer <minutos>   para dentro de esos minutos
//	timer track       para al acabar esta canción
//	timer off         lo quita

// setTimer pone o quita el temporizador.
func (d *Daemon) setTimer(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: timer <minutes>|track|off")
	}
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.timerAt = time.Time{}
	d.mu.Unlock()
	d.eng.SetStopAfter(false)
	switch args[0] {
	case "off":
		log.Print("timer: off")
		return nil
	case "track":
		log.Print("timer: at the end of this song")
		d.eng.SetStopAfter(true)
		return nil
	}
	min, err := strconv.ParseFloat(args[0], 64)
	if err != nil || min <= 0 || min > 24*60 {
		return errors.New("usage: timer <minutes>|track|off")
	}
	after := time.Duration(min * float64(time.Minute))
	d.mu.Lock()
	d.timerAt = time.Now().Add(after)
	d.timer = time.AfterFunc(after, d.timerDone)
	d.mu.Unlock()
	log.Printf("timer: stopping in %v", after)
	return nil
}

// timerDone para la música al acabar el tiempo.
func (d *Daemon) timerDone() {
	d.mu.Lock()
	d.timer, d.timerAt = nil, time.Time{}
	d.mu.Unlock()
	log.Print("timer: time is up, pausing")
	d.eng.Pause()
	control.Notify()
}
