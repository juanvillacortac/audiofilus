package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/lyrics"
	"github.com/juanvillacortac/audiofilus/internal/radio"
)

// La vista de letras de Now Playing: la portada y el título arriba, la letra
// en el medio (en una sincronizada, la línea que suena resaltada y
// coloreándose como en un karaoke) y la barra de progreso abajo. Se abre
// desde el menú de Now Playing o tocando el visualizador. La letra la busca
// internal/lyrics en segundo plano: el .lrc o .txt junto a la canción, sus
// etiquetas o LRCLIB; START deja elegir otra.

// lyricsView es el estado de la vista.
type lyricsView struct {
	path    string // la canción de la que es la letra
	loading bool
	opt     lyrics.Option
	offset  time.Duration // adelanta (positivo) la letra
	gen     int           // cada búsqueda; las respuestas viejas se tiran

	// Lo partido en filas para el ancho de ahora (cada línea, sus filas).
	wrapW int
	rows  [][]string
	// scroll es el desplazamiento que se pinta (píxeles), que se acerca suave
	// al que toca; manual y manualAt, el que eligió la persona con las
	// flechas o el dedo (en una letra sin sincronizar) y cuándo.
	scroll   float64
	manual   float64
	manualAt float64
}

// lyricsResult es lo que devuelve una búsqueda de letra.
type lyricsResult struct {
	gen  int
	path string
	opt  lyrics.Option
	// opts y found: las opciones para elegir (con menu); found son los
	// resultados de internet, para guardar el elegido.
	opts  []lyrics.Option
	found []lyrics.Found
	menu  bool
}

// Medidas de la vista.
const (
	lyrRowH   = 15 // alto de cada fila de la letra (en BigFace)
	lyrGap    = 4  // el aire de más entre una línea y la siguiente
	lyrHeadH  = 34 // la cabecera con la portada
	lyrManual = 6  // segundos que dura el desplazamiento a mano
)

// lyricsFinder prepara el buscador de letras (con internet, salvo que se
// haya apagado en Ajustes o sea una maqueta).
func (a *App) lyricsFinder() *lyrics.Finder {
	if a.lyrFinder == nil {
		a.lyrFinder = &lyrics.Finder{Store: lyrics.NewStore(filepath.Join(a.paths.Data, "lyrics"))}
	}
	a.lyrFinder.Web = nil
	if !a.cfg.UI.LyricsOffline && !a.offline {
		a.lyrFinder.Web = &lyrics.LRCLIB{HTTP: &http.Client{Timeout: 15 * time.Second}}
	}
	return a.lyrFinder
}

// lyricsQuery es lo que se sabe de la canción que suena, para buscarla.
func (a *App) lyricsQuery() lyrics.Query {
	s := a.playingSong
	return lyrics.Query{Title: s.Title, Artist: s.Artist, Album: s.Album, Duration: a.duration}
}

// noLyrics dice si lo que suena no puede tener letra (una radio).
func (a *App) noLyrics() bool {
	return radio.IsRadio(a.playingSong.Path) || strings.HasPrefix(a.playingSong.Path, a.podDir())
}

// openLyrics abre la vista de letras.
func (a *App) openLyrics() {
	a.lyricsOn = true
	a.screen = NowPlaying
	a.lyr.manualAt = -100
}

// updateLyricsFetch busca la letra de lo que suena si cambió de canción (con
// la vista abierta) y recoge las respuestas.
func (a *App) updateLyricsFetch() {
	for {
		select {
		case r := <-a.lyrCh:
			a.lyricsDone(r)
			continue
		default:
		}
		break
	}
	if !a.lyricsOn || a.playingSong.Path == "" || a.lyr.path == a.playingSong.Path {
		return
	}
	a.lyr = lyricsView{path: a.playingSong.Path, gen: a.lyr.gen + 1, manualAt: -100}
	if a.noLyrics() {
		a.lyr.opt = lyrics.Option{ID: "none"}
		return
	}
	a.lyr.loading = true
	fd, q, path, gen := a.lyricsFinder(), a.lyricsQuery(), a.lyr.path, a.lyr.gen
	a.lyr.offset = time.Duration(fd.Store.Choice(path).OffsetMS) * time.Millisecond
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		a.lyrCh <- lyricsResult{gen: gen, path: path, opt: fd.Find(ctx, path, q)}
		ebiten.ScheduleFrame()
	}()
}

