package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/radio"
)

// La radio por internet: una pantalla de la barra de Select con cuatro
// pestañas, Favoritas, Populares (las más escuchadas del país, o del mundo
// con X), Buscar (por nombre, con el teclado en pantalla) y Géneros. Las
// emisoras salen del directorio libre de radio-browser.info y suenan por el
// servicio (internal/radio): MP3 y AAC, con el título de lo que suena si la
// emisora lo dice. Al escuchar una, la cola es la lista donde estaba, así
// L1/R1 cambian de emisora. Favoritas va en data/radio.json.

// Las pestañas de la radio.
const (
	rtFav   = iota
	rtLists // las listas propias (.m3u, .pls) de la carpeta radio/
	rtTop
	rtSearch
	rtTags
	rtCount
)

var radioTabKeys = [rtCount]string{"rd_fav", "rd_lists", "rd_top", "rd_search", "rd_tags"}

// radioDir es la carpeta de las listas de emisoras propias: la elegida en
// Ajustes o radio/ junto a audiofilus (AUDIOFILUS_HOME).
func (a *App) radioDir() string {
	if a.cfg.UI.RadioDir != "" {
		return a.cfg.UI.RadioDir
	}
	return filepath.Join(a.paths.Base, "radio")
}

// radioState es la pantalla de la radio.
type radioState struct {
	tab      int
	tabFocus bool
	sel      [rtCount]int
	lists    [rtCount][]radio.Station
	tags     []radio.Tag
	tag      string // el género abierto en Géneros ("" la lista de géneros)
	tagList  []radio.Station
	tagSel   int
	scr      [rtCount]scroller // el desplazamiento con el dedo de cada lista
	tagScr   scroller          // y el de las emisoras de un género
	query    string
	world    bool // Populares de todo el mundo (si no, del país)
	loading  [rtCount]bool
	err      error
	results  chan radioResult
	favs     []radio.Station
	stations *radio.Stations
	loaded   bool
	logos    chan string // logos que terminaron de bajar (la ruta de la emisora)
}

type radioResult struct {
	tab   int
	list  []radio.Station
	tags  []radio.Tag
	tag   string
	query string
	err   error
}

// initRadio lee las favoritas.
func (a *App) initRadio() {
	r := &a.radio
	r.results = make(chan radioResult, 8)
	r.logos = make(chan string, 4)
	r.stations = radio.NewStations(a.paths.Data)
	if b, err := os.ReadFile(a.favFile()); err == nil {
		_ = json.Unmarshal(b, &r.favs)
	}
	r.lists[rtFav] = r.favs
}

func (a *App) favFile() string { return filepath.Join(a.paths.Data, "radio.json") }

// saveFavs guarda las favoritas.
func (a *App) saveFavs() {
	b, err := json.MarshalIndent(a.radio.favs, "", " ")
	if err == nil {
		_ = os.WriteFile(a.favFile(), b, 0o644)
	}
	a.radio.lists[rtFav] = a.radio.favs
}

// isFav dice si una emisora es favorita.
func (a *App) isFav(s radio.Station) bool {
	for _, f := range a.radio.favs {
		if f.UUID == s.UUID {
			return true
		}
	}
	return false
}

// toggleFav añade o quita una emisora de favoritas.
func (a *App) toggleFav(s radio.Station) {
	r := &a.radio
	for i, f := range r.favs {
		if f.UUID == s.UUID {
			r.favs = append(r.favs[:i], r.favs[i+1:]...)
			a.saveFavs()
			a.flash(T("rd_unfav", s.Name))
			return
		}
	}
	r.favs = append(r.favs, s)
	a.saveFavs()
	_ = r.stations.Put(s)
	a.flash(T("rd_faved", s.Name))
}

// country es el país de Populares: el elegido en el menú de Start o, en
// automático, el que se deduce del sistema; "" es todo el mundo.
func (a *App) country() string {
	switch c := a.cfg.UI.RadioCountry; c {
	case "":
		return radioCountry()
	case "world":
		return ""
	default:
		return c
	}
}

