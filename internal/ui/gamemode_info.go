package ui

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// La ficha de un juego: manteniendo A en la lista (soltarlo antes lo
// lanza, como siempre). A la izquierda, lo que tenga para ver: la caja, la
// captura y el vídeo (◀ ▶); a la derecha, sus datos y la descripción (▲ ▼).

// gmHoldFrames es cuánto hay que mantener A para la ficha (medio segundo).
const gmHoldFrames = 30

// Lo que se puede ver en la ficha.
const (
	mediaBox = iota
	mediaShot
	mediaVideo
)

// gmInfoState es la ficha abierta.
type gmInfoState struct {
	media  []int // lo que tiene este juego, en orden
	tab    int   // cuál se ve
	scroll int   // la primera línea de la descripción
	video  gmVideo
	shot   *Album
	// paused: se pausó la música para el sonido del vídeo (se vuelve a poner
	// al dejarlo).
	paused bool
}

// gmOpenInfo abre la ficha del juego elegido.
func (a *App) gmOpenInfo() {
	g := &a.gm
	s := g.gmSys()
	_, gm := s.at(s.sel)
	in := &g.info
	in.video.stop()
	*in = gmInfoState{media: []int{mediaBox}}
	cover := gm.Thumbnail
	if cover == "" {
		cover = gm.Image
	}
	if gm.Image != "" && gm.Image != cover {
		if local := g.root.Path(gm.Image); fileOK(local) {
			in.media = append(in.media, mediaShot)
			in.shot = &Album{Title: gm.Name, coverPath: local, logo: true, clear: true, stray: true, passing: true}
		}
	}
	if gm.Video != "" && fileOK(g.root.Path(gm.Video)) {
		in.media = append(in.media, mediaVideo)
	}
	g.screen, g.changeAt = gmInfo, a.t
}

