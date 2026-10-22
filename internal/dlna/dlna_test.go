package dlna

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeWAV escribe un WAV de 16 bits estéreo con un tono de frames frames.
func writeWAV(t *testing.T, path string, rate, frames int) {
	t.Helper()
	var b bytes.Buffer
	b.Write(wavHeader(rate, int64(frames)))
	for i := 0; i < frames; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/float64(rate)) * 8000)
		_ = binary.Write(&b, binary.LittleEndian, v)
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testLibrary arma una biblioteca: data/library.json con dos álbumes, un
// título con caracteres que hay que escapar y una lista.
func testLibrary(t *testing.T) (dataDir, music string) {
	t.Helper()
	root := t.TempDir()
	dataDir, music = filepath.Join(root, "data"), filepath.Join(root, "Music")
	os.MkdirAll(filepath.Join(music, "Band", "First"), 0o755)
	os.MkdirAll(filepath.Join(dataDir, "playlists"), 0o755)
	a := filepath.Join(music, "Band", "First", "01.wav")
	b := filepath.Join(music, "Band", "First", "02.wav")
	c := filepath.Join(music, "solo.wav")
	for _, p := range []string{a, b, c} {
		writeWAV(t, p, 8000, 8000)
	}
	os.WriteFile(filepath.Join(music, "Band", "First", "cover.jpg"), []byte("\xff\xd8\xff fake jpeg"), 0o644)
	entries := []cacheEntry{
		{Path: a, Title: "Rock & <Roll>", Artist: "Band", Album: "First", Track: 1, Size: 32044},
		{Path: b, Title: "Second", Artist: "Band", Album: "First", Track: 2, Size: 32044},
		{Path: c, Title: "", Artist: "Solo", Album: "Alone"},
		{Path: "subsonic://srv/song/1", Title: "Remote"},
	}
	data, _ := json.Marshal(map[string]any{"dirs": []string{music}, "entries": entries})
	os.WriteFile(filepath.Join(dataDir, "library.json"), data, 0o644)
	os.WriteFile(filepath.Join(dataDir, "playlists", "Mix.m3u"), []byte("#EXTM3U\n"+c+"\n"+a+"\n"), 0o644)
	return dataDir, music
}

