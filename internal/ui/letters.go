package ui

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
)

// Saltar por letras: en la biblioteca, L y R van al principio de la letra
// anterior o siguiente (en la pantalla de reproducción siguen siendo canción
// anterior y siguiente), y la letra se ve un momento en grande en el centro.

// letterShow es cuánto se ve la letra después de saltar; letterFade, cuánto
// dura su fundido al final.
const (
	letterShow = 0.7
	letterFade = 0.2
)

// accents pasa las vocales con tilde (y la ñ) a su letra base, para agrupar.
var accents = strings.NewReplacer("Á", "A", "À", "A", "Ä", "A", "Â", "A", "É", "E", "È", "E", "Ë", "E", "Ê", "E",
	"Í", "I", "Ì", "I", "Ï", "I", "Î", "I", "Ó", "O", "Ò", "O", "Ö", "O", "Ô", "O", "Ú", "U", "Ù", "U", "Ü", "U", "Û", "U",
	"Ñ", "N", "Ç", "C")

// letterOf es la letra por la que se agrupa s: su primera letra en
// mayúscula y sin tilde, o "#" si empieza por un número u otra cosa.
func letterOf(s string) string {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			l := accents.Replace(string(unicode.ToUpper(r)))
			if l >= "A" && l <= "Z" {
				return l
			}
			return "#"
		case unicode.IsDigit(r):
			return "#"
		}
	}
	return "#"
}

// letterKey es el texto por el que está ordenado el elemento i de la vista, o
// "" si la vista no está en orden alfabético (un álbum o una lista van en su
// orden propio).
func (v *view) letterKey(i int, nested bool) string {
	switch v.kind {
	case vAlbums:
		if nested {
			return v.albums[i].Title // los álbumes de un artista, por título
		}
		return v.albums[i].Artist // todos, por artista
	case vArtists:
		return v.artists[i].Name
	case vPlaylists:
		return v.playlists[i].Name
	case vSongs:
		return v.songs[i].Artist // van por artista y álbum
	case vFolder:
		if i < len(v.dirs) {
			return filepath.Base(v.dirs[i])
		}
		return filepath.Base(v.songs[i-len(v.dirs)].Path)
	}
	return ""
}

// jumpLetter salta al principio de la letra siguiente (dir 1) o anterior
// (dir -1) de la vista actual de la biblioteca.
func (a *App) jumpLetter(dir int) {
	v := a.cur()
	n := v.count()
	nested := len(a.stacks[a.libTab]) > 1
	if n == 0 || v.letterKey(0, nested) == "" {
		return
	}
	letter := func(i int) string { return letterOf(v.letterKey(i, nested)) }
	start := func(i int) int { // el primero de la letra de i
		for i > 0 && letter(i-1) == letter(i) {
			i--
		}
		return i
	}
	i := v.sel
	if dir > 0 {
		cur := letter(i)
		for i < n && letter(i) == cur {
			i++
		}
		if i == n {
			i = 0 // de la última letra, a la primera
		}
	} else {
		i = start(i)
		if i == 0 {
			i = start(n - 1) // de la primera, a la última
		} else {
			i = start(i - 1)
		}
	}
	v.sel = i
	a.tabFocus = false
	a.letter, a.letterAt = letter(i), a.t
}

// letterCanvas es donde se pinta la letra a su tamaño, antes de ampliarla.
var letterCanvas *ebiten.Image

// drawLetter pinta la letra del último salto en el centro, al doble y en un
// recuadro, y la apaga por escalones al final.
func (a *App) drawLetter(dst *ebiten.Image) {
	age := a.t - a.letterAt
	if a.letter == "" || age > letterShow {
		return
	}
	alpha := float32(1)
	if f := (letterShow - age) / letterFade; f < 1 {
		alpha = steps(f)
	}
	if alpha <= 0 {
		return
	}
	k := uint8(255 * alpha)
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	const box = 56
	x, y := (w-box)/2, (h-box)/2
	panel(dst, x+3, y+3, box, box, withAlpha(th.Shadow, k))
	panel(dst, x, y, box, box, withAlpha(mix(th.Bg2, th.Accent, 0.25), k))
	rect(dst, x+1, y, box-2, 1, withAlpha(th.Accent, k))

	// La letra, al doble: en un lienzo a su tamaño y ampliada sin suavizar.
	const cw, ch = 24, 24
	if letterCanvas == nil {
		letterCanvas = ebiten.NewImage(cw, ch)
	}
	letterCanvas.Clear()
	tw := textWidth(a.letter, BigFace)
	// Centrada por sus mayúsculas (bigCap), no por la línea, que reserva sitio
	// para acentos y descendentes y la dejaba subida.
	ly := (ch - bigCap) / 2
	drawText(letterCanvas, a.letter, BigFace, (cw-tw)/2+1, ly+1, th.Shadow)
	drawText(letterCanvas, a.letter, BigFace, (cw-tw)/2, ly, th.Text)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(2, 2)
	op.GeoM.Translate(float64(x+(box-2*cw)/2), float64(y+(box-2*ch)/2))
	op.ColorScale.ScaleAlpha(alpha)
	dst.DrawImage(letterCanvas, op)
}
