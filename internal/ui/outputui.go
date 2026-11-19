package ui

import (
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/platform"
)

// La salida de audio (Ajustes > Sonido): Automático (un DAC USB al
// conectarlo, de vuelta a la de antes al quitarlo) o una fija. Las salidas
// las da el servicio en su estado (daemon/outputs.go); si no da ninguna, el
// sistema ya lo hace solo (Windows, macOS, el móvil) y la baldosa no sale.

// outputName es el nombre de la salida id para mostrar.
func (a *App) outputName(id string) string {
	for _, o := range a.status.Outputs {
		if o.ID == id {
			return outputLabel(o)
		}
	}
	if id == platform.OutputBuiltin {
		return T("out_builtin")
	}
	return id
}

// outputLabel es el nombre de una salida: el del sistema, o "La de la
// consola" para la tarjeta de siempre de ALSA.
func outputLabel(o platform.Output) string {
	switch {
	case o.ID == platform.OutputBuiltin:
		return T("out_builtin")
	case o.HDMI:
		return "HDMI"
	case strings.TrimSpace(o.Name) == "":
		return o.ID
	}
	return o.Name
}

// outputValue es lo que se ve en la baldosa: la que suena, y si es en
// automático.
func (a *App) outputValue() string {
	name := a.outputName(a.status.OutputID)
	if a.status.OutputPref == "" {
		return T("out_auto") + " · " + name
	}
	return name
}

// openOutputMenu abre la elección de salida.
func (a *App) openOutputMenu() {
	choose := func(id string) {
		a.cfg.AudioOutput = id
		a.saveConfig()
		a.status.OutputPref = id
		a.send("output", orDefault(id, "auto"))
	}
	m := &menu{title: T("set_output"), subtitle: T("out_sub")}
	m.items = append(m.items, menuItem{label: T("out_auto"), icon: "headphones",
		active: func() bool { return a.status.OutputPref == "" },
		do:     func() { choose("") }})
	for _, o := range a.status.Outputs {
		icon := "speaker"
		switch {
		case o.USB:
			icon = "headphones"
		case o.HDMI:
			icon = "screen"
		case strings.HasPrefix(o.ID, "bluez"):
			icon = "bluetooth"
		}
		m.items = append(m.items, menuItem{label: outputLabel(o), icon: icon,
			value:  func() string { return a.outputState(o.ID) },
			active: func() bool { return a.status.OutputPref == o.ID },
			do:     func() { choose(o.ID) }})
	}
	a.menu = m
}

// outputState dice debajo de cada salida si es la que suena.
func (a *App) outputState(id string) string {
	if a.status.OutputID == id {
		return T("out_playing")
	}
	return ""
}
