package ui

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"github.com/juanvillacortac/audiofilus/internal/tracker"
)

// La pantalla del tracker (en Juegos): hacer canciones chiptune de cuatro
// canales con el mando, como en los trackers de Game Boy. Dos vistas:
//
//   - Patrón: 16 pasos × 4 canales; cada paso, nota, volumen y efecto. La
//     cruceta mueve el cursor; con A mantenida, la cruceta cambia el valor
//     (↑↓ de a uno, ←→ de a mucho: una octava en las notas); A sola pone lo
//     último que se usó; B+A borra. L1/R1 cambian de patrón (R1 al final crea
//     uno nuevo).
//   - Canción: el orden de los patrones y el tempo. A mantenida y ↑↓ cambian
//     el patrón de esa posición; A sola en una posición abre su patrón; en el
//     hueco del final, añade una.
//
// X cambia de vista e Y toca o para (el patrón en bucle, o la canción desde
// la posición elegida). Al tocar se pausa la música. Start abre el menú:
// tocar la canción entera, exportarla a WAV a la biblioteca, duplicar o
// vaciar el patrón y volver a Juegos. Se guarda sola en data/tracker/.

// trackerState es el tracker abierto.
type trackerState struct {
	song      *tracker.Song
	player    *tracker.Player
	streaming bool // su salida de audio está abierta
	playing   bool
	songView  bool
	pat       int // el patrón que se edita
	row, col  int // en el patrón; col = canal*3 + campo (nota, volumen, efecto)
	ord       int // en la canción: -1 el tempo, 0.. las posiciones
	ordScroll int
	// Lo último que se puso, para A sola.
	lastNote int8
	lastVol  int8
	lastFx   byte
	lastArg  uint8
	aEdited  bool // A se usó para cambiar un valor (al soltarla no es un toque)
	aWasHeld bool
	dirty    bool
	dirtyAt  float64
	exported chan string // el archivo exportado (o el error), desde la goroutine
}

func (a *App) trackerPath() string { return filepath.Join(a.paths.Data, "tracker", "song.json") }

// newTracker abre la canción guardada, o una de ejemplo.
func (a *App) newTracker() *trackerState {
	s, err := tracker.Load(a.trackerPath())
	if err != nil {
		s = tracker.NewSong()
	}
	t := &trackerState{song: s, player: tracker.NewPlayer(sfx.Rate), lastNote: 49, lastVol: 12, lastFx: tracker.FxArp, lastArg: 0x47, exported: make(chan string, 1)}
	t.player.SetSong(s)
	return t
}

// trackerChanged marca la canción como cambiada: suena así al momento y se
// guarda en un segundo.
func (a *App) trackerChanged() {
	t := a.tracker
	t.player.SetSong(t.song)
	t.dirty, t.dirtyAt = true, a.t
	t.aEdited = true
}

// saveTracker guarda la canción si cambió.
func (a *App) saveTracker() {
	t := a.tracker
	if t == nil || !t.dirty || a.offline {
		return
	}
	t.dirty = false
	_ = t.song.Save(a.trackerPath())
}

// trackerToggle toca o para.
func (a *App) trackerToggle() {
	t := a.tracker
	if t.playing {
		t.player.Stop()
		t.playing = false
		return
	}
	if a.isPlaying {
		a.send("toggle") // la música, en pausa: si no, suenan las dos
	}
	if t.songView {
		t.player.PlaySong(max(0, t.ord))
	} else {
		t.player.PlayPattern(t.pat)
	}
	t.playing = true
}

