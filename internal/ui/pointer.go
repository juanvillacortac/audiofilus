package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Ratón y pantalla táctil. Al dibujarse, cada pantalla registra sus zonas
// tocables (filas de las listas, portadas, pestañas, botones, la barra de
// progreso, los chips de la ayuda de abajo...); en el siguiente Update, un
// toque o un clic hace lo de la zona de encima (la última registrada que lo
// contenga). Lo demás son gestos que valen en todas partes, convertidos en
// las acciones de siempre (las de la cruceta), así cualquier pantalla los
// entiende sin hacer nada:
//
//   - arrastrar: ↑/↓ (o ←/→) por cada paso arrastrado (una fila; una
//     portada en la cuadrícula), con inercia al soltar, como en un móvil;
//   - la rueda del ratón: ↑/↓;
//   - mantener pulsado (sin moverse): START, el menú Más;
//   - clic derecho: B (atrás); clic central: pausa.

// zone es una zona tocable: lo que hace un toque en ella (con dónde fue,
// para la barra de progreso).
type zone struct {
	x, y, w, h int
	tap        func(px, py int)
}

// Los umbrales de los gestos, en píxeles del lienzo y segundos.
const (
	tapSlop     = 4    // lo que se puede mover un toque sin ser arrastre
	longPress   = 0.55 // lo que dura una pulsación larga
	flingDecay  = 4.0  // cuánto frena la inercia (por segundo)
	flingStop   = 40.0 // por debajo de esta velocidad (px/s), se para
	defaultStep = 14   // lo que hay que arrastrar por paso, si la pantalla no dice otro
)

// pointer es el estado del ratón o del dedo.
type pointer struct {
	zones, next  []zone // las del cuadro que se ve, y las que se van registrando
	stepX, stepY int    // el paso del arrastre en la pantalla que se ve (0: el de siempre)
	nextSX       int
	nextSY       int

	down     bool
	touch    bool
	id       ebiten.TouchID
	sx, sy   int     // dónde empezó
	x, y     int     // dónde está
	at       float64 // cuándo empezó
	moved    bool
	axis     int // 0 sin decidir, 1 horizontal, 2 vertical
	acc      float64
	vel      float64 // px/s en el eje del arrastre, para la inercia
	lastT    float64
	long     bool
	pending  int // pasos que quedan por mandar (+: abajo/derecha)
	flingVel float64
	flingAx  int
	wheel    float64
}

// zone registra una zona tocable del cuadro que se está dibujando.
func (a *App) zone(x, y, w, h int, tap func(px, py int)) {
	a.ptr.next = append(a.ptr.next, zone{x, y, w, h, tap})
}

// tapZone registra una zona que al tocarla hace do (sin importar dónde).
func (a *App) tapZone(x, y, w, h int, do func()) {
	a.zone(x, y, w, h, func(int, int) { do() })
}

// dragStep dice cuánto hay que arrastrar por paso en la pantalla que se está
// dibujando (la cuadrícula de portadas, una portada).
func (a *App) dragStep(x, y int) { a.ptr.nextSX, a.ptr.nextSY = x, y }

// beginZones empieza a registrar las zonas de un cuadro.
func (a *App) beginZones() {
	a.ptr.next = a.ptr.next[:0]
	a.ptr.nextSX, a.ptr.nextSY = 0, 0
}

// endZones da por buenas las zonas del cuadro dibujado.
func (a *App) endZones() {
	p := &a.ptr
	p.zones, p.next = p.next, p.zones
	p.stepX, p.stepY = p.nextSX, p.nextSY
}

