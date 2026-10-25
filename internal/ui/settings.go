package ui

import (
	"fmt"
	"github.com/juanvillacortac/audiofilus/internal/sfx"
	"image"
	"math"
	"runtime"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// resamplerModes son las formas de remuestrear: auto (calidad en el
// escritorio, lineal en las portátiles), lineal (batería) y calidad (sinc).
var resamplerModes = []string{"auto", "linear", "quality"}

// Las opciones de Ajustes, en orden.
const (
	setFolder = iota
	setRescan
	setLanguage
	setShuffle
	setRepeat
	setVolume
	setSleep
	setSleepMode
	setClock
	setSwapAB
	setExitBg
	setExitStop
	setGitHub
	setServers
	setSounds
	setUISounds
	setDonate
	setCloseBg
	setLyricsWeb
	setReplayGain
	setEQ
	setCrossfade
	setScrobble
	setRate
	setUpdate
	setResampler
	setDLNAServer
	setDLNARenderer
	setMilkDir
	setRadioDir
	setThemeColor
	setCrossfeed
	setAlarm
	setVibration
	setMPD
	setCount
)

// sleepChoices son los tiempos que se pueden elegir para el sleep (segundos; 0 es nunca).
var sleepChoices = []int{0, 15, 30, 60, 120, 300}

// Ajustes es un mosaico: cada opción es una baldosa con su icono, su nombre y
// su valor a la vista, agrupadas por secciones. Hay cuatro tipos de baldosa:
//
//   - interruptor (sí/no): A lo cambia;
//   - elección (varias opciones, que se ven como puntitos): A la abre, la
//     izquierda y la derecha eligen (se aplica al momento) y A o B la cierran;
//   - deslizador (el volumen): como la elección, de 10 en 10;
//   - acción (carpeta, buscar, salir): A la hace.
//
// Las flechas se mueven entre baldosas por su posición en pantalla, así que el
// mosaico se adapta a cualquier ancho sin tablas de vecinos.

// tileKind es el tipo de una baldosa.
type tileKind int

const (
	tileToggle tileKind = iota
	tileChoice
	tileSlider
	tileAction
)

// settingTile es una baldosa de Ajustes.
type settingTile struct {
	id   int
	icon string
	kind tileKind
}

// settingSection es un grupo de baldosas con su título.
type settingSection struct {
	title string // clave de lang.go
	tiles []settingTile
}

// settingSections es el mosaico entero, en orden.
var settingSections = []settingSection{
	{"sec_library", []settingTile{
		{setFolder, "folder", tileAction},
		{setRescan, "search", tileAction},
		{setServers, "server", tileAction},
		{setRadioDir, "folder", tileAction},
		{setMilkDir, "folder", tileAction},
	}},
	{"sec_playback", []settingTile{
		{setShuffle, "shuffle", tileToggle},
		{setRepeat, "repeat", tileChoice},
		{setVolume, "speaker", tileSlider},
		{setCrossfade, "repeat", tileChoice},
		{setAlarm, "clock", tileAction},
	}},
	{"sec_sound", []settingTile{
		{setReplayGain, "speaker", tileChoice},
		{setEQ, "eq", tileAction},
		{setRate, "headphones", tileChoice},
		{setResampler, "headphones", tileChoice},
		{setCrossfeed, "headphones", tileChoice},
	}},
	{"sec_online", []settingTile{
		{setScrobble, "user", tileAction},
		{setLyricsWeb, "lyrics", tileToggle},
		{setDLNAServer, "server", tileToggle},
		{setDLNARenderer, "speaker", tileToggle},
		{setMPD, "server", tileAction},
	}},
	{"sec_screen", []settingTile{
		{setSleep, "moon", tileChoice},
		{setSleepMode, "screen", tileChoice},
		{setClock, "clock", tileToggle},
		{setThemeColor, "palette", tileChoice},
	}},
	{"sec_system", []settingTile{
		{setLanguage, "globe", tileChoice},
		{setSounds, "speaker", tileChoice},
		{setUISounds, "speaker", tileChoice},
		{setVibration, "vibrate", tileChoice},
		{setSwapAB, "pad", tileToggle},
	}},
	{"sec_about", []settingTile{
		{setUpdate, "download", tileAction},
		{setGitHub, "gh", tileAction},
		{setDonate, "cup", tileAction},
	}},
	{"sec_exit", []settingTile{
		{setCloseBg, "bg", tileToggle},
		{setExitBg, "bg", tileAction},
		{setExitStop, "power", tileAction},
	}},
}

// Medidas del mosaico.
const (
	tileH   = 34
	tileGap = 4
	secH    = 13 // el título de cada sección
)

// tileRect es dónde va una baldosa, en coordenadas del contenido (sin desplazar).
type tileRect struct {
	tile       settingTile
	x, y, w, h int
}

// settingsLayout coloca las baldosas para un ancho w: dos columnas en pantallas
// estrechas y tres en las anchas. Devuelve también el alto total.
// sections son las secciones de Ajustes que tienen sentido aquí: el sleep
// de la app (apagar la pantalla, los gestos del bolsillo) es de las
// portátiles, y en el móvil la música ya sigue al salir de la app (sin
// "Segundo plano" ni "Al cerrar, seguir"). Una sección sin baldosas no sale.
func (a *App) sections() []settingSection {
	hide := map[int]bool{setSleep: !a.canSleep(), setSleepMode: !a.canSleep(),
		setExitBg: a.mobile(), setCloseBg: a.mobile(),
		// En el móvil el servicio va dentro de la app y no se puede reiniciar.
		setRate: a.mobile(), setUpdate: a.mobile() && InstallPackage == nil, setResampler: a.mobile(),
		// iOS no deja a la app usar multicast (SSDP) sin un permiso de Apple:
		// DLNA no se encontraría. MPD sí, por Bonjour (mobile/network.go).
		setDLNAServer: runtime.GOOS == "ios", setDLNARenderer: runtime.GOOS == "ios",
		// En el móvil no hay explorador de carpetas fuera de la app.
		setMilkDir: a.mobile(), setRadioDir: a.mobile(),
		// La vibración, solo en las portátiles (el PC y el móvil no tienen motor).
		setVibration: a.mobile() || a.status.Platform == "" || a.status.Platform == "desktop"}
	var out []settingSection
	for _, sec := range settingSections {
		s := settingSection{title: sec.title}
		for _, t := range sec.tiles {
			if !hide[t.id] {
				s.tiles = append(s.tiles, t)
			}
		}
		if len(s.tiles) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func settingsLayout(w int, secs []settingSection) ([]tileRect, []int, int) {
	cols := 2
	if w >= 380 {
		cols = 3
	}
	tw := (w - 2*margin - (cols-1)*tileGap) / cols
	var out []tileRect
	var heads []int
	y := 0
	for _, sec := range secs {
		heads = append(heads, y)
		y += secH
		for i, t := range sec.tiles {
			c := i % cols
			if c == 0 && i > 0 {
				y += tileH + tileGap
			}
			out = append(out, tileRect{t, margin + c*(tw+tileGap), y, tw, tileH})
		}
		y += tileH + tileGap + 2
	}
	return out, heads, y
}

// updateSettings atiende los botones en Ajustes.
func (a *App) updateSettings() {
	in := &a.input
	tiles, _, _ := settingsLayout(a.w, a.sections())
	a.settingsSel = min(a.settingsSel, len(tiles)-1)
	cur := tiles[a.settingsSel].tile
	if a.settingsQR {
		if in.Pressed(ActConfirm) || in.Pressed(ActBack) {
			a.settingsQR = false
		}
		return
	}
	if a.settingsEdit {
		switch {
		case in.Repeat(ActLeft):
			a.changeSetting(cur.id, -1)
		case in.Repeat(ActRight):
			a.changeSetting(cur.id, 1)
		case in.Pressed(ActConfirm), in.Pressed(ActBack):
			a.settingsEdit = false
			if a.rateDirty {
				a.rateDirty = false
				a.send("restart")
			}
		}
		return
	}
	for _, d := range []struct {
		act    Action
		dx, dy int
	}{{ActUp, 0, -1}, {ActDown, 0, 1}, {ActLeft, -1, 0}, {ActRight, 1, 0}} {
		if in.Repeat(d.act) {
			a.settingsSel = nearestTile(tiles, a.settingsSel, d.dx, d.dy)
			return
		}
	}
	if !in.Pressed(ActConfirm) {
		return
	}
	switch cur.kind {
	case tileToggle:
		a.changeSetting(cur.id, 1)
	case tileChoice, tileSlider:
		a.settingsEdit = true
	case tileAction:
		switch cur.id {
		case setFolder:
			if PickFolder != nil { // el selector de carpetas del sistema (iOS)
				PickFolder()
				return
			}
			a.picker = newPicker(firstOr(a.cfg.MusicDirs, ""))
			a.pickFor = pickMusic
			a.mode = modePicker
		case setMilkDir:
			a.picker = newPicker(a.milkDir())
			a.pickFor = pickMilkDrop
			a.mode = modePicker
		case setRadioDir:
			a.picker = newPicker(a.radioDir())
			a.pickFor = pickRadio
			a.mode = modePicker
		case setRescan:
			a.startScan()
		case setExitBg, setExitStop:
			a.exit(cur.id == setExitBg)
		case setUpdate:
			a.pressUpdate()
		case setAlarm:
			a.openAlarm() // alarmui.go
		case setGitHub:
			a.settingsQR, a.qrURL = true, projectURL
		case setDonate:
			a.settingsQR, a.qrURL = true, donateURL
		case setServers:
			a.openServersMenu()
		case setEQ:
			a.openEQMenu() // gráfico o paramétrico (peq.go)
		case setScrobble:
			a.openScrobbleMenu()
		case setMPD:
			a.openMPDMenu() // mpdui.go
		}
	}
}

// nearestTile es la baldosa más cercana a la elegida en la dirección (dx, dy):
// la que queda en ese lado y más cerca, penalizando el desvío lateral. Si no
// hay ninguna, se queda donde está.
func nearestTile(tiles []tileRect, sel, dx, dy int) int {
	c := tiles[sel]
	cx, cy := c.x+c.w/2, c.y+c.h/2
	best, bestD := sel, 1<<30
	for i, t := range tiles {
		tx, ty := t.x+t.w/2, t.y+t.h/2
		along := (tx-cx)*dx + (ty-cy)*dy
		if i == sel || along <= 0 {
			continue
		}
		side := (tx - cx) * dy
		if dy == 0 {
			side = ty - cy
		}
		if dx != 0 && side != 0 {
			continue // a los lados, solo en la misma fila
		}
		d := along + 3*abs(side)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// changeSetting mueve una opción step pasos (o la cambia, si es sí/no).
func (a *App) changeSetting(id, step int) {
	cycle := func(n, i int) int { return (i + step + n) % n }
	switch id {
	case setLanguage:
		_, i := a.settingChoice(id)
		SetLanguage(languages[cycle(len(languages), i)])
		a.cfg.Language = lang
		a.local.relabel()
		a.resetBrowse() // los títulos de las vistas están en el idioma anterior
		a.saveConfig()
	case setShuffle:
		a.send("shuffle", onOffArg(!a.status.Shuffle))
	case setRepeat:
		n, i := a.settingChoice(id)
		a.send("repeat", repeatOrder[cycle(n, i)])
	case setVolume:
		a.changeVolume(10 * step)
	case setSleep:
		n, i := a.settingChoice(id)
		a.cfg.UI.SleepAfter = sleepChoices[cycle(n, i)]
		a.saveConfig()
	case setSleepMode:
		n, i := a.settingChoice(id)
		a.cfg.UI.SleepMode = sleepModes[cycle(n, i)]
		a.saveConfig()
	case setClock:
		a.cfg.UI.Clock24 = !a.cfg.UI.Clock24
		a.saveConfig()
	case setCloseBg:
		on := !a.keepPlaying()
		a.cfg.UI.CloseToBackground = &on
		a.saveConfig()
		if a.client != nil {
			a.client.SetKeepPlaying(on)
		}
	case setLyricsWeb:
		a.cfg.UI.LyricsOffline = !a.cfg.UI.LyricsOffline
		a.saveConfig()
	case setReplayGain:
		n, i := a.settingChoice(id)
		mode := rgModes[cycle(n, i)]
		a.send("replaygain", mode)
		a.status.DSP.ReplayGain = mode
	case setRate:
		// La salida se abre al arrancar el servicio: se guarda y se reinicia.
		n, i := a.settingChoice(id)
		a.cfg.SampleRate = rateChoices[cycle(n, i)]
		a.saveConfig()
		// Con la cruceta se reinicia al cerrar la elección, no en cada paso.
		if a.settingsEdit {
			a.rateDirty = true
		} else {
			a.send("restart")
		}
	case setCrossfeed:
		n, i := a.settingChoice(id)
		lv := engine.CrossfeedLevels[cycle(n, i)]
		a.send("crossfeed", orDefault(lv, "off"))
		a.status.DSP.Crossfeed = lv
	case setThemeColor:
		n, i := a.settingChoice(id)
		c := themeColors[cycle(n, i)]
		a.cfg.UI.ThemeColor = ""
		if c.name != "album" {
			a.cfg.UI.ThemeColor = c.name
		}
		a.saveConfig()
		if th, ok := fixedTheme(a.cfg.UI.ThemeColor); ok {
			a.theme = th
		} else if a.playing != nil {
			a.themeFor = a.playing // vuelve al color del disco que suena
		} else {
			a.theme = DefaultTheme
		}
	case setResampler:
		n, i := a.settingChoice(id)
		m := resamplerModes[cycle(n, i)]
		a.send("resampler", m)
		a.status.DSP.Resampler = m
	case setDLNAServer, setDLNARenderer:
		// DLNA (internal/dlna, en el servicio): se guarda en audiofilus.json y
		// el servicio lo vuelve a leer.
		if id == setDLNAServer {
			a.cfg.DLNA.Server = !a.cfg.DLNA.Server
		} else {
			a.cfg.DLNA.Renderer = !a.cfg.DLNA.Renderer
		}
		a.saveConfig()
		a.send("dlna", "apply")
	case setCrossfade:
		n, i := a.settingChoice(id)
		v := xfChoices[cycle(n, i)]
		a.send("crossfade", fmt.Sprint(v))
		a.status.DSP.Crossfade = v
	case setSwapAB:
		a.cfg.UI.SwapAB = !a.cfg.UI.SwapAB
		a.input.swapAB = a.cfg.UI.SwapAB
		a.saveConfig()
	case setUISounds:
		n, i := a.settingChoice(id)
		a.cfg.UI.UISounds = soundLevels[cycle(n, i)]
		a.saveConfig()
		a.playSound(sfx.Confirm) // para oír el volumen nuevo
	case setVibration:
		n, i := a.settingChoice(id)
		a.cfg.UI.Vibration = soundLevels[cycle(n, i)]
		a.saveConfig()
		a.send("vibration", orDefault(a.cfg.UI.Vibration, "mid")) // vibra con la nueva
	case setSounds:
		n, i := a.settingChoice(id)
		a.cfg.UI.Sounds = soundLevels[cycle(n, i)]
		a.saveConfig()
		a.playSound(sfx.Play) // para oír el volumen nuevo
	}
}

// repeatOrder y sleepModes son las opciones de Repetir y del modo de apagado.
var (
	repeatOrder = []string{"off", "all", "one"}
	sleepModes  = []string{"backlight", "oled"}
	soundLevels = []string{"off", "low", "", "high"} // "" es el medio, el de por defecto
)

// settingChoice dice cuántas opciones tiene una elección y cuál está puesta.
func (a *App) settingChoice(id int) (n, cur int) {
	find := func(n int, eq func(int) bool) (int, int) {
		for i := 0; i < n; i++ {
			if eq(i) {
				return n, i
			}
		}
		return n, 0
	}
	switch id {
	case setLanguage:
		return find(len(languages), func(i int) bool { return languages[i] == lang })
	case setRepeat:
		return find(len(repeatOrder), func(i int) bool { return repeatOrder[i] == orDefault(a.status.Repeat, "off") })
	case setSleep:
		return find(len(sleepChoices), func(i int) bool { return sleepChoices[i] == a.cfg.UI.SleepAfter })
	case setSleepMode:
		return find(len(sleepModes), func(i int) bool { return sleepModes[i] == orDefault(a.cfg.UI.SleepMode, "backlight") })
	case setSounds:
		return find(len(soundLevels), func(i int) bool { return soundLevels[i] == a.cfg.UI.Sounds })
	case setUISounds:
		return find(len(soundLevels), func(i int) bool { return soundLevels[i] == a.cfg.UI.UISounds })
	case setVibration:
		return find(len(soundLevels), func(i int) bool { return soundLevels[i] == a.cfg.UI.Vibration })
	case setReplayGain:
		return find(len(rgModes), func(i int) bool { return rgModes[i] == a.dspOf().ReplayGain })
	case setCrossfade:
		return find(len(xfChoices), func(i int) bool { return xfChoices[i] == a.dspOf().Crossfade })
	case setResampler:
		return find(len(resamplerModes), func(i int) bool { return resamplerModes[i] == orDefault(a.dspOf().Resampler, "auto") })
	case setThemeColor:
		return find(len(themeColors), func(i int) bool { return themeColors[i].name == orDefault(a.cfg.UI.ThemeColor, "album") })
	case setCrossfeed:
		return find(len(engine.CrossfeedLevels), func(i int) bool { return engine.CrossfeedLevels[i] == a.dspOf().Crossfeed })
	case setRate:
		return find(len(rateChoices), func(i int) bool { return rateChoices[i] == a.cfg.SampleRate })
	}
	return 0, 0
}

func onOffArg(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// settingValue es el valor que se ve en cada baldosa.
func (a *App) settingValue(i int) string {
	switch i {
	case setFolder:
		return firstOr(a.cfg.MusicDirs, "-")
	case setMilkDir:
		return a.milkDir()
	case setAlarm:
		return a.alarmLabel()
	case setRadioDir:
		return a.radioDir()
	case setRescan:
		return T("rescan_sub")
	case setLanguage:
		return langNames[lang]
	case setShuffle:
		return onOffLabel(a.status.Shuffle)
	case setRepeat:
		return T("repeat_" + orDefault(a.status.Repeat, "off"))
	case setVolume:
		return fmt.Sprintf("%d%%", int(a.volume*100+0.5))
	case setSleep:
		if a.cfg.UI.SleepAfter == 0 {
			return T("never")
		}
		if a.cfg.UI.SleepAfter < 60 {
			return fmt.Sprintf("%d s", a.cfg.UI.SleepAfter)
		}
		return fmt.Sprintf("%d min", a.cfg.UI.SleepAfter/60)
	case setSleepMode:
		return T("mode_" + orDefault(a.cfg.UI.SleepMode, "backlight"))
	case setClock:
		return onOffLabel(a.cfg.UI.Clock24)
	case setSwapAB:
		return onOffLabel(a.cfg.UI.SwapAB)
	case setCloseBg:
		return onOffLabel(a.keepPlaying())
	case setLyricsWeb:
		return onOffLabel(!a.cfg.UI.LyricsOffline)
	case setReplayGain:
		return T("rg_" + a.dspOf().ReplayGain)
	case setCrossfade:
		if v := a.dspOf().Crossfade; v > 0 {
			return fmt.Sprintf("%g s", v)
		}
		return onOffLabel(false)
	case setEQ:
		if a.dspOf().EQMode == "parametric" {
			return T("eq_parametric")
		}
		return T("eq_" + a.dspOf().EQPreset)
	case setResampler:
		return T("rs_" + orDefault(a.dspOf().Resampler, "auto"))
	case setThemeColor:
		return T("tc_" + orDefault(a.cfg.UI.ThemeColor, "album"))
	case setCrossfeed:
		return crossfeedLabel(a.dspOf().Crossfeed)
	case setDLNAServer:
		return onOffLabel(a.cfg.DLNA.Server)
	case setDLNARenderer:
		return onOffLabel(a.cfg.DLNA.Renderer)
	case setMPD:
		return onOffLabel(a.cfg.MPD.On)
	case setRate:
		return rateLabel(a.cfg.SampleRate)
	case setScrobble:
		var names []string
		if a.cfg.Scrobble.LastFMSession != "" {
			names = append(names, "Last.fm")
		}
		if a.cfg.Scrobble.ListenBrainzToken != "" {
			names = append(names, "ListenBrainz")
		}
		if len(names) == 0 {
			return T("acct_none")
		}
		return strings.Join(names, ", ")
	case setExitBg:
		return T("exit_bg_sub")
	case setExitStop:
		return T("exit_stop_sub")
	case setUpdate:
		return a.updateValue()
	case setGitHub:
		return projectURL[len("https://"):]
	case setDonate:
		return donateURL[len("https://"):]
	case setSounds:
		return T("snd_" + orDefault(a.cfg.UI.Sounds, "mid"))
	case setUISounds:
		return T("snd_" + orDefault(a.cfg.UI.UISounds, "mid"))
	case setVibration:
		return T("vib_" + orDefault(a.cfg.UI.Vibration, "mid"))
	case setServers:
		if len(a.cfg.Servers) == 0 {
			return T("srv_none")
		}
		return N("n_servers", len(a.cfg.Servers))
	}
	return ""
}

// settingOn dice si un interruptor está encendido.
func (a *App) settingOn(id int) bool {
	switch id {
	case setShuffle:
		return a.status.Shuffle
	case setClock:
		return a.cfg.UI.Clock24
	case setDLNAServer:
		return a.cfg.DLNA.Server
	case setDLNARenderer:
		return a.cfg.DLNA.Renderer
	case setSwapAB:
		return a.cfg.UI.SwapAB
	case setCloseBg:
		return a.keepPlaying()
	case setLyricsWeb:
		return !a.cfg.UI.LyricsOffline
	}
	return false
}

// settingNames son los nombres cortos de las baldosas.
func settingNames() [setCount]string {
	return [setCount]string{
		T("set_folder"), T("set_rescan"), T("set_language"), T("set_shuffle"), T("set_repeat"),
		T("set_volume"), T("set_sleep"), T("set_sleep_mode"), T("set_clock"), T("set_swap"),
		T("m_exit_bg"), T("m_exit_stop"), "GitHub", T("srv_title"), T("set_sounds"), T("set_ui_sounds"), T("set_donate"),
		T("set_close_bg"), T("set_lyrics_web"), T("set_rg"), T("set_eq"), T("set_crossfade"), T("set_scrobble"), T("set_rate"), T("set_update"), T("set_resampler"), T("set_dlna_server"), T("set_dlna_renderer"),
		T("set_milk_dir"), T("set_radio_dir"), T("set_theme_color"), T("set_crossfeed"), T("al_title"), T("set_vibration"), T("set_mpd"),
	}
}

// drawSettings pinta el mosaico, desplazado para que la baldosa elegida se vea.
func (a *App) drawSettings(dst *ebiten.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	secs := a.sections()
	tiles, heads, total := settingsLayout(w, secs)
	sel := min(a.settingsSel, len(tiles)-1)
	top, bottom := topH+4, h-bottomH-2
	view := bottom - top

	// El desplazamiento sigue a la baldosa elegida (con su título, si es la
	// primera de su sección) y se acerca suave, como las listas.
	// Con el dedo se desplaza libre, píxel a píxel; en cuanto la elegida
	// cambia por la cruceta, vuelve a seguirla.
	sc := &a.settingsScr
	if sel != sc.drawn {
		sc.free, sc.drawn = false, sel
	}
	sc.max = float64(max(0, total-view))
	if sc.free {
		a.settingsScroll = max(0, min(sc.max, sc.px))
	} else {
		want := a.settingsScroll
		if t := tiles[sel]; t.y-secH < int(want) {
			want = float64(t.y - secH)
		} else if t.y+t.h > int(want)+view {
			want = float64(t.y + t.h - view)
		}
		want = max(0, min(want, float64(total-view)))
		a.settingsScroll += (want - a.settingsScroll) * 0.35
		if abs(int(want-a.settingsScroll)) < 1 {
			a.settingsScroll = want
		}
		sc.px = a.settingsScroll
	}
	a.scrollArea(0, top, w, view, sc.by)
	oy := top - int(a.settingsScroll+0.5)

	area := dst.SubImage(image.Rect(0, top, w, bottom)).(*ebiten.Image)
	for i, sec := range secs {
		y := oy + heads[i]
		if y+secH < top || y > bottom {
			continue
		}
		drawText(area, T(sec.title), SmallFace, margin, y+2, th.Accent)
		tw := textWidth(T(sec.title), SmallFace)
		rect(area, margin+tw+5, y+5, w-2*margin-tw-5, 1, withAlpha(th.Dim, 0x40))
	}
	names := settingNames()
	a.dragStep(0, tileH+tileGap)
	for i, t := range tiles {
		y := oy + t.y
		if y+t.h < top || y > bottom {
			continue
		}
		// Tocar una baldosa la elige y la usa (una elección: la abre; ya
		// abierta, la cierra). Antes de pintarla: sus chips y su barra van
		// encima y se tocan directamente.
		idx := i
		vy0, vy1 := max(y, top), min(y+t.h, bottom)
		a.tapZone(t.x, vy0, t.w, vy1-vy0, func() { sc.pick(&a.settingsSel, idx); a.input.inject(ActConfirm) })
		a.drawTile(area, t, names[t.tile.id], t.x, y, i == sel)
	}
	a.scrollBar(dst, w-4, top, view, int(a.settingsScroll), view, total)

	a.drawTopBar(dst, T("settings"))
	cur := tiles[sel].tile
	if a.settingsQR {
		a.drawQR(dst)
	}
	switch {
	case a.settingsQR:
		a.drawHints(dst, []hint{{"B", T("h_close")}})
	case a.settingsEdit:
		a.drawHints(dst, []hint{{"<>", T("h_choose")}, {"A", T("h_done")}})
	case cur.kind == tileToggle:
		a.drawHints(dst, []hint{{"A", T("h_toggle")}, {"B", T("h_back")}})
	case cur.kind == tileAction:
		a.drawHints(dst, []hint{{"A", T("h_ok")}, {"B", T("h_back")}})
	default:
		a.drawHints(dst, []hint{{"A", T("h_change")}, {"B", T("h_back")}})
	}
}

// drawTile pinta una baldosa: icono y nombre arriba, el valor abajo.
func (a *App) drawTile(dst *ebiten.Image, t tileRect, name string, x, y int, sel bool) {
	th := a.theme
	w, h := t.w, t.h
	id := t.tile.id
	edit := sel && a.settingsEdit
	bg := withAlpha(th.Bg2, 0xc0)
	if sel {
		bg = mix(th.Bg2, th.Accent, 0.28)
	}
	panel(dst, x, y, w, h, bg)
	if sel {
		if edit {
			marching(dst, x, y, w, h, a.t, th.Accent)
		} else {
			frame(dst, x+1, y, w-2, 1, th.Accent)
			rect(dst, x, y+1, 1, h-2, th.Accent)
			rect(dst, x+w-1, y+1, 1, h-2, th.Accent)
			rect(dst, x+1, y+h-1, w-2, 1, th.Accent)
		}
	}
	iconCol := th.Dim
	if sel {
		iconCol = th.Accent
	}
	if id == setExitStop {
		iconCol = mix(iconCol, th.Accent2, 0.6)
	}
	drawIcon(dst, t.tile.icon, x+5, y+5, iconCol)
	drawTextW(dst, name, SmallFace, x+15, y+5, w-19, th.Text)

	// El valor, según el tipo.
	vy := y + 19
	vw := w - 10
	val := a.settingValue(id)
	valCol := th.Dim
	if sel {
		valCol = th.Text
	}
	switch t.tile.kind {
	case tileToggle:
		on := a.settingOn(id)
		sx := x + w - 19
		track := withAlpha(th.Dim, 0x50)
		if on {
			track = th.Accent
		}
		panel(dst, sx, vy, 14, 7, track)
		kx := sx + 1
		if on {
			kx = sx + 8
		}
		rect(dst, kx, vy+1, 5, 5, th.Text)
		drawText(dst, val, SmallFace, x+5, vy, valCol)
	case tileSlider:
		drawText(dst, val, SmallFace, x+5, vy, valCol)
		bx := x + 5 + textWidth("200%", SmallFace) + 4
		bw := x + w - 5 - bx
		if bw > 10 {
			rect(dst, bx, vy+3, bw, 2, withAlpha(th.Dim, 0x50))
			rect(dst, bx, vy+3, int(float64(bw)*min(a.volume, 2)/2), 2, th.Accent)
			rect(dst, bx+bw/2, vy+1, 1, 6, withAlpha(th.Text, 0x80)) // el 100 %
			// Tocar la barra pone el volumen ahí (de 0 a 200 %).
			a.zone(bx, vy-4, bw, 15, func(px, _ int) {
				want := float64(max(0, min(bw, px))) / float64(bw) * 2
				a.changeVolume(int(math.Round((want - a.volume) * 100)))
			})
		}
	case tileChoice:
		n, cur := a.settingChoice(id)
		if a.drawChips(dst, id, n, cur, x+5, vy-2, vw, sel) {
			break
		}
		if edit {
			val = "< " + val + " >"
		}
		drawTextW(dst, val, SmallFace, x+5, vy, vw, valCol)
		// Un puntito por opción: se ve cuántas hay y en cuál se está.
		dx := x + w - 5 - n*4
		for i := 0; i < n; i++ {
			c := withAlpha(th.Dim, 0x60)
			if i == cur {
				c = th.Accent
			}
			rect(dst, dx+i*4, y+h-5, 2, 2, c)
		}
	case tileAction:
		if id == setFolder || id == setMilkDir || id == setRadioDir {
			val = fitLeft(val, SmallFace, vw)
		}
		drawTextW(dst, val, SmallFace, x+5, vy, vw, withAlpha(valCol, 0xc0))
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// fitLeft recorta s por el principio ("...fin") para que quepa en w: para rutas.
func fitLeft(s string, face text.Face, w int) string {
	if textWidth(s, face) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 && textWidth("..."+string(r), face) > w {
		r = r[1:]
	}
	return "..." + string(r)
}

// saveConfig guarda audiofilus.json (es el mismo archivo que lee el servicio).
func (a *App) saveConfig() {
	if a.offline {
		return
	}
	if err := config.Save(a.paths, a.cfg); err != nil {
		a.flash(err.Error())
	}
}

// choiceLabel es el texto de la opción i de una elección.
func choiceLabel(id, i int) string {
	switch id {
	case setLanguage:
		return langNames[languages[i]]
	case setRepeat:
		return T("repeat_" + repeatOrder[i])
	case setSleepMode:
		return T("mode_" + sleepModes[i])
	case setSounds, setUISounds:
		return T("snd_" + orDefault(soundLevels[i], "mid"))
	case setVibration:
		return T("vib_" + orDefault(soundLevels[i], "mid"))
	case setReplayGain:
		return T("rg_" + rgModes[i])
	case setResampler:
		return T("rs_" + resamplerModes[i])
	case setThemeColor:
		return T("tc_" + themeColors[i].name)
	case setCrossfeed:
		return crossfeedLabel(engine.CrossfeedLevels[i])
	case setRate:
		return rateLabel(rateChoices[i])
	case setCrossfade:
		if xfChoices[i] == 0 {
			return onOffLabel(false)
		}
		return fmt.Sprintf("%gs", xfChoices[i])
	case setSleep:
		s := sleepChoices[i]
		switch {
		case s == 0:
			return T("never")
		case s < 60:
			return fmt.Sprintf("%ds", s)
		}
		return fmt.Sprintf("%dm", s/60)
	}
	return ""
}

// drawChips pinta todas las opciones de una elección, una al lado de otra con
// la puesta resaltada, si caben en w. Si no caben, no pinta nada y devuelve
// false (la baldosa enseña solo el valor y unos puntitos).
func (a *App) drawChips(dst *ebiten.Image, id, n, cur, x, y, w int, sel bool) bool {
	th := a.theme
	const pad, gap = 3, 2
	widths := make([]int, n)
	total := (n - 1) * gap
	for i := range widths {
		widths[i] = textWidth(choiceLabel(id, i), SmallFace) + 2*pad
		total += widths[i]
	}
	if total > w {
		return false
	}
	for i, cw := range widths {
		bg, fg := withAlpha(th.Shadow, 0x70), th.Dim
		if i == cur {
			bg, fg = withAlpha(th.Accent, 0x90), th.Text
			if sel {
				bg = th.Accent
			}
		}
		panel(dst, x, y, cw, 11, bg)
		drawText(dst, choiceLabel(id, i), SmallFace, x+pad, y+2, fg)
		if d := i - cur; d != 0 { // tocar una opción la elige
			a.tapZone(x, y-2, cw+gap, 15, func() { a.changeSetting(id, d) })
		}
		x += cw + gap
	}
	return true
}

// keepPlaying dice si, al cerrarse la interfaz, la música sigue sonando
// (Ajustes → Al cerrar, seguir sonando). Si no se eligió, sí en el escritorio
// (cerrar la ventana de un reproductor no suele parar la música) y no en las
// portátiles (el atajo de salida de PortMaster cierra la interfaz, y ahí se
// espera que pare).
func (a *App) keepPlaying() bool {
	if v := a.cfg.UI.CloseToBackground; v != nil {
		return *v
	}
	return a.status.Platform == "desktop"
}

// crossfeedLabel es el nombre de un nivel de crossfeed ("" es apagado).
func crossfeedLabel(level string) string {
	if level == "" {
		return onOffLabel(false)
	}
	return T("cf_" + level)
}
