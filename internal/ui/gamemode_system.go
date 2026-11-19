package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/esdata"
	"github.com/juanvillacortac/audiofilus/internal/proc"
)

// El menú Sistema del modo juego (SELECT en el carrusel): Wi-Fi, Bluetooth
// y las opciones globales de emulación. Wi-Fi y Bluetooth se manejan con los
// mismos scripts que usa EmulationStation (knulli-wifi, knulli-bluetooth),
// en segundo plano: conectar a una red o emparejar un mando tarda segundos.
// Fuera de la consola no se ejecuta nada.

// Las vistas del menú Sistema.
const (
	sysMain = iota // Wi-Fi, Bluetooth, opciones globales, buscar juegos, reiniciar
	sysWifi        // las redes
	sysBT          // los dispositivos
)

// btDevice es un dispositivo Bluetooth emparejado.
type btDevice struct {
	addr, name string
	connected  bool
}

// gmSysState es el menú Sistema.
type gmSysState struct {
	view   int
	sel    int
	wifiOn bool
	btOn   bool
	ssid   string // la red a la que está conectada
	ip     string
	host   string // el nombre de la consola (host.local por mDNS)
	inet   bool   // llega a internet
	nets   []string
	devs   []btDevice
	busy   string // lo que se está haciendo (mientras, no se aceptan órdenes)
	status string // la última línea del emparejamiento
	// confirm es la fila (reiniciar o apagar) que espera la segunda A, desde
	// confirmAt.
	confirm   int
	confirmAt float64
	results   chan func()
}

// gmRun ejecuta un script de Knulli en segundo plano; done recibe su salida
// en el hilo de la interfaz. busy es lo que se muestra mientras.
func (a *App) gmRun(busy string, timeout time.Duration, done func(out string, err error), name string, args ...string) {
	g := &a.gm
	sy := &g.sys
	if !g.root.Live() {
		a.flash(T("gx_only_console"))
		return
	}
	if sy.results == nil {
		sy.results = make(chan func(), 8)
	}
	sy.busy = busy
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	go func() {
		defer cancel()
		var out []byte
		err := proc.NormalPriority(func() (err error) { // ver proc.NormalPriority
			out, err = proc.OwnSession(exec.CommandContext(ctx, name, args...)).CombinedOutput()
			return err
		})
		if err != nil {
			log.Printf("game mode: %s %v: %v: %s", name, args, err, strings.TrimSpace(string(out)))
		}
		sy.results <- func() {
			sy.busy = ""
			done(string(out), err)
		}
	}()
}

// gmSysStatusScript dice, una por línea: si el Wi-Fi está encendido, si lo
// está el Bluetooth, la red conectada, la IP, el nombre de la consola (con
// el que se anuncia por mDNS) y si llega a internet (knulli-internet-check,
// que prueba unos sitios conocidos).
const gmSysStatusScript = `knulli-settings-get wifi.enabled
knulli-settings-get controllers.bluetooth.enabled
connmanctl services 2>/dev/null | grep '^\*A' | sed 's/^...[ ]*//' | awk -F '[ ]*[ ]wifi_' '{print $1}' | head -1
ip -4 -o addr show "$(knulli-wifi get_interface)" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1
hostname
timeout 6 knulli-internet-check >/dev/null 2>&1 && echo 1 || echo 0`

// gmOpenSystem abre el menú Sistema.
func (a *App) gmOpenSystem() {
	g := &a.gm
	g.sys.view, g.sys.sel, g.sys.confirm = sysMain, 0, -1
	g.screen = gmSysMenu
	a.gmSysRefresh()
}

// gmSysRefresh relee el estado del Wi-Fi y del Bluetooth.
func (a *App) gmSysRefresh() {
	sy := &a.gm.sys
	if !a.gm.root.Live() {
		return
	}
	a.gmRun(T("gx_sys_reading"), 10*time.Second, func(out string, err error) {
		lines := strings.Split(out+strings.Repeat("\n", 6), "\n")
		sy.wifiOn = strings.TrimSpace(lines[0]) == "1"
		sy.btOn = strings.TrimSpace(lines[1]) == "1"
		sy.ssid, sy.ip = strings.TrimSpace(lines[2]), strings.TrimSpace(lines[3])
		sy.host = strings.ToLower(strings.TrimSpace(lines[4]))
		sy.inet = strings.TrimSpace(lines[5]) == "1"
	}, "sh", "-c", gmSysStatusScript)
}

