package ui

import (
	"fmt"
	"github.com/juanvillacortac/audiofilus/internal/radio"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Screen es una pantalla de la maqueta.
type Screen int

const (
	NowPlaying Screen = iota
	LibraryScreen
	Queue
	Blocks    // Bloques, el juego (tetris.go)
	Rhythm    // Ritmo, el juego de carriles (rhythm.go)
	Visual    // los visualizadores (visual.go; Derrape en drift.go)
	Downloads // las descargas de YouTube Music (ytmusic.go; solo con yt-dlp)
	Radio     // la radio por internet (radio.go)
	Podcasts  // los podcasts (podcasts.go)
	Settings
	Games   // los minijuegos juntos (games.go)
	Crypt   // Cripta, el roguelike de ritmo (crypt.go)
	Tracker // el tracker chiptune (trackerui.go; el motor en internal/tracker)
	Quiz    // ¿Qué canción es? (quiz.go)
	Snake   // Serpiente al ritmo (snake.go)
	Shooter // Naves al ritmo (shooter.go)
	Duel    // Duelo, batallas por turnos al ritmo (duel.go)
	Runner  // Carrera, un corredor al ritmo (runner.go)
	Modules // los módulos de la red: The Mod Archive, Modland... (modules.go)
	screenCount
)

func (s Screen) String() string {
	return [...]string{"reproduciendo", "biblioteca", "cola", "bloques", "ritmo", "visualizador", "descargas", "radio", "podcasts", "ajustes", "juegos", "cripta", "tracker", "adivina", "serpiente", "naves", "duelo", "carrera", "modulos"}[s]
}

const (
	topH   = 15 // barra de arriba
	margin = 10
)

// bottomH es el alto de la barra de ayuda de abajo: 13 con mando o teclado y
// más con el dedo (sus botones se tocan). Lo pone setBottomH en cada
// fotograma, antes de colocar nada; toda la interfaz se coloca con él.
var bottomH = 13

// Los altos de la barra de abajo.
const (
	bottomPad   = 13
	bottomTouch = 22
)

// setBottomH pone el alto de la barra de abajo según con qué se maneja.
func (a *App) setBottomH() {
	bottomH = bottomPad
	if a.input.touch {
		bottomH = bottomTouch
	}
}

// drawTopBar pinta el título de la pantalla, la hora y la batería.
func (a *App) drawTopBar(dst *ebiten.Image, title string) {
	w := dst.Bounds().Dx()
	th := a.theme
	// Tocar el título abre la barra de pantallas (como SELECT).
	a.tapZone(0, 0, w*2/3, topH+2, func() { a.input.inject(ActSelect) })
	rect(dst, 0, 0, w, topH, withAlpha(th.Shadow, 0x99))
	rect(dst, 0, topH, w, 1, withAlpha(th.Accent, 0x55))
	drawIcon(dst, "note", margin-2, 4, th.Accent)
	title = a.castTitle(title) // "→ aparato" mientras se manda (castui.go)
	drawText(dst, title, SmallFace, margin+8, 5, th.Text)
	clock := a.now.Format("15:04")
	if !a.cfg.UI.Clock24 {
		clock = a.now.Format("3:04 PM") // con AM o PM, para no confundir las horas
	}
	right := w - margin
	if n := a.ytDownloads(); n > 0 {
		// Descargas de YouTube Music en curso: la flecha y cuántas.
		cnt := fmt.Sprint(n)
		right -= textWidth(cnt, SmallFace)
		drawText(dst, cnt, SmallFace, right, 5, th.Accent)
		right -= 9
		drawIcon(dst, "download", right, 3, th.Accent)
		right -= 6
	}
	if a.battery >= 0 && !a.mobile() {
		// Batería: contorno de 11×7 con el nivel dentro.
		right -= 13
		frame(dst, right, 4, 11, 7, th.Dim)
		rect(dst, right+11, 6, 1, 3, th.Dim)
		fill := int(7*a.battery + 0.5)
		if a.charging && fill < 7 {
			// Cargando: el nivel sube a saltos hasta llena y vuelve a empezar.
			fill += int(a.t*2) % (8 - fill)
		}
		rect(dst, right+2, 6, fill, 3, th.Accent)
		if a.charging {
			right -= 6
			drawIcon(dst, "bolt", right, 4, th.Accent)
		}
		// El porcentaje, junto a la pila.
		pct := fmt.Sprintf("%d%%", int(a.battery*100+0.5))
		right -= 3 + textWidth(pct, SmallFace)
		drawText(dst, pct, SmallFace, right, 5, th.Dim)
		right -= 6
	}
	if !a.mobile() { // en el móvil están la hora y la batería del sistema, en su barra
		right -= textWidth(clock, SmallFace)
		drawText(dst, clock, SmallFace, right, 5, th.Dim)
	}
	// Audífonos: de cable o bluetooth (con el altavoz no se pinta nada). En
	// el móvil, nada de esto: lo muestra el sistema en su barra.
	switch {
	case a.mobile():
	case a.status.Output == "wired":
		drawIcon(dst, "headphones", right-11, 4, th.Text)
	case a.status.Output == "bluetooth":
		drawIcon(dst, "headphones", right-17, 4, th.Text)
		drawIcon(dst, "bluetooth", right-9, 4, th.Accent)
	}
}

// keyNames son las teclas que se muestran en la ayuda cuando no hay mando.
var keyNames = map[string]string{"A": "ENTER", "B": "ESC", "X": "S", "Y": "SPACE", "START": "M", "SEL": "TAB", "FN": "F1", "L1": "Q", "R1": "E", "L2": ",", "R2": "."}

// btn es el nombre de un botón del mando, o de su tecla si se está usando el
// teclado. Traduce también los compuestos ("X X", "A/Y", "FN x3 L1/R1"),
// botón a botón. Con "=" delante es una tecla tal cual (sin traducir), para
// las ayudas que ya son del teclado.
func (a *App) btn(b string) string {
	if k, ok := strings.CutPrefix(b, "="); ok {
		return k
	}
	if a.input.usingPad || a.offline {
		if a.input.oneHand && a.input.rot == rotLeft {
			return a.input.oneHandBtn(b)
		}
		return b
	}
	if k, ok := keyNames[b]; ok {
		return k
	}
	var out strings.Builder
	word := 0 // dónde empieza el botón que se está leyendo
	for i := 0; i <= len(b); i++ {
		if i < len(b) && !strings.ContainsRune(" /+", rune(b[i])) {
			continue
		}
		if k, ok := keyNames[b[word:i]]; ok {
			out.WriteString(k)
		} else {
			out.WriteString(b[word:i])
		}
		if i < len(b) {
			out.WriteByte(b[i])
		}
		word = i + 1
	}
	return out.String()
}

// oneHandBtn es el nombre de un botón con una mano y la cruceta abajo, donde
// A/B/X/Y se pulsan en la cruceta: cada letra pasa a la flecha que queda en
// su sitio (aceptar, abajo, es "v"), y las flechas de moverse llevan delante
// "LS", el stick con el que se mueve esa mano. Traduce botón a botón, como
// btn ("A+^v" es "v+LS ^v").
func (in *Input) oneHandBtn(b string) string {
	var out strings.Builder
	word := 0
	for i := 0; i <= len(b); i++ {
		if i < len(b) && !strings.ContainsRune(" /+", rune(b[i])) {
			continue
		}
		w := b[word:i]
		switch {
		case w != "" && strings.Trim(w, "ABXY") == "":
			for _, r := range w {
				out.WriteByte(in.faceArrow(hintActions[string(r)]))
			}
		case w != "" && strings.Trim(w, "<>^v") == "":
			out.WriteString("LS " + w)
		default:
			out.WriteString(w)
		}
		if i < len(b) {
			out.WriteByte(b[i])
		}
		word = i + 1
	}
	return out.String()
}

// faceArrow es la flecha de la posición del botón de la acción a entre
// A/B/X/Y (arriba, derecha, abajo, izquierda), con A/B y X/Y como estén.
func (in *Input) faceArrow(a Action) byte {
	for _, b := range in.buttons(a) {
		for i, r := range faceRing {
			if r == b {
				return "^>v<"[i]
			}
		}
	}
	return '?'
}

// hint es un botón y lo que hace, para la barra de abajo.
type hint struct{ btn, label string }

// drawHints pinta la ayuda de botones, centrada.
func (a *App) drawHints(dst *ebiten.Image, hints []hint) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	y := h - bottomH
	rect(dst, 0, y, w, bottomH, withAlpha(th.Shadow, 0x99))
	if a.input.touch {
		a.drawTouchHints(dst, hints, y)
		return
	}
	// Si lo último que se usó fue el teclado, se muestran las teclas. Tocar
	// un chip es pulsar su botón (hintActions); lo de antes de traducirlo.
	orig := make([]string, len(hints))
	for i := range hints {
		orig[i] = hints[i].btn
		hints[i].btn = a.btn(hints[i].btn)
	}
	total := 0
	for _, hn := range hints {
		total += btnChipWidth(hn.btn) + 3 + textWidth(hn.label, SmallFace) + 10
	}
	x := (w - total + 10) / 2
	for i, hn := range hints {
		x0 := x
		x += a.drawBtnChip(dst, hn.btn, x, y+2) + 3
		drawText(dst, hn.label, SmallFace, x, y+4, th.Dim)
		x += textWidth(hn.label, SmallFace) + 10
		if act, ok := hintActions[orig[i]]; ok {
			a.tapZone(x0-4, y, x-x0, bottomH, func() { a.input.inject(act) })
		}
	}
}

