package ui

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/engine"
	"github.com/juanvillacortac/audiofilus/internal/scrobble"
)

// El sonido en Ajustes: ReplayGain, el fundido entre canciones, el
// ecualizador (un editor con sus diez bandas) y las cuentas donde contar
// lo que se escucha (Last.fm, ListenBrainz). Todo lo aplica el servicio
// (engine.DSP, internal/scrobble); aquí solo se elige.

// rgModes son los modos de ReplayGain, en el orden en que se eligen.
var rgModes = []string{"auto", "track", "album", "off"}

// rateChoices son las frecuencias de salida que se pueden elegir. Todo se
// remuestrea a la de salida; con la misma que el archivo y el sonido sin
// tocar (volumen al 100 %, sin ReplayGain ni ecualizador), las muestras
// salen tal cual (aunque luego PipeWire o PulseAudio pueden remuestrear a
// la suya).
var rateChoices = []int{44100, 48000, 96000, 192000}

// rateLabel es una frecuencia legible: "44,1 kHz".
func rateLabel(r int) string {
	s := strconv.FormatFloat(float64(r)/1000, 'f', -1, 64)
	if lang == "es" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s + " kHz"
}

// xfChoices son los segundos de fundido que se pueden elegir (0: nada).
var xfChoices = []float64{0, 2, 4, 6, 8, 10, 12}

// dspOf es la configuración del sonido que dice el servicio.
func (a *App) dspOf() engine.DSP { return a.status.DSP.Normalize() }

// eqEditor es el editor del ecualizador abierto.
type eqEditor struct {
	gains  []float64
	preset string
	sel    int
}

// openEQ abre el editor con lo que tiene el servicio.
func (a *App) openEQ() {
	d := a.dspOf()
	gains := slices.Clone(d.EQ)
	if gains == nil {
		gains = make([]float64, len(engine.EQBands))
	}
	a.eqEdit = &eqEditor{gains: gains, preset: d.EQPreset}
}

// sendEQ manda al servicio lo del editor.
func (a *App) sendEQ() {
	e := a.eqEdit
	if e.preset != "custom" {
		a.send("eq", e.preset)
	} else {
		args := make([]string, len(e.gains))
		for i, g := range e.gains {
			args[i] = strconv.FormatFloat(g, 'f', -1, 64)
		}
		a.send("eq", args...)
	}
	// Se ve ya, sin esperar al servicio.
	a.status.DSP.EQ, a.status.DSP.EQPreset = slices.Clone(e.gains), e.preset
}

// nextPreset elige el preajuste siguiente (o anterior, con dir -1).
func (a *App) nextPreset(dir int) {
	e := a.eqEdit
	i := slices.IndexFunc(engine.EQPresets, func(p struct {
		Name  string
		Gains []float64
	}) bool {
		return p.Name == e.preset
	})
	n := len(engine.EQPresets)
	i = ((i+dir)%n + n) % n
	e.preset, e.gains = engine.EQPresets[i].Name, engine.PresetGains(engine.EQPresets[i].Name)
	a.sendEQ()
}

// setBand cambia la banda i a g dB (y el preajuste pasa a "a mano").
func (a *App) setBand(i int, g float64) {
	e := a.eqEdit
	g = math.Max(-12, math.Min(12, math.Round(g)))
	if e.gains[i] == g {
		return
	}
	e.gains[i], e.preset = g, "custom"
	a.sendEQ()
}

// updateEQ atiende los botones del editor: ←/→ la banda, ↑/↓ un dB, X (o
// L1/R1) el preajuste, Y todo a 0; A o B cierran (los cambios ya están).
func (a *App) updateEQ() {
	e, in := a.eqEdit, &a.input
	n := len(e.gains)
	switch {
	case in.Repeat(ActLeft):
		e.sel = (e.sel - 1 + n) % n
	case in.Repeat(ActRight):
		e.sel = (e.sel + 1) % n
	case in.Repeat(ActUp):
		a.setBand(e.sel, e.gains[e.sel]+1)
	case in.Repeat(ActDown):
		a.setBand(e.sel, e.gains[e.sel]-1)
	case in.Pressed(ActShuffle), in.Pressed(ActNext):
		a.nextPreset(1)
	case in.Pressed(ActPrev):
		a.nextPreset(-1)
	case in.Pressed(ActPause):
		e.preset, e.gains = "flat", engine.PresetGains("flat")
		a.sendEQ()
	case in.Pressed(ActConfirm), in.Pressed(ActBack), in.Pressed(ActMenu):
		a.eqEdit = nil
	}
}

