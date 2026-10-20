package ui

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"github.com/juanvillacortac/audiofilus/internal/tracker"
)

// La pantalla del tracker (en Juegos): hacer canciones chiptune con el
// mando, como en LSDJ, con cosas de PICO-8 (sus ondas, el instrumento en
// cada paso y un piano roll). Ocho vistas en fila, como el mapa de LSDJ:
//
//	PROYECTO · CANCIÓN · CADENA · FRASE · INSTRUMENTO · TABLA · GROOVE · ONDA
//
// L1/R1 pasan a la vista de al lado; R1 entra en lo que hay bajo el cursor
// (la cadena de la celda de la canción, la frase del paso de la cadena, el
// instrumento del paso de la frase...). En todas se edita igual: la cruceta
// mueve el cursor; con A mantenida, la cruceta cambia el valor (↑↓ de a
// uno, ←→ de a mucho); A en una celda vacía pone lo último que se usó y dos
// veces A, uno nuevo sin usar; B+A borra; B+←→ cambia de cadena, frase,
// instrumento... (B+↑↓ salta 16 filas en la canción). Y toca o para (la
// canción, la cadena o la frase, según la vista) y B+Y silencia el canal; X
// en la frase cambia al piano roll. Start abre el menú: deshacer, copiar,
// pegar, clonar, las canciones, exportar a WAV. Se guarda sola en
// data/tracker/.

// tkView es una vista del tracker.
type tkView int

const (
	tvProject tkView = iota
	tvSong
	tvChain
	tvPhrase
	tvInstr
	tvTable
	tvGroove
	tvWave
	tvCount
)

// tkViewNames son las pestañas de arriba (abreviaturas, como en LSDJ).
var tkViewNames = [tvCount]string{"PROJ", "SONG", "CHAIN", "PHRASE", "INSTR", "TABLE", "GROOVE", "WAVE"}

// tkViewShort son las pestañas para pantallas estrechas.
var tkViewShort = [tvCount]string{"PRJ", "SNG", "CHN", "PHR", "INS", "TBL", "GRV", "WAV"}

// trackerState es el tracker abierto.
type trackerState struct {
	song      *tracker.Song
	file      string // dónde se guarda (en data/tracker/)
	player    *tracker.Player
	streaming bool // su salida de audio está abierta
	// an mide lo que sale (meterTap): con él reaccionan los visualizadores y
	// los juegos a lo que toca el tracker.
	an         *engine.Analyzer
	playing    bool
	playWhat   tracker.What
	playTarget int

	view tkView
	ch   int // el canal en que se trabaja (la columna de la canción)
	// Los cursores de cada vista.
	songRow, songScroll int
	chain, chainRow     int
	chainCol            int
	phrase, phRow       int
	phCol               int
	roll                bool // la frase, en piano roll
	rollNote, rollBase  int
	instr, instrRow     int
	table, tbRow, tbCol int
	groove, grRow       int
	wave, waveX         int
	projRow             int

	// Lo último que se puso, para A en una celda vacía.
	lastNote   int8
	lastInstr  int
	lastVol    int8
	lastCmd    tracker.Cmd
	lastChain  int
	lastPhrase int

	undo     [][]byte // la canción antes de cada cambio (JSON)
	editing  bool     // ya se guardó el deshacer de esta pulsación de A
	clip     any      // lo copiado: *Chain, *Phrase, *Instrument, *Table, Groove o *Wave
	tapKey   string   // la celda del último toque de A (para el doble toque)
	tapAt    float64
	aEdited  bool // A se usó para cambiar un valor (al soltarla no es un toque)
	aWasHeld bool
	dirty    bool
	dirtyAt  float64
	exported chan string // el archivo exportado (o el error), desde la goroutine
	hits     [tracker.MaxChannels]int
	hitAt    [tracker.MaxChannels]float64 // cuándo sonó la última nota de cada canal
	scope    []float32
}

// Las canciones del tracker van en data/tracker/, una por archivo .json;
// "current" dice cuál estaba abierta.
func (a *App) trackerDir() string { return filepath.Join(a.paths.Data, "tracker") }

// newTracker abre la canción que estaba abierta o, la primera vez, la
// primera de ejemplo.
func (a *App) newTracker() *trackerState {
	t := &trackerState{player: tracker.NewPlayer(sfx.Rate), lastNote: 49, lastVol: 12, lastCmd: tracker.Cmd{Code: tracker.CmdChord, Arg: 0x47},
		exported: make(chan string, 1), view: tvSong}
	file := "song.json"
	if b, err := os.ReadFile(filepath.Join(a.trackerDir(), "current")); err == nil && len(b) > 0 {
		file = filepath.Base(string(b))
	}
	s, err := tracker.Load(filepath.Join(a.trackerDir(), file))
	if err != nil {
		s, file = tracker.Examples()[0], "example-"+slugFile(tracker.Examples()[0].Name)+".json"
	}
	t.song, t.file = s, file
	t.player.SetSong(s)
	return t
}

// trackerSongs son las canciones guardadas: archivo y nombre.
//
// Las copias de los ejemplos (example-*.json) no salen: se abren desde su
// ejemplo.
func (a *App) trackerSongs() [][2]string {
	files, _ := filepath.Glob(filepath.Join(a.trackerDir(), "*.json"))
	var out [][2]string
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), "example-") {
			continue
		}
		if s, err := tracker.Load(f); err == nil {
			out = append(out, [2]string{filepath.Base(f), s.Name})
		}
	}
	return out
}

// openTrackerSong deja abierta s (que se guarda en file), guardando antes la
// que había. Si save, se guarda ya (una canción nueva); si no (un ejemplo),
// solo cuando se edite: abrir ejemplos no deja archivos.
func (a *App) openTrackerSong(s *tracker.Song, file string, save bool) {
	t := a.tracker
	a.saveTracker()
	if t.playing {
		a.trackerToggle()
	}
	t.song, t.file = s, file
	t.view, t.ch, t.songRow, t.songScroll = tvSong, 0, 0, 0
	t.chain, t.chainRow, t.chainCol, t.phrase, t.phRow, t.phCol = 0, 0, 0, 0, 0, 0
	t.instr, t.instrRow, t.table, t.tbRow, t.tbCol, t.groove, t.grRow, t.wave, t.waveX = 0, 0, 0, 0, 0, 0, 0, 0, 0
	t.undo, t.roll = nil, false
	t.player.SetSong(s)
	t.dirty = save
	a.saveTracker()
}

