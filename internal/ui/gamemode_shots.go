package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// Las capturas del modo juego: con -shots y AUDIOFILUS_KNULLI_ROOT (una copia
// de los archivos de una consola), en vez de las del reproductor se guardan
// las del modo juego con esos sistemas y juegos.

// gmShotFrames cuenta los fotogramas de las capturas desde que hay datos.
var gmShotFrames int

// gmShot es una captura: su nombre y cómo dejar el modo juego para ella.
type gmShot struct {
	name string
	set  func()
}

// gmShotPlan son las capturas del modo juego.
func (a *App) gmShotPlan() []gmShot {
	g := &a.gm
	find := func(name string) int {
		for i, s := range g.systems {
			if s.sys.Name == name {
				return i
			}
		}
		return -1
	}
	var plan []gmShot
	for _, name := range []string{"gba", "gb", "psp", "nds", "snes", "n64", "megadrive", "psx", "dreamcast", "mame", "ports", "virtualboy", "pico8", "pokemini", "gbc", "nes", "pcenginecd", "neogeo", "3ds", gmFavorites, gmRecent, gmMostPlayed} {
		i := find(name)
		if i < 0 {
			continue
		}
		plan = append(plan, gmShot{"sistemas-" + name, func() { g.screen, g.sel, g.opts = gmSystems, i, nil; g.carPos = float64(i) }})
	}
	for _, name := range []string{"gba", "snes", "psx", "ports", "mame", gmFavorites} {
		i := find(name)
		if i < 0 {
			continue
		}
		plan = append(plan, gmShot{"juegos-" + name, func() {
			g.screen, g.sel, g.opts = gmGames, i, nil
			s := g.systems[i]
			s.sel = len(s.games) / 3
			for k := s.sel; k < len(s.games); k++ { // uno con caja
				if a.gmCover(&s.games[k]) != nil {
					s.sel = k
					break
				}
			}
		}})
	}
	if i := find("gba"); i >= 0 {
		plan = append(plan, gmShot{"opciones-sistema", func() {
			g.screen, g.sel = gmSystems, i
			s := g.systems[i]
			a.gmOpenOptions(s.sys.FullName, esdata.Scope{System: &s.sys})
			g.opts.sel = 2
		}})
		plan = append(plan, gmShot{"opciones-juego", func() {
			g.screen, g.sel = gmGames, i
			s := g.systems[i]
			gm := &s.games[s.sel]
			a.gmOpenOptions(gm.Name, esdata.Scope{System: &s.sys, Game: gm})
			g.opts.sel = 0
		}})
	}
	for _, typing := range []bool{true, false} {
		name := "buscar"
		if typing {
			name = "buscar-escribiendo"
		}
		plan = append(plan, gmShot{name, func() {
			g.opts = nil
			g.screen = gmSearch
			g.search.query, g.search.typing, g.search.row, g.search.col = []rune("MARIO"), typing, 2, 4
			g.search.hits, g.search.lastQuery = g.gmRunSearch("MARIO"), "MARIO"
			g.search.sel = min(3, len(g.search.hits)-1)
		}})
	}
	if i := find("gb"); i >= 0 {
		s := g.systems[i]
		for k := range s.games {
			if st := g.root.SaveStates(&s.sys, &s.games[k]); len(st) > 1 {
				gi, states := k, st
				for _, sel := range []int{0, 1} {
					plan = append(plan, gmShot{fmt.Sprintf("partidas-%d", sel), func() {
						g.opts, g.sel, s.sel = nil, i, gi
						g.saves = gmSavesState{sys: s, game: &s.games[gi], states: states, sel: sel}
						g.screen = gmSaves
					}})
				}
				break
			}
		}
	}
	if i := find(gmViewerSystem); i >= 0 {
		plan = append(plan, gmShot{"capturas", func() { g.opts, g.sel, g.screen = nil, i, gmGames; g.systems[i].sel = 2 }})
		plan = append(plan, gmShot{"visor", func() { g.opts, g.sel, g.screen = nil, i, gmViewer; g.systems[i].sel = 2; g.zoom = gmZoom{} }})
		plan = append(plan, gmShot{"visor-zoom", func() {
			g.opts, g.sel, g.screen = nil, i, gmViewer
			g.systems[i].sel = 2
			g.zoom = gmZoom{level: 2, cx: 0.3, cy: 0.6}
		}})
	}
	plan = append(plan, gmShot{"sistema", func() {
		g.opts, g.screen = nil, gmSysMenu
		g.sys = gmSysState{view: sysMain, wifiOn: true, btOn: true, ssid: "Mi red", ip: "192.168.0.108"}
	}})
	plan = append(plan, gmShot{"bluetooth", func() {
		g.opts, g.screen = nil, gmSysMenu
		g.sys = gmSysState{view: sysBT, sel: 1, btOn: true, busy: "", devs: []btDevice{{"41:42:D3:60:5D:50", "Wireless Controller", true}, {"7B:8B:CE:4E:30:30", "Audífonos", false}}}
	}})
	if i := find("gba"); i >= 0 {
		s := g.systems[i]
		for k := range s.games {
			gm := &s.games[k]
			if gm.Video == "" || !fileOK(g.root.Path(gm.Video)) || gm.Image == "" {
				continue
			}
			for _, tab := range []int{0, 1, 2} {
				gi, tab := k, tab
				plan = append(plan, gmShot{fmt.Sprintf("ficha-%d", tab), func() {
					g.opts, g.sel, s.sel = nil, i, gi
					a.gmOpenInfo()
					g.info.tab = min(tab, len(g.info.media)-1)
					if g.info.media[g.info.tab] == mediaVideo {
						// Hasta que llegue un cuadro (o tres segundos).
						g.info.video.play(g.root.Path(s.games[gi].Video), gmVideoW, gmVideoH, gmVideoFPS)
						for w := 0; w < 300 && !g.info.video.shown && !g.info.video.failed; w++ {
							time.Sleep(10 * time.Millisecond)
							g.info.video.update()
						}
					}
				}})
			}
			break
		}
	}
	if i := find("ports"); i >= 0 {
		s := g.systems[i]
		for k := range s.games {
			if a.gmCover(&s.games[k]) == nil {
				gi := k
				if j := find("gba"); j >= 0 {
					plan = append(plan, gmShot{"sin-caratula", func() {
						// Un juego sin carátula, como si fuera de la GBA (caja 3D).
						g.opts, g.sel, g.screen = nil, j, gmGames
						g.systems[j].games = append(g.systems[j].games, s.games[gi])
						g.systems[j].sel = len(g.systems[j].games) - 1
					}})
				}
				break
			}
		}
	}
	plan = append(plan, gmShot{"opciones-globales", func() {
		g.screen = gmSystems
		a.gmOpenOptions(T("gx_global"), esdata.Scope{})
		g.opts.sel = 3
	}})
	return plan
}

