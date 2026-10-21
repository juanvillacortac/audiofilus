package ui

import (
	"fmt"
	"image/color"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/milkdrop"
)

// MilkDrop: el visualizador de presets de MilkDrop 1 (internal/milkdrop), uno
// más de la pantalla de visualizadores. No pasa por la paleta (los presets
// traen sus colores) y va a la resolución de la pantalla. Los presets son los
// de serie y los del usuario en <carpeta de audiofilus>/milkdrop (y sus
// subcarpetas), que se buscan la primera vez que se abre.
//
// L1/R1 cambian de preset (con dos segundos de mezcla), A fija el que se ve
// (deja de cambiar solo) y START tiene anterior, siguiente, al azar y fijar.
// Solo, cambia cada milkInterval segundos, en un golpe. En START un botón
// cambia los colores: los del preset (lo normal) o los del disco que suena
// (el tema: la luminancia del preset pasa a un degradado fondo → acento →
// acento 2 → texto), que se guarda en audiofilus.json.

// milkInterval son los segundos de cada preset, y milkBlend los de la mezcla.
const (
	milkInterval = 20.0
	milkBlend    = 2.0
)

// milkState es el visualizador abierto.
type milkState struct {
	lib    *milkdrop.Library
	r      *milkdrop.Renderer
	cyc    milkdrop.Cycler
	idx    int
	nameAt float64 // cuándo se cambió (para enseñar el nombre)
	bands  [16]float64
	lastT  float64
	err    error
	// Los colores del disco: cuánto se usan (va hacia 0 o 1 poco a poco) y
	// el degradado, que sigue al tema despacio (el tema cambia de golpe).
	albumMix float64
	pal      [4][3]float64
	palSet   bool
}

// milk es el visualizador, que se crea la primera vez (con el primer preset).
func (a *App) milk() *milkState {
	v := &a.vis
	if v.milk == nil {
		m := &milkState{
			lib: milkdrop.NewLibrary(a.milkDir()),
			r:   milkdrop.NewRenderer(milkdrop.Options{Width: 320, Height: 240}),
			cyc: milkdrop.Cycler{Interval: milkInterval, OnBeat: true},
		}
		v.milk = m
		a.milkGo(0, 0)
	}
	return v.milk
}

// milkGo pone el preset i (dando la vuelta), mezclando blend segundos. Si no
// se puede leer, prueba con el siguiente.
func (a *App) milkGo(i int, blend float64) {
	m := a.vis.milk
	list := m.lib.Entries()
	if len(list) == 0 {
		return
	}
	for try := 0; try < len(list); try++ {
		k := ((i+try)%len(list) + len(list)) % len(list)
		p, err := milkdrop.Load(list[k])
		if err != nil {
			log.Printf("milkdrop: %v", err)
			continue
		}
		if w := m.r.SetPreset(p, blend); len(w) > 0 {
			log.Printf("milkdrop: %s: %v", list[k].Name, w)
		}
		m.idx, m.nameAt = k, a.t
		m.cyc.Reset()
		return
	}
}

// milkStep pasa d presets (−1 el anterior, 1 el siguiente).
func (a *App) milkStep(d int) {
	m := a.milk()
	a.milkGo(m.idx+d, milkBlend)
}

// milkRandom pone uno al azar (que no sea el mismo).
func (a *App) milkRandom() {
	m := a.milk()
	n := len(m.lib.Entries())
	if n < 2 {
		return
	}
	a.milkGo(m.idx+1+rand.IntN(n-1), milkBlend)
}

// milkLock fija el preset o lo suelta.
func (a *App) milkLock() {
	m := a.milk()
	m.cyc.Locked = !m.cyc.Locked
	m.nameAt = a.t
}

// updateMilk atiende los botones propios de MilkDrop (el cambio solo va en
// stepVisual). Devuelve true si usó el botón.
func (a *App) updateMilk() bool {
	in := &a.input
	switch {
	case in.Pressed(ActPrev):
		a.milkStep(-1)
	case in.Pressed(ActNext):
		a.milkStep(1)
	case in.Pressed(ActConfirm):
		a.milkLock()
	default:
		return false
	}
	return true
}