// radioCountry es el país para Populares: el de la zona horaria (en la
// consola y el PC suele estar puesta) o, si no, el de LANG.
func radioCountry() string {
	if c := tzCountry[tzName()]; c != "" {
		return c
	}
	if l := os.Getenv("LANG"); len(l) >= 5 && l[2] == '_' {
		return strings.ToUpper(l[3:5])
	}
	return ""
}

// tzCountry es el país de las zonas horarias más comunes entre los usuarios.
var tzCountry = map[string]string{
	"America/Caracas": "VE", "America/Bogota": "CO", "America/Mexico_City": "MX", "America/Lima": "PE",
	"America/Santiago": "CL", "America/Argentina/Buenos_Aires": "AR", "America/Buenos_Aires": "AR",
	"America/Guayaquil": "EC", "America/Montevideo": "UY", "America/Asuncion": "PY", "America/La_Paz": "BO",
	"America/Panama": "PA", "America/Costa_Rica": "CR", "America/Guatemala": "GT", "America/El_Salvador": "SV",
	"America/Tegucigalpa": "HN", "America/Managua": "NI", "America/Santo_Domingo": "DO", "America/Havana": "CU",
	"America/Puerto_Rico": "PR", "Europe/Madrid": "ES", "America/New_York": "US", "America/Chicago": "US",
	"America/Denver": "US", "America/Los_Angeles": "US", "America/Sao_Paulo": "BR", "Europe/London": "GB",
	"Europe/Paris": "FR", "Europe/Berlin": "DE", "Europe/Rome": "IT", "Europe/Lisbon": "PT",
}

// radioLoad pide una pestaña al directorio en segundo plano.
func (a *App) radioLoad(tab int) {
	r := &a.radio
	if r.loading[tab] {
		return
	}
	r.loading[tab] = true
	world, query, tag, country, dir := r.world, r.query, r.tag, a.country(), a.radioDir()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res := radioResult{tab: tab, query: query, tag: tag}
		switch tab {
		case rtLists:
			res.list = radio.LoadPlaylists(dir)
		case rtTop:
			c := country
			if world {
				c = ""
			}
			res.list, res.err = radio.Top(ctx, c)
		case rtSearch:
			res.list, res.err = radio.Search(ctx, query)
		case rtTags:
			if tag == "" {
				res.tags, res.err = radio.Tags(ctx, 80)
			} else {
				res.list, res.err = radio.ByTag(ctx, tag)
			}
		}
		r.results <- res
		ebiten.ScheduleFrame()
	}()
}

// updateRadioResults recoge lo que llegó del directorio. Va en cada Update.
func (a *App) updateRadioResults() {
	r := &a.radio
	for {
		select {
		case path := <-r.logos:
			if path == a.playingSong.Path {
				a.lastTrack = "" // ya está el logo: se vuelve a armar lo que suena
			}
		case res := <-r.results:
			r.loading[res.tab] = false
			if res.err != nil {
				a.flash(T("rd_error", res.err))
				continue
			}
			switch {
			case res.tab == rtTags && res.tag == "":
				r.tags = res.tags
			case res.tab == rtTags:
				if res.tag == r.tag {
					r.tagList, r.tagSel = res.list, 0
				}
			default:
				r.lists[res.tab], r.sel[res.tab] = res.list, 0
			}
		default:
			return
		}
	}
}

// radioList es la lista de emisoras que se ve, con su selección.
func (a *App) radioList() ([]radio.Station, *int) {
	r := &a.radio
	if r.tab == rtTags && r.tag != "" {
		return r.tagList, &r.tagSel
	}
	return r.lists[r.tab], &r.sel[r.tab]
}

// radioPlay pone a sonar la emisora i de list, con la lista como cola (L1/R1
// cambian de emisora).
func (a *App) radioPlay(list []radio.Station, i int) {
	if i < 0 || i >= len(list) {
		return
	}
	if err := a.radio.stations.Put(list...); err != nil {
		a.flash(T("rd_error", err))
		return
	}
	args := []string{fmt.Sprint(i)}
	for _, s := range list {
		args = append(args, s.Path())
	}
	a.send("load", args...)
	a.send("shuffle", "off")
	a.screen = NowPlaying
	uuid := list[i].UUID
	if strings.HasPrefix(uuid, "url-") {
		return // una emisora propia: no es del directorio
	}
	go func() { // radio-browser pide contar cada escucha
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		radio.Click(ctx, uuid)
	}()
}

