package ui

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// El HDMI en Knulli: al conectar o desconectar la tele, Knulli
// (knulli-hdmi-switch) reinicia EmulationStation, que al arrancar toma la
// salida nueva; pero si hay un "juego" abierto (y Audiofilus lo es, para
// ES) no hace nada. Aquí se hace lo mismo con la interfaz: se cierra (la
// música sigue en el servicio), el lanzador del port la abre otra vez ya en
// la salida nueva y el modo juego vuelve a donde estaba. Durante un juego
// la interfaz está cerrada: no hace falta nada.

const (
	// hdmiState es el estado del conector ("HDMI=0" o "HDMI=1").
	hdmiState = "/sys/devices/platform/soc/6000000.hdmi/extcon/hdmi/state"
	// RestartPath es la marca para que el lanzador del port vuelva a abrir
	// la interfaz al cerrarse (portmaster/Audiofilus.sh).
	RestartPath = "/tmp/audiofilus-restart"
)

// watchHDMI mira el conector cada segundo mientras dure la interfaz (solo
// en Knulli con el lanzador del port, que es quien la vuelve a abrir).
func (a *App) watchHDMI() {
	if a.offline || os.Getenv("AUDIOFILUS_LAUNCHER") == "" {
		return
	}
	first, err := os.ReadFile(hdmiState)
	if err != nil {
		return // no tiene HDMI (o no es Knulli)
	}
	go func() {
		for {
			time.Sleep(time.Second)
			now, err := os.ReadFile(hdmiState)
			if err == nil && strings.TrimSpace(string(now)) != strings.TrimSpace(string(first)) {
				a.hdmiChanged.Store(true)
				return
			}
		}
	}()
}

// updateHDMI, si cambió el HDMI, pasa el sonido a donde toca y cierra la
// interfaz para que el lanzador la abra en la salida nueva. Dice si se
// está cerrando.
func (a *App) updateHDMI() bool {
	if !a.hdmiChanged.Load() || a.quitting {
		return false
	}
	log.Printf("hdmi: cable changed: restarting the interface on the new output")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "knulli-audio", "set", "auto").CombinedOutput(); err != nil {
		log.Printf("hdmi: knulli-audio set auto: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if a.gm.on {
		a.gmSaveConf()
		a.gmSaveResume()
	}
	if err := os.WriteFile(RestartPath, nil, 0o644); err != nil {
		log.Printf("hdmi: %v", err)
		return false
	}
	a.exit(true) // la música sigue
	return true
}