// milkMenuItems son las opciones de MilkDrop en el menú de START.
func (a *App) milkMenuItems() []menuItem {
	lock := T("milk_lock")
	if a.milk().cyc.Locked {
		lock = T("milk_unlock")
	}
	// Los colores: un solo botón que cambia entre los del preset y los del
	// disco (debajo dice cuáles van).
	colors := menuItem{label: T("milk_colors"), icon: "palette",
		value: func() string {
			if a.cfg.UI.MilkDropColors == "album" {
				return T("milk_from_album")
			}
			return T("milk_from_preset")
		},
		do: func() {
			if a.cfg.UI.MilkDropColors == "album" {
				a.cfg.UI.MilkDropColors = ""
			} else {
				a.cfg.UI.MilkDropColors = "album"
			}
			a.saveConfig()
		}}
	return []menuItem{
		colors,
		{label: T("milk_prev"), icon: "prev", close: true, do: func() { a.milkStep(-1) }},
		{label: T("milk_next"), icon: "next", close: true, do: func() { a.milkStep(1) }},
		{label: T("milk_random"), icon: "shuffle", close: true, do: a.milkRandom},
		{label: lock, icon: "star", close: true, do: a.milkLock},
		{label: T("milk_rescan"), icon: "folder", close: true, do: func() {
			m := a.milk()
			m.lib.Rescan()
			a.flash(T("milk_count", len(m.lib.Entries())))
		}},
	}
}

// drawMilk pinta MilkDrop entre la barra de arriba y la de abajo.
func (a *App) drawMilk(dst *ebiten.Image, top, bottom int) {
	v, m := &a.vis, a.milk()
	w := dst.Bounds().Dx()
	lw, lh := w, bottom-top
	if lw < 8 || lh < 8 {
		return
	}
	iw, ih := lw/milkScale, lh/milkScale
	// A 30 fps, como los demás (entre medias, el último).
	v.frame++
	_, rh := m.r.Size()
	img := v.milkOut
	if img == nil || v.frame%2 == 0 || a.offline || rh != ih {
		m.r.Resize(iw, ih)
		dt := a.t - m.lastT
		if dt <= 0 || dt > 0.25 {
			dt = 1.0 / 30
		}
		m.lastT = a.t
		a.milkPalette(dt)
		_, _, m.bands = a.visBands()
		m.r.Update(milkdrop.Input{Dt: dt, Bands: m.bands[:], Progress: m.cyc.Progress()})
		var err error
		if img, err = m.r.Draw(); err != nil {
			if m.err == nil {
				log.Printf("milkdrop: %v", err)
			}
			m.err = err
		}
		v.milkOut = img
	}
	if img != nil {
		op := &ebiten.DrawImageOptions{}
		sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
		op.GeoM.Scale(float64(lw)/float64(sw), float64(lh)/float64(sh))
		op.GeoM.Translate(0, float64(top))
		dst.DrawImage(img, op)
	}
	if v.bg {
		return // de fondo en el reproductor: sin nombre ni ayuda
	}
	th := a.theme
	if m.err != nil {
		drawText(dst, m.err.Error(), SmallFace, 8, top+8, th.Text)
	}
	// El nombre del visualizador y del preset, un momento al cambiar.
	since := a.t - max(m.nameAt, v.switchAt)
	if p := since / 2.5; p < 1 {
		name := T("vis_milk")
		if list := m.lib.Entries(); m.idx < len(list) {
			name += " · " + list[m.idx].Name
		}
		if m.cyc.Locked {
			name += " · " + T("milk_locked")
		}
		k := uint8(255 * steps(1-p))
		tw := textWidth(name, SmallFace)
		panel(dst, (w-tw)/2-6, top+8, tw+12, smallCap+10, withAlpha(th.Bg2, k))
		drawText(dst, name, SmallFace, (w-tw)/2, top+13, withAlpha(th.Text, k))
	}
	lock := T("milk_lock")
	if m.cyc.Locked {
		lock = T("milk_unlock")
	}
	// En la barra de abajo van en mayúsculas, como las de las demás pantallas
	// (milk_lock es también una opción del menú, en minúsculas).
	a.drawHints(dst, []hint{{"<>", T("h_visual")}, {"L1/R1", hintUpper(T("milk_h_preset"))}, {"A", hintUpper(lock)}, {"START", T("h_more")}})
}

