package ui

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Framed pone la interfaz en la ventana del escritorio a una escala entera
// (×2, ×3...): así los píxeles se ven duros y todos del mismo tamaño a
// cualquier tamaño de ventana. A escala no entera, FilterPixelated deja los
// bordes suaves y ningún filtro puede evitar que unos píxeles midan uno más
// que otros. En vez de bandas, el lienzo toma todo lo que mide la ventana a esa
// escala (las pantallas se acomodan a cualquier ancho y alto, como en el
// móvil). En las portátiles (y mientras no se sabe dónde corre) se queda como
// siempre (ViewSize): sus pantallas ya son de escala entera.
type Framed struct{ *App }

// NewFramed envuelve la interfaz para ebiten.RunGame en el escritorio.
func NewFramed(a *App) *Framed { return &Framed{a} }

// LayoutF elige la escala entera más grande a la que cabe el lado corto (240)
// y da el lienzo de la ventana entera a esa escala. Trabaja en píxeles
// físicos, así en una pantalla HiDPI también es entera. Una ventana que no da
// para ×2 (menos de 480 de lado corto) se queda con ViewSize, estirada como
// antes: a ×1 la interfaz se vería diminuta.
func (f *Framed) LayoutF(w, h float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	pw, ph := w*s, h*s
	if f.App.status.Platform != "desktop" || math.Min(pw, ph) < 2*ScreenH {
		lw, lh := f.App.Layout(int(w), int(h))
		return float64(lw), float64(lh)
	}
	k := math.Floor(math.Min(pw, ph) / ScreenH)
	f.App.w, f.App.h = int(pw/k), int(ph/k)
	return pw / k, ph / k
}

// Layout no se usa (ebiten llama a LayoutF), pero lo pide ebiten.Game.
func (f *Framed) Layout(w, h int) (int, int) {
	lw, lh := f.LayoutF(float64(w), float64(h))
	return int(lw), int(lh)
}
