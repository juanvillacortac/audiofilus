package ui

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Action es lo que pide el usuario, venga del teclado o del mando.
type Action int

const (
	ActUp Action = iota
	ActDown
	ActLeft
	ActRight
	ActConfirm // A / Enter
	ActBack    // B / Retroceso o Esc
	ActShuffle // X / S
	ActMenu    // Start / M: el menú "más" de cada pantalla
	ActPause   // Y / Espacio: pausa o sigue
	ActPrev    // L1 / Q
	ActNext    // R1 / E
	ActSeekBack
	ActSeekFwd
	ActSelect // Select / Tab: cambia de pantalla
	ActVolUp  // + : sube el volumen de audiofilus
	ActVolDown
	ActGuide // FN / F1: mientras se mantiene, la guía de controles
	actCount
)

// Teclas y botones de cada acción. Los botones son los del mando estándar de
// Ebitengine, que en Linux sale del mapeo de SDL: el mismo que reciben los
// juegos de PortMaster, así que A es la A física en cada consola.
var (
	actionKeys = [actCount][]ebiten.Key{
		ActUp:       {ebiten.KeyArrowUp},
		ActDown:     {ebiten.KeyArrowDown},
		ActLeft:     {ebiten.KeyArrowLeft},
		ActRight:    {ebiten.KeyArrowRight},
		ActConfirm:  {ebiten.KeyEnter, ebiten.KeyZ},
		ActBack:     {ebiten.KeyBackspace, ebiten.KeyEscape},
		ActShuffle:  {ebiten.KeyS},
		ActMenu:     {ebiten.KeyM},
		ActPause:    {ebiten.KeySpace},
		ActPrev:     {ebiten.KeyQ},
		ActNext:     {ebiten.KeyE},
		ActSeekBack: {ebiten.KeyComma},
		ActSeekFwd:  {ebiten.KeyPeriod},
		ActSelect:   {ebiten.KeyTab},
		ActVolUp:    {ebiten.KeyEqual, ebiten.KeyNumpadAdd},
		ActVolDown:  {ebiten.KeyMinus, ebiten.KeyNumpadSubtract},
		ActGuide:    {ebiten.KeyF1},
	}
	actionButtons = [actCount][]ebiten.StandardGamepadButton{
		ActUp:       {ebiten.StandardGamepadButtonLeftTop},
		ActDown:     {ebiten.StandardGamepadButtonLeftBottom},
		ActLeft:     {ebiten.StandardGamepadButtonLeftLeft},
		ActRight:    {ebiten.StandardGamepadButtonLeftRight},
		ActConfirm:  {ebiten.StandardGamepadButtonRightBottom},
		ActBack:     {ebiten.StandardGamepadButtonRightRight},
		ActShuffle:  {ebiten.StandardGamepadButtonRightLeft},
		ActMenu:     {ebiten.StandardGamepadButtonCenterRight},
		ActPause:    {ebiten.StandardGamepadButtonRightTop},
		ActPrev:     {ebiten.StandardGamepadButtonFrontTopLeft},
		ActNext:     {ebiten.StandardGamepadButtonFrontTopRight},
		ActSeekBack: {ebiten.StandardGamepadButtonFrontBottomLeft},
		ActSeekFwd:  {ebiten.StandardGamepadButtonFrontBottomRight},
		ActSelect:   {ebiten.StandardGamepadButtonCenterLeft},
		ActGuide:    {ebiten.StandardGamepadButtonCenterCenter},
	}
)

// Repetición al mantener pulsada una dirección, en frames (a 60 por segundo).
const (
	repeatDelay = 20
	repeatEvery = 5
)

// Input lee las acciones de un frame.
type Input struct {
	pads []ebiten.GamepadID // solo los que tienen mapeo estándar (mandos de verdad)
	all  []ebiten.GamepadID
	keys []ebiten.Key
	// usingPad dice si lo último que se usó fue un mando, para mostrar sus
	// botones en la ayuda; si fue el teclado, se muestran las teclas.
	usingPad bool
	// padsSeen son los mandos que había el frame anterior: si hay más, se
	// acaba de conectar uno.
	padsSeen int
	decided  bool
	// any dice si en este frame se pulsó cualquier tecla o botón.
	any bool
	// swapAB intercambia A con B y X con Y: la distribución de Nintendo (A a
	// la derecha, X arriba) en vez de la de Xbox, para que las letras de la
	// ayuda sean las impresas en la consola.
	swapAB bool
	// stick cuenta cuántos frames lleva el joystick izquierdo empujado hacia
	// cada dirección, igual que una cruceta: así vale en toda la app.
	stick [4]int
	// touch: lo último que se usó fue el dedo (o es el móvil): la ayuda sale
	// como botones, sin teclas ni botones del mando.
	touch bool
	// rot es cómo está girada la interfaz (config.UI.Orientation): 0
	// horizontal, rotLeft o rotRight. La cruceta, el stick izquierdo y
	// A/B/X/Y se leen por la posición en que quedan en la mano, así arriba en
	// la pantalla es arriba en la cruceta y el botón de abajo sigue siendo el
	// de aceptar.
	rot int
	// virt son las acciones de este fotograma que vienen del ratón o de un
	// dedo (pointer.go): cuentan como recién pulsadas.
	virt [actCount]bool
}

