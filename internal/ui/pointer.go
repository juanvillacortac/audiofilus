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
//   - arrastrar en una lista que se desplaza sola (scrollArea: las de la
//     biblioteca, la cuadrícula de portadas, la cola, Ajustes): el contenido
//     sigue al dedo píxel a píxel, con inercia al soltar, como en un móvil;
//     la rueda del ratón la desplaza igual;
//   - en un juego, deslizar es una sola pulsación hacia donde va el dedo
//     (en Bloques, a los lados la pieza sigue al dedo casilla a casilla,
//     abajo la suelta y arriba la guarda; tocar el pozo la gira);
//   - arrastrar en lo demás: ↑/↓ (o ←/→) por cada paso arrastrado, también
//     con inercia; la rueda, ↑/↓;
//   - mantener pulsado (sin moverse): START, el menú Más;
//   - clic derecho: B (atrás); clic central: pausa.

// zone es una zona tocable: lo que hace un toque en ella (con dónde fue,
// para la barra de progreso).
type zone struct {
	x, y, w, h int
	tap        func(px, py int)
	press      bool             // responde al tocar, sin esperar a soltar (los juegos de ritmo)
	drag       func(dx, dy int) // en vez de tocar: recibe lo que se mueve el dedo (mover la nave)
}

// scrollZone es una zona que se desplaza con el dedo: by la mueve d píxeles
// (positivo: hacia abajo en el contenido).
type scrollZone struct {
	x, y, w, h int
	by         func(d float64)
}

// scroller es el desplazamiento libre de una lista: en píxeles desde
// arriba. Lo mueve el dedo (free); en cuanto la selección cambia por otra
// vía (la cruceta), la vista vuelve a seguirla.
type scroller struct {
	px    float64
	max   float64
	free  bool
	drawn int // la selección del último dibujo
}

// place da la primera fila visible y cuánto se ve cortada (en píxeles)
// para una lista de n filas de rowH, de las que caben rows, con la selección
// en sel. Sin desplazamiento libre, la selección va hacia el centro.
func (s *scroller) place(sel, rows, n, rowH int) (first, off int) {
	if sel != s.drawn {
		s.free, s.drawn = false, sel
	}
	s.max = float64(max(0, n-rows) * rowH)
	if !s.free {
		s.px = float64(max(0, min(sel-rows/2, n-rows)) * rowH)
	}
	s.px = max(0, min(s.max, s.px))
	p := int(s.px)
	return p / rowH, p % rowH
}

// by mueve la lista d píxeles (con el dedo o la rueda).
func (s *scroller) by(d float64) {
	s.free = true
	s.px = max(0, min(s.max, s.px+d))
}

// pick elige la fila idx sin que la vista salte (la eligió el dedo).
func (s *scroller) pick(sel *int, idx int) { *sel, s.drawn = idx, idx }

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
	scrolls      []scrollZone
	nextScrolls  []scrollZone
	scroll       func(d float64) // la zona que se arrastra ahora (o nil)
	flingScroll  func(d float64)
	stepX, stepY int // el paso del arrastre en la pantalla que se ve (0: el de siempre)
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
	used     bool             // una zona "al pulsar" ya se usó: soltar no hace nada más
	dragFn   func(dx, dy int) // la zona de arrastre bajo el dedo
	swiped   bool             // en un juego, el deslizamiento ya dio su pulsación
	pending  int              // pasos que quedan por mandar (+: abajo/derecha)
	pendAx   int              // en qué eje son (1 horizontal, 2 vertical)
	flingVel float64
	flingAx  int
	wheel    float64
}

// zone registra una zona tocable del cuadro que se está dibujando.
func (a *App) zone(x, y, w, h int, tap func(px, py int)) {
	a.ptr.next = append(a.ptr.next, zone{x: x, y: y, w: w, h: h, tap: tap})
}

// scrollArea registra una zona que se desplaza con el dedo y la rueda.
func (a *App) scrollArea(x, y, w, h int, by func(d float64)) {
	a.ptr.nextScrolls = append(a.ptr.nextScrolls, scrollZone{x, y, w, h, by})
}

