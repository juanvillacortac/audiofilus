package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/cast"
)

// castFake es un altavoz UPnP de mentira: apunta las órdenes que recibe.
type castFake struct {
	mu      sync.Mutex
	actions []string
	uris    []string
	state   string
}

func (f *castFake) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPAction")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	out := ""
	switch action {
	case "SetAVTransportURI":
		f.uris = append(f.uris, string(b))
	case "Play":
		f.state = "PLAYING"
	case "Pause":
		f.state = "PAUSED_PLAYBACK"
	case "Stop":
		f.state = "STOPPED"
	case "GetTransportInfo":
		out = "<CurrentTransportState>" + f.state + "</CurrentTransportState>"
	case "GetPositionInfo":
		out = "<RelTime>0:00:01</RelTime><TrackDuration>0:00:02</TrackDuration>"
	case "SetNextAVTransportURI":
		w.WriteHeader(500)
		return
	}
	_, _ = io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+action+`Response>`+out+`</u:`+action+`Response></s:Body></s:Envelope>`)
}

// loaded son las canciones que recibió (los cuerpos de SetAVTransportURI).
func (f *castFake) loaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.uris...)
}

// count es cuántas veces recibió action.
func (f *castFake) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.actions {
		if a == action {
			n++
		}
	}
	return n
}

// waitFor espera (un rato) a que cond se cumpla, atendiendo a la interfaz.
func waitFor(t *testing.T, a *App, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		a.pollCast()
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Enviar a un aparato: empieza en la canción que suena, los mandos (pausa,
// siguiente, saltar) van al aparato y no al servicio, la pantalla muestra
// lo del aparato y dejar de enviar lo para.
func TestCastFlow(t *testing.T) {
	f := &castFake{state: "STOPPED"}
	ts := httptest.NewServer(http.HandlerFunc(f.handler))
	defer ts.Close()
	dev := cast.Device{Kind: cast.UPnP, Name: "Tele de prueba", Host: strings.TrimPrefix(ts.URL, "http://"),
		AVTransport: ts.URL + "/avt", Rendering: ts.URL + "/rc"}

	dir := filepath.Join("..", "decode", "testdata")
	lib := buildLibrary([]entry{
		{Path: filepath.Join(dir, "tone.mp3"), Title: "Uno", Artist: "Prueba", Album: "Tonos", Track: 1},
		{Path: filepath.Join(dir, "tone.flac"), Title: "Dos", Artist: "Prueba", Album: "Tonos", Track: 2},
	})
	a := &App{lib: lib, local: lib}
	a.playing, a.playingSong = lib.Albums[0], lib.Albums[0].Songs[0]

	a.castStart(dev)
	if !a.casting() {
		t.Fatal("not casting")
	}
	defer func() {
		if a.cast.srv != nil {
			a.cast.srv.Close()
		}
	}()
	waitFor(t, a, "first song", func() bool { return f.count("Play") > 0 })
	if u := f.loaded(); !strings.Contains(u[0], "Uno") {
		t.Errorf("first SetAVTransportURI: %s", u[0])
	}
	if a.castTitle("Reproduciendo") != "Reproduciendo → Tele de prueba" {
		t.Errorf("title = %q", a.castTitle("Reproduciendo"))
	}

	// Los mandos van al aparato.
	if !a.castSend("pause", nil) {
		t.Fatal("pause not intercepted")
	}
	waitFor(t, a, "pause", func() bool { return f.count("Pause") > 0 })
	a.castSend("seek", []string{"+1"})
	waitFor(t, a, "seek", func() bool { return f.count("Seek") > 0 })
	a.castSend("next", nil)
	waitFor(t, a, "second song", func() bool { return len(f.loaded()) >= 2 })
	if u := f.loaded(); !strings.Contains(u[len(u)-1], "Dos") {
		t.Errorf("next SetAVTransportURI: %s", u[len(u)-1])
	}
	waitFor(t, a, "display of the second song", func() bool { return a.playingSong.Title == "Dos" })
	if a.castSend("shuffle", []string{"on"}) {
		t.Error("shuffle intercepted; it belongs to the service")
	}

	a.castEnd(false)
	if a.casting() {
		t.Fatal("still casting")
	}
	waitFor(t, a, "stop", func() bool { return f.count("Stop") > 0 })
	if a.castSend("pause", nil) {
		t.Error("pause intercepted after stopping")
	}
}
