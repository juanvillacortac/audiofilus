package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
)

// La pantalla de la cola: lo que va a sonar, en orden. La cola la lleva el
// servicio; se le pide cada segundo mientras se mira esta pantalla.

// updateQueue atiende los botones en la cola.
func (a *App) updateQueue() {
	in := &a.input
	if a.t-a.queueAt > 1 {
		a.queueAt = a.t
		a.send("queue")
	}
	q := a.queue()
	n := len(q)
	if !a.queueMoved {
		a.queueSel = max(0, a.status.Index)
	}
	switch {
	case in.Repeat(ActDown) && n > 0:
		a.queueSel, a.queueMoved = min(n-1, a.queueSel+1), true
	case in.Repeat(ActUp):
		a.queueSel, a.queueMoved = max(0, a.queueSel-1), true
	case in.Repeat(ActRight) && n > 0:
		a.queueSel, a.queueMoved = min(n-1, a.queueSel+10), true
	case in.Repeat(ActLeft):
		a.queueSel, a.queueMoved = max(0, a.queueSel-10), true
	case in.Pressed(ActConfirm) && n > 0:
		a.send("jump", fmt.Sprint(a.queueSel))
		a.queueMoved = false
	case in.Pressed(ActBack):
		a.goBack()
		a.queueMoved = false
	}
}

// queue es la última cola recibida del servicio.
func (a *App) queue() []string {
	if a.client == nil {
		var out []string
		if a.playing != nil {
			out = paths(a.playing.Songs)
		}
		return out
	}
	return a.client.Queue()
}

// drawQueue pinta la cola, con lo que suena marcado.
func (a *App) drawQueue(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	q := a.queue()
	x, y := margin, topH+8
	cw := w - 2*margin - 6
	bottom := h - bottomH - 36
	const rowH = 13
	rows := max(1, (bottom-y)/rowH)
	cur := a.status.Index
	if a.client == nil {
		cur = a.playingIdx
	}
	sel := a.queueSel
	if !a.queueMoved {
		sel = max(0, cur)
	}
	first := max(0, min(sel-rows/2, len(q)-rows))
	for i := 0; i < rows && first+i < len(q); i++ {
		idx := first + i
		p := q[idx]
		title, artist := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), ""
		if s, ok := a.lib.Song(p); ok {
			title, artist = s.Title, s.Artist
		}
		ry := y + i*rowH
		mainCol, subCol := th.Text, th.Dim
		if idx < cur {
			mainCol = th.Dim // ya sonaron
		}
		if idx == sel {
			panel(dst, x-3, ry-4, cw+6, rowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, x-3, ry-4, 2, rowH, th.Accent)
			subCol = th.Accent
		}
		if idx == cur {
			drawIcon(dst, "note", x, ry-1, th.Accent2)
		} else {
			drawText(dst, fmt.Sprint(idx+1), SmallFace, x, ry, th.Dim)
		}
		mainW := cw * 58 / 100
		drawTextW(dst, title, SmallFace, x+20, ry, mainW-22, mainCol)
		drawTextW(dst, artist, SmallFace, x+mainW, ry, cw-mainW, subCol)
	}
	a.scrollBar(dst, x+cw+4, y-3, rows*rowH, first, rows, len(q))
	if len(q) == 0 {
		msg := T("nothing")
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, y+30, th.Dim)
	}
	a.drawMiniPlayer(dst)
	a.drawTopBar(dst, fmt.Sprintf("%s  %d", T("queue"), len(q)))
	a.drawHints(dst, []hint{{"A", T("h_play")}, {"B", T("h_back")}, {"SEL", T("h_menu")}})
}
