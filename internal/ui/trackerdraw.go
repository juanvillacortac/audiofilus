package ui

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/tracker"
)

// Medidas del tracker.
const (
	tkRowH = 11
	tkGap  = 4
)

// chanName es el nombre corto de un canal: los de la Game Boy o CH1..CH8.
func chanName(s *tracker.Song, ch int) string {
	if s.Mode == tracker.ModeGB {
		return [4]string{"PU1", "PU2", "WAV", "NOI"}[ch]
	}
	return fmt.Sprintf("CH%d", ch+1)
}

// tkCol es una columna de una rejilla: su texto, si tiene valor y su color.
type tkCol struct {
	s  string
	on bool
	c  color.RGBA
}

// drawTkCells pinta una fila de columnas desde x; sel es la columna del
// cursor (-1 ninguna). Devuelve el ancho.
func (a *App) drawTkCells(dst *ebiten.Image, cols []tkCol, x, y, sel int) int {
	th := a.theme
	x0 := x
	for i, c := range cols {
		w := textWidth(c.s, SmallFace)
		col := withAlpha(th.Dim, 0x80)
		if c.on {
			col = c.c
		}
		if i == sel {
			rect(dst, x-1, y-2, w+2, tkRowH, th.Accent)
			col = th.Bg
		}
		drawText(dst, c.s, SmallFace, x, y, col)
		x += w + tkGap
	}
	return x - x0
}

// drawTracker pinta las pestañas y la vista que toque.
func (a *App) drawTracker(dst *ebiten.Image) {
	t, th := a.tracker, a.theme
	w := dst.Bounds().Dx()
	// Las pestañas: el mapa de vistas, con la actual encendida.
	names := tkViewNames[:]
	tw := 0
	for _, n := range names {
		tw += textWidth(n, SmallFace) + 6
	}
	if tw > w-2*margin-10 {
		names, tw = tkViewShort[:], 0 // en pantallas estrechas, de tres letras
		for _, n := range names {
			tw += textWidth(n, SmallFace) + 6
		}
	}
	x, y := max(2, (w-tw)/2), topH+3
	for i, n := range names {
		nw := textWidth(n, SmallFace)
		c := th.Dim
		if tkView(i) == t.view {
			rect(dst, x-2, y-2, nw+4, tkRowH, th.Accent)
			c = th.Bg
		}
		drawText(dst, n, SmallFace, x, y, c)
		x += nw + 6
	}
	if t.playing {
		drawIcon(dst, "play", w-margin-6, y-1, th.Accent)
	}
	switch t.view {
	case tvProject:
		a.drawTkList(dst, a.tkProjectParams(), t.projRow, T("tk_v_project"))
	case tvSong:
		a.drawTkSong(dst)
	case tvChain:
		a.drawTkChain(dst)
	case tvPhrase:
		if t.roll {
			a.drawTkRoll(dst)
		} else {
			a.drawTkPhrase(dst)
		}
	case tvInstr:
		title := fmt.Sprintf("%s %02X", T("tk_v_instr"), t.instr)
		if t.song.Instrs[t.instr] == nil {
			title += " · " + T("tk_empty")
		}
		a.drawTkList(dst, a.tkInstrParams(), t.instrRow, title)
	case tvTable:
		a.drawTkTable(dst)
	case tvGroove:
		a.drawTkGroove(dst)
	case tvWave:
		a.drawTkWave(dst)
	}
	a.drawHints(dst, a.tkHints())
}

