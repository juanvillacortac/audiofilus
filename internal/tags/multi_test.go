package tags

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// comments es un bloque de comentarios Vorbis con fields ("CAMPO=valor").
func comments(fields ...string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, 4)
	b = append(b, "test"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(fields)))
	for _, f := range fields {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(f)))
		b = append(b, f...)
	}
	return b
}

// flacWith es el FLAC de prueba de internal/decode con sus comentarios
// Vorbis cambiados por fields.
func flacWith(t *testing.T, fields ...string) []byte {
	t.Helper()
	src, err := os.ReadFile("../decode/testdata/tone.flac")
	if err != nil {
		t.Fatal(err)
	}
	out := []byte("fLaC")
	p := 4
	for {
		h := src[p]
		size := int(src[p+1])<<16 | int(src[p+2])<<8 | int(src[p+3])
		if h&0x7f != 4 {
			out = append(out, h&0x7f)
			out = append(out, src[p+1:p+4+size]...)
		}
		p += 4 + size
		if h&0x80 != 0 {
			break
		}
	}
	c := comments(fields...)
	out = append(out, 0x80|4, byte(len(c)>>16), byte(len(c)>>8), byte(len(c)))
	out = append(out, c...)
	return append(out, src[p:]...)
}

// TestFLACMultipleArtists comprueba que varios ARTIST y ALBUMARTIST de un
// FLAC se muestren todos, unidos con ", " (dhowden/tag solo daba el
// último).
func TestFLACMultipleArtists(t *testing.T) {
	m, err := ReadFrom(bytes.NewReader(flacWith(t, "TITLE=Song", "ARTIST=Alpha", "ARTIST=Beta", "ALBUMARTIST=Alpha", "ALBUMARTIST=Gamma")))
	if err != nil {
		t.Fatal(err)
	}
	if m.Artist() != "Alpha, Beta" || m.AlbumArtist() != "Alpha, Gamma" || m.Title() != "Song" {
		t.Errorf("artist %q, album artist %q, title %q", m.Artist(), m.AlbumArtist(), m.Title())
	}
}

// TestFLACSingleArtistUntouched comprueba que un solo ARTIST con varios
// nombres escritos a mano quede tal cual, y que ARTISTS (la lista de
// MusicBrainz Picard) no lo cambie.
func TestFLACSingleArtistUntouched(t *testing.T) {
	m, err := ReadFrom(bytes.NewReader(flacWith(t, "ARTIST=Alpha feat. Beta & Gamma", "ARTISTS=Alpha", "ARTISTS=Beta", "ARTISTS=Gamma")))
	if err != nil {
		t.Fatal(err)
	}
	if m.Artist() != "Alpha feat. Beta & Gamma" {
		t.Errorf("artist %q", m.Artist())
	}
}

// TestFLACArtistsFallback comprueba que, sin ARTIST, se usen los ARTISTS.
func TestFLACArtistsFallback(t *testing.T) {
	m, err := ReadFrom(bytes.NewReader(flacWith(t, "TITLE=Song", "ARTISTS=Alpha", "ARTISTS=Beta")))
	if err != nil {
		t.Fatal(err)
	}
	if m.Artist() != "Alpha, Beta" {
		t.Errorf("artist %q", m.Artist())
	}
}

// TestFLACAfterID3v2 comprueba que en un FLAC con un ID3v2 (vacío) delante
// se lean las etiquetas del FLAC, no el ID3.
func TestFLACAfterID3v2(t *testing.T) {
	pad := make([]byte, 100)
	id3 := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, byte(len(pad))}, pad...)
	m, err := ReadFrom(bytes.NewReader(append(id3, flacWith(t, "TITLE=Song", "ARTIST=Alpha", "ARTIST=Beta")...)))
	if err != nil {
		t.Fatal(err)
	}
	if m.Title() != "Song" || m.Artist() != "Alpha, Beta" {
		t.Errorf("title %q, artist %q", m.Title(), m.Artist())
	}
}

