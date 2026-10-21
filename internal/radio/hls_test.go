package radio

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// tsPackets arma un trozo MPEG-TS con una PAT, una PMT con un flujo de audio
// de tipo typ en el PID 0x101 y la carga en un PES repartido en paquetes.
func tsPackets(typ byte, payload []byte) []byte {
	packet := func(pid int, start bool, body []byte) []byte {
		p := make([]byte, tsPacket)
		p[0] = 0x47
		p[1] = byte(pid >> 8 & 0x1f)
		if start {
			p[1] |= 0x40
		}
		p[2] = byte(pid)
		if len(body) >= tsPacket-4 {
			p[3] = 0x10
			copy(p[4:], body)
			return p
		}
		// Lo que sobra va en el campo de adaptación, de relleno.
		fill := tsPacket - 4 - len(body)
		p[3] = 0x30
		p[4] = byte(fill - 1)
		for i := 5; i < 4+fill; i++ {
			p[i] = 0xff
		}
		if fill > 1 {
			p[5] = 0
		}
		copy(p[4+fill:], body)
		return p
	}
	psi := func(tableID byte, body []byte) []byte {
		length := 5 + len(body) + 4
		sec := []byte{0, tableID, 0xb0 | byte(length>>8), byte(length), 0, 1, 0xc1, 0, 0}
		sec = append(sec, body...)
		return append(sec, 0, 0, 0, 0) // CRC (no se comprueba)
	}
	var out []byte
	out = append(out, packet(0, true, psi(0x00, []byte{0, 1, 0xe1, 0x00}))...)
	out = append(out, packet(0x100, true, psi(0x02, []byte{0xe1, 0x01, 0xf0, 0, typ, 0xe1, 0x01, 0xf0, 0}))...)
	pes := append([]byte{0, 0, 1, 0xc0, 0, 0, 0x80, 0x80, 5, 0x21, 0, 1, 0, 1}, payload...)
	for first := true; len(pes) > 0; first = false {
		n := min(len(pes), tsPacket-4)
		out = append(out, packet(0x101, first, pes[:n])...)
		pes = pes[n:]
	}
	return out
}

// adts son unos bytes que empiezan como un frame AAC en ADTS.
func adts(n int, fill byte) []byte {
	b := bytes.Repeat([]byte{fill}, n)
	b[0], b[1] = 0xFF, 0xF1
	return b
}

// De una lista maestra se elige la pista de audio aparte si la hay y, si no,
// la variante de más calidad que no pase de hlsMaxBandwidth.
func TestHLSMaster(t *testing.T) {
	variants := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=64000,CODECS=\"mp4a.40.5\"\nlow.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=320000,CODECS=\"mp4a.40.2\"\nhigh.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2500000,CODECS=\"avc1.4d401f,mp4a.40.2\"\nvideo.m3u8\n"
	if got := hlsMaster([]byte(variants)); got != "high.m3u8" {
		t.Errorf("variants: got %q, want high.m3u8", got)
	}
	audio := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"en\",DEFAULT=YES,URI=\"audio/en.m3u8\"\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2500000,AUDIO=\"a\"\nvideo.m3u8\n"
	if got := hlsMaster([]byte(audio)); got != "audio/en.m3u8" {
		t.Errorf("audio rendition: got %q, want audio/en.m3u8", got)
	}
	if got := hlsMaster([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\na.ts\n")); got != "" {
		t.Errorf("media playlist: got %q, want \"\"", got)
	}
}

// Una lista de medios da sus trozos con su número de secuencia, su título y
// su clave.
func TestParseHLSMedia(t *testing.T) {
	m := parseHLSMedia([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:41\n" +
		"#EXTINF:10.0,Artist - Song\nseg41.ts\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x000102030405060708090a0b0c0d0e0f\n" +
		"#EXTINF:10.0,\nseg42.ts\n"))
	if m.target != 10*time.Second || len(m.segments) != 2 || m.ended {
		t.Fatalf("got %+v", m)
	}
	if s := m.segments[0]; s.seq != 41 || s.title != "Artist - Song" || s.key != nil {
		t.Errorf("first segment: %+v", s)
	}
	if s := m.segments[1]; s.seq != 42 || s.key == nil || s.key.uri != "key.bin" || len(s.key.iv) != 16 || s.key.iv[15] != 0x0f {
		t.Errorf("second segment: %+v (key %+v)", s, s.key)
	}
}

// Del MPEG-TS sale el audio tal cual (sin cabeceras de PES), con su códec.
func TestTSDemux(t *testing.T) {
	want := adts(500, 0x42)
	for typ, codec := range map[byte]string{tsAACADTS: "aac", tsMPEG1Audio: "mp3"} {
		var d tsDemuxer
		got, c, err := d.audio(tsPackets(typ, want))
		if err != nil || c != codec || !bytes.Equal(got, want) {
			t.Errorf("type %#x: codec %q, %d bytes, err %v; want %q and the same %d bytes", typ, c, len(got), err, codec, len(want))
		}
	}
	// Audio empaquetado: una etiqueta ID3 y luego ADTS.
	var d tsDemuxer
	packed := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 3, 1, 2, 3}, want...)
	if got, c, err := d.audio(packed); err != nil || c != "aac" || !bytes.Equal(got, want) {
		t.Errorf("packed audio: codec %q, %d bytes, err %v", c, len(got), err)
	}
}

