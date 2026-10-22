package daemon

import (
	"log"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// jackFast es cada cuánto se mira el jack mientras suena cuando leerlo es
// barato (un archivo: el pin de debugfs, el de muOS): así desenchufarlos (o
// que se suelten de un tirón) pausa casi al momento, antes de que se oiga
// por el altavoz. Ninguna consola avisa con un evento. jackSlow, cuando
// leerlo lanza un programa (amixer, en ArkOS).
const (
	jackFast = 150 * time.Millisecond
	jackSlow = time.Second
)

// watchJack pausa la música cuando se desenchufan los audífonos de cable,
// como un teléfono: si no, de golpe sonaría por el altavoz. Mientras suena
// mira el jack cada jackFast (o jackSlow si leerlo es caro); en pausa solo
// mira, cada jackSlow, si volvió a sonar (casi nada en el bolsillo). Solo
// donde se sabe leer el jack (platform.WiredJack). Los bluetooth mandan su
// propia pausa al irse.
func (d *Daemon) watchJack(stop <-chan struct{}) {
	every := jackSlow
	tick := time.NewTicker(every)
	defer tick.Stop()
	setEvery := func(v time.Duration) {
		if v != every {
			every = v
			tick.Reset(v)
		}
	}
	was, known := false, false
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		if !d.eng.Playing() {
			known = false // al volver a sonar se mira de nuevo desde cero
			setEvery(jackSlow)
			continue
		}
		plugged, ok, cheap := platform.WiredJack()
		if !ok {
			setEvery(jackSlow)
			continue
		}
		if cheap {
			setEvery(jackFast)
		} else {
			setEvery(jackSlow)
		}
		if known && was && !plugged {
			log.Print("headphones unplugged: pause")
			d.eng.Pause()
		}
		was, known = plugged, true
	}
}