// actRandom es el botón táctil "Al azar" (no es una acción del mando: es X
// dos veces).
const actRandom = actCount

// drawTouchHints es la ayuda de abajo para el dedo: sin teclas ni botones
// del mando, cada acción es un botón con su nombre que se toca. Las de las
// flechas no salen (con el dedo se toca o se desliza).
func (a *App) drawTouchHints(dst *ebiten.Image, hints []hint, y int) {
	w := dst.Bounds().Dx()
	th := a.theme
	type item struct {
		label string
		act   Action
	}
	var items []item
	total := 0
	for _, hn := range hints {
		act, ok := hintActions[hn.btn]
		if hn.btn == "XX" {
			act, ok = actRandom, true
		}
		if !ok || act == ActLeft || act == ActRight || act == ActUp || act == ActDown {
			continue
		}
		items = append(items, item{hn.label, act})
		total += textWidth(hn.label, SmallFace) + 12
	}
	gap := 4
	// Si sobra ancho, los botones crecen a partes iguales (más fáciles de
	// tocar); si falta, se aprietan.
	extra := 0
	if n := len(items); n > 0 {
		extra = max(0, min(30, (w-2*margin-total-gap*(n-1))/n))
		total += extra * n
	}
	x := (w - total - gap*(len(items)-1)) / 2
	for _, it := range items {
		bw := textWidth(it.label, SmallFace) + 12 + extra
		panel(dst, x, y+2, bw, bottomH-4, withAlpha(th.Bg2, 0xe0))
		drawText(dst, it.label, SmallFace, x+(bw-textWidth(it.label, SmallFace))/2, y+(bottomH-smallCap)/2, th.Text)
		act := it.act
		a.tapZone(x, y, bw, bottomH, func() {
			if act == actRandom {
				a.playRandomSong()
				return
			}
			a.input.inject(act)
		})
		x += bw + gap
	}
}

