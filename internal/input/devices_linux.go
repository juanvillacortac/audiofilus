package input

import (
	"bufio"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/sdlmap"
)

// device es un dispositivo de entrada según /proc/bus/input/devices.
type device struct {
	name string
	path string // /dev/input/eventN
	keys []uint16
	abs  []uint16
}

// listDevices lee /proc/bus/input/devices.
func listDevices() ([]device, error) {
	f, err := os.Open("/proc/bus/input/devices")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []device
	var cur device
	flush := func() {
		if cur.path != "" {
			out = append(out, cur)
		}
		cur = device{}
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "N: Name="):
			cur.name = strings.Trim(strings.TrimPrefix(line, "N: Name="), `"`)
		case strings.HasPrefix(line, "H: Handlers="):
			for _, h := range strings.Fields(strings.TrimPrefix(line, "H: Handlers=")) {
				if strings.HasPrefix(h, "event") {
					cur.path = "/dev/input/" + h
				}
			}
		case strings.HasPrefix(line, "B: KEY="):
			cur.keys = bits(strings.Fields(strings.TrimPrefix(line, "B: KEY=")))
		case strings.HasPrefix(line, "B: ABS="):
			cur.abs = bits(strings.Fields(strings.TrimPrefix(line, "B: ABS=")))
		}
	}
	flush()
	return out, sc.Err()
}

func (d device) hasKey(code uint16) bool {
	for _, k := range d.keys {
		if k == code {
			return true
		}
	}
	return false
}

// Resolved es el mando elegido y cómo se lee.
type Resolved struct {
	Name   string
	Path   string
	Layout Layout
	Source string // "sdl" o "standard"
}

// Resolve elige el mando y su mapeo. Si path no está vacío, se usa ese
// dispositivo. El mapeo sale de SDL si está disponible y conoce el mando; si no,
// se usan los códigos estándar.
func Resolve(path string) (Resolved, error) {
	devs, err := listDevices()
	if err != nil {
		return Resolved{}, err
	}
	if ctrls, err := sdlmap.Controllers(); err != nil {
		log.Printf("SDL unavailable for the gamepad mapping: %v", err)
	} else {
		for _, c := range ctrls {
			d, ok := match(devs, c, path)
			if !ok {
				continue
			}
			return Resolved{Name: d.name, Path: d.path, Layout: FromSDL(c.Mapping, d.keys, d.abs), Source: "sdl"}, nil
		}
	}
	for _, d := range devs {
		if (path == "" && d.hasKey(btnJoystick+0x10)) || d.path == path { // BTN_SOUTH: es un gamepad
			return Resolved{Name: d.name, Path: d.path, Layout: Standard, Source: "standard"}, nil
		}
	}
	return Resolved{}, errors.New("no gamepad found")
}

// match busca el dispositivo de evdev de un mando de SDL: por su ruta (SDL 2.24
// o posterior) o, si no, por el nombre.
func match(devs []device, c sdlmap.Controller, want string) (device, bool) {
	for _, d := range devs {
		if want != "" && d.path != want {
			continue
		}
		if c.Path != "" && sameFile(c.Path, d.path) {
			return d, true
		}
		if c.Path == "" && d.name == c.Name {
			return d, true
		}
	}
	return device{}, false
}

func sameFile(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
