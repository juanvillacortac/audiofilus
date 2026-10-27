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
// para ×2 (menos de 480 de lado corto) va a la escala justa para que ese lado
// mida 240, sin redondear (a ×1 la interfaz se vería diminuta); también ahí el
// lienzo toma toda la ventana, sin el tope de 21:9 de ViewSize, así una
// ventana muy apaisada no queda con bandas.
func (f *Framed) LayoutF(w, h float64) (float64, float64) {
	if f.App.status.Platform != "desktop" {
		lw, lh := f.App.Layout(int(w), int(h))
		return float64(lw), float64(lh)
	}
	s := ebiten.Monitor().DeviceScaleFactor()
	pw, ph := w*s, h*s
	k := math.Min(pw, ph) / ScreenH
	if k >= 2 {
		k = math.Floor(k)
	}
	lw, lh := math.Round(pw/k), math.Round(ph/k)
	f.App.w, f.App.h = int(lw), int(lh)
	return lw, lh
}

// Layout no se usa (ebiten llama a LayoutF), pero lo pide ebiten.Game.
func (f *Framed) Layout(w, h int) (int, int) {
	lw, lh := f.LayoutF(float64(w), float64(h))
	return int(lw), int(lh)
}