// lyricsDone recoge una búsqueda.
func (a *App) lyricsDone(r lyricsResult) {
	if r.menu {
		a.lyr.loading = false
		if r.path == a.playingSong.Path {
			a.openLyricsPicker(r)
		}
		return
	}
	if r.gen != a.lyr.gen || r.path != a.lyr.path {
		return
	}
	a.lyr.loading, a.lyr.opt, a.lyr.rows, a.lyr.scroll = false, r.opt, nil, 0
}

// lyricsLabel es de dónde sale la letra, para la cabecera.
func lyricsLabel(o lyrics.Option) string {
	switch o.Kind() {
	case "file":
		return strings.ToUpper(strings.TrimPrefix(filepath.Ext(o.Label), "."))
	case "tag":
		return T("lyr_tag")
	case "lrclib":
		return "LRCLIB"
	}
	return ""
}

// updateLyrics atiende los botones con la vista de letras abierta (lo que
// no cambia respecto a Now Playing lo atiende él).
func (a *App) updateLyrics() {
	in := &a.input
	l := &a.lyr
	switch {
	case in.Pressed(ActBack):
		a.lyricsOn = false
	case in.Pressed(ActMenu):
		a.openLyricsMenu()
	case l.opt.Lyrics.Synced && in.Repeat(ActUp):
		a.shiftLyrics(+500 * time.Millisecond)
	case l.opt.Lyrics.Synced && in.Repeat(ActDown):
		a.shiftLyrics(-500 * time.Millisecond)
	case !l.opt.Lyrics.Synced && in.Repeat(ActUp):
		a.scrollLyrics(-3 * lyrRowH)
	case !l.opt.Lyrics.Synced && in.Repeat(ActDown):
		a.scrollLyrics(3 * lyrRowH)
	}
}

// scrollLyrics mueve a mano una letra sin sincronizar.
func (a *App) scrollLyrics(dy float64) {
	l := &a.lyr
	if a.t-l.manualAt > lyrManual {
		l.manual = l.scroll
	}
	l.manual += dy
	l.manualAt = a.t
}

// shiftLyrics adelanta o atrasa la letra y lo guarda para la canción.
func (a *App) shiftLyrics(d time.Duration) {
	l := &a.lyr
	l.offset += d
	ms := int(l.offset.Milliseconds())
	path := l.path
	go func() { _ = a.lyricsFinder().Store.Update(path, func(c *lyrics.Choice) { c.OffsetMS = ms }) }()
	a.flash(T("lyr_offset_now", offsetLabel(l.offset)))
}

// offsetLabel es un desfase legible: "+0,5 s".
func offsetLabel(d time.Duration) string {
	s := fmt.Sprintf("%+.1f s", d.Seconds())
	if lang == "es" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// openLyricsMenu es el menú de START en la vista de letras.
func (a *App) openLyricsMenu() {
	m := &menu{title: T("lyr_title"), subtitle: a.playingSong.Title, album: a.playing}
	if !a.noLyrics() {
		m.items = append(m.items, menuItem{label: T("lyr_choose"), icon: "list", do: a.findLyricsOptions})
	}
	if a.lyr.opt.Lyrics.Synced {
		m.items = append(m.items, menuItem{label: T("lyr_offset"), icon: "clock",
			do:     func() { a.shiftLyrics(-a.lyr.offset) },
			value:  func() string { return offsetLabel(a.lyr.offset) },
			active: func() bool { return a.lyr.offset != 0 }})
	}
	m.items = append(m.items, menuItem{label: T("lyr_hide"), icon: "arrow_l", do: func() { a.lyricsOn = false }})
	a.menu = m
}

// findLyricsOptions busca todas las letras posibles (a mano y en internet)
// y abre la lista para elegir cuando las tiene.
func (a *App) findLyricsOptions() {
	path, q, fd := a.playingSong.Path, a.lyricsQuery(), a.lyricsFinder()
	a.lyr.loading = true
	a.flash(T("lyr_searching"))
	go func() {
		r := lyricsResult{path: path, menu: true, opts: lyrics.Local(path)}
		if fd.Web != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			found, err := fd.Web.Search(ctx, q)
			cancel()
			if err == nil {
				r.found = found
				for _, f := range found {
					if o, ok := lyrics.OptionOf(f); ok {
						r.opts = append(r.opts, o)
					}
				}
			}
		}
		a.lyrCh <- r
		ebiten.ScheduleFrame()
	}()
}