// updateRadio atiende la pantalla de la radio.
func (a *App) updateRadio() {
	r, in := &a.radio, &a.input
	if !r.loaded {
		r.loaded = true
		a.radioLoad(rtTop)
		a.radioLoad(rtTags)
		a.radioLoad(rtLists)
	}
	if r.tabFocus {
		switch {
		case in.Repeat(ActUp):
			r.tab = (r.tab + rtCount - 1) % rtCount
		case in.Repeat(ActDown):
			r.tab = (r.tab + 1) % rtCount
		case in.Pressed(ActRight), in.Pressed(ActConfirm):
			r.tabFocus = false
			if r.tab == rtSearch && len(r.lists[rtSearch]) == 0 {
				a.radioAskSearch()
			}
			if r.tab == rtLists {
				a.radioLoad(rtLists) // por si se añadió una lista
			}
		case in.Pressed(ActBack):
			a.goBack()
		}
		return
	}
	if r.tab == rtTags && r.tag == "" {
		a.updateRadioTags()
		return
	}
	list, sel := a.radioList()
	n := len(list)
	switch {
	case in.Repeat(ActUp):
		if *sel == 0 {
			r.tabFocus = true
		} else {
			*sel--
		}
	case in.Repeat(ActDown):
		*sel = min(max(0, n-1), *sel+1)
	case in.Pressed(ActLeft):
		r.tabFocus = true
	case in.Pressed(ActBack):
		if r.tab == rtTags && r.tag != "" {
			r.tag = ""
		} else {
			r.tabFocus = true
		}
	case in.Pressed(ActConfirm) && n > 0:
		a.radioPlay(list, *sel)
	case in.Pressed(ActMenu) && n > 0:
		a.radioMenu(list, *sel)
	case in.Pressed(ActMenu) && (r.tab == rtFav || r.tab == rtLists):
		a.radioAddURL()
	case in.Pressed(ActMenu) && r.tab == rtSearch:
		a.radioAskSearch()
	case in.Pressed(ActMenu):
		a.openCountryMenu()
	case in.Pressed(ActShuffle) && r.tab == rtTop:
		r.world = !r.world // del país o del mundo
		a.radioLoad(rtTop)
	case in.Pressed(ActShuffle) && r.tab == rtSearch:
		a.radioAskSearch()
	}
}

// updateRadioTags atiende la lista de géneros.
func (a *App) updateRadioTags() {
	r, in := &a.radio, &a.input
	n := len(r.tags)
	switch {
	case in.Repeat(ActUp):
		if r.sel[rtTags] == 0 {
			r.tabFocus = true
		} else {
			r.sel[rtTags]--
		}
	case in.Repeat(ActDown):
		r.sel[rtTags] = min(max(0, n-1), r.sel[rtTags]+1)
	case in.Pressed(ActLeft), in.Pressed(ActBack):
		r.tabFocus = true
	case in.Pressed(ActConfirm) && n > 0:
		r.tag, r.tagList = r.tags[r.sel[rtTags]].Name, nil
		a.radioLoad(rtTags)
	}
}

// radioAddURL añade una emisora por su dirección (y un nombre) a
// favoritas.
func (a *App) radioAddURL() {
	a.openRawPrompt(T("rd_url_q"), "http://", false, func(u string) {
		u = strings.TrimSpace(u)
		if !radio.IsStreamURL(u) || len(u) <= len("https://") {
			a.flash(T("rd_url_bad"))
			return
		}
		a.openRawPrompt(T("rd_name_q"), "", false, func(name string) {
			s := radio.FromURL(u, name)
			if !a.isFav(s) {
				a.toggleFav(s)
			}
			a.radio.tab, a.radio.tabFocus = rtFav, false
			a.radio.lists[rtFav] = a.radio.favs
		})
	})
}

