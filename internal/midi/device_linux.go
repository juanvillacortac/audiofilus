package midi

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dónde mirar (los tests las cambian por carpetas de prueba).
var (
	devDir  = "/dev/snd"
	procDir = "/proc/asound"
)

// List son los puertos MIDI crudos de ALSA (/dev/snd/midiC<tarjeta>D<puerto>).
func List() ([]Device, error) {
	paths, err := filepath.Glob(filepath.Join(devDir, "midiC*D*"))
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, p := range paths {
		var card, dev int
		if _, err := fmt.Sscanf(filepath.Base(p), "midiC%dD%d", &card, &dev); err != nil {
			continue
		}
		out = append(out, Device{Path: p, Name: deviceName(card, dev), Card: card, Dev: dev})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Card != out[j].Card {
			return out[i].Card < out[j].Card
		}
		return out[i].Dev < out[j].Dev
	})
	return out, nil
}

// deviceName es el nombre del aparato: la primera línea de
// /proc/asound/card<N>/midi<D> ("Keystation 49"), si no el id de la tarjeta,
// si no "MIDI <N>".
func deviceName(card, dev int) string {
	if f, err := os.Open(filepath.Join(procDir, fmt.Sprintf("card%d", card), fmt.Sprintf("midi%d", dev))); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			if s := strings.TrimSpace(sc.Text()); s != "" {
				return s
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(procDir, fmt.Sprintf("card%d", card), "id")); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return fmt.Sprintf("MIDI %d", card)
}

// Open abre un puerto para leer y escribir (si no se puede escribir, solo
// para leer) y empieza a pasar sus mensajes a handle, desde la goroutine que
// lee: handle tiene que volver rápido.
func Open(d Device, handle func(Event)) (*Port, error) {
	f, err := os.OpenFile(d.Path, os.O_RDWR, 0)
	canWrite := err == nil
	if err != nil {
		f, err = os.OpenFile(d.Path, os.O_RDONLY, 0)
	}
	if err != nil {
		return nil, err
	}
	return newPort(d, f, canWrite, handle), nil
}