// updateTracker atiende el tracker.
func (a *App) updateTracker() {
	t, in := a.tracker, &a.input
	if !t.streaming && !a.offline {
		a.playStream(t.player)
		t.streaming = true
	}
	t.player.SetGain(0.8 * math.Min(1, a.volume))
	select {
	case msg := <-t.exported:
		a.flash(msg)
		if a.yt.bgScan == nil {
			a.yt.bgScan = StartScan(a.paths, a.cfg.MusicDirs)
		}
	default:
	}
	if t.dirty && a.t-t.dirtyAt > 1 {
		a.saveTracker()
	}
	switch {
	case in.Pressed(ActMenu):
		a.openTrackerMenu()
		return
	case in.Pressed(ActPause):
		a.trackerToggle()
		return
	case in.Pressed(ActShuffle):
		t.songView = !t.songView
		return
	}
	aHeld := in.duration(ActConfirm) > 0
	bHeld := in.duration(ActBack) > 0
	if in.Pressed(ActConfirm) {
		t.aEdited = false
	}
	if t.songView {
		a.updateTrackerSong(aHeld, bHeld)
	} else {
		a.updateTrackerPattern(aHeld, bHeld)
	}
	t.aWasHeld = aHeld
}

// trackerDir es la cruceta de este fotograma: dy (↑↓) y dx (←→), con
// repetición al mantener.
func trackerDir(in *Input) (dx, dy int) {
	switch {
	case in.Repeat(ActUp):
		dy = -1
	case in.Repeat(ActDown):
		dy = 1
	case in.Repeat(ActLeft):
		dx = -1
	case in.Repeat(ActRight):
		dx = 1
	}
	return dx, dy
}

// fxCycle son los efectos, en el orden en que A+←→ los recorre.
var fxCycle = []byte{tracker.FxNone, tracker.FxArp, tracker.FxDuty, tracker.FxBend}

func (a *App) updateTrackerPattern(aHeld, bHeld bool) {
	t, in := a.tracker, &a.input
	p := &t.song.Patterns[t.pat]
	ch, field := t.col/3, t.col%3
	c := &p[t.row][ch]
	if in.Pressed(ActConfirm) {
		if bHeld { // B+A: borrar
			*c = tracker.Cell{Vol: -1}
			a.trackerChanged()
			return
		}
		switch field { // A sola en un campo vacío: lo último que se usó
		case 0:
			if c.Note == tracker.NoteEmpty {
				c.Note = t.lastNote
				if c.Vol < 0 {
					c.Vol = t.lastVol
				}
				a.trackerChanged()
				t.aEdited = false
			}
			t.player.Preview(ch, *c)
		case 1:
			if c.Vol < 0 {
				c.Vol = t.lastVol
				a.trackerChanged()
				t.aEdited = false
			}
		case 2:
			if c.Fx == tracker.FxNone {
				c.Fx, c.Arg = t.lastFx, t.lastArg
				a.trackerChanged()
				t.aEdited = false
			}
		}
		return
	}
	dx, dy := trackerDir(in)
	if aHeld && (dx != 0 || dy != 0) {
		switch field {
		case 0:
			n := c.Note
			if n <= 0 {
				n = t.lastNote
			}
			n += int8(-dy + 12*dx)
			c.Note = int8(max(1, min(tracker.NoteMax, int(n))))
			t.lastNote = c.Note
			t.player.Preview(ch, *c)
		case 1:
			v := c.Vol
			if v < 0 {
				v = t.lastVol
			}
			c.Vol = int8(max(0, min(15, int(v)-dy+4*dx)))
			t.lastVol = c.Vol
		case 2:
			if dx != 0 {
				i := 0
				for k, f := range fxCycle {
					if f == c.Fx {
						i = k
					}
				}
				c.Fx = fxCycle[(i+dx+len(fxCycle))%len(fxCycle)]
				if c.Fx == tracker.FxDuty {
					c.Arg &= 3
				}
			} else if c.Fx != tracker.FxNone {
				lim := 255
				if c.Fx == tracker.FxDuty {
					lim = 3
				}
				c.Arg = uint8(max(0, min(lim, int(c.Arg)-dy)))
			}
			t.lastFx, t.lastArg = c.Fx, c.Arg
		}
		a.trackerChanged()
		return
	}
	if aHeld {
		return
	}
	switch {
	case dy != 0:
		t.row = (t.row + dy + tracker.Steps) % tracker.Steps
	case dx != 0:
		t.col = (t.col + dx + 3*tracker.Channels) % (3 * tracker.Channels)
	case in.Pressed(ActPrev):
		t.pat = max(0, t.pat-1)
	case in.Pressed(ActNext):
		if t.pat == len(t.song.Patterns)-1 && len(t.song.Patterns) < tracker.MaxPatterns {
			t.song.Patterns = append(t.song.Patterns, tracker.NewPattern())
			a.trackerChanged()
		}
		t.pat = min(len(t.song.Patterns)-1, t.pat+1)
	}
	if t.playing && !t.songView {
		if pat, _, _, _ := t.player.Position(); pat != t.pat {
			t.player.PlayPattern(t.pat) // cambió de patrón: suena el nuevo
		}
	}
}

