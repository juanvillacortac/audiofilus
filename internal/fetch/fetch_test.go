package fetch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Lo apuntado se baja entero antes de sonar (Open dice que no está listo
// mientras tanto); el PCM crudo (audio/L16) se guarda como WAV.
func TestFetch(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	b.Write(l16Header(8000, 2, 8000*4))
	for i := 0; i < 8000; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/8000) * 8000)
		_ = binary.Write(&b, binary.LittleEndian, v)
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	wav := b.Bytes()
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
	f := New[string](filepath.Join(dir, "cache"), "test")
	for _, path := range []string{"/a.wav", "/raw"} {
		key := "dlna://r" + path
		f.Add(key, srv.URL+path, "meta")
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
		if uri, m, ok := f.Entry(key); !ok || m != "meta" || TitleFromURL(uri) == "" {
			t.Errorf("%s: entry %q %q %v", path, uri, m, ok)
		}
	}
	// Olvidar algo borra su archivo.
	f.Keep()
	if left, _ := filepath.Glob(filepath.Join(dir, "cache", "*")); len(left) != 0 {
		t.Errorf("cache not cleaned: %v", left)
	}
}

// La portada de una ruta es la de su URL, bajada aparte (.img); si esa URL
// no es una imagen, o no hay, el archivo de la canción ya bajado (que la
// trae incrustada). Mientras nada está listo, "".
func TestCover(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "\x00\x00\x00\x0dIHDR")
	song := []byte("RIFF....WAVEfmt ")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/c.png":
			w.Write(png)
		case "/s.wav":
			w.Header().Set("Content-Length", fmt.Sprint(len(song)))
			w.Write(song)
		default:
			w.Write([]byte("<html>no</html>"))
		}
	}))
	defer srv.Close()
	f := New[string](filepath.Join(t.TempDir(), "cache"), "test")
	wait := func(p, art string) string {
		for i := 0; i < 200; i++ {
			_, _, _ = f.Open(p) // que se baje la canción
			if c := f.Cover(p, art); c != "" {
				return c
			}
			time.Sleep(10 * time.Millisecond)
		}
		return ""
	}
	f.Add("a", srv.URL+"/s.wav", "")
	if c := wait("a", srv.URL+"/c.png"); filepath.Ext(c) != ".img" {
		t.Errorf("cover from its URL: %q", c)
	} else if b, _ := os.ReadFile(c); !bytes.Equal(b, png) {
		t.Errorf("cover content: %q", b)
	}
	f.Add("b", srv.URL+"/s.wav", "")
	if c := wait("b", srv.URL+"/not-image"); filepath.Ext(c) != ".wav" {
		t.Errorf("fallback to the song: %q", c)
	}
	f.Add("c", srv.URL+"/s.wav", "")
	if c := wait("c", ""); filepath.Ext(c) != ".wav" {
		t.Errorf("no URL: %q", c)
	}
	img := f.Cover("a", srv.URL+"/c.png")
	f.Keep()
	if _, err := os.Stat(img); err == nil {
		t.Error("cover kept after Keep")
	}
}