// updateGMShots lleva las capturas del modo juego: carga los datos, pide las
// cajas de todas las capturas, espera a que lleguen y las guarda.
func (a *App) updateGMShots() error {
	g := &a.gm
	if g.systems == nil {
		root, _ := a.gameRoot()
		r := loadGameData(root, filepath.Join(a.ShotsDir, "gamemode-cache.gob"))
		if r.err != nil {
			a.shotsErr = r.err
			return ebiten.Termination
		}
		g.root, g.on = root, true
		g.systems, g.feats, g.conf = r.systems, r.feats, r.conf
		esCatalogRoot, esCatalogLang = root, ""
		g.changeAt = -10
		return nil
	}
	plan := a.gmShotPlan()
	a.w, a.h = 360, ScreenH
	gmShotFrames++
	if gmShotFrames < 4 || len(a.covers.pending) > 0 {
		tmp := ebiten.NewImage(a.w, a.h)
		for _, s := range plan {
			s.set()
			a.drawGameMode(tmp)
		}
		if a.FramesDir != "" {
			a.warmGMFrames(tmp) // y lo que sale en las animaciones (gamemode_frames.go)
		}
		tmp.Deallocate()
		if gmShotFrames > 1200 {
			a.shotsErr = fmt.Errorf("covers did not load in time")
			return ebiten.Termination
		}
		return nil
	}
	if err := os.MkdirAll(a.ShotsDir, 0o755); err != nil {
		a.shotsErr = err
		return ebiten.Termination
	}
	a.battery, a.charging = 0.72, false
	for i, s := range plan {
		s.set()
		a.gmFollowTheme()           // el color de la consola o del juego, como en la consola
		a.t = 3.2 + float64(i)*0.37 // cada maqueta, desde otro ángulo
		path := filepath.Join(a.ShotsDir, "juego-"+s.name+".png")
		if err := a.render(path, a.w, func(dst *ebiten.Image) {
			drawBackground(dst, a.theme, a.t)
			a.drawGameMode(dst)
		}); err != nil {
			a.shotsErr = err
			return ebiten.Termination
		}
	}
	if a.FramesDir != "" {
		if err := a.writeGMFrames(); err != nil {
			a.shotsErr = err
		}
	}
	// La configuración de la copia no se toca (las capturas cambian opciones
	// solo en memoria).
	return ebiten.Termination
}