// radioAskSearch pide el nombre de una emisora.
func (a *App) radioAskSearch() {
	a.openRawPrompt(T("rd_search_q"), a.radio.query, false, func(q string) {
		a.radio.query = q
		a.radio.tab = rtSearch
		a.radioLoad(rtSearch)
	})
}

// radioMenu es el menú (Start) de una emisora.
func (a *App) radioMenu(list []radio.Station, i int) {
	s := list[i]
	m := &menu{title: s.Name, subtitle: stationSub(s)}
	fav := T("rd_fav_add")
	if a.isFav(s) {
		fav = T("rd_fav_remove")
	}
	m.items = []menuItem{
		{label: T("ytm_listen"), icon: "play", do: func() { a.radioPlay(list, i) }},
		{label: fav, icon: "check", do: func() { a.toggleFav(s) }},
	}
	if a.radio.tab == rtSearch {
		m.items = append(m.items, menuItem{label: T("rd_search"), icon: "search", do: a.radioAskSearch})
	}
	if t := a.radio.tab; t == rtFav || t == rtLists {
		m.items = append(m.items, menuItem{label: T("rd_add_url"), icon: "plus", do: a.radioAddURL})
	}
	m.items = append(m.items, menuItem{label: T("rd_country_item"), icon: "globe",
		value: func() string { return a.countryLabel() }, do: a.openCountryMenu, close: true})
	a.menu = m
}

// stationSub es la línea de abajo de una emisora: país, códec y géneros.
func stationSub(s radio.Station) string {
	parts := []string{}
	if s.Country != "" {
		parts = append(parts, s.Country)
	}
	codec := strings.ToUpper(s.Codec)
	if s.Bitrate > 0 {
		codec += fmt.Sprintf(" %dk", s.Bitrate)
	}
	if codec = strings.TrimSpace(codec); codec != "" { // las propias no lo dicen
		parts = append(parts, codec)
	}
	if s.Tags != "" {
		tags := strings.Split(s.Tags, ",")
		parts = append(parts, strings.Join(tags[:min(2, len(tags))], ", "))
	}
	return strings.Join(parts, " · ")
}

