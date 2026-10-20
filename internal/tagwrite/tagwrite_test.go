package tagwrite

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhowden/tag"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/library"
)

// Los archivos de prueba (tonos sintéticos, ver testdata/README.md).
var fixtures = []string{"tone24.mp3", "tone23.mp3", "bare.mp3", "tone.flac", "tone.ogg", "tone.opus", "front.m4a", "back.m4a"}

// copyFixture copia un archivo de testdata a una carpeta temporal.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pngCover es una portada PNG de w×h píxeles (con ruido, para que pese).
func pngCover(t *testing.T, w, h int) *Cover {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	img.Set(0, 0, color.RGBA{1, 2, 3, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return &Cover{MIME: "image/png", Data: b.Bytes()}
}

// payload es el hash del audio de un archivo, leído sin el paquete (salvo
// en Ogg, donde son los datos y posiciones de las páginas de audio).
func payload(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		if len(b) >= 10 && string(b[:3]) == "ID3" {
			b = b[10+synchsafe(b[6:10]):]
		}
	case ".flac":
		pos := 4
		for {
			last := b[pos]&0x80 != 0
			pos += 4 + (int(b[pos+1])<<16 | int(b[pos+2])<<8 | int(b[pos+3]))
			if last {
				break
			}
		}
		b = b[pos:]
	case ".ogg", ".opus":
		f, err := parseOgg(b)
		if err != nil {
			t.Fatal(err)
		}
		var h bytes.Buffer
		for _, p := range f.audio {
			binary.Write(&h, binary.LittleEndian, p.granule)
			h.Write(p.lacing)
			h.Write(p.body)
		}
		b = h.Bytes()
	case ".m4a":
		// El contenido del mdat.
		for pos := 0; pos+8 <= len(b); {
			n := topSize(b, pos)
			if string(b[pos+4:pos+8]) == "mdat" {
				b = b[pos+8 : pos+n]
				break
			}
			pos += n
		}
	}
	return sha256.Sum256(b)
}

// decodes comprueba que el archivo aún se decodifica (si este equipo tiene
// el decodificador: Opus y AAC van por bibliotecas de C).
func decodes(t *testing.T, path string) {
	t.Helper()
	src, err := decode.Open(path)
	if err != nil {
		if strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "library") {
			t.Logf("%s: no decoder here (%v)", filepath.Base(path), err)
			return
		}
		t.Fatalf("%s no longer decodes: %v", filepath.Base(path), err)
	}
	defer src.Close()
	buf := make([]float32, 4096)
	total := 0
	for {
		n, err := src.Read(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: decode error after %d samples: %v", filepath.Base(path), total, err)
		}
	}
	if total == 0 {
		t.Fatalf("%s: decoded nothing", filepath.Base(path))
	}
}

// full son todas las etiquetas, con letras de fuera de Latin-1.
func full(t *testing.T) Tags {
	return Tags{
		Title: "Canción ñ 日本", Artist: "Ärtist", Album: "Álbum", AlbumArtist: "Varios", Genre: "Chiptune",
		Composer: "Compositora", Comment: "un comentario", Year: 1999, Track: 3, TrackTotal: 12, Disc: 1, DiscTotal: 2,
		ReplayGain: &Gain{TrackGain: -6.54, TrackPeak: 0.987654, AlbumGain: -7.25, AlbumPeak: 0.999, HasAlbum: true},
		Cover:      pngCover(t, 8, 8),
	}
}

// same compara lo que se leyó con lo escrito.
func same(t *testing.T, name string, got, want Tags, opus bool) {
	t.Helper()
	if got.Title != want.Title || got.Artist != want.Artist || got.Album != want.Album || got.AlbumArtist != want.AlbumArtist ||
		got.Genre != want.Genre || got.Composer != want.Composer || got.Comment != want.Comment || got.Year != want.Year ||
		got.Track != want.Track || got.TrackTotal != want.TrackTotal || got.Disc != want.Disc || got.DiscTotal != want.DiscTotal {
		t.Errorf("%s: read back\n %+v\nwant\n %+v", name, got, want)
	}
	if (got.ReplayGain == nil) != (want.ReplayGain == nil) {
		t.Fatalf("%s: gain %v, want %v", name, got.ReplayGain, want.ReplayGain)
	}
	if g, w := got.ReplayGain, want.ReplayGain; g != nil {
		tol := 0.005
		if opus {
			tol = 1.0 / 256
		}
		if math.Abs(g.TrackGain-w.TrackGain) > tol || math.Abs(g.AlbumGain-w.AlbumGain) > tol || g.HasAlbum != w.HasAlbum ||
			math.Abs(g.TrackPeak-w.TrackPeak) > 1e-6 || math.Abs(g.AlbumPeak-w.AlbumPeak) > 1e-6 {
			t.Errorf("%s: gain %+v, want %+v", name, *g, *w)
		}
	}
	if (got.Cover == nil) != (want.Cover == nil) {
		t.Fatalf("%s: cover %v, want %v", name, got.Cover != nil, want.Cover != nil)
	}
	if got.Cover != nil && (!bytes.Equal(got.Cover.Data, want.Cover.Data) || got.Cover.MIME != want.Cover.MIME) {
		t.Errorf("%s: cover changed (%s %d bytes, want %s %d)", name, got.Cover.MIME, len(got.Cover.Data), want.Cover.MIME, len(want.Cover.Data))
	}
}