// soap manda una acción y devuelve el cuerpo de la respuesta.
func soap(t *testing.T, url, service, action string, args map[string]string) (int, string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%s xmlns:u="%s">`, action, service)
	for k, v := range args {
		fmt.Fprintf(&b, "<%s>%s</%s>", k, xmlEscape(v), k)
	}
	fmt.Fprintf(&b, "</u:%s></s:Body></s:Envelope>", action)
	req, _ := http.NewRequest("POST", url, strings.NewReader(b.String()))
	req.Header.Set("SOAPACTION", `"`+service+"#"+action+`"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// testService es un Service sin red (sin SSDP): solo su HTTP.
func testService(t *testing.T, dataDir string, p Player) (*Service, *httptest.Server) {
	t.Helper()
	s := &Service{opts: Options{Name: "test & co", Server: true, Renderer: p != nil, DataDir: dataDir, Player: p}}
	s.serverID, s.rendID = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	s.fetch = newFetcher(filepath.Join(dataDir, "cache", "dlna"))
	s.media = &mediaServer{src: &libSource{dataDir: dataDir}}
	if p != nil {
		s.rend = newRenderer(p, s.fetch)
	}
	srv := httptest.NewServer(s.mux())
	t.Cleanup(srv.Close)
	return s, srv
}

// unescape deshace el escape del Result de un Browse.
func unescape(s string) string {
	r := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&#34;", `"`, "&quot;", `"`, "&#39;", "'", "&amp;", "&")
	return r.Replace(s)
}

// La raíz tiene los cinco contenedores; Artistas → Álbumes → Canciones va en
// orden de pista; lo remoto no se sirve; los títulos van escapados.
func TestBrowse(t *testing.T) {
	dataDir, _ := testLibrary(t)
	s, srv := testService(t, dataDir, nil)
	ctl := srv.URL + "/server/cd/control"
	code, body := soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": "0", "BrowseFlag": "BrowseDirectChildren", "StartingIndex": "0", "RequestedCount": "0"})
	if code != 200 || !strings.Contains(body, "<TotalMatches>5</TotalMatches>") {
		t.Fatalf("root: %d %s", code, body)
	}
	l := s.media.src.get()
	if len(l.songs) != 3 {
		t.Fatalf("songs = %d, want 3 (remote skipped)", len(l.songs))
	}
	var band *artistEntry
	for _, a := range l.artists {
		if a.name == "Band" {
			band = a
		}
	}
	if band == nil || len(band.albums) != 1 {
		t.Fatalf("artists = %+v", l.artists)
	}
	_, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": band.albums[0].id, "BrowseFlag": "BrowseDirectChildren", "StartingIndex": "0", "RequestedCount": "10"})
	res := unescape(body)
	if i, j := strings.Index(res, "Rock &amp; &lt;Roll&gt;"), strings.Index(res, "Second"); i < 0 || j < 0 || i > j {
		t.Fatalf("album children (escaped title, track order): %s", res)
	}
	if !strings.Contains(res, "/media/") || !strings.Contains(res, "protocolInfo=\"http-get:*:audio/wav:") {
		t.Errorf("no resource: %s", res)
	}
	if !strings.Contains(res, "/art/") {
		t.Errorf("no album art for an album with cover.jpg: %s", res)
	}
	// Paginado.
	_, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": "songs", "BrowseFlag": "BrowseDirectChildren", "StartingIndex": "1", "RequestedCount": "1"})
	if !strings.Contains(body, "<NumberReturned>1</NumberReturned>") || !strings.Contains(body, "<TotalMatches>3</TotalMatches>") {
		t.Errorf("paging: %s", body)
	}
	// La lista y las carpetas.
	_, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": "playlists", "BrowseFlag": "BrowseDirectChildren"})
	if !strings.Contains(unescape(body), "Mix") {
		t.Errorf("playlists: %s", body)
	}
	_, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": l.folders[0].id, "BrowseFlag": "BrowseDirectChildren"})
	if r := unescape(body); !strings.Contains(r, ">Band<") || !strings.Contains(r, ">solo<") {
		t.Errorf("folders (subfolder and file named after itself): %s", r)
	}
	// Metadatos y un objeto que no existe.
	_, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": l.songs[0].id, "BrowseFlag": "BrowseMetadata"})
	if !strings.Contains(body, "<NumberReturned>1</NumberReturned>") {
		t.Errorf("metadata: %s", body)
	}
	code, body = soap(t, ctl, svcCD, "Browse", map[string]string{"ObjectID": "nope", "BrowseFlag": "BrowseMetadata"})
	if code != 500 || !strings.Contains(body, "<errorCode>701</errorCode>") {
		t.Errorf("missing object: %d %s", code, body)
	}
	// Buscar.
	_, body = soap(t, ctl, svcCD, "Search", map[string]string{"ContainerID": "0", "SearchCriteria": `upnp:class derivedfrom "object.item.audioItem" and dc:title contains "second"`})
	if !strings.Contains(body, "<TotalMatches>1</TotalMatches>") {
		t.Errorf("search: %s", body)
	}
}

// Las canciones se sirven con Range y las cabeceras de DLNA; la portada
// suelta de la carpeta también.
func TestServeMediaRange(t *testing.T) {
	dataDir, _ := testLibrary(t)
	s, srv := testService(t, dataDir, nil)
	song := s.media.src.get().albums[1].songs[0] // las de "First" (por título: Alone, First)
	// Un .wav se sirve tal cual (no se decodifica).
	req, _ := http.NewRequest("GET", srv.URL+"/media/"+song.id+".wav", nil)
	req.Header.Set("Range", "bytes=40-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	file, _ := os.ReadFile(song.path)
	if resp.StatusCode != 206 || !bytes.Equal(body, file[40:]) {
		t.Fatalf("range: %d, %d bytes", resp.StatusCode, len(body))
	}
	if resp.Header.Get("transferMode.dlna.org") != "Streaming" || !strings.Contains(resp.Header.Get("contentFeatures.dlna.org"), "DLNA.ORG_OP=01") {
		t.Errorf("dlna headers: %v", resp.Header)
	}
	resp, err = http.Get(srv.URL + "/art/" + song.id)
	if err != nil {
		t.Fatal(err)
	}
	art, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.HasPrefix(art, []byte("\xff\xd8\xff")) {
		t.Errorf("art: %d %q", resp.StatusCode, art)
	}
}

// Lo que se decodifica sale como WAV de 16 bits, entero o desde un Range.
func TestServeWAVTranscode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.wav")
	writeWAV(t, p, 8000, 4000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveWAV(w, r, p) }))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	whole, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(whole) != 44+4000*4 || string(whole[:4]) != "RIFF" || resp.Header.Get("Content-Length") != fmt.Sprint(44+4000*4) {
		t.Fatalf("whole: %d bytes, %v", len(whole), resp.Header)
	}
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=1046-")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(part, whole[1046:]) {
		t.Fatalf("range: %d, %d bytes (want %d)", resp.StatusCode, len(part), len(whole)-1046)
	}
}

