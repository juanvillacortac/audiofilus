package input

import (
	"encoding/binary"
	"io"
	"os"
	"slices"
	"time"
)

// keyPower es KEY_POWER de linux/input-event-codes.h.
const keyPower = 116

// PowerButtons son los dispositivos con la tecla de encendido que no son
// mandos (en las Anbernic con Knulli, el del chip de carga: "axp2202-pek").
func PowerButtons() []string {
	devs, err := listDevices()
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range devs {
		if d.hasKey(keyPower) && !slices.ContainsFunc(d.keys, func(k uint16) bool { return k >= 0x130 && k <= 0x13e }) {
			out = append(out, d.path)
		}
	}
	return out
}

// PowerWatcher lee la tecla de encendido sin acapararla: Knulli sigue
// recibiéndola (mantenerla sigue apagando).
type PowerWatcher struct{ f *os.File }

// WatchPower abre path para PowerWatcher.Run.
func WatchPower(path string) (*PowerWatcher, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &PowerWatcher{f}, nil
}

// Run llama a fn al soltar la tecla, con lo que estuvo pulsada, hasta que la
// lectura falle (o se llame a Close). Bloquea: sin pulsaciones no gasta CPU.
func (p *PowerWatcher) Run(fn func(held time.Duration)) {
	var buf [inputEventSize]byte
	var down time.Time
	for {
		if _, err := io.ReadFull(p.f, buf[:]); err != nil {
			return
		}
		typ := binary.LittleEndian.Uint16(buf[16:])
		code := binary.LittleEndian.Uint16(buf[18:])
		value := int32(binary.LittleEndian.Uint32(buf[20:]))
		if typ != evKey || code != keyPower {
			continue
		}
		switch value {
		case 1:
			down = time.Now()
		case 0:
			if !down.IsZero() {
				fn(time.Since(down))
				down = time.Time{}
			}
		}
	}
}

// Close para Run.
func (p *PowerWatcher) Close() error { return p.f.Close() }