// id3Tag es una etiqueta ID3v2 de versión ver con frames (id y datos ya
// codificados).
func id3Tag(ver byte, frames ...[2]string) []byte {
	var body []byte
	for _, f := range frames {
		body = append(body, f[0]...)
		n := len(f[1])
		if ver == 4 {
			body = append(body, byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f))
		} else {
			body = binary.BigEndian.AppendUint32(body, uint32(n))
		}
		body = append(body, 0, 0)
		body = append(body, f[1]...)
	}
	body = append(body, make([]byte, 64)...) // relleno
	n := len(body)
	h := []byte{'I', 'D', '3', ver, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(h, body...)
}

// TestID3MultipleArtists comprueba que un TPE1 con varios valores separados
// por ceros (ID3v2.4, en UTF-8 y en UTF-16 con su BOM cada uno; y un v2.3
// que lo imita) dé los artistas unidos con ", ", no pegados ("AlphaBeta").
func TestID3MultipleArtists(t *testing.T) {
	utf16le := func(s string) string { return string(u16(s, false)) }
	bom := "\xff\xfe"
	for name, tag := range map[string][]byte{
		"v2.4 utf-8": id3Tag(4, [2]string{"TIT2", "\x03Song"}, [2]string{"TPE1", "\x03Alpha\x00Beta"}, [2]string{"TPE2", "\x03Alpha"}),
		"v2.4 utf-16": id3Tag(4, [2]string{"TIT2", "\x03Song"},
			[2]string{"TPE1", "\x01" + bom + utf16le("Alpha") + "\x00\x00" + bom + utf16le("Beta") + "\x00\x00"},
			[2]string{"TPE2", "\x01" + bom + utf16le("Alpha")}),
		"v2.3 latin-1": id3Tag(3, [2]string{"TIT2", "\x00Song"}, [2]string{"TPE1", "\x00Alpha\x00Beta\x00"}, [2]string{"TPE2", "\x00Alpha"}),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := ReadFrom(bytes.NewReader(append(tag, make([]byte, 256)...)))
			if err != nil {
				t.Fatal(err)
			}
			if m.Artist() != "Alpha, Beta" || m.AlbumArtist() != "Alpha" || m.Title() != "Song" {
				t.Errorf("artist %q, album artist %q, title %q", m.Artist(), m.AlbumArtist(), m.Title())
			}
		})
	}
}

// oggCRC es el CRC de una página Ogg (polinomio 0x04c11db7, sin reflejar).
func oggCRC(b []byte) uint32 {
	var crc uint32
	for _, v := range b {
		crc ^= uint32(v) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// oggPage es una página Ogg con un paquete entero.
func oggPage(seq uint32, flags byte, pkt []byte) []byte {
	var segs []byte
	n := len(pkt)
	for ; n >= 255; n -= 255 {
		segs = append(segs, 255)
	}
	segs = append(segs, byte(n))
	h := []byte("OggS")
	h = append(h, 0, flags)
	h = append(h, make([]byte, 8)...) // posición
	h = binary.LittleEndian.AppendUint32(h, 1)
	h = binary.LittleEndian.AppendUint32(h, seq)
	h = append(h, 0, 0, 0, 0) // CRC
	h = append(h, byte(len(segs)))
	page := append(append(h, segs...), pkt...)
	binary.LittleEndian.PutUint32(page[22:], oggCRC(page))
	return page
}

// TestOpusMultipleArtists comprueba lo mismo que con FLAC en un Opus: los
// comentarios Vorbis del paquete OpusTags.
func TestOpusMultipleArtists(t *testing.T) {
	head := []byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00")
	tags := append([]byte("OpusTags"), comments("TITLE=Song", "ARTIST=Alpha", "ARTIST=Beta")...)
	file := append(oggPage(0, 2, head), oggPage(1, 0, tags)...)
	m, err := ReadFrom(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if m.Artist() != "Alpha, Beta" || m.Title() != "Song" {
		t.Errorf("artist %q, title %q", m.Artist(), m.Title())
	}
}
