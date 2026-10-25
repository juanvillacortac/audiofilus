package ui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/mdns"
)

// El servidor MPD (internal/mpd, en el servicio) en Ajustes → En línea: una
// baldosa que abre su menú, con el interruptor, la contraseña y la
// dirección a la que conectarse desde una app de MPD. Se guarda en
// audiofilus.json y el servicio lo vuelve a leer ("mpd apply").

// openMPDMenu abre el menú del servidor MPD.
func (a *App) openMPDMenu() {
	c := &a.cfg.MPD
	apply := func() {
		a.saveConfig()
		a.send("mpd", "apply")
	}
	m := &menu{title: T("set_mpd"), subtitle: T("mpd_sub")}
	m.items = append(m.items,
		menuItem{label: T("mpd_server"), icon: "server", value: func() string { return onOffLabel(c.On) },
			active: func() bool { return c.On },
			do:     func() { c.On = !c.On; apply() }},
		menuItem{label: T("mpd_password"), icon: "user",
			value: func() string {
				if c.Password == "" {
					return T("mpd_no_password")
				}
				return strings.Repeat("•", min(len([]rune(c.Password)), 8))
			},
			do: func() {
				a.openRawPrompt(T("mpd_password"), c.Password, true, func(s string) {
					c.Password = strings.TrimSpace(s)
					apply()
				})
			}},
		menuItem{label: T("mpd_address"), icon: "globe", value: func() string { return mpdAddress(c.MPDPort()) }, do: func() {}},
	)
	a.menu = m
}

// mpdAddress es la dirección del servidor en la red local ("192.168.1.5:6600").
func mpdAddress(port int) string {
	ips := mdns.LANIPs()
	if len(ips) == 0 {
		return fmt.Sprintf(":%d", port)
	}
	return net.JoinHostPort(ips[0].String(), strconv.Itoa(port))
}