// fileOK dice si el archivo existe.
func fileOK(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// updateGMInfo atiende la ficha.
func (a *App) updateGMInfo() {
	g := &a.gm
	in, inp := &g.info, &a.input
	s := g.gmSys()
	rs, gm := s.at(s.sel)
	switch {
	case inp.Repeat(ActLeft):
		in.tab = (in.tab + len(in.media) - 1) % len(in.media)
		g.changeAt = a.t
	case inp.Repeat(ActRight):
		in.tab = (in.tab + 1) % len(in.media)
		g.changeAt = a.t
	case inp.Repeat(ActUp):
		in.scroll = max(0, in.scroll-1)
	case inp.Repeat(ActDown):
		in.scroll++
	case inp.Pressed(ActBack):
		a.gmVideoOff()
		g.screen = gmGames
		return
	case inp.Pressed(ActShuffle) && in.media[in.tab] == mediaVideo:
		// X: el vídeo a pantalla completa (sigue sonando).
		g.screen = gmVideoFull
		return
	case inp.Pressed(ActShuffle):
		// X: la caja o la captura, a pantalla completa (con zoom).
		g.zoomAl = in.shot
		if in.media[in.tab] == mediaBox {
			g.zoomAl = a.gmCover(gm)
		}
		g.zoom = gmZoom{}
		g.screen = gmZoom2
		return
	case inp.Pressed(ActConfirm):
		a.gmVideoOff()
		a.gmPlay(rs, gm)
		return
	}
	if in.media[in.tab] == mediaVideo {
		a.gmVideoOn(g.root.Path(gm.Video))
	} else {
		a.gmVideoOff()
	}
	in.video.update()
}

// gmVideoOn pone el vídeo (y su sonido): la música, si sonaba, se pausa.
func (a *App) gmVideoOn(path string) {
	in := &a.gm.info
	if in.video.path != path && a.isPlaying && !in.paused {
		a.send("pause")
		in.paused = true
	}
	if a.gm.screen == gmVideoFull {
		w, h := a.gmVideoFullSize()
		in.video.play(path, w, h, gmVideoFullFPS)
		return
	}
	in.video.play(path, gmVideoW, gmVideoH, gmVideoFPS)
}

// gmVideoOff para el vídeo y, si se había pausado la música, la sigue.
func (a *App) gmVideoOff() {
	in := &a.gm.info
	in.video.stop()
	if in.paused {
		a.send("resume")
		in.paused = false
	}
}

// drawGMInfo pinta la ficha.
func (a *App) drawGMInfo(dst *ebiten.Image) {
	g := &a.gm
	in, th := &g.info, a.theme
	s := g.gmSys()
	rs, gm := s.at(s.sel)
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	cx, cy, cw, ch := gmConsoleBox(w, h)
	ch -= 12 // debajo, cuál se ve
	size := min(cw, ch)
	bx, by := cx+(cw-size)/2, cy+(ch-size)/2

	switch in.media[in.tab] {
	case mediaBox:
		cover, r, alpha := a.gmBoxArt(gm, &rs.sys)
		switch {
		case gmFlat(rs):
			a.drawGMFlat(dst, a.gmCover(gm), gm.Name, rs.sys.FullName, bx, by, size)
		case cover == nil && a.gmCoverLoading(gm, 96):
			a.drawGameBox(dst, &g.box, rs.sys.Name, nil, image.Rectangle{}, 0, cx, cy, cw, ch, a.gmSpin())
			a.drawHourglass(dst, cx+cw/2, cy+ch/2)
		default:
			a.drawGameBox(dst, &g.box, rs.sys.Name, cover, r, alpha, cx, cy, cw, ch, a.gmSpin())
		}
	case mediaShot:
		a.drawGMFlat(dst, in.shot, gm.Name, rs.sys.FullName, bx, by, size)
	case mediaVideo:
		v := &in.video
		switch {
		case v.failed:
			msg := T("gx_no_video")
			drawText(dst, msg, SmallFace, cx+(cw-textWidth(msg, SmallFace))/2, cy+ch/2, th.Dim)
		case !v.shown:
			panel(dst, cx, cy+(ch-cw*3/4)/2, cw, cw*3/4, th.Bg2)
			a.drawHourglass(dst, cx+cw/2, cy+ch/2)
		default:
			// Al mayor tamaño entero que quepa (pixelado, sin suavizar).
			k := max(1, min(cw/gmVideoW, ch/gmVideoH))
			fw, fh := float64(cw)/gmVideoW, float64(ch)/gmVideoH
			sc := min(fw, fh)
			if float64(k) <= sc {
				sc = max(sc, float64(k))
			}
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Scale(sc, sc)
			op.GeoM.Translate(float64(cx)+(float64(cw)-gmVideoW*sc)/2, float64(cy)+(float64(ch)-gmVideoH*sc)/2)
			dst.DrawImage(v.img, op)
		}
	}
	// Las pestañas de lo que se puede ver, debajo.
	names := map[int]string{mediaBox: T("gx_media_box"), mediaShot: T("gx_media_shot"), mediaVideo: T("gx_media_video")}
	var labels []string
	for _, m := range in.media {
		labels = append(labels, names[m])
	}
	tw := 0
	for _, l := range labels {
		tw += textWidth(l, SmallFace) + 10
	}
	x := cx + (cw-tw+10)/2
	for i, l := range labels {
		c := th.Dim
		if i == in.tab {
			c = th.Accent
			rect(dst, x, cy+ch+11, textWidth(l, SmallFace), 1, th.Accent)
		}
		drawText(dst, l, SmallFace, x, cy+ch+3, c)
		x += textWidth(l, SmallFace) + 10
	}

	// Los datos, a la derecha.
	lx := margin + cw + 10
	lw := w - lx - margin
	y := topH + 8
	// El logo del juego en vez del nombre, si tiene (mientras llega, el
	// nombre en su sitio, para que nada salte).
	if al := a.gmLogo(gm); al != nil && !al.missing {
		if !a.drawGMLogo(dst, gm, lx, y, lw, gmLogoH, false) {
			drawTextW(dst, gm.Name, BigFace, lx, y+(gmLogoH-12)/2, lw, th.Text)
		}
		y += gmLogoH + 4
	} else {
		drawTextW(dst, gm.Name, BigFace, lx, y, lw, th.Text)
		y += 16
	}
	line := func(s string, c color.RGBA) {
		if s == "" {
			return
		}
		drawTextW(dst, s, SmallFace, lx, y, lw, c)
		y += 10
	}
	var meta []string
	if yr := gm.Year(); yr != "" {
		meta = append(meta, yr)
	}
	if gm.Developer != "" {
		meta = append(meta, gm.Developer)
	}
	if gm.Publisher != "" && gm.Publisher != gm.Developer {
		meta = append(meta, gm.Publisher)
	}
	line(strings.Join(meta, " · "), th.Dim)
	var meta2 []string
	if gm.Genre != "" {
		meta2 = append(meta2, gm.Genre)
	}
	if gm.Players != "" {
		meta2 = append(meta2, T("gx_players", gm.Players))
	}
	if gm.Rating > 0 {
		stars := int(gm.Rating*5 + 0.5)
		meta2 = append(meta2, strings.Repeat("*", stars)+strings.Repeat("·", 5-stars))
	}
	line(strings.Join(meta2, " · "), th.Dim)
	plays := T("gx_never")
	if gm.PlayCount > 0 {
		plays = N("gx_n_plays", gm.PlayCount)
		if !gm.LastPlayed.IsZero() {
			plays += " · " + gm.LastPlayed.Format("02/01/2006")
		}
	}
	line(plays+" · "+rs.sys.FullName, th.Accent)
	y += 4
	// La descripción, desplazable.
	bottom := h - bottomH - gmMiniH - 4
	desc := strings.TrimSpace(gm.Desc)
	if desc == "" {
		desc = T("gx_no_desc")
	}
	lines := wrapLines(desc, SmallFace, lw-6)
	vis := max(1, (bottom-y)/10)
	in.scroll = min(in.scroll, max(0, len(lines)-vis))
	for i := 0; i < vis && in.scroll+i < len(lines); i++ {
		drawText(dst, lines[in.scroll+i], SmallFace, lx, y+i*10, th.Text)
	}
	a.scrollBar(dst, w-margin-2, y-2, vis*10, in.scroll, vis, len(lines))

	a.drawGMNowPlaying(dst)
	a.drawTopBar(dst, fmt.Sprintf("%s · %s", rs.sys.FullName, T("gx_info")))
	hints := []hint{{"<>", T("gx_media")}, {"^v", T("gx_scroll")}, {"A", T("gx_play")}}
	if in.media[in.tab] == mediaVideo {
		hints = append(hints, hint{"X", T("gx_fullscreen")})
	} else {
		hints = append(hints, hint{"X", T("gx_zoom")})
	}
	a.drawHints(dst, append(hints, hint{"B", T("h_back")}))
}

// updateGMVideoFull atiende el vídeo a pantalla completa: B (o X) vuelve a
// la ficha.
func (a *App) updateGMVideoFull() {
	g := &a.gm
	if a.input.Pressed(ActBack) || a.input.Pressed(ActShuffle) {
		g.screen = gmInfo
		return
	}
	// A la resolución de la pantalla (vuelve a empezar, más grande).
	s := g.gmSys()
	if _, gm := s.at(s.sel); gm.Video != "" {
		a.gmVideoOn(g.root.Path(gm.Video))
	}
	g.info.video.update()
}

// drawGMVideoFull pinta el vídeo a toda la pantalla, al mayor tamaño que
// quepa, sobre negro.
func (a *App) drawGMVideoFull(dst *ebiten.Image) {
	g := &a.gm
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	v := &g.info.video
	if a.gmVideoNative() {
		return // lo pinta DrawFinalScreen, a la resolución de la pantalla
	}
	dst.Fill(color.Black)
	switch {
	case v.failed:
		msg := T("gx_no_video")
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h/2, a.theme.Dim)
	case !v.shown:
		a.drawHourglass(dst, w/2, h/2)
	default:
		iw, ih := float64(v.img.Bounds().Dx()), float64(v.img.Bounds().Dy())
		sc := min(float64(w)/iw, float64(h)/ih)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(sc, sc)
		op.GeoM.Translate((float64(w)-iw*sc)/2, (float64(h)-ih*sc)/2)
		op.Filter = ebiten.FilterLinear
		dst.DrawImage(v.img, op)
	}
}
