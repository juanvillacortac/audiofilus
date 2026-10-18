// Package input lee el mando y lo convierte en botones lógicos (L1, R1,
// Start...), para los gestos del sleep y los atajos de segundo plano.
//
// Qué código de evdev es cada botón depende de la consola: en la RG34XX-SP, R2
// manda el código que el estándar reserva para Start. Por eso el mapeo sale de
// SDL (paquete sdlmap), igual que en los juegos de PortMaster, y solo si SDL no
// está se usan los códigos estándar.
package input

import (
	"sort"
	"strconv"
	"strings"
)

// Button es un botón lógico, con el nombre que le da SDL a los gamepads.
type Button int

const (
	None   Button = iota
	L1            // leftshoulder
	R1            // rightshoulder
	L2            // lefttrigger
	R2            // righttrigger
	Start         // start
	Select        // back
	FN            // guide: la tecla de función o menú
)

func (b Button) String() string {
	return [...]string{"none", "L1", "R1", "L2", "R2", "Start", "Select", "FN"}[b]
}

// sdlNames son los nombres de los botones en una cadena de mapeo de SDL.
var sdlNames = map[string]Button{
	"leftshoulder":  L1,
	"rightshoulder": R1,
	"lefttrigger":   L2,
	"righttrigger":  R2,
	"start":         Start,
	"back":          Select,
	"guide":         FN,
}

// Códigos de evdev (linux/input-event-codes.h) que usa la numeración de SDL.
const (
	btnJoystick = 0x120
	keyMax      = 0x2ff
	absHat0X    = 0x10
	absHat3Y    = 0x17
	absMax      = 0x3f
)

// Standard es el mapeo cuando SDL no está: los códigos estándar de Linux para
// gamepads (BTN_TL, BTN_TR...). No vale para la RG34XX-SP.
var Standard = Layout{
	Keys: map[uint16]Button{
		0x136: L1, 0x137: R1, 0x138: L2, 0x139: R2,
		0x13b: Start, 0x13a: Select, 0x13c: FN,
	},
}

// Layout traduce códigos de evdev a botones lógicos.
type Layout struct {
	Keys map[uint16]Button // EV_KEY
	Axes map[uint16]Button // EV_ABS: gatillos analógicos (L2/R2 de muchos mandos de PC)
}

// Describe lista el mapeo, para mostrarlo en la CLI y en el log.
func (l Layout) Describe() string {
	var parts []string
	for code, b := range l.Keys {
		parts = append(parts, b.String()+"=key:0x"+strconv.FormatUint(uint64(code), 16))
	}
	for code, b := range l.Axes {
		parts = append(parts, b.String()+"=abs:0x"+strconv.FormatUint(uint64(code), 16))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// FromSDL construye el mapeo a partir de una cadena de mapeo de SDL y de los
// códigos que tiene el dispositivo. SDL (2.32 en Knulli) numera los botones por
// orden de código, de menor a mayor, contando todas las teclas del dispositivo;
// los ejes, en orden saltando los hats. "bN" es el botón N y "aN" el eje N.
//
// Comprobado en la RG34XX-SP: con sus teclas (1, 114, 115, 0x130-0x13c, 354)
// el mapeo de Knulli dice leftshoulder:b7, que es 0x134, el código que manda L1.
func FromSDL(mapping string, keys, abs []uint16) Layout {
	var buttons []uint16
	for _, c := range sorted(keys) {
		if c < keyMax {
			buttons = append(buttons, c)
		}
	}
	var axes []uint16
	for _, c := range sorted(abs) {
		if c >= absHat0X && c <= absHat3Y || c >= absMax {
			continue
		}
		axes = append(axes, c)
	}

	l := Layout{Keys: map[uint16]Button{}, Axes: map[uint16]Button{}}
	for _, field := range strings.Split(mapping, ",") {
		name, value, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}
		b, known := sdlNames[name]
		// "+a2", "-a2" y "a2~" son medios ejes o ejes invertidos; aquí basta el índice.
		value = strings.TrimRight(strings.TrimLeft(value, "+-"), "~")
		if !known || len(value) < 2 {
			continue
		}
		idx, err := strconv.Atoi(value[1:])
		if err != nil || idx < 0 {
			continue
		}
		switch value[0] {
		case 'b':
			if idx < len(buttons) {
				l.Keys[buttons[idx]] = b
			}
		case 'a':
			if idx < len(axes) {
				l.Axes[axes[idx]] = b
			}
		}
	}
	return l
}

func sorted(s []uint16) []uint16 {
	out := append([]uint16(nil), s...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
