package ui

import (
	"bufio"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// El icono de conexión de la barra de arriba: si hay salida a la red (una
// ruta por defecto, o en sistemas sin /proc una interfaz con dirección
// global). Se mira cada pocos segundos en segundo plano, sin conectarse a
// ningún sitio.

// onlineEvery es cada cuánto se mira la conexión.
const onlineEvery = 5 * time.Second

// watchOnline mira la conexión mientras dure la interfaz.
func (a *App) watchOnline() {
	go func() {
		for {
			a.online.Store(hasRoute())
			time.Sleep(onlineEvery)
		}
	}()
}

// hasRoute dice si hay salida a la red. En Linux, una ruta por defecto
// (/proc/net/route o ipv6_route): las interfaces de Docker o de una VPN
// caída tienen dirección pero no son salida. Donde no hay /proc, una
// interfaz levantada con una dirección que no sea local.
func hasRoute() bool {
	if ok, known := procDefaultRoute(); known {
		return ok
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, ad := range addrs {
			n, ok := ad.(*net.IPNet)
			if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() {
				continue
			}
			// IPv4 de la red de casa vale; IPv6, solo global (no la local).
			if n.IP.To4() != nil || !n.IP.IsPrivate() {
				return true
			}
		}
	}
	return false
}

// procDefaultRoute busca una ruta por defecto en /proc; known es false si
// no hay /proc (no es Linux).
func procDefaultRoute() (ok, known bool) {
	for _, t := range []struct {
		path string
		is   func([]string) bool
	}{{"/proc/net/route", isDefault4}, {"/proc/net/ipv6_route", isDefault6}} {
		f, err := os.Open(t.path)
		if err != nil {
			continue
		}
		known = true
		found := hasLine(f, t.is)
		f.Close()
		if found {
			return true, true
		}
	}
	return false, known
}

// hasLine dice si alguna línea de r (en campos) cumple is.
func hasLine(r io.Reader, is func([]string) bool) bool {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if is(strings.Fields(sc.Text())) {
			return true
		}
	}
	return false
}

// isDefault4 dice si una línea de /proc/net/route es la ruta por defecto:
// interfaz, destino 00000000...
func isDefault4(fs []string) bool {
	return len(fs) > 2 && fs[1] == "00000000" && fs[0] != "lo"
}

// isDefault6 dice si una línea de /proc/net/ipv6_route es la ruta por
// defecto: destino ::/0 con una puerta que no es ::, y no por lo.
func isDefault6(fs []string) bool {
	zero := strings.Repeat("0", 32)
	return len(fs) == 10 && fs[0] == zero && fs[1] == "00" && fs[4] != zero && fs[9] != "lo"
}