// hintArrows son las flechas que se dibujan como icono en las pastillas.
var hintArrows = map[rune]string{'<': "hint_l", '>': "hint_r", '^': "hint_u", 'v': "hint_d"}

// btnParts parte el nombre de un botón en trozos: cada flecha suelta es un
// icono y el resto, texto ("<>" son dos flechas, también tras "+" como en
// "A+^v"; "L1/R1", texto).
func btnParts(b string) []string {
	var parts []string
	word := []rune{}
	for _, r := range b {
		if _, ok := hintArrows[r]; ok && (len(word) == 0 || word[len(word)-1] == ' ' || word[len(word)-1] == '+') {
			if len(word) > 0 {
				parts = append(parts, string(word))
				word = word[:0]
			}
			parts = append(parts, string(r))
			continue
		}
		word = append(word, r)
	}
	if len(word) > 0 {
		parts = append(parts, string(word))
	}
	return parts
}

// btnPartWidth es el ancho de un trozo.
func btnPartWidth(p string) int {
	if ic, ok := hintArrows[[]rune(p)[0]]; ok && len(p) == 1 {
		return icon(ic).Bounds().Dx()
	}
	return textWidth(p, SmallFace)
}

// btnChipWidth es el ancho de la pastilla de un botón.
func btnChipWidth(b string) int {
	w := 0
	for i, p := range btnParts(b) {
		if i > 0 {
			w++
		}
		w += btnPartWidth(p)
	}
	return max(9, w+4)
}

// drawBtnChip pinta la pastilla de un botón en (x, y), con las flechas como
// iconos, y devuelve su ancho.
func (a *App) drawBtnChip(dst *ebiten.Image, b string, x, y int) int {
	th := a.theme
	cw := btnChipWidth(b)
	panel(dst, x, y, cw, 9, th.Accent)
	parts := btnParts(b)
	inner := cw - 4
	used := 0
	for i, p := range parts {
		if i > 0 {
			used++
		}
		used += btnPartWidth(p)
	}
	px := x + 2 + (inner-used)/2
	for _, p := range parts {
		if ic, ok := hintArrows[[]rune(p)[0]]; ok && len(p) == 1 {
			b := icon(ic).Bounds()
			drawIcon(dst, ic, px, y+(9-b.Dy())/2, th.Bg)
		} else {
			drawText(dst, p, SmallFace, px, y+2, th.Bg)
		}
		px += btnPartWidth(p) + 1
	}
	return cw
}