// openLyricsPicker abre la lista de letras posibles: elegir una la usa (y
// se guarda para esa canción).
func (a *App) openLyricsPicker(r lyricsResult) {
	m := &menu{title: T("lyr_choose"), subtitle: a.playingSong.Title, album: a.playing, cols: 2}
	cur := a.lyr.opt.ID
	pick := func(o lyrics.Option) {
		path := r.path
		go func() { _ = a.lyricsFinder().Store.Remember(path, o, r.found) }()
		if path == a.lyr.path {
			a.lyr.opt, a.lyr.rows, a.lyr.scroll, a.lyr.loading = o, nil, 0, false
		}
	}
	for _, o := range r.opts {
		label, note := o.Label, o.Detail
		switch o.Kind() {
		case "tag":
			label = T("lyr_tag_long")
		case "lrclib":
			label = "LRCLIB · " + o.Label
		}
		kind := T("lyr_plain")
		if o.Synced() {
			kind = T("lyr_synced")
		}
		if note != "" {
			note = kind + " · " + note
		} else {
			note = kind
		}
		id := o.ID
		m.items = append(m.items, menuItem{label: label, icon: lyricsIcon(o), note: note, close: true,
			active: func() bool { return id == cur }, do: func() { pick(o) }})
	}
	if len(r.opts) == 0 {
		m.items = append(m.items, menuItem{label: T("lyr_none_found"), icon: "search", note: T("lyr_none_hint"), do: func() {}})
	}
	m.items = append(m.items, menuItem{label: T("lyr_off"), icon: "arrow_l", do: func() { pick(lyrics.Option{ID: "none"}) }})
	a.menu = m
}

// lyricsIcon es el icono de cada origen en la lista.
func lyricsIcon(o lyrics.Option) string {
	switch o.Kind() {
	case "lrclib":
		return "globe"
	case "tag":
		return "disc"
	}
	return "folder"
}

// lyricsPos es la posición de la canción para la letra (con el desfase).
func (a *App) lyricsPos() time.Duration {
	return time.Duration(a.shownPos*float64(time.Second)) + a.lyr.offset
}

