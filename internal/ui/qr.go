package ui

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	qrcode "github.com/skip2/go-qrcode"
)

// authorURL es el GitHub del autor: sale en Ajustes, con su QR.
const authorURL = "https://github.com/juanvillacortac"

// qrImage es el QR de authorURL, un píxel por módulo, con su margen blanco
// (los lectores lo necesitan). Se hace la primera vez que se pide.
var qrImage *ebiten.Image

// authorQR devuelve qrImage, haciéndolo si hace falta.
func authorQR() *ebiten.Image {
	if qrImage != nil {
		return qrImage
	}
	q, err := qrcode.New(authorURL, qrcode.Medium)
	if err != nil {
		return nil
	}
	q.DisableBorder = true
	bits := q.Bitmap()
	const quiet = 2 // margen en módulos; con el panel blanco alrededor basta
	n := len(bits) + 2*quiet
	img := image.NewGray(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y, row := range bits {
		for x, on := range row {
			if on {
				img.SetGray(x+quiet, y+quiet, color.Gray{})
			}
		}
	}
	qrImage = ebiten.NewImageFromImage(img)
	return qrImage
}

// drawQR pinta, sobre Ajustes, un panel con el QR del GitHub del autor y la
// dirección debajo. Los módulos se amplían a un número entero de píxeles para
// que se lean bien.
func (a *App) drawQR(dst *ebiten.Image) {
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, 0, w, h, withAlpha(th.Bg, 0xc0))
	qr := authorQR()
	if qr == nil {
		return
	}
	n := qr.Bounds().Dx()
	room := h - topH - bottomH - 40
	s := max(1, min(room/n, 5))
	size := n * s
	url := authorURL[len("https://"):]
	pw := max(size, textWidth(url, SmallFace)) + 16
	ph := size + 36
	px, py := (w-pw)/2, topH+(h-topH-bottomH-ph)/2
	panel(dst, px, py, pw, ph, th.Bg2)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(s), float64(s))
	op.GeoM.Translate(float64((w-size)/2), float64(py+8))
	dst.DrawImage(qr, op)
	drawText(dst, url, SmallFace, (w-textWidth(url, SmallFace))/2, py+size+12, th.Text)
	drawText(dst, T("qr_scan"), SmallFace, (w-textWidth(T("qr_scan"), SmallFace))/2, py+size+23, th.Dim)
}
