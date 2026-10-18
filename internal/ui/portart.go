package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
)

// La portada del port de PortMaster (portmaster/audiofilus/cover.png): un
// vinilo que sale de su funda, el logo del splash con su ecualizador y la
// firma. Sin portadas de discos de verdad, para poder publicarla. Se pinta a
// 320×240 y se guarda con las capturas (-shots).

// portTagline es la frase de la portada, en inglés (lo que ve la mayoría en
// PortMaster), en dos líneas.
var portTagline = [2]string{"Keep the music going while you game.", "Pocket mode: screen off, music on."}

// drawPortCover pinta la portada en dst (320×240).
func drawPortCover(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := DefaultTheme
	const t = 2.35 // un instante con las letras ya en su sitio
	drawBackground(dst, th, t)

	// El logo, arriba y centrado, con su ecualizador debajo.
	lw, lh := splashLogoSize()
	const ly = 8
	drawLogo(dst, (w-lw*splashScale)/2, ly, t)
	drawLEDs(dst, w/2, ly+lh*splashScale-6, t, 1)

	// Abajo a la izquierda, el vinilo asomando de la funda.
	const sx, sy, ss = 14, 116, 92
	const vr = ss/2 - 6 // el radio del vinilo
	drawVinyl(dst, sx+ss+10, sy+ss/2, vr, 0.6, th.Accent2)
	rect(dst, sx+3, sy+3, ss, ss, th.Shadow)
	sleeve := sleeveArt(ss, th)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(sx, sy)
	dst.DrawImage(sleeve, op)
	frame(dst, sx, sy, ss, ss, withAlpha(th.Text, 0x40))
	rect(dst, sx+1, sy+1, ss-2, 1, withAlpha(th.Text, 0x50)) // brillo de arriba
	// Una nota grande, pixelada, en la funda.
	op = &ebiten.DrawImageOptions{}
	op.GeoM.Scale(7, 7)
	op.GeoM.Translate(float64(sx+ss/2-24), float64(sy+ss/2-24))
	op.ColorScale.ScaleWithColor(withAlpha(th.Bg, 0x90))
	dst.DrawImage(icon("note"), op)

	// A la derecha del vinilo, la frase y la firma.
	right := sx + ss + vr + 16
	cx := right + (w-margin-right)/2
	ty := sy + 22
	for i, line := range portTagline {
		drawTextW(dst, line, SmallFace, max(right, cx-textWidth(line, SmallFace)/2), ty+i*11, w-margin-right, th.Text)
	}
	drawSignature(dst, cx-signatureWidth()/2, h-30, 1)
}

// writePortCover guarda la portada (×3, como las capturas).
func (a *App) writePortCover(path string) error {
	return a.render(path, 320, drawPortCover)
}

// sleeveArt es la funda: un degradado vertical de solo cinco colores, pasando
// de uno a otro con trama de Bayer, como un degradado de 16 bits (con franjas
// finas se veía de alta resolución, fuera de lugar entre los píxeles).
func sleeveArt(size int, th Theme) *ebiten.Image {
	const levels = 5
	var pal [levels]color.RGBA
	for i := range pal {
		pal[i] = mix(th.Bg2, mix(th.Accent, th.Accent2, float64(i)/(levels-1)), 0.55)
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		f := float64(y) / float64(size-1) * (levels - 1)
		for x := 0; x < size; x++ {
			i := int(f + float64(bayer4[y%4][x%4])/16)
			img.SetRGBA(x, y, pal[min(i, levels-1)])
		}
	}
	return ebiten.NewImageFromImage(img)
}

// writeLogo guarda el logo solo, sobre fondo transparente (para la web y el
// README), en el mismo instante que la portada del port. Queda con aire
// alrededor: hay que recortarlo.
func (a *App) writeLogo(path string) error {
	lw, _ := splashLogoSize()
	return a.render(path, lw*splashScale+16, func(dst *ebiten.Image) {
		drawLogo(dst, 8, 8, 2.35)
	})
}

// writeLogoFrames guarda la entrada del logo del splash (letras que caen,
// brillo y ecualizador) a 30 frames por segundo sobre fondo transparente,
// para animarlo en la web. Como writeLogo, con aire alrededor.
func (a *App) writeLogoFrames(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lw, lh := splashLogoSize()
	w := lw*splashScale + 16
	for i := 0; float64(i)/30 < 3; i++ {
		t := float64(i) / 30
		err := a.render(filepath.Join(dir, fmt.Sprintf("f%04d.png", i)), w, func(dst *ebiten.Image) {
			drawLogo(dst, 8, 40, t)
			drawLEDs(dst, w/2, 40+lh*splashScale+2, t, math.Min(1, math.Max(0, (t-0.7)/0.4)))
		})
		if err != nil {
			return err
		}
	}
	return nil
}