// inject pulsa la acción a en este fotograma (un toque, un gesto).
func (in *Input) inject(a Action) {
	in.virt[a] = true
	in.any = true
}

// mouseUsed dice que lo último que se usó fue el ratón (la ayuda muestra
// las teclas, no los botones del mando).
func (in *Input) mouseUsed() { in.usingPad, in.decided = false, true }

// Umbrales del joystick izquierdo (cuánto se empuja, de 0 a 1): se activa al
// pasar stickOn (el de Trancazo: hay sticks que hacia algún lado no llegan ni
// a la mitad) y se suelta al bajar de stickOff, para que no tiemble en el
// borde.
const (
	stickOn  = 0.35
	stickOff = 0.25
)

// stickDirs son las acciones de cada dirección del joystick, en el orden de
// stick.
var stickDirs = [4]Action{ActLeft, ActRight, ActUp, ActDown}

// updateStick lee el joystick izquierdo de cada mando por separado (como
// Trancazo): pasado stickOn, se ajusta a una de ocho direcciones, igual que
// la cruceta, y vale hasta que vuelve a menos de stickOff. Las direcciones
// de todos los mandos se suman.
func (in *Input) updateStick() {
	var dirs [4]bool
	for _, id := range in.pads {
		x := ebiten.StandardGamepadAxisValue(id, ebiten.StandardGamepadAxisLeftStickHorizontal)
		y := ebiten.StandardGamepadAxisValue(id, ebiten.StandardGamepadAxisLeftStickVertical)
		// Girada, la derecha en la pantalla es abajo en el stick (a la
		// izquierda) o arriba (a la derecha).
		switch in.rot {
		case rotLeft:
			x, y = y, -x
		case rotRight:
			x, y = -y, x
		}
		held := in.stick != [4]int{}
		if r := math.Hypot(x, y); r < stickOff || (!held && r < stickOn) {
			continue
		}
		an := math.Round(math.Atan2(y, x)/(math.Pi/4)) * (math.Pi / 4)
		dx, dy := math.Round(math.Cos(an)), math.Round(math.Sin(an))
		dirs[0] = dirs[0] || dx < 0
		dirs[1] = dirs[1] || dx > 0
		dirs[2] = dirs[2] || dy < 0
		dirs[3] = dirs[3] || dy > 0
	}
	for i, on := range dirs {
		if !on {
			in.stick[i] = 0
			continue
		}
		in.stick[i]++
		if in.stick[i] == 1 {
			in.usingPad, in.decided, in.any = true, true, true
		}
	}
}

// update se llama una vez por frame, antes de preguntar.
func (in *Input) update() {
	// Algunos receptores de ratón o teclado se anuncian como joystick: sin un
	// mapeo estándar no se cuentan como mando.
	in.all = ebiten.AppendGamepadIDs(in.all[:0])
	in.pads = in.pads[:0]
	for _, id := range in.all {
		if ebiten.IsStandardGamepadLayoutAvailable(id) && !notAPad(id, in.all) {
			in.pads = append(in.pads, id)
		}
	}
	if !in.decided {
		in.usingPad = len(in.pads) > 0
	}
	// Al aparecer un mando (al abrir, o al conectarlo después) la interfaz
	// pasa a la del mando también en el móvil, que empieza con el dedo; tocar
	// la pantalla la devuelve a la táctil (pointerDown).
	if len(in.pads) > in.padsSeen {
		in.usingPad, in.touch = true, false
	}
	in.padsSeen = len(in.pads)
	in.any = false
	in.virt = [actCount]bool{}
	if in.keys = inpututil.AppendJustPressedKeys(in.keys[:0]); len(in.keys) > 0 {
		in.usingPad, in.decided, in.any, in.touch = false, true, true, false
	}
	for _, id := range in.pads {
		for b := ebiten.StandardGamepadButton(0); b <= ebiten.StandardGamepadButtonMax; b++ {
			if inpututil.IsStandardGamepadButtonJustPressed(id, b) {
				in.usingPad, in.decided, in.any, in.touch = true, true, true, false
			}
		}
	}
	in.updateStick()
}

