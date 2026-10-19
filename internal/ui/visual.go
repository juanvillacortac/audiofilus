package ui

import (
	"fmt"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Visualizador: una pantalla con varios visualizadores, todos de un solo
// color: cada uno pinta en intensidad (gris) a media resolución y un shader lo
// pasa a la rampa de la paleta (fondo → acento → texto) con trama de Bayer.
// ←/→ cambian de visualizador, A su cámara o variante.
//
//   - Derrape: un coche en 3D derrapando por una carretera hecha de música
//     (drift.go).
//   - Barras: el analizador de Winamp, con picos que caen, y un osciloscopio.
//   - Túnel: realimentación al estilo MilkDrop o "Battery" de Windows Media
//     Player: cada fotograma reusa el anterior, girado, ampliado y apagándose,
//     y encima un anillo que es el espectro.
//   - Plasma: el "Ambience" de Windows Media Player, plasma que se agita con
//     los graves y los agudos, y ondas en cada golpe.
//   - Ondas: un osciloscopio con estela de fósforo.
//   - Trinchera: un caza por la trinchera de una estación espacial hecha de
//     música (trench.go).
//   - Lago: un lago de noche con nenúfares, flores y animales que reaccionan
//     a la música (lake.go).
//   - Crestas: las líneas apiladas de Unknown Pleasures, hechas del espectro
//     de cada momento (ridges.go).
//
// El servicio da el espectro, no la forma de onda: los osciloscopios la
// sintetizan del espectro real (reaccionan a la música, pero no son la onda).

const (
	visDrift = iota
	visBars
	visTunnel
	visPlasma
	visWaves
	visTrench
	visLake
	visRidges
	visCount
)

// visNames son las claves de los nombres de cada visualizador.
var visNames = [visCount]string{"vis_drift", "vis_bars", "vis_tunnel", "vis_plasma", "vis_waves", "vis_trench", "vis_lake", "vis_ridges"}

// visState es la pantalla de visualizadores.
type visState struct {
	kind       int
	variant    [visCount]int
	switchAt   float64          // cuándo se cambió (para enseñar el nombre)
	img        *ebiten.Image    // el visualizador, en intensidad, a media resolución
	out        *ebiten.Image    // ya en la paleta
	fb         [2]*ebiten.Image // la realimentación (Túnel, Ondas)
	bars       []float64
	peaks      []float64
	barAvg     []float64
	phase      [engine.MeterBands]float64
	ridges     []ridgeRow // Crestas (ridges.go)
	ridgeAt    float64
	ridgeSeed  float64
	ridgeBatch rectBatch
	frame      int // para redibujar a 30 fps
	lastKind   int
}

// updateVisual atiende la pantalla de visualizadores.
func (a *App) updateVisual() {
	v, in := &a.vis, &a.input
	switch {
	case in.Pressed(ActRight):
		v.kind, v.switchAt = (v.kind+1)%visCount, a.t
	case in.Pressed(ActLeft):
		v.kind, v.switchAt = (v.kind+visCount-1)%visCount, a.t
	case in.Pressed(ActConfirm):
		switch {
		case v.kind == visDrift:
			if a.drift == nil {
				a.drift = newDrift()
			}
			a.drift.cam = (a.drift.cam + 1) % 3
		case v.kind == visLake:
			if a.lake == nil {
				a.lake = newLake()
			}
			a.lake.cam = (a.lake.cam + 1) % 3
		case v.kind == visTrench:
			if a.trench == nil {
				a.trench = newTrench()
			}
			a.trench.cam = (a.trench.cam + 1) % 3
		default:
			v.variant[v.kind] = (v.variant[v.kind] + 1) % 2
		}
		v.switchAt = a.t
	case in.Pressed(ActPause):
		a.send("toggle")
	case in.Pressed(ActMenu):
		a.openVisualMenu()
		return
	case in.Pressed(ActBack):
		a.goBack()
		return
	case in.Pressed(ActPrev):
		a.send("prev")
	case in.Pressed(ActNext):
		a.send("next")
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	}
	switch v.kind {
	case visDrift:
		if a.drift == nil {
			a.drift = newDrift()
		}
		a.stepDrift(1.0 / 60)
	case visTrench:
		if a.trench == nil {
			a.trench = newTrench()
		}
		a.stepTrench(1.0 / 60)
	case visLake:
		if a.lake == nil {
			a.lake = newLake()
		}
		a.stepLake(1.0 / 60)
	}
}

// fitImg deja *img del tamaño w×h.
func fitImg(img **ebiten.Image, w, h int) {
	if *img != nil && (*img).Bounds().Dx() == w && (*img).Bounds().Dy() == h {
		return
	}
	if *img != nil {
		(*img).Deallocate()
	}
	*img = ebiten.NewImage(w, h)
}

// drawVisual pinta el visualizador elegido.
func (a *App) drawVisual(dst *ebiten.Image) {
	v := &a.vis
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	top, bottom := topH, h-bottomH
	// A media resolución (píxeles gordos), salvo Derrape: el coche necesita
	// detalle.
	scale := 2
	if v.kind == visDrift || v.kind == visTrench || v.kind == visLake {
		scale = 1
	}
	lw, lh := w/scale, (bottom-top)/scale
	if lw < 8 || lh < 8 {
		return
	}
	// Se redibuja a 30 fps (uno de cada dos fotogramas): para música basta,
	// y en la consola la mitad de CPU es batería. Entre medias, la anterior.
	v.frame++
	fresh := v.out != nil && v.out.Bounds().Dx() == lw && v.out.Bounds().Dy() == lh && v.lastKind == v.kind
	v.lastKind = v.kind
	if fresh && v.frame%2 == 1 && !a.offline {
		a.blitVisual(dst, scale, top)
		return
	}
	fitImg(&v.img, lw, lh)
	fitImg(&v.out, lw, lh)
	v.img.Fill(gray(0))
	switch v.kind {
	case visDrift:
		a.drawDriftScene(v.img)
	case visBars:
		a.drawBars(v.img)
	case visTunnel:
		a.drawTunnel(v.img)
	case visPlasma:
		a.drawPlasma(v.img)
	case visWaves:
		a.drawWaves(v.img)
	case visTrench:
		a.drawTrenchScene(v.img)
	case visLake:
		a.drawLakeScene(v.img)
	case visRidges:
		a.drawRidges(v.img)
	}
	// A la paleta (con la trama a media resolución: píxeles gordos) y al
	// doble a la pantalla.
	th := a.theme
	op := &ebiten.DrawRectShaderOptions{}
	op.Images[0] = v.img
	op.Uniforms = map[string]any{"Bg": vec3(th.Bg), "Mid": vec3(th.Accent), "Hi": vec3(th.Text)}
	v.out.DrawRectShader(lw, lh, paletteShader, op)
	a.blitVisual(dst, scale, top)
}

// blitVisual pone el visualizador ya pintado en la pantalla, con su nombre y
// la ayuda.
func (a *App) blitVisual(dst *ebiten.Image, scale, top int) {
	v := &a.vis
	w := dst.Bounds().Dx()
	th := a.theme
	dop := &ebiten.DrawImageOptions{}
	dop.GeoM.Scale(float64(scale), float64(scale))
	dop.GeoM.Translate(0, float64(top))
	dst.DrawImage(v.out, dop)

	// El nombre, un momento al cambiar.
	if p := (a.t - v.switchAt) / 1.4; v.switchAt > 0 && p < 1 {
		name := T(visNames[v.kind])
		cam := -1
		switch {
		case v.kind == visDrift && a.drift != nil:
			cam = a.drift.cam
		case v.kind == visLake && a.lake != nil:
			cam = a.lake.cam
		case v.kind == visTrench && a.trench != nil:
			cam = a.trench.cam
		}
		if cam >= 0 {
			name += fmt.Sprintf(" · %s %d", T("h_camera"), cam+1)
		}
		k := uint8(255 * steps(1-p))
		tw := textWidth(name, SmallFace)
		panel(dst, (w-tw)/2-6, top+8, tw+12, smallCap+10, withAlpha(th.Bg2, k))
		drawText(dst, name, SmallFace, (w-tw)/2, top+13, withAlpha(th.Text, k))
	}
	aLabel := T("h_camera")
	if v.kind != visDrift && v.kind != visLake && v.kind != visTrench {
		aLabel = T("h_variant")
	}
	a.drawHints(dst, []hint{{"<>", T("h_visual")}, {"A", aLabel}, {"START", T("h_more")}, {"SEL", T("h_menu")}})
}

// visBands es el espectro de ahora (0 si no suena).
func (a *App) visBands() (level, bass float64, bands [engine.MeterBands]float64) {
	level, bass, bands, _ = a.meterNow()
	return
}

// bandAt interpola el espectro en f (0..1, de graves a agudos).
func bandAt(bands [engine.MeterBands]float64, f float64) float64 {
	pos := math.Max(0, math.Min(1, f)) * (engine.MeterBands - 1)
	k := int(pos)
	v := bands[k]
	if k+1 < engine.MeterBands {
		v += (bands[k+1] - bands[k]) * (pos - float64(k))
	}
	return v
}

// drawBars es el analizador de Winamp: barras con un degradado por bloques,
// picos que caen despacio y un osciloscopio abajo. Variante: en espejo.
func (a *App) drawBars(img *ebiten.Image) {
	v := &a.vis
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	_, _, bands := a.visBands()
	const bw = 4
	n := lw / bw
	if len(v.bars) != n {
		v.bars, v.peaks, v.barAvg = make([]float64, n), make([]float64, n), make([]float64, n)
	}
	mirror := v.variant[visBars] == 1
	base := lh * 3 / 4
	if mirror {
		base = lh / 2
	}
	for i := 0; i < n; i++ {
		x := bandAt(bands, float64(i)/float64(n-1))
		// Contraste, como el visualizador de la pantalla de reproducción.
		v.barAvg[i] += (x - v.barAvg[i]) * 0.02
		x = math.Max(0, math.Min(1, 0.5*x+2*(x-v.barAvg[i])))
		v.bars[i] = math.Max(x, v.bars[i]-0.03)
		v.peaks[i] = math.Max(v.bars[i], v.peaks[i]-0.008)
		hgt := int(v.bars[i] * float64(base-4))
		for y := 0; y < hgt; y += 2 { // por bloques, con el degradado de Winamp
			t := 0.35 + 1.1*float64(y)/float64(base)
			a.batch.add(i*bw, base-y-2, bw-1, 1, gray(t))
			if mirror {
				a.batch.add(i*bw, base+y+1, bw-1, 1, gray(t*0.6))
			}
		}
		py := base - int(v.peaks[i]*float64(base-4)) - 2
		a.batch.add(i*bw, py, bw-1, 1, gray(1.5))
	}
	a.batch.draw(img)
	if !mirror { // el osciloscopio, debajo
		a.scopeLine(img, base+(lh-base)/2, float64(lh-base)/2-2, bands, 1.2)
	}
}

// scopeLine pinta una forma de onda sintetizada del espectro, centrada en y.
func (a *App) scopeLine(img *ebiten.Image, y int, amp float64, bands [engine.MeterBands]float64, tone float64) {
	v := &a.vis
	lw := img.Bounds().Dx()
	for b := range v.phase {
		v.phase[b] += (0.05 + 0.02*float64(b)) // cada banda, a su paso
	}
	prev := 0.0
	for x := 0; x < lw; x++ {
		s := 0.0
		for b := 0; b < engine.MeterBands; b++ {
			f := 1 + float64(b)*1.7
			s += bands[b] * math.Sin(float64(x)/float64(lw)*2*math.Pi*f+v.phase[b]) / (1 + float64(b)*0.25)
		}
		s = math.Max(-1, math.Min(1, s*0.5))
		yy := float64(y) - s*amp
		if x > 0 {
			vector.StrokeLine(img, float32(x-1), float32(prev), float32(x), float32(yy), 1, gray(tone), false)
		}
		prev = yy
	}
}

// feedback copia la realimentación anterior a la nueva, girada y ampliada
// (zoom y rot) y apagándose (decay), y devuelve la nueva para pintar encima.
func (a *App) feedback(lw, lh int, zoom, rot, decay float64) *ebiten.Image {
	v := &a.vis
	fitImg(&v.fb[0], lw, lh)
	fitImg(&v.fb[1], lw, lh)
	src, dst := v.fb[0], v.fb[1]
	dst.Fill(gray(0))
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(lw)/2, -float64(lh)/2)
	op.GeoM.Rotate(rot)
	op.GeoM.Scale(zoom, zoom)
	op.GeoM.Translate(float64(lw)/2, float64(lh)/2)
	op.ColorScale.Scale(float32(decay), float32(decay), float32(decay), 1)
	dst.DrawImage(src, op)
	v.fb[0], v.fb[1] = dst, src
	return dst
}

