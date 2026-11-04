package ui

import (
	"fmt"
	_ "image/gif" // las capturas también pueden venir en GIF o BMP
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// El visor de capturas: el sistema de las capturas de pantalla
// (imageviewer) no se lanza con emulatorlauncher, ES las abre en su propio
// visor. Aquí también: en la lista, la captura en plano (no en una caja) y,
// con A, a pantalla completa, pasando de una a otra con izquierda y derecha.
// Pixeladas y tramadas como las portadas.

// gmViewerSystem es el sistema de las capturas.
const gmViewerSystem = "imageviewer"

// isPicture dice si p es una imagen que se puede ver (las capturas también
// pueden ser vídeos o PDF, que no).
func isPicture(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp":
		return true
	}
	return false
}

// updateGMViewer atiende el visor: ◀ ▶ pasan de captura (sin zoom), A
// acerca y, con zoom, las flechas mueven la imagen.
func (a *App) updateGMViewer() {
	g := &a.gm
	s, in := g.gmSys(), &a.input
	n := len(s.games)
	if in.Pressed(ActBack) {
		g.zoom = gmZoom{}
		g.screen = gmGames
		return
	}
	if a.updateZoom(&g.zoom) {
		return
	}
	switch {
	case in.Repeat(ActLeft), in.Repeat(ActUp):
		s.sel = (s.sel + n - 1) % n
		g.changeAt = a.t
	case in.Repeat(ActRight), in.Repeat(ActDown):
		s.sel = (s.sel + 1) % n
		g.changeAt = a.t
	}
	// La siguiente y la anterior, preparándose ya.
	for _, d := range []int{-1, 1} {
		if al := a.gmCover(&s.games[(s.sel+d+n)%n]); al != nil {
			a.cover(al, gmZoomSize)
		}
	}
}

// drawGMViewer: la captura a pantalla completa, con su nombre y cuál es.
func (a *App) drawGMViewer(dst *ebiten.Image) {
	g := &a.gm
	s := g.gmSys()
	gm := g.gmGame()
	a.drawZoomed(dst, a.gmCover(gm), &g.zoom, gm.Name, fmt.Sprintf("%d / %d", s.sel+1, len(s.games)))
	if g.zoom.level > 1 {
		a.drawHints(dst, []hint{{"<>^v", T("gx_pan")}, {"A", T("gx_zoom")}, {"B", T("h_back")}})
	} else {
		a.drawHints(dst, []hint{{"<>", T("h_choose")}, {"A", T("gx_zoom")}, {"B", T("h_back")}})
	}
}

// gmIsViewer dice si el sistema s es el de las capturas.
func gmIsViewer(s *gmSystem) bool { return s != nil && s.sys.Name == gmViewerSystem }

// gmPicture es la imagen de una captura (el propio archivo, si su gamelist
// no tiene otra).
func gmPicture(gm *esdata.Game) string {
	if isPicture(gm.Path) {
		return gm.Path
	}
	return ""
}