// openTrackerSongs es el menú de canciones: una nueva (de Game Boy o libre),
// las guardadas y las de ejemplo (si se editó un ejemplo, abre esa versión).
func (a *App) openTrackerSongs() {
	t := a.tracker
	saved := a.trackerSongs()
	newSong := func(mode tracker.Mode) func() {
		return func() {
			name := fmt.Sprintf(T("tk_new_name"), len(saved)+1)
			a.openTrackerSong(tracker.NewSong(name, mode), fmt.Sprintf("song-%d.json", time.Now().Unix()), true)
		}
	}
	items := []menuItem{
		{label: T("tk_new") + " · " + T("tk_mode_gb"), icon: "tracker", do: newSong(tracker.ModeGB)},
		{label: T("tk_new") + " · " + T("tk_mode_free"), icon: "tracker", do: newSong(tracker.ModeFree)},
	}
	for _, f := range saved {
		label := f[1]
		if f[0] == t.file {
			label = "• " + label
		}
		items = append(items, menuItem{label: label, icon: "note", do: func() {
			if s, err := tracker.Load(filepath.Join(a.trackerDir(), f[0])); err == nil {
				a.openTrackerSong(s, f[0], false)
			}
		}})
	}
	for _, ex := range tracker.Examples() {
		file := "example-" + slugFile(ex.Name) + ".json"
		label := T("tk_example") + " " + ex.Name
		if file == t.file {
			label = "• " + label
		}
		items = append(items, menuItem{label: label, icon: "play", do: func() {
			if s, err := tracker.Load(filepath.Join(a.trackerDir(), file)); err == nil {
				a.openTrackerSong(s, file, false) // la versión editada
				return
			}
			a.openTrackerSong(ex, file, false)
		}})
	}
	a.menu = &menu{title: T("tk_songs"), subtitle: t.song.Name, items: items}
}

// slugFile es un nombre para archivo: minúsculas y guiones.
func slugFile(s string) string {
	out := []rune{}
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case len(out) > 0 && out[len(out)-1] != '-':
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}

// tkUndoPoint guarda la canción para deshacer, una vez por pulsación de A
// (todo lo que se cambia manteniéndola se deshace de una vez).
func (a *App) tkUndoPoint() {
	t := a.tracker
	if t.editing {
		return
	}
	t.editing = true
	if b, err := json.Marshal(t.song); err == nil {
		t.undo = append(t.undo, b)
		if len(t.undo) > 64 {
			t.undo = t.undo[1:]
		}
	}
}

// tkUndo vuelve la canción a como estaba antes del último cambio.
func (a *App) tkUndo() {
	t := a.tracker
	if len(t.undo) == 0 {
		return
	}
	var s tracker.Song
	if json.Unmarshal(t.undo[len(t.undo)-1], &s) != nil {
		return
	}
	t.undo = t.undo[:len(t.undo)-1]
	s.Fix()
	t.song = &s
	a.trackerChanged()
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
	_ = t.song.Save(filepath.Join(a.trackerDir(), t.file))
	_ = os.WriteFile(filepath.Join(a.trackerDir(), "current"), []byte(t.file), 0o644)
}

// trackerToggle toca o para lo de la vista: la canción desde la fila del
// cursor, la cadena o la frase en bucle (las demás vistas tocan la frase).
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
	what, target := tracker.PlaySong, t.songRow
	switch t.view {
	case tvChain:
		what, target = tracker.PlayChain, t.chain
	case tvPhrase, tvInstr, tvTable, tvGroove, tvWave:
		if t.song.Phrases[t.phrase] != nil {
			what, target = tracker.PlayPhrase, t.phrase
		}
	}
	if what == tracker.PlaySong {
		target = min(target, len(t.song.Rows)-1)
	}
	t.player.Play(what, target, t.ch)
	t.playing, t.playWhat, t.playTarget = true, what, target
}