// drawLyrics pinta la vista de letras.
func (a *App) drawLyrics(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	song, l := a.playingSong, &a.lyr

	// La cabecera: la portada chica, el título, el artista y de dónde sale.
	const cs = 26
	cx, cy := margin, topH+5
	rect(dst, cx+1, cy+1, cs, cs, th.Shadow)
	a.drawCover(dst, a.playing, cx, cy, cs, 1)
	tx := cx + cs + 7
	src := lyricsLabel(l.opt)
	tw := w - tx - margin
	if src != "" {
		sw := textWidth(src, SmallFace) + 8
		tw -= sw + 4
		panel(dst, w-margin-sw, cy+1, sw, 10, withAlpha(th.Shadow, 0xc0))
		drawText(dst, src, SmallFace, w-margin-sw+4, cy+3, th.Accent2)
	}
	a.drawMarquee(dst, song.Title, tx, cy+3, tw, false)
	drawTextW(dst, song.Artist, SmallFace, tx, cy+18, w-tx-margin, th.Accent)
	a.tapZone(0, cy, w, cs, func() { a.lyricsOn = false })

	y0, y1 := topH+lyrHeadH+4, h-bottomH-22
	a.drawProgress(dst, margin+a.seekNudge(), y1+8, w-2*margin)

	ly := l.opt.Lyrics
	if l.loading || ly.Empty() {
		msg, sub := T("lyr_searching"), ""
		if !l.loading {
			msg, sub = T("lyr_none"), T("lyr_none_hint")
			if a.noLyrics() {
				sub = ""
			}
		}
		cy := (y0+y1)/2 - 8
		drawText(dst, msg, SmallFace, (w-textWidth(msg, SmallFace))/2, cy, th.Text)
		drawText(dst, sub, SmallFace, (w-textWidth(sub, SmallFace))/2, cy+13, th.Dim)
		a.drawLyricsHints(dst)
		return
	}

	// Partida en filas para este ancho (una vez por letra y ancho).
	lw := w - 2*margin - 12
	fresh := l.rows == nil || l.wrapW != lw // recién abierta: sin viaje desde arriba
	if fresh {
		l.wrapW, l.rows = lw, make([][]string, len(ly.Lines))
		for i, ln := range ly.Lines {
			l.rows[i] = wrapLines(ln.Text, BigFace, lw)
			if len(l.rows[i]) == 0 {
				l.rows[i] = []string{""}
			}
		}
	}
	tops := make([]int, len(l.rows)+1)
	for i, r := range l.rows {
		// Entre líneas, un poco de aire: así una partida en dos filas no
		// parece dos.
		tops[i+1] = tops[i] + len(r)*lyrRowH + lyrGap
		if len(r) == 1 && r[0] == "" {
			tops[i+1] = tops[i] + lyrRowH/2 // una línea en blanco, media fila
		}
	}
	total, view := tops[len(l.rows)], y1-y0
	pos := a.lyricsPos()
	cur := ly.Index(pos)

	// Hasta dónde desplazar: en una sincronizada, la línea que suena en el
	// centro; en una sin sincronizar, según lo que va de canción (o lo que
	// eligió la persona).
	want := 0.0
	switch {
	case ly.Synced && cur >= 0:
		want = float64(tops[cur]+(tops[cur+1]-tops[cur])/2) - float64(view)/2
	case ly.Synced:
		want = -float64(view) / 3
	case a.t-l.manualAt < lyrManual:
		want = l.manual
	case a.duration > 0:
		want = (a.shownPos/a.duration)*float64(total-view+lyrRowH*2) - lyrRowH
	}
	if !ly.Synced {
		want = math.Max(-lyrRowH, math.Min(want, float64(max(0, total-view+lyrRowH))))
	}
	// El dedo (o la rueda) mueve la letra sin sincronizar, como una lista.
	if !ly.Synced {
		a.scrollArea(margin, y0, w-2*margin, view, a.scrollLyrics)
	}
	l.scroll += (want - l.scroll) * 0.15
	if fresh || math.Abs(want-l.scroll) < 0.5 {
		l.scroll = want
	}

	// Lo que mide la franja que se desvanece arriba y abajo.
	fade := math.Min(float64(view)/3, 56)
	b := dst.Bounds()
	clip := dst.SubImage(image.Rect(b.Min.X, b.Min.Y+y0, b.Max.X, b.Min.Y+y1)).(*ebiten.Image)
	for i, rows := range l.rows {
		top := y0 + tops[i] - int(math.Round(l.scroll))
		if top+len(rows)*lyrRowH < y0 || top > y1 {
			continue
		}
		c := th.Text
		if ly.Synced {
			// Las lejanas a la que suena se apagan.
			d := abs(i - cur)
			switch {
			case i == cur:
			case d == 1:
				c = mix(th.Text, th.Bg, 0.45)
			default:
				c = mix(th.Text, th.Bg, 0.65)
			}
		}
		// En la que suena, el karaoke: se va pintando del color de acento
		// según avanza hasta la siguiente.
		frac := -1.0
		if ly.Synced && i == cur {
			end := pos + 4*time.Second
			if i+1 < len(ly.Lines) {
				end = ly.Lines[i+1].At
			}
			start := ly.Lines[i].At
			frac = math.Max(0, math.Min(1, float64(pos-start)/float64(max(1, end-start))))
		}
		widths, sum := make([]int, len(rows)), 0
		for j, r := range rows {
			widths[j] = textWidth(r, BigFace)
			sum += widths[j]
		}
		lit := int(frac * float64(sum))
		for j, r := range rows {
			x := margin + 6 + (lw-widths[j])/2
			y := top + j*lyrRowH - 2 // las mayúsculas, centradas en la fila
			// Como un carrusel: arriba y abajo las filas se desvanecen, en
			// cuatro pasos (como el texto largo, ver drawTextW).
			mid := y + bigCapTop + bigCap/2
			f := math.Min(1, float64(min(mid-y0, y1-mid))/fade)
			if f = math.Ceil(f*4) / 4; f <= 0 {
				lit -= widths[j]
				continue
			}
			alpha := func(c color.RGBA) color.RGBA { return withAlpha(c, uint8(float64(c.A)*f)) }
			drawText(clip, r, BigFace, x, y, alpha(c))
			if frac >= 0 && lit > 0 {
				part := min(lit, widths[j])
				sub := clip.SubImage(image.Rect(b.Min.X+x, b.Min.Y+max(y, y0), b.Min.X+x+part, b.Min.Y+min(y+bigCapTop+bigCap+5, y1))).(*ebiten.Image)
				drawText(sub, r, BigFace, x, y, alpha(th.Accent))
				lit -= part
			}
		}
	}
	a.drawLyricsHints(dst)
}

// drawLyricsHints es la barra de abajo de la vista.
func (a *App) drawLyricsHints(dst *ebiten.Image) {
	hints := []hint{{"A", T("h_pause")}}
	switch {
	case a.lyr.opt.Lyrics.Synced && a.input.touch:
		hints = append(hints, hint{"^", T("h_sooner")}, hint{"v", T("h_later")})
	case a.lyr.opt.Lyrics.Synced:
		hints = append(hints, hint{"^v", T("h_offset")})
	}
	hints = append(hints, hint{"START", T("h_more")}, hint{"B", T("h_back")})
	a.drawHints(dst, hints)
}