// Una emisora HLS de verdad: lista maestra, lista de medios en directo que
// se renueva y trozos (uno cifrado con AES-128) que llegan al búfer en orden.
func TestHLSStream(t *testing.T) {
	key := []byte("0123456789abcdef")
	seg := func(n int) []byte { return tsPackets(tsAACADTS, adts(300, byte(n))) }
	enc := func(b []byte, seq int) []byte {
		pad := aes.BlockSize - len(b)%aes.BlockSize
		b = append(append([]byte{}, b...), bytes.Repeat([]byte{byte(pad)}, pad)...)
		iv := make([]byte, 16)
		iv[15] = byte(seq)
		block, _ := aes.NewCipher(key)
		out := make([]byte, len(b))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, b)
		return out
	}
	start := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("/live.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=128000\naudio/media.m3u8\n")
	})
	mux.HandleFunc("/audio/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		// Cada 200 ms sale un trozo nuevo; la lista tiene los cuatro últimos.
		top := 5 + int(time.Since(start)/(200*time.Millisecond))
		var b strings.Builder
		fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-TARGETDURATION:0.2\n#EXT-X-MEDIA-SEQUENCE:%d\n", top-3)
		for n := top - 3; n <= top; n++ {
			if n == 6 {
				b.WriteString("#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\n")
			} else if n == 7 {
				b.WriteString("#EXT-X-KEY:METHOD=NONE\n")
			}
			fmt.Fprintf(&b, "#EXTINF:0.2,Song %d\nseg%d.ts\n", n, n)
		}
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/audio/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/audio/seg%d.ts", &n); err != nil {
			http.NotFound(w, r)
			return
		}
		if n == 6 {
			w.Write(enc(seg(n), n))
			return
		}
		w.Write(seg(n))
	})
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) { w.Write(key) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	s := &Stream{url: srv.URL + "/live.m3u8", cancel: cancel}
	s.space = sync.NewCond(&s.mu)
	done := make(chan error, 1)
	go func() { done <- s.connect(ctx) }()
	// Hasta tener los trozos 3 a 7 (empieza por los tres últimos: 3, 4, 5).
	var want []byte
	for n := 3; n <= 7; n++ {
		want = append(want, adts(300, byte(n))...)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := len(s.buf)
		s.mu.Unlock()
		if got >= len(want) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fatal != nil {
		t.Fatalf("fatal: %v", s.fatal)
	}
	if s.codec != "aac" {
		t.Errorf("codec %q, want aac", s.codec)
	}
	if len(s.buf) < len(want) || !bytes.Equal(s.buf[:len(want)], want) {
		var segs []byte
		for i := 2; i < len(s.buf); i += 300 {
			segs = append(segs, s.buf[i])
		}
		t.Errorf("buffer: %d bytes (segments %v), want segments 3 to 7 in order (%d bytes)", len(s.buf), segs, len(want))
	}
	if !strings.HasPrefix(s.title, "Song ") {
		t.Errorf("title %q, want the #EXTINF one", s.title)
	}
}

// Las emisoras HLS del directorio salen aunque radio-browser no diga su
// códec (lo da como "UNKNOWN"); las de un códec que no se entiende, no.
func TestPlayableHLS(t *testing.T) {
	cases := []struct {
		st   Station
		want bool
	}{
		{Station{Codec: "MP3"}, true},
		{Station{Codec: "UNKNOWN", HLS: 1}, true},
		{Station{Codec: "UNKNOWN", URL: "https://example.com/live/index.m3u8?id=1"}, true},
		{Station{Codec: "UNKNOWN", URL: "https://example.com/stream"}, false},
		{Station{Codec: "FLAC"}, false},
	}
	for _, c := range cases {
		if got := c.st.Playable(); got != c.want {
			t.Errorf("%+v: Playable() = %v, want %v", c.st, got, c.want)
		}
	}
}

// Un trozo MPEG-TS con algo delante se lee igual, y uno con AAC en LATM dice
// que no se entiende en vez de quedarse en silencio.
func TestTSSyncAndLATM(t *testing.T) {
	want := adts(400, 0x33)
	var d tsDemuxer
	junk := append([]byte{1, 2, 3, 4, 5}, tsPackets(tsAACADTS, want)...)
	if got, c, err := d.audio(junk); err != nil || c != "aac" || !bytes.Equal(got, want) {
		t.Errorf("leading junk: codec %q, %d bytes, err %v", c, len(got), err)
	}
	var l tsDemuxer
	if _, _, err := l.audio(tsPackets(tsAACLATM, want)); err == nil || !strings.Contains(err.Error(), "LATM") {
		t.Errorf("LATM: err %v, want one that says LATM", err)
	}
}

// Si un trozo falla suelto (un 404), se sigue con los demás en la lista
// siguiente en vez de reconectar desde cero.
func TestHLSSegmentFailure(t *testing.T) {
	start := time.Now()
	failed := false
	mux := http.NewServeMux()
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		top := 5 + int(time.Since(start)/(200*time.Millisecond))
		var b strings.Builder
		fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-TARGETDURATION:0.2\n#EXT-X-MEDIA-SEQUENCE:%d\n", top-3)
		for n := top - 3; n <= top; n++ {
			fmt.Fprintf(&b, "#EXTINF:0.2,\nseg%d.ts\n", n)
		}
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/seg%d.ts", &n); err != nil {
			http.NotFound(w, r)
			return
		}
		if n == 4 && !failed { // falla la primera vez
			failed = true
			http.Error(w, "gone for a moment", http.StatusNotFound)
			return
		}
		w.Write(tsPackets(tsAACADTS, adts(300, byte(n))))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Stream{url: srv.URL + "/media.m3u8", cancel: cancel}
	s.space = sync.NewCond(&s.mu)
	done := make(chan error, 1)
	go func() { done <- s.connect(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.buf)
		s.mu.Unlock()
		if n >= 4*300 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("connect returned %v, want it to keep going after one failed segment", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var want []byte
	for n := 3; n <= 6; n++ {
		want = append(want, adts(300, byte(n))...)
	}
	if len(s.buf) < len(want) || !bytes.Equal(s.buf[:len(want)], want) {
		t.Errorf("buffer: %d bytes, want segments 3, 4 (retried), 5 and 6 in order", len(s.buf))
	}
}

// Los trozos dados como rangos de un solo archivo (#EXT-X-BYTERANGE, sin @
// siguen al anterior) se piden con Range y llegan en orden.
func TestHLSByteRange(t *testing.T) {
	var file []byte
	var want []byte
	var list strings.Builder
	list.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for n := 0; n < 3; n++ {
		seg := tsPackets(tsAACADTS, adts(250, byte(10+n)))
		if n == 0 {
			fmt.Fprintf(&list, "#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\nall.ts\n", len(seg), len(file))
		} else {
			fmt.Fprintf(&list, "#EXTINF:1,\n#EXT-X-BYTERANGE:%d\nall.ts\n", len(seg))
		}
		file = append(file, seg...)
		want = append(want, adts(250, byte(10+n))...)
	}
	list.WriteString("#EXT-X-ENDLIST\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/list.m3u8", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, list.String()) })
	mux.HandleFunc("/all.ts", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "all.ts", time.Time{}, bytes.NewReader(file))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := &Stream{url: srv.URL + "/list.m3u8"}
	s.space = sync.NewCond(&s.mu)
	if err := s.connect(context.Background()); err == nil || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("connect: %v, want the stream to end after the list", err)
	}
	if !bytes.Equal(s.buf, want) {
		t.Errorf("buffer: %d bytes, want the three ranges in order (%d bytes)", len(s.buf), len(want))
	}
}