// Los anuncios SSDP llevan lo que piden los clientes.
func TestSSDPMessages(t *testing.T) {
	d := ssdpDevice{uuid: "abc", devType: typeRenderer, services: []string{svcAVT}, desc: "/renderer/desc.xml"}
	ts := d.targets()
	if len(ts) != 4 || ts[0][0] != "upnp:rootdevice" || ts[0][1] != "uuid:abc::upnp:rootdevice" || ts[1][1] != "uuid:abc" || ts[3][1] != "uuid:abc::"+svcAVT {
		t.Fatalf("targets %v", ts)
	}
	msg := ssdpResponse(ts[2][0], ts[2][1], "http://10.0.0.2:1234/renderer/desc.xml")
	for _, want := range []string{"HTTP/1.1 200 OK\r\n", "CACHE-CONTROL: max-age=1800\r\n", "EXT:\r\n", "LOCATION: http://10.0.0.2:1234/renderer/desc.xml\r\n", "ST: " + typeRenderer + "\r\n", "USN: uuid:abc::" + typeRenderer + "\r\n"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in %q", want, msg)
		}
	}
	if !strings.HasSuffix(msg, "\r\n\r\n") {
		t.Errorf("no blank line at the end")
	}
}

// fakePlayer es un motor falso.
type fakePlayer struct {
	mu      sync.Mutex
	queue   []string
	idx     int
	playing bool
	paused  bool
	pos     float64
	vol     int
}

func (f *fakePlayer) Load(p string, play bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue, f.idx, f.playing, f.paused, f.pos = []string{p}, 0, play, !play, 0
}
func (f *fakePlayer) Append(p string) { f.mu.Lock(); f.queue = append(f.queue, p); f.mu.Unlock() }
func (f *fakePlayer) Play()           { f.mu.Lock(); f.playing, f.paused = true, false; f.mu.Unlock() }
func (f *fakePlayer) Pause()          { f.mu.Lock(); f.playing, f.paused = false, true; f.mu.Unlock() }
func (f *fakePlayer) Stop()           { f.mu.Lock(); f.playing, f.paused, f.pos = false, false, 0; f.mu.Unlock() }
func (f *fakePlayer) Seek(s float64)  { f.mu.Lock(); f.pos = s; f.mu.Unlock() }
func (f *fakePlayer) Next() {
	f.mu.Lock()
	if f.idx+1 < len(f.queue) {
		f.idx++
	}
	f.mu.Unlock()
}
func (f *fakePlayer) Prev() {}
func (f *fakePlayer) Status() PlayerStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := ""
	if f.idx < len(f.queue) {
		p = f.queue[f.idx]
	}
	return PlayerStatus{Path: p, Playing: f.playing, Paused: f.paused, Position: f.pos, Duration: 200}
}
func (f *fakePlayer) Volume() int     { f.mu.Lock(); defer f.mu.Unlock(); return f.vol }
func (f *fakePlayer) SetVolume(v int) { f.mu.Lock(); f.vol = v; f.mu.Unlock() }

