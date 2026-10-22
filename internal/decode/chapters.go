package decode

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Los capítulos de un audiolibro: los de un M4B/M4A (los de Nero, "chpl", o
// la pista de texto de QuickTime que señala "tref/chap") y los de un MP3
// (marcos CHAP de ID3v2). Un libro en varios archivos no los lleva: cada
// archivo es un capítulo, y eso lo ve la interfaz.

// Chapter es un capítulo: dónde empieza (segundos) y su título.
type Chapter struct {
	Start float64
	Title string
}

// Chapters lee los capítulos de path, en orden; nil si no tiene (o no se
// pudieron leer).
func Chapters(path string) []Chapter {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m4b", ".m4a", ".mp4", ".aac":
		moov, err := readMoov(f)
		if err != nil {
			return nil
		}
		if ch := chplChapters(mp4Find(moov, "udta", "chpl")); len(ch) > 0 {
			return ch
		}
		return quicktimeChapters(f, moov)
	case ".mp3":
		var hdr [10]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil || string(hdr[:3]) != "ID3" {
			return nil
		}
		size := int(hdr[6]&0x7f)<<21 | int(hdr[7]&0x7f)<<14 | int(hdr[8]&0x7f)<<7 | int(hdr[9]&0x7f)
		if size > 16<<20 {
			return nil
		}
		tag := make([]byte, size)
		if _, err := io.ReadFull(f, tag); err != nil {
			return nil
		}
		return id3Chapters(hdr[3], tag)
	}
	return nil
}

// chplChapters lee los capítulos de Nero: versión, banderas, (en la 1, 4
// bytes más), cuántos, y cada uno: comienzo (en cien nanosegundos), largo del
// título y el título.
func chplChapters(b []byte) []Chapter {
	if len(b) < 5 {
		return nil
	}
	i := 4
	if b[0] == 1 {
		i += 4
	}
	if i >= len(b) {
		return nil
	}
	n := int(b[i])
	i++
	var out []Chapter
	for k := 0; k < n && i+9 <= len(b); k++ {
		start := binary.BigEndian.Uint64(b[i:])
		l := int(b[i+8])
		i += 9
		if i+l > len(b) {
			break
		}
		out = append(out, Chapter{Start: float64(start) / 1e7, Title: string(b[i : i+l])})
		i += l
	}
	return out
}

// quicktimeChapters lee la pista de texto que la de audio señala como sus
// capítulos (tref/chap): cada muestra es un título (dos bytes de largo y el
// texto) y empieza cuando empieza la muestra.
func quicktimeChapters(f *os.File, moov []byte) []Chapter {
	be := binary.BigEndian
	var chapID uint32
	traks := map[uint32][]byte{}
	for _, c := range mp4Children(moov) {
		if c.typ != "trak" {
			continue
		}
		var id uint32
		if tkhd := mp4Find(c.data, "tkhd"); len(tkhd) >= 24 {
			if tkhd[0] == 1 {
				id = be.Uint32(tkhd[20:])
			} else {
				id = be.Uint32(tkhd[12:])
			}
		}
		traks[id] = c.data
		if chap := mp4Find(c.data, "tref", "chap"); len(chap) >= 4 && chapID == 0 {
			chapID = be.Uint32(chap)
		}
	}
	trak, ok := traks[chapID]
	if !ok || chapID == 0 {
		return nil
	}
	mdia := mp4Find(trak, "mdia")
	t := &mp4Track{}
	if mdhd := mp4Find(mdia, "mdhd"); len(mdhd) >= 24 && mdhd[0] == 0 {
		t.timescale = int64(be.Uint32(mdhd[12:]))
	} else if len(mdhd) >= 32 {
		t.timescale = int64(be.Uint32(mdhd[20:]))
	}
	if t.timescale <= 0 || t.parseTables(mp4Find(mdia, "minf", "stbl")) != nil {
		return nil
	}
	var out []Chapter
	for i := 0; i < t.samples() && i < 1000; i++ {
		size := min(int(t.sizes[i]), 1024)
		buf := make([]byte, size)
		if size < 2 {
			continue
		}
		if _, err := f.ReadAt(buf, t.offsets[i]); err != nil {
			break
		}
		l := min(int(be.Uint16(buf)), size-2)
		out = append(out, Chapter{Start: float64(t.starts[i]) / float64(t.timescale), Title: string(buf[2 : 2+l])})
	}
	return out
}

// id3Chapters lee los marcos CHAP de una etiqueta ID3v2 (versión 3 o 4):
// identificador, comienzo y fin (ms), dos desplazamientos y, dentro, sus
// propios marcos, de los que sale el título (TIT2).
func id3Chapters(version byte, tag []byte) []Chapter {
	be := binary.BigEndian
	frameSize := func(b []byte) int {
		if version == 4 {
			return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
		}
		return int(be.Uint32(b))
	}
	var out []Chapter
	for i := 0; i+10 <= len(tag); {
		id := string(tag[i : i+4])
		if id[0] == 0 {
			break // relleno
		}
		size := frameSize(tag[i+4:])
		body := tag[i+10:]
		if size < 0 || size > len(body) {
			break
		}
		body = body[:size]
		i += 10 + size
		if id != "CHAP" {
			continue
		}
		z := strings.IndexByte(string(body), 0)
		if z < 0 || z+17 > len(body) {
			continue
		}
		ch := Chapter{Start: float64(be.Uint32(body[z+1:])) / 1000}
		sub := body[z+17:]
		for j := 0; j+10 <= len(sub); {
			sid := string(sub[j : j+4])
			ssize := frameSize(sub[j+4:])
			if ssize <= 0 || j+10+ssize > len(sub) {
				break
			}
			if sid == "TIT2" {
				ch.Title = id3Text(sub[j+10 : j+10+ssize])
			}
			j += 10 + ssize
		}
		out = append(out, ch)
	}
	// Los CHAP pueden venir en cualquier orden.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Start < out[j-1].Start; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// id3Text lee un marco de texto de ID3: la codificación y el texto.
func id3Text(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	enc, b := b[0], b[1:]
	switch enc {
	case 1, 2: // UTF-16 (con BOM o big-endian)
		bigEndian := enc == 2
		if len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF) {
			bigEndian = b[0] == 0xFE
			b = b[2:]
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			v := uint16(b[i])<<8 | uint16(b[i+1])
			if !bigEndian {
				v = uint16(b[i+1])<<8 | uint16(b[i])
			}
			if v == 0 {
				break
			}
			u = append(u, v)
		}
		return string(utf16.Decode(u))
	default: // Latin-1 o UTF-8
		s := strings.TrimRight(string(b), "\x00")
		if enc == 0 {
			r := []rune{}
			for _, c := range []byte(s) {
				r = append(r, rune(c))
			}
			return string(r)
		}
		return s
	}
}
