package tagwrite

import (
	"bytes"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MP3: la etiqueta ID3v2 del principio. Se lee la 2.3 y la 2.4 (y la 2.2,
// que se pasa a 2.4 quedándose solo con lo que se entiende); se escribe en
// la misma versión que tenía (2.4 si no tenía), conservando los marcos que
// no se tocan byte a byte. Lo de detrás de la etiqueta (el audio, y un
// ID3v1 o APE al final) se copia tal cual. Si la etiqueta nueva cabe en la
// vieja (con su relleno), ocupa lo mismo; si no, se deja 1 KB de relleno.

type id3Frame struct {
	id    string
	flags [2]byte
	data  []byte // el contenido tal como está en el archivo (sin cabecera)
}

type mp3File struct {
	version byte // 3 o 4
	frames  []id3Frame
	size    int // lo que ocupaba la etiqueta entera (cabecera incluida)
	audio   []byte
}

func parseMP3(data []byte) (*mp3File, error) {
	f := &mp3File{version: 4}
	if len(data) < 10 || string(data[:3]) != "ID3" {
		f.audio = data
		return f, nil
	}
	ver, flags := data[3], data[5]
	n := synchsafe(data[6:10])
	end := 10 + n
	if flags&0x10 != 0 {
		end += 10 // pie (2.4)
	}
	if end > len(data) || data[6]|data[7]|data[8]|data[9] >= 0x80 {
		return nil, ErrCorrupt
	}
	f.size, f.audio = end, data[end:]
	body := data[10 : 10+n]
	if ver < 4 && flags&0x80 != 0 {
		body = unsync(body) // la etiqueta entera "desincronizada" (2.2, 2.3)
	}
	if flags&0x40 != 0 { // cabecera extendida: se descarta
		if len(body) < 4 {
			return nil, ErrCorrupt
		}
		// Los tamaños se comparan en uint64: un int de 32 bits (Android
		// armeabi-v7a) con un tamaño de 4 GB queda negativo y pasaría.
		var ext uint64
		if ver == 4 {
			ext = uint64(synchsafe(body[:4]))
		} else {
			ext = uint64(binary.BigEndian.Uint32(body[:4])) + 4
		}
		if ext > uint64(len(body)) {
			return nil, ErrCorrupt
		}
		body = body[ext:]
	}
	switch ver {
	case 2:
		return f, f.parse22(body)
	case 3, 4:
		f.version = ver
	default:
		return nil, ErrUnsupported
	}
	for pos := 0; pos+10 <= len(body); {
		id := string(body[pos : pos+4])
		if id[0] == 0 {
			break // el relleno
		}
		var size64 uint64
		if ver == 4 {
			size64 = uint64(synchsafe(body[pos+4 : pos+8]))
		} else {
			size64 = uint64(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		}
		if uint64(pos)+10+size64 > uint64(len(body)) || !validID(id) {
			return nil, ErrCorrupt
		}
		size := int(size64)
		f.frames = append(f.frames, id3Frame{id: id, flags: [2]byte{body[pos+8], body[pos+9]}, data: body[pos+10 : pos+10+size]})
		pos += 10 + size
	}
	return f, nil
}

// validID dice si id es un identificador de marco (A-Z, 0-9).
func validID(id string) bool {
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// unsync deshace la "desincronización" de ID3 (0xff 0x00 → 0xff).
func unsync(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xff && i+1 < len(b) && b[i+1] == 0 {
			i++
		}
	}
	return out
}

// parse22 lee una etiqueta 2.2 (identificadores de 3 letras) y se queda
// con lo que entiende, como marcos de 2.4.
func (f *mp3File) parse22(body []byte) error {
	f.version = 4
	conv := map[string]string{"TT2": "TIT2", "TP1": "TPE1", "TAL": "TALB", "TP2": "TPE2", "TCO": "TCON",
		"TCM": "TCOM", "TRK": "TRCK", "TPA": "TPOS", "TYE": "TDRC", "COM": "COMM", "TXX": "TXXX"}
	for pos := 0; pos+6 <= len(body); {
		id := string(body[pos : pos+3])
		if id[0] == 0 {
			break
		}
		size := int(body[pos+3])<<16 | int(body[pos+4])<<8 | int(body[pos+5])
		if pos+6+size > len(body) {
			return ErrCorrupt
		}
		d := body[pos+6 : pos+6+size]
		switch {
		case conv[id] != "":
			f.frames = append(f.frames, id3Frame{id: conv[id], data: d})
		case id == "PIC" && len(d) > 5:
			// PIC: codificación, formato de 3 letras, tipo, descripción, datos.
			mime := "image/" + strings.ToLower(strings.TrimSpace(string(d[1:4])))
			if mime == "image/jpg" {
				mime = "image/jpeg"
			}
			apic := append([]byte{d[0]}, mime...)
			apic = append(apic, 0)
			apic = append(apic, d[4:]...)
			f.frames = append(f.frames, id3Frame{id: "APIC", data: apic})
		}
		pos += 6 + size
	}
	return nil
}

// frameBody es el contenido de un marco ya sin la desincronización ni el
// indicador de longitud de 2.4 (nil si está comprimido o cifrado).
func (f *mp3File) frameBody(fr id3Frame) []byte {
	d := fr.data
	if f.version == 4 {
		if fr.flags[1]&0x0c != 0 { // comprimido o cifrado
			return nil
		}
		if fr.flags[1]&0x02 != 0 {
			d = unsync(d)
		}
		if fr.flags[1]&0x01 != 0 {
			if len(d) < 4 {
				return nil
			}
			d = d[4:]
		}
	} else if fr.flags[1]&0xc0 != 0 { // 2.3: comprimido o cifrado
		return nil
	}
	return d
}

// decodeText lee un texto de ID3 (con su byte de codificación); devuelve
// los valores separados por 0.
func decodeText(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	enc, b := b[0], b[1:]
	var s string
	switch enc {
	case 1, 2:
		s = decodeUTF16(b, enc == 2)
	case 3:
		s = string(b)
	default:
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		s = string(r)
	}
	s = strings.TrimRight(s, "\x00")
	return strings.Split(s, "\x00")
}

// decodeUTF16 lee UTF-16 (con BOM, o big endian sin él si be).
func decodeUTF16(b []byte, be bool) string {
	if len(b) >= 2 {
		switch {
		case b[0] == 0xff && b[1] == 0xfe:
			be, b = false, b[2:]
		case b[0] == 0xfe && b[1] == 0xff:
			be, b = true, b[2:]
		}
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if be {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
	}
	return string(utf16.Decode(u))
}

// encodeText escribe un texto: UTF-8 en 2.4; en 2.3, Latin-1 si se puede y
// si no UTF-16 con BOM.
func (f *mp3File) encodeText(s string) []byte {
	if f.version == 4 {
		return append([]byte{3}, s...)
	}
	latin := true
	for _, r := range s {
		if r > 0xff {
			latin = false
			break
		}
	}
	if latin {
		b := []byte{0}
		for _, r := range s {
			b = append(b, byte(r))
		}
		return b
	}
	b := []byte{1, 0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// terminator es el fin de cadena de una codificación.
func terminator(enc byte) []byte {
	if enc == 1 || enc == 2 {
		return []byte{0, 0}
	}
	return []byte{0}
}

// splitTerm corta b en el primer fin de cadena de enc.
func splitTerm(b []byte, enc byte) (string, []byte) {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return decodeUTF16(b[:i], enc == 2), b[i+2:]
			}
		}
		return decodeUTF16(b, enc == 2), nil
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		if enc == 0 {
			return latin1(b[:i]), b[i+1:]
		}
		return string(b[:i]), b[i+1:]
	}
	return string(b), nil
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// text es el primer valor de un marco de texto.
func (f *mp3File) text(id string) string {
	for _, fr := range f.frames {
		if fr.id == id {
			if v := decodeText(f.frameBody(fr)); len(v) > 0 {
				return v[0]
			}
		}
	}
	return ""
}

// txxx es el valor de un TXXX por su descripción (sin mayúsculas).
func (f *mp3File) txxx(desc string) (string, bool) {
	for _, fr := range f.frames {
		if fr.id != "TXXX" {
			continue
		}
		b := f.frameBody(fr)
		if len(b) < 1 {
			continue
		}
		d, rest := splitTerm(b[1:], b[0])
		if strings.EqualFold(d, desc) {
			v := decodeText(append([]byte{b[0]}, rest...))
			if len(v) > 0 {
				return v[0], true
			}
			return "", true
		}
	}
	return "", false
}

// comment es el primer COMM sin descripción (o el primero).
func (f *mp3File) comment() string {
	first := ""
	for _, fr := range f.frames {
		if fr.id != "COMM" {
			continue
		}
		b := f.frameBody(fr)
		if len(b) < 5 {
			continue
		}
		d, rest := splitTerm(b[4:], b[0])
		v := decodeText(append([]byte{b[0]}, rest...))
		val := ""
		if len(v) > 0 {
			val = v[0]
		}
		if d == "" {
			return val
		}
		if first == "" {
			first = val
		}
	}
	return first
}

// apic lee una portada (la delantera si hay, si no la primera).
func (f *mp3File) apic() *Cover {
	var out *Cover
	for _, fr := range f.frames {
		if fr.id != "APIC" {
			continue
		}
		b := f.frameBody(fr)
		if len(b) < 4 {
			continue
		}
		enc := b[0]
		i := bytes.IndexByte(b[1:], 0)
		if i < 0 {
			continue
		}
		mime := string(b[1 : 1+i])
		rest := b[2+i:]
		if len(rest) < 1 {
			continue
		}
		kind := rest[0]
		_, data := splitTerm(rest[1:], enc)
		if data == nil {
			continue
		}
		if mime == "" || mime == "-->" {
			mime = sniffMIME(data)
		}
		if out == nil || kind == 3 {
			out = &Cover{MIME: mime, Data: data}
			if kind == 3 {
				break
			}
		}
	}
	return out
}

func (f *mp3File) tags() Tags {
	var t Tags
	t.Title = f.text("TIT2")
	t.Artist = f.text("TPE1")
	t.Album = f.text("TALB")
	t.AlbumArtist = f.text("TPE2")
	if g := f.text("TCON"); g != "" {
		t.Genre = genreName(g)
	}
	t.Composer = f.text("TCOM")
	t.Comment = f.comment()
	if f.version == 4 {
		t.Year = parseYear(f.text("TDRC"))
	}
	if t.Year == 0 {
		t.Year = parseYear(f.text("TYER"))
	}
	t.Track, t.TrackTotal = parseNumPair(f.text("TRCK"))
	t.Disc, t.DiscTotal = parseNumPair(f.text("TPOS"))
	t.ReplayGain = gainFromFields(func(k string) (string, bool) { return f.txxx(k) })
	t.Cover = f.apic()
	return t
}

// replace quita los marcos que diga drop y pone add donde estaba el primero
// quitado (o al final).
func (f *mp3File) replace(drop func(id3Frame) bool, add ...id3Frame) {
	at := -1
	var out []id3Frame
	for _, fr := range f.frames {
		if drop(fr) {
			if at < 0 {
				at = len(out)
			}
			continue
		}
		out = append(out, fr)
	}
	if at < 0 {
		at = len(out)
	}
	f.frames = append(out[:at], append(add, out[at:]...)...)
}

// setFrame pone (o quita, con v vacío) un marco de texto.
func (f *mp3File) setFrame(id, v string, also ...string) {
	drop := func(fr id3Frame) bool {
		if fr.id == id {
			return true
		}
		for _, a := range also {
			if fr.id == a {
				return true
			}
		}
		return false
	}
	if v == "" {
		f.replace(drop)
		return
	}
	f.replace(drop, id3Frame{id: id, data: f.encodeText(v)})
}

var id3Text = map[string]string{
	"title": "TIT2", "artist": "TPE1", "album": "TALB", "albumartist": "TPE2", "genre": "TCON", "composer": "TCOM",
}

func (f *mp3File) setText(field, v string) {
	if field == "comment" {
		// Solo los COMM sin descripción (los demás son de otros programas).
		drop := func(fr id3Frame) bool {
			if fr.id != "COMM" {
				return false
			}
			b := f.frameBody(fr)
			if len(b) < 5 {
				return true
			}
			d, _ := splitTerm(b[4:], b[0])
			return d == ""
		}
		if v == "" {
			f.replace(drop)
			return
		}
		t := f.encodeText(v)
		data := append([]byte{t[0]}, "eng"...)
		data = append(data, terminator(t[0])...) // descripción vacía
		data = append(data, t[1:]...)
		f.replace(drop, id3Frame{id: "COMM", data: data})
		return
	}
	f.setFrame(id3Text[field], v)
}

func (f *mp3File) setYear(y int) {
	v := ""
	if y > 0 {
		v = strconv.Itoa(y)
	}
	if f.version == 4 {
		f.setFrame("TDRC", v, "TYER")
	} else {
		f.setFrame("TYER", v, "TDRC")
	}
}

func (f *mp3File) setTrack(n, total int) { f.setFrame("TRCK", numPair(n, total)) }
func (f *mp3File) setDisc(n, total int)  { f.setFrame("TPOS", numPair(n, total)) }

// txxxFrame es un TXXX.
func (f *mp3File) txxxFrame(desc, v string) id3Frame {
	t := f.encodeText(desc)
	data := append(t, terminator(t[0])...)
	val := f.encodeText(v)
	if val[0] != t[0] { // el mismo código para los dos (2.3: UTF-16 si hace falta)
		val = append([]byte{t[0]}, f.encodeAs(v, t[0])...)
	}
	data = append(data, val[1:]...)
	return id3Frame{id: "TXXX", data: data}
}

// encodeAs escribe s en la codificación enc (sin su byte).
func (f *mp3File) encodeAs(s string, enc byte) []byte {
	switch enc {
	case 3:
		return []byte(s)
	case 1:
		b := []byte{0xff, 0xfe}
		for _, u := range utf16.Encode([]rune(s)) {
			b = append(b, byte(u), byte(u>>8))
		}
		return b
	}
	var b []byte
	for _, r := range s {
		if r > 0xff {
			r = '?'
		}
		b = append(b, byte(r))
	}
	return b
}

func (f *mp3File) setGain(g *Gain) {
	drop := func(fr id3Frame) bool {
		if fr.id != "TXXX" {
			return false
		}
		b := f.frameBody(fr)
		if len(b) < 1 {
			return false
		}
		d, _ := splitTerm(b[1:], b[0])
		return strings.HasPrefix(strings.ToUpper(d), "REPLAYGAIN_")
	}
	if g == nil {
		f.replace(drop)
		return
	}
	add := []id3Frame{
		f.txxxFrame("REPLAYGAIN_TRACK_GAIN", formatGain(g.TrackGain)),
		f.txxxFrame("REPLAYGAIN_TRACK_PEAK", formatPeak(g.TrackPeak)),
	}
	if g.HasAlbum {
		add = append(add, f.txxxFrame("REPLAYGAIN_ALBUM_GAIN", formatGain(g.AlbumGain)),
			f.txxxFrame("REPLAYGAIN_ALBUM_PEAK", formatPeak(g.AlbumPeak)))
	}
	f.replace(drop, add...)
}

func (f *mp3File) setCover(c *Cover) {
	n := 0
	for _, fr := range f.frames {
		if fr.id == "APIC" {
			n++
		}
	}
	drop := func(fr id3Frame) bool {
		if fr.id != "APIC" {
			return false
		}
		if n == 1 {
			return true // la única imagen es la portada
		}
		b := f.frameBody(fr)
		if b == nil {
			return false
		}
		i := bytes.IndexByte(b[1:], 0)
		return i < 0 || len(b) < 3+i || b[2+i] == 3
	}
	if c == nil {
		f.replace(drop)
		return
	}
	mime := c.MIME
	if mime == "" {
		mime = sniffMIME(c.Data)
	}
	data := append([]byte{0}, mime...) // descripción en Latin-1 (vacía)
	data = append(data, 0, 3, 0)
	data = append(data, c.Data...)
	f.replace(drop, id3Frame{id: "APIC", data: data})
}

func (f *mp3File) write(w io.Writer) error {
	var body bytes.Buffer
	for _, fr := range f.frames {
		var h [10]byte
		copy(h[:], fr.id)
		n := len(fr.data)
		if f.version == 4 {
			if n >= 1<<28 {
				return ErrCorrupt
			}
			h[4], h[5], h[6], h[7] = byte(n>>21)&0x7f, byte(n>>14)&0x7f, byte(n>>7)&0x7f, byte(n)&0x7f
		} else {
			binary.BigEndian.PutUint32(h[4:], uint32(n))
		}
		h[8], h[9] = fr.flags[0], fr.flags[1]
		body.Write(h[:])
		body.Write(fr.data)
	}
	size := body.Len()
	pad := 1024
	if old := f.size - 10; f.size > 0 && size <= old {
		pad = old - size // ocupa lo mismo que la de antes
	}
	total := size + pad
	if total >= 1<<28 {
		return ErrCorrupt
	}
	hdr := []byte{'I', 'D', '3', f.version, 0, 0,
		byte(total>>21) & 0x7f, byte(total>>14) & 0x7f, byte(total>>7) & 0x7f, byte(total) & 0x7f}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(body.Bytes()); err != nil {
		return err
	}
	if _, err := w.Write(make([]byte, pad)); err != nil {
		return err
	}
	_, err := w.Write(f.audio)
	return err
}