// El renderer sigue la máquina de estados de AVTransport: sin nada, puesto,
// sonando, en pausa, buscando; la siguiente pasa a ser la actual; el volumen
// y el silencio.
func TestRendererTransport(t *testing.T) {
	dataDir, _ := testLibrary(t)
	p := &fakePlayer{vol: 70}
	_, srv := testService(t, dataDir, p)
	avt, rc := srv.URL+"/renderer/avt/control", srv.URL+"/renderer/rc/control"
	state := func() string {
		_, b := soap(t, avt, svcAVT, "GetTransportInfo", map[string]string{"InstanceID": "0"})
		i := strings.Index(b, "<CurrentTransportState>")
		j := strings.Index(b, "</CurrentTransportState>")
		if i < 0 || j < 0 {
			t.Fatalf("no state: %s", b)
		}
		return b[i+len("<CurrentTransportState>") : j]
	}
	if s := state(); s != "NO_MEDIA_PRESENT" {
		t.Fatalf("empty: %s", s)
	}
	meta := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="1"><dc:title>Song A</dc:title><upnp:artist>Artist A</upnp:artist><res duration="0:03:20.000">http://x/a.mp3</res></item></DIDL-Lite>`
	if code, b := soap(t, avt, svcAVT, "SetAVTransportURI", map[string]string{"InstanceID": "0", "CurrentURI": "http://x/a.mp3", "CurrentURIMetaData": meta}); code != 200 {
		t.Fatalf("set uri: %s", b)
	}
	if s := state(); s != "PAUSED_PLAYBACK" && s != "STOPPED" {
		t.Fatalf("after set: %s", s)
	}
	soap(t, avt, svcAVT, "Play", map[string]string{"InstanceID": "0", "Speed": "1"})
	if s := state(); s != "PLAYING" {
		t.Fatalf("after play: %s", s)
	}
	soap(t, avt, svcAVT, "Seek", map[string]string{"InstanceID": "0", "Unit": "REL_TIME", "Target": "0:01:05"})
	_, b := soap(t, avt, svcAVT, "GetPositionInfo", map[string]string{"InstanceID": "0"})
	if !strings.Contains(b, "<RelTime>0:01:05</RelTime>") || !strings.Contains(b, "<TrackURI>http://x/a.mp3</TrackURI>") || !strings.Contains(b, "<TrackDuration>0:03:20</TrackDuration>") {
		t.Errorf("position: %s", b)
	}
	soap(t, avt, svcAVT, "Pause", map[string]string{"InstanceID": "0"})
	if s := state(); s != "PAUSED_PLAYBACK" {
		t.Fatalf("after pause: %s", s)
	}
	soap(t, avt, svcAVT, "Stop", map[string]string{"InstanceID": "0"})
	if s := state(); s != "STOPPED" {
		t.Fatalf("after stop: %s", s)
	}
	soap(t, avt, svcAVT, "Play", map[string]string{"InstanceID": "0"})
	if s := state(); s != "PLAYING" {
		t.Fatalf("play after stop: %s", s)
	}
	soap(t, avt, svcAVT, "Pause", map[string]string{"InstanceID": "0"})
	// La siguiente: el motor pasa a ella y es la actual.
	soap(t, avt, svcAVT, "SetNextAVTransportURI", map[string]string{"InstanceID": "0", "NextURI": "http://x/b.mp3", "NextURIMetaData": ""})
	p.Next()
	_, b = soap(t, avt, svcAVT, "GetMediaInfo", map[string]string{"InstanceID": "0"})
	if !strings.Contains(b, "<CurrentURI>http://x/b.mp3</CurrentURI>") {
		t.Errorf("after next: %s", b)
	}
	// Volumen y silencio.
	soap(t, rc, svcRC, "SetVolume", map[string]string{"InstanceID": "0", "Channel": "Master", "DesiredVolume": "35"})
	if p.Volume() != 35 {
		t.Errorf("volume %d", p.Volume())
	}
	soap(t, rc, svcRC, "SetMute", map[string]string{"InstanceID": "0", "Channel": "Master", "DesiredMute": "1"})
	_, b = soap(t, rc, svcRC, "GetVolume", map[string]string{"InstanceID": "0", "Channel": "Master"})
	if p.Volume() != 0 || !strings.Contains(b, "<CurrentVolume>35</CurrentVolume>") {
		t.Errorf("mute: engine %d, %s", p.Volume(), b)
	}
	soap(t, rc, svcRC, "SetMute", map[string]string{"InstanceID": "0", "Channel": "Master", "DesiredMute": "0"})
	if p.Volume() != 35 {
		t.Errorf("unmute: %d", p.Volume())
	}
	// Errores.
	if code, b := soap(t, avt, svcAVT, "Seek", map[string]string{"InstanceID": "0", "Unit": "TRACK_NR", "Target": "2"}); code != 500 || !strings.Contains(b, "710") {
		t.Errorf("bad seek mode: %d %s", code, b)
	}
	if code, _ := soap(t, avt, svcAVT, "Play", map[string]string{"InstanceID": "3"}); code != 500 {
		t.Errorf("bad instance accepted")
	}
}