// drawCover pinta la portada de un álbum, o una de relleno si no tiene.
func (a *App) drawCover(dst *ebiten.Image, al *Album, x, y, size int, dim float32) {
	th := a.theme
	if al == nil {
		panel(dst, x, y, size, size, th.Bg2)
		if size >= 20 {
			drawIcon(dst, "note", x+size/2-3, y+size/2-3, th.Dim)
		}
		return
	}
	img, alpha := a.cover(al, size)
	if alpha < 1 {
		// La de relleno, mientras llega la portada (o si no tiene).
		panel(dst, x, y, size, size, th.Bg2)
		if size >= 20 {
			drawIcon(dst, "note", x+size/2-3, y+size/2-3, th.Dim)
		}
	}
	if img == nil {
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.Scale(dim*alpha, dim*alpha, dim*alpha, alpha)
	dst.DrawImage(img, op)
}

// drawNowPlaying es la pantalla de la canción que suena.
func (a *App) drawNowPlaying(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	// En vertical (un móvil) sobra alto: la portada crece hasta llenar el
	// ancho (con el vinilo asomando) y bajo el artista va el álbum.
	tall := h > ScreenH+40
	th := a.theme
	al, song := a.playing, a.playingSong
	// Lado a lado solo si es apaisada de verdad: una ventana casi cuadrada
	// del escritorio va como en vertical, con la portada grande.
	wide := w >= 300 && w*10 >= h*12
	if song.Path == "" {
		a.drawNothing(dst)
		return
	}
	if a.lyricsOn {
		a.drawLyrics(dst)
		return
	}

	var size, cx, cy, textW int
	// En una ventana más apaisada que 21:9 (el escritorio, framed.go), la
	// portada y los textos van centrados, con el ancho de texto de un bloque
	// de 21:9 como mucho, en vez de quedar pegados a la izquierda; el
	// espectro y la barra siguen a todo el ancho.
	off := max(0, (w-max(maxW, h*21/9))/2)
	if wide {
		// Con alto de sobra (una ventana del escritorio a escala entera,
		// framed.go), la portada crece con él y el espectro se queda en su
		// proporción.
		size = min(128+max(0, h-ScreenH)*3/4, (w-2*off)*34/100)
		cx, cy = margin+4, topH+12
		if off > 0 {
			// Se centra lo que se ve: la portada con el vinilo y la línea
			// de texto más larga (o los controles), no el bloque entero, que
			// con un título corto dejaría el hueco a la derecha.
			lead := size + size*34/100 + (size/2-3)/2 + 12
			line := max(textWidth(song.Title, BigFace), textWidth(song.Artist, SmallFace), textWidth(song.Album, SmallFace), 90)
			if al != nil {
				line = max(line, textWidth(al.Title, SmallFace))
			}
			textW = min(w-2*off-2*margin-4-lead, line)
			cx = (w - lead - textW) / 2
		}
	} else {
		size = 92
		cx, cy = (w-size)/2-16, topH+8
		if tall {
			size = min(92+(h-ScreenH)/2, (w-2*margin-8)*100/134)
			cx, cy = (w-size-size*34/100)/2+2, topH+10+min(16, (h-ScreenH)/12)
		}
	}

	// El vinilo asoma por detrás de la portada y gira mientras suena.
	slide := size * 34 / 100
	r := size/2 - 3
	a.spin += a.spinSpeed() // el vinilo solo gira mientras suena
	book := a.isBook(song.Path)
	if book {
		// Un audiolibro: un casete en vez del vinilo (bookart.go), con la
		// cinta pasando de un carrete al otro según lo escuchado.
		progress := 0.0
		if a.duration > 0 {
			progress = a.shownPos / a.duration
		}
		// Un poco más afuera que el vinilo: que se vea el carrete de la derecha.
		drawCassette(dst, cx+size/2+slide+r/3, cy+size/2, r, a.spin, progress, th)
	} else {
		drawVinyl(dst, cx+size/2+slide, cy+size/2, r, a.spin, th.Accent)
	}
	// La portada entra desde el lado al cambiar de canción, con un destello.
	ox := a.coverOffset(size)
	rect(dst, cx+2+ox, cy+2, size, size, th.Shadow)
	if book && (al == nil || !al.HasCover()) {
		drawBookArt(dst, cx+ox, cy, size, th) // sin portada: un librito
	} else {
		a.drawCover(dst, al, cx+ox, cy, size, 1)
	}
	if f := a.coverFlash(); f > 0 {
		rect(dst, cx+ox, cy, size, size, withAlpha(th.Text, uint8(150*f)))
	}
	a.prefetchCovers(al, size)
	frame(dst, cx-1+ox, cy-1, size+2, size+2, withAlpha(th.Text, 0x40))
	// Lo de YouTube Music que suena sin guardar: su icono en la esquina de
	// la portada.
	_, streaming := a.streamTrack()
	live := radio.IsRadio(song.Path)
	pod := strings.HasPrefix(song.Path, a.podDir())
	switch {
	case pod:
		panel(dst, cx+ox+3, cy+size-13, 13, 11, withAlpha(th.Shadow, 0xd0))
		drawIcon(dst, "mic", cx+ox+6, cy+size-11, th.Accent)
	case streaming:
		panel(dst, cx+ox+3, cy+size-13, 15, 10, withAlpha(th.Shadow, 0xd0))
		drawIcon(dst, "ytm", cx+ox+6, cy+size-12, ytRed)
	case live:
		panel(dst, cx+ox+3, cy+size-13, 13, 11, withAlpha(th.Shadow, 0xd0))
		drawIcon(dst, "radio", cx+ox+6, cy+size-11, th.Accent)
	}

	// En las esquinas de arriba de la portada: el corazón si es favorita y
	// lo que le queda al temporizador.
	if a.isFavSong(song.Path) {
		panel(dst, cx+ox+size-15, cy+3, 12, 11, withAlpha(th.Shadow, 0xd0))
		drawIcon(dst, "heart", cx+ox+size-13, cy+5, th.Accent)
	}
	if a.status.TimerLeft > 0 || a.status.StopAfter {
		// La luna, y los minutos que quedan o una nota (al acabar la canción).
		label := fmt.Sprint(int(a.status.TimerLeft/60) + 1)
		lw := textWidth(label, SmallFace)
		if a.status.StopAfter {
			lw = 7
		}
		panel(dst, cx+ox+3, cy+3, lw+15, 11, withAlpha(th.Shadow, 0xd0))
		drawIcon(dst, "moon", cx+ox+5, cy+5, th.Accent)
		if a.status.StopAfter {
			drawIcon(dst, "note", cx+ox+14, cy+5, th.Text)
		} else {
			drawText(dst, label, SmallFace, cx+ox+14, cy+5, th.Text)
		}
	}

	// Los textos.
	var tx, tw, ty int
	if wide {
		tx = cx + size + slide + r/2 + 12
		tw = w - tx - margin
		if off > 0 {
			tw = textW
		}
		ty = cy + 4
	} else {
		tx, tw, ty = margin, w-2*margin, cy+size+8
	}
	if wide {
		state := T("playing")
		if !a.isPlaying {
			state = T("paused")
		}
		drawText(dst, state, SmallFace, tx, ty, th.Accent2)
		if pod {
			// Un episodio: "PODCAST" con el micrófono.
			bx := tx + textWidth(state, SmallFace) + 6
			label := T("podcast")
			panel(dst, bx, ty-2, textWidth(label, SmallFace)+16, 10, withAlpha(th.Shadow, 0xb0))
			drawIcon(dst, "mic", bx+3, ty-2, th.Accent)
			drawText(dst, label, SmallFace, bx+13, ty, th.Text)
		}
		if live {
			// Una radio: "EN VIVO" con la antena.
			bx := tx + textWidth(state, SmallFace) + 6
			label := T("live")
			panel(dst, bx, ty-2, textWidth(label, SmallFace)+16, 10, withAlpha(th.Shadow, 0xb0))
			drawIcon(dst, "radio", bx+3, ty-2, th.Accent)
			drawText(dst, label, SmallFace, bx+13, ty, th.Text)
		}
		if streaming {
			// Y junto al estado, de dónde viene: "STREAMING" con el icono.
			bx := tx + textWidth(state, SmallFace) + 6
			label := T("streaming")
			panel(dst, bx, ty-2, textWidth(label, SmallFace)+16, 10, withAlpha(th.Shadow, 0xb0))
			drawIcon(dst, "ytm", bx+2, ty-1, ytRed)
			drawText(dst, label, SmallFace, bx+13, ty, th.Text)
		}
		ty += 12
	}
	a.drawMarquee(dst, song.Title, tx, ty, tw, !wide)
	ty += 17
	artist := song.Artist
	albumTitle, count := song.Album, 0
	if al != nil && !al.stray {
		albumTitle, count = al.Title, len(al.Songs)
	}
	album := albumTitle
	if wide {
		drawTextW(dst, artist, SmallFace, tx, ty, tw, th.Accent)
		drawTextW(dst, album, SmallFace, tx, ty+11, tw, th.Dim)
		if ch := a.chapterLine(); ch != "" {
			drawTextW(dst, ch, SmallFace, tx, ty+22, tw, th.Dim) // un libro con capítulos
		} else if count > 0 {
			drawText(dst, T("track_of", a.playingIdx+1, count), SmallFace, tx, ty+22, th.Dim)
		}
		ty += 40
	} else {
		drawTextW(dst, artist, SmallFace, tx+max(0, (tw-textWidth(artist, SmallFace))/2), ty, tw, th.Accent)
		ty += 13
		if tall && album != "" {
			drawTextW(dst, album, SmallFace, tx+max(0, (tw-textWidth(album, SmallFace))/2), ty, tw, th.Dim)
			ty += 14
		}
	}

	// Controles.
	ctrlX := tx
	if !wide {
		ctrlX = (w - 5*22) / 2
	}
	a.drawControls(dst, ctrlX, ty)

	// Visualizador y barra de progreso, a todo el ancho.
	vy0 := ty + 24
	if wide {
		vy0 = max(vy0, cy+size+14)
	}
	vy1 := dst.Bounds().Dy() - bottomH - 24
	a.drawSpectrum(dst, margin, vy0, w-2*margin, vy1-vy0)
	// Tocar el visualizador abre la letra.
	a.tapZone(margin, vy0, w-2*margin, vy1-vy0, a.openLyrics)
	a.drawProgress(dst, margin+a.seekNudge(), vy1+8, w-2*margin)

	hints := []hint{{"A", T("h_pause")}, {"X", T("h_shuffle")}, {"START", T("h_more")}, {"SEL", T("h_menu")}}
	if a.input.touch {
		// Con el dedo, aleatorio ya está en los controles: el botón de abajo
		// pone una canción al azar (lo que hace X dos veces).
		hints[1] = hint{"XX", T("h_random")}
	}
	a.drawHints(dst, hints)
}

// drawNothing es la pantalla de reproducción cuando no suena nada.
func (a *App) drawNothing(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	size := 72
	cx, cy := (w-size)/2-12, h/2-size/2-24
	drawVinyl(dst, cx+size/2+24, cy+size/2, size/2-3, a.spin, th.Accent)
	a.drawCover(dst, nil, cx, cy, size, 1)
	frame(dst, cx-1, cy-1, size+2, size+2, withAlpha(th.Text, 0x40))
	msg, hintMsg := T("nothing"), T("nothing_hint")
	if a.lib == nil || len(a.lib.Songs) == 0 {
		hintMsg = T("nothing_empty") // sin música propia: dónde conseguirla
	}
	drawTextShadow(dst, msg, BigFace, (w-textWidth(msg, BigFace))/2, cy+size+12, th.Text, th.Shadow)
	drawText(dst, hintMsg, SmallFace, (w-textWidth(hintMsg, SmallFace))/2, cy+size+30, th.Accent)
	a.drawHints(dst, []hint{{"B", T("library")}, {"SEL", T("h_menu")}})
}

// spinSpeed es cuánto gira el vinilo por frame: frena al pausar.
func (a *App) spinSpeed() float64 {
	target := 0.0
	if a.isPlaying {
		target = vinylSpin / 60
	}
	a.spinVel += (target - a.spinVel) * 0.06
	return a.spinVel
}

// drawMarquee escribe un título grande; si no cabe, se desplaza de lado a lado.
func (a *App) drawMarquee(dst *ebiten.Image, s string, x, y, w int, centered bool) {
	th := a.theme
	tw := textWidth(s, BigFace)
	if tw <= w {
		if centered {
			x += (w - tw) / 2
		}
		drawTextShadow(dst, s, BigFace, x, y, th.Text, th.Shadow)
		return
	}
	a.scrollText(dst, s, BigFace, x, y, w, th.Text, th.Shadow)
}

// scrollText escribe s en un hueco de w en el que no cabe, desplazándose de
// lado a lado con pausas en los extremos, para que se lea entero (los
// títulos grandes, el valor de la baldosa elegida de un menú).
func (a *App) scrollText(dst *ebiten.Image, s string, face text.Face, x, y, w int, c, shadow color.Color) {
	over := float64(textWidth(s, face) - w)
	if over <= 0 {
		drawText(dst, s, face, x, y, c)
		return
	}
	period := over/20 + 3
	ph := math.Mod(a.t, period*2) / period
	f := math.Max(0, math.Min(1, (math.Abs(ph-1)*period-1.5)/(period-3)))
	off := int(f * over)
	// Se desvanece por el lado donde el texto sigue: por la izquierda si ya
	// se desplazó, por la derecha si aún queda.
	drawFaded(dst, s, face, x, y, w, off, off > 0, off < int(over), c, shadow)
}

// drawControls pinta anterior, play/pausa, siguiente, aleatorio y repetir.
func (a *App) drawControls(dst *ebiten.Image, x, y int) {
	th := a.theme
	// Anterior y siguiente se ven pulsados (con su anillo) al cambiar de
	// canción hacia ese lado, como play al pausar.
	for _, b := range []struct {
		dir  int
		x    int
		name string
	}{{-1, x, "prev"}, {1, x + 38, "next"}} {
		col := th.Text
		if a.trackPressed(b.dir) {
			a.drawRing(dst, b.x, y, a.juice.trackAt)
			pop := a.popAt(a.juice.trackAt)
			panel(dst, b.x-pop, y-pop, 15+2*pop, 15+2*pop, th.Accent)
			col = th.Bg
		}
		drawIcon(dst, b.name, b.x+4, y+4, col)
	}
	a.drawRing(dst, x+20, y, a.juice.toggleAt)
	pop := a.playPop()
	panel(dst, x+20-pop, y-pop, 15+2*pop, 15+2*pop, th.Accent)
	name := "pause"
	if !a.isPlaying {
		name = "play"
	}
	drawIcon(dst, name, x+24, y+4, th.Bg)
	shuffleCol, repeatCol := th.Dim, th.Dim
	if a.status.Shuffle {
		shuffleCol = th.Accent
		rect(dst, x+66, y+13, 3, 1, th.Accent) // punto de "activo"
	}
	drawIcon(dst, "shuffle", x+64, y+4, shuffleCol)
	if a.status.Repeat == "all" || a.status.Repeat == "one" {
		repeatCol = th.Accent
		rect(dst, x+84, y+13, 3, 1, th.Accent)
	}
	drawIcon(dst, "repeat", x+82, y+4, repeatCol)
	// Los botones, tocables (con algo de margen, son pequeños; con el dedo,
	// más altos todavía).
	zy, zh := y-4, 23
	if a.input.touch {
		zy, zh = y-10, 35
	}
	a.tapZone(x-3, zy, 21, zh, func() { a.send("prev") })
	a.tapZone(x+18, zy, 21, zh, func() { a.send("toggle") })
	a.tapZone(x+39, zy, 21, zh, func() { a.send("next") })
	a.tapZone(x+60, zy, 18, zh, func() { a.send("shuffle", onOffArg(!a.status.Shuffle)); a.status.Shuffle = !a.status.Shuffle })
	a.tapZone(x+78, zy, 18, zh, func() {
		next := map[string]string{"off": "all", "all": "one", "one": "off"}[orDefault(a.status.Repeat, "off")]
		a.send("repeat", next)
		a.status.Repeat = next
	})
	if a.status.Repeat == "one" {
		drawText(dst, "1", SmallFace, x+90, y+1, th.Accent)
	}
}

// drawSpectrum pinta un visualizador de barras tipo LED. En la maqueta los
// valores son inventados; en la app saldrán del audio.
func (a *App) drawSpectrum(dst *ebiten.Image, x, y, w, h int) {
	if h < 8 {
		return
	}
	th := a.theme
	const bw, gap = 5, 2
	n := (w + gap) / (bw + gap)
	x += (w - (n*(bw+gap) - gap)) / 2
	blocks := h / 3
	if len(a.vizBars) != n {
		a.vizBars, a.vizPeaks, a.vizAvg = make([]float64, n), make([]float64, n), make([]float64, n)
	}
	_, _, bands, real := a.meterNow()
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n)
		var v float64
		if real {
			// El espectro de verdad: cada barra interpola entre dos de las 16
			// bandas (de graves a agudos).
			pos := f * (engine.MeterBands - 1)
			k := int(pos)
			v = bands[k]
			if k+1 < engine.MeterBands {
				v += (bands[k+1] - bands[k]) * (pos - float64(k))
			}
			// Contraste: las mezclas actuales van casi siempre arriba, así
			// que se resalta lo que cambia respecto a lo reciente (~1 s): los
			// golpes suben de golpe y los huecos bajan.
			avg := &a.vizAvg[i]
			*avg += (v - *avg) * 0.02
			v = math.Max(0, math.Min(1, 0.45*v+2.2*(v-*avg)))
		}
		// Sube de golpe y cae despacio, como un vúmetro; el pico se queda un
		// momento arriba.
		bar := &a.vizBars[i]
		*bar = math.Max(v, *bar-0.035)
		peak := &a.vizPeaks[i]
		*peak = math.Max(*bar, *peak-0.012)
		lit := int(*bar*float64(blocks) + 0.5)
		top := int(*peak*float64(blocks) + 0.5)
		for b := 0; b < blocks; b++ {
			by := y + h - (b+1)*3
			c := mix(th.Accent, th.Accent2, float64(b)/float64(blocks))
			switch {
			case b < lit:
			case b == top-1 && top > lit:
				c = th.Text // la marca del pico
			default:
				c = withAlpha(th.Bg2, 0xff)
			}
			a.batch.add(x+i*(bw+gap), by, bw, 2, c)
		}
	}
	a.batch.draw(dst) // todos los bloques de una vez
}

