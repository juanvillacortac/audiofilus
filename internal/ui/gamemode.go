package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
	"github.com/juanvillacortac/audiofilus/internal/proc"
)

// El modo juego (solo en Knulli): la otra cara de Audiofilus, un lanzador de
// juegos con los sistemas, sus juegos y las mismas opciones por sistema y
// por juego que EmulationStation (lo lee y lo guarda en los mismos archivos,
// ver internal/esdata), con la música sonando detrás. FN tres veces pasa del
// reproductor al modo juego y vuelta, y cada uno sigue donde estaba.
//
// En el modo juego la interfaz no pide que la consola siga despierta: la
// suspensión de Knulli (la tapa, el botón) funciona siempre, suene música o
// no.

// gmScreen es una pantalla del modo juego.
type gmScreen int

const (
	gmSystems gmScreen = iota
	gmGames
	gmOptions
	gmSaves     // elegir la partida guardada (gamemode_saves.go)
	gmSysMenu   // el menú Sistema: Wi-Fi, Bluetooth (gamemode_system.go)
	gmViewer    // el visor de capturas (gamemode_viewer.go)
	gmInfo      // la ficha de un juego (gamemode_info.go)
	gmZoom2     // una imagen de la ficha a pantalla completa (gamemode_zoom.go)
	gmVideoFull // el vídeo de la ficha a pantalla completa (gamemode_info.go)
	gmSearch    // buscar entre todos los juegos (gamemode_search.go)
)

// gmSystem es un sistema con sus juegos.
type gmSystem struct {
	sys   esdata.System
	games []esdata.Game
	plays int       // partidas de todos sus juegos
	last  time.Time // la última vez que se jugó a alguno
	sel   int       // el juego elegido
	// from y ref, en las colecciones y los grupos: de qué sistema es cada
	// juego y el juego en él (games son copias, para ordenarlas juntas).
	from []*gmSystem
	ref  []*esdata.Game
	// group: es un grupo de sistemas de ES (los ports sueltos en Ports, las
	// herramientas en Herramientas...), no una colección.
	group bool
}

// isCollection dice si s es una colección (Favoritos, Recientes...): sus
// juegos ya están en su sistema.
func (s *gmSystem) isCollection() bool { return s.from != nil && !s.group }

// Las colecciones: "sistemas" con juegos de todos los demás, al principio
// del carrusel. Favoritos (los marcados), Jugados recientemente y Más
// jugados (los que más partidas tienen), estos dos de hasta gmCollectionMax.
const (
	gmFavorites  = "favorites"
	gmRecent     = "recent"
	gmMostPlayed = "mostplayed"
)

// at es el juego i con su sistema de verdad (en una colección, el suyo).
func (s *gmSystem) at(i int) (*gmSystem, *esdata.Game) {
	if s.from != nil {
		return s.from[i], s.ref[i]
	}
	return s, &s.games[i]
}

// gmCollectionMax es cuántos juegos van en Recientes y Más jugados.
const gmCollectionMax = 30