// btLine es una línea de `knulli-bluetooth list`.
var btLine = regexp.MustCompile(`<device id="([^"]*)" name="([^"]*)" type="[^"]*" connected="([^"]*)"/>`)

// gmSysRows son las filas de la vista actual.
func (a *App) gmSysRows() []gmRow {
	sy := &a.gm.sys
	onOff := func(on bool) string {
		if on {
			return T("on")
		}
		return T("off")
	}
	switch sy.view {
	case sysWifi:
		rows := []gmRow{{main: T("gx_sys_scan")}}
		for _, n := range sy.nets {
			sub := ""
			if n == sy.ssid {
				sub = T("gx_sys_connected")
			}
			rows = append(rows, gmRow{main: n, sub: sub})
		}
		return rows
	case sysBT:
		rows := []gmRow{{main: T("gx_sys_pair")}}
		for _, d := range sy.devs {
			sub := ""
			if d.connected {
				sub = T("gx_sys_connected")
			}
			rows = append(rows, gmRow{main: d.name, sub: sub})
		}
		return rows
	}
	wifi := onOff(sy.wifiOn)
	if sy.wifiOn && sy.ssid != "" {
		wifi = sy.ssid
	}
	confirm := func(row int) string {
		if sy.confirm == row && a.t-sy.confirmAt < gmConfirmTime {
			return T("gx_sys_again")
		}
		return ""
	}
	return []gmRow{
		{main: "Wi-Fi", sub: wifi},
		{main: T("gx_sys_networks")},
		{main: "Bluetooth", sub: onOff(sy.btOn)},
		{main: T("gx_sys_devices")},
		{main: T("gx_global")},
		{main: T("gx_sys_rescan")},
		{main: T("gx_sys_restart_app")},
		{main: T("gx_sys_reboot"), sub: confirm(7)},
		{main: T("gx_sys_poweroff"), sub: confirm(8)},
	}
}

// updateGMSystem atiende el menú Sistema.
func (a *App) updateGMSystem() {
	g := &a.gm
	sy, in := &g.sys, &a.input
	n := len(a.gmSysRows())
	switch {
	case in.Repeat(ActUp):
		sy.sel = (sy.sel + n - 1) % n
		return
	case in.Repeat(ActDown):
		sy.sel = (sy.sel + 1) % n
		return
	case in.Pressed(ActBack):
		if sy.busy != "" && sy.view == sysBT {
			// Deja de buscar mandos como lo hace Knulli: con la marca que el
			// script mira cada medio segundo (así cierra su agente).
			if err := os.WriteFile("/var/run/bt_cancel", nil, 0o644); err == nil {
				time.AfterFunc(1500*time.Millisecond, func() { os.Remove("/var/run/bt_cancel") })
			}
			return
		}
		if sy.view == sysMain {
			g.screen = gmSystems
		} else {
			sy.view, sy.sel = sysMain, 0
		}
		return
	}
	if sy.busy != "" {
		return // una orden a la vez
	}
	switch sy.view {
	case sysMain:
		a.updateGMSysMain()
	case sysWifi:
		a.updateGMSysWifi()
	case sysBT:
		a.updateGMSysBT()
	}
}