// drawRadio pinta la pantalla de la radio: las pestañas a la izquierda y la
// lista a la derecha.
func (a *App) drawRadio(dst *ebiten.Image) {
	r := &a.radio
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	x0, top := a.contentX(), topH+8
	tabW := 72
	// Elegir una pestaña (tocándola o en el desplegable): como bajar con la
	// cruceta y entrar.
	pickTab := func(i int) {
		r.tab, r.tabFocus = i, false
		if i == rtSearch && len(r.lists[rtSearch]) == 0 {
			a.radioAskSearch()
		}
		if i == rtLists {
			a.radioLoad(rtLists)
		}
	}
	names := make([]string, rtCount)
	for i := range names {
		names[i] = T(radioTabKeys[i])
	}
	for i := 0; i < rtCount; i++ {
		y := topH + 8 + i*16
		t := T(radioTabKeys[i])
		if w < 320 {
			continue
		}
		a.tapZone(margin-3, y-4, tabW, 13, func() { pickTab(i) })
		switch {
		case i == r.tab && r.tabFocus:
			panel(dst, margin-3, y-4, tabW, 13, th.Accent)
			drawText(dst, t, SmallFace, margin+2, y, th.Bg)
		case i == r.tab:
			panel(dst, margin-3, y-4, tabW, 13, mix(th.Bg2, th.Accent, 0.35))
			drawText(dst, t, SmallFace, margin+2, y, th.Text)
		default:
			drawText(dst, t, SmallFace, margin+2, y, th.Dim)
		}
	}
	if w >= 320 {
		rect(dst, margin+75, topH+5, 1, h-topH-bottomH-44, withAlpha(th.Dim, 0x40))
	} else if a.input.touch {
		top += a.tabsButton(dst, x0, top, w-x0-margin, names, r.tab, pickTab)
	} else {
		drawText(dst, T(radioTabKeys[r.tab]), SmallFace, x0, top, th.Accent)
		top += 13
	}
	cw := w - x0 - margin
	bottom := h - bottomH - 36
	// La cabecera de la lista: el país, la búsqueda o el género.
	head := ""
	switch {
	case r.tab == rtTop && r.world:
		head = T("rd_world")
	case r.tab == rtTop && a.country() == "":
		head = T("rd_world")
	case r.tab == rtTop:
		head = T("rd_country", countryName(a.country()))
	case r.tab == rtSearch && r.query != "":
		head = "\"" + r.query + "\""
	case r.tab == rtTags && r.tag != "":
		head = "< " + r.tag
	}
	if head != "" {
		if r.tab == rtTags && r.tag != "" { // "< Género": tocarlo es volver a los géneros
			a.tapZone(x0-3, top-4, cw+6, 13, func() { a.input.inject(ActBack) })
		}
		drawTextW(dst, head, SmallFace, x0, top, cw, th.Accent2)
		top += 12
	}
	const rowH = 22
	rows := max(1, (bottom-top)/rowH)
	msg := ""
	if r.tab == rtTags && r.tag == "" {
		n := len(r.tags)
		first, off, clip := a.touchList(dst, &r.scr[rtTags], &r.sel[rtTags], n, x0-3, top-2, cw+6, bottom-top, 11, func() {
			r.tabFocus = false
			a.input.inject(ActConfirm)
		})
		whole := dst
		dst = clip
		for i := 0; i <= (bottom-top)/11 && first+i < n; i++ {
			t := r.tags[first+i]
			y := top + 2 + i*11 - off
			if first+i == r.sel[rtTags] && !r.tabFocus {
				panel(dst, x0-3, y-3, cw+6, 11, mix(th.Bg2, th.Accent, 0.35))
			}
			drawTextW(dst, t.Name, SmallFace, x0, y, cw-40, th.Text)
			c := fmt.Sprint(t.Stations)
			drawText(dst, c, SmallFace, x0+cw-textWidth(c, SmallFace), y, th.Dim)
		}
		dst = whole
		if n == 0 {
			msg = T("rd_loading")
		}
	} else {
		list, sel := a.radioList()
		n := len(list)
		scr := &r.scr[r.tab]
		if r.tab == rtTags && r.tag != "" {
			scr = &r.tagScr
		}
		first, off, clip := a.touchList(dst, scr, sel, n, x0-3, top-1, cw+6, bottom-top, rowH, func() {
			r.tabFocus = false
			a.input.inject(ActConfirm)
		})
		whole := dst
		dst = clip
		for i := 0; i <= rows && first+i < n; i++ {
			s := list[first+i]
			y := top + 3 + i*rowH - off
			if first+i == *sel && !r.tabFocus {
				panel(dst, x0-3, y-3, cw+6, rowH-2, mix(th.Bg2, th.Accent, 0.35))
				rect(dst, x0-3, y-3, 2, rowH-2, th.Accent)
			}
			icCol := th.Accent
			if a.playingSong.Path == s.Path() {
				icCol = th.Accent2
			}
			drawIcon(dst, "radio", x0, y, icCol)
			name := s.Name
			if a.isFav(s) {
				name = "* " + name
			}
			drawTextW(dst, strings.TrimSpace(name), SmallFace, x0+11, y, cw-11, th.Text)
			drawTextW(dst, stationSub(s), SmallFace, x0+11, y+9, cw-11, th.Dim)
		}
		dst = whole
		a.scrollBar(dst, x0+cw+4, top, bottom-top, first, rows, n)
		switch {
		case r.loading[r.tab]:
			msg = T("rd_loading")
		case n == 0 && r.tab == rtFav:
			msg = T("rd_no_favs")
		case n == 0 && r.tab == rtLists:
			msg = T("rd_lists_hint")
		case n == 0 && r.tab == rtSearch:
			msg = T("rd_search_hint")
		case n == 0:
			msg = T("empty")
		}
	}
	if msg != "" {
		drawTextW(dst, msg, SmallFace, x0+max(0, (cw-textWidth(msg, SmallFace))/2), top+30, cw, th.Dim)
	}
	a.drawMiniPlayer(dst)
	a.drawTopBar(dst, T("scr_radio")+" · "+T(radioTabKeys[r.tab]))
	hints := []hint{{"A", T("h_listen")}, {"START", T("h_more")}, {"SEL", T("h_menu")}}
	switch {
	case r.tab == rtTop:
		hints = append(hints[:2], hint{"X", T("rd_h_world")}, hints[2])
	case r.tab == rtSearch:
		hints = append(hints[:2], hint{"X", T("rd_search")}, hints[2])
	}
	a.drawHints(dst, hints)
}

