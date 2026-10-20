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
	case a.screen == Tracker && a.tracker != nil && a.tracker.view == tvPhrase && a.tracker.roll:
		own = []hint{{"<>", "tk_g_step"}, {"^v", "tk_g_pitch"}, {"A", "tk_g_toggle"}, {"A+^v", "tk_g_move"}, {"B+A", "tk_g_delete"}, {"X", "tk_g_grid"}, {"Y", "tk_g_play"}}
	case a.screen == Tracker && a.tracker != nil && a.tracker.view == tvSong:
		own = []hint{{"<>^v", "g_dock_move"}, {"A", "tk_g_open"}, {"A A", "tk_g_new"}, {"A+^v", "tk_g_small"}, {"A+<>", "tk_g_big"}, {"B+A", "tk_g_delete"},
			{"B+^v", "tk_g_page"}, {"L1/R1", "tk_g_views"}, {"Y", "tk_g_play"}, {"B+Y", "tk_g_mute"}, {"START", "tk_g_menu"}}
	case a.screen == Tracker && a.tracker != nil && a.tracker.view == tvProject:
		own = []hint{{"^v", "g_dock_move"}, {"A+<>^v", "tk_g_small"}, {"A", "tk_g_name"}, {"L1/R1", "tk_g_views"}, {"Y", "tk_g_play"}, {"START", "tk_g_menu"}}
	case a.screen == Tracker && a.tracker != nil && a.tracker.view == tvWave:
		own = []hint{{"<>", "g_dock_move"}, {"A+^v", "tk_g_small"}, {"A+<>", "tk_g_draw"}, {"B+<>", "tk_g_item"}, {"L1/R1", "tk_g_views"}, {"Y", "tk_g_play"}, {"START", "tk_g_menu"}}
	case a.screen == Tracker:
		own = []hint{{"<>^v", "g_dock_move"}, {"A", "tk_g_put"}, {"A A", "tk_g_new"}, {"A+^v", "tk_g_small"}, {"A+<>", "tk_g_big"}, {"B+A", "tk_g_delete"},
			{"B+<>", "tk_g_item"}, {"L1/R1", "tk_g_views"}, {"Y", "tk_g_play"}, {"B+Y", "tk_g_mute"}, {"X", "tk_g_roll"}, {"START", "tk_g_menu"}}
	case a.screen == Quiz:
		own = []hint{{"<>^v", "gm_choose"}, {"A", "qz_answer"}, {"START", "h_pause"}}
	case a.screen == Snake:
		own = []hint{{"<>^v", "sn_h_turn"}, {"Y", "g_pause"}, {"START", "h_pause"}}
	case a.screen == Shooter:
		own = []hint{{"<>^v", "sh_h_move"}, {"Y", "g_pause"}, {"START", "h_pause"}}
	case a.screen == Duel:
		own = []hint{{"A", "du_g_hit"}, {"<>^v", "du_g_dodge"}, {"Y", "g_pause"}, {"START", "h_pause"}}
	case a.screen == Runner:
		own = []hint{{"A", "ru_g_jump"}, {"v", "ru_g_duck"}, {"Y", "g_pause"}, {"START", "h_pause"}}
	case a.screen == Games:
		own = []hint{{"<>", "gm_choose"}, {"A", "gm_play"}, {"B", "h_back"}}
	case a.screen == Visual:
		own = []hint{{"<>", "h_visual"}, {"A", "g_camera"}, {"B", "h_back"}}
	case a.screen == Modules:
		own = []hint{{"^v", "g_dock_move"}, {"A", "ytm_listen"}, {"START", "md_g_menu"}, {"X", "rd_g_x"}, {"<", "g_tabs"}}
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
	if a.screen == Blocks || a.screen == Rhythm || a.screen == Crypt || a.screen == Quiz || a.screen == Snake || a.screen == Shooter || a.screen == Duel || a.screen == Runner {
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
	cols := [][]section{
		{{"g_here", own}, {"g_everywhere", all}},
		{{"g_sleep_head", guideSleep}, {"g_bg_head", guideBackground}},
	}
	if !a.canSleep() {
		// Los gestos del sleep y los atajos de FN en segundo plano son de
		// las portátiles.
		cols = cols[:1]
	}
	// En vertical, si todo cabe en una columna, va en una: en dos, a 240 de
	// ancho, no se leen.
	tall := 0
	for _, secs := range cols {
		for _, sec := range secs {
			tall += headH + 3 + len(sec.rows)*rowH
		}
	}
	if w < 300 && topH+4+tall <= h-bottomH {
		cols = [][]section{append(cols[0], cols[1]...)}
	}
	// Lo que no cabe en el alto de una columna sigue en otra (la cabecera de
	// la sección, repetida). Si las columnas no caben a lo ancho (a 110 px ya
	// no se leen), se quitan las secciones del final: lo de esta pantalla va
	// primero.
	top, bottom := topH+4, h-bottomH-2
	type entry struct {
		head string
		row  hint
	}
	flow := func(cols [][]section) [][]entry {
		var out [][]entry
		for _, secs := range cols {
			var col []entry
			y := top
			for _, sec := range secs {
				if y+headH+rowH > bottom && len(col) > 0 {
					out, col, y = append(out, col), nil, top
				}
				col = append(col, entry{head: sec.head})
				y += headH
				for _, r := range sec.rows {
					if y+rowH > bottom {
						out, col, y = append(out, col), []entry{{head: sec.head}}, top+headH
					}
					col = append(col, entry{row: r})
					y += rowH
				}
				y += 3
			}
			if len(col) > 0 {
				out = append(out, col)
			}
		}
		return out
	}
	laid := flow(cols)
	for len(laid) > 1 && (w-(len(laid)+1)*margin)/len(laid) < 110 {
		last := cols[len(cols)-1]
		if len(last) > 1 {
			cols[len(cols)-1] = last[:len(last)-1]
		} else if len(cols) > 1 {
			cols = cols[:len(cols)-1]
		} else {
			break
		}
		laid = flow(cols)
	}
	colW := (w - (len(laid)+1)*margin) / len(laid)
	for c, col := range laid {
		x := margin + c*(colW+margin)
		y := top
		for i, e := range col {
			if e.head != "" {
				if i > 0 {
					y += 3
				}
				drawText(dst, T(e.head), SmallFace, x, y, th.Accent)
				rect(dst, x, y+9, colW, 1, withAlpha(th.Dim, 0x50))
				y += headH
				continue
			}
			bw := a.drawBtnChip(dst, a.btn(e.row.btn), x, y-2)
			drawTextW(dst, T(e.row.label), SmallFace, x+bw+4, y, colW-bw-4, th.Text)
			y += rowH
		}
	}
	rect(dst, 0, h-bottomH, w, bottomH, th.Bg)
	msg := T("g_release")
	drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, h-bottomH+4, th.Dim)
}