// tkHints es la ayuda de abajo de cada vista.
func (a *App) tkHints() []hint {
	t := a.tracker
	switch t.view {
	case tvSong, tvChain:
		return []hint{{"A", T("tk_h_open")}, {"A+<>^v", T("tk_h_edit")}, {"L1/R1", T("tk_h_views")}, {"Y", T("tk_h_play")}}
	case tvPhrase:
		if t.roll {
			return []hint{{"A", T("tk_h_note")}, {"A+^v", T("tk_h_move")}, {"X", T("tk_h_grid")}, {"Y", T("tk_h_play")}}
		}
		return []hint{{"A+<>^v", T("tk_h_edit")}, {"X", T("tk_h_roll")}, {"L1/R1", T("tk_h_views")}, {"Y", T("tk_h_play")}}
	case tvProject:
		return []hint{{"A+<>^v", T("tk_h_edit")}, {"L1/R1", T("tk_h_views")}, {"Y", T("tk_h_play")}}
	case tvWave:
		return []hint{{"A+^v", T("tk_h_edit")}, {"A+<>", T("tk_h_draw")}, {"B+<>", T("tk_h_other")}, {"Y", T("tk_h_play")}}
	}
	return []hint{{"A+<>^v", T("tk_h_edit")}, {"B+<>", T("tk_h_other")}, {"L1/R1", T("tk_h_views")}, {"Y", T("tk_h_play")}}
}

// tkTitle pinta el título de la vista (debajo de las pestañas) y, a la
// derecha, el osciloscopio del canal en que se trabaja. Devuelve la y de la
// rejilla.
func (a *App) tkTitle(dst *ebiten.Image, title string, scope bool) int {
	t, th := a.tracker, a.theme
	w := dst.Bounds().Dx()
	y := topH + 16
	drawText(dst, title, SmallFace, margin, y, th.Text)
	if scope {
		name := chanName(t.song, t.ch)
		sw := 40
		x := w - margin - sw
		a.drawTkScope(dst, t.ch, x, y-2, sw)
		drawText(dst, name, SmallFace, x-textWidth(name, SmallFace)-4, y, th.Dim)
	}
	return y + 14
}

// drawTkScope pinta la onda que suena ahora en el canal ch.
func (a *App) drawTkScope(dst *ebiten.Image, ch, x, y, w int) {
	t, th := a.tracker, a.theme
	rect(dst, x, y, w, tkRowH, th.Bg2)
	if t.player.Muted()[ch] {
		rect(dst, x+2, y+5, w-4, 1, withAlpha(th.Dim, 0x80))
		return
	}
	if len(t.scope) != w {
		t.scope = make([]float32, w)
	}
	t.player.Scope(ch, t.scope)
	sc := th.Accent
	if ch%4 >= 2 {
		sc = th.Accent2
	}
	prev := 0
	for i, v := range t.scope {
		yy := max(-4, min(4, int(math.Round(float64(-v)*9))))
		lo, hi := min(prev, yy), max(prev, yy)
		if i == 0 {
			lo, hi = yy, yy
		}
		rect(dst, x+i, y+5+lo, 1, hi-lo+1, sc)
		prev = yy
	}
}

// tkRowBand pinta el fondo de una fila: más fuerte en cada tiempo, y la que
// suena.
func (a *App) tkRowBand(dst *ebiten.Image, x, y, w, r int, playing bool) {
	th := a.theme
	switch {
	case playing:
		rect(dst, x-2, y-2, w+4, tkRowH, withAlpha(th.Accent, 0x60))
	case r%4 == 0:
		rect(dst, x-2, y-2, w+4, tkRowH, withAlpha(th.Bg2, 0xa0))
	}
}

