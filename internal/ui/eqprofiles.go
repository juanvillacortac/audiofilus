package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/autoeq"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Los perfiles de EQ de los audífonos: los ParametricEQ.txt de la carpeta eq/
// (los que pone el usuario y los que se bajan de AutoEQ, que se guardan ahí
// solos). En el menú de Start, "Perfil de EQ" pasa de uno a otro sin red,
// para quien tiene varios audífonos o uno con perfiles distintos (con y sin
// cancelación de ruido). El elegido queda en audiofilus.json (ui.eq_profile).

// eqProfileSuffix es como termina el nombre de los archivos de AutoEq.
const eqProfileSuffix = " ParametricEQ.txt"

// eqProfiles son los nombres de los perfiles de eq/, en orden.
func (a *App) eqProfiles() []string {
	files, _ := filepath.Glob(filepath.Join(a.eqDir(), "*.txt"))
	var names []string
	for _, f := range files {
		names = append(names, eqProfileName(f))
	}
	slices.Sort(names)
	return names
}

// eqProfileName es el nombre de un perfil por su archivo.
func eqProfileName(file string) string {
	base := filepath.Base(file)
	if n, ok := strings.CutSuffix(base, eqProfileSuffix); ok {
		return n
	}
	return strings.TrimSuffix(base, ".txt")
}

// eqProfileFile es el archivo de un perfil (el que existe, o el que tendría).
func (a *App) eqProfileFile(name string) string {
	for _, f := range []string{name + eqProfileSuffix, name + ".txt"} {
		if p := filepath.Join(a.eqDir(), f); fileExists(p) {
			return p
		}
	}
	return filepath.Join(a.eqDir(), safeFileName(name)+eqProfileSuffix)
}

// saveEQProfile guarda un perfil bajado en eq/ (si no estaba).
func (a *App) saveEQProfile(name string, bands []engine.PEQBand, preamp float64) {
	f := a.eqProfileFile(name)
	if fileExists(f) {
		return
	}
	_ = os.MkdirAll(a.eqDir(), 0o755)
	_ = os.WriteFile(f, []byte(autoeq.FormatParametric(bands, preamp)), 0o644)
}

// useEQProfile pone el perfil name en el paramétrico (sin abrirlo); "" lo
// deja plano. Devuelve el error de leerlo.
func (a *App) useEQProfile(name string) error {
	var bands []engine.PEQBand
	pre := 0.0
	if name != "" {
		b, err := os.ReadFile(a.eqProfileFile(name))
		if err != nil {
			return err
		}
		if bands, pre, err = autoeq.ParseParametric(string(b)); err != nil {
			return err
		}
	}
	a.send("eqmode", "parametric")
	a.status.DSP.EQMode = "parametric"
	if b, err := json.Marshal(bands); err == nil {
		a.send("peq", string(b))
	}
	a.send("peqpreamp", strconv.FormatFloat(pre, 'f', 1, 64))
	a.status.DSP.PEQ, a.status.DSP.PEQPreamp = bands, pre
	a.cfg.UI.EQProfile = name
	a.saveConfig()
	return nil
}

// eqProfileItem es "Perfil de EQ" en el menú de Start: A pasa al siguiente
// (sin perfil, el primero, el segundo...). Sin perfiles guardados no sale.
func (a *App) eqProfileItem() (menuItem, bool) {
	if len(a.eqProfiles()) == 0 {
		return menuItem{}, false
	}
	return menuItem{label: T("eqp_title"), icon: "headphones",
		active: func() bool { return a.cfg.UI.EQProfile != "" },
		value: func() string {
			if a.cfg.UI.EQProfile == "" {
				return T("off")
			}
			return a.cfg.UI.EQProfile
		},
		do: func() {
			names := append([]string{""}, a.eqProfiles()...)
			next := names[(slices.Index(names, a.cfg.UI.EQProfile)+1)%len(names)]
			if err := a.useEQProfile(next); err != nil {
				a.flash(T("aeq_error", err))
			}
		}}, true
}

// safeFileName quita de un nombre lo que no puede ir en un archivo (en FAT,
// exFAT o Windows).
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// askEQDelete pregunta si borrar el perfil name (Y sobre él en AutoEQ):
// Conservar (o B) vuelve a back; borrarlo vuelve a AutoEQ, ya sin él. Si era
// el puesto, el ecualizador queda plano.
func (a *App) askEQDelete(name string, back *menu) {
	a.menu = &menu{title: T("eqp_delete_q", name), parent: back, items: []menuItem{
		{label: T("pl_keep"), icon: "arrow_l", do: func() { a.menu = back }},
		{label: T("eqp_delete_yes"), icon: "trash", danger: true, do: func() {
			if err := os.Remove(a.eqProfileFile(name)); err != nil && !os.IsNotExist(err) {
				a.flash(T("aeq_error", err))
				return
			}
			if a.cfg.UI.EQProfile == name {
				_ = a.useEQProfile("")
			}
			a.flash(T("eqp_deleted", name))
			// De vuelta en AutoEQ, ya sin él (linkMenu lo pone en el
			// sitio del de antes).
			a.openAutoEQ()
		}},
	}}
}