// drawTunnel es el túnel de realimentación: el anillo del espectro se va
// hacia fuera, girando, y deja estela. Variante: hacia dentro.
func (a *App) drawTunnel(img *ebiten.Image) {
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	level, bass, bands := a.visBands()
	pulse := a.beatPulse()
	zoom, rot := 1.03+0.05*bass, 0.012+0.03*level
	if a.vis.variant[visTunnel] == 1 {
		zoom, rot = 0.96-0.03*bass, -rot
	}
	fb := a.feedback(lw, lh, zoom, rot, 0.9)
	// El anillo: 64 puntos, su radio sigue al espectro.
	cx, cy := float64(lw)/2, float64(lh)/2
	r0 := float64(lh) * (0.12 + 0.08*pulse)
	const n = 64
	var px, py float64
	for i := 0; i <= n; i++ {
		ang := 2 * math.Pi * float64(i) / n
		f := math.Abs(float64(i%n)/n*2 - 1) // simétrico
		r := r0 * (1 + 0.9*bandAt(bands, f))
		x, y := cx+r*math.Cos(ang+a.t*0.3), cy+r*math.Sin(ang+a.t*0.3)
		if i > 0 {
			vector.StrokeLine(fb, float32(px), float32(py), float32(x), float32(y), 1, gray(1.1+0.4*pulse), false)
		}
		px, py = x, y
	}
	img.DrawImage(fb, nil)
}