// drawTkSong pinta la canción: filas × canales de cadenas, con lo que suena
// en cada canal.
func (a *App) drawTkSong(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	y0 := a.tkTitle(dst, fmt.Sprintf("%s · %d BPM", s.Name, s.BPM), false)
	n := s.Channels()
	cellW := textWidth("00", SmallFace)
	colW := max(cellW+10, min(42, (w-2*margin-20)/n))
	x0 := max(margin, (w-(20+n*colW))/2)
	// Los canales, con su osciloscopio.
	muted := t.player.Muted()
	for c := 0; c < n; c++ {
		x := x0 + 20 + c*colW
		col := th.Dim
		if c == t.ch {
			col = th.Text
		}
		if muted[c] {
			col = withAlpha(th.Dim, 0x60)
		}
		drawText(dst, chanName(s, c), SmallFace, x, y0, col)
	}
	y0 += 11
	for c := 0; c < n; c++ {
		a.drawTkScope(dst, c, x0+20+c*colW, y0-2, colW-6)
	}
	y0 += 13
	rows := (h - bottomH - y0) / tkRowH
	if t.songRow >= t.songScroll+rows {
		t.songScroll = t.songRow - rows + 1
	}
	if t.songRow < t.songScroll {
		t.songScroll = t.songRow
	}
	var pos [tracker.MaxChannels]int
	var on [tracker.MaxChannels]bool
	for c := 0; c < n; c++ {
		pos[c], _, _, on[c] = t.player.Position(c)
		on[c] = on[c] && t.playWhat == tracker.PlaySong
	}
	for r := t.songScroll; r < t.songScroll+rows && r <= len(s.Rows) && r < tracker.SongRows; r++ {
		y := y0 + (r-t.songScroll)*tkRowH
		if r%4 == 0 {
			rect(dst, x0-2, y-2, 20+n*colW, tkRowH, withAlpha(th.Bg2, 0xa0))
		}
		rc := th.Dim
		if r%4 == 0 {
			rc = th.Text
		}
		drawText(dst, hexStr(r, 2), SmallFace, x0, y, rc)
		for c := 0; c < n; c++ {
			x := x0 + 20 + c*colW
			id := tracker.Empty
			if r < len(s.Rows) {
				id = int(s.Rows[r][c])
			}
			str, col := "--", withAlpha(th.Dim, 0x80)
			if id != tracker.Empty {
				str, col = hexStr(id, 2), th.Text
			}
			if on[c] && pos[c] == r {
				rect(dst, x-2, y-2, cellW+4, tkRowH, withAlpha(th.Accent, 0x70))
				if d := a.t - t.hitAt[c]; d < 0.25 {
					rect(dst, x-2, y-2, cellW+4, tkRowH, withAlpha(th.Text, uint8(90*(1-d/0.25))))
				}
			}
			if r == t.songRow && c == t.ch {
				rect(dst, x-1, y-2, cellW+2, tkRowH, th.Accent)
				col = th.Bg
			}
			drawText(dst, str, SmallFace, x, y, col)
		}
	}
}

// drawTkChain pinta la cadena: 16 pasos de frase y transposición.
func (a *App) drawTkChain(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w := dst.Bounds().Dx()
	title := fmt.Sprintf("%s %02X", T("tk_v_chain"), t.chain)
	c := s.Chains[t.chain]
	if c == nil {
		title += " · " + T("tk_empty")
	}
	y0 := a.tkTitle(dst, title, true)
	nowChain, nowPhrase := t.player.Now(t.ch)
	_, playPos, _, _ := t.player.Position(t.ch)
	rowW := textWidth("00  00 +00", SmallFace) + 6
	x0 := max(margin, (w-rowW)/2)
	for r := 0; r < tracker.Steps; r++ {
		y := y0 + r*tkRowH
		st := tracker.ChainStep{Phrase: tracker.Empty}
		if c != nil {
			st = c[r]
		}
		a.tkRowBand(dst, x0, y, rowW, r, nowChain == t.chain && nowPhrase != tracker.Empty && playPos == r)
		drawText(dst, hexStr(r, 1), SmallFace, x0, y, th.Dim)
		ph := "--"
		if st.Phrase != tracker.Empty {
			ph = hexStr(int(st.Phrase), 2)
		}
		sel := -1
		if r == t.chainRow {
			sel = t.chainCol
		}
		a.drawTkCells(dst, []tkCol{{ph, st.Phrase != tracker.Empty, th.Text}, {signedHex(int(st.Transpose)), st.Transpose != 0, th.Accent2}},
			x0+textWidth("00", SmallFace)+6, y, sel)
	}
}