// Escribir todas las etiquetas y leerlas da lo mismo, el audio no cambia,
// el archivo se sigue decodificando y otro lector (dhowden/tag, el de la
// biblioteca) ve lo escrito.
func TestRoundTrip(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			p := copyFixture(t, name)
			before := payload(t, p)
			want := full(t)
			if err := Write(p, want); err != nil {
				t.Fatal(err)
			}
			got, err := Read(p)
			if err != nil {
				t.Fatal(err)
			}
			same(t, name, got, want, strings.HasSuffix(name, ".opus"))
			if payload(t, p) != before {
				t.Errorf("audio changed")
			}
			decodes(t, p)
			f, _ := os.Open(p)
			m, err := tag.ReadFrom(f)
			f.Close()
			if err != nil {
				t.Fatalf("dhowden/tag can't read it: %v", err)
			}
			if m.Title() != want.Title || m.Artist() != want.Artist || m.Album() != want.Album {
				t.Errorf("dhowden/tag reads %q / %q / %q", m.Title(), m.Artist(), m.Album())
			}
			if n, total := m.Track(); n != want.Track || total != want.TrackTotal {
				t.Errorf("dhowden/tag track %d/%d", n, total)
			}
			if g, ok := library.ReadGain(p); !ok || math.Abs(g.Track-want.ReplayGain.TrackGain) > 0.01 || !g.HasAlbum {
				t.Errorf("library.ReadGain = %+v %v", g, ok)
			}
			// Escribir lo mismo otra vez no cambia nada.
			st, _ := os.Stat(p)
			if err := Write(p, want); err != nil {
				t.Fatal(err)
			}
			st2, _ := os.Stat(p)
			if !st2.ModTime().Equal(st.ModTime()) {
				t.Errorf("rewrote the file although nothing changed")
			}
		})
	}
}

// Escribir un campo deja los demás como estaban.
func TestPartialKeepsOthers(t *testing.T) {
	for _, name := range fixtures {
		if name == "bare.mp3" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			p := copyFixture(t, name)
			old, err := Read(p)
			if err != nil {
				t.Fatal(err)
			}
			if old.Artist != "audiofilus" {
				t.Fatalf("fixture artist %q", old.Artist)
			}
			if err := Write(p, Tags{Title: "Nuevo"}); err != nil {
				t.Fatal(err)
			}
			got, _ := Read(p)
			want := old
			want.Title = "Nuevo"
			same(t, name, got, want, false)
		})
	}
}

// Clear borra los campos pedidos (y la portada).
func TestClear(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			p := copyFixture(t, name)
			if err := Write(p, full(t)); err != nil {
				t.Fatal(err)
			}
			if err := Write(p, Tags{Clear: []string{"artist", "cover", "replaygain", "year", "track", "tracktotal"}}); err != nil {
				t.Fatal(err)
			}
			got, _ := Read(p)
			if got.Artist != "" || got.Cover != nil || got.ReplayGain != nil || got.Year != 0 || got.Track != 0 || got.TrackTotal != 0 {
				t.Errorf("not cleared: %+v", got)
			}
			if got.Title != "Canción ñ 日本" || got.Disc != 1 {
				t.Errorf("cleared too much: %+v", got)
			}
			decodes(t, p)
		})
	}
}

