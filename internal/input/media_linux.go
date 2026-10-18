package input

import (
	"encoding/binary"
	"io"
	"os"
	"slices"
	"time"

	"golang.org/x/sys/unix"
)

// Los botones multimedia de los audífonos: los de bluetooth (BlueZ crea un
// dispositivo "(AVRCP)" al conectarlos) y los de cable si el chip de audio de
// la consola los ve. Se leen sin acapararlos, como el mando.

// Teclas multimedia de linux/input-event-codes.h.
const (
	KeyStop         = 128
	KeyNextSong     = 163
	KeyPlayPause    = 164
	KeyPreviousSong = 165
	KeyStopCD       = 166
	KeyRewind       = 168
	KeyPlayCD       = 200
	KeyPauseCD      = 201
	KeyFastForward  = 208
	KeyMedia        = 226 // el botón único de los audífonos de cable
)

var mediaKeys = []uint16{KeyStop, KeyNextSong, KeyPlayPause, KeyPreviousSong, KeyStopCD, KeyRewind, KeyPlayCD, KeyPauseCD, KeyFastForward, KeyMedia}

// MediaDevices son los dispositivos con teclas multimedia que no son mandos.
func MediaDevices() []string {
	devs, err := listDevices()
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range devs {
		gamepad := slices.ContainsFunc(d.keys, func(k uint16) bool { return k >= 0x130 && k <= 0x13e })
		if gamepad {
			continue
		}
		if slices.ContainsFunc(mediaKeys, d.hasKey) {
			out = append(out, d.path)
		}
	}
	return out
}

// MediaReader lee las teclas multimedia de un dispositivo.
type MediaReader struct{ f *os.File }

// OpenMedia abre path.
func OpenMedia(path string) (*MediaReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &MediaReader{f}, nil
}

// Run llama a fn con cada tecla multimedia pulsada (no las sueltas ni las
// repeticiones), hasta que falle la lectura (se desconectó) o Close.
func (m *MediaReader) Run(fn func(key uint16, at time.Time)) {
	var buf [inputEventSize]byte
	for {
		if _, err := io.ReadFull(m.f, buf[:]); err != nil {
			return
		}
		typ := binary.LittleEndian.Uint16(buf[16:])
		code := binary.LittleEndian.Uint16(buf[18:])
		value := int32(binary.LittleEndian.Uint32(buf[20:]))
		if typ == evKey && value == 1 && slices.Contains(mediaKeys, code) {
			fn(code, time.Now())
		}
	}
}

// Close para Run.
func (m *MediaReader) Close() error { return m.f.Close() }

// HookCounter cuenta las pulsaciones seguidas del botón único de los
// audífonos de cable (como en Android: una pausa, dos pasa a la siguiente,
// tres a la anterior). Press devuelve cuántas van; tras gap sin otra, el
// llamador actúa con Done.
type HookCounter struct {
	gap   time.Duration
	n     int
	last  time.Time
	timer *time.Timer
}

// NewHookCounter prepara el contador con la separación máxima entre
// pulsaciones.
func NewHookCounter(gap time.Duration) *HookCounter { return &HookCounter{gap: gap} }

// Press apunta una pulsación y llama a done(n) cuando pasa gap sin otra.
func (h *HookCounter) Press(at time.Time, done func(n int)) {
	if at.Sub(h.last) > h.gap {
		h.n = 0
	}
	h.n++
	h.last = at
	if h.timer != nil {
		h.timer.Stop()
	}
	n := h.n
	h.timer = time.AfterFunc(h.gap, func() {
		if h.n == n {
			h.n = 0
			done(n)
		}
	})
}

// WaitNewDevices bloquea hasta que aparezca un dispositivo nuevo en
// /dev/input (con inotify: sin despertar la CPU mientras no pase nada) y
// llama a fn cada vez, hasta que done se cierre. Si inotify falla, no avisa
// de nada.
func WaitNewDevices(done <-chan struct{}, fn func()) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return
	}
	if _, err := unix.InotifyAddWatch(fd, "/dev/input", unix.IN_CREATE); err != nil {
		unix.Close(fd)
		return
	}
	go func() {
		<-done
		unix.Close(fd) // desbloquea el Read de abajo
	}()
	buf := make([]byte, 4096)
	for {
		if _, err := unix.Read(fd, buf); err != nil {
			return
		}
		// Se da un momento a udev para los permisos y las capacidades.
		time.Sleep(500 * time.Millisecond)
		fn()
	}
}
