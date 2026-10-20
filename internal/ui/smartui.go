package ui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/smart"
)

// Las listas inteligentes (internal/smart): salen en Listas detrás de las
// automáticas, y sus canciones se calculan al abrirlas (no al listarlas: en
// una portátil, con muchas canciones, cuesta). Están en data/smart.json; la
// primera vez son las de serie. Una nueva se arma con menús: campo,
// condición, valor (y más reglas), orden y límite.

// smartNames son las claves de lang.go de las listas de serie (por su
// nombre en internal/smart), para que salgan en el idioma de la interfaz.
var smartNames = map[string]string{
	"Most played (30 days)": "sm_most",
	"Recently added":        "sm_recent",
	"Never played":          "sm_never",
	"Liked":                 "sm_liked",
	"Top rated":             "sm_rated",
	"Forgotten favorites":   "sm_forgotten",
	"Random mix (today)":    "sm_random",
}

// smartName es el nombre que se ve de una lista inteligente.
func smartName(d smart.Def) string {
	if k, ok := smartNames[d.Name]; ok {
		return T(k)
	}
	return d.Name
}

// smartDefs son las listas inteligentes guardadas (o las de serie).
func (a *App) smartDefs() []smart.Def {
	defs, err := smart.Load(a.paths.Data)
	if err != nil {
		log.Printf("smart playlists: %v", err)
	}
	return defs
}

// smartPlaylists son las entradas de Listas de las listas inteligentes (sin
// canciones aún: se calculan al abrirlas).
func (a *App) smartPlaylists() []*Playlist {
	if a.local == nil {
		return nil
	}
	var out []*Playlist
	for i, d := range a.smartDefs() {
		out = append(out, &Playlist{Name: smartName(d), Remote: true, icon: "bolt", smart: i + 1})
	}
	return out
}

// smartFill calcula las canciones de una lista inteligente.
func (a *App) smartFill(pl *Playlist) {
	if pl.smart == 0 || a.local == nil {
		return
	}
	defs := a.smartDefs()
	if pl.smart > len(defs) {
		return
	}
	lib := a.local
	tracks := make([]smart.Track, 0, len(lib.Songs))
	for _, s := range lib.Songs {
		m := lib.meta[s.Path]
		t := smart.Track{Path: s.Path, Title: s.Title, Artist: s.Artist, Album: s.Album, AlbumArtist: m.albumArtist,
			Genre: m.genre, Year: m.year, Track: s.Track % 1000}
		if m.added > 0 {
			t.Added = time.Unix(m.added, 0)
		}
		tracks = append(tracks, t)
	}
	paths := smart.Evaluate(defs[pl.smart-1], tracks, a.listens(), a.marksStore(), time.Now())
	pl.Songs = pl.Songs[:0]
	for _, p := range paths {
		if s, ok := lib.Song(p); ok {
			pl.Songs = append(pl.Songs, s)
		}
	}
}

// smartItems son las opciones de una lista inteligente en su menú: editar
// sus reglas, cambiarle el nombre y borrarla.
func (a *App) smartItems(pl *Playlist) []menuItem {
	i := pl.smart - 1
	return []menuItem{
		{label: T("sm_edit"), icon: "pencil", do: func() {
			defs := a.smartDefs()
			if i < len(defs) {
				a.smartWizard(defs[i], i)
			}
		}},
		{label: T("pl_rename"), icon: "pencil", do: func() {
			defs := a.smartDefs()
			if i >= len(defs) {
				return
			}
			a.openPrompt(T("pl_rename"), smartName(defs[i]), func(n string) {
				defs[i].Name = n
				a.smartSave(defs)
			})
		}},
		{label: T("pl_delete"), icon: "trash", danger: true, do: func() {
			defs := a.smartDefs()
			if i < len(defs) {
				a.smartSave(append(defs[:i:i], defs[i+1:]...))
			}
		}},
	}
}

// smartSave guarda las listas y rehace la vista de Listas.
func (a *App) smartSave(defs []smart.Def) {
	if err := smart.Save(a.paths.Data, defs); err != nil {
		a.flash(err.Error())
		return
	}
	a.refreshPlaylists()
}

// smartNew empieza una lista inteligente nueva.
func (a *App) smartNew() {
	a.smartWizard(smart.Def{Name: T("sm_new_name"), Match: "all", Live: true}, -1)
}