// milkFrames guarda fotogramas de tres presets (para la web y para ver que
// hay estela y warp), con sus colores y con los del disco:
// dir/<preset>/f0000.png... y dir/<preset>-album/...
func (a *App) milkFrames(dir string) error {
	m := a.milk()
	saved := a.cfg.UI.MilkDropColors
	defer func() { a.cfg.UI.MilkDropColors = saved }()
	for k, name := range []string{"ember-tunnel", "liquid-chrome", "kaleido-bloom"} {
		for _, colors := range []string{"", "album"} {
			for i, e := range m.lib.Entries() {
				if e.Builtin && filepath.Base(e.Path) == name+".milk" {
					a.milkGo(i, 0)
				}
			}
			m.r.Clear()
			m.nameAt, m.lastT = -100, 0 // sin el nombre encima
			a.cfg.UI.MilkDropColors = colors
			sub := filepath.Join(dir, name)
			if colors != "" {
				sub += "-" + colors
			}
			if err := os.MkdirAll(sub, 0o755); err != nil {
				return err
			}
			for i := 0; i < 100; i++ {
				a.t = 40 + float64(k)*10 + float64(i)/20
				for j := 0; j < 3; j++ {
					a.beatHit = a.updateBeat()
					a.updatePump()
				}
				if err := a.renderShot(filepath.Join(sub, fmt.Sprintf("f%04d.png", i)), 320); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// milkScale es cuánto se reduce la resolución interna respecto a la pantalla
// (AUDIOFILUS_MILKDROP_SCALE=2: a la mitad, para consolas con poca GPU).
var milkScale = func() int {
	if n, err := strconv.Atoi(os.Getenv("AUDIOFILUS_MILKDROP_SCALE")); err == nil && n >= 1 && n <= 4 {
		return n
	}
	return 1
}()

// milkPalette lleva los colores del disco hacia lo elegido y el degradado
// hacia el tema de ahora, y se los pasa al visualizador.
func (a *App) milkPalette(dt float64) {
	m := a.vis.milk
	// De fondo en el reproductor, siempre los del disco: así va con la
	// portada y el resto de la pantalla.
	want := 0.0
	if a.cfg.UI.MilkDropColors == "album" || a.vis.bg {
		want = 1
	}
	th := a.theme
	stops := [4]color.RGBA{th.Bg, th.Accent, th.Accent2, th.Text}
	k := 1 - math.Exp(-dt/0.8)
	if !m.palSet || a.offline {
		k, m.palSet = 1, true
		if a.offline {
			m.albumMix = want
		}
	}
	m.albumMix += (want - m.albumMix) * math.Min(1, dt/0.5)
	if math.Abs(want-m.albumMix) < 0.01 {
		m.albumMix = want
	}
	var out [4][3]float32
	for i, c := range stops {
		for j, v := range [3]uint8{c.R, c.G, c.B} {
			m.pal[i][j] += (float64(v)/255 - m.pal[i][j]) * k
			out[i][j] = float32(m.pal[i][j])
		}
	}
	m.r.SetPalette(out, m.albumMix)
}

// milkDir es la carpeta de los presets del usuario: la elegida en Ajustes
// o milkdrop/ junto a audiofilus.
func (a *App) milkDir() string {
	if a.cfg.UI.MilkDropDir != "" {
		return a.cfg.UI.MilkDropDir
	}
	return filepath.Join(a.paths.Base, "milkdrop")
}

// hintUpper pasa un texto a mayúsculas para la barra de ayuda (en turco, con
// su İ con punto).
func hintUpper(s string) string {
	if lang == "tr" {
		return strings.ToUpperSpecial(unicode.TurkishCase, s)
	}
	return strings.ToUpper(s)
}
