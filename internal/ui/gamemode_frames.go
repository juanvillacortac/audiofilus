package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// Las animaciones del modo juego: con -shots, -frames y
// AUDIOFILUS_KNULLI_ROOT, además de las capturas se guardan secuencias de
// cuadros (a 30 por segundo, con el tiempo simulado) para hacer GIF: el
// carrusel de sistemas, el giro a mano de una maqueta, la lista de juegos y
// la ficha de un juego con su video.

// gmFPS son los cuadros por segundo de las secuencias.
const gmFPS = 30

// gmSeq es una secuencia: su carpeta, cuántos cuadros y cómo dejar el modo
// juego en el cuadro i (con a.t ya puesto).
type gmSeq struct {
	dir    string
	frames int
	set    func(i int)
	// live: el cuadro depende de algo que corre en tiempo real (el video):
	// se espera un cuadro de verdad entre uno y otro.
	live bool
}

// gmFrameSeqs son las secuencias del modo juego.
func (a *App) gmFrameSeqs() []gmSeq {
	g := &a.gm
	find := func(name string) int {
		for i, s := range g.systems {
			if s.sys.Name == name {
				return i
			}
		}
		return -1
	}
	var seqs []gmSeq
	n := len(g.systems)
	if n == 0 {
		return nil
	}
	// El carrusel: quieto un momento y luego un sistema a la derecha cada
	// 0,8 s, diez veces.
	start := max(0, find("gb"))
	const step = 24
	seqs = append(seqs, gmSeq{dir: "juego-carrusel", frames: 12 + 10*step + 24, set: func(i int) {
		g.screen, g.opts = gmSystems, nil
		k := min(10, max(0, (i-12)/step+1))
		if i < 12 {
			k = 0
		}
		sel := (start + k) % n
		if sel != g.sel {
			g.sel, g.changeAt = sel, a.t
		}
		if i == 0 {
			g.carPos, g.carT = float64(sel), a.t
		}
	}})
	// El giro a mano (joystick derecho): la maqueta de la PlayStation da una
	// vuelta y se inclina, y al soltar vuelve sola.
	if psx := find("psx"); psx >= 0 {
		seqs = append(seqs, gmSeq{dir: "juego-giro", frames: 150, set: func(i int) {
			g.screen, g.opts, g.sel = gmSystems, nil, psx
			if i == 0 {
				g.carPos, g.carT, g.changeAt = float64(psx), a.t, a.t
			}
			if i < 100 {
				p := float64(i) / 100
				g.spinYaw = 2 * math.Pi * p
				g.spinPitch = gmSpinPitchMax * 0.6 * math.Sin(math.Pi*p)
				g.spinAt = a.t
			}
		}})
	}
	// La lista de juegos de la GBA: baja de uno en uno, con la caja de cada
	// uno girando.
	if gba := find("gba"); gba >= 0 {
		s := g.systems[gba]
		first := len(s.games) / 3
		for k := first; k < len(s.games); k++ { // desde uno con caja
			if a.gmCover(&s.games[k]) != nil {
				first = k
				break
			}
		}
		const gstep = 15
		seqs = append(seqs, gmSeq{dir: "juego-juegos", frames: 12 + 10*gstep + 30, set: func(i int) {
			g.screen, g.opts, g.sel = gmGames, nil, gba
			k := 0
			if i >= 12 {
				k = min(10, (i-12)/gstep+1)
			}
			sel := min(len(s.games)-1, first+k)
			if sel != s.sel || i == 0 {
				s.sel, g.changeAt = sel, a.t
			}
		}})
		// La ficha de un juego con video.
		for k := range s.games {
			gm := &s.games[k]
			if gm.Video == "" || !fileOK(g.root.Path(gm.Video)) {
				continue
			}
			gi := k
			seqs = append(seqs, gmSeq{dir: "juego-ficha", frames: 6 * gmFPS, live: true, set: func(i int) {
				if i == 0 {
					g.opts, g.sel, s.sel = nil, gba, gi
					a.gmOpenInfo()
					for t, m := range g.info.media {
						if m == mediaVideo {
							g.info.tab = t
						}
					}
					g.info.video.play(g.root.Path(gm.Video), gmVideoW, gmVideoH, gmVideoFPS)
				}
				g.info.video.update()
			}})
			break
		}
	}
	return seqs
}

// warmGMFrames recorre las secuencias (con su tiempo, dibujando uno de cada
// tres cuadros sin guardarlos) para que las carátulas y los fondos que van
// a salir se pidan: los fondos esperan a que el sistema lleve un rato
// elegido.
func (a *App) warmGMFrames(tmp *ebiten.Image) {
	t := a.t
	for _, sq := range a.gmFrameSeqs() {
		if sq.live {
			continue
		}
		a.t = 10
		for i := 0; i < sq.frames; i++ {
			a.t += 1.0 / gmFPS
			sq.set(i)
			if i%3 == 0 {
				a.drawGameMode(tmp)
			}
		}
	}
	a.t = t
}

// writeGMFrames guarda las secuencias en a.FramesDir. Antes de guardar cada
// cuadro se dibuja una vez y se espera a las imágenes que pidió (la caja
// nueva, el fondo del juego), como si hubiera pasado el tiempo de verdad.
func (a *App) writeGMFrames() error {
	g := &a.gm
	tmp := ebiten.NewImage(a.w, a.h)
	defer tmp.Deallocate()
	for _, sq := range a.gmFrameSeqs() {
		g.backdrop = gmBackdrop{} // sin el fondo de la secuencia anterior
		dir := filepath.Join(a.FramesDir, sq.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		a.t = 10
		g.spinYaw, g.spinPitch, g.spinAt = 0, 0, 0
		for i := 0; i < sq.frames; i++ {
			a.t += 1.0 / gmFPS
			sq.set(i)
			if sq.live {
				time.Sleep(time.Second / gmFPS)
				g.info.video.update()
			}
			a.gmFollowTheme()
			a.drawGameMode(tmp)
			for w := 0; w < 200; w++ {
				a.covers.collect(a.t - 10) // ya cargadas: sin fundido de entrada
				if len(a.covers.pending) == 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			path := filepath.Join(dir, fmt.Sprintf("f%04d.png", i))
			if err := a.render(path, a.w, func(dst *ebiten.Image) {
				drawBackground(dst, a.theme, a.t)
				a.drawGameMode(dst)
			}); err != nil {
				return err
			}
		}
		if sq.live {
			g.info.video.stop()
		}
	}
	g.spinYaw, g.spinPitch = 0, 0
	return nil
}
