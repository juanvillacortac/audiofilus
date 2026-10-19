package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Crestas: las líneas apiladas de la portada de Unknown Pleasures. Cada línea
// es el espectro de un momento; las nuevas entran por abajo y suben, y cada
// una tapa a las de detrás. Variante 0: los graves en el centro, en picos,
// como el púlsar; variante 1: de graves a agudos, de izquierda a derecha.

const (
	ridgeRows  = 18       // las líneas a la vez
	ridgeEvery = 1.0 / 12 // cada cuánto entra una nueva, en segundos
	ridgeMax   = 4.5      // la altura de un pico, en separaciones entre líneas
)

// ridgeRow es una línea: el espectro de cuando entró y su ruido propio.
type ridgeRow struct {
	bands [engine.MeterBands]float64
	seed  float64
	beat  float64 // el golpe de entonces (0..1), para que esas líneas brillen
}

// drawRidges pinta las crestas en intensidad.
func (a *App) drawRidges(img *ebiten.Image) {
	v := &a.vis
	lw, lh := img.Bounds().Dx(), img.Bounds().Dy()
	level, _, bands := a.visBands()
	now := a.beatClock()
	// Las líneas nuevas; si hace mucho que no se pintaba, se rellena de una.
	if now-v.ridgeAt > ridgeEvery*ridgeRows {
		v.ridgeAt = now - ridgeEvery*ridgeRows
	}
	for now-v.ridgeAt >= ridgeEvery {
		v.ridgeAt += ridgeEvery
		v.ridgeSeed += 1.7
		v.ridges = append(v.ridges, ridgeRow{bands: bands, seed: v.ridgeSeed, beat: a.beatPulse()})
		if len(v.ridges) > ridgeRows {
			v.ridges = v.ridges[len(v.ridges)-ridgeRows:]
		}
	}
	frac := (now - v.ridgeAt) / ridgeEvery
	gap := float64(lh-24) / ridgeRows
	center := v.variant[visRidges] == 0
	// De atrás (arriba, las viejas) hacia delante: cada una pinta de negro lo
	// que hay debajo de su línea, así tapa a las anteriores.
	b := &v.ridgeBatch
	n := len(v.ridges)
	margin := lw / 6
	for i, r := range v.ridges {
		base := 20 + (float64(ridgeRows-n+i)+1-frac)*gap
		bright := gray(0.5 + 0.7*float64(i)/ridgeRows + 0.4*r.beat)
		prev := -1
		for x := margin; x < lw-margin; x++ {
			u := float64(x-margin) / float64(lw-2*margin)
			var f, env float64
			if center {
				f = math.Abs(u-0.5) * 2
				env = math.Exp(-math.Pow((u-0.5)/0.2, 2))
			} else {
				f = u
				env = 0.3 + 0.7*math.Sin(math.Pi*u)
			}
			// El ruido de cada línea, más con la música, y el espectro.
			noise := math.Sin(float64(x)*0.9+r.seed) * math.Sin(float64(x)*0.37+r.seed*1.3)
			hgt := gap * (ridgeMax*env*bandAt(r.bands, f) + (0.25+0.6*level)*env*math.Abs(noise) + 0.15*math.Abs(noise))
			y := int(math.Round(base - hgt))
			b.add(x, y, 1, lh-y, gray(0))
			top, h := y, 1
			if prev >= 0 && prev < y { // une con la columna de antes
				top, h = prev, y-prev+1
			} else if prev > y {
				h = prev - y + 1
			}
			b.add(x, top, 1, h, bright)
			prev = y
		}
	}
	b.draw(img)
}
