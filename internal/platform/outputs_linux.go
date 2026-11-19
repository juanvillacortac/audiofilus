//go:build linux && !android

package platform

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jfreymuth/pulse/proto"
)

// OpenOutputs abre la elección de salida: por PipeWire/PulseAudio si hay
// servidor, si no por ALSA. Se llama con el engine ya abierto (que apunta
// PULSE_SERVER al socket del sistema). nil si no hay ninguno.
func OpenOutputs() Outputs {
	if o, err := openPulseOutputs(); err == nil {
		return o
	} else if pulseSocket() {
		log.Printf("audio outputs: %v", err)
		return nil // hay servidor pero no responde: mejor no tocar ALSA
	}
	if _, err := os.Stat(asoundCards); err == nil {
		return newALSAOutputs()
	}
	return nil
}

// pulseSocket dice si hay un socket de PulseAudio (o de pipewire-pulse) a la
// vista, como los que busca el engine: entonces oto usa ese y no ALSA.
func pulseSocket() bool {
	if os.Getenv("PULSE_SERVER") != "" {
		return true
	}
	paths := []string{"/var/run/pulse/native", "/run/pulse/native"}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		paths = append(paths, dir+"/pulse/native")
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// PickALSAOutput elige, antes de abrir el audio, la tarjeta que abrirá oto
// cuando no hay PipeWire/PulseAudio: pref si está conectada, y si pref está
// vacío (automático), el primer DAC USB. La deja en OTO_ALSA_DEVICE (que se
// hereda al reiniciar el servicio, pero se vuelve a decidir aquí). Devuelve
// la elegida (OutputBuiltin si es la de siempre, "" si no es ALSA).
func PickALSAOutput(pref string) string {
	if pulseSocket() {
		return ""
	}
	b, err := os.ReadFile(asoundCards)
	if err != nil {
		return ""
	}
	pick := OutputBuiltin
	for _, o := range parseALSACards(string(b)) {
		if o.ID == pref || (pref == "" && o.USB && pick == OutputBuiltin) {
			pick = o.ID
		}
	}
	if pick == OutputBuiltin {
		os.Unsetenv(alsaDeviceEnv)
	} else {
		os.Setenv(alsaDeviceEnv, alsaPCM(pick))
	}
	return pick
}

// pulseOutputs elige la salida por defecto de PipeWire/PulseAudio con su
// protocolo (no con pactl: nada de lanzar procesos), y se suscribe a los
// cambios de salidas: en reposo no cuesta nada.
type pulseOutputs struct {
	c    *proto.Client
	conn net.Conn
	ch   chan struct{}
	poke chan struct{}

	mu   sync.Mutex
	list []Output
	cur  string
}

// openPulseOutputs se conecta al servidor y lee las salidas.
func openPulseOutputs() (*pulseOutputs, error) {
	c, conn, err := proto.Connect("")
	if err != nil {
		return nil, err
	}
	c.SetTimeout(3 * time.Second)
	props := proto.PropList{
		"application.name":       proto.PropListString("Audiofilus"),
		"application.process.id": proto.PropListString(strconv.Itoa(os.Getpid())),
	}
	if err := c.Request(&proto.SetClientName{Props: props}, &proto.SetClientNameReply{}); err != nil {
		conn.Close()
		return nil, err
	}
	o := &pulseOutputs{c: c, conn: conn, ch: make(chan struct{}, 1), poke: make(chan struct{}, 1)}
	// Las respuestas llegan por el mismo hilo que los eventos: dentro del
	// callback no se puede pedir nada, solo avisar.
	c.Callback = func(msg any) {
		switch m := msg.(type) {
		case *proto.SubscribeEvent:
			f, t := m.Event.GetFacility(), m.Event.GetType()
			// Las salidas que van y vienen, y el servidor (cambió la
			// salida por defecto). Los cambios de una salida (su volumen)
			// no: son muchos, y listarlas consulta sus controles de ALSA,
			// que en las H700 con HDMI paran la imagen.
			if (f == proto.EventSink && t != proto.EventChange) || f == proto.EventServer {
				signal(o.poke)
			}
		case *proto.ConnectionClosed:
			close(o.poke)
		}
	}
	if err := o.refresh(); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.Request(&proto.Subscribe{Mask: proto.SubscriptionMaskSink | proto.SubscriptionMaskServer}, nil); err != nil {
		conn.Close()
		return nil, err
	}
	go o.watch()
	return o, nil
}

// signal avisa por ch sin bloquear (ch tiene hueco para uno: los avisos se
// juntan).
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// watch vuelve a leer las salidas tras cada aviso del servidor, juntando los
// que llegan seguidos (al conectar un DAC llegan varios).
func (o *pulseOutputs) watch() {
	for range o.poke {
		time.Sleep(300 * time.Millisecond)
		select {
		case <-o.poke:
		default:
		}
		o.mu.Lock()
		n := len(o.list)
		o.mu.Unlock()
		if err := o.refresh(); err != nil {
			log.Printf("audio outputs: %v", err)
			continue
		}
		o.mu.Lock()
		if len(o.list) != n {
			log.Printf("audio outputs: %d available, playing on %q", len(o.list), o.cur)
		}
		o.mu.Unlock()
		signal(o.ch)
	}
}

// refresh lee la salida por defecto y la lista de salidas.
func (o *pulseOutputs) refresh() error {
	var info proto.GetServerInfoReply
	if err := o.c.Request(&proto.GetServerInfo{}, &info); err != nil {
		return err
	}
	var sinks proto.GetSinkInfoListReply
	if err := o.c.Request(&proto.GetSinkInfoList{}, &sinks); err != nil {
		return err
	}
	list := make([]Output, 0, len(sinks))
	for _, s := range sinks {
		if s.SinkName == "auto_null" {
			continue
		}
		prop := func(k string) string {
			if v, ok := s.Properties[k]; ok {
				return v.String()
			}
			return ""
		}
		card := prop("alsa.card_name")
		hdmi := strings.Contains(strings.ToLower(s.SinkName+" "+card+" "+s.Device), "hdmi")
		list = append(list, Output{ID: s.SinkName, Name: s.Device, USB: prop("device.bus") == "usb", HDMI: hdmi, card: card})
	}
	nameDuplicates(list)
	o.mu.Lock()
	o.list, o.cur = list, info.DefaultSinkName
	o.mu.Unlock()
	return nil
}

// nameDuplicates añade el nombre de la tarjeta a las salidas que se llaman
// igual que otra (en Knulli, la de la consola y la del HDMI son las dos
// "Built-in Audio Stereo").
func nameDuplicates(list []Output) {
	seen := map[string]int{}
	for _, o := range list {
		seen[o.Name]++
	}
	for i, o := range list {
		if seen[o.Name] > 1 && o.card != "" {
			list[i].Name = o.Name + " (" + o.card + ")"
		}
	}
}

func (o *pulseOutputs) List() []Output {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Output(nil), o.list...)
}