// scrollAt es la zona desplazable bajo x, y (o nil).
func (p *pointer) scrollAt(x, y int) func(float64) {
	for i := len(p.scrolls) - 1; i >= 0; i-- {
		z := p.scrolls[i]
		if x >= z.x && x < z.x+z.w && y >= z.y && y < z.y+z.h {
			return z.by
		}
	}
	return nil
}

// pressZone registra una zona que responde al tocar (no al soltar), con la
// posición del toque relativa a la zona: en los juegos de ritmo, esperar a
// levantar el dedo llega tarde al golpe.
func (a *App) pressZone(x, y, w, h int, press func(px, py int)) {
	a.ptr.next = append(a.ptr.next, zone{x: x, y: y, w: w, h: h, tap: press, press: true})
}

// dragArea registra una zona en la que arrastrar mueve algo del juego
// directamente (la nave, el corazón): drag recibe cuánto se movió el dedo.
func (a *App) dragArea(x, y, w, h int, drag func(dx, dy int)) {
	a.ptr.next = append(a.ptr.next, zone{x: x, y: y, w: w, h: h, drag: drag})
}

// held dice si hay un dedo (o el botón del ratón) apoyado ahora, y dónde
// empezó: Carrera salta más mientras se mantiene.
func (a *App) held() (down bool, x, y int) { return a.ptr.down, a.ptr.sx, a.ptr.sy }

// tapZone registra una zona que al tocarla hace do (sin importar dónde).
func (a *App) tapZone(x, y, w, h int, do func()) {
	a.zone(x, y, w, h, func(int, int) { do() })
}

// zoneAt es la zona de encima en x, y (la última registrada que lo
// contenga), o nil.
func (p *pointer) zoneAt(x, y int) *zone {
	for i := len(p.zones) - 1; i >= 0; i-- {
		z := &p.zones[i]
		if x >= z.x && x < z.x+z.w && y >= z.y && y < z.y+z.h {
			return z
		}
	}
	return nil
}

// dragStep dice cuánto hay que arrastrar por paso en la pantalla que se está
// dibujando (la cuadrícula de portadas, una portada).
func (a *App) dragStep(x, y int) { a.ptr.nextSX, a.ptr.nextSY = x, y }

// beginZones empieza a registrar las zonas de un cuadro.
func (a *App) beginZones() {
	a.ptr.next = a.ptr.next[:0]
	a.ptr.nextScrolls = a.ptr.nextScrolls[:0]
	a.ptr.nextSX, a.ptr.nextSY = 0, 0
}

// endZones da por buenas las zonas del cuadro dibujado.
func (a *App) endZones() {
	p := &a.ptr
	p.zones, p.next = p.next, p.zones
	p.scrolls, p.nextScrolls = p.nextScrolls, p.scrolls
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
		in.mouseUsed()
		if by := p.scrollAt(ebiten.CursorPosition()); by != nil {
			by(-wy * 24) // suave, en píxeles, sobre una lista
		} else {
			p.wheel += wy
		}
	}
	for p.wheel >= 1 {
		p.wheel--
		p.pending, p.pendAx = p.pending-1, 2
	}
	for p.wheel <= -1 {
		p.wheel++
		p.pending, p.pendAx = p.pending+1, 2
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
		} else if !p.moved && !p.long && !p.used && p.dragFn == nil && !a.swipeMode() && a.t-p.at >= longPress {
			p.long = true
			in.inject(ActMenu) // mantener pulsado: el menú Más
		}
	}

	// La inercia de un arrastre soltado.
	if !p.down && p.flingAx != 0 {
		if p.flingScroll != nil {
			p.flingScroll(p.flingVel * dt)
		} else {
			p.acc += p.flingVel * dt
			a.dragSteps(p.flingAx)
		}
		p.flingVel *= math.Exp(-flingDecay * dt)
		if math.Abs(p.flingVel) < flingStop {
			p.flingAx, p.flingVel, p.acc, p.flingScroll = 0, 0, 0, nil
		}
	}

	// Un paso por fotograma (Pressed ve una pulsación por fotograma).
	axisH := p.pendAx == 1
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
	*p = pointer{zones: p.zones, next: p.next, scrolls: p.scrolls, nextScrolls: p.nextScrolls,
		stepX: p.stepX, stepY: p.stepY, nextSX: p.nextSX, nextSY: p.nextSY,
		down: true, touch: touch, id: id, sx: x, sy: y, x: x, y: y, at: a.t, lastT: a.t}
	p.scroll = p.scrollAt(x, y) // tocar una lista frena su inercia (se reinició arriba)
	a.input.any = true
	switch z := p.zoneAt(x, y); {
	case z == nil:
	case z.press:
		p.used = true
		z.tap(x-z.x, y-z.y)
	case z.drag != nil:
		p.dragFn = z.drag
	}
}

