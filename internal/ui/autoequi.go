package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/autoeq"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// AutoEQ en el ecualizador paramétrico (internal/autoeq): se busca el modelo
// de los audífonos en el índice de AutoEq y su perfil se pone en el
// paramétrico, que queda abierto para verlo o retocarlo. También se leen los
// ParametricEQ.txt que haya en la carpeta eq/ junto a audiofilus (los de
// AutoEq o los de Equalizer APO), para usarlo sin red.

// eqDir es la carpeta de los perfiles propios.
func (a *App) eqDir() string { return filepath.Join(a.paths.Base, "eq") }

// openAutoEQ es el menú de AutoEQ: buscar en línea y los archivos de eq/;
// Y sobre uno lo borra (con confirmación).
func (a *App) openAutoEQ() {
	var m *menu // este menú: B en el editor vuelve a él
	items := []menuItem{{label: T("aeq_search"), icon: "search", do: func() { a.autoEQSearch(m) }}}
	files, _ := filepath.Glob(filepath.Join(a.eqDir(), "*.txt"))
	for _, f := range files {
		name := eqProfileName(f)
		items = append(items, menuItem{label: name, icon: "headphones", close: true,
			del: func() { a.askEQDelete(name, m) }, do: func() {
				b, err := os.ReadFile(f)
				if err != nil {
					a.flash(err.Error())
					return
				}
				bands, pre, err := autoeq.ParseParametric(string(b))
				if err != nil {
					a.flash(err.Error())
					return
				}
				a.applyAutoEQ(name, bands, pre)
				a.editorBack = m
			}})
	}
	m = &menu{title: "AutoEQ", subtitle: T("aeq_sub"), items: items}
	a.menu = m
}

// autoEQSearch pide el modelo y lo busca en el índice (en segundo plano: la
// primera vez se baja, ~1 MB). from es el menú de AutoEQ, al que B vuelve
// desde los resultados.
func (a *App) autoEQSearch(from *menu) {
	a.openPrompt(T("aeq_model"), "", func(q string) {
		if strings.TrimSpace(q) == "" {
			return
		}
		a.flash(T("aeq_searching"))
		cache := filepath.Join(a.paths.Data, "cache", "autoeq-index.md")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			idx, err := autoeq.Index(ctx, cache)
			a.acctCh <- func() {
				if err != nil {
					a.flash(T("aeq_error", err))
					return
				}
				a.autoEQResults(q, autoeq.Search(idx, q, 40), from)
			}
		}()
	})
	a.prompt.back = from // B sin haber escrito: de vuelta a AutoEQ
}

// autoEQResults muestra lo encontrado; elegir uno lo baja y lo aplica.
func (a *App) autoEQResults(q string, res []autoeq.Profile, from *menu) {
	if len(res) == 0 {
		a.flash(T("aeq_none", q))
		return
	}
	var m *menu // B en el editor vuelve a estos resultados
	var items []menuItem
	for _, p := range res {
		items = append(items, menuItem{label: p.Name, icon: "headphones", note: p.Source, close: true, do: func() {
			a.flash(T("aeq_loading"))
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				bands, pre, err := autoeq.Fetch(ctx, p)
				a.acctCh <- func() {
					if err != nil {
						a.flash(T("aeq_error", err))
						return
					}
					a.saveEQProfile(p.Name, bands, pre) // para usarlo sin red (eqprofiles.go)
					a.applyAutoEQ(p.Name, bands, pre)
					a.editorBack = m
				}
			}()
		}})
	}
	m = &menu{title: "AutoEQ", subtitle: q, items: items, cols: 1, parent: from}
	a.menu = m
}

// applyAutoEQ pone el perfil en el paramétrico y lo abre.
func (a *App) applyAutoEQ(name string, bands []engine.PEQBand, preamp float64) {
	a.send("eqmode", "parametric")
	a.status.DSP.EQMode = "parametric"
	a.peqEdit = &peqEditor{bands: bands, preamp: preamp, row: 1}
	a.sendPEQ()
	a.cfg.UI.EQProfile = name
	a.saveConfig()
	a.flash(T("aeq_applied", name))
}