func (o *pulseOutputs) Current() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cur
}

// Set la pone como salida por defecto y mueve a ella la música de este
// proceso: con PipeWire se mueve sola, pero PulseAudio no mueve lo que ya
// se movió a mano alguna vez.
func (o *pulseOutputs) Set(id string) error {
	if err := o.c.Request(&proto.SetDefaultSink{SinkName: id}, nil); err != nil {
		return fmt.Errorf("set default sink %s: %w", id, err)
	}
	var inputs proto.GetSinkInputInfoListReply
	if err := o.c.Request(&proto.GetSinkInputInfoList{}, &inputs); err == nil {
		pid := strconv.Itoa(os.Getpid())
		for _, in := range inputs {
			if p, ok := in.Properties["application.process.id"]; ok && p.String() == pid {
				_ = o.c.Request(&proto.MoveSinkInput{SinkInputIndex: in.SinkInputIndex, DeviceIndex: proto.Undefined, DeviceName: id}, nil)
			}
		}
	}
	o.mu.Lock()
	o.cur = id
	o.mu.Unlock()
	return nil
}

func (o *pulseOutputs) Restart() bool            { return false }
func (o *pulseOutputs) Changed() <-chan struct{} { return o.ch }
func (o *pulseOutputs) Close()                   { o.conn.Close() }