// radioLogo es el archivo del logo de una emisora (puede no estar aún).
func (a *App) radioLogo(uuid string) string {
	return filepath.Join(a.paths.Data, "cache", "radio", "logos", uuid+".img")
}

// fetchRadioLogo baja el logo de la emisora path, si tiene y no estaba.
func (a *App) fetchRadioLogo(path string) {
	uuid := strings.TrimPrefix(path, radio.Scheme)
	st, ok := a.radio.stations.Get(uuid)
	file := a.radioLogo(uuid)
	if !ok || st.Favicon == "" {
		return
	}
	if _, err := os.Stat(file); err == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", st.Favicon, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", radio.UserAgent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			return
		}
		// Solo si se puede dibujar (JPEG o PNG; los .ico y .svg, no).
		if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
			return
		}
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		if os.WriteFile(file, b, 0o644) == nil {
			a.radio.logos <- path
			ebiten.ScheduleFrame()
		}
	}()
}

// tzName es la zona horaria del sistema ("America/Caracas"): de TZ, del
// enlace /etc/localtime o de la configuración de Knulli (Go solo dice
// "Local").
func tzName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return strings.TrimPrefix(tz, ":")
	}
	if l, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(l, "zoneinfo/"); ok {
			return after
		}
	}
	for _, f := range []string{"/userdata/system/batocera.conf", "/userdata/system/knulli.conf"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "system.timezone="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// radioCountries son los países que se pueden elegir (con el automático y
// todo el mundo, caben en el menú sin desplazarse), con su nombre en su
// idioma.
var radioCountries = []struct{ code, name string }{
	{"VE", "Venezuela"}, {"CO", "Colombia"}, {"MX", "México"}, {"AR", "Argentina"},
	{"CL", "Chile"}, {"PE", "Perú"}, {"EC", "Ecuador"}, {"DO", "Rep. Dominicana"},
	{"CU", "Cuba"}, {"PR", "Puerto Rico"}, {"UY", "Uruguay"}, {"BR", "Brasil"},
	{"ES", "España"}, {"US", "United States"},
}

// countryName es el nombre de un país (o su código si no está en la lista).
func countryName(code string) string {
	for _, c := range radioCountries {
		if c.code == code {
			return c.name
		}
	}
	return orDefault(code, "?")
}

// countryLabel es cómo se ve la elección de país.
func (a *App) countryLabel() string {
	switch c := a.cfg.UI.RadioCountry; c {
	case "":
		return T("rd_auto", orDefault(radioCountry(), "?"))
	case "world":
		return T("rd_world_short")
	default:
		return countryName(c)
	}
}

// openCountryMenu deja elegir el país de Populares.
func (a *App) openCountryMenu() {
	set := func(c string) func() {
		return func() {
			a.cfg.UI.RadioCountry = c
			a.saveConfig()
			a.radio.world = false
			a.radio.tab, a.radio.tabFocus = rtTop, false
			a.radioLoad(rtTop)
		}
	}
	is := func(c string) func() bool { return func() bool { return a.cfg.UI.RadioCountry == c } }
	m := &menu{title: T("rd_country_item"), subtitle: T("rd_country_sub")}
	m.items = []menuItem{
		{label: T("rd_auto", orDefault(radioCountry(), "?")), icon: "clock", do: set(""), active: is(""), close: true},
		{label: T("rd_world_short"), icon: "globe", do: set("world"), active: is("world"), close: true},
	}
	for _, c := range radioCountries {
		m.items = append(m.items, menuItem{label: c.name, icon: "radio", do: set(c.code), active: is(c.code), close: true})
	}
	for i, it := range m.items {
		if it.active() {
			m.sel = i
		}
	}
	a.menu = m
}
