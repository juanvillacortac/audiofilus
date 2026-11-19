package ui

import (
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
)

// Continuar una partida: al abrir un juego de RetroArch que tiene partidas
// guardadas, antes de lanzarlo se elige desde dónde (una nueva o una de las
// guardadas, con su captura), como en ES.

// gmSavesState es la elección de partida.
type gmSavesState struct {
	sys    *gmSystem
	game   *esdata.Game
	states []esdata.SaveState
	sel    int // 0 es una partida nueva; i > 0, states[i-1]
}

// gmPlay abre el juego: si su emulador carga partidas guardadas y tiene
// alguna, primero se elige cuál; si no, se lanza.
func (a *App) gmPlay(s *gmSystem, gm *esdata.Game) {
	g := &a.gm
	if gmIsViewer(s) {
		g.screen = gmViewer // las capturas se ven aquí
		return
	}
	if esdata.IsOwnLauncher(g.root.Path(gm.Path)) {
		a.setGameMode(false, g.root) // Audiofilus ya está abierto: al reproductor
		return
	}
	emu, _ := esdata.CurrentEmulator(g.conf, esdata.Scope{System: &s.sys, Game: gm})
	if esdata.SupportsSaveStates(emu) {
		if states := g.root.SaveStates(&s.sys, gm); len(states) > 0 {
			g.saves = gmSavesState{sys: s, game: gm, states: states}
			g.screen, g.changeAt = gmSaves, a.t
			return
		}
	}
	a.gmLaunch(s, gm, nil)
}

// updateGMSaves atiende la elección de partida.
func (a *App) updateGMSaves() {
	g := &a.gm
	sv, in := &g.saves, &a.input
	n := len(sv.states) + 1
	switch {
	case in.Repeat(ActUp):
		sv.sel = (sv.sel + n - 1) % n
		g.changeAt = a.t
	case in.Repeat(ActDown):
		sv.sel = (sv.sel + 1) % n
		g.changeAt = a.t
	case in.Pressed(ActBack):
		g.screen = gmGames
	case in.Pressed(ActConfirm):
		var extra []string
		if sv.sel > 0 {
			extra = esdata.StateArgs(sv.states[sv.sel-1])
		}
		a.gmLaunch(sv.sys, sv.game, extra)
	}
}

// gmStateImage es la captura de una partida, como portada (pixelada como las de
// la música).
func (a *App) gmStateImage(st esdata.SaveState) *Album {
	g := &a.gm
	if st.Image == "" {
		return nil
	}
	if g.covers == nil {
		g.covers = map[string]*Album{}
	}
	if al, ok := g.covers[st.Image]; ok {
		return al
	}
	var al *Album
	if _, err := os.Stat(st.Image); err == nil {
		al = &Album{Title: st.Path, coverPath: st.Image, logo: true, clear: true, stray: true, passing: true}
	}
	g.covers[st.Image] = al
	return al
}

// slotName es cómo se llama una partida en la lista.
func slotName(st esdata.SaveState) string {
	if st.Slot == esdata.AutoSlot {
		return T("gx_state_auto")
	}
	return fmt.Sprintf(T("gx_state_slot"), st.Slot)
}

// drawGMSaves: a la izquierda, la captura de la partida elegida (o la caja
// del juego, para una nueva); a la derecha, las partidas con su fecha.
func (a *App) drawGMSaves(dst *ebiten.Image) {
	g := &a.gm
	sv, th := &g.saves, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	cx, cy, cw, ch := gmConsoleBox(w, h)
	if sv.sel == 0 {
		cover, r, alpha := a.gmBoxArt(sv.game, &sv.sys.sys)
		a.drawGameBox(dst, &g.box, sv.sys.sys.Name, cover, r, alpha, cx, cy, cw, ch, a.gmSpin())
	} else {
		size := min(cw, ch)
		a.drawGMFlat(dst, a.gmStateImage(sv.states[sv.sel-1]), sv.game.Name, sv.sys.sys.FullName, cx+(cw-size)/2, cy+(ch-size)/2, size)
	}
	drawTextW(dst, sv.game.Name, SmallFace, margin, cy+ch+4, cw, th.Text)
	drawTextW(dst, N("gx_n_states", len(sv.states)), SmallFace, margin, cy+ch+14, cw, th.Accent)

	lx := margin + cw + 10
	lw := w - lx - margin - 4
	rows := []gmRow{{main: T("gx_state_new")}}
	for _, st := range sv.states {
		rows = append(rows, gmRow{main: slotName(st), sub: st.Time.Format("02/01/2006 15:04")})
	}
	a.drawGMList(dst, rows, sv.sel, lx, topH+8, lw, h-topH-bottomH-gmMiniH-12)
	a.drawGMNowPlaying(dst)
	a.drawTopBar(dst, T("gx_continue"))
	a.drawHints(dst, []hint{{"^v", T("h_choose")}, {"A", T("gx_play")}, {"B", T("h_back")}})
}