// updateGMSysMain: las filas del menú principal.
func (a *App) updateGMSysMain() {
	g := &a.gm
	sy, in := &g.sys, &a.input
	if !in.Pressed(ActConfirm) && !in.Pressed(ActLeft) && !in.Pressed(ActRight) {
		return
	}
	if sy.sel != sy.confirm {
		sy.confirm = -1
	}
	toggle := !in.Pressed(ActConfirm) || sy.sel == 0 || sy.sel == 2
	switch {
	case sy.sel == 0 && toggle:
		cmd := "enable"
		if sy.wifiOn {
			cmd = "disable"
		}
		a.gmRun(T("gx_sys_working"), 30*time.Second, func(string, error) { a.gmSysRefresh() }, "knulli-wifi", cmd)
	case sy.sel == 1 && in.Pressed(ActConfirm):
		sy.view, sy.sel = sysWifi, 0
		a.gmSysScan()
	case sy.sel == 2 && toggle:
		cmd := "enable"
		if sy.btOn {
			cmd = "disable"
		}
		a.gmRun(T("gx_sys_working"), 30*time.Second, func(string, error) { a.gmSysRefresh() }, "knulli-bluetooth", cmd)
	case sy.sel == 3 && in.Pressed(ActConfirm):
		sy.view, sy.sel = sysBT, 0
		a.gmSysBTList()
	case sy.sel == 4 && in.Pressed(ActConfirm):
		a.gmOpenOptions(T("gx_global"), esdata.Scope{})
	case sy.sel == 5 && in.Pressed(ActConfirm):
		a.gmRescan()
	case sy.sel == 6 && in.Pressed(ActConfirm):
		a.gmRestartApp()
	case (sy.sel == 7 || sy.sel == 8) && in.Pressed(ActConfirm):
		// Reiniciar o apagar, con una segunda A para confirmar.
		if sy.confirm != sy.sel || a.t-sy.confirmAt > gmConfirmTime {
			sy.confirm, sy.confirmAt = sy.sel, a.t
			return
		}
		sy.confirm = -1
		a.gmPowerOff(sy.sel == 7)
	}
}

// gmConfirmTime es cuánto se espera la segunda A de reiniciar o apagar.
const gmConfirmTime = 3.0

// gmPowerOff reinicia o apaga la consola como lo hace ES (knulli-shutdown:
// apaga la pantalla y el sonido y sigue la secuencia de Knulli). Antes se
// guardan las opciones.
func (a *App) gmPowerOff(reboot bool) {
	g := &a.gm
	if !g.root.Live() {
		a.flash(T("gx_only_console"))
		return
	}
	a.gmSaveConf()
	flag := "-s"
	if reboot {
		flag = "-r"
	}
	log.Printf("game mode: knulli-shutdown %s", flag)
	// Por un sh que lo deja en segundo plano y termina: knulli-shutdown cierra
	// el "juego" que ve ES (Audiofilus, un port) y todos sus descendientes
	// (knulli-es-swissknife --emukill); siendo hijo de Audiofilus moría a
	// medias, con la pantalla ya apagada. Así queda colgando de init.
	cmd := exec.Command("sh", "-c", "knulli-shutdown "+flag+" >/dev/null 2>&1 &")
	detach(cmd)
	if err := cmd.Run(); err != nil {
		a.flash(err.Error())
	}
}

// gmSysScan busca redes.
func (a *App) gmSysScan() {
	sy := &a.gm.sys
	a.gmRun(T("gx_sys_scanning"), 20*time.Second, func(out string, err error) {
		sy.nets = sy.nets[:0]
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				sy.nets = append(sy.nets, l)
			}
		}
	}, "knulli-wifi", "scanlist")
}

// updateGMSysWifi: buscar redes o conectarse a una (pidiendo su clave).
func (a *App) updateGMSysWifi() {
	sy, in := &a.gm.sys, &a.input
	if !in.Pressed(ActConfirm) {
		return
	}
	if sy.sel == 0 {
		a.gmSysScan()
		return
	}
	ssid := sy.nets[sy.sel-1]
	a.openRawPrompt(fmt.Sprintf(T("gx_sys_key"), ssid), "", true, func(key string) {
		a.gmRun(fmt.Sprintf(T("gx_sys_joining"), ssid), 40*time.Second, func(_ string, err error) {
			if err != nil {
				a.flash(fmt.Sprintf(T("gx_sys_join_fail"), ssid))
			}
			a.gmSysRefresh()
		}, "knulli-wifi", "enable", ssid, key)
	})
}

// gmSysBTList lee los dispositivos emparejados.
func (a *App) gmSysBTList() {
	sy := &a.gm.sys
	a.gmRun(T("gx_sys_reading"), 15*time.Second, func(out string, err error) {
		sy.devs = sy.devs[:0]
		for _, m := range btLine.FindAllStringSubmatch(out, -1) {
			name := m[2]
			if name == "" {
				name = m[1]
			}
			sy.devs = append(sy.devs, btDevice{addr: m[1], name: name, connected: m[3] == "yes"})
		}
	}, "knulli-bluetooth", "list")
}