// drawTkPhrase pinta la frase: nota, instrumento, volumen y dos comandos
// por paso.
func (a *App) drawTkPhrase(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w := dst.Bounds().Dx()
	title := fmt.Sprintf("%s %02X", T("tk_v_phrase"), t.phrase)
	p := s.Phrases[t.phrase]
	if p == nil {
		title += " · " + T("tk_empty")
	}
	y0 := a.tkTitle(dst, title, true)
	_, nowPhrase := t.player.Now(t.ch)
	_, _, playStep, _ := t.player.Position(t.ch)
	rowW := textWidth("0  C-4 00 F C 00 C 00", SmallFace) + 7*tkGap
	x0 := max(margin, (w-rowW)/2)
	for r := 0; r < tracker.Steps; r++ {
		y := y0 + r*tkRowH
		st := tracker.Step{Instr: tracker.Empty, Vol: tracker.Empty}
		if p != nil {
			st = p[r]
		}
		playing := nowPhrase == t.phrase && playStep == r
		a.tkRowBand(dst, x0, y, rowW, r, playing)
		if playing && st.Note > 0 {
			if d := a.t - t.hitAt[t.ch]; d < 0.25 {
				rect(dst, x0-2, y-2, rowW+4, tkRowH, withAlpha(th.Text, uint8(80*(1-d/0.25))))
			}
		}
		drawText(dst, hexStr(r, 1), SmallFace, x0, y, th.Dim)
		instr, vol := "--", "-"
		if st.Instr != tracker.Empty {
			instr = hexStr(int(st.Instr), 2)
		}
		if st.Vol != tracker.Empty {
			vol = hexStr(int(st.Vol), 1)
		}
		cols := []tkCol{{tracker.NoteName(st.Note), st.Note != tracker.NoteEmpty, th.Text}, {instr, st.Instr != tracker.Empty, th.Accent2}, {vol, st.Vol != tracker.Empty, th.Accent}}
		cols = append(cols, cmdCols(st.Cmds, th)...)
		sel := -1
		if r == t.phRow {
			sel = t.phCol
		}
		a.drawTkCells(dst, cols, x0+textWidth("0", SmallFace)+6, y, sel)
	}
}

// cmdCols son las columnas de dos comandos: letra y argumento de cada uno.
func cmdCols(cmds [2]tracker.Cmd, th Theme) []tkCol {
	var out []tkCol
	for _, c := range cmds {
		code, arg := "-", "--"
		if !c.None() {
			code, arg = string(rune(c.Code)), hexStr(int(c.Arg), 2)
		}
		out = append(out, tkCol{code, !c.None(), th.Accent}, tkCol{arg, !c.None(), th.Text})
	}
	return out
}

