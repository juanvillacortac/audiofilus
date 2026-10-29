package decode

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeSID escribe en dir/MUSICIANS/T/t.sid un PSID de dos canciones que
// suenan una sierra en la voz 1, y la lista de duraciones de la HVSC
// (DOCUMENTS/Songlengths.md5 en dir) que dice que la 1 dura length y la 2,
// 1 s. Devuelve la ruta del .sid.
func writeSID(t *testing.T, dir, length string) string {
	t.Helper()
	h := make([]byte, 0x7c)
	copy(h, "PSID")
	be := binary.BigEndian
	be.PutUint16(h[4:], 2)
	be.PutUint16(h[6:], 0x7c)
	be.PutUint16(h[8:], 0x1000)  // carga
	be.PutUint16(h[10:], 0x1000) // inicio
	be.PutUint16(h[12:], 0x1020) // tocar
	be.PutUint16(h[14:], 2)
	be.PutUint16(h[16:], 1)
	copy(h[0x16:], "Chip Test")
	copy(h[0x36:], "Tester")
	code := make([]byte, 0x21)
	copy(code, []byte{
		0xa9, 0x0f, 0x8d, 0x18, 0xd4, // volumen
		0xa9, 0xf0, 0x8d, 0x06, 0xd4, // sostenido máximo
		0xa9, 0x1d, 0x8d, 0x01, 0xd4, // frecuencia
		0xa9, 0x21, 0x8d, 0x04, 0xd4, // sierra y puerta
		0x60,
	})
	code[0x20] = 0x60
	data := append(h, code...)
	sub := filepath.Join(dir, "MUSICIANS", "T")
	os.MkdirAll(sub, 0o755)
	os.MkdirAll(filepath.Join(dir, "DOCUMENTS"), 0o755)
	path := filepath.Join(sub, "t.sid")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	db := hex.EncodeToString(sum[:]) + "=" + length + " 0:01\n"
	os.WriteFile(filepath.Join(dir, "DOCUMENTS", "Songlengths.md5"), []byte(db), 0o644)
	return path
}

// TestChipSongs comprueba los .sid (internal/chip) por el camino de libgme,
// sin libgme: dos canciones con su ruta, que duran lo que dice la lista de
// la HVSC, suenan, y saltar hacia atrás y hacia delante deja la posición.
func TestChipSongs(t *testing.T) {
	path := writeSID(t, t.TempDir(), "0:02")
	if !IsAudioFile(path) || !IsGMEFile(path) {
		t.Fatal("a .sid isn't listed as playable")
	}
	tracks, err := GMETracks(path)
	if err != nil || len(tracks) != 2 || tracks[0].Game != "Chip Test" || tracks[0].Length != 2000 || tracks[1].Length != 1000 {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	s, err := Open(GMEPath(path, 2))
	if err != nil {
		t.Fatal(err)
	}
	samples := readAll(t, s)
	if got := float64(len(samples)/2) / float64(s.SampleRate()); math.Abs(got-1) > 0.01 {
		t.Errorf("song 2: %.3f s, want 1", got)
	}
	if pk := peak(samples); pk < 0.05 {
		t.Errorf("peak %.3f, the saw is silent", pk)
	}
	for _, to := range []int64{12000, 3000, 40000} {
		if err := s.SeekFrame(to); err != nil || s.Position() != to {
			t.Errorf("seek to %d: position %d, %v", to, s.Position(), err)
		}
	}
	s.Close()
}

// TestChipFade comprueba que una canción que da la vuelta (sin duración en
// la lista: aquí, "0:00") se apague al final: lo último es más bajo que el
// principio.
func TestChipFade(t *testing.T) {
	path := writeSID(t, t.TempDir(), "0:00")
	tracks, _ := GMETracks(path)
	if !tracks[0].Loops {
		t.Fatalf("song 1 = %+v, want a looping one", tracks[0])
	}
	s, err := Open(GMEPath(path, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	buf := make([]float32, 9600)
	s.Read(buf)
	start := peak(buf)
	s.SeekFrame(s.Length() - 2400)
	n, _ := s.Read(buf)
	if end := peak(buf[:n]); n == 0 || end > start/4 {
		t.Errorf("peak %.3f at the end (start %.3f, %d samples): no fade", end, start, n)
	}
}