// collection arma una colección con los juegos de systems que cumplen keep,
// ordenados con less (nil si no hay ninguno); hasta limit (0: todos).
func collection(systems []*gmSystem, name, title string, keep func(*esdata.Game) bool, less func(a, b *esdata.Game) bool, limit int) *gmSystem {
	type item struct {
		s  *gmSystem
		gm *esdata.Game
	}
	var items []item
	for _, s := range systems {
		if s.from != nil || gmIsViewer(s) {
			continue
		}
		for i := range s.games {
			if gm := &s.games[i]; keep(gm) {
				items = append(items, item{s, gm})
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return less(items[i].gm, items[j].gm) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	c := &gmSystem{sys: esdata.System{Name: name, FullName: title}}
	for _, it := range items {
		c.games = append(c.games, *it.gm)
		c.from = append(c.from, it.s)
		c.ref = append(c.ref, it.gm)
		c.plays += it.gm.PlayCount
		if it.gm.LastPlayed.After(c.last) {
			c.last = it.gm.LastPlayed
		}
	}
	return c
}

// collectionsOf son las colecciones de systems, en orden (las que tienen
// juegos).
func collectionsOf(systems []*gmSystem) []*gmSystem {
	byName := func(a, b *esdata.Game) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) }
	var out []*gmSystem
	for _, c := range []*gmSystem{
		collection(systems, gmFavorites, T("gx_favorites"), func(g *esdata.Game) bool { return g.Favorite }, byName, 0),
		collection(systems, gmRecent, T("gx_recent"), func(g *esdata.Game) bool { return !g.LastPlayed.IsZero() },
			func(a, b *esdata.Game) bool { return a.LastPlayed.After(b.LastPlayed) }, gmCollectionMax),
		collection(systems, gmMostPlayed, T("gx_mostplayed"), func(g *esdata.Game) bool { return g.PlayCount > 0 },
			func(a, b *esdata.Game) bool { return a.PlayCount > b.PlayCount }, gmCollectionMax),
	} {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// arrangeSystems es lo que va en el carrusel: las colecciones y, como en ES,
// los sistemas de base con los de un mismo grupo juntos (en el sitio del
// primero). faces son todos los sistemas por nombre: el de un grupo (ports,
// tools, snes) le da su nombre y su maqueta aunque no tenga juegos propios.
func arrangeSystems(base []*gmSystem, faces map[string]esdata.System, conf *esdata.Conf) []*gmSystem {
	members := map[string][]*gmSystem{}
	for _, s := range base {
		if s.sys.Grouped(conf) {
			members[s.sys.Group] = append(members[s.sys.Group], s)
		}
	}
	out := collectionsOf(base)
	done := map[string]bool{}
	for _, s := range base {
		name := s.sys.Group
		if !s.sys.Grouped(conf) || len(members[name]) < 2 {
			out = append(out, s) // solo, o el único del grupo con juegos
			continue
		}
		if done[name] {
			continue
		}
		done[name] = true
		out = append(out, groupOf(name, members[name], faces))
	}
	return out
}

// groupOf junta en un "sistema" los juegos de los sistemas ms del grupo
// name, por nombre.
func groupOf(name string, ms []*gmSystem, faces map[string]esdata.System) *gmSystem {
	face, ok := faces[name]
	if !ok {
		face = esdata.System{Name: name, FullName: ms[0].sys.FullName, Hardware: ms[0].sys.Hardware}
	}
	g := &gmSystem{sys: face, group: true}
	type item struct {
		s  *gmSystem
		gm *esdata.Game
	}
	var items []item
	for _, s := range ms {
		g.plays += s.plays
		if s.last.After(g.last) {
			g.last = s.last
		}
		for i := range s.games {
			items = append(items, item{s, &s.games[i]})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].gm.Name) < strings.ToLower(items[j].gm.Name)
	})
	for _, it := range items {
		g.games = append(g.games, *it.gm)
		g.from = append(g.from, it.s)
		g.ref = append(g.ref, it.gm)
	}
	return g
}

// refreshCollections vuelve a armar las colecciones y los grupos (al marcar
// o desmarcar un favorito: sus juegos son copias), sin mover lo elegido.
func (g *gameModeState) refreshCollections() {
	cur := g.gmSys()
	old := map[string]int{}
	for _, s := range g.systems {
		if s.from != nil {
			old[s.sys.Name] = s.sel
		}
	}
	g.systems = arrangeSystems(g.base, g.faces, g.conf)
	for _, s := range g.systems {
		if s.from != nil {
			s.sel = min(old[s.sys.Name], len(s.games)-1)
		}
	}
	g.sel = 0
	found := false
	for i, s := range g.systems {
		if s == cur || cur != nil && cur.from != nil && s.sys.Name == cur.sys.Name {
			g.sel, found = i, true
		}
	}
	if !found && cur != nil && cur.from != nil {
		g.screen = gmSystems // la colección quedó vacía: ya no está
	}
}

// gmOptMenu es un menú de opciones abierto: las de los ajustes globales, las
// de un sistema o las de un juego, o un submenú de ellas.
type gmOptMenu struct {
	title  string
	scope  esdata.Scope
	rows   []esdata.Option
	sel    int
	parent *gmOptMenu // el menú del que salió (un submenú); nil el primero
	subOf  int        // en el padre, la fila del submenú
}

// gmLoaded es lo que trae la carga en segundo plano.
type gmLoaded struct {
	systems []*gmSystem
	base    []*gmSystem
	faces   map[string]esdata.System
	feats   *esdata.Features
	conf    *esdata.Conf
	err     error
}

// gameModeState es el modo juego.
type gameModeState struct {
	on      bool
	taps    int     // toques seguidos de FN
	lastTap float64 // cuándo fue el último

	root    esdata.Root
	loading bool
	ch      chan gmLoaded
	err     string
	systems []*gmSystem // lo del carrusel (arrangeSystems)
	// base son los sistemas con juegos, sin agrupar ni colecciones; faces,
	// todos los de es_systems.cfg por nombre (ver arrangeSystems).
	base  []*gmSystem
	faces map[string]esdata.System
	feats *esdata.Features
	conf  *esdata.Conf

	screen   gmScreen
	sel      int // el sistema elegido
	opts     *gmOptMenu
	optsFrom gmScreen // a qué pantalla se vuelve al cerrar las opciones
	changeAt float64  // cuándo cambió la elección (la maqueta da una vuelta)

	covers map[string]*Album // las cajas de los juegos, como portadas
	// resume es dónde volver tras un juego (gmCheckResume); resumeChecked,
	// que ya se miró.
	resume        *gmResume
	resumeChecked bool
	cv            consoleView
	trans         gmTrans
	search        gmSearchState
	info          gmInfoState
	// zoom es cómo se ve la imagen a pantalla completa (el visor y la de la
	// ficha, zoomAl).
	zoom     gmZoom
	zoomAl   *Album
	backdrop gmBackdrop // el fondo del carrusel (gamemode_backdrop.go)
	// placeholders son las portadas hechas aquí para los juegos sin
	// carátula (gmPlaceholder).
	placeholders map[string]*ebiten.Image
	// aDown: A se pulsó en la lista de juegos (al soltarlo se lanza; si se
	// mantiene, se abre la ficha y aFired lo marca).
	aDown, aFired bool
	// themeAl es la carátula con que se tiñó el tema, y themeSys la consola
	// (ver gmFollowTheme).
	themeAl  *Album
	themeSys string
	sys      gmSysState
	saves    gmSavesState
	// stills son las maquetas de los lados del carrusel de sistemas, ya
	// dibujadas (quietas) a su tamaño mayor; stillCV, el lienzo en que se
	// dibujan (las maquetas armadas se comparten con cv).
	stills  map[*consoleModel]*stillModel
	stillCV consoleView
	box     consoleView // la caja del juego (pantalla de juegos)
	// spinYaw y spinPitch son el giro a mano de la maqueta o la caja con el
	// joystick derecho (radianes; vuelven a 0 al soltarlo), y spinAt, cuándo
	// se movió por última vez.
	spinYaw, spinPitch, spinAt float64
	// carPos es por dónde va el carrusel (un índice con decimales, que
	// persigue a sel); carT, cuándo se movió por última vez.
	carPos, carT float64
}

// gmTapGap es el tiempo máximo entre dos toques de FN del triple toque (el
// mismo que el atajo del servicio, config.Hotkeys).
const gmTapGap = 0.4

// gameRoot es la raíz de Knulli para el modo juego: la de
// AUDIOFILUS_KNULLI_ROOT (una copia de sus archivos, para probarlo en el
// PC) o "/" en Knulli. ok es false si no hay modo juego (otro sistema).
func (a *App) gameRoot() (esdata.Root, bool) {
	if dir := os.Getenv("AUDIOFILUS_KNULLI_ROOT"); dir != "" {
		return esdata.Root(dir), true
	}
	if a.status.Platform == "knulli" {
		return esdata.Root("/"), true
	}
	return "", false
}

// gameModeToggle cuenta los toques de FN y, al tercero seguido, pasa del
// reproductor al modo juego o al revés. Dice si cambió.
func (a *App) gameModeToggle() bool {
	g := &a.gm
	if !a.input.Pressed(ActGuide) {
		return false
	}
	if a.t-g.lastTap > gmTapGap {
		g.taps = 0
	}
	g.taps++
	g.lastTap = a.t
	if g.taps < 3 {
		return false
	}
	g.taps = 0
	root, ok := a.gameRoot()
	if !ok {
		return false
	}
	a.setGameMode(!g.on, root)
	return true
}

// setGameMode entra o sale del modo juego.
func (a *App) setGameMode(on bool, root esdata.Root) {
	g := &a.gm
	g.on = on
	a.menu, a.dock.open, a.guideAt = nil, false, 0
	if a.client != nil {
		a.client.HoldAwake(!on)
	}
	g.aDown = false
	a.gmVideoOff()
	if !on {
		a.gmSaveConf()
		a.gmLeaveTransition()
		if (g.themeAl != nil || g.themeSys != "") && a.playing != nil {
			a.themeFor = a.playing // de vuelta, el color del disco que suena
		}
		g.themeAl, g.themeSys = nil, ""
		return
	}
	g.trans.fromApp = true
	if g.systems == nil && !g.loading {
		g.root, g.loading = root, true
		g.ch = make(chan gmLoaded, 1)
		cache := a.gmCachePath()
		go func() { g.ch <- loadGameData(root, cache) }()
	}
}

// loadGameData lee los sistemas con juegos, sus juegos, las opciones y
// knulli.conf. Va en segundo plano: en la consola son decenas de gamelists.
func loadGameData(root esdata.Root, cachePath string) gmLoaded {
	t0 := time.Now()
	systems, err := root.LoadSystems()
	if err != nil {
		return gmLoaded{err: err}
	}
	feats, err := root.LoadFeatures()
	if err != nil {
		return gmLoaded{err: err}
	}
	conf, err := root.LoadConf()
	if err != nil {
		return gmLoaded{err: err}
	}
	// Los juegos, del caché donde nada cambió (ver esdata.GameCache).
	start := time.Now()
	setup := start.Sub(t0)
	cache := esdata.LoadGameCache(cachePath)
	cacheLoad := time.Since(start)
	var slow []string // los sistemas que tardaron (para el log)
	keep := map[string]bool{}
	hits := 0
	var out []*gmSystem
	hidden := root.HiddenSystems()
	faces := map[string]esdata.System{}
	for i := range systems {
		faces[systems[i].Name] = systems[i]
		if hidden[systems[i].Name] {
			continue // oculto en ES: tampoco aquí
		}
		s := &gmSystem{sys: systems[i]}
		keep[s.sys.Name] = true
		var hit bool
		ts := time.Now()
		s.games, hit = root.LoadGamesCached(&s.sys, cache)
		if hit {
			hits++
		}
		if d := time.Since(ts); d > 300*time.Millisecond {
			slow = append(slow, fmt.Sprintf("%s %v (cache %v)", s.sys.Name, d.Round(time.Millisecond), hit))
		}
		if len(s.games) == 0 {
			continue
		}
		for _, gm := range s.games {
			s.plays += gm.PlayCount
			if gm.LastPlayed.After(s.last) {
				s.last = gm.LastPlayed
			}
		}
		out = append(out, s)
	}
	cache.Prune(keep)
	var names []string
	for _, s := range out {
		names = append(names, s.sys.Name)
	}
	go preloadModels(names) // no hace esperar a la lista
	if cache.Dirty() {
		if err := cache.Save(cachePath); err != nil {
			log.Printf("game mode: game cache: %v", err)
		}
	}
	log.Printf("game mode: %d systems read in %v (%d from cache; systems, features and settings %v, cache file %v; slow: %s)",
		len(systems), time.Since(start).Round(time.Millisecond), hits, setup.Round(time.Millisecond), cacheLoad.Round(time.Millisecond), strings.Join(slow, ", "))
	return gmLoaded{systems: arrangeSystems(out, faces, conf), base: out, faces: faces, feats: feats, conf: conf}
}

// gmSys es el sistema elegido (nil si no hay).
func (g *gameModeState) gmSys() *gmSystem {
	if g.sel < 0 || g.sel >= len(g.systems) {
		return nil
	}
	return g.systems[g.sel]
}

// gmGame es el juego elegido del sistema elegido (nil si no hay).
func (g *gameModeState) gmGame() *esdata.Game {
	s := g.gmSys()
	if s == nil || s.sel < 0 || s.sel >= len(s.games) {
		return nil
	}
	return &s.games[s.sel]
}

// updateGameMode atiende el modo juego en cada fotograma.
func (a *App) updateGameMode() {
	g := &a.gm
	in := &a.input
	if g.loading {
		select {
		case r := <-g.ch:
			g.loading = false
			if r.err != nil {
				g.err = r.err.Error()
				log.Printf("game mode: %v", r.err)
				break
			}
			g.systems, g.base, g.faces, g.feats, g.conf = r.systems, r.base, r.faces, r.feats, r.conf
			esCatalogRoot, esCatalogLang = g.root, "" // se lee al primer texto
			g.changeAt = a.t
			if g.resume != nil {
				a.gmApplyResume()
			}
		default:
		}
		return
	}
	a.gmSysCollect()
	a.updateFreeSpin()
	if a.updateGuide() {
		return // con FN mantenido solo se mira la guía (Knulli usa FN+botón)
	}
	if a.prompt != nil {
		a.updatePrompt() // la clave del Wi-Fi
		return
	}
	switch {
	case in.Repeat(ActVolUp):
		a.changeVolume(volumeStep)
	case in.Repeat(ActVolDown):
		a.changeVolume(-volumeStep)
	}
	// La música, en las pantallas de sistemas y juegos: L1/R1 cambian de
	// canción, L2/R2 buscan e Y pausa (en las demás, los botones son suyos).
	if g.screen == gmSystems || g.screen == gmGames {
		a.overlayMusicKeys()
		if in.Pressed(ActPause) {
			a.send("toggle")
			return
		}
	}
	defer a.gmFollowTheme()
	switch g.screen {
	case gmSystems:
		a.updateGMSystems()
	case gmGames:
		a.updateGMGames()
	case gmOptions:
		a.updateGMOptions()
	case gmSearch:
		a.updateGMSearch()
	case gmSaves:
		a.updateGMSaves()
	case gmViewer:
		a.updateGMViewer()
	case gmSysMenu:
		a.updateGMSystem()
	case gmInfo:
		a.updateGMInfo()
	case gmZoom2:
		a.updateGMZoom()
	case gmVideoFull:
		a.updateGMVideoFull()
	}
}

// updateGMSystems: el carrusel de sistemas.
func (a *App) updateGMSystems() {
	g := &a.gm
	in := &a.input
	n := len(g.systems)
	if n == 0 {
		return
	}
	switch {
	case in.Repeat(ActLeft), in.Repeat(ActUp):
		g.sel = (g.sel + n - 1) % n
		g.changeAt = a.t
	case in.Repeat(ActRight), in.Repeat(ActDown):
		g.sel = (g.sel + 1) % n
		g.changeAt = a.t
	case in.Pressed(ActConfirm):
		g.screen = gmGames
	case in.Pressed(ActShuffle):
		a.gmOpenSearch()
	case in.Pressed(ActMenu):
		s := g.gmSys()
		a.gmOpenOptions(s.sys.FullName, esdata.Scope{System: &s.sys})
	case in.Pressed(ActSelect):
		a.gmOpenSystem()
	}
}

// updateGMGames: los juegos del sistema elegido.
func (a *App) updateGMGames() {
	g := &a.gm
	in := &a.input
	s := g.gmSys()
	n := len(s.games)
	if a.gmHoldA(s) {
		return
	}
	moved := true
	switch {
	case in.Repeat(ActUp):
		s.sel = (s.sel + n - 1) % n
	case in.Repeat(ActDown):
		s.sel = (s.sel + 1) % n
	case in.Repeat(ActLeft):
		s.sel = max(0, s.sel-10)
	case in.Repeat(ActRight):
		s.sel = min(n-1, s.sel+10)
	case in.Pressed(ActShuffle):
		s.sel = rand.IntN(n)
	default:
		moved = false
	}
	if moved {
		g.changeAt = a.t
		return
	}
	switch {
	case in.Pressed(ActBack):
		g.screen = gmSystems
	case in.Pressed(ActConfirm):
		g.aDown, g.aFired = true, false // se lanza al soltar (ver gmHoldA)
	case in.Pressed(ActSelect):
		a.gmOpenSearch()
	case in.Pressed(ActMenu):
		rs, gm := s.at(s.sel)
		a.gmOpenOptions(gm.Name, esdata.Scope{System: &rs.sys, Game: gm})
	}
}

// gmOpenOptions abre las opciones del ámbito sc.
func (a *App) gmOpenOptions(title string, sc esdata.Scope) {
	g := &a.gm
	if g.feats == nil || g.conf == nil {
		return
	}
	g.optsFrom = g.screen
	rows := g.root.Options(g.feats, g.conf, sc)
	if sc.Game != nil {
		// Primero, si es favorito (en su gamelist.xml, como en ES).
		fav := esdata.Option{Kind: esdata.OptFavorite, Label: strings.ToUpper(T("gx_favorite")), // como las de ES
			Values: []esdata.Choice{{Name: esdata.ValOff, Value: "0"}, {Name: esdata.ValOn, Value: "1"}}}
		rows = append([]esdata.Option{fav}, rows...)
	}
	g.opts = &gmOptMenu{title: title, scope: sc, rows: rows}
	g.screen = gmOptions
}

// updateGMOptions: un menú de opciones. Izquierda y derecha cambian el
// valor; A abre un submenú (o pasa al valor siguiente); B vuelve y, al
// cerrar el primero, guarda en knulli.conf.
func (a *App) updateGMOptions() {
	g := &a.gm
	in := &a.input
	m := g.opts
	n := len(m.rows)
	switch {
	case in.Pressed(ActBack), in.Pressed(ActMenu):
		if m.parent != nil {
			g.opts = m.parent
			return
		}
		a.gmSaveConf()
		g.opts, g.screen = nil, g.optsFrom
		return
	}
	if n == 0 {
		return
	}
	switch {
	case in.Repeat(ActUp):
		m.sel = (m.sel + n - 1) % n
	case in.Repeat(ActDown):
		m.sel = (m.sel + 1) % n
	case in.Repeat(ActLeft):
		a.gmStep(m, -1)
	case in.Repeat(ActRight):
		a.gmStep(m, 1)
	case in.Pressed(ActConfirm):
		r := &m.rows[m.sel]
		if r.Kind == esdata.OptSubmenu {
			g.opts = &gmOptMenu{title: r.Label, scope: m.scope, rows: r.Sub, parent: m, subOf: m.sel}
			return
		}
		a.gmStep(m, 1)
	}
}

// gmStep cambia el valor de la fila elegida d pasos (dando la vuelta). Si es
// el emulador, las demás filas cambian con él.
func (a *App) gmStep(m *gmOptMenu, d int) {
	g := &a.gm
	r := &m.rows[m.sel]
	if r.Kind == esdata.OptFavorite {
		gm := m.scope.Game
		if err := g.root.SetFavorite(m.scope.System, gm, !gm.Favorite); err != nil {
			log.Printf("game mode: favorite: %v", err)
			a.flash(err.Error())
			return
		}
		g.refreshCollections()
		return
	}
	if (r.Kind != esdata.OptChoice && r.Kind != esdata.OptEmulator) || len(r.Values) == 0 {
		return
	}
	n := len(r.Values)
	r.Set(g.conf, (r.Current(g.conf)+d+n)%n)
	if r.Kind == esdata.OptEmulator {
		sel := m.sel
		rows := g.root.Options(g.feats, g.conf, m.scope)
		if len(m.rows) > 0 && m.rows[0].Kind == esdata.OptFavorite {
			rows = append([]esdata.Option{m.rows[0]}, rows...)
		}
		m.rows = rows
		m.sel = min(sel, len(m.rows)-1)
	}
}

// gmSaveConf guarda knulli.conf si cambió algo.
func (a *App) gmSaveConf() {
	g := &a.gm
	if g.conf == nil || !g.conf.Dirty() {
		return
	}
	if err := g.conf.Save(); err != nil {
		log.Printf("game mode: saving settings: %v", err)
		a.flash(err.Error())
	}
}

// gmLaunch lanza el juego gm del sistema s. La interfaz no puede tener la
// pantalla mientras juega: deja la orden en esdata.RequestPath, apunta dónde
// estaba y se cierra con la música sonando; el lanzador del port (que pone
// AUDIOFILUS_LAUNCHER) lanza el juego con `audiofilus launch` y al terminar
// vuelve a abrir la interfaz, que sigue en el modo juego donde estaba.
func (a *App) gmLaunch(s *gmSystem, gm *esdata.Game, extra []string) {
	g := &a.gm
	emu, core := esdata.CurrentEmulator(g.conf, esdata.Scope{System: &s.sys, Game: gm})
	args := esdata.LaunchArgs(&s.sys, gm, emu, core, []string{esdata.ControllersToken})
	args = append(args, extra...) // la partida guardada, si se eligió una
	log.Printf("game mode: launch %q", args)
	if !g.root.Live() {
		a.flash(T("gx_launch_pc", strings.Join(args[2:min(len(args), 6)], " ")))
		return
	}
	if os.Getenv("AUDIOFILUS_LAUNCHER") == "" {
		a.flash(T("gx_launch_soon")) // un lanzador del port de antes
		return
	}
	a.gmSaveConf()
	g.info.video.stop()
	g.info.paused = false // el juego tiene la pantalla; la música sigue como estaba
	q := esdata.Request{Args: args, System: s.sys.Name, Rom: gm.Path, Name: gm.Name, Dir: s.sys.Path}
	if err := q.Save(); err != nil {
		a.flash(err.Error())
		return
	}
	a.gmSaveResume()
	a.exit(true)
}

// gmResume es dónde estaba el modo juego al lanzar un juego, para volver ahí
// al abrir de nuevo la interfaz (data/gamemode.json).
type gmResume struct {
	System string   `json:"system"`
	Game   string   `json:"game"`
	Screen gmScreen `json:"screen"`
}

// gmResumePath es el archivo de gmResume.
func (a *App) gmResumePath() string { return filepath.Join(a.paths.Data, "gamemode.json") }

// gmCachePath es el caché de los juegos (esdata.GameCache).
func (a *App) gmCachePath() string { return filepath.Join(a.paths.Data, "gamemode-cache.gob") }

// gmSaveResume apunta dónde está el modo juego.
func (a *App) gmSaveResume() {
	b, _ := json.Marshal(a.gmResumeHere())
	if err := os.WriteFile(a.gmResumePath(), b, 0o644); err != nil {
		log.Printf("game mode: %v", err)
	}
}

// gmResumeHere es dónde está el modo juego, para volver ahí.
func (a *App) gmResumeHere() gmResume {
	g := &a.gm
	r := gmResume{Screen: g.screen}
	switch g.screen {
	case gmOptions:
		r.Screen = g.optsFrom
	case gmSaves, gmSearch, gmViewer, gmInfo, gmZoom2, gmVideoFull: // al volver, al juego en su lista
		r.Screen = gmGames
	case gmSysMenu:
		r.Screen = gmSystems
	}
	if r.Screen == gmSysMenu {
		r.Screen = gmSystems // las opciones globales, abiertas desde Sistema
	}
	if s := g.gmSys(); s != nil {
		r.System = s.sys.Name
		if gm := g.gmGame(); gm != nil {
			r.Game = gm.Path
		}
	}
	return r
}

// gmRescan vuelve a leer los juegos de todos los sistemas sin el caché
// (como "Actualizar las listas de juegos" de ES): para lo que el caché no
// ve, como un gamelist.xml cambiado sin cambiar de tamaño en el mismo
// segundo, o ROMs nuevas en una subcarpeta. Al terminar, vuelve a donde
// estaba.
func (a *App) gmRescan() {
	g := &a.gm
	if g.loading {
		return
	}
	if err := os.Remove(a.gmCachePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("game mode: %v", err)
	}
	log.Printf("game mode: rescanning the games")
	r := a.gmResumeHere()
	if r.Screen == gmSysMenu {
		r.Screen = gmSystems
	}
	g.resume, g.screen, g.opts = &r, gmSystems, nil
	g.loading, g.err = true, ""
	g.ch = make(chan gmLoaded, 1)
	root, cache := g.root, a.gmCachePath()
	go func() { g.ch <- loadGameData(root, cache) }()
}

// gmRestartApp reinicia Audiofilus: el servicio (que sigue sonando, como
// con "restart") y la interfaz, que el lanzador del port vuelve a abrir
// (con la marca de RestartPath, como al cambiar el HDMI) en el modo juego,
// donde estaba.
func (a *App) gmRestartApp() {
	a.gmSaveConf()
	log.Printf("game mode: restarting Audiofilus")
	a.send("restart")
	time.Sleep(300 * time.Millisecond) // que el servicio reciba el pedido antes de irnos
	if os.Getenv("AUDIOFILUS_LAUNCHER") == "" {
		// Sin el lanzador del port nadie la vuelve a abrir: se ejecuta otra vez.
		a.gmSaveResume()
		if err := proc.Reexec(); err != nil {
			os.Remove(a.gmResumePath())
			a.flash(err.Error())
		}
		return
	}
	if err := os.WriteFile(RestartPath, nil, 0o644); err != nil {
		a.flash(err.Error())
		return
	}
	a.gmSaveResume()
	a.exit(true) // la música sigue
}

// gmCheckResume, una vez al arrancar (en cuanto se sabe en qué sistema
// corre), vuelve al modo juego si se cerró para lanzar un juego.
func (a *App) gmCheckResume() {
	g := &a.gm
	if g.resumeChecked || a.offline || (a.status.Platform == "" && os.Getenv("AUDIOFILUS_KNULLI_ROOT") == "") {
		return
	}
	g.resumeChecked = true
	b, err := os.ReadFile(a.gmResumePath())
	if err != nil {
		return
	}
	os.Remove(a.gmResumePath())
	var r gmResume
	if json.Unmarshal(b, &r) != nil {
		return
	}
	root, ok := a.gameRoot()
	if !ok {
		return
	}
	g.resume = &r
	a.setGameMode(true, root)
}

// gmApplyResume deja el modo juego donde dice g.resume, ya con los datos.
func (a *App) gmApplyResume() {
	g := &a.gm
	r := g.resume
	g.resume = nil
	for i, s := range g.systems {
		if s.sys.Name != r.System {
			continue
		}
		g.sel = i
		for k := range s.games {
			if s.games[k].Path == r.Game {
				s.sel = k
			}
		}
		g.screen = r.Screen
		if g.screen != gmSystems {
			g.screen = gmGames
		}
	}
}

// gmCover es la caja del juego como una portada (para el cargador de
// portadas), o nil si no tiene.
func (a *App) gmCover(gm *esdata.Game) *Album {
	g := &a.gm
	p := gm.Thumbnail
	if p == "" {
		p = gm.Image
	}
	if p == "" {
		p = gmPicture(gm) // las capturas: la imagen es el propio archivo
	}
	if p == "" {
		return nil
	}
	if g.covers == nil {
		g.covers = map[string]*Album{}
	}
	if al, ok := g.covers[p]; ok {
		return al
	}
	local := g.root.Path(p)
	var al *Album
	if _, err := os.Stat(local); err == nil {
		al = &Album{Title: gm.Name, coverPath: local, logo: true, clear: true, stray: true, passing: true}
	}
	g.covers[p] = al
	return al
}

// gmFollowTheme tiñe la interfaz con la carátula del juego elegido, como el
// reproductor con la del disco que suena, y en el carrusel con los colores de
// la consola (solo con el color "del disco" en Ajustes → Color). Donde no hay
// de qué, vuelve el del disco.
func (a *App) gmFollowTheme() {
	g := &a.gm
	if a.themeColor() != "album" {
		return
	}
	want := a.playing
	if s := g.gmSys(); s != nil && len(s.games) > 0 && (g.screen == gmGames || g.screen == gmSaves || g.screen == gmViewer ||
		g.screen == gmOptions && g.optsFrom == gmGames) {
		_, gm := s.at(s.sel)
		if al := a.gmCover(gm); al != nil {
			want = al
		}
	}
	// En el carrusel, el color de la consola elegida (de su maqueta).
	if s := g.gmSys(); s != nil && g.screen == gmSystems {
		if m := g.cv.model(s.sys.Name, s.sys.Hardware); m.hasAccent {
			if g.themeSys != s.sys.Name {
				g.themeSys, g.themeAl = s.sys.Name, nil
				a.theme, a.themeFor = ThemeFor(m.accent[0], m.accent[1]), nil
			}
			return
		}
	}
	g.themeSys = ""
	if want == nil || want == g.themeAl {
		return
	}
	g.themeAl = want
	a.themeFor = want
}

// gmHoldA lleva A en la lista de juegos: soltarlo lanza el juego; mantenerlo
// medio segundo abre su ficha. Dice si atendió algo.
func (a *App) gmHoldA(s *gmSystem) bool {
	g := &a.gm
	if !g.aDown {
		return false
	}
	d := a.input.duration(ActConfirm)
	switch {
	case d >= gmHoldFrames && !g.aFired:
		g.aFired = true
		a.gmOpenInfo()
	case d == 0:
		g.aDown = false
		if !g.aFired {
			a.gmPlay(s.at(s.sel))
		}
	}
	return true
}