// pointerMove sigue al dedo: pasado tapSlop es un arrastre, en el eje en que
// más se movió, y cada paso arrastrado es una flecha.
func (a *App) pointerMove(x, y int, dt float64) {
	p := &a.ptr
	dx, dy := x-p.x, y-p.y
	p.x, p.y = x, y
	if p.dragFn != nil { // arrastrar algo del juego: tal cual, sin gestos
		if dx != 0 || dy != 0 {
			p.dragFn(dx, dy)
		}
		p.moved = true
		return
	}
	if p.used {
		return
	}
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
	// En un juego, deslizar es una sola pulsación hacia donde va el dedo
	// (como una cruceta), sin pasos ni inercia: en Cripta cada paso de más
	// era un movimiento fuera del golpe.
	if a.swipeMode() {
		// Bloques: a los lados, la pieza sigue al dedo casilla a casilla
		// (stepX es una casilla del pozo); abajo, soltarla; arriba, guardarla.
		if a.screen == Blocks && p.axis == 1 {
			p.acc += float64(dx)
			a.dragSteps(1)
			return
		}
		if a.screen == Blocks && !p.swiped {
			p.swiped = true
			if y > p.sy {
				a.input.inject(ActUp) // soltar (↑ en la cruceta)
			} else {
				a.input.inject(ActShuffle) // guardar
			}
			return
		}
		if !p.swiped {
			p.swiped = true
			ddx, ddy := x-p.sx, y-p.sy
			switch {
			case p.axis == 1 && ddx > 0:
				a.input.inject(ActRight)
			case p.axis == 1:
				a.input.inject(ActLeft)
			case ddy > 0:
				a.input.inject(ActDown)
			default:
				a.input.inject(ActUp)
			}
		}
		return
	}
	// El contenido sigue al dedo: subir el dedo es bajar por la lista.
	d := -float64(dy)
	if p.axis == 1 {
		d = -float64(dx)
	}
	p.vel = p.vel*0.6 + d/dt*0.4
	if p.axis == 2 && p.scroll != nil {
		p.scroll(d) // la lista sigue al dedo, píxel a píxel
		return
	}
	p.acc += d
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
	if p.pendAx != axis {
		p.pending, p.pendAx = 0, axis
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
	case p.long, p.used, p.dragFn != nil:
	case !p.moved:
		if z := p.zoneAt(p.x, p.y); z != nil && z.tap != nil && !z.press {
			a.input.any = true
			z.tap(p.x-z.x, p.y-z.y)
			return
		}
	case p.swiped:
	case math.Abs(p.vel) > flingStop*4:
		p.flingAx, p.flingVel = p.axis, p.vel
		if p.axis == 2 {
			p.flingScroll = p.scroll
		}
	}
}

// swipeMode dice si se está en un juego (sin menú encima): ahí deslizar es
// una sola pulsación, no un desplazamiento.
func (a *App) swipeMode() bool {
	return isGame(a.screen) && a.screen != Games && a.menu == nil && !a.dock.open
}

// hintActions son las acciones de los botones de la ayuda de abajo: tocar
// un chip es pulsar su botón.
var hintActions = map[string]Action{
	"A": ActConfirm, "B": ActBack, "X": ActShuffle, "Y": ActPause, "START": ActMenu, "SEL": ActSelect,
	"L1": ActPrev, "R1": ActNext, "L2": ActSeekBack, "R2": ActSeekFwd,
	"<": ActLeft, ">": ActRight, "^": ActUp, "v": ActDown,
}
