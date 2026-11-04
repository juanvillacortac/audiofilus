package tags

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"unicode/utf16"
)

// TestWMAFromFFmpeg comprueba las etiquetas de un WMA escrito por ffmpeg
// (el tono de internal/decode/testdata): título y autor en la descripción,
// álbum y pista en la extendida.
func TestWMAFromFFmpeg(t *testing.T) {
	f, err := os.Open("../decode/testdata/tone.wma")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := ReadFrom(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format() != ASF || m.FileType() != WMA {
		t.Errorf("format %q, file type %q", m.Format(), m.FileType())
	}
	if m.Title() != "Prueba wma" || m.Artist() != "audiofilus" || m.Album() != "Tonos" {
		t.Errorf("title %q, artist %q, album %q", m.Title(), m.Artist(), m.Album())
	}
	if n, _ := m.Track(); n != 3 {
		t.Errorf("track %d, want 3", n)
	}
}

// u16 es s en UTF-16LE, con el cero del final si z.
func u16(s string, z bool) []byte {
	var b bytes.Buffer
	for _, c := range utf16.Encode([]rune(s)) {
		_ = binary.Write(&b, binary.LittleEndian, c)
	}
	if z {
		b.Write([]byte{0, 0})
	}
	return b.Bytes()
}

// object es un objeto ASF: GUID, tamaño (con la cabecera) y datos.
func object(id [16]byte, data []byte) []byte {
	b := append(id[:], make([]byte, 8)...)
	binary.LittleEndian.PutUint64(b[16:], uint64(24+len(data)))
	return append(b, data...)
}

// TestASFFields comprueba un ASF hecho a mano como los de Windows Media
// Player: el año como fecha, la pista como "3/12", el disco, la ganancia, y
// la carátula grande en la Metadata Library (dentro de la extensión de la
// cabecera), que es donde va cuando no cabe en 64 KB.
func TestASFFields(t *testing.T) {
	str := func(name, v string) []byte {
		var b bytes.Buffer
		n, val := u16(name, true), u16(v, true)
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(n)))
		b.Write(n)
		_ = binary.Write(&b, binary.LittleEndian, [2]uint16{0, uint16(len(val))})
		b.Write(val)
		return b.Bytes()
	}
	ext := []byte{4, 0}
	ext = append(ext, str("WM/Year", "2004-05-01")...)
	ext = append(ext, str("WM/TrackNumber", "3/12")...)
	ext = append(ext, str("WM/PartOfSet", "2/2")...)
	ext = append(ext, str("REPLAYGAIN_TRACK_GAIN", "-6.5 dB")...)

	img := bytes.Repeat([]byte{0xAB}, 70000)
	pic := []byte{3}
	pic = binary.LittleEndian.AppendUint32(pic, uint32(len(img)))
	pic = append(pic, u16("image/png", true)...)
	pic = append(pic, u16("", true)...)
	pic = append(pic, img...)
	name := u16("WM/Picture", true)
	lib := []byte{1, 0, 0, 0, 0, 0}
	lib = binary.LittleEndian.AppendUint16(lib, uint16(len(name)))
	lib = binary.LittleEndian.AppendUint16(lib, 1) // bytes
	lib = binary.LittleEndian.AppendUint32(lib, uint32(len(pic)))
	lib = append(lib, name...)
	lib = append(lib, pic...)
	inner := object(guidMetadataLib, lib)
	hext := make([]byte, 18)
	hext = binary.LittleEndian.AppendUint32(hext, uint32(len(inner)))
	hext = append(hext, inner...)

	body := append(object(guidExtContent, ext), object(guidHeaderExt, hext)...)
	hdr := object(guidHeader, append([]byte{2, 0, 0, 0, 1, 2}, body...))

	m, err := ReadFrom(bytes.NewReader(hdr))
	if err != nil {
		t.Fatal(err)
	}
	if m.Year() != 2004 {
		t.Errorf("year %d, want 2004", m.Year())
	}
	if n, total := m.Track(); n != 3 || total != 12 {
		t.Errorf("track %d/%d, want 3/12", n, total)
	}
	if n, total := m.Disc(); n != 2 || total != 2 {
		t.Errorf("disc %d/%d, want 2/2", n, total)
	}
	if m.Raw()["REPLAYGAIN_TRACK_GAIN"] != "-6.5 dB" {
		t.Errorf("gain %v", m.Raw()["REPLAYGAIN_TRACK_GAIN"])
	}
	p := m.Picture()
	if p == nil || p.MIMEType != "image/png" || p.Ext != "png" || !bytes.Equal(p.Data, img) {
		t.Errorf("picture not read from the metadata library: %+v", p)
	}
}

// TestASFTruncated comprueba que una cabecera cortada dé error en vez de
// leer fuera.
func TestASFTruncated(t *testing.T) {
	hdr := object(guidHeader, append([]byte{1, 0, 0, 0, 1, 2}, object(guidExtContent, []byte{9, 0, 50, 0})...))
	binary.LittleEndian.PutUint64(hdr[16:], 1<<40)
	if _, err := ReadFrom(bytes.NewReader(hdr)); err == nil {
		t.Errorf("a header claiming 1 TB was accepted")
	}
	hdr = object(guidHeader, append([]byte{1, 0, 0, 0, 1, 2}, object(guidExtContent, []byte{9, 0, 50, 0})...))
	if _, err := ReadFrom(bytes.NewReader(hdr)); err != nil {
		t.Errorf("a short extended description should be skipped, got %v", err)
	}
}

// TestNotASF comprueba que lo que no es ASF siga yendo a dhowden/tag.
func TestNotASF(t *testing.T) {
	f, err := os.Open("../decode/testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := ReadFrom(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title() != "Prueba flac" {
		t.Errorf("title %q", m.Title())
	}
}