// drawProgress pinta los tiempos y la barra de progreso.
func (a *App) drawProgress(dst *ebiten.Image, x, y, w int) {
	th := a.theme
	if a.duration <= 0 {
		a.drawLiveBar(dst, x, y, w)
		return
	}
	cur := clock(a.shownPos)
	tot := clock(a.duration)
	drawText(dst, cur, SmallFace, x, y, th.Text)
	drawText(dst, tot, SmallFace, x+w-textWidth(tot, SmallFace), y, th.Dim)
	bx := x + textWidth(cur, SmallFace) + 6
	bw := w - (bx - x) - textWidth(tot, SmallFace) - 6
	rect(dst, bx, y+1, bw, 3, th.Bg2)
	fill := int(float64(bw) * a.shownPos / math.Max(a.duration, 1))
	rect(dst, bx, y+1, fill, 3, th.Accent)
	panel(dst, bx+fill-2, y-1, 5, 7, th.Text)
	// Tocar la barra salta a ese punto.
	dur := a.duration
	a.zone(bx, y-5, bw, 15, func(px, _ int) {
		a.send("seek", fmt.Sprintf("%.1f", dur*float64(max(0, min(bw, px)))/float64(bw)))
	})
}

// drawLiveBar es la barra de lo que no tiene duración (una radio): lo que
// se lleva escuchando, una línea con un pulso que la recorre y "● EN VIVO".
func (a *App) drawLiveBar(dst *ebiten.Image, x, y, w int) {
	th := a.theme
	cur := clock(a.shownPos)
	live := T("live")
	drawText(dst, cur, SmallFace, x, y, th.Text)
	lw := textWidth(live, SmallFace)
	dotCol := th.Accent
	if a.isPlaying && a.beatPulse() > 0.5 || !a.isPlaying {
		dotCol = mix(th.Accent, th.Text, 0.5)
	}
	rect(dst, x+w-lw-7, y+1, 4, 4, dotCol) // el punto rojo de "al aire"
	drawText(dst, live, SmallFace, x+w-lw, y, th.Accent)
	bx := x + textWidth(cur, SmallFace) + 6
	bw := w - (bx - x) - lw - 14
	rect(dst, bx, y+2, bw, 1, withAlpha(th.Dim, 0x60))
	if a.isPlaying && bw > 20 {
		p := int(math.Mod(a.t*0.35, 1) * float64(bw-16))
		rect(dst, bx+p, y+1, 16, 3, th.Accent)
	}
}

