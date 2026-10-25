package cast

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// La descripción de un aparato UPnP: el MediaRenderer dentro de otro, con
// URL de control relativas.
const descFixture = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <device>
  <deviceType>urn:schemas-upnp-org:device:Basic:1</deviceType>
  <friendlyName>Root</friendlyName>
  <deviceList><device>
   <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
   <friendlyName>Sala</friendlyName>
   <modelName>Altavoz 3000</modelName>
   <UDN>uuid:1234</UDN>
   <serviceList>
    <service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/upnp/control/avt</controlURL></service>
    <service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>rc</controlURL></service>
    <service><serviceType>urn:schemas-upnp-org:service:ConnectionManager:1</serviceType><controlURL>/cm</controlURL></service>
   </serviceList>
  </device></deviceList>
 </device>
</root>`

// Se encuentra el renderer anidado y sus URL de control quedan absolutas.
func TestParseDescription(t *testing.T) {
	d, err := parseDescription([]byte(descFixture), "http://192.168.1.20:49152/desc/root.xml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Sala" || d.Model != "Altavoz 3000" || d.ID != "uuid:1234" || d.Host != "192.168.1.20:49152" {
		t.Errorf("device = %+v", d)
	}
	if d.AVTransport != "http://192.168.1.20:49152/upnp/control/avt" || d.Rendering != "http://192.168.1.20:49152/desc/rc" {
		t.Errorf("control URLs: %q %q", d.AVTransport, d.Rendering)
	}
}

// De una respuesta SSDP sale el LOCATION (y se ignora lo que no es renderer).
func TestSSDPLocation(t *testing.T) {
	resp := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nLOCATION: http://10.0.0.5:1400/xml/device.xml\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
	if got := ssdpLocation([]byte(resp)); got != "http://10.0.0.5:1400/xml/device.xml" {
		t.Errorf("location = %q", got)
	}
	other := strings.Replace(resp, "MediaRenderer", "MediaServer", 1)
	if got := ssdpLocation([]byte(other)); got != "" {
		t.Errorf("media server accepted: %q", got)
	}
}

// fakeRenderer es un MediaRenderer de mentira: apunta las órdenes SOAP.
type fakeRenderer struct {
	mu      sync.Mutex
	actions []string
	bodies  map[string]string
	state   string
	pos     string
	vol     int
}

func (f *fakeRenderer) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPAction")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	f.bodies[action] = string(b)
	var out string
	switch action {
	case "Play":
		f.state = "PLAYING"
	case "Pause":
		f.state = "PAUSED_PLAYBACK"
	case "Stop":
		f.state = "STOPPED"
	case "SetVolume":
		f.vol = 42
	case "GetTransportInfo":
		out = "<CurrentTransportState>" + f.state + "</CurrentTransportState>"
	case "GetPositionInfo":
		out = "<RelTime>" + f.pos + "</RelTime><TrackDuration>0:03:20</TrackDuration>"
	case "GetVolume":
		out = "<CurrentVolume>" + strconv.Itoa(f.vol) + "</CurrentVolume>"
	case "SetNextAVTransportURI":
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `<s:Envelope><s:Body><s:Fault><detail><UPnPError><errorCode>401</errorCode></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
		return
	}
	_, _ = io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+action+`Response>`+out+`</u:`+action+`Response></s:Body></s:Envelope>`)
}