// updateTracker atiende el tracker.
func (a *App) updateTracker() {
	t, in := a.tracker, &a.input
	if !t.streaming && !a.offline {
		// Lo que sale se oye tras el búfer de la salida (50 ms) y el de oto.
		t.an = engine.NewAnalyzer(sfx.Rate, 100*time.Millisecond)
		a.playStream(&meterTap{r: t.player, an: t.an})
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
	a.trackerFollow()
	aHeld := in.duration(ActConfirm) > 0
	bHeld := in.duration(ActBack) > 0
	switch {
	case in.Pressed(ActMenu):
		a.openTrackerMenu()
		return
	case in.Pressed(ActPause) && bHeld: // B+Y: silenciar el canal
		m := t.player.Muted()
		t.player.SetMuted(t.ch, !m[t.ch])
		return
	case in.Pressed(ActPause):
		a.trackerToggle()
		return
	case in.Pressed(ActShuffle) && t.view == tvPhrase:
		t.roll = !t.roll
		return
	case in.Pressed(ActPrev) && !aHeld:
		a.tkGo(-1)
		return
	case in.Pressed(ActNext) && !aHeld:
		a.tkGo(1)
		return
	}
	if in.Pressed(ActConfirm) {
		t.aEdited, t.editing = false, false
	}
	if bHeld && !aHeld {
		if dx, dy := trackerDir(in); dx != 0 || dy != 0 {
			a.tkItem(dx, dy)
			return
		}
	}
	switch t.view {
	case tvProject:
		a.updateTkProject(aHeld, bHeld)
	case tvSong:
		a.updateTkSong(aHeld, bHeld)
	case tvChain:
		a.updateTkChain(aHeld, bHeld)
	case tvPhrase:
		if t.roll {
			a.updateTkRoll(aHeld, bHeld)
		} else {
			a.updateTkPhrase(aHeld, bHeld)
		}
	case tvInstr:
		a.updateTkInstr(aHeld, bHeld)
	case tvTable:
		a.updateTkTable(aHeld, bHeld)
	case tvGroove:
		a.updateTkGroove(aHeld, bHeld)
	case tvWave:
		a.updateTkWave(aHeld, bHeld)
	}
	t.aWasHeld = aHeld
	// Tocando una cadena o una frase sola, si se cambia de cadena o de
	// frase, suena la nueva.
	if t.playing {
		switch {
		case t.playWhat == tracker.PlayChain && t.view == tvChain && t.playTarget != t.chain,
			t.playWhat == tracker.PlayPhrase && t.view == tvPhrase && t.playTarget != t.phrase:
			t.playing = false
			a.trackerToggle()
		}
	}
}

// tkGo pasa a la vista de al lado; hacia la derecha, entra en lo que hay
// bajo el cursor.
func (a *App) tkGo(d int) {
	t, s := a.tracker, a.tracker.song
	next := tkView(max(0, min(int(tvCount)-1, int(t.view)+d)))
	if d > 0 {
		switch t.view {
		case tvSong:
			if t.songRow < len(s.Rows) {
				if id := s.Rows[t.songRow][t.ch]; id != tracker.Empty {
					t.chain, t.chainRow = int(id), 0
				}
			}
		case tvChain:
			if c := s.Chains[t.chain]; c != nil && c[t.chainRow].Phrase != tracker.Empty {
				t.phrase, t.phRow = int(c[t.chainRow].Phrase), 0
			}
		case tvPhrase:
			// El instrumento del paso, o el último que sonó antes en la
			// frase.
			if p := s.Phrases[t.phrase]; p != nil {
				for r := t.phRow; r >= 0; r-- {
					if p[r].Instr != tracker.Empty {
						t.instr = int(p[r].Instr)
						break
					}
				}
			}
		case tvInstr:
			if in := s.Instrs[t.instr]; in != nil && in.Table != tracker.Empty {
				t.table = int(in.Table)
			}
		case tvGroove:
			if in := s.Instrs[t.instr]; in != nil && in.Type == tracker.InstrWave {
				t.wave = int(in.Wave)
			}
		}
	}
	t.view = next
}

// tkItem es B+cruceta: B+←→ cambia de cadena, frase, instrumento, tabla,
// groove u onda; B+↑↓ salta 16 filas en la canción.
func (a *App) tkItem(dx, dy int) {
	t := a.tracker
	step := func(v *int, n int) { *v = (*v + dx + n) % n }
	switch t.view {
	case tvSong:
		if dy != 0 {
			t.songRow = max(0, min(min(len(t.song.Rows), tracker.SongRows-1), t.songRow+16*dy))
		}
	case tvChain:
		step(&t.chain, tracker.NumChains)
	case tvPhrase:
		step(&t.phrase, tracker.NumPhrases)
	case tvInstr:
		step(&t.instr, tracker.NumInstr)
	case tvTable:
		step(&t.table, tracker.NumTables)
	case tvGroove:
		step(&t.groove, tracker.NumGrooves)
	case tvWave:
		step(&t.wave, tracker.NumWaves)
	}
}

// trackerFollow sigue lo que suena: enciende la celda de cada nota que
// sonó.
func (a *App) trackerFollow() {
	t := a.tracker
	if h := t.player.Hits(); h != t.hits {
		for c := range h {
			if h[c] != t.hits[c] {
				t.hitAt[c] = a.t
			}
		}
		t.hits = h
	}
	if t.playing && !t.player.Playing() {
		t.playing = false // se acabó sola (no pasa tocando en bucle)
	}
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

// tkField es un valor que se edita en una celda.
type tkField struct {
	key      string // la celda (para el doble toque de A)
	val      int
	emptyVal int // el valor de vacío (tracker.Empty, o NoteEmpty en las notas)
	canEmpty bool
	min, max int
	big      int  // lo que cambia A+←→
	def      int  // lo que pone A en una celda vacía
	fresh    int  // lo que pone el doble toque de A: uno nuevo sin usar (-1: nada)
	skip0    bool // las notas: el 0 no es un valor (bajo C-0 va OFF)
	set      func(int)
	tap      func() // A en una celda llena (oír la nota, p. ej.)
}

// tkEdit edita f con los botones de este fotograma; dice si los usó.
func (a *App) tkEdit(f tkField, aHeld, bHeld bool) bool {
	t, in := a.tracker, &a.input
	change := func(v int) {
		a.tkUndoPoint()
		f.set(v)
		a.trackerChanged()
	}
	if in.Pressed(ActConfirm) {
		switch {
		case bHeld:
			if f.canEmpty && f.val != f.emptyVal {
				change(f.emptyVal)
			}
		case f.val == f.emptyVal:
			change(f.def)
			t.tapKey, t.tapAt = f.key, a.t
		case f.key == t.tapKey && a.t-t.tapAt < 0.4 && f.fresh >= 0:
			change(f.fresh) // el doble toque: uno nuevo
			t.tapKey = ""
		default:
			if f.tap != nil {
				f.tap()
			}
		}
		return true
	}
	dx, dy := trackerDir(in)
	if !aHeld || (dx == 0 && dy == 0) {
		return aHeld
	}
	v := f.val
	if v == f.emptyVal {
		v = f.def
	} else {
		v += -dy + f.big*dx
		if f.skip0 && v == 0 {
			v = -1
			if -dy+f.big*dx > 0 {
				v = 1
			}
		}
	}
	change(max(f.min, min(f.max, v)))
	return true
}

// freeID es el primer número sin usar de un mapa de n.
func freeID[T any](m map[int]T, n int) int {
	for i := 0; i < n; i++ {
		if _, ok := m[i]; !ok {
			return i
		}
	}
	return -1
}

// La canción: filas × canales de cadenas.
func (a *App) updateTkSong(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	t.ch = min(t.ch, s.Channels()-1)
	row, ch := t.songRow, t.ch
	val := tracker.Empty
	if row < len(s.Rows) {
		val = int(s.Rows[row][ch])
	}
	// Soltar A sin haber cambiado nada en una celda con cadena: abrirla.
	if !aHeld && t.aWasHeld && !t.aEdited && val != tracker.Empty {
		t.chain, t.chainRow, t.view = val, 0, tvChain
		return
	}
	f := tkField{key: fmt.Sprintf("s%d.%d", row, ch), val: val, emptyVal: tracker.Empty, canEmpty: true,
		max: tracker.NumChains - 1, big: 16, def: t.lastChain, fresh: freeID(s.Chains, tracker.NumChains),
		set: func(v int) {
			for len(s.Rows) <= row {
				s.Rows = append(s.Rows, tracker.EmptyRow())
			}
			s.Rows[row][ch] = int16(v)
			if v != tracker.Empty {
				if s.Chains[v] == nil {
					s.Chains[v] = tracker.NewChain()
				}
				t.lastChain = v
			}
			// Las filas vacías del final sobran (queda al menos una).
			for len(s.Rows) > 1 && s.Rows[len(s.Rows)-1] == tracker.EmptyRow() {
				s.Rows = s.Rows[:len(s.Rows)-1]
			}
		}}
	if f.def == tracker.Empty {
		f.def = 0
	}
	if a.tkEdit(f, aHeld, bHeld) {
		t.aEdited = t.aEdited || f.val == tracker.Empty || bHeld
		return
	}
	dx, dy := trackerDir(in)
	t.songRow = max(0, min(min(len(s.Rows), tracker.SongRows-1), t.songRow+dy))
	t.ch = (t.ch + dx + s.Channels()) % s.Channels()
}

// La cadena: 16 pasos de frase y transposición.
func (a *App) updateTkChain(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	c := s.Chains[t.chain]
	get := func() *tracker.Chain {
		if s.Chains[t.chain] == nil {
			s.Chains[t.chain] = tracker.NewChain()
		}
		return s.Chains[t.chain]
	}
	r := t.chainRow
	st := tracker.ChainStep{Phrase: tracker.Empty}
	if c != nil {
		st = c[r]
	}
	if !aHeld && t.aWasHeld && !t.aEdited && t.chainCol == 0 && st.Phrase != tracker.Empty {
		t.phrase, t.phRow, t.view = int(st.Phrase), 0, tvPhrase
		return
	}
	var f tkField
	if t.chainCol == 0 {
		f = tkField{key: fmt.Sprintf("c%d.%d", t.chain, r), val: int(st.Phrase), emptyVal: tracker.Empty, canEmpty: true,
			max: tracker.NumPhrases - 1, big: 16, def: max(0, t.lastPhrase), fresh: freeID(s.Phrases, tracker.NumPhrases),
			set: func(v int) {
				get()[r].Phrase = int16(v)
				if v != tracker.Empty {
					if s.Phrases[v] == nil {
						s.Phrases[v] = tracker.NewPhrase()
					}
					t.lastPhrase = v
				}
			}}
	} else {
		f = tkField{key: fmt.Sprintf("ct%d.%d", t.chain, r), val: int(st.Transpose), emptyVal: math.MinInt, min: -128, max: 127, big: 12,
			set: func(v int) { get()[r].Transpose = int8(v) }}
	}
	if a.tkEdit(f, aHeld, bHeld) {
		t.aEdited = t.aEdited || f.val == f.emptyVal || bHeld
		return
	}
	dx, dy := trackerDir(in)
	t.chainRow = (t.chainRow + dy + tracker.Steps) % tracker.Steps
	t.chainCol = (t.chainCol + dx + 2) % 2
}

// phraseStep es el paso del cursor de la frase (creándola si hace falta).
func (a *App) phraseGet() *tracker.Phrase {
	s := a.tracker.song
	if s.Phrases[a.tracker.phrase] == nil {
		s.Phrases[a.tracker.phrase] = tracker.NewPhrase()
	}
	return s.Phrases[a.tracker.phrase]
}

// tkPreview hace sonar una nota con un instrumento en el canal en que se
// trabaja (con la canción parada).
func (a *App) tkPreview(note int8, instr int) {
	t := a.tracker
	if instr == tracker.Empty {
		instr = t.lastInstr
	}
	t.player.Preview(t.ch, note, instr)
}

// cmdFields son los dos campos de un comando: la letra (por el orden de
// tracker.Commands) y el argumento.
func (a *App) cmdFields(key string, cmd tracker.Cmd, set func(tracker.Cmd)) (code, arg tkField) {
	t := a.tracker
	idx := 0
	for i, c := range tracker.Commands {
		if c == cmd.Code {
			idx = i + 1
		}
	}
	lastIdx := 1
	for i, c := range tracker.Commands {
		if c == t.lastCmd.Code {
			lastIdx = i + 1
		}
	}
	code = tkField{key: key, val: idx, emptyVal: 0, canEmpty: true, min: 1, max: len(tracker.Commands), big: 4, def: lastIdx, fresh: -1,
		set: func(v int) {
			if v == 0 {
				set(tracker.Cmd{})
				return
			}
			c := tracker.Cmd{Code: tracker.Commands[v-1], Arg: cmd.Arg}
			if cmd.None() && c.Code == t.lastCmd.Code {
				c.Arg = t.lastCmd.Arg
			}
			set(c)
			t.lastCmd = c
		}}
	arg = tkField{key: key + "a", val: int(cmd.Arg), emptyVal: math.MinInt, max: 255, big: 16,
		set: func(v int) {
			c := tracker.Cmd{Code: cmd.Code, Arg: byte(v)}
			set(c)
			t.lastCmd = c
		}}
	if cmd.None() {
		arg.set = func(int) {} // sin letra, el argumento no hace nada
	}
	return code, arg
}

// La frase: 16 pasos de nota, instrumento, volumen y dos comandos.
func (a *App) updateTkPhrase(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	r := t.phRow
	st := tracker.Step{Instr: tracker.Empty, Vol: tracker.Empty}
	if p := s.Phrases[t.phrase]; p != nil {
		st = p[r]
	}
	key := fmt.Sprintf("p%d.%d.%d", t.phrase, r, t.phCol)
	var f tkField
	switch t.phCol {
	case 0:
		f = tkField{key: key, val: int(st.Note), emptyVal: tracker.NoteEmpty, canEmpty: true, min: -1, max: tracker.NoteMax, big: 12,
			def: int(t.lastNote), fresh: -1, skip0: true,
			set: func(v int) {
				p := a.phraseGet()
				p[r].Note = int8(v)
				if v > 0 {
					if p[r].Instr == tracker.Empty {
						p[r].Instr = int16(t.lastInstr)
					}
					t.lastNote = int8(v)
					a.tkPreview(int8(v), int(p[r].Instr))
				}
			},
			tap: func() { a.tkPreview(st.Note, int(st.Instr)) }}
	case 1:
		f = tkField{key: key, val: int(st.Instr), emptyVal: tracker.Empty, canEmpty: true, max: tracker.NumInstr - 1, big: 16,
			def: t.lastInstr, fresh: freeID(s.Instrs, tracker.NumInstr),
			set: func(v int) {
				a.phraseGet()[r].Instr = int16(v)
				if v != tracker.Empty {
					if s.Instrs[v] == nil {
						s.Instrs[v] = tracker.DefaultInstrument(tracker.InstrPulse)
					}
					t.lastInstr = v
				}
			}}
	case 2:
		f = tkField{key: key, val: int(st.Vol), emptyVal: tracker.Empty, canEmpty: true, max: 15, big: 4, def: int(t.lastVol), fresh: -1,
			set: func(v int) {
				a.phraseGet()[r].Vol = int8(v)
				if v >= 0 {
					t.lastVol = int8(v)
				}
			}}
	default:
		i := (t.phCol - 3) / 2
		code, arg := a.cmdFields(key, st.Cmds[i], func(c tracker.Cmd) { a.phraseGet()[r].Cmds[i] = c })
		f = code
		if (t.phCol-3)%2 == 1 {
			f = arg
		}
	}
	if a.tkEdit(f, aHeld, bHeld) {
		return
	}
	dx, dy := trackerDir(in)
	t.phRow = (t.phRow + dy + tracker.Steps) % tracker.Steps
	t.phCol = (t.phCol + dx + 7) % 7
}

// El piano roll de la frase (como en PICO-8): los pasos en horizontal y las
// notas en vertical. ←→ eligen el paso, ↑↓ la nota; A pone o quita la nota,
// A+↑↓ mueve la del paso, B+A la borra.
func (a *App) updateTkRoll(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	r := t.phRow
	st := tracker.Step{Instr: tracker.Empty, Vol: tracker.Empty}
	if p := s.Phrases[t.phrase]; p != nil {
		st = p[r]
	}
	if t.rollNote <= 0 {
		t.rollNote = int(t.lastNote)
	}
	put := func(n int8) {
		a.tkUndoPoint()
		p := a.phraseGet()
		p[r].Note = n
		if n > 0 {
			if p[r].Instr == tracker.Empty {
				p[r].Instr = int16(t.lastInstr)
			}
			t.lastNote = n
			a.tkPreview(n, int(p[r].Instr))
		}
		a.trackerChanged()
	}
	if in.Pressed(ActConfirm) {
		switch {
		case bHeld || int(st.Note) == t.rollNote:
			if st.Note != tracker.NoteEmpty {
				put(tracker.NoteEmpty)
			}
		default:
			put(int8(t.rollNote))
		}
		return
	}
	dx, dy := trackerDir(in)
	if aHeld {
		if dy != 0 && st.Note > 0 {
			t.rollNote = max(1, min(tracker.NoteMax, int(st.Note)-dy))
			put(int8(t.rollNote))
		}
		return
	}
	t.phRow = (t.phRow + dx + tracker.Steps) % tracker.Steps
	t.rollNote = max(1, min(tracker.NoteMax, t.rollNote-dy))
	if dx != 0 && st.Note > 0 {
		// Al pasar de paso, el cursor va a la nota de ese paso si tiene.
		if p := s.Phrases[t.phrase]; p != nil && p[t.phRow].Note > 0 {
			t.rollNote = int(p[t.phRow].Note)
		}
	}
}

// tkParam es una fila de las vistas de lista (instrumento, proyecto).
type tkParam struct {
	label string
	show  string
	field tkField
	do    func() // A: en vez de editar (el nombre: el teclado)
}

// hexStr es v en hexadecimal de n cifras.
func hexStr(v, n int) string { return fmt.Sprintf("%0*X", n, v) }

// signedHex es una transposición: +0C, -03.
func signedHex(v int) string {
	if v < 0 {
		return "-" + hexStr(-v, 2)
	}
	return "+" + hexStr(v, 2)
}

// onOff es "ON" u "OFF".
func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// instrTypes son los tipos que admite el modo de la canción.
func instrTypes(m tracker.Mode) []tracker.InstrType {
	if m == tracker.ModeFree {
		return []tracker.InstrType{tracker.InstrPulse, tracker.InstrWave, tracker.InstrNoise, tracker.InstrSynth}
	}
	return []tracker.InstrType{tracker.InstrPulse, tracker.InstrWave, tracker.InstrNoise}
}

// synthShapes son las formas del synth.
var synthShapes = []string{"triangle", "sine", "saw", "square", "organ", "phantom", "noise"}

// tkInstrParams son las filas del instrumento abierto (si no existe, uno de
// pulso que se crea al tocarlo).
func (a *App) tkInstrParams() []tkParam {
	t, s := a.tracker, a.tracker.song
	in := s.Instrs[t.instr]
	if in == nil {
		in = tracker.DefaultInstrument(tracker.InstrPulse)
	}
	id := t.instr
	mk := func(f func(v int)) func(int) {
		return func(v int) {
			if s.Instrs[id] == nil {
				s.Instrs[id] = in
			}
			f(v)
		}
	}
	num := func(key string, v, lo, hi, big int, set func(int)) tkField {
		return tkField{key: key, val: v, emptyVal: math.MinInt, min: lo, max: hi, big: big, set: mk(set)}
	}
	types := instrTypes(s.Mode)
	ti := 0
	for i, ty := range types {
		if ty == in.Type {
			ti = i
		}
	}
	ps := []tkParam{
		{label: T("tk_i_name"), show: strings.ToUpper(in.Name), do: func() {
			a.openPrompt(T("tk_i_name"), in.Name, func(n string) {
				if s.Instrs[id] == nil {
					s.Instrs[id] = in
				}
				in.Name = strings.ToLower(n)
				a.trackerChanged()
			})
		}},
		{label: T("tk_i_type"), show: strings.ToUpper(string(in.Type)), field: num("type", ti, 0, len(types)-1, 1, func(v int) {
			*in = *tracker.DefaultInstrument(types[v])
		})},
		{label: T("tk_i_vol"), show: hexStr(int(in.Volume), 1), field: num("vol", int(in.Volume), 0, 15, 4, func(v int) { in.Volume = int8(v) })},
	}
	if s.Mode == tracker.ModeGB {
		if in.Type != tracker.InstrWave {
			dir := map[int8]string{-1: T("tk_i_down"), 0: T("tk_i_flat"), 1: T("tk_i_up")}[in.Env.Dir]
			ps = append(ps,
				tkParam{label: T("tk_i_env"), show: hexStr(int(in.Env.Start), 1), field: num("env", int(in.Env.Start), 0, 15, 4, func(v int) { in.Env.Start = int8(v) })},
				tkParam{label: T("tk_i_dir"), show: dir, field: num("dir", int(in.Env.Dir), -1, 1, 1, func(v int) { in.Env.Dir = int8(v) })},
				tkParam{label: T("tk_i_speed"), show: hexStr(int(in.Env.Period), 1), field: num("per", int(in.Env.Period), 0, 7, 2, func(v int) { in.Env.Period = int8(v) })})
		}
	} else {
		ps = append(ps,
			tkParam{label: T("tk_i_attack"), show: hexStr(in.Env.Attack, 2), field: num("a", in.Env.Attack, 0, 255, 16, func(v int) { in.Env.Attack = v })},
			tkParam{label: T("tk_i_decay"), show: hexStr(in.Env.Decay, 2), field: num("d", in.Env.Decay, 0, 255, 16, func(v int) { in.Env.Decay = v })},
			tkParam{label: T("tk_i_sustain"), show: hexStr(int(in.Env.Sustain), 1), field: num("s", int(in.Env.Sustain), 0, 15, 4, func(v int) { in.Env.Sustain = int8(v) })},
			tkParam{label: T("tk_i_release"), show: hexStr(in.Env.Release, 2), field: num("r", in.Env.Release, 0, 255, 16, func(v int) { in.Env.Release = v })})
	}
	length := "--"
	if in.Length > 0 {
		length = hexStr(in.Length, 2)
	}
	table := "--"
	if in.Table != tracker.Empty {
		table = hexStr(int(in.Table), 2)
	}
	pan := map[int8]string{-1: "L", 0: "LR", 1: "R"}[in.Pan]
	ps = append(ps,
		tkParam{label: T("tk_i_length"), show: length, field: num("len", in.Length, 0, 255, 16, func(v int) { in.Length = v })},
		tkParam{label: T("tk_i_table"), show: table, field: tkField{key: "table", val: int(in.Table), emptyVal: tracker.Empty, canEmpty: true,
			max: tracker.NumTables - 1, big: 4, def: 0, fresh: freeID(s.Tables, tracker.NumTables), set: mk(func(v int) {
				in.Table = int16(v)
				if v != tracker.Empty && s.Tables[v] == nil {
					s.Tables[v] = tracker.NewTable()
				}
			})}},
		tkParam{label: T("tk_i_pan"), show: pan, field: num("pan", int(in.Pan), -1, 1, 1, func(v int) { in.Pan = int8(v) })},
		tkParam{label: T("tk_i_transpose"), show: onOff(in.Transpose), field: num("tr", b2i(in.Transpose), 0, 1, 1, func(v int) { in.Transpose = v == 1 })},
		tkParam{label: T("tk_i_vibrato"), show: hexStr(int(in.Vibrato), 2), field: num("vib", int(in.Vibrato), 0, 255, 16, func(v int) { in.Vibrato = byte(v) })},
	)
	duty := [4]string{"12.5%", "25%", "50%", "75%"}
	switch in.Type {
	case tracker.InstrPulse:
		ps = append(ps, tkParam{label: T("tk_i_duty"), show: duty[in.Duty&3], field: num("duty", int(in.Duty), 0, 3, 1, func(v int) { in.Duty = int8(v) })})
		if s.Mode == tracker.ModeGB {
			ps = append(ps, tkParam{label: T("tk_i_sweep"), show: hexStr(int(in.Sweep), 2), field: num("sweep", int(in.Sweep), 0, 255, 16, func(v int) { in.Sweep = byte(v) })})
		}
	case tracker.InstrWave:
		ps = append(ps, tkParam{label: T("tk_i_wave"), show: hexStr(int(in.Wave), 2), field: num("wave", int(in.Wave), 0, tracker.NumWaves-1, 16, func(v int) { in.Wave = int16(v) })})
		if s.Mode == tracker.ModeGB {
			ps = append(ps, tkParam{label: T("tk_i_wavevol"), show: [3]string{"100%", "50%", "25%"}[max(0, min(2, int(in.WaveVol)))],
				field: num("wv", int(in.WaveVol), 0, 2, 1, func(v int) { in.WaveVol = int8(v) })})
		}
	case tracker.InstrNoise:
		ps = append(ps, tkParam{label: T("tk_i_short"), show: onOff(in.Short), field: num("short", b2i(in.Short), 0, 1, 1, func(v int) { in.Short = v == 1 })})
	case tracker.InstrSynth:
		si := 0
		for i, sh := range synthShapes {
			if sh == in.Shape {
				si = i
			}
		}
		ps = append(ps,
			tkParam{label: T("tk_i_shape"), show: strings.ToUpper(in.Shape), field: num("shape", si, 0, len(synthShapes)-1, 1, func(v int) { in.Shape = synthShapes[v] })},
			tkParam{label: T("tk_i_duty"), show: duty[in.Duty&3], field: num("duty", int(in.Duty), 0, 3, 1, func(v int) { in.Duty = int8(v) })})
	}
	return ps
}

// b2i es 1 o 0.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// updateTkList atiende una vista de lista: ↑↓ eligen la fila y A la edita
// (o hace lo suyo). tap, si está, se llama al pulsar A en una fila que se
// edita (para oír el instrumento).
func (a *App) updateTkList(ps []tkParam, row *int, aHeld, bHeld bool, tap func()) {
	in := &a.input
	*row = max(0, min(len(ps)-1, *row))
	p := ps[*row]
	if p.do != nil {
		if in.Pressed(ActConfirm) && !bHeld {
			p.do()
			return
		}
	} else {
		f := p.field
		f.tap = tap
		if a.tkEdit(f, aHeld, bHeld) {
			return
		}
	}
	if aHeld {
		return
	}
	_, dy := trackerDir(in)
	*row = (*row + dy + len(ps)) % len(ps)
}

func (a *App) updateTkInstr(aHeld, bHeld bool) {
	t := a.tracker
	a.updateTkList(a.tkInstrParams(), &t.instrRow, aHeld, bHeld, func() { a.tkPreview(t.lastNote, t.instr) })
}

// tkProjectParams son las filas del proyecto: nombre, modo, tempo y groove.
func (a *App) tkProjectParams() []tkParam {
	s := a.tracker.song
	mode := T("tk_mode_gb")
	if s.Mode == tracker.ModeFree {
		mode = T("tk_mode_free")
	}
	return []tkParam{
		{label: T("tk_p_name"), show: s.Name, do: func() {
			a.openPrompt(T("tk_p_name"), s.Name, func(n string) {
				s.Name = n
				a.trackerChanged()
			})
		}},
		{label: T("tk_p_mode"), show: mode, field: tkField{key: "mode", val: b2i(s.Mode == tracker.ModeFree), emptyVal: math.MinInt, max: 1, big: 1,
			set: func(v int) {
				s.Mode = tracker.ModeGB
				if v == 1 {
					s.Mode = tracker.ModeFree
				}
			}}},
		{label: T("tk_tempo"), show: fmt.Sprintf("%d BPM", s.BPM), field: tkField{key: "bpm", val: s.BPM, emptyVal: math.MinInt,
			min: tracker.MinBPM, max: tracker.MaxBPM, big: 10, set: func(v int) { s.BPM = v }}},
		{label: T("tk_p_groove"), show: hexStr(s.Groove, 2), field: tkField{key: "groove", val: s.Groove, emptyVal: math.MinInt,
			max: tracker.NumGrooves - 1, big: 4, set: func(v int) {
				if len(s.Grooves[v]) == 0 {
					s.Grooves[v] = tracker.Groove{6, 6}
				}
				s.Groove = v
			}}},
	}
}

func (a *App) updateTkProject(aHeld, bHeld bool) {
	a.updateTkList(a.tkProjectParams(), &a.tracker.projRow, aHeld, bHeld, nil)
}

// La tabla: 16 filas de volumen, transposición y dos comandos.
func (a *App) updateTkTable(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	get := func() *tracker.Table {
		if s.Tables[t.table] == nil {
			s.Tables[t.table] = tracker.NewTable()
		}
		return s.Tables[t.table]
	}
	r := t.tbRow
	row := tracker.TableRow{Vol: tracker.Empty}
	if tb := s.Tables[t.table]; tb != nil {
		row = tb[r]
	}
	key := fmt.Sprintf("t%d.%d.%d", t.table, r, t.tbCol)
	var f tkField
	switch t.tbCol {
	case 0:
		f = tkField{key: key, val: int(row.Vol), emptyVal: tracker.Empty, canEmpty: true, max: 15, big: 4, def: int(t.lastVol), fresh: -1,
			set: func(v int) { get()[r].Vol = int8(v) }}
	case 1:
		f = tkField{key: key, val: int(row.Transpose), emptyVal: math.MinInt, min: -128, max: 127, big: 12,
			set: func(v int) { get()[r].Transpose = int8(v) }}
	default:
		i := (t.tbCol - 2) / 2
		code, arg := a.cmdFields(key, row.Cmds[i], func(c tracker.Cmd) { get()[r].Cmds[i] = c })
		f = code
		if (t.tbCol-2)%2 == 1 {
			f = arg
		}
	}
	if a.tkEdit(f, aHeld, bHeld) {
		return
	}
	dx, dy := trackerDir(in)
	t.tbRow = (t.tbRow + dy + tracker.Steps) % tracker.Steps
	t.tbCol = (t.tbCol + dx + 6) % 6
}

// El groove: los ticks de cada paso, en ciclo (hasta 16); la fila de
// después de la última añade una.
func (a *App) updateTkGroove(aHeld, bHeld bool) {
	t, in, s := a.tracker, &a.input, a.tracker.song
	g := s.Grooves[t.groove]
	r := min(t.grRow, len(g))
	t.grRow = r
	if r == len(g) {
		// El hueco del final: A añade un paso (como el anterior).
		if in.Pressed(ActConfirm) && !bHeld && len(g) < 16 {
			a.tkUndoPoint()
			v := 6
			if len(g) > 0 {
				v = g[len(g)-1]
			}
			s.Grooves[t.groove] = append(g, v)
			a.trackerChanged()
			return
		}
	} else {
		if in.Pressed(ActConfirm) && bHeld {
			if len(g) > 1 || t.groove != 0 {
				a.tkUndoPoint()
				s.Grooves[t.groove] = append(g[:r:r], g[r+1:]...)
				if len(s.Grooves[t.groove]) == 0 {
					delete(s.Grooves, t.groove)
				}
				a.trackerChanged()
			}
			return
		}
		f := tkField{key: fmt.Sprintf("g%d.%d", t.groove, r), val: g[r], emptyVal: math.MinInt, min: 1, max: 32, big: 4,
			set: func(v int) { s.Grooves[t.groove][r] = v }}
		if a.tkEdit(f, aHeld, bHeld) {
			return
		}
	}
	if aHeld {
		return
	}
	_, dy := trackerDir(in)
	t.grRow = max(0, min(len(g), t.grRow+dy))
}

// waveGet es la onda abierta (si no existe, una triangular que se crea al
// cambiarla).
func (a *App) waveGet(create bool) *tracker.Wave {
	t, s := a.tracker, a.tracker.song
	if w := s.Waves[t.wave]; w != nil {
		return w
	}
	w := tracker.PresetWaves()[tracker.WaveTriangle]
	if create {
		s.Waves[t.wave] = &w
	}
	return &w
}

// La onda: 32 muestras de 0 a F. ←→ eligen la muestra, A+↑↓ la cambian y
// A+←→ dibujan (la de al lado toma el valor de esta).
func (a *App) updateTkWave(aHeld, bHeld bool) {
	t, in := a.tracker, &a.input
	dx, dy := trackerDir(in)
	if aHeld && (dx != 0 || dy != 0) {
		a.tkUndoPoint()
		w := a.waveGet(true)
		if dy != 0 {
			w[t.waveX] = uint8(max(0, min(15, int(w[t.waveX])-dy)))
		} else {
			v := w[t.waveX]
			t.waveX = (t.waveX + dx + tracker.WaveLen) % tracker.WaveLen
			w[t.waveX] = v
		}
		a.trackerChanged()
		return
	}
	if aHeld {
		return
	}
	t.waveX = (t.waveX + dx + tracker.WaveLen) % tracker.WaveLen
}

// openTrackerMenu es el menú de Start.
func (a *App) openTrackerMenu() {
	t, s := a.tracker, a.tracker.song
	items := []menuItem{
		{label: T("tk_play_song"), icon: "play", do: func() {
			if t.playing {
				a.trackerToggle()
			}
			v := t.view
			t.view, t.songRow = tvSong, 0
			a.trackerToggle()
			t.view = v
		}},
	}
	if len(t.undo) > 0 {
		items = append(items, menuItem{label: T("tk_undo"), icon: "back", do: a.tkUndo})
	}
	// Copiar, pegar, clonar y vaciar lo de la vista.
	if copyable, paste, clone, clear := a.tkClipboard(); copyable != nil {
		items = append(items, menuItem{label: T("tk_copy"), icon: "list", do: copyable})
		if paste != nil {
			items = append(items, menuItem{label: T("tk_paste"), icon: "list", do: paste})
		}
		if clone != nil {
			items = append(items, menuItem{label: T("tk_clone"), icon: "list", do: clone})
		}
		items = append(items, menuItem{label: T("tk_clear_item"), icon: "power", do: clear})
	}
	if t.view == tvWave {
		for i, name := range tracker.WaveNames {
			items = append(items, menuItem{label: T("tk_preset") + " " + strings.ToUpper(name), icon: "eq", do: func() {
				a.tkUndoPoint()
				w := tracker.PresetWaves()[i]
				s.Waves[t.wave] = &w
				a.trackerChanged()
			}})
		}
	}
	items = append(items,
		menuItem{label: T("tk_songs"), icon: "list", do: a.openTrackerSongs},
		menuItem{label: T("tk_export"), icon: "download", do: a.exportTracker},
		menuItem{label: T("tk_delete"), icon: "power", do: a.deleteTrackerSong},
		menuItem{label: T("gm_leave"), icon: "pad", do: func() { a.saveTracker(); a.screen = Games }},
	)
	mode := T("tk_mode_gb")
	if s.Mode == tracker.ModeFree {
		mode = T("tk_mode_free")
	}
	a.menu = &menu{title: T("tk_title_menu"), subtitle: fmt.Sprintf("%s · %s · %d BPM", s.Name, mode, s.BPM), items: items}
}

// tkClipboard son las acciones de copiar, pegar, clonar (en el primer
// número libre, y la celda de donde se vino pasa a usar la copia) y vaciar
// lo que se ve. Nada en las vistas de proyecto y canción.
func (a *App) tkClipboard() (copyFn, paste, clone, clear func()) {
	t, s := a.tracker, a.tracker.song
	edit := func(f func()) func() {
		return func() {
			t.editing = false
			a.tkUndoPoint()
			f()
			a.trackerChanged()
		}
	}
	switch t.view {
	case tvChain:
		cur := func() tracker.Chain {
			if c := s.Chains[t.chain]; c != nil {
				return *c
			}
			return *tracker.NewChain()
		}
		copyFn = func() { c := cur(); t.clip = &c }
		if c, ok := t.clip.(*tracker.Chain); ok {
			paste = edit(func() { cp := *c; s.Chains[t.chain] = &cp })
		}
		if id := freeID(s.Chains, tracker.NumChains); id >= 0 {
			clone = edit(func() {
				c := cur()
				s.Chains[id] = &c
				if t.songRow < len(s.Rows) && int(s.Rows[t.songRow][t.ch]) == t.chain {
					s.Rows[t.songRow][t.ch] = int16(id)
				}
				t.chain = id
			})
		}
		clear = edit(func() { delete(s.Chains, t.chain) })
	case tvPhrase:
		cur := func() tracker.Phrase {
			if p := s.Phrases[t.phrase]; p != nil {
				return *p
			}
			return *tracker.NewPhrase()
		}
		copyFn = func() { p := cur(); t.clip = &p }
		if p, ok := t.clip.(*tracker.Phrase); ok {
			paste = edit(func() { cp := *p; s.Phrases[t.phrase] = &cp })
		}
		if id := freeID(s.Phrases, tracker.NumPhrases); id >= 0 {
			clone = edit(func() {
				p := cur()
				s.Phrases[id] = &p
				if c := s.Chains[t.chain]; c != nil && int(c[t.chainRow].Phrase) == t.phrase {
					c[t.chainRow].Phrase = int16(id)
				}
				t.phrase = id
			})
		}
		clear = edit(func() { delete(s.Phrases, t.phrase) })
	case tvInstr:
		cur := func() tracker.Instrument {
			if in := s.Instrs[t.instr]; in != nil {
				return *in
			}
			return *tracker.DefaultInstrument(tracker.InstrPulse)
		}
		copyFn = func() { in := cur(); t.clip = &in }
		if in, ok := t.clip.(*tracker.Instrument); ok {
			paste = edit(func() { cp := *in; s.Instrs[t.instr] = &cp })
		}
		if id := freeID(s.Instrs, tracker.NumInstr); id >= 0 {
			clone = edit(func() { in := cur(); s.Instrs[id] = &in; t.instr = id })
		}
		clear = edit(func() { delete(s.Instrs, t.instr) })
	case tvTable:
		cur := func() tracker.Table {
			if tb := s.Tables[t.table]; tb != nil {
				return *tb
			}
			return *tracker.NewTable()
		}
		copyFn = func() { tb := cur(); t.clip = &tb }
		if tb, ok := t.clip.(*tracker.Table); ok {
			paste = edit(func() { cp := *tb; s.Tables[t.table] = &cp })
		}
		if id := freeID(s.Tables, tracker.NumTables); id >= 0 {
			clone = edit(func() { tb := cur(); s.Tables[id] = &tb; t.table = id })
		}
		clear = edit(func() { delete(s.Tables, t.table) })
	case tvGroove:
		copyFn = func() { t.clip = append(tracker.Groove{}, s.Grooves[t.groove]...) }
		if g, ok := t.clip.(tracker.Groove); ok && len(g) > 0 {
			paste = edit(func() { s.Grooves[t.groove] = append(tracker.Groove{}, g...) })
		}
		clear = edit(func() {
			if t.groove == 0 {
				s.Grooves[0] = tracker.Groove{6, 6}
			} else {
				delete(s.Grooves, t.groove)
			}
		})
	case tvWave:
		copyFn = func() { w := *a.waveGet(false); t.clip = &w }
		if w, ok := t.clip.(*tracker.Wave); ok {
			paste = edit(func() { cp := *w; s.Waves[t.wave] = &cp })
		}
		if id := freeID(s.Waves, tracker.NumWaves); id >= 0 {
			clone = edit(func() { w := *a.waveGet(false); s.Waves[id] = &w; t.wave = id })
		}
		clear = edit(func() { s.Waves[t.wave] = &tracker.Wave{} })
	}
	return
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
	name := t.song.Name + " " + time.Now().Format("2006-01-02 1504") + ".wav"
	a.flash(T("tk_exporting"))
	go func() {
		samples := tracker.Render(song, sfx.Rate, 20*60)
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

// deleteTrackerSong borra la canción abierta (su archivo) y abre otra de las
// guardadas, o una nueva vacía si no queda ninguna. Un ejemplo vuelve a ser
// el original.
func (a *App) deleteTrackerSong() {
	t := a.tracker
	if t.playing {
		a.trackerToggle()
	}
	_ = os.Remove(filepath.Join(a.trackerDir(), t.file))
	t.dirty = false
	if saved := a.trackerSongs(); len(saved) > 0 {
		if s, err := tracker.Load(filepath.Join(a.trackerDir(), saved[0][0])); err == nil {
			a.openTrackerSong(s, saved[0][0], false)
			_ = os.WriteFile(filepath.Join(a.trackerDir(), "current"), []byte(saved[0][0]), 0o644)
			return
		}
	}
	a.openTrackerSong(tracker.NewSong(fmt.Sprintf(T("tk_new_name"), 1), tracker.ModeGB), fmt.Sprintf("song-%d.json", time.Now().Unix()), true)
}

// meterTap pasa el audio del tracker a la salida y, de paso, al medidor.
type meterTap struct {
	r   io.Reader
	an  *engine.Analyzer
	buf []float32
}

func (m *meterTap) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	frames := n / 4
	if cap(m.buf) < frames {
		m.buf = make([]float32, frames)
	}
	buf := m.buf[:frames]
	for i := range buf {
		buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(p[i*4:]))
	}
	m.an.Add(buf)
	return n, err
}