func (a *App) updateTrackerSong(aHeld, bHeld bool) {
	t, in := a.tracker, &a.input
	s := t.song
	if in.Pressed(ActConfirm) && bHeld { // B+A: quitar la posición
		if t.ord >= 0 && t.ord < len(s.Order) && len(s.Order) > 1 {
			s.Order = append(s.Order[:t.ord], s.Order[t.ord+1:]...)
			t.ord = min(t.ord, len(s.Order)-1)
			a.trackerChanged()
		}
		return
	}
	if in.Pressed(ActConfirm) && t.ord == len(s.Order) && len(s.Order) < tracker.MaxOrder {
		last := s.Order[len(s.Order)-1]
		s.Order = append(s.Order, last) // el hueco del final: añadir
		a.trackerChanged()
		t.aEdited = true
		return
	}
	// Soltar A sin haber cambiado nada: abrir ese patrón.
	if !aHeld && t.aWasHeld && !t.aEdited && t.ord >= 0 && t.ord < len(s.Order) {
		t.pat, t.songView = s.Order[t.ord], false
		return
	}
	dx, dy := trackerDir(in)
	if aHeld && (dx != 0 || dy != 0) {
		if t.ord < 0 { // el tempo
			s.BPM = max(tracker.MinBPM, min(tracker.MaxBPM, s.BPM-dy+10*dx))
		} else if t.ord < len(s.Order) {
			n := s.Order[t.ord] - dy + 8*dx
			if n == len(s.Patterns) && len(s.Patterns) < tracker.MaxPatterns {
				s.Patterns = append(s.Patterns, tracker.NewPattern())
			}
			s.Order[t.ord] = max(0, min(len(s.Patterns)-1, n))
		}
		a.trackerChanged()
		return
	}
	if !aHeld && dy != 0 {
		t.ord = max(-1, min(len(s.Order), t.ord+dy))
	}
}

// openTrackerMenu es el menú de Start.
func (a *App) openTrackerMenu() {
	t := a.tracker
	a.menu = &menu{
		title:    T("tk_title_menu"),
		subtitle: fmt.Sprintf("%s · %d BPM · %d/%d", T("tk_song"), t.song.BPM, len(t.song.Order), len(t.song.Patterns)),
		items: []menuItem{
			{label: T("tk_play_song"), icon: "play", do: func() {
				t.songView, t.ord = true, 0
				if t.playing {
					a.trackerToggle()
				}
				a.trackerToggle()
			}},
			{label: T("tk_export"), icon: "download", do: a.exportTracker},
			{label: T("tk_dup"), icon: "list", do: func() {
				if len(t.song.Patterns) < tracker.MaxPatterns {
					t.song.Patterns = append(t.song.Patterns, t.song.Patterns[t.pat])
					t.pat = len(t.song.Patterns) - 1
					a.trackerChanged()
				}
			}},
			{label: T("tk_clear"), icon: "power", do: func() {
				t.song.Patterns[t.pat] = tracker.NewPattern()
				a.trackerChanged()
			}},
			{label: T("gm_leave"), icon: "pad", do: func() { a.saveTracker(); a.screen = Games }},
		},
	}
}