// Una portada grande hace crecer las etiquetas más allá del relleno (en Ogg,
// el paquete ocupa varias páginas; en M4A con el moov delante, se corren las
// posiciones del audio) y quitarla las encoge: el audio no cambia y se
// sigue decodificando.
func TestBigCoverGrowAndShrink(t *testing.T) {
	big := pngCover(t, 220, 220) // ~145 KB
	if len(big.Data) < 100<<10 {
		t.Fatalf("cover only %d bytes", len(big.Data))
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			p := copyFixture(t, name)
			before := payload(t, p)
			size0 := fileSize(t, p)
			if err := Write(p, Tags{Cover: big}); err != nil {
				t.Fatal(err)
			}
			if fileSize(t, p) < size0+int64(len(big.Data))-16<<10 { // menos el relleno que hubiera
				t.Errorf("file didn't grow with the cover")
			}
			got, _ := Read(p)
			if got.Cover == nil || !bytes.Equal(got.Cover.Data, big.Data) {
				t.Fatalf("big cover not read back")
			}
			if payload(t, p) != before {
				t.Errorf("audio changed")
			}
			decodes(t, p)
			if err := Write(p, Tags{Clear: []string{"cover"}}); err != nil {
				t.Fatal(err)
			}
			if got, _ := Read(p); got.Cover != nil {
				t.Errorf("cover not removed")
			}
			if payload(t, p) != before {
				t.Errorf("audio changed after removing the cover")
			}
			decodes(t, p)
		})
	}
}

func fileSize(t *testing.T, p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// En Opus, la ganancia va como R128_* (1/256 dB respecto a -23 LUFS) y no
// como REPLAYGAIN_*_GAIN.
func TestOpusR128(t *testing.T) {
	p := copyFixture(t, "tone.opus")
	if err := Write(p, Tags{ReplayGain: &Gain{TrackGain: -3, TrackPeak: 0.5}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	f, err := parseOgg(b)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.vc.get("R128_TRACK_GAIN"); v != "-2048" { // (-3 - 5) * 256
		t.Errorf("R128_TRACK_GAIN = %q, want -2048", v)
	}
	if _, ok := f.vc.get("REPLAYGAIN_TRACK_GAIN"); ok {
		t.Errorf("REPLAYGAIN_TRACK_GAIN written in an Opus file")
	}
	if g, ok := library.ReadGain(p); !ok || g.Track != -3 {
		t.Errorf("library reads %+v", g)
	}
}

// Una etiqueta ID3v2.3 sigue siendo 2.3 (con UTF-16 para lo que no es
// Latin-1); un MP3 sin etiqueta recibe una 2.4.
func TestID3Versions(t *testing.T) {
	p := copyFixture(t, "tone23.mp3")
	if err := Write(p, Tags{Title: "日本 v23"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if b[3] != 3 {
		t.Errorf("version %d, want 3", b[3])
	}
	if got, _ := Read(p); got.Title != "日本 v23" || got.Genre != "Rock" {
		t.Errorf("read %+v", got)
	}
	p = copyFixture(t, "bare.mp3")
	if err := Write(p, Tags{Title: "nuevo"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b[:3]) != "ID3" || b[3] != 4 {
		t.Errorf("bare mp3 got %q v%d", b[:3], b[3])
	}
}

// Con el relleno, cambiar una etiqueta corta no mueve el audio de sitio.
func TestPaddingKeepsSize(t *testing.T) {
	for _, name := range []string{"tone24.mp3", "tone.flac"} {
		p := copyFixture(t, name)
		if err := Write(p, Tags{Title: "x"}); err != nil { // la primera vez puede crecer
			t.Fatal(err)
		}
		s := fileSize(t, p)
		if err := Write(p, Tags{Title: "otra cosa", Album: "algo"}); err != nil {
			t.Fatal(err)
		}
		if fileSize(t, p) != s {
			t.Errorf("%s: size %d → %d", name, s, fileSize(t, p))
		}
	}
}

// Un archivo dañado da error (sin pánico) y el original queda igual.
func TestCorrupt(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, name := range fixtures {
		orig, _ := os.ReadFile(filepath.Join("testdata", name))
		for i := 0; i < 300; i++ {
			b := append([]byte(nil), orig...)
			switch i % 3 {
			case 0: // cortado
				b = b[:r.IntN(len(b))]
			default: // bytes cambiados en la cabecera
				for k := 0; k < 1+r.IntN(8); k++ {
					b[r.IntN(min(len(b), 600))] = byte(r.IntN(256))
				}
			}
			p := filepath.Join(t.TempDir(), name)
			os.WriteFile(p, b, 0o644)
			func() {
				defer func() {
					if e := recover(); e != nil {
						t.Fatalf("%s case %d: panic %v", name, i, e)
					}
				}()
				_, _ = Read(p)
				if err := Write(p, Tags{Title: "x"}); err != nil {
					if after, _ := os.ReadFile(p); !bytes.Equal(after, b) {
						t.Fatalf("%s case %d: failed write changed the file", name, i)
					}
				}
			}()
		}
	}
}

// Supported va por la extensión.
func TestSupported(t *testing.T) {
	for p, want := range map[string]bool{"a.mp3": true, "a.FLAC": true, "a.opus": true, "a.m4a": true, "a.wav": false, "a.aiff": false} {
		if Supported(p) != want {
			t.Errorf("Supported(%q) = %v", p, !want)
		}
	}
}