// Un suscrito al AVTransport recibe el estado al suscribirse y el
// LastChange al empezar a sonar.
func TestRendererEvents(t *testing.T) {
	dataDir, _ := testLibrary(t)
	p := &fakePlayer{vol: 50}
	_, srv := testService(t, dataDir, p)
	got := make(chan string, 8)
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "NOTIFY" && r.Header.Get("NT") == "upnp:event" {
			b, _ := io.ReadAll(r.Body)
			got <- string(b)
		}
	}))
	defer cb.Close()
	req, _ := http.NewRequest("SUBSCRIBE", srv.URL+"/renderer/avt/event", nil)
	req.Header.Set("CALLBACK", "<"+cb.URL+"/>")
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("TIMEOUT", "Second-300")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("SID"), "uuid:") || resp.Header.Get("TIMEOUT") != "Second-300" {
		t.Fatalf("subscribe: %d %v", resp.StatusCode, resp.Header)
	}
	wait := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case b := <-got:
				if strings.Contains(unescape(b), want) {
					return
				}
			case <-deadline:
				t.Fatalf("no event with %q", want)
			}
		}
	}
	wait(`TransportState val="NO_MEDIA_PRESENT"`)
	soap(t, srv.URL+"/renderer/avt/control", svcAVT, "SetAVTransportURI", map[string]string{"InstanceID": "0", "CurrentURI": "http://x/a.mp3"})
	soap(t, srv.URL+"/renderer/avt/control", svcAVT, "Play", map[string]string{"InstanceID": "0"})
	wait(`TransportState val="PLAYING"`)
}