// bandLabel es la frecuencia de una banda: "31", "1k", "16k".
func bandLabel(f float64) string {
	if f >= 1000 {
		return fmt.Sprintf("%gk", f/1000)
	}
	return fmt.Sprintf("%g", f)
}

// drawEQ pinta el editor: el preajuste arriba y las diez bandas como
// deslizadores verticales (de -12 a +12 dB), la elegida resaltada con su
// valor. Tocar una columna la elige y pone el valor de esa altura.
func (a *App) drawEQ(dst *ebiten.Image) {
	e := a.eqEdit
	if e == nil {
		return
	}
	th := a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	rect(dst, 0, 0, w, h, withAlpha(th.Shadow, 0xb0))
	pw, ph := min(w-2*margin, 340), min(h-topH-bottomH-16, 190)
	px, py := (w-pw)/2, topH+(h-topH-bottomH-ph)/2
	panel(dst, px, py, pw, ph, th.Bg2)
	rect(dst, px+1, py, pw-2, 1, th.Accent)
	drawIcon(dst, "eq", px+8, py+7, th.Accent)
	drawText(dst, T("set_eq"), SmallFace, px+20, py+8, th.Text)
	name := T("eq_" + e.preset)
	nw := textWidth(name, SmallFace) + 12
	panel(dst, px+pw-nw-8, py+5, nw, 11, mix(th.Bg2, th.Accent, 0.3))
	drawText(dst, name, SmallFace, px+pw-nw-2, py+8, th.Text)
	a.tapZone(px+pw-nw-8, py+5, nw, 11, func() { a.nextPreset(1) })

	n := len(e.gains)
	colW := (pw - 16) / n
	top, bot := py+34, py+ph-22
	zero := (top + bot) / 2
	// La línea de 0 dB.
	rect(dst, px+8, zero, colW*n, 1, withAlpha(th.Dim, 0x60))
	for i, g := range e.gains {
		cx := px + 8 + i*colW + colW/2
		c := th.Dim
		if i == e.sel {
			c = th.Accent
			panel(dst, cx-colW/2+1, top-10, colW-2, bot-top+24, withAlpha(th.Accent, 0x22))
			v := fmt.Sprintf("%+g", g)
			drawText(dst, v, SmallFace, cx-textWidth(v, SmallFace)/2, top-8, th.Text)
		}
		rect(dst, cx, top, 1, bot-top, withAlpha(th.Dim, 0x80))
		y := zero - int(math.Round(g/12*float64(zero-top)))
		// La barra desde 0 hasta el valor, y el pomo.
		if y < zero {
			rect(dst, cx-1, y, 3, zero-y, c)
		} else {
			rect(dst, cx-1, zero, 3, y-zero, c)
		}
		panel(dst, cx-3, y-2, 7, 5, th.Text)
		l := bandLabel(engine.EQBands[i])
		drawText(dst, l, SmallFace, cx-textWidth(l, SmallFace)/2, bot+6, c)
		i := i
		a.zone(cx-colW/2, top-10, colW, bot-top+24, func(_, yy int) {
			e.sel = i
			rel := float64(zero-(yy+top-10)) / float64(zero-top)
			a.setBand(i, rel*12)
		})
	}
	a.drawHints(dst, []hint{{"<>", T("h_band")}, {"^v", "dB"}, {"X", T("h_preset")}, {"Y", T("h_reset")}, {"A", T("h_ok")}})
}