// Cómo está girada la interfaz (Input.rot).
const (
	rotLeft  = 1 // la consola, girada a la izquierda (la cruceta abajo)
	rotRight = 3 // girada a la derecha (la cruceta arriba)
)

// rotation es el Input.rot de config.UI.Orientation.
func rotation(orientation string) int {
	switch orientation {
	case "left":
		return rotLeft
	case "right":
		return rotRight
	}
	return 0
}

// Los cuatro botones de la cruceta y los de la derecha, en el orden en que
// los recorre un giro a la derecha: arriba, derecha, abajo, izquierda.
var (
	dpadRing = [4]ebiten.StandardGamepadButton{ebiten.StandardGamepadButtonLeftTop, ebiten.StandardGamepadButtonLeftRight,
		ebiten.StandardGamepadButtonLeftBottom, ebiten.StandardGamepadButtonLeftLeft}
	faceRing = [4]ebiten.StandardGamepadButton{ebiten.StandardGamepadButtonRightTop, ebiten.StandardGamepadButtonRightRight,
		ebiten.StandardGamepadButtonRightBottom, ebiten.StandardGamepadButtonRightLeft}
)

// physical es el botón de la consola que, con la interfaz girada, queda donde
// b en la mano: con la consola girada a la izquierda, el de la derecha queda
// arriba, así que arriba se lee del de la derecha.
func (in *Input) physical(b ebiten.StandardGamepadButton) ebiten.StandardGamepadButton {
	if in.rot == 0 {
		return b
	}
	for _, ring := range [][4]ebiten.StandardGamepadButton{dpadRing, faceRing} {
		for i, r := range ring {
			if r == b {
				return ring[(i+in.rot)%4]
			}
		}
	}
	return b
}

// buttons son los botones de una acción, con A/B y X/Y intercambiados si se
// pidió.
func (in *Input) buttons(a Action) []ebiten.StandardGamepadButton {
	if in.swapAB {
		switch a {
		case ActConfirm:
			return actionButtons[ActBack]
		case ActBack:
			return actionButtons[ActConfirm]
		case ActShuffle:
			return actionButtons[ActPause]
		case ActPause:
			return actionButtons[ActShuffle]
		}
	}
	return actionButtons[a]
}

// duration es cuántos frames lleva pulsada la acción (0 si no lo está).
func (in *Input) duration(a Action) int {
	d := 0
	for _, k := range actionKeys[a] {
		d = max(d, inpututil.KeyPressDuration(k))
	}
	for _, id := range in.pads {
		for _, b := range in.buttons(a) {
			d = max(d, inpututil.StandardGamepadButtonPressDuration(id, in.physical(b)))
		}
	}
	for i, sa := range stickDirs {
		if sa == a {
			d = max(d, in.stick[i])
		}
	}
	if in.virt[a] && d == 0 {
		d = 1
	}
	return d
}

// Pressed dice si la acción se pulsó en este frame.
func (in *Input) Pressed(a Action) bool { return in.duration(a) == 1 }

// Repeat es como Pressed, pero al mantenerla se repite: para moverse por listas.
func (in *Input) Repeat(a Action) bool {
	d := in.duration(a)
	return d == 1 || (d > repeatDelay && (d-repeatDelay)%repeatEvery == 0)
}

// notPads son trozos del nombre de lo que el sistema presenta como mando sin
// serlo: los sensores de movimiento y el panel táctil de un mando de
// PlayStation (con su diseño estándar, y el giroscopio de "stick", que no
// está en cero en reposo), y ratones y teclados virtuales.
var notPads = []string{"motion sensors", "touchpad", "mouse", "keyboard"}

// notAPad dice si id no es un mando de verdad (como en Trancazo): por su
// nombre, o porque comparte id de SDL con otro que tiene más del doble de
// botones (el mando, e id sus sensores o su panel táctil; Ebitengine les da
// el nombre de su base de datos, no el del dispositivo). Dos mandos iguales
// tienen los mismos botones y se quedan los dos.
func notAPad(id ebiten.GamepadID, all []ebiten.GamepadID) bool {
	name := strings.ToLower(ebiten.GamepadName(id))
	for _, n := range notPads {
		if strings.Contains(name, n) {
			return true
		}
	}
	sdl, buttons := ebiten.GamepadSDLID(id), ebiten.GamepadButtonCount(id)
	for _, other := range all {
		if other != id && ebiten.GamepadSDLID(other) == sdl && buttons*2 < ebiten.GamepadButtonCount(other) {
			return true
		}
	}
	return false
}
