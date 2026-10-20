package decode

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf16"
)

// Los tests usan la libvgmshim que compila tools/libvgm/build.sh en build/
// (si está), además de la que haya junto al binario o en el sistema.
func init() {
	arch := map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH]
	lib, err := filepath.Abs(filepath.Join("..", "..", "build", "libvgm", "libs."+arch, "libvgmshim.so"))
	if _, serr := os.Stat(lib); err == nil && serr == nil {
		vgmLibNames = append([]string{lib}, vgmLibNames...)
	}
}

// writeYM2151 escribe un VGM de secs segundos con un tono del YM2151 (el chip
// de las placas arcade de Sega y Capcom, que libgme no emula) y etiquetas
// GD3: el título en inglés y el juego solo en japonés.
func writeYM2151(t *testing.T, path string, secs int) {
	t.Helper()
	const hdrLen = 0x80
	reg := func(r, v byte) []byte { return []byte{0x54, r, v} }
	var data []byte
	data = append(data, reg(0x20, 0xC7)...) // canal 0: los dos lados, algoritmo 7
	data = append(data, reg(0x28, 0x4A)...) // nota La de la octava 4
	data = append(data, reg(0x30, 0x00)...)
	for _, op := range []byte{0, 8, 16, 24} { // los cuatro operadores del canal 0
		data = append(data, reg(0x40+op, 0x01)...) // MUL 1
		data = append(data, reg(0x60+op, 0x08)...) // TL alto (fuerte)
		data = append(data, reg(0x80+op, 0x1F)...) // ataque instantáneo
		data = append(data, reg(0xA0+op, 0x00)...)
		data = append(data, reg(0xC0+op, 0x00)...)
		data = append(data, reg(0xE0+op, 0x0F)...)
	}
	data = append(data, reg(0x08, 0x78)...) // nota pulsada en los cuatro operadores
	for left := 44100 * secs; left > 0; {
		n := min(left, 65535)
		data = append(data, 0x61, byte(n), byte(n>>8))
		left -= n
	}
	data = append(data, 0x66)
	var tags []byte
	for _, s := range []string{"Tone", "", "", "Arcade Test", "Arcade", "", "Tester", "", "2026", "", ""} {
		for _, u := range utf16.Encode([]rune(s)) {
			tags = binary.LittleEndian.AppendUint16(tags, u)
		}
		tags = append(tags, 0, 0)
	}
	gd3 := append([]byte("Gd3 \x00\x01\x00\x00"), binary.LittleEndian.AppendUint32(nil, uint32(len(tags)))...)
	gd3 = append(gd3, tags...)
	hdr := make([]byte, hdrLen)
	copy(hdr, "Vgm ")
	binary.LittleEndian.PutUint32(hdr[0x04:], uint32(hdrLen+len(data)+len(gd3)-4))
	binary.LittleEndian.PutUint32(hdr[0x08:], 0x151)
	binary.LittleEndian.PutUint32(hdr[0x14:], uint32(hdrLen+len(data)-0x14))
	binary.LittleEndian.PutUint32(hdr[0x18:], uint32(44100*secs))
	binary.LittleEndian.PutUint32(hdr[0x30:], 3579545) // YM2151
	binary.LittleEndian.PutUint32(hdr[0x34:], hdrLen-0x34)
	file := append(append(hdr, data...), gd3...)
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestVGMFullChips comprueba los VGM con libvgm (si está): uno del YM2151 se
// lista con sus etiquetas (el juego, del japonés a falta del inglés) y sus
// 2 s, suena (no es silencio), dura eso, y saltar deja la posición.
func TestVGMFullChips(t *testing.T) {
	if !VGMFullChips() {
		t.Skip("libvgmshim not built (tools/libvgm/build.sh build/libvgm)")
	}
	path := filepath.Join(t.TempDir(), "ym2151.vgm")
	writeYM2151(t, path, 2)
	tracks, err := GMETracks(path)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	tr := tracks[0]
	if tr.Title != "Tone" || tr.Game != "Arcade Test" || tr.Author != "Tester" || tr.System != "Arcade" || tr.Length != 2000 || tr.Loops {
		t.Errorf("track = %+v", tr)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.SampleRate() != 48000 || s.Length() != 96000 {
		t.Errorf("rate %d, length %d", s.SampleRate(), s.Length())
	}
	samples := readAll(t, s)
	if got := float64(len(samples)/2) / 48000; math.Abs(got-2) > 0.05 {
		t.Errorf("rendered %.2f s, want 2", got)
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v) * float64(v)
	}
	if rms := math.Sqrt(sum / float64(len(samples))); rms < 0.01 {
		t.Errorf("rms %.4f: silence", rms)
	}
	if err := s.SeekFrame(24000); err != nil || s.Position() != 24000 {
		t.Errorf("seek: pos %d, %v", s.Position(), err)
	}
	if rest := readAll(t, s); math.Abs(float64(len(rest)/2)-72000) > 2400 {
		t.Errorf("after seek: %d frames, want 72000", len(rest)/2)
	}
	s.Close()
}