// asoundCards es la lista de tarjetas de sonido de ALSA.
const asoundCards = "/proc/asound/cards"

// alsaDeviceEnv es la variable con la que el fork de oto abre una tarjeta
// en vez de "default".
const alsaDeviceEnv = "OTO_ALSA_DEVICE"

// alsaEvery es cada cuánto se mira si se conectó o desconectó una tarjeta
// (leer un archivo de /proc: casi nada).
const alsaEvery = 2 * time.Second

// alsaOutputs elige la tarjeta de ALSA: la de la consola (default, con lo
// que el sistema haya configurado) o un DAC USB.
type alsaOutputs struct {
	ch chan struct{}

	mu   sync.Mutex
	list []Output
	stop chan struct{}
}

// newALSAOutputs lee las tarjetas y las vigila.
func newALSAOutputs() *alsaOutputs {
	o := &alsaOutputs{ch: make(chan struct{}, 1), stop: make(chan struct{})}
	b, _ := os.ReadFile(asoundCards)
	o.list = alsaOutputList(string(b))
	go o.watch(b)
	return o
}

// watch avisa cuando cambia la lista de tarjetas.
func (o *alsaOutputs) watch(last []byte) {
	t := time.NewTicker(alsaEvery)
	defer t.Stop()
	for {
		select {
		case <-o.stop:
			return
		case <-t.C:
		}
		b, err := os.ReadFile(asoundCards)
		if err != nil || bytes.Equal(b, last) {
			continue
		}
		last = b
		o.mu.Lock()
		o.list = alsaOutputList(string(b))
		log.Printf("audio outputs: ALSA cards changed (%d)", len(o.list))
		o.mu.Unlock()
		signal(o.ch)
	}
}

// alsaOutputList es la de la consola más los DAC USB.
func alsaOutputList(cards string) []Output {
	return append([]Output{{ID: OutputBuiltin}}, parseALSACards(cards)...)
}

func (o *alsaOutputs) List() []Output {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Output(nil), o.list...)
}

// Current es la que se abrió al arrancar (OTO_ALSA_DEVICE).
func (o *alsaOutputs) Current() string {
	dev := os.Getenv(alsaDeviceEnv)
	if id, ok := strings.CutPrefix(dev, "plughw:CARD="); ok {
		return OutputBuiltin + id
	}
	return OutputBuiltin
}

// Set la apunta para el próximo arranque: quien llama reinicia el servicio.
func (o *alsaOutputs) Set(id string) error {
	if id == OutputBuiltin {
		return os.Unsetenv(alsaDeviceEnv)
	}
	return os.Setenv(alsaDeviceEnv, alsaPCM(id))
}

func (o *alsaOutputs) Restart() bool            { return true }
func (o *alsaOutputs) Changed() <-chan struct{} { return o.ch }
func (o *alsaOutputs) Close()                   { close(o.stop) }

// alsaPCM es el dispositivo de ALSA de una salida: plughw convierte el
// formato y la frecuencia a lo que acepte el DAC.
func alsaPCM(id string) string {
	return "plughw:CARD=" + strings.TrimPrefix(id, OutputBuiltin)
}

// alsaCardLine es la primera línea de cada tarjeta en /proc/asound/cards:
// " 1 [Audio          ]: USB-Audio - USB Audio Device".
var alsaCardLine = regexp.MustCompile(`^\s*\d+\s+\[(\S+)\s*\]:\s+(\S+)\s+-\s+(.*)$`)

// parseALSACards saca de /proc/asound/cards los DAC USB (el driver
// USB-Audio). Las demás tarjetas (la de la consola, el HDMI) se dejan al
// sistema: "default" ya es la que él eligió.
func parseALSACards(cards string) []Output {
	var out []Output
	for _, line := range strings.Split(cards, "\n") {
		m := alsaCardLine.FindStringSubmatch(line)
		if m == nil || m[2] != "USB-Audio" {
			continue
		}
		out = append(out, Output{ID: OutputBuiltin + m[1], Name: strings.TrimSpace(m[3]), USB: true})
	}
	return out
}