// drawMiniPlayer pinta lo que suena abajo, mientras se navega.
func (a *App) drawMiniPlayer(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	miniH := 30
	// Mini reproductor.
	my := h - bottomH - miniH - 2
	panel(dst, margin-4, my, w-2*margin+8, miniH, withAlpha(th.Bg2, 0xf0))
	if a.playingSong.Path == "" {
		drawText(dst, T("nothing"), SmallFace, margin+28, my+11, th.Dim)
		a.drawCover(dst, nil, margin, my+4, 22, 1)
		return
	}
	a.drawCover(dst, a.playing, margin, my+4, 22, 1)
	// Tocarlo abre Reproduciendo (los botones, encima, hacen lo suyo).
	a.tapZone(margin-4, my, w-2*margin+8, miniH, func() { a.screen = NowPlaying })
	mx := margin + 28
	mw := w - mx - margin - 22
	if a.input.touch {
		mw = w - mx - margin - 5*miniBtn - 2
	}
	drawTextW(dst, a.playingSong.Title, SmallFace, mx, my+6, mw, th.Text)
	drawTextW(dst, a.playingSong.Artist, SmallFace, mx, my+16, mw, th.Accent)
	rect(dst, margin-3, my+miniH-2, w-2*margin+6, 1, withAlpha(th.Dim, 0x50))
	if a.duration > 0 {
		fill := int(float64(w-2*margin) * a.shownPos / a.duration)
		rect(dst, margin-3, my+miniH-2, fill, 1, th.Accent)
	} else if a.isPlaying {
		// En vivo: un pulso que recorre la línea.
		p := int(math.Mod(a.t*0.35, 1) * float64(w-2*margin-10))
		rect(dst, margin-3+p, my+miniH-2, 16, 1, th.Accent)
	}
	icon := "pause"
	if !a.isPlaying {
		icon = "play"
	}
	if !a.input.touch {
		drawIcon(dst, icon, w-margin-12, my+11, th.Text)
		a.tapZone(w-margin-18, my, 22, miniH, func() { a.send("toggle") })
		return
	}
	// Con el dedo, los controles a mano: anterior, -10 s, pausa, +10 s y
	// siguiente, con zonas del alto del reproductor (los iconos son
	// pequeños).
	bx := w - margin - 5*miniBtn + 2
	for _, b := range []struct {
		icon, text string
		do         func()
	}{
		{"prev", "", func() { a.send("prev") }},
		{"", "-10", func() { a.send("seek", "-10") }},
		{icon, "", func() { a.send("toggle") }},
		{"", "+10", func() { a.send("seek", "+10") }},
		{"next", "", func() { a.send("next") }},
	} {
		cx := bx + miniBtn/2
		if b.icon == icon && b.text == "" {
			panel(dst, cx-8, my+7, 16, 16, th.Accent)
			drawIcon(dst, b.icon, cx-3, my+11, th.Bg)
		} else if b.icon != "" {
			drawIcon(dst, b.icon, cx-3, my+11, th.Text)
		} else {
			drawText(dst, b.text, SmallFace, cx-textWidth(b.text, SmallFace)/2, my+12, th.Dim)
		}
		a.tapZone(bx, my, miniBtn, miniH, b.do)
		bx += miniBtn
	}
}

