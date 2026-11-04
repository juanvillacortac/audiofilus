package ui

import (
	"strings"
	"testing"
)

// TestDefaultRoute comprueba que se reconoce la ruta por defecto en
// /proc/net/route y ipv6_route, y no las de una red local (Docker) ni las de
// lo.
func TestDefaultRoute(t *testing.T) {
	v4 := "Iface\tDestination\tGateway\tFlags\n" +
		"wlan0\t00000000\t0100A8C0\t0003\n" +
		"docker0\t000011AC\t00000000\t0001\n"
	if !hasLine(strings.NewReader(v4), isDefault4) {
		t.Errorf("IPv4 default route not found")
	}
	local := "Iface\tDestination\tGateway\tFlags\ndocker0\t000011AC\t00000000\t0001\n"
	if hasLine(strings.NewReader(local), isDefault4) {
		t.Errorf("a local-only route counted as a way out")
	}
	zero := strings.Repeat("0", 32)
	v6 := zero + " 00 " + zero + " 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003 wlan0\n" +
		zero + " 00 " + zero + " 00 " + zero + " ffffffff 00000001 00000000 00200200 lo\n"
	if !hasLine(strings.NewReader(v6), isDefault6) {
		t.Errorf("IPv6 default route not found")
	}
}