// updateGMSysBT: emparejar uno nuevo, conectar o desconectar (A) u olvidar
// (X) uno emparejado.
func (a *App) updateGMSysBT() {
	sy, in := &a.gm.sys, &a.input
	switch {
	case in.Pressed(ActConfirm) && sy.sel == 0:
		// Empareja lo que esté en modo emparejamiento (hasta un minuto).
		a.gmRun(T("gx_sys_pairing"), 70*time.Second, func(out string, err error) {
			if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) > 0 {
				sy.status = lines[len(lines)-1]
			}
			a.gmSysBTList()
		}, "knulli-bluetooth", "trust", "input")
	case in.Pressed(ActConfirm) && sy.sel > 0:
		d := sy.devs[sy.sel-1]
		cmd := "connect"
		if d.connected {
			cmd = "disconnect"
		}
		a.gmRun(T("gx_sys_working"), 20*time.Second, func(string, error) { a.gmSysBTList() }, "knulli-bluetooth", cmd, d.addr)
	case in.Pressed(ActShuffle) && sy.sel > 0:
		d := sy.devs[sy.sel-1]
		a.gmRun(T("gx_sys_working"), 20*time.Second, func(string, error) {
			sy.sel = 0
			a.gmSysBTList()
		}, "knulli-bluetooth", "remove", d.addr)
	}
}

// gmSysCollect atiende lo que terminó en segundo plano (en cada fotograma).
func (a *App) gmSysCollect() {
	sy := &a.gm.sys
	for {
		select {
		case f := <-sy.results:
			f()
			if n := len(a.gmSysRows()); sy.sel >= n {
				sy.sel = max(0, n-1)
			}
		default:
			return
		}
	}
}

// drawGMSystem: la lista de la vista, con lo que se está haciendo debajo.
func (a *App) drawGMSystem(dst *ebiten.Image) {
	g := &a.gm
	sy, th := &g.sys, a.theme
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	x, lw := margin+4, w-2*margin-12
	bottom := h - bottomH - gmMiniH - 4
	listH := bottom - topH - 22
	if sy.view != sysBT && g.root.Live() {
		listH -= 34 // el estado de la red, debajo
	}
	a.drawGMList(dst, a.gmSysRows(), sy.sel, x, topH+10, lw, listH)
	msg := sy.busy
	if msg == "" && sy.view == sysBT {
		msg = sy.status
	}
	if sy.view != sysBT && sy.busy == "" && g.root.Live() {
		// El estado de la red: si llega a internet, la IP y el nombre mDNS.
		y := bottom - 30
		state, col := T("gx_sys_offline"), th.Dim
		if sy.inet {
			state, col = T("gx_sys_online"), th.Accent
		}
		drawIcon(dst, "wifi", x, y-1, col)
		drawText(dst, state, SmallFace, x+10, y, col)
		if sy.ip != "" {
			drawText(dst, "IP  "+sy.ip, SmallFace, x, y+10, th.Text)
		}
		if sy.host != "" {
			drawText(dst, "mDNS  "+sy.host+".local", SmallFace, x, y+20, th.Text)
		}
		msg = ""
	}
	if msg != "" {
		if sy.busy != "" {
			a.drawHourglass(dst, x+6, bottom-6)
			drawTextW(dst, msg, SmallFace, x+16, bottom-9, lw-16, th.Accent)
		} else {
			drawTextW(dst, msg, SmallFace, x, bottom-9, lw, th.Dim)
		}
	}
	a.drawGMNowPlaying(dst)
	title := T("gx_sys")
	switch sy.view {
	case sysWifi:
		title = "Wi-Fi"
	case sysBT:
		title = "Bluetooth"
	}
	a.drawTopBar(dst, title)
	hints := []hint{{"^v", T("h_choose")}, {"A", T("h_open")}, {"B", T("h_back")}}
	switch {
	case sy.view == sysMain:
		hints = []hint{{"^v", T("h_choose")}, {"A", T("h_change")}, {"B", T("h_back")}}
	case sy.view == sysBT && sy.sel > 0:
		hints = []hint{{"^v", T("h_choose")}, {"A", T("gx_sys_connect")}, {"X", T("gx_sys_forget")}, {"B", T("h_back")}}
	}
	a.drawHints(dst, hints)
}