// miniBtn es el ancho de cada botón del mini reproductor con el dedo.
const miniBtn = 19

// volumeShow es cuánto se ve el indicador de volumen después de un cambio.
const volumeShow = 1.6

// drawVolume pinta el indicador de volumen: un altavoz, la barra y el
// porcentaje. Lo que pasa del 100 % va en el segundo color del tema, para que
// se note que se está amplificando.
// showVolume muestra el indicador de volumen. Si ya se ve, sigue ahí (solo
// cambia el número y la barra); si se estaba yendo, vuelve sin entrar otra vez.
func (a *App) showVolume() {
	age := a.t - a.volumeAt
	switch {
	case a.volumeAt == 0 || age > volumeShow:
		a.volumeIn = a.t // no se veía: entra desde arriba
	case age > volumeShow-0.2:
		a.volumeIn = a.t - 1 // se estaba yendo: vuelve a su sitio de golpe
	}
	a.volumeAt = a.t
}

func (a *App) drawVolume(dst *ebiten.Image) {
	age := a.t - a.volumeAt
	if a.volumeAt == 0 || age > volumeShow {
		return
	}
	in := a.t - a.volumeIn
	w := dst.Bounds().Dx()
	th := a.theme
	const bw, ph = 120, 21
	pw := bw + 52
	px, py := (w-pw)/2, topH+6
	// Entra y sale deslizándose desde arriba.
	switch {
	case in < 0.12:
		py -= int((0.12 - in) / 0.12 * 30)
	case age > volumeShow-0.2:
		py -= int((age - (volumeShow - 0.2)) / 0.2 * 30)
	}
	rect(dst, px+2, py+2, pw, ph, th.Shadow)
	panel(dst, px, py, pw, ph, th.Bg2)
	frame(dst, px, py, pw, ph, withAlpha(th.Accent, 0x80))
	drawIcon(dst, "speaker", px+6, py+7, th.Text)
	bx, by := px+18, py+8
	rect(dst, bx, by, bw, 5, th.Shadow)
	full := int(float64(bw) * min(a.volume, volumeMax) / volumeMax)
	half := bw / 2 // el 100 %
	rect(dst, bx, by, min(full, half), 5, th.Accent)
	if full > half {
		rect(dst, bx+half, by, full-half, 5, th.Accent2)
	}
	rect(dst, bx+half, by-2, 1, 9, th.Text) // marca del 100 %
	pct := fmt.Sprintf("%d%%", int(a.volume*100+0.5))
	drawText(dst, pct, SmallFace, px+pw-6-textWidth(pct, SmallFace), py+8, th.Text)
}
