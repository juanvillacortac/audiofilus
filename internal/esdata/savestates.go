package esdata

import (
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Las partidas guardadas (savestates) de RetroArch, como las muestra ES: en
// /userdata/saves/<sistema>/ cada una es <rom sin extensión>.state<N> (la
// ranura 0 sin número; la automática, .state.auto), con su captura al lado
// en <archivo>.png. Para empezar desde una, emulatorlauncher recibe
// -state_slot y -state_filename (configgen la pasa a RetroArch con -e, o
// como carga automática si es la .auto).

// SaveState es una partida guardada.
type SaveState struct {
	Slot  int    // la ranura (0..), o AutoSlot
	Path  string // el archivo, en la ruta de la consola
	Image string // la captura (ruta en esta máquina), o "" si no hay
	Time  time.Time
}

// AutoSlot es la ranura de la partida automática (.state.auto).
const AutoSlot = -1

// savesDir es donde RetroArch guarda las partidas.
const savesDir = "/userdata/saves"

// SupportsSaveStates dice si el emulador carga partidas guardadas (los de
// RetroArch, como en ES).
func SupportsSaveStates(emulator string) bool {
	return emulator == "libretro" || strings.HasPrefix(emulator, "lr-")
}

// SaveStates son las partidas guardadas del juego g, de la más reciente a
// la más vieja.
func (r Root) SaveStates(s *System, g *Game) []SaveState {
	dir := path.Join(savesDir, s.Name)
	base := strings.TrimSuffix(path.Base(g.Path), path.Ext(g.Path)) + ".state"
	entries, err := os.ReadDir(r.Path(dir))
	if err != nil {
		return nil
	}
	var out []SaveState
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, base) || strings.HasSuffix(name, ".png") {
			continue
		}
		slot, ok := parseSlot(name[len(base):])
		if !ok {
			continue
		}
		st := SaveState{Slot: slot, Path: path.Join(dir, name)}
		if info, err := e.Info(); err == nil {
			st.Time = info.ModTime()
		}
		if img := r.Path(st.Path + ".png"); fileExists(img) {
			st.Image = img
		}
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// parseSlot lee lo que va tras ".state": nada (ranura 0), un número o
// ".auto".
func parseSlot(s string) (int, bool) {
	switch s {
	case "":
		return 0, true
	case ".auto":
		return AutoSlot, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// StateArgs son los argumentos de emulatorlauncher para empezar desde st.
func StateArgs(st SaveState) []string {
	return []string{"-state_slot", strconv.Itoa(st.Slot), "-state_filename", st.Path}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