// drawPlasma es el plasma: un shader de ondas que se agitan con la música.
// Variante: más fino.
func (a *App) drawPlasma(img *ebiten.Image) {
	scale := 1.0
	if a.vis.variant[visPlasma] == 1 {
		scale = 2.2
	}
	a.plasmaInto(img, scale)
}

// plasmaInto pinta el plasma en intensidad en img, con la escala de sus
// ondas (Carrera lo usa de cielo).
func (a *App) plasmaInto(img *ebiten.Image, scale float64) {
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	level, bass, bands := a.visBands()
	var treble float64
	for k := 10; k < engine.MeterBands; k++ {
		treble += bands[k] / 6
	}
	since := 9.0
	if a.beat.at > 0 {
		since = math.Max(0, a.beatClock()-a.beat.at)
	}
	op := &ebiten.DrawRectShaderOptions{}
	op.Uniforms = map[string]any{
		"Size": []float32{float32(lw), float32(lh)}, "Time": float32(a.t),
		"Bass": float32(bass), "Treble": float32(treble), "Level": float32(level),
		"Since": float32(since), "Scale": float32(scale),
	}
	img.DrawRectShader(lw, lh, plasmaShader, op)
}

// drawWaves es el osciloscopio con estela de fósforo. Variante: tres líneas.
func (a *App) drawWaves(img *ebiten.Image) {
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	_, bass, bands := a.visBands()
	fb := a.feedback(lw, lh, 1.0, 0, 0.8)
	lines := 1
	if a.vis.variant[visWaves] == 1 {
		lines = 3
	}
	for k := 0; k < lines; k++ {
		y := lh / 2
		if lines > 1 {
			y = lh * (k + 1) / (lines + 1)
		}
		a.scopeLine(fb, y, float64(lh)*(0.3+0.2*bass)/float64(lines), bands, 1.4-0.25*float64(k))
	}
	img.DrawImage(fb, nil)
}