// exportTracker pasa la canción a WAV en la carpeta de música
// ("audiofilus tracker/"), en segundo plano; al terminar se avisa y la
// biblioteca se pone al día.
func (a *App) exportTracker() {
	t := a.tracker
	if len(a.cfg.MusicDirs) == 0 {
		a.flash(T("tk_no_folder"))
		return
	}
	song := t.song.Clone()
	dir := filepath.Join(a.cfg.MusicDirs[0], "audiofilus tracker")
	name := "tracker " + time.Now().Format("2006-01-02 1504") + ".wav"
	a.flash(T("tk_exporting"))
	go func() {
		samples := tracker.Render(song, sfx.Rate)
		err := os.MkdirAll(dir, 0o755)
		var f *os.File
		if err == nil {
			f, err = os.Create(filepath.Join(dir, name))
		}
		if err == nil {
			err = tracker.WriteWAV(f, samples, sfx.Rate)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		msg := T("tk_exported", name)
		if err != nil {
			msg = T("tk_export_failed", err)
		}
		t.exported <- msg
		ebiten.ScheduleFrame()
	}()
}

// trackerStopIfAway cierra la salida del tracker al salir de él si no está
// tocando (tocando, sigue sonando de fondo).
func (a *App) trackerStopIfAway() {
	t := a.tracker
	if t == nil || a.screen == Tracker || !t.streaming || t.playing {
		return
	}
	a.saveTracker()
	a.stopStream()
	t.streaming = false
}

// drawTracker pinta la vista que toque.
func (a *App) drawTracker(dst *ebiten.Image) {
	if a.tracker.songView {
		a.drawTrackerSong(dst)
	} else {
		a.drawTrackerPattern(dst)
	}
}

// Medidas del patrón.
const tkRowH = 11

var tkChanNames = [tracker.Channels]string{"PU1", "PU2", "TRI", "NOI"}

func (a *App) drawTrackerPattern(dst *ebiten.Image) {
	t, th := a.tracker, a.theme
	w := dst.Bounds().Dx()
	noteW := textWidth("C#4", SmallFace)
	volW := textWidth("F", SmallFace)
	fxW := textWidth("A37", SmallFace)
	const gap = 3
	chW := noteW + volW + fxW + 2*gap + 6
	stepW := textWidth("00", SmallFace) + 6
	x0 := max(2, (w-(stepW+tracker.Channels*chW))/2)
	y0 := topH + 4
	// La cabecera: el patrón, el tempo y si suena.
	pat, step, _, playing := t.player.Position()
	head := fmt.Sprintf("%s %02X/%02X   %d BPM", T("tk_pattern"), t.pat, len(t.song.Patterns)-1, t.song.BPM)
	drawText(dst, head, SmallFace, x0, y0, th.Text)
	if playing {
		drawIcon(dst, "play", w-margin-8, y0-1, th.Accent)
	}
	y0 += 11
	// Los canales, con un medidor de lo que suena en cada uno.
	lv := t.player.Level()
	for c := 0; c < tracker.Channels; c++ {
		x := x0 + stepW + c*chW
		drawText(dst, tkChanNames[c], SmallFace, x, y0, th.Dim)
		bw := chW - 8 - textWidth("PU1", SmallFace) - 4
		rect(dst, x+textWidth("PU1", SmallFace)+4, y0+1, bw, 4, th.Bg2)
		rect(dst, x+textWidth("PU1", SmallFace)+4, y0+1, int(float64(bw)*math.Min(1, lv[c]*1.4)), 4, th.Accent)
	}
	y0 += 10
	p := t.song.Patterns[t.pat]
	for s := 0; s < tracker.Steps; s++ {
		y := y0 + s*tkRowH
		switch {
		case playing && pat == t.pat && s == step:
			rect(dst, x0-2, y-2, stepW+tracker.Channels*chW, tkRowH, withAlpha(th.Accent, 0x50))
		case s%4 == 0:
			rect(dst, x0-2, y-2, stepW+tracker.Channels*chW, tkRowH, withAlpha(th.Bg2, 0xa0))
		}
		sc := th.Dim
		if s%4 == 0 {
			sc = th.Text
		}
		drawText(dst, fmt.Sprintf("%02X", s), SmallFace, x0, y, sc)
		for c := 0; c < tracker.Channels; c++ {
			cell := p[s][c]
			x := x0 + stepW + c*chW
			note := tracker.NoteName(cell.Note)
			vol := "-"
			if cell.Vol >= 0 {
				vol = fmt.Sprintf("%X", cell.Vol)
			}
			fx := "---"
			switch cell.Fx {
			case tracker.FxArp, tracker.FxBend:
				fx = fmt.Sprintf("%c%02X", cell.Fx, cell.Arg)
			case tracker.FxDuty:
				fx = fmt.Sprintf("D%d ", cell.Arg)
			}
			fields := [3]struct {
				s  string
				x  int
				w  int
				on bool
			}{
				{note, x, noteW, cell.Note != tracker.NoteEmpty},
				{vol, x + noteW + gap, volW, cell.Vol >= 0},
				{fx, x + noteW + volW + 2*gap, fxW, cell.Fx != tracker.FxNone},
			}
			colors := [3]colorPair{{th.Text, withAlpha(th.Dim, 0x80)}, {th.Accent, withAlpha(th.Dim, 0x80)}, {th.Accent2, withAlpha(th.Dim, 0x80)}}
			for f, fd := range fields {
				col := colors[f].off
				if fd.on {
					col = colors[f].on
				}
				if s == t.row && c*3+f == t.col {
					rect(dst, fd.x-1, y-2, fd.w+2, tkRowH, th.Accent)
					col = th.Bg
				}
				drawText(dst, fd.s, SmallFace, fd.x, y, col)
			}
		}
	}
	a.drawHints(dst, []hint{{"A+<>^v", T("tk_h_edit")}, {"Y", T("tk_h_play")}, {"X", T("tk_h_song")}, {"START", T("h_more")}})
}

// colorPair son los colores de un campo con valor (on) y vacío (off).
type colorPair struct{ on, off color.RGBA }

func (a *App) drawTrackerSong(dst *ebiten.Image) {
	t, th := a.tracker, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	s := t.song
	x := max(margin, w/2-90)
	y := topH + 6
	_, _, playPos, playing := t.player.Position()
	// El tempo, editable como una fila más.
	bpm := fmt.Sprintf("%s  %d BPM", T("tk_tempo"), s.BPM)
	if t.ord == -1 {
		rect(dst, x-3, y-2, 180, tkRowH, th.Accent)
		drawText(dst, bpm, SmallFace, x, y, th.Bg)
	} else {
		drawText(dst, bpm, SmallFace, x, y, th.Text)
	}
	y += 16
	drawText(dst, T("tk_order"), SmallFace, x, y, th.Dim)
	y += 11
	rows := (h - bottomH - y - 4) / tkRowH
	if t.ord >= t.ordScroll+rows {
		t.ordScroll = t.ord - rows + 1
	}
	if t.ord >= 0 && t.ord < t.ordScroll {
		t.ordScroll = t.ord
	}
	for i := t.ordScroll; i <= len(s.Order) && i < t.ordScroll+rows; i++ {
		yy := y + (i-t.ordScroll)*tkRowH
		label := "--"
		if i < len(s.Order) {
			label = fmt.Sprintf("%s %02X", T("tk_pattern"), s.Order[i])
		}
		line := fmt.Sprintf("%02X  %s", i, label)
		c := th.Text
		if i == len(s.Order) {
			c = th.Dim
		}
		if playing && t.player != nil && i == playPos {
			rect(dst, x-3, yy-2, 180, tkRowH, withAlpha(th.Accent, 0x50))
		}
		if i == t.ord {
			rect(dst, x-3, yy-2, 180, tkRowH, th.Accent)
			c = th.Bg
		}
		drawText(dst, line, SmallFace, x, yy, c)
	}
	a.drawHints(dst, []hint{{"A", T("tk_h_open")}, {"A+^v", T("tk_h_edit")}, {"Y", T("tk_h_play")}, {"X", T("tk_h_pattern")}})
}