// wifiKickAfter es cuánto se espera al abrir antes de mirar si hay red.
const wifiKickAfter = 20 * time.Second

// kickWifi hace en Knulli lo que hace EmulationStation al arrancar: si el
// Wi-Fi está activado pero no hay red, se lo vuelve a pedir a connman, y si
// el Bluetooth lo está pero su adaptador no, se enciende
// (knulli-wifi enable). Al encender, el primer intento a veces falla (el
// router corta el saludo) y connman no reintenta; con ES de frontend no se
// nota porque ES lo pide al arrancar, pero con Audiofilus de frontend (el
// "juego al iniciar" de Knulli) la consola se quedaba sin red. Una vez por
// sesión.
func (a *App) kickWifi() {
	if a.wifiKicked || a.status.Platform != "knulli" || a.offline {
		return
	}
	a.wifiKicked = true
	go func() {
		time.Sleep(wifiKickAfter)
		if hasRoute() {
			return
		}
		out, err := exec.Command("knulli-settings-get", "wifi.enabled").Output()
		if err != nil || strings.TrimSpace(string(out)) != "1" {
			return
		}
		log.Printf("wifi: enabled but no network after %v: asking connman again", wifiKickAfter)
		if err := proc.NormalPriority(func() (err error) { // connmand, con prioridad normal
			out, err = proc.OwnSession(exec.Command("knulli-wifi", "enable")).CombinedOutput()
			return err
		}); err != nil {
			log.Printf("wifi: knulli-wifi enable: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}()
	// Lo mismo con el Bluetooth: activado en Knulli pero con el adaptador
	// apagado, se vuelve a encender como lo hace ES.
	go func() {
		time.Sleep(wifiKickAfter)
		out, err := exec.Command("knulli-settings-get", "controllers.bluetooth.enabled").Output()
		if err != nil || strings.TrimSpace(string(out)) != "1" {
			return
		}
		show, _ := exec.Command("bluetoothctl", "show").Output()
		if strings.Contains(string(show), "Powered: yes") {
			return
		}
		log.Printf("bluetooth: enabled but the adapter is off: starting it again")
		if err := proc.NormalPriority(func() (err error) { // bluetoothd, con prioridad normal
			out, err = proc.OwnSession(exec.Command("knulli-bluetooth", "enable")).CombinedOutput()
			return err
		}); err != nil {
			log.Printf("bluetooth: knulli-bluetooth enable: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}()
}

// gmIdleMargin es cuánto antes que el idlewatcher de Knulli se apaga la
// pantalla en el modo juego (ver gmIdleLimit).
const gmIdleMargin = 15

// gmIdleLimit es tras cuántos segundos sin tocar nada se apaga la pantalla
// en el modo juego (0: nunca). Antes que el idlewatcher de Knulli: con su
// ahorro "agresivo", a su tiempo congela (SIGSTOP) todo lo que corre con la
// prioridad de los juegos, y la interfaz la tiene (es un port): se quedaba
// oscura y congelada sin volver. El sueño de la app para el idlewatcher
// mientras dura, así que el suyo ya no llega. Si "apagar la pantalla" de
// Ajustes es antes, vale ese.
func (a *App) gmIdleLimit() float64 {
	limit := float64(a.cfg.UI.SleepAfter)
	if g := &a.gm; g.conf != nil && a.status.Platform == "knulli" {
		if mode := g.conf.Get("system.batterysaver.mode"); mode != "none" {
			t, err := strconv.Atoi(g.conf.Get("system.idlewatcher.timer"))
			if err != nil || t < 60 {
				t = 300 // el de Knulli (S96idlewatcher-daemon)
			}
			if k := float64(t - gmIdleMargin); limit <= 0 || k < limit {
				limit = k
			}
		}
	}
	return limit
}

// gmMaybeSleep apaga la pantalla (el sueño de la app) tras gmIdleLimit sin
// tocar nada; no mientras se ve un vídeo.
func (a *App) gmMaybeSleep() {
	limit := a.gmIdleLimit()
	if limit <= 0 || a.t-a.lastInput < limit || a.gm.info.video.cmd != nil || a.fadingOut {
		return
	}
	a.enterSleep()
}