// drawTkRoll pinta la frase como piano roll: los pasos en horizontal, las
// notas en vertical (las teclas negras más oscuras), cada nota hasta la
// siguiente.
func (a *App) drawTkRoll(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	y0 := a.tkTitle(dst, fmt.Sprintf("%s %02X · %s", T("tk_v_phrase"), t.phrase, tracker.NoteName(int8(max(1, t.rollNote)))), true)
	const keyH = 6
	rows := (h - bottomH - y0 - 2) / keyH
	// Con un margen de tres semitonos; si la nota quedó lejos (al abrir),
	// centrada.
	switch {
	case t.rollNote < t.rollBase-rows || t.rollNote > t.rollBase+2*rows:
		t.rollBase = t.rollNote - rows/2
	case t.rollNote >= t.rollBase+rows-3:
		t.rollBase = t.rollNote - rows + 4
	case t.rollNote < t.rollBase+3:
		t.rollBase = t.rollNote - 3
	}
	t.rollBase = max(1, min(tracker.NoteMax-rows+1, t.rollBase))
	labelW := textWidth("C-4", SmallFace) + 4
	stepW := max(6, min(20, (w-2*margin-labelW)/tracker.Steps))
	x0 := max(margin, (w-labelW-stepW*tracker.Steps)/2) + labelW
	gridH := rows * keyH
	// Las teclas.
	for i := 0; i < rows; i++ {
		n := t.rollBase + rows - 1 - i
		y := y0 + i*keyH
		switch (n - 1) % 12 {
		case 1, 3, 6, 8, 10:
			rect(dst, x0, y, stepW*tracker.Steps, keyH, withAlpha(th.Bg, 0xc0))
		default:
			rect(dst, x0, y, stepW*tracker.Steps, keyH, withAlpha(th.Bg2, 0x90))
		}
		if (n-1)%12 == 0 {
			rect(dst, x0, y+keyH-1, stepW*tracker.Steps, 1, withAlpha(th.Dim, 0x60))
			drawText(dst, tracker.NoteName(int8(n)), SmallFace, x0-labelW, y-1, th.Dim)
		}
	}
	for st := 0; st <= tracker.Steps; st += 4 {
		rect(dst, x0+st*stepW, y0, 1, gridH, withAlpha(th.Dim, 0x50))
	}
	// Lo que suena.
	_, nowPhrase := t.player.Now(t.ch)
	_, _, playStep, _ := t.player.Position(t.ch)
	if nowPhrase == t.phrase {
		rect(dst, x0+playStep*stepW, y0, stepW, gridH, withAlpha(th.Accent, 0x30))
	}
	// Las notas: cada una dura hasta la siguiente nota o el corte.
	if p := s.Phrases[t.phrase]; p != nil {
		for st := 0; st < tracker.Steps; st++ {
			n := int(p[st].Note)
			if n <= 0 {
				continue
			}
			end := st + 1
			for end < tracker.Steps && p[end].Note == tracker.NoteEmpty {
				end++
			}
			if n < t.rollBase || n >= t.rollBase+rows {
				continue
			}
			y := y0 + (t.rollBase+rows-1-n)*keyH
			rect(dst, x0+st*stepW+1, y+1, (end-st)*stepW-1, keyH-1, withAlpha(th.Accent2, 0x90))
			rect(dst, x0+st*stepW+1, y+1, 2, keyH-1, th.Text)
		}
	}
	// El cursor.
	if t.rollNote >= t.rollBase && t.rollNote < t.rollBase+rows {
		y := y0 + (t.rollBase+rows-1-t.rollNote)*keyH
		x := x0 + t.phRow*stepW
		rect(dst, x, y, stepW, 1, th.Accent)
		rect(dst, x, y+keyH-1, stepW, 1, th.Accent)
		rect(dst, x, y, 1, keyH, th.Accent)
		rect(dst, x+stepW-1, y, 1, keyH, th.Accent)
	}
}

// drawTkList pinta una vista de lista (proyecto, instrumento): nombre y
// valor por fila.
func (a *App) drawTkList(dst *ebiten.Image, ps []tkParam, row int, title string) {
	t, th := a.tracker, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	y0 := a.tkTitle(dst, title, t.view == tvInstr)
	labelW := 0
	for _, p := range ps {
		labelW = max(labelW, textWidth(p.label, SmallFace))
	}
	x0 := max(margin, w/2-labelW-30)
	// Si no caben, en dos columnas.
	rows := (h - bottomH - y0) / tkRowH
	colW := labelW + 70
	for i, p := range ps {
		x, y := x0, y0+i*tkRowH
		if i >= rows {
			x, y = x0+colW, y0+(i-rows)*tkRowH
		}
		if len(ps) > rows {
			x -= colW / 2
		}
		drawText(dst, p.label, SmallFace, x, y, th.Dim)
		vx := x + labelW + 8
		vw := textWidth(p.show, SmallFace)
		c := th.Text
		if i == row {
			rect(dst, vx-2, y-2, vw+4, tkRowH, th.Accent)
			c = th.Bg
		}
		drawText(dst, p.show, SmallFace, vx, y, c)
	}
}

