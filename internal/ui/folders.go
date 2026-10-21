package ui

import "github.com/juanvillacortac/audiofilus/internal/milkdrop"

// Las carpetas que se eligen en Ajustes con el explorador: la de la música
// (lo de siempre), la de los presets de MilkDrop y la de las listas de radio.

// pickTarget es para qué se abrió el explorador de carpetas.
type pickTarget int

const (
	pickMusic pickTarget = iota
	pickMilkDrop
	pickRadio
)

// pickedDir guarda la carpeta elegida para MilkDrop o la radio y vuelve a
// Ajustes. MilkDrop, si ya estaba abierto, busca los presets de la nueva; la
// radio lee sus listas al abrirse.
func (a *App) pickedDir(dir string) {
	switch a.pickFor {
	case pickMilkDrop:
		a.cfg.UI.MilkDropDir = dir
		if m := a.vis.milk; m != nil {
			m.lib = milkdrop.NewLibrary(dir)
			a.milkGo(0, milkBlend)
			a.flash(T("milk_count", len(m.lib.Entries())))
		}
	case pickRadio:
		a.cfg.UI.RadioDir = dir
	}
	a.saveConfig()
	a.pickFor = pickMusic
	a.mode = modeMain
}
