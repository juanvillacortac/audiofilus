package platform

// Output es una salida de audio que se puede elegir (Ajustes > Salida de
// audio): una de PipeWire o PulseAudio, o una tarjeta de ALSA.
type Output struct {
	// ID la identifica: el nombre de la salida en PipeWire/PulseAudio, o
	// "alsa:<id de la tarjeta>" en ALSA. OutputBuiltin es la de siempre.
	ID string `json:"id"`
	// Name es el que se muestra (el del sistema); vacío en OutputBuiltin,
	// que la interfaz nombra en su idioma.
	Name string `json:"name,omitempty"`
	// USB: es un DAC (o unos audífonos) por USB, a la que se cambia sola al
	// conectarla.
	USB bool `json:"usb,omitempty"`
	// HDMI: la salida a la tele (en Knulli se llama igual que la de la
	// consola, "Built-in Audio Stereo"; la interfaz la nombra "HDMI").
	HDMI bool `json:"hdmi,omitempty"`

	card string // el nombre de la tarjeta de ALSA, para distinguir nombres repetidos
}

// OutputBuiltin es la salida de la consola en ALSA: "default", la que abre
// el sistema (el altavoz o el jack).
const OutputBuiltin = "alsa:"

// Outputs elige la salida de audio. Hay dos maneras:
//
//   - PipeWire o PulseAudio (Knulli, muOS, ROCKNIX, el PC con Linux): se
//     cambia la salida por defecto del servidor y la música se mueve sola,
//     sin cortes.
//   - ALSA directo (ArkOS, TrimUI, EmuELEC): oto abre una tarjeta al
//     arrancar, así que cambiar es reiniciar el servicio con otra
//     (OTO_ALSA_DEVICE; Restart dice true).
//
// En Windows, macOS, Android e iOS el sistema ya cambia solo al conectar un
// DAC y oto lo sigue: OpenOutputs devuelve nil y no hay nada que elegir.
type Outputs interface {
	// List son las salidas que hay ahora.
	List() []Output
	// Current es el ID de la que suena.
	Current() string
	// Set cambia a la salida id. Con Restart, solo la apunta para el
	// siguiente arranque del servicio.
	Set(id string) error
	// Restart dice si cambiar de salida pide reiniciar el servicio.
	Restart() bool
	// Changed avisa cuando aparece o desaparece una salida, o cambia la
	// que suena desde fuera.
	Changed() <-chan struct{}
	// Close suelta la conexión.
	Close()
}
