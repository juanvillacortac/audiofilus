package ui

import (
	"image/color"
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
type Framed struct {
	*App
	k      float64     // la escala entera elegida (0: no entera, la estira ebiten)
	cursor mouseCursor // el puntero del ratón, pintado (mousecursor.go)
}

// NewFramed envuelve la interfaz para ebiten.RunGame en el escritorio.
func NewFramed(a *App) *Framed { return &Framed{App: a} }

// Update es el de la interfaz, y luego decide si se ve el puntero.
func (f *Framed) Update() error {
	err := f.App.Update()
	f.cursor.update(f.App)
	return err
}

// LayoutF elige la escala entera más grande a la que cabe el lado corto (240)
// y da el lienzo de la ventana entera a esa escala. Trabaja en píxeles
// físicos, así en una pantalla HiDPI también es entera. Una ventana que no da
// para ×2 (menos de 480 de lado corto) va a la escala justa para que ese lado
// mida 240, sin redondear (a ×1 la interfaz se vería diminuta); también ahí el
// lienzo toma toda la ventana, sin el tope de 21:9 de ViewSize, así una
// ventana muy apaisada no queda con bandas.
func (f *Framed) LayoutF(w, h float64) (float64, float64) {
	f.k = 0
	if f.App.status.Platform != "desktop" || handheldUI {
		lw, lh := f.App.Layout(int(w), int(h))
		return float64(lw), float64(lh)
	}
	s := ebiten.Monitor().DeviceScaleFactor()
	pw, ph := w*s, h*s
	k := math.Min(pw, ph) / ScreenH
	lw, lh := math.Round(pw/k), math.Round(ph/k)
	if k >= 2 {
		// El lienzo, redondeado hacia abajo: si la ventana no es múltiplo
		// de la escala (1366 a ×3), sobra un píxel que DrawFinalScreen
		// deja al borde, en vez de estirar a ×3,002.
		k = math.Floor(k)
		f.k = k
		lw, lh = math.Floor(pw/k), math.Floor(ph/k)
	}
	f.App.w, f.App.h = int(lw), int(lh)
	return lw, lh
}

// DrawFinalScreen pone el lienzo a la escala entera exacta, centrado, con
// lo que sobra (un píxel o dos) del color del fondo; sin escala entera, como
// siempre (App.DrawFinalScreen).
func (f *Framed) DrawFinalScreen(screen ebiten.FinalScreen, offscreen *ebiten.Image, geoM ebiten.GeoM) {
	if f.k == 0 {
		f.App.DrawFinalScreen(screen, offscreen, geoM)
		f.cursor.draw(screen, f.App, geoM, max(1, math.Round(geoM.Element(0, 0))))
		return
	}
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()
	ow, oh := offscreen.Bounds().Dx(), offscreen.Bounds().Dy()
	if f.App.Asleep() {
		screen.Fill(color.Black)
	} else {
		screen.Fill(f.App.theme.Bg)
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(f.k, f.k)
	op.GeoM.Translate(math.Floor((float64(sw)-float64(ow)*f.k)/2), math.Floor((float64(sh)-float64(oh)*f.k)/2))
	op.Filter = ebiten.FilterNearest
	screen.DrawImage(offscreen, op)
	f.cursor.draw(screen, f.App, op.GeoM, f.k)
}

// Layout no se usa (ebiten llama a LayoutF), pero lo pide ebiten.Game.
func (f *Framed) Layout(w, h int) (int, int) {
	lw, lh := f.LayoutF(float64(w), float64(h))
	return int(lw), int(lh)
}
