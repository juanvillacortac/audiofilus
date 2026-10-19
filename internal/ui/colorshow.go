package ui

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"time"
)

// La animación de la web de cómo la interfaz toma los colores de cada disco
// (-frames, carpeta "colores"): el reproductor pasando por cuatro álbumes de
// la biblioteca, elegidos con los tonos más distintos entre sí, con el cambio
// de canción y el color fundiéndose de uno al otro.

// hue es el tono de un color (0..1).
func hue(c color.RGBA) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := mx - mn
	if d == 0 {
		return 0
	}
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	if h < 0 {
		h++
	}
	return h
}

// hueDist es la distancia entre dos tonos, en la rueda (0..0,5).
func hueDist(a, b float64) float64 {
	d := math.Abs(a - b)
	return math.Min(d, 1-d)
}

// mixTheme funde dos temas.
func mixTheme(a, b Theme, t float64) Theme {
	return Theme{mix(a.Bg, b.Bg, t), mix(a.Bg2, b.Bg2, t), mix(a.Text, b.Text, t), mix(a.Dim, b.Dim, t),
		mix(a.Accent, b.Accent, t), mix(a.Accent2, b.Accent2, t), mix(a.Shadow, b.Shadow, t)}
}

// pickColorful elige n álbumes con portada y colores bien distintos: lee los
// colores de los primeros que haya y se queda, uno a uno, con el más lejano
// en tono de los ya elegidos.
func (a *App) pickColorful(n int) []*Album {
	var cands []*Album
	for _, al := range a.lib.Albums {
		if al.HasCover() && len(al.Songs) > 0 && al.Title != "Sin álbum" {
			cands = append(cands, al)
			a.cover(al, 16)
		}
		if len(cands) == 80 {
			break
		}
	}
	for end := time.Now().Add(20 * time.Second); len(a.covers.pending) > 0 && time.Now().Before(end); {
		a.covers.collect(a.t - 10)
		time.Sleep(10 * time.Millisecond)
	}
	var withColor []*Album
	for _, al := range cands {
		if _, _, ok := al.Accent(); ok {
			withColor = append(withColor, al)
		}
	}
	var out []*Album
	for len(out) < n && len(withColor) > 0 {
		best, bestD := 0, -1.0
		for i, al := range withColor {
			c1, _, _ := al.Accent()
			d := 1.0
			for _, o := range out {
				oc, _, _ := o.Accent()
				d = math.Min(d, hueDist(hue(c1), hue(oc)))
			}
			// Mejor si además tiene saturación (un gris no luce).
			if r, g, b := float64(c1.R), float64(c1.G), float64(c1.B); math.Max(r, math.Max(g, b))-math.Min(r, math.Min(g, b)) < 60 {
				d -= 0.3
			}
			if d > bestD {
				best, bestD = i, d
			}
		}
		out = append(out, withColor[best])
		withColor = append(withColor[:best], withColor[best+1:]...)
	}
	return out
}

// writeColorShow guarda la animación: 1,6 s por álbum a 20 cuadros por
// segundo, con el color fundiéndose en 0,4 s al cambiar.
func (a *App) writeColorShow(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	albums := a.pickColorful(4)
	if len(albums) < 2 {
		return nil
	}
	// Las portadas al tamaño del reproductor a 360 de ancho, ya cargadas (y
	// sin su fundido de entrada: como si hiciera rato que llegaron).
	size := min(128, 360*34/100)
	for _, al := range albums {
		a.cover(al, size)
	}
	for end := time.Now().Add(10 * time.Second); len(a.covers.pending) > 0 && time.Now().Before(end); {
		a.covers.collect(a.t - 10)
		time.Sleep(10 * time.Millisecond)
	}
	a.screen = NowPlaying
	frame := 0
	prev := a.theme
	for i, al := range albums {
		c1, c2, _ := al.Accent()
		next := ThemeFor(c1, c2)
		a.playing, a.playingIdx, a.playingSong = al, 0, al.Songs[0]
		a.position, a.duration = 20+float64(i*37), 200
		a.juiceTrack(1)
		start := a.t
		for k := 0; k < 32; k++ {
			a.t = start + float64(k)/20
			a.beatHit = a.updateBeat()
			a.updatePump()
			a.theme = mixTheme(prev, next, math.Min(1, float64(k)/8))
			if err := a.renderShot(filepath.Join(dir, fmt.Sprintf("f%04d.png", frame)), 360); err != nil {
				return err
			}
			frame++
		}
		prev = next
	}
	return nil
}