// Lo que manda un mando se baja entero antes de sonar (Open dice que no está
// listo mientras tanto); el PCM crudo (audio/L16) se guarda como WAV.
func TestFetch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "src.wav")
	writeWAV(t, p, 8000, 8000)
	wav, _ := os.ReadFile(p)
	pcmBE := make([]byte, 8000*4)
	for i := 0; i < len(pcmBE); i += 2 {
		pcmBE[i], pcmBE[i+1] = wav[44+i+1], wav[44+i] // big endian
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.wav":
			w.Header().Set("Content-Type", "audio/wav")
			w.Header().Set("Content-Length", fmt.Sprint(len(wav)))
			w.Write(wav)
		case "/raw":
			w.Header().Set("Content-Type", "audio/L16;rate=8000;channels=2")
			w.Header().Set("Content-Length", fmt.Sprint(len(pcmBE)))
			w.Write(pcmBE)
		}
	}))
	defer srv.Close()
	f := newFetcher(filepath.Join(dir, "cache"))
	for _, path := range []string{"/a.wav", "/raw"} {
		key := "dlna://r" + path
		f.add(key, srv.URL+path, Meta{})
		var ready bool
		for i := 0; i < 100 && !ready; i++ {
			src, ok, err := f.Open(key)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if ok {
				ready = true
				buf := make([]float32, 512)
				n, _ := src.Read(buf)
				if src.SampleRate() != 8000 || n == 0 {
					t.Errorf("%s: rate %d, read %d", path, src.SampleRate(), n)
				}
				if src.Length() != 8000 {
					t.Errorf("%s: length %d", path, src.Length())
				}
				src.Close()
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ready {
			t.Fatalf("%s never got ready", path)
		}
		if m, ok := f.Track(key); !ok || m.Title == "" {
			t.Errorf("%s: no fallback title", path)
		}
	}
	// Olvidar algo borra su archivo.
	f.keep()
	if left, _ := filepath.Glob(filepath.Join(dir, "cache", "*")); len(left) != 0 {
		t.Errorf("cache not cleaned: %v", left)
	}
}

// Los metadatos DIDL de un mando se leen sin fiarse de los prefijos.
func TestParseMeta(t *testing.T) {
	m := parseMeta(`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"><item><dc:title>Tom &amp; Jerry</dc:title><dc:creator>Someone</dc:creator><upnp:album>LP</upnp:album><upnp:albumArtURI>http://x/a.jpg</upnp:albumArtURI><res duration="1:02:03.5">u</res></item></DIDL-Lite>`)
	if m.Title != "Tom & Jerry" || m.Artist != "Someone" || m.Album != "LP" || m.Art != "http://x/a.jpg" || m.Duration != 3723.5 {
		t.Errorf("meta %+v", m)
	}
	if fmtTime(3723.5) != "1:02:03" {
		t.Errorf("fmtTime %s", fmtTime(3723.5))
	}
}

// Las descripciones son XML válido con el nombre escapado.
func TestDescriptions(t *testing.T) {
	dataDir, _ := testLibrary(t)
	_, srv := testService(t, dataDir, &fakePlayer{})
	for _, p := range []string{"/server/desc.xml", "/renderer/desc.xml", "/server/cd/scpd.xml", "/server/cm/scpd.xml", "/renderer/avt/scpd.xml", "/renderer/rc/scpd.xml"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
		if err := xmlValid(b); err != nil {
			t.Errorf("%s: %v", p, err)
		}
		if strings.HasSuffix(p, "desc.xml") && !strings.Contains(string(b), "test &amp; co") {
			t.Errorf("%s: name not escaped", p)
		}
	}
}

// xmlValid dice si b es XML bien formado.
func xmlValid(b []byte) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// TestRendererUDN comprueba que RendererUDN da el UDN del renderer guardado
// (el que anuncia por SSDP) y nada si nunca se creó.
func TestRendererUDN(t *testing.T) {
	dir := t.TempDir()
	if got := RendererUDN(dir); got != "" {
		t.Errorf("RendererUDN before creating = %q, want empty", got)
	}
	id := persistentUUID(dir, "dlna-renderer.uuid")
	if got := RendererUDN(dir); got != "uuid:"+id {
		t.Errorf("RendererUDN = %q, want %q", got, "uuid:"+id)
	}
}