// smartWizard es el editor de una lista: un menú con sus reglas (elegir una
// la quita), añadir una regla, coincidir con todas o alguna, el orden, el
// límite y guardar. idx es su lugar en las guardadas (-1: nueva).
func (a *App) smartWizard(d smart.Def, idx int) {
	items := []menuItem{}
	for i, r := range d.Rules {
		items = append(items, menuItem{label: smartRuleText(r), icon: "trash", do: func() {
			d.Rules = append(d.Rules[:i:i], d.Rules[i+1:]...)
			a.smartWizard(d, idx)
		}})
	}
	items = append(items, menuItem{label: T("sm_add_rule"), icon: "plus", do: func() { a.smartPickField(d, idx) }})
	match := T("sm_match_all")
	if d.Match == "any" {
		match = T("sm_match_any")
	}
	items = append(items,
		menuItem{label: match, icon: "list", do: func() {
			if d.Match == "any" {
				d.Match = "all"
			} else {
				d.Match = "any"
			}
			a.smartWizard(d, idx)
		}},
		menuItem{label: T("sm_sort", smartSortText(d.Sort)), icon: "list", do: func() { a.smartPickSort(d, idx) }},
		menuItem{label: T("sm_limit", smartLimitText(d.Limit)), icon: "list", do: func() {
			a.openPrompt(T("sm_limit_q"), strconv.Itoa(d.Limit), func(s string) {
				n, _ := strconv.Atoi(strings.TrimSpace(s))
				d.Limit = max(0, n)
				a.smartWizard(d, idx)
			})
		}},
		menuItem{label: T("sm_save"), icon: "check", do: func() {
			if err := smart.Validate(d); err != nil {
				a.flash(err.Error())
				a.smartWizard(d, idx)
				return
			}
			defs := a.smartDefs()
			if idx >= 0 && idx < len(defs) {
				defs[idx] = d
			} else {
				a.openPrompt(T("sm_name_q"), d.Name, func(n string) {
					d.Name = n
					a.smartSave(append(defs, d))
				})
				return
			}
			a.smartSave(defs)
		}},
	)
	a.menu = &menu{title: smartName(d), subtitle: T("sm_title"), items: items}
}

// smartPickField elige el campo de una regla nueva.
func (a *App) smartPickField(d smart.Def, idx int) {
	var items []menuItem
	for _, f := range smart.Fields() {
		items = append(items, menuItem{label: T("smf_" + f.Name), icon: "list", do: func() { a.smartPickOp(d, idx, f) }})
	}
	a.menu = &menu{title: T("sm_add_rule"), subtitle: T("sm_field"), items: items}
}

// smartPickOp elige la condición y luego pide el valor (si hace falta).
func (a *App) smartPickOp(d smart.Def, idx int, f smart.Field) {
	var items []menuItem
	for _, op := range f.Ops {
		items = append(items, menuItem{label: T("so_" + op), icon: "list", do: func() {
			r := smart.Rule{Field: f.Name, Op: op}
			switch {
			case f.Kind == smart.Bool:
				d.Rules = append(d.Rules, r)
				a.smartWizard(d, idx)
			case op == "between":
				a.openRawPrompt(T("sm_value_from"), "", false, func(v string) {
					r.Value = v
					a.openRawPrompt(T("sm_value_to"), "", false, func(v2 string) {
						r.Value2 = v2
						d.Rules = append(d.Rules, r)
						a.smartWizard(d, idx)
					})
				})
			default:
				q := T("sm_value")
				if op == "inlast" || op == "notinlast" {
					q = T("sm_days")
				}
				a.openRawPrompt(q, "", false, func(v string) {
					r.Value = v
					d.Rules = append(d.Rules, r)
					a.smartWizard(d, idx)
				})
			}
		}})
	}
	a.menu = &menu{title: T("smf_" + f.Name), subtitle: T("sm_op"), items: items}
}

// smartSorts son los órdenes que se ofrecen.
var smartSorts = []smart.Sort{
	{Field: "random"}, {Field: "artist"}, {Field: "album"}, {Field: "title"},
	{Field: "plays", Desc: true}, {Field: "lastplayed", Desc: true}, {Field: "added", Desc: true},
	{Field: "year"}, {Field: "year", Desc: true}, {Field: "rating", Desc: true},
}

// smartPickSort elige el orden.
func (a *App) smartPickSort(d smart.Def, idx int) {
	var items []menuItem
	for _, s := range smartSorts {
		items = append(items, menuItem{label: smartSortText(s), icon: "list", do: func() {
			d.Sort = s
			a.smartWizard(d, idx)
		}})
	}
	a.menu = &menu{title: T("sm_sort", ""), items: items}
}

// smartRuleText es una regla en palabras: "Género contiene rock".
func smartRuleText(r smart.Rule) string {
	s := T("smf_"+r.Field) + " " + T("so_"+r.Op)
	if r.Value != "" {
		s += " " + r.Value
	}
	if r.Value2 != "" {
		s += " – " + r.Value2
	}
	return s
}

// smartSortText es un orden en palabras.
func smartSortText(s smart.Sort) string {
	switch {
	case s.Field == "":
		return "-"
	case s.Field == "random":
		return T("sm_random_order")
	case s.Desc:
		return T("smf_"+s.Field) + " ↓"
	}
	return T("smf_"+s.Field) + " ↑"
}

// smartLimitText es el límite en palabras.
func smartLimitText(n int) string {
	if n <= 0 {
		return T("sm_no_limit")
	}
	return fmt.Sprint(n)
}
