package lyrics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
)

// ParseSYLT lee un frame SYLT de ID3v2 (la letra sincronizada de los MP3),
// sin su cabecera de frame. Solo entiende tiempos en milisegundos, que es lo
// que escriben los editores de etiquetas.
//
// Cada entrada es un trozo de texto con su tiempo: unos ponen una línea por
// entrada y otros una palabra, con "\n" delante de cada línea nueva.
func ParseSYLT(b []byte) (Lyrics, error) {
	if len(b) < 6 {
		return Lyrics{}, errors.New("sylt: frame too short")
	}
	enc, format := b[0], b[4]
	if format != 2 {
		return Lyrics{}, errors.New("sylt: timestamps are not in milliseconds")
	}
	rest := b[6:]
	// El descriptor del contenido, que no interesa.
	_, rest, ok := cutText(rest, enc)
	if !ok {
		return Lyrics{}, errors.New("sylt: no content")
	}
	type entry struct {
		text string
		at   time.Duration
	}
	var entries []entry
	newlines := false
	for len(rest) > 0 {
		var t string
		t, rest, ok = cutText(rest, enc)
		if !ok || len(rest) < 4 {
			break
		}
		at := time.Duration(binary.BigEndian.Uint32(rest)) * time.Millisecond
		rest = rest[4:]
		if strings.HasPrefix(t, "\n") || strings.HasPrefix(t, "\r") {
			newlines = true
		}
		entries = append(entries, entry{t, at})
	}
	if len(entries) == 0 {
		return Lyrics{}, errors.New("sylt: no lines")
	}
	var out []Line
	for _, e := range entries {
		if newlines && len(out) > 0 && !strings.HasPrefix(e.text, "\n") && !strings.HasPrefix(e.text, "\r") {
			out[len(out)-1].Text += e.text // una palabra más de la misma línea
			continue
		}
		out = append(out, Line{At: e.at, Text: e.text})
	}
	for i := range out {
		out[i].Text = strings.TrimSpace(out[i].Text)
	}
	return Lyrics{Lines: out, Synced: true}, nil
}

// cutText lee un texto terminado en nulo con la codificación de ID3 (0
// Latin-1, 1 UTF-16 con BOM, 2 UTF-16BE, 3 UTF-8) y devuelve lo que sigue.
func cutText(b []byte, enc byte) (string, []byte, bool) {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return decodeUTF16(b[:i], enc == 2), b[i+2:], true
			}
		}
		return "", nil, false
	}
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return "", nil, false
	}
	if enc == 0 {
		r := make([]rune, i)
		for j, c := range b[:i] {
			r[j] = rune(c)
		}
		return string(r), b[i+1:], true
	}
	return string(b[:i]), b[i+1:], true
}

func decodeUTF16(b []byte, bigEndian bool) string {
	if len(b) >= 2 && !bigEndian {
		switch {
		case b[0] == 0xFE && b[1] == 0xFF:
			bigEndian, b = true, b[2:]
		case b[0] == 0xFF && b[1] == 0xFE:
			b = b[2:]
		}
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if bigEndian {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}
