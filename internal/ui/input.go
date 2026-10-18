package ui

import (
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
	decided  bool
	// any dice si en este frame se pulsó cualquier tecla o botón.
	any bool
	// swapAB intercambia A y B, para quien los quiera al revés.
	swapAB bool
}

// update se llama una vez por frame, antes de preguntar.
func (in *Input) update() {
	// Algunos receptores de ratón o teclado se anuncian como joystick: sin un
	// mapeo estándar no se cuentan como mando.
	in.all = ebiten.AppendGamepadIDs(in.all[:0])
	in.pads = in.pads[:0]
	for _, id := range in.all {
		if ebiten.IsStandardGamepadLayoutAvailable(id) {
			in.pads = append(in.pads, id)
		}
	}
	if !in.decided {
		in.usingPad = len(in.pads) > 0
	}
	in.any = false
	if in.keys = inpututil.AppendJustPressedKeys(in.keys[:0]); len(in.keys) > 0 {
		in.usingPad, in.decided, in.any = false, true, true
	}
	for _, id := range in.pads {
		for b := ebiten.StandardGamepadButton(0); b <= ebiten.StandardGamepadButtonMax; b++ {
			if inpututil.IsStandardGamepadButtonJustPressed(id, b) {
				in.usingPad, in.decided, in.any = true, true, true
			}
		}
	}
}

// buttons son los botones de una acción, con A y B intercambiados si se pidió.
func (in *Input) buttons(a Action) []ebiten.StandardGamepadButton {
	if in.swapAB {
		switch a {
		case ActConfirm:
			return actionButtons[ActBack]
		case ActBack:
			return actionButtons[ActConfirm]
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
			d = max(d, inpututil.StandardGamepadButtonPressDuration(id, b))
		}
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