// drawTkTable pinta la tabla: volumen, transposición y dos comandos por
// fila (una por tick).
func (a *App) drawTkTable(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w := dst.Bounds().Dx()
	title := fmt.Sprintf("%s %02X", T("tk_v_table"), t.table)
	tb := s.Tables[t.table]
	if tb == nil {
		title += " · " + T("tk_empty")
	}
	y0 := a.tkTitle(dst, title, false)
	rowW := textWidth("0  F +00 C 00 C 00", SmallFace) + 6*tkGap
	x0 := max(margin, (w-rowW)/2)
	for r := 0; r < tracker.Steps; r++ {
		y := y0 + r*tkRowH
		row := tracker.TableRow{Vol: tracker.Empty}
		if tb != nil {
			row = tb[r]
		}
		a.tkRowBand(dst, x0, y, rowW, r, false)
		drawText(dst, hexStr(r, 1), SmallFace, x0, y, th.Dim)
		vol := "-"
		if row.Vol != tracker.Empty {
			vol = hexStr(int(row.Vol), 1)
		}
		cols := []tkCol{{vol, row.Vol != tracker.Empty, th.Accent}, {signedHex(int(row.Transpose)), row.Transpose != 0, th.Accent2}}
		cols = append(cols, cmdCols(row.Cmds, th)...)
		sel := -1
		if r == t.tbRow {
			sel = t.tbCol
		}
		a.drawTkCells(dst, cols, x0+textWidth("0", SmallFace)+6, y, sel)
	}
}

// drawTkGroove pinta el groove: los ticks de cada paso, con una barra, y el
// swing que da.
func (a *App) drawTkGroove(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w := dst.Bounds().Dx()
	g := s.Grooves[t.groove]
	title := fmt.Sprintf("%s %02X", T("tk_v_groove"), t.groove)
	if len(g) == 0 {
		title += " · " + T("tk_empty")
	} else if len(g) >= 2 {
		title += fmt.Sprintf(" · swing %d%%", g[0]*100/(g[0]+g[1]))
	}
	y0 := a.tkTitle(dst, title, false)
	x0 := max(margin, w/2-60)
	for r := 0; r <= len(g) && r < 16; r++ {
		y := y0 + r*tkRowH
		drawText(dst, hexStr(r, 1), SmallFace, x0, y, th.Dim)
		str, on := "--", false
		if r < len(g) {
			str, on = hexStr(g[r], 2), true
			rect(dst, x0+30, y, g[r]*3, tkRowH-4, withAlpha(th.Accent2, 0x90))
		}
		sel := -1
		if r == t.grRow {
			sel = 0
		}
		a.drawTkCells(dst, []tkCol{{str, on, th.Text}}, x0+12, y, sel)
	}
}

// drawTkWave pinta la onda: 32 barras de 0 a F, con el cursor.
func (a *App) drawTkWave(dst *ebiten.Image) {
	t, th, s := a.tracker, a.theme, a.tracker.song
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	title := fmt.Sprintf("%s %02X", T("tk_v_wave"), t.wave)
	if t.wave < tracker.NumPresetWaves {
		title += " · " + tracker.WaveNames[t.wave]
	}
	if s.Waves[t.wave] == nil {
		title += " · " + T("tk_empty")
	}
	y0 := a.tkTitle(dst, title, false)
	wv := a.waveGet(false)
	barW := max(4, min(10, (w-2*margin)/tracker.WaveLen))
	x0 := (w - barW*tracker.WaveLen) / 2
	unit := max(3, (h-bottomH-y0-20)/16)
	base := y0 + 16*unit
	rect(dst, x0, y0, barW*tracker.WaveLen, 16*unit, withAlpha(th.Bg2, 0x90))
	rect(dst, x0, y0+8*unit, barW*tracker.WaveLen, 1, withAlpha(th.Dim, 0x60))
	for i, v := range wv {
		c := th.Accent2
		if i == t.waveX {
			c = th.Accent
			rect(dst, x0+i*barW, y0, barW, 16*unit, withAlpha(th.Accent, 0x30))
		}
		rect(dst, x0+i*barW+1, base-(int(v)+1)*unit, barW-1, unit, c)
	}
	drawText(dst, fmt.Sprintf("%02X: %X", t.waveX, wv[t.waveX]), SmallFace, x0, base+6, th.Text)
}