// updatePointer lee el ratón y los dedos y los convierte en toques, gestos
// y acciones (Input.inject). Va justo después de Input.update.
func (a *App) updatePointer() {
	p, in := &a.ptr, &a.input
	const dt = 1.0 / 60

	// Botones del ratón que son acciones.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
		in.inject(ActBack)
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonMiddle) {
		in.inject(ActPause)
	}
	// La rueda: un paso por muesca (las de los touchpads llegan en trozos).
	if _, wy := ebiten.Wheel(); wy != 0 {
		p.wheel += wy
		in.mouseUsed()
	}
	for p.wheel >= 1 {
		p.wheel--
		p.pending--
	}
	for p.wheel <= -1 {
		p.wheel++
		p.pending++
	}

	// El dedo o el botón izquierdo.
	if !p.down {
		if ids := inpututil.AppendJustPressedTouchIDs(nil); len(ids) > 0 {
			x, y := ebiten.TouchPosition(ids[0])
			a.pointerDown(x, y, true, ids[0])
		} else if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			x, y := ebiten.CursorPosition()
			a.pointerDown(x, y, false, 0)
			in.mouseUsed()
		}
	} else {
		var x, y int
		var up bool
		if p.touch {
			x, y = ebiten.TouchPosition(p.id)
			up = inpututil.IsTouchJustReleased(p.id)
			if up {
				x, y = p.x, p.y // al soltar, TouchPosition ya no lo sabe
			}
		} else {
			x, y = ebiten.CursorPosition()
			up = !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
		}
		a.pointerMove(x, y, dt)
		if up {
			a.pointerUp()
		} else if !p.moved && !p.long && a.t-p.at >= longPress {
			p.long = true
			in.inject(ActMenu) // mantener pulsado: el menú Más
		}
	}

	// La inercia de un arrastre soltado.
	if !p.down && p.flingAx != 0 {
		p.acc += p.flingVel * dt
		p.flingVel *= math.Exp(-flingDecay * dt)
		a.dragSteps(p.flingAx)
		if math.Abs(p.flingVel) < flingStop {
			p.flingAx, p.flingVel, p.acc = 0, 0, 0
		}
	}

	// Un paso por fotograma (Pressed ve una pulsación por fotograma).
	axisH := p.flingAx == 1 || p.down && p.axis == 1
	switch {
	case p.pending > 0:
		p.pending--
		in.inject(map[bool]Action{true: ActRight, false: ActDown}[axisH])
	case p.pending < 0:
		p.pending++
		in.inject(map[bool]Action{true: ActLeft, false: ActUp}[axisH])
	}
}

// pointerDown empieza un toque o un clic en x, y.
func (a *App) pointerDown(x, y int, touch bool, id ebiten.TouchID) {
	p := &a.ptr
	*p = pointer{zones: p.zones, next: p.next, stepX: p.stepX, stepY: p.stepY, nextSX: p.nextSX, nextSY: p.nextSY,
		down: true, touch: touch, id: id, sx: x, sy: y, x: x, y: y, at: a.t, lastT: a.t}
	a.input.any = true
}

// pointerMove sigue al dedo: pasado tapSlop es un arrastre, en el eje en que
// más se movió, y cada paso arrastrado es una flecha.
func (a *App) pointerMove(x, y int, dt float64) {
	p := &a.ptr
	dx, dy := x-p.x, y-p.y
	p.x, p.y = x, y
	if !p.moved && (abs(x-p.sx) > tapSlop || abs(y-p.sy) > tapSlop) {
		p.moved = true
		p.axis = 2
		if abs(x-p.sx) > abs(y-p.sy) {
			p.axis = 1
		}
		dx, dy = x-p.sx, y-p.sy // cuenta desde donde empezó, no desde el margen
	}
	if !p.moved {
		return
	}
	// El contenido sigue al dedo: subir el dedo es bajar por la lista.
	d := -float64(dy)
	if p.axis == 1 {
		d = -float64(dx)
	}
	p.acc += d
	p.vel = p.vel*0.6 + d/dt*0.4
	a.dragSteps(p.axis)
}

// dragSteps pasa lo arrastrado en el eje (1 horizontal, 2 vertical) a pasos.
func (a *App) dragSteps(axis int) {
	p := &a.ptr
	step := float64(defaultStep)
	if axis == 1 && p.stepX > 0 {
		step = float64(p.stepX)
	} else if axis == 2 && p.stepY > 0 {
		step = float64(p.stepY)
	}
	for p.acc >= step {
		p.acc -= step
		p.pending++
	}
	for p.acc <= -step {
		p.acc += step
		p.pending--
	}
}

// pointerUp termina el toque: sin moverse, es un toque en su zona; si se
// arrastraba, sigue un poco por inercia.
func (a *App) pointerUp() {
	p := &a.ptr
	p.down = false
	switch {
	case p.long:
	case !p.moved:
		for i := len(p.zones) - 1; i >= 0; i-- {
			z := p.zones[i]
			if p.x >= z.x && p.x < z.x+z.w && p.y >= z.y && p.y < z.y+z.h {
				a.input.any = true
				z.tap(p.x-z.x, p.y-z.y)
				return
			}
		}
	case math.Abs(p.vel) > flingStop*4:
		p.flingAx, p.flingVel = p.axis, p.vel
	}
}

// hintActions son las acciones de los botones de la ayuda de abajo: tocar
// un chip es pulsar su botón.
var hintActions = map[string]Action{
	"A": ActConfirm, "B": ActBack, "X": ActShuffle, "Y": ActPause, "START": ActMenu, "SEL": ActSelect,
	"L1": ActPrev, "R1": ActNext, "L2": ActSeekBack, "R2": ActSeekFwd,
	"<": ActLeft, ">": ActRight, "^": ActUp, "v": ActDown,
}
