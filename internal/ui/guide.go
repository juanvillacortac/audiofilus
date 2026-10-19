package ui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// La guía de controles: mientras se mantiene FN (F1 en el teclado) se ve un
// panel con lo que hace cada botón en la pantalla actual y en todas. Con FN
// mantenido no se atiende ningún otro botón, porque Knulli usa FN+botón para
// sus atajos (brillo, volumen...) y no deben hacer nada aquí.

// updateGuide abre o cierra la guía; devuelve true mientras se ve.
func (a *App) updateGuide() bool {
	if a.input.duration(ActGuide) > 0 {
		if a.guideAt == 0 {
			a.guideAt = a.t
		}
		return true
	}
	a.guideAt = 0
	return false
}

// guideRows son los controles de la pantalla actual, primero los suyos.
func (a *App) guideRows() (own, all []hint) {
	switch {
	case a.dock.open:
		own = []hint{{"<>", "g_dock_move"}, {"SEL/A", "g_dock_go"}, {"B", "h_close"}}
	case a.screen == NowPlaying:
		own = []hint{{"A/Y", "g_pause"}, {"<>", "g_tap_skip"}, {"<> ...", "g_hold_seek"}, {"X", "g_shuffle"}, {"X X", "g_random"}, {"B", "g_library"}}
	case a.screen == LibraryScreen && a.searchTyping():
		own = []hint{{"A", "h_type"}, {"B", "h_del"}, {"X", "h_space"}, {"Y", "g_clear"}, {"START", "h_search"}}
	case a.screen == LibraryScreen:
		own = []hint{{"A", "g_open"}, {"B", "h_back"}, {"X", "g_play_shuffle"}, {"X X", "g_random"}, {"L1/R1", "g_letters"}, {"<", "g_tabs"}}
	case a.screen == Queue:
		own = []hint{{"A", "h_play"}, {"<>", "g_page"}, {"B", "h_back"}}
	case a.screen == Blocks:
		own = []hint{{"<>", "g_move"}, {"v", "g_soft"}, {"^", "g_hard"}, {"A/B", "h_rotate"}, {"X", "h_hold"}, {"START", "h_pause"}}
	case a.screen == Rhythm:
		own = []hint{{"< v B A", "g_lanes"}, {"START", "h_pause"}}
	case a.screen == Crypt:
		own = []hint{{"<>^v", "g_cr_move"}, {"<>^v", "g_cr_attack"}, {"START", "h_pause"}}
	case a.screen == Games:
		own = []hint{{"<>", "gm_choose"}, {"A", "gm_play"}, {"B", "h_back"}}
	case a.screen == Visual:
		own = []hint{{"<>", "h_visual"}, {"A", "g_camera"}, {"B", "h_back"}}
	case a.screen == Podcasts:
		own = []hint{{"^v", "g_dock_move"}, {"A", "pd_g_a"}, {"START", "pd_g_menu"}, {"<>", "g_page"}, {"B", "h_back"}}
	case a.screen == Radio:
		own = []hint{{"^v", "g_dock_move"}, {"A", "ytm_listen"}, {"START", "rd_g_menu"}, {"X", "rd_g_x"}, {"<", "g_tabs"}}
	case a.screen == Downloads:
		own = []hint{{"^v", "g_dock_move"}, {"A", "g_dl_open"}, {"B", "h_back"}}
	case a.screen == Settings:
		own = []hint{{"<>^v", "g_dock_move"}, {"A", "h_change"}, {"B", "h_back"}}
	}
	all = []hint{{"SEL", "g_screens"}, {"SEL SEL", "g_prev_screen"}, {"START", "g_more"}, {"Y", "g_pause"}, {"L1/R1", "g_skip"}, {"L2/R2", "g_seek"}}
	if a.screen == Blocks || a.screen == Rhythm || a.screen == Crypt {
		all[2] = hint{"START", "h_pause"}
	}
	return own, all
}

// guideSleep y guideBackground son los controles del sleep (los gestos de los
// gatillos, que atiende el servicio con la pantalla apagada) y del segundo
// plano (el atajo de FN, que funciona incluso dentro de los juegos).
var (
	guideSleep = []hint{
		{"START START", "g_sl_enter"}, {"L1/R1 x2", "g_skip"}, {"L2/R2 x2", "g_seek"},
		{"L1+L2 x2", "g_pause"}, {"R1+R2 x2", "g_random"}, {"START", "g_sl_wake"},
	}
	guideBackground = []hint{
		{"START", "g_bg_enter"}, {"FN x3 L1/R1", "g_skip"}, {"FN x3 L2/R2", "g_seek"}, {"FN x3 SEL", "g_pause"},
	}
)

// drawGuide pinta la guía mientras se mantiene FN, en dos columnas: a la
// izquierda esta pantalla y todas; a la derecha el sleep y el segundo plano.
// Cada fila lleva su botón en una pastilla.
func (a *App) drawGuide(dst *ebiten.Image) {
	if a.guideAt == 0 {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	own, all := a.guideRows()
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xe8))
	const rowH, headH = 11, 14
	type section struct {
		head string
		rows []hint
	}
	cols := [2][]section{
		{{"g_here", own}, {"g_everywhere", all}},
		{{"g_sleep_head", guideSleep}, {"g_bg_head", guideBackground}},
	}
	colW := (w - 3*margin) / 2
	for c, secs := range cols {
		x := margin + c*(colW+margin)
		y := topH + 4
		for _, sec := range secs {
			drawText(dst, T(sec.head), SmallFace, x, y, th.Accent)
			rect(dst, x, y+9, colW, 1, withAlpha(th.Dim, 0x50))
			y += headH
			for _, r := range sec.rows {
				bw := a.drawBtnChip(dst, a.btn(r.btn), x, y-2)
				drawTextW(dst, T(r.label), SmallFace, x+bw+4, y, colW-bw-4, th.Text)
				y += rowH
			}
			y += 3
		}
	}
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg)
	msg := T("g_release")
	drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h-bottomH+4, th.Dim)
}
