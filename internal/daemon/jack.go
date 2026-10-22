package daemon

import (
	"log"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// jackEvery es cada cuánto se mira el jack de los audífonos mientras suena:
// lo bastante seguido para que desenchufarlos (o que se suelten de un tirón)
// pause enseguida, y barato (un archivo de debugfs o de muOS).
const jackEvery = time.Second

// watchJack pausa la música cuando se desenchufan los audífonos de cable,
// como un teléfono: si no, de golpe sonaría por el altavoz. Solo mira
// mientras suena (en pausa no gasta nada) y solo donde se sabe leer el jack
// (platform.WiredJack). Los bluetooth mandan su propia pausa al irse.
func (d *Daemon) watchJack(stop <-chan struct{}) {
	tick := time.NewTicker(jackEvery)
	defer tick.Stop()
	was, known := false, false
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		if d.eng.Status().State != "playing" {
			known = false // al volver a sonar se mira de nuevo desde cero
			continue
		}
		plugged, ok := platform.WiredJack()
		if !ok {
			continue
		}
		if known && was && !plugged {
			log.Print("headphones unplugged: pause")
			d.eng.Pause()
		}
		was, known = plugged, true
	}
}
