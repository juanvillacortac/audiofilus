package mdns

import (
	"context"
	"encoding/binary"
	"testing"
	"time"
)

// Una respuesta mDNS de un Chromecast (con un nombre comprimido) da su
// instancia, puerto, nombre, modelo y dirección.
func TestParseDNS(t *testing.T) {
	const svc = "_googlecast._tcp.local"
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[2:], 0x8400)
	binary.BigEndian.PutUint16(b[6:], 4)
	rr := func(name []byte, typ uint16, rdata []byte) {
		b = append(b, name...)
		b = binary.BigEndian.AppendUint16(b, typ)
		b = binary.BigEndian.AppendUint16(b, 1)
		b = binary.BigEndian.AppendUint32(b, 120)
		b = binary.BigEndian.AppendUint16(b, uint16(len(rdata)))
		b = append(b, rdata...)
	}
	svcOff := len(b)
	inst := appendName(nil, "Chromecast-abc."+svc)
	rr(appendName(nil, svc), dnsPTR, inst)
	// La instancia, comprimida: "Chromecast-abc" + puntero al servicio.
	ptr := []byte{0xc0 | byte(svcOff>>8), byte(svcOff)}
	instC := append([]byte{14}, "Chromecast-abc"...)
	instC = append(instC, ptr...)
	srv := []byte{0, 0, 0, 0, 0x1f, 0x49}
	srv = append(srv, appendName(nil, "abc.local")...)
	rr(instC, dnsSRV, srv)
	var txt []byte
	for _, kv := range []string{"id=abc", "fn=Tele del salón", "md=Chromecast"} {
		txt = append(txt, byte(len(kv)))
		txt = append(txt, kv...)
	}
	rr(instC, dnsTXT, txt)
	rr(appendName(nil, "abc.local"), dnsA, []byte{192, 168, 1, 30})
	recs, err := parseDNS(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[0].ptr != "Chromecast-abc."+svc || recs[1].name != "Chromecast-abc."+svc ||
		recs[1].port != 8009 || recs[1].target != "abc.local" || recs[2].txt["fn"] != "Tele del salón" || recs[3].ip.String() != "192.168.1.30" {
		t.Errorf("records = %+v", recs)
	}
}

// La respuesta del responder trae PTR, SRV, TXT y A, y se lee de vuelta;
// una pregunta se lee con su id.
func TestResponse(t *testing.T) {
	r := &Responder{instance: "audiofilus (pc)._mpd._tcp.local", service: "_mpd._tcp.local", target: "pc.local",
		port: 6600, txt: map[string]string{"app": "audiofilus"}}
	q := dnsQuery("_mpd._tcp.local", dnsPTR)
	q[0], q[1] = 0x12, 0x34
	id, qs, err := parseQuestions(q)
	if err != nil || id != 0x1234 || len(qs) != 1 || qs[0].name != "_mpd._tcp.local" || qs[0].typ != dnsPTR {
		t.Fatalf("questions: %x %+v %v", id, qs, err)
	}
	recs, err := parseDNS(r.response(id, qs, nil, 1))
	if err != nil || len(recs) < 3 {
		t.Fatalf("records: %+v %v", recs, err)
	}
	if recs[0].ptr != r.instance || recs[1].port != 6600 || recs[1].target != "pc.local" || recs[2].txt["app"] != "audiofilus" {
		t.Errorf("records = %+v", recs)
	}
}

// Dos procesos (aquí, dos responders) se anuncian a la vez en el mismo
// equipo y Browse encuentra los dos.
func TestAnnounceBrowse(t *testing.T) {
	if len(lanIPs()) == 0 {
		t.Skip("no multicast network interface")
	}
	a, err := Announce("test A", "_audiofilus-test._tcp.local", 16601, map[string]string{"k": "a"})
	if err != nil {
		t.Skipf("can't listen on 5353: %v", err)
	}
	defer a.Close()
	b, err := Announce("test B", "_audiofilus-test._tcp.local", 16602, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	found, err := Browse(ctx, "_audiofilus-test._tcp.local")
	if err != nil {
		t.Fatal(err)
	}
	ports := map[string]int{}
	for _, in := range found {
		ports[in.Name] = in.Port
	}
	if ports["test A"] != 16601 || ports["test B"] != 16602 {
		t.Errorf("found %+v", found)
	}
}
