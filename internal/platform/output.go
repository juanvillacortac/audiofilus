//go:build linux

package platform

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Las salidas de audio que distingue OutputKind.
const (
	OutputSpeaker   = "speaker"   // el altavoz (o cualquier otra cosa)
	OutputWired     = "wired"     // audífonos de cable
	OutputBluetooth = "bluetooth" // audífonos bluetooth
)

// AudioOutput dice por dónde sale el sonido ahora: la salida por defecto de
// PulseAudio (o PipeWire) y su puerto activo. Sirve en el PC y en Knulli, que
// usan el mismo servidor; si pactl falla, devuelve "".
//
// En las portátiles con H700 (las Anbernic RG**XX) PulseAudio no ve el jack:
// la salida es siempre la misma y su puerto no cambia. Allí el kernel tiene un
// pin "Headphone detection" que sí cambia (alto con audífonos), y se lee en
// debugfs (/sys/kernel/debug/gpio; Knulli lo monta al arrancar).
func AudioOutput() string {
	// muOS: sus scripts dejan /run/muos/headphones con los audífonos de cable;
	// el bluetooth sale de PipeWire.
	if _, err := os.Stat(muosRun); err == nil {
		if info, err := exec.Command("wpctl", "inspect", "@DEFAULT_AUDIO_SINK@").Output(); err == nil && strings.Contains(string(info), "bluez") {
			return OutputBluetooth
		}
		if _, err := os.Stat(muosRun + "/headphones"); err == nil {
			return OutputWired
		}
		return OutputSpeaker
	}
	// Las portátiles con el pin de los audífonos (H700): el pin dice cable o
	// altavoz, y para el bluetooth basta el nombre de la salida por defecto
	// (pactl info). pactl list sinks no se usa allí: lista todas las salidas
	// consultando sus controles de ALSA, también los del audio HDMI, y en los
	// Allwinner el HDMI comparte controlador con el vídeo: con la consola en
	// la tele, cada consulta paraba la imagen casi un segundo.
	if b, err := os.ReadFile(gpioDebug); err == nil {
		if plugged, found := jackFromGPIO(string(b)); found {
			if strings.HasPrefix(pactlDefaultSink(), "bluez") {
				return OutputBluetooth
			}
			if plugged {
				return OutputWired
			}
			return OutputSpeaker
		}
	}
	out := ""
	start := time.Now()
	if name := pactlDefaultSink(); name != "" {
		if sinks, err := exec.Command("pactl", "list", "sinks").Output(); err == nil {
			out = outputKind(name, string(sinks))
		}
	}
	// Lento, al log: para ver si coincide con los tirones de la interfaz.
	if d := time.Since(start); d > 200*time.Millisecond {
		log.Printf("pactl took %v", d.Round(time.Millisecond))
	}
	if out == OutputBluetooth || out == OutputWired {
		return out
	}
	// ArkOS: sin PulseAudio; su script de audífonos cambia "Playback Path" de
	// ALSA a HP o SPK.
	if out, err := exec.Command("amixer", "-c", "0", "sget", "Playback Path").Output(); err == nil && fileExists(arkPause) {
		switch s := string(out); {
		case strings.Contains(s, "'HP'"):
			return OutputWired
		case strings.Contains(s, "'SPK'"):
			return OutputSpeaker
		}
	}
	return out
}

// pactlDefaultSink es la salida por defecto de PulseAudio (pactl info: es
// barato, no consulta las tarjetas), o "" si no hay pactl.
func pactlDefaultSink() string {
	info, err := exec.Command("pactl", "info").Output()
	if err != nil {
		return ""
	}
	return defaultSink(string(info))
}

// gpioDebug es la lista de pines del kernel, con su nombre y su estado.
const gpioDebug = "/sys/kernel/debug/gpio"

// jackFromGPIO busca en la lista de pines de debugfs el de detección de
// audífonos ("gpio-259 (  |Headphone detection ) in  hi") y dice si está alto
// (enchufados) y si lo encontró.
func jackFromGPIO(list string) (plugged, found bool) {
	for _, line := range strings.Split(list, "\n") {
		if !strings.Contains(strings.ToLower(line), "headphone detection") {
			continue
		}
		_, state, ok := strings.Cut(line, ")")
		if !ok {
			continue
		}
		f := strings.Fields(state)
		if len(f) >= 2 && f[0] == "in" {
			return f[1] == "hi", true
		}
	}
	return false, false
}

// defaultSink saca "Default Sink:" de pactl info (get-default-sink no existe en
// los pactl viejos).
func defaultSink(info string) string {
	for _, line := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Default Sink:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// outputKind clasifica la salida name a partir de pactl list sinks: bluetooth
// si es de bluez, audífonos de cable si el puerto activo (o el nombre de la
// salida, con UCM cada puerto es una salida) habla de audífonos y no está
// desconectado, y si no, el altavoz.
func outputKind(name, sinks string) string {
	if name == "" {
		return ""
	}
	var (
		in               bool
		bus, port, avail string
		inPorts          bool
	)
	sc := bufio.NewScanner(strings.NewReader(sinks))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "Name:"):
			in = strings.TrimSpace(strings.TrimPrefix(line, "Name:")) == name
			inPorts = false
		case !in:
		case strings.HasPrefix(line, "device.bus ="):
			bus = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "device.bus =")), `"`)
		case line == "Ports:":
			inPorts = true
		case strings.HasPrefix(line, "Active Port:"):
			port = strings.TrimSpace(strings.TrimPrefix(line, "Active Port:"))
			inPorts = false
		case inPorts:
			// Las líneas de los puertos: "nombre: descripción (..., available)".
			// Se guardan todas; se mira la del puerto activo al final.
			avail += line + "\n"
		}
	}
	if strings.HasPrefix(name, "bluez") || bus == "bluetooth" {
		return OutputBluetooth
	}
	l := strings.ToLower(name + " " + port)
	if strings.Contains(l, "headphone") || strings.Contains(l, "headset") {
		for _, p := range strings.Split(avail, "\n") {
			if port != "" && strings.HasPrefix(p, port+":") && strings.Contains(p, "not available") {
				return OutputSpeaker // el puerto existe, pero no hay nada enchufado
			}
		}
		return OutputWired
	}
	return OutputSpeaker
}