// Mandar una canción a un aparato UPnP: carga con su DIDL (título y tipo),
// toca, lee estado y volumen, y SetNext que el aparato no tiene se recuerda.
func TestUPnPSession(t *testing.T) {
	f := &fakeRenderer{bodies: map[string]string{}, state: "STOPPED", pos: "0:01:05", vol: 30}
	ts := httptest.NewServer(http.HandlerFunc(f.handler))
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	d := Device{Kind: UPnP, Name: "Fake", Host: host, AVTransport: ts.URL + "/avt", Rendering: ts.URL + "/rc"}
	srv, err := NewServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	s, _ := NewSession(srv, d)
	ctx := context.Background()
	song := filepath.Join("..", "decode", "testdata", "tone.mp3")
	if err := s.Cast(ctx, Item{Path: song, Title: "Prueba & tono", Artist: "audiofilus", Duration: 2}, 0); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	body := f.bodies["SetAVTransportURI"]
	f.mu.Unlock()
	if !strings.Contains(body, "Prueba &amp;amp; tono") || !strings.Contains(body, "audio/mpeg") || !strings.Contains(body, "/m/") {
		t.Errorf("SetAVTransportURI body: %s", body)
	}
	if err := s.SetVolume(ctx, 0.42); err != nil {
		t.Fatal(err)
	}
	st, err := s.p.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != Playing || st.Position != 65 || st.Duration != 200 || st.Volume != 0.42 {
		t.Errorf("status = %+v", st)
	}
	// Si después se queda sin nada puesto, dejó de recibir.
	f.mu.Lock()
	f.state = "NO_MEDIA_PRESENT"
	f.mu.Unlock()
	if _, err := s.p.Status(ctx); !errors.Is(err, ErrReleased) {
		t.Errorf("status with no media after loading: %v, want ErrReleased", err)
	}
	_ = s.Queue(ctx, Item{Path: song, Title: "Otra"})
	if !s.p.(*upnpPlayer).noNext {
		t.Errorf("SetNextAVTransportURI failure not remembered")
	}
	_ = s.Close(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actions[len(f.actions)-1] != "Stop" {
		t.Errorf("close didn't stop: %v", f.actions)
	}
}

// Un CastMessage se escribe y se lee igual.
func TestCastMessageRoundTrip(t *testing.T) {
	m := castMsg{Source: "sender-0", Dest: "receiver-0", Namespace: nsReceiver, Payload: `{"type":"LAUNCH"}`}
	got, err := decodeCastMessage(encodeCastMessage(m))
	if err != nil || got != m {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}

// selfSigned es un certificado de mentira para el Chromecast de los tests.
func selfSigned(t *testing.T) tls.Certificate {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cast"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeCast es un Chromecast de mentira: lanza la app, carga y toca.
func fakeCast(t *testing.T, ln net.Listener, got chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	state := "IDLE"
	reply := func(m castMsg, ns string, pl map[string]any) {
		b, _ := json.Marshal(pl)
		_ = writeFrame(conn, castMsg{Source: m.Dest, Dest: m.Source, Namespace: ns, Payload: string(b)})
	}
	media := func(id float64) map[string]any {
		return map[string]any{"type": "MEDIA_STATUS", "requestId": id, "status": []any{map[string]any{
			"mediaSessionId": 7, "playerState": state, "currentTime": 12.5, "media": map[string]any{"duration": 200.0}}}}
	}
	for {
		m, err := readFrame(conn)
		if err != nil {
			return
		}
		var pl map[string]any
		_ = json.Unmarshal([]byte(m.Payload), &pl)
		id, _ := pl["requestId"].(float64)
		typ, _ := pl["type"].(string)
		got <- typ
		switch typ {
		case "LAUNCH":
			reply(m, nsReceiver, map[string]any{"type": "RECEIVER_STATUS", "requestId": id, "status": map[string]any{
				"applications": []any{map[string]any{"appId": defaultMediaReceiver, "transportId": "web-1"}},
				"volume":       map[string]any{"level": 0.3}}})
		case "LOAD":
			if m.Dest != "web-1" {
				t.Errorf("LOAD sent to %q", m.Dest)
			}
			state = "PLAYING"
			reply(m, nsMedia, media(id))
		case "PAUSE":
			state = "PAUSED"
			reply(m, nsMedia, media(id))
		case "GET_STATUS", "PLAY", "SEEK":
			reply(m, nsMedia, media(id))
		}
	}
}

// Chromecast: lanza el receptor, carga la canción en su transporte, pausa y
// lee el estado.
func TestCastPlayer(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 64)
	go fakeCast(t, ln, got)
	p := newCastPlayer(Device{Kind: Chromecast, Host: ln.Addr().String()})
	ctx := context.Background()
	if err := p.Load(ctx, media{URL: "http://x/m/a.mp3", MIME: "audio/mpeg", Title: "T"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := p.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != Paused || st.Position != 12.5 || st.Duration != 200 || st.Volume != 0.3 {
		t.Errorf("status = %+v", st)
	}
	_ = p.Close()
	var seen []string
	for len(got) > 0 {
		seen = append(seen, <-got)
	}
	want := []string{"CONNECT", "LAUNCH", "CONNECT", "LOAD", "PAUSE", "GET_STATUS"}
	if strings.Join(seen[:len(want)], ",") != strings.Join(want, ",") {
		t.Errorf("messages = %v", seen)
	}
}

// Un archivo que el aparato sabe tocar se sirve tal cual, con Range.
func TestServeDirectRange(t *testing.T) {
	srv, err := NewServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	song := filepath.Join("..", "decode", "testdata", "tone.mp3")
	want, _ := os.ReadFile(song)
	m, err := srv.URLFor(Device{Host: "127.0.0.1:9"}, Item{Path: song}, false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", m.URL, nil)
	req.Header.Set("Range", "bytes=100-199")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(b) != string(want[100:200]) || resp.Header.Get("Content-Type") != "audio/mpeg" {
		t.Errorf("range: %s %d bytes %q", resp.Status, len(b), resp.Header.Get("Content-Type"))
	}
	// Lo no publicado no se sirve.
	if r, _ := http.Get(strings.Replace(m.URL, "/m/", "/m/x", 1)); r.StatusCode != http.StatusNotFound {
		t.Errorf("unpublished file served: %s", r.Status)
	}
}

// Lo que se convierte sale como un WAV de tamaño exacto, y un Range a mitad
// da los bytes que tocan (los mismos que en la descarga entera).
func TestServeTranscodeWAV(t *testing.T) {
	srv, err := NewServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	song := filepath.Join("..", "decode", "testdata", "tone.flac") // 2 s a 44,1 kHz
	m, err := srv.URLFor(Device{Host: "127.0.0.1:9"}, Item{Path: song}, true)
	if err != nil || m.MIME != "audio/wav" || !strings.HasSuffix(m.URL, ".wav") {
		t.Fatalf("media = %+v %v", m, err)
	}
	resp, err := http.Get(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	frames := int64(2 * 44100)
	if int64(len(full)) != 44+frames*4 || resp.Header.Get("Content-Length") != strconv.FormatInt(44+frames*4, 10) {
		t.Fatalf("size %d, content-length %s", len(full), resp.Header.Get("Content-Length"))
	}
	if string(full[:4]) != "RIFF" || string(full[8:16]) != "WAVEfmt " || binary.LittleEndian.Uint32(full[24:]) != 44100 ||
		binary.LittleEndian.Uint32(full[40:]) != uint32(frames*4) || binary.LittleEndian.Uint16(full[34:]) != 16 {
		t.Errorf("bad WAV header % x", full[:44])
	}
	req, _ := http.NewRequest("GET", m.URL, nil)
	req.Header.Set("Range", "bytes=40002-40101") // a mitad de un frame
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || len(part) != 100 {
		t.Fatalf("range: %s, %d bytes", resp.Status, len(part))
	}
	if string(part) != string(full[40002:40102]) {
		t.Errorf("range bytes differ from the full download")
	}
}

// parseRange: rangos normales, abiertos, los últimos bytes y los malos.
func TestParseRange(t *testing.T) {
	for _, c := range []struct {
		h          string
		start, end int64
		ok         bool
	}{
		{"", 0, 999, true}, {"bytes=0-99", 0, 99, true}, {"bytes=500-", 500, 999, true},
		{"bytes=-100", 900, 999, true}, {"bytes=900-5000", 900, 999, true},
		{"bytes=1000-", 0, 0, false}, {"bytes=5-2", 0, 0, false}, {"bytes=0-1,5-6", 0, 0, false},
	} {
		s, e, ok := parseRange(c.h, 1000)
		if ok != c.ok || ok && (s != c.start || e != c.end) {
			t.Errorf("parseRange(%q) = %d %d %v", c.h, s, e, ok)
		}
	}
}

// hms y parseHMS van y vuelven.
func TestHMS(t *testing.T) {
	if hms(3725) != "1:02:05" || parseHMS("1:02:05.500") != 3725.5 || parseHMS("NOT_IMPLEMENTED") != 0 {
		t.Errorf("hms %s / %v", hms(3725), parseHMS("1:02:05.500"))
	}
}

// stubPlayer es un aparato de mentira para la lógica de la sesión.
type stubPlayer struct {
	mu     sync.Mutex
	loaded []string
}

func (p *stubPlayer) Load(_ context.Context, m media, _ float64) error {
	p.mu.Lock()
	p.loaded = append(p.loaded, m.Title)
	p.mu.Unlock()
	return nil
}
func (p *stubPlayer) Play(context.Context) error               { return nil }
func (p *stubPlayer) Pause(context.Context) error              { return nil }
func (p *stubPlayer) Stop(context.Context) error               { return nil }
func (p *stubPlayer) Seek(context.Context, float64) error      { return nil }
func (p *stubPlayer) SetVolume(context.Context, float64) error { return nil }
func (p *stubPlayer) SetNext(context.Context, media) error     { return ErrUnsupported }
func (p *stubPlayer) Status(context.Context) (Status, error)   { return Status{}, nil }
func (p *stubPlayer) Close() error                             { return nil }

// Al acabar la canción, la sesión pone la siguiente (Advanced) y, sin
// siguiente, avisa Finished.
func TestSessionAdvance(t *testing.T) {
	srv, _ := NewServer("127.0.0.1:0")
	defer srv.Close()
	p := &stubPlayer{}
	s := &Session{srv: srv, dev: Device{Host: "127.0.0.1:9"}, p: p, events: make(chan Event, 16), quit: make(chan struct{})}
	ctx := context.Background()
	song := filepath.Join("..", "decode", "testdata", "tone.mp3")
	_ = s.Cast(ctx, Item{Path: song, Title: "uno", Duration: 100}, 0)
	_ = s.Queue(ctx, Item{Path: song, Title: "dos", Duration: 100})
	s.loadedAt = time.Now().Add(-time.Minute)
	s.step(Status{State: Playing, Position: 98, Duration: 100})
	s.step(Status{State: Stopped, Position: 0, Duration: 100})
	var kinds []EventKind
	for len(s.events) > 0 {
		e := <-s.events
		kinds = append(kinds, e.Kind)
		if e.Kind == Advanced && e.Item.Title != "dos" {
			t.Errorf("advanced to %q", e.Item.Title)
		}
	}
	if s.Current().Title != "dos" || len(p.loaded) != 2 {
		t.Fatalf("current %q, loaded %v, events %v", s.Current().Title, p.loaded, kinds)
	}
	s.loadedAt = time.Now().Add(-time.Minute)
	s.step(Status{State: Playing, Position: 99, Duration: 100})
	s.step(Status{State: Stopped, Duration: 100})
	finished := false
	for len(s.events) > 0 {
		if (<-s.events).Kind == Finished {
			finished = true
		}
	}
	if !finished {
		t.Errorf("no Finished event at the end")
	}
}

// Una canción como portada (la del álbum es la incrustada) se sirve como su
// imagen: la saca de las etiquetas.
func TestServeEmbeddedCover(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	frame := append([]byte("\x00image/png\x00\x03\x00"), png...)
	var id3 bytes.Buffer
	id3.WriteString("ID3\x03\x00\x00")
	size := 10 + len(frame)
	id3.Write([]byte{byte(size >> 21 & 0x7f), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)})
	id3.WriteString("APIC")
	_ = binary.Write(&id3, binary.BigEndian, uint32(len(frame)))
	id3.Write([]byte{0, 0})
	id3.Write(frame)
	tone, _ := os.ReadFile(filepath.Join("..", "decode", "testdata", "tone.mp3"))
	id3.Write(tone)
	song := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(song, id3.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	m, err := srv.URLFor(Device{Host: "127.0.0.1:9"}, Item{Path: song, Cover: song}, false)
	if err != nil || !strings.Contains(m.Cover, "/c/") {
		t.Fatalf("cover url %q: %v", m.Cover, err)
	}
	resp, err := http.Get(m.Cover)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(b, png) || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("embedded cover: %q %q", b, resp.Header.Get("Content-Type"))
	}
}