// openScrobbleMenu es el menú de las cuentas donde contar lo que se escucha.
func (a *App) openScrobbleMenu() {
	s := &a.cfg.Scrobble
	m := &menu{title: T("set_scrobble"), subtitle: T("scrobble_sub")}
	lbValue := func() string { return orDefault(s.ListenBrainzUser, T("acct_none")) }
	m.items = append(m.items, menuItem{label: "ListenBrainz", icon: "user", value: lbValue,
		active: func() bool { return s.ListenBrainzToken != "" },
		do: func() {
			if s.ListenBrainzToken != "" {
				s.ListenBrainzToken, s.ListenBrainzUser = "", ""
				a.saveConfig()
				return
			}
			a.openRawPrompt(T("lb_token"), "", true, a.connectListenBrainz)
		}})
	if key, _ := a.lastFMKeys(); key != "" {
		m.items = append(m.items, menuItem{label: "Last.fm", icon: "user",
			value:  func() string { return orDefault(s.LastFMUser, T("acct_none")) },
			active: func() bool { return s.LastFMSession != "" },
			do: func() {
				if s.LastFMSession != "" {
					s.LastFMSession, s.LastFMUser = "", ""
					a.saveConfig()
					return
				}
				a.connectLastFM()
			}})
	} else {
		m.items = append(m.items, menuItem{label: "Last.fm", icon: "user", note: T("lfm_nokey"), do: func() {}})
	}
	if len(a.cfg.Servers) > 0 {
		m.items = append(m.items, menuItem{label: T("scrobble_server"), icon: "server",
			value:  func() string { return onOffLabel(!s.NoServer) },
			active: func() bool { return !s.NoServer },
			do:     func() { s.NoServer = !s.NoServer; a.saveConfig() }})
	}
	a.menu = m
}

// connectListenBrainz comprueba el token y lo guarda.
func (a *App) connectListenBrainz(token string) {
	if token == "" {
		return
	}
	a.flash(T("acct_checking"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		user, err := (&scrobble.ListenBrainz{Token: token}).ValidateToken(ctx)
		a.acctCh <- func() {
			if err != nil {
				a.flash(T("acct_error", err))
				return
			}
			a.cfg.Scrobble.ListenBrainzToken, a.cfg.Scrobble.ListenBrainzUser = token, user
			a.saveConfig()
			a.flash(T("acct_ok", "ListenBrainz", user))
		}
		ebiten.ScheduleFrame()
	}()
}

// lastFMKeys es la aplicación de Last.fm: la de audiofilus.json o la que
// trae la compilación.
func (a *App) lastFMKeys() (key, secret string) {
	if s := a.cfg.Scrobble; s.LastFMKey != "" {
		return s.LastFMKey, s.LastFMSecret
	}
	return scrobble.DefaultLastFMKey, scrobble.DefaultLastFMSecret
}

// connectLastFM inicia sesión en Last.fm: pide un token, muestra la página
// para autorizarlo como QR y pregunta cada pocos segundos si ya se
// autorizó (hasta cinco minutos, o hasta cerrar el QR).
func (a *App) connectLastFM() {
	key, secret := a.lastFMKeys()
	l := &scrobble.LastFM{Key: key, Secret: secret}
	a.flash(T("acct_checking"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		token, err := l.GetToken(ctx)
		if err != nil {
			a.acctCh <- func() { a.flash(T("acct_error", err)) }
			ebiten.ScheduleFrame()
			return
		}
		a.acctCh <- func() {
			a.screen, a.settingsQR, a.qrURL = Settings, true, l.AuthURL(token)
			a.flash(T("lfm_scan"))
		}
		ebiten.ScheduleFrame()
		for ctx.Err() == nil {
			time.Sleep(3 * time.Second)
			sk, user, err := l.GetSession(ctx, token)
			if err != nil {
				continue // aún sin autorizar
			}
			a.acctCh <- func() {
				a.cfg.Scrobble.LastFMSession, a.cfg.Scrobble.LastFMUser = sk, user
				a.saveConfig()
				a.settingsQR = false
				a.flash(T("acct_ok", "Last.fm", user))
			}
			ebiten.ScheduleFrame()
			return
		}
	}()
}

// updateAccounts aplica lo que devolvieron las comprobaciones de cuentas.
func (a *App) updateAccounts() {
	for {
		select {
		case f := <-a.acctCh:
			f()
		default:
			return
		}
	}
}
