package decode

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// copyTone copia testdata/tone.flac a path (creando sus carpetas), con pre
// delante y post detrás.
func copyTone(t *testing.T, path string, pre, post []byte) {
	t.Helper()
	b, err := os.ReadFile("testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := append(append(append([]byte{}, pre...), b...), post...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// id3v2 es una etiqueta ID3v2.4 mínima (un TIT2) con relleno, como la que
// algunos programas ponen delante de un FLAC.
func id3v2() []byte {
	frame := append([]byte("TIT2\x00\x00\x00\x05\x00\x00"), []byte("\x03hey\x00")...)
	body := append(frame, make([]byte, 1000)...)
	n := len(body)
	hdr := []byte{'I', 'D', '3', 4, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(hdr, body...)
}

// TestFLACWithID3v2 comprueba que un FLAC con una etiqueta ID3v2 delante
// (lo hacen algunos etiquetadores) se abra como FLAC y no como MP3.
func TestFLACWithID3v2(t *testing.T) {
	p := filepath.Join(t.TempDir(), "id3.flac")
	copyTone(t, p, id3v2(), nil)
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.(*flacSource); !ok {
		t.Fatalf("opened as %T, want the FLAC decoder", s)
	}
	if got := len(readAll(t, s)) / 2; int64(got) != s.Length() {
		t.Errorf("decoded %d frames, want %d", got, s.Length())
	}
}

// TestFLACTrailingTag comprueba que una etiqueta ID3v1 o APE detrás del
// audio de un FLAC acabe la canción con io.EOF y no con un error de
// decodificación.
func TestFLACTrailingTag(t *testing.T) {
	for name, tail := range map[string][]byte{
		"id3v1": append([]byte("TAG"), make([]byte, 125)...),
		"ape":   append([]byte("APETAGEX"), make([]byte, 24)...),
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "tail.flac")
			copyTone(t, p, nil, tail)
			s, err := Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			buf := make([]float32, 4096)
			for {
				_, err := s.Read(buf)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("Read at frame %d of %d: %v", s.Position(), s.Length(), err)
				}
			}
		})
	}
}

// TestFLAC32BitUnsupported comprueba que un FLAC de 32 bits (libFLAC 1.4
// en adelante; mewkiz/flac no sabe leer sus bloques) se rechace al abrirlo
// con ErrUnsupported, en vez de abrirse y fallar en la primera lectura.
// El de la prueba es el tono con la STREAMINFO cambiada a 32 bits.
func TestFLAC32BitUnsupported(t *testing.T) {
	b, err := os.ReadFile("testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	// STREAMINFO va tras "fLaC" y su cabecera de bloque; los bits por
	// muestra menos uno están en el bit bajo de su byte 12 y los 4 altos
	// del 13.
	si := b[8:]
	bps := 32 - 1
	si[12] = si[12]&^1 | byte(bps>>4)
	si[13] = si[13]&0x0f | byte(bps&0xf)<<4
	p := filepath.Join(t.TempDir(), "b32.flac")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err == nil {
		s.Close()
		t.Fatal("32-bit FLAC opened; want ErrUnsupported")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

// TestOpenUnusualNames comprueba que nombres con caracteres raros (los que
// los globs, las URL o los shells tratan aparte, emoji, puntos y espacios
// al final, uno que parece una pista CUE, uno muy largo) se abran, también
// después de pasar por JSON como en el socket.
func TestOpenUnusualNames(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"Disc #1/01 100% [Live] (Remix).flac",
		"Ünïcödé — 日本語 🎵/04 é.flac",
		"Folder#t=12/06 track.flac",
		strings.Repeat("ñ", 120) + ".flac",
	}
	if runtime.GOOS != "windows" { // nombres que Windows no admite
		names = append(names, "What?*/02 a*b?c[d].flac", `Album: "Quoted"/03 x.flac`,
			"Trailing dot./05 space .flac", "%41%20/07 back\\slash.flac")
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		copyTone(t, p, nil, nil)
		var back string
		b, _ := json.Marshal(p)
		_ = json.Unmarshal(b, &back)
		s, err := Open(back)
		if err != nil {
			t.Errorf("%q: %v", n, err)
			continue
		}
		s.Close()
	}
}

// TestOpenMangledName comprueba que un archivo cuyo nombre no es UTF-8
// válido (Latin-1 en un disco viejo) se abra aunque su ruta haya pasado por
// JSON (el socket, la caché de la biblioteca), que cambia cada byte
// inválido por U+FFFD.
func TestOpenMangledName(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux file systems take names that are not UTF-8")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "Caf\xe9 del Mar", "01 Caf\xe9.flac")
	copyTone(t, p, nil, nil)
	var back string
	b, _ := json.Marshal(p)
	_ = json.Unmarshal(b, &back)
	if back == p {
		t.Fatal("JSON kept the invalid bytes; the test assumes it does not")
	}
	s, err := Open(back)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got := FindFile(back); got != p {
		t.Errorf("FindFile = %q, want %q", got, p)
	}
}

// TestFindFileNormalization comprueba que una ruta en NFC encuentre el
// archivo guardado en NFD (macOS, algunas tarjetas exFAT) y al revés.
func TestFindFileNormalization(t *testing.T) {
	dir := t.TempDir()
	disk := filepath.Join(dir, norm.NFD.String("Canción Ñandú"), norm.NFD.String("01 Pequeña.flac"))
	copyTone(t, disk, nil, nil)
	want := filepath.Join(dir, norm.NFC.String("Canción Ñandú"), norm.NFC.String("01 Pequeña.flac"))
	if want == disk {
		t.Fatal("NFC and NFD forms are equal; the test needs accented letters")
	}
	if _, err := os.Lstat(want); err == nil {
		t.Skip("this file system ignores the normalization (APFS)")
	}
	if got := FindFile(want); got != disk {
		t.Errorf("FindFile(NFC) = %q, want %q", got, disk)
	}
	s, err := Open(want)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got := FindFile(filepath.Join(dir, "missing.flac")); got != filepath.Join(dir, "missing.flac") {
		t.Errorf("FindFile of a missing file = %q, want it unchanged", got)
	}
}
