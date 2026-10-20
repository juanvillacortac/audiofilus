package tagwrite

import (
	"bytes"
	"encoding/binary"
	"io"
	"strconv"
	"strings"
)

// MP4/M4A: las etiquetas van en moov/udta/meta/ilst, un átomo por campo con
// su átomo "data" dentro (y "----" para los libres, como los de
// ReplayGain). Se reescribe el moov entero con el ilst cambiado; los demás
// átomos se copian tal cual. Si el moov va delante del mdat y cambia de
// tamaño, se usa (o se crea) un átomo "free" a su lado para que el audio no
// se mueva; si no se puede, se corren las posiciones de los trozos de audio
// (stco/co64). Con fragmentos (moof) solo se escribe si el audio no se mueve.

// atom es un átomo: su tipo, y su contenido (los hijos si es un
// contenedor que se conoce).
type atom struct {
	kind     string
	data     []byte // el contenido sin cabecera (de los que no se abren)
	children []*atom
	prefix   []byte // los 4 bytes de versión y banderas de "meta" (contenedor "completo")
	tail     []byte // ceros sueltos al final de un contenedor (algunos udta los traen)
	open     bool
}

// containers son los átomos que se abren para buscar dentro.
var containers = map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true,
	"udta": true, "meta": true, "ilst": true, "edts": true, "dinf": true}

// parseAtoms lee los átomos de b; tail son unos ceros sueltos al final
// (menos de 8 bytes, o un tamaño 0 de cierre), que se conservan.
func parseAtoms(b []byte, depth int) (atoms []*atom, tail []byte, err error) {
	if depth > 12 {
		return nil, nil, ErrCorrupt
	}
	var out []*atom
	for pos := 0; pos < len(b); {
		if pos+8 > len(b) || (depth > 0 && binary.BigEndian.Uint32(b[pos:]) == 0) {
			if allZero(b[pos:]) {
				return out, b[pos:], nil
			}
			return nil, nil, ErrCorrupt
		}
		size := int(binary.BigEndian.Uint32(b[pos:]))
		kind := string(b[pos+4 : pos+8])
		hdr := 8
		switch size {
		case 1:
			if pos+16 > len(b) {
				return nil, nil, ErrCorrupt
			}
			big := binary.BigEndian.Uint64(b[pos+8:])
			if big > uint64(len(b)-pos) {
				return nil, nil, ErrCorrupt
			}
			size, hdr = int(big), 16
		case 0:
			size = len(b) - pos // hasta el final
		}
		if size < hdr || pos+size > len(b) {
			return nil, nil, ErrCorrupt
		}
		a := &atom{kind: kind, data: b[pos+hdr : pos+size]}
		if containers[kind] {
			content := a.data
			if kind == "meta" {
				// "meta" lleva 4 bytes de versión y banderas (salvo en
				// algunos archivos de QuickTime, que no los traen).
				if len(content) >= 8 && string(content[4:8]) != "hdlr" {
					a.prefix, content = content[:4], content[4:]
				}
			}
			kids, tail, err := parseAtoms(content, depth+1)
			if err == nil {
				a.children, a.tail, a.open, a.data = kids, tail, true, nil
			} else {
				a.prefix = nil
			}
		}
		out = append(out, a)
		pos += size
	}
	return out, nil, nil
}

// allZero dice si b son todo ceros.
func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// bytes escribe el átomo.
func (a *atom) bytes() []byte {
	var content []byte
	if a.open {
		var b bytes.Buffer
		b.Write(a.prefix)
		for _, c := range a.children {
			b.Write(c.bytes())
		}
		b.Write(a.tail)
		content = b.Bytes()
	} else {
		content = a.data
	}
	n := 8 + len(content)
	if n > 0xffffffff {
		out := make([]byte, 16, 16+len(content))
		binary.BigEndian.PutUint32(out, 1)
		copy(out[4:], a.kind)
		binary.BigEndian.PutUint64(out[8:], uint64(n+8))
		return append(out, content...)
	}
	out := make([]byte, 8, n)
	binary.BigEndian.PutUint32(out, uint32(n))
	copy(out[4:], a.kind)
	return append(out, content...)
}

// child es el primer hijo de un tipo.
func (a *atom) child(kind string) *atom {
	for _, c := range a.children {
		if c.kind == kind {
			return c
		}
	}
	return nil
}

type mp4File struct {
	top  []*atom
	moov *atom
	ilst *atom
	orig []byte // el archivo de antes (para calcular las posiciones)
}

func parseMP4(data []byte) (*mp4File, error) {
	top, _, err := parseAtoms(data, 0)
	if err != nil {
		return nil, err
	}
	f := &mp4File{top: top, orig: data}
	if len(top) == 0 || (top[0].kind != "ftyp" && top[0].kind != "moov") {
		return nil, ErrCorrupt
	}
	for _, a := range top {
		if a.kind == "moov" {
			f.moov = a
			break
		}
	}
	if f.moov == nil || !f.moov.open {
		return nil, ErrCorrupt
	}
	// moov/udta/meta/ilst, creándolos si no están.
	udta := f.moov.child("udta")
	if udta == nil {
		udta = &atom{kind: "udta", open: true}
		f.moov.children = append(f.moov.children, udta)
	}
	if !udta.open {
		return nil, ErrCorrupt
	}
	meta := udta.child("meta")
	if meta == nil {
		hdlr := &atom{kind: "hdlr", data: append(make([]byte, 8), []byte("mdirappl\x00\x00\x00\x00\x00\x00\x00\x00\x00")...)}
		meta = &atom{kind: "meta", open: true, prefix: make([]byte, 4), children: []*atom{hdlr}}
		udta.children = append(udta.children, meta)
	}
	if !meta.open {
		return nil, ErrCorrupt
	}
	f.ilst = meta.child("ilst")
	if f.ilst == nil {
		f.ilst = &atom{kind: "ilst", open: true}
		meta.children = append(meta.children, f.ilst)
	}
	if !f.ilst.open {
		return nil, ErrCorrupt
	}
	return f, nil
}

// itemData es el contenido del primer "data" de un elemento del ilst (sin
// los 8 bytes de tipo y "locale"), y su tipo.
func itemData(it *atom) ([]byte, uint32, bool) {
	if !it.open {
		// Los elementos se leen como contenedores aunque no estén en la
		// lista (sus nombres empiezan por ©).
		kids, tail, err := parseAtoms(it.data, 1)
		if err != nil {
			return nil, 0, false
		}
		it.children, it.tail, it.open, it.data = kids, tail, true, nil
	}
	d := it.child("data")
	if d == nil || len(d.data) < 8 {
		return nil, 0, false
	}
	return d.data[8:], binary.BigEndian.Uint32(d.data[:4]) & 0xffffff, true
}

// item busca un elemento del ilst por su tipo.
func (f *mp4File) item(kind string) *atom {
	for _, c := range f.ilst.children {
		if c.kind == kind {
			return c
		}
	}
	return nil
}

// textOf es el texto de un elemento.
func (f *mp4File) textOf(kind string) string {
	if it := f.item(kind); it != nil {
		if d, _, ok := itemData(it); ok {
			return string(d)
		}
	}
	return ""
}

// freeform es el valor de un "----" por su nombre (sin mayúsculas).
func (f *mp4File) freeform(name string) (string, bool) {
	for _, c := range f.ilst.children {
		if c.kind != "----" {
			continue
		}
		if _, _, ok := itemData(c); !ok {
			continue
		}
		n := c.child("name")
		if n == nil || len(n.data) < 4 || !strings.EqualFold(string(n.data[4:]), name) {
			continue
		}
		d, _, _ := itemData(c)
		return string(d), true
	}
	return "", false
}

// pairOf lee trkn o disk (número y total).
func (f *mp4File) pairOf(kind string) (int, int) {
	if it := f.item(kind); it != nil {
		if d, _, ok := itemData(it); ok && len(d) >= 6 {
			return int(binary.BigEndian.Uint16(d[2:])), int(binary.BigEndian.Uint16(d[4:]))
		}
	}
	return 0, 0
}

func (f *mp4File) tags() Tags {
	var t Tags
	t.Title = f.textOf("\xa9nam")
	t.Artist = f.textOf("\xa9ART")
	t.Album = f.textOf("\xa9alb")
	t.AlbumArtist = f.textOf("aART")
	t.Genre = f.textOf("\xa9gen")
	if t.Genre == "" {
		if it := f.item("gnre"); it != nil {
			if d, _, ok := itemData(it); ok && len(d) >= 2 {
				if n := int(binary.BigEndian.Uint16(d)); n > 0 && n <= len(genres) {
					t.Genre = genres[n-1]
				}
			}
		}
	}
	t.Composer = f.textOf("\xa9wrt")
	t.Comment = f.textOf("\xa9cmt")
	t.Year = parseYear(f.textOf("\xa9day"))
	t.Track, t.TrackTotal = f.pairOf("trkn")
	t.Disc, t.DiscTotal = f.pairOf("disk")
	t.ReplayGain = gainFromFields(func(k string) (string, bool) { return f.freeform(k) })
	if it := f.item("covr"); it != nil {
		if d, typ, ok := itemData(it); ok {
			mime := sniffMIME(d)
			switch typ {
			case 13:
				mime = "image/jpeg"
			case 14:
				mime = "image/png"
			}
			t.Cover = &Cover{MIME: mime, Data: append([]byte(nil), d...)}
		}
	}
	return t
}

// dataAtom es un "data" con su tipo (1 texto, 0 binario, 13 JPEG, 14 PNG).
func dataAtom(typ uint32, v []byte) *atom {
	d := make([]byte, 8, 8+len(v))
	binary.BigEndian.PutUint32(d, typ)
	return &atom{kind: "data", data: append(d, v...)}
}

// setItem pone (o quita, con v nil) un elemento, donde estaba.
func (f *mp4File) setItem(kind string, it *atom, drop ...string) {
	at := -1
	var out []*atom
	for _, c := range f.ilst.children {
		if c.kind == kind || containsStr(drop, c.kind) {
			if at < 0 {
				at = len(out)
			}
			continue
		}
		out = append(out, c)
	}
	if it != nil {
		if at < 0 {
			at = len(out)
		}
		out = append(out[:at], append([]*atom{it}, out[at:]...)...)
	}
	f.ilst.children = out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

var mp4Text = map[string]string{"title": "\xa9nam", "artist": "\xa9ART", "album": "\xa9alb", "albumartist": "aART",
	"genre": "\xa9gen", "composer": "\xa9wrt", "comment": "\xa9cmt"}

func (f *mp4File) setText(field, v string) {
	kind := mp4Text[field]
	var drop []string
	if field == "genre" {
		drop = []string{"gnre"}
	}
	if v == "" {
		f.setItem(kind, nil, drop...)
		return
	}
	f.setItem(kind, &atom{kind: kind, open: true, children: []*atom{dataAtom(1, []byte(v))}}, drop...)
}

func (f *mp4File) setYear(y int) {
	if y <= 0 {
		f.setItem("\xa9day", nil)
		return
	}
	f.setItem("\xa9day", &atom{kind: "\xa9day", open: true, children: []*atom{dataAtom(1, []byte(strconv.Itoa(y)))}})
}

func (f *mp4File) setPair(kind string, n, total, size int) {
	if n <= 0 && total <= 0 {
		f.setItem(kind, nil)
		return
	}
	v := make([]byte, size)
	binary.BigEndian.PutUint16(v[2:], uint16(n))
	binary.BigEndian.PutUint16(v[4:], uint16(total))
	f.setItem(kind, &atom{kind: kind, open: true, children: []*atom{dataAtom(0, v)}})
}

func (f *mp4File) setTrack(n, total int) { f.setPair("trkn", n, total, 8) }
func (f *mp4File) setDisc(n, total int)  { f.setPair("disk", n, total, 6) }

// freeformAtom es un "----" de iTunes.
func freeformAtom(name, v string) *atom {
	mean := &atom{kind: "mean", data: append(make([]byte, 4), "com.apple.iTunes"...)}
	nm := &atom{kind: "name", data: append(make([]byte, 4), name...)}
	return &atom{kind: "----", open: true, children: []*atom{mean, nm, dataAtom(1, []byte(v))}}
}

func (f *mp4File) setGain(g *Gain) {
	// Se quitan los REPLAYGAIN_* de antes.
	var out []*atom
	at := -1
	for _, c := range f.ilst.children {
		if c.kind == "----" {
			itemData(c)
			if n := c.child("name"); n != nil && len(n.data) >= 4 && strings.HasPrefix(strings.ToUpper(string(n.data[4:])), "REPLAYGAIN_") {
				if at < 0 {
					at = len(out)
				}
				continue
			}
		}
		out = append(out, c)
	}
	if at < 0 {
		at = len(out)
	}
	var add []*atom
	if g != nil {
		add = append(add, freeformAtom("REPLAYGAIN_TRACK_GAIN", formatGain(g.TrackGain)),
			freeformAtom("REPLAYGAIN_TRACK_PEAK", formatPeak(g.TrackPeak)))
		if g.HasAlbum {
			add = append(add, freeformAtom("REPLAYGAIN_ALBUM_GAIN", formatGain(g.AlbumGain)),
				freeformAtom("REPLAYGAIN_ALBUM_PEAK", formatPeak(g.AlbumPeak)))
		}
	}
	f.ilst.children = append(out[:at], append(add, out[at:]...)...)
}

func (f *mp4File) setCover(c *Cover) {
	if c == nil {
		f.setItem("covr", nil)
		return
	}
	typ := uint32(13)
	mime := c.MIME
	if mime == "" {
		mime = sniffMIME(c.Data)
	}
	if mime == "image/png" {
		typ = 14
	}
	f.setItem("covr", &atom{kind: "covr", open: true, children: []*atom{dataAtom(typ, c.Data)}})
}

// write escribe el archivo con el moov nuevo, sin mover el audio si se
// puede.
func (f *mp4File) write(w io.Writer) error {
	// Dónde estaba cada átomo de arriba.
	type span struct{ start, end int }
	spans := make([]span, len(f.top))
	pos := 0
	for i := range f.top {
		n := topSize(f.orig, pos)
		spans[i] = span{pos, pos + n}
		pos += n
	}
	moovIdx := -1
	for i, a := range f.top {
		if a == f.moov {
			moovIdx = i
		}
	}
	oldMoov := topSize(f.orig, spans[moovIdx].start)
	// Lo que viene después del moov en el archivo: si hay datos de audio
	// (mdat o moof), el moov no puede cambiar de tamaño sin más.
	audioAfter := false
	fragmented := false
	for i, a := range f.top {
		if a.kind == "moof" || a.kind == "mfra" {
			fragmented = true
		}
		if i > moovIdx && (a.kind == "mdat" || a.kind == "moof") {
			audioAfter = true
		}
	}
	newMoov := f.moov.bytes()
	delta := len(newMoov) - oldMoov
	// El hueco disponible: los "free"/"skip" seguidos tras el moov.
	freeAfter := 0
	j := moovIdx + 1
	for ; j < len(f.top) && (f.top[j].kind == "free" || f.top[j].kind == "skip"); j++ {
		freeAfter += spans[j].end - spans[j].start
	}
	var out bytes.Buffer
	out.Write(f.orig[:spans[moovIdx].start])
	switch {
	case !audioAfter || delta == 0:
		// El audio no se mueve (o va antes): el moov nuevo tal cual.
		out.Write(newMoov)
		out.Write(f.orig[spans[moovIdx].end:])
	case delta <= freeAfter-8 || delta == freeAfter:
		// Cabe comiéndose el relleno de detrás.
		out.Write(newMoov)
		if rest := freeAfter - delta; rest > 0 {
			out.Write(freeAtom(rest))
		}
		out.Write(f.orig[spans[j-1].end:])
	case delta < 0 && -delta+freeAfter >= 8:
		// Encoge: el hueco va a un "free".
		out.Write(newMoov)
		out.Write(freeAtom(-delta + freeAfter))
		out.Write(f.orig[spans[j-1].end:])
	case fragmented:
		return ErrUnsupported // con fragmentos no se corren las posiciones
	default:
		// Se corren las posiciones de los trozos de audio (y se deja 1 KB de
		// relleno para la próxima vez).
		shift := delta + freeAtomPad - freeAfter
		if err := shiftOffsets(f.moov, int64(shift)); err != nil {
			return err
		}
		newMoov = f.moov.bytes()
		if len(newMoov)-oldMoov+freeAtomPad-freeAfter != shift {
			return ErrCorrupt // stco pasó a co64 (no pasa: se corre en su sitio)
		}
		out.Write(newMoov)
		out.Write(freeAtom(freeAtomPad))
		out.Write(f.orig[spans[j-1].end:])
	}
	_, err := w.Write(out.Bytes())
	return err
}

const freeAtomPad = 1024

// freeAtom es un átomo "free" de n bytes en total (n >= 8).
func freeAtom(n int) []byte {
	b := make([]byte, n)
	binary.BigEndian.PutUint32(b, uint32(n))
	copy(b[4:], "free")
	return b
}

// topSize es el tamaño del átomo que empieza en pos.
func topSize(b []byte, pos int) int {
	size := int(binary.BigEndian.Uint32(b[pos:]))
	switch size {
	case 1:
		return int(binary.BigEndian.Uint64(b[pos+8:]))
	case 0:
		return len(b) - pos
	}
	return size
}

// shiftOffsets corre por d las posiciones de los trozos (stco y co64) de
// todas las pistas.
func shiftOffsets(a *atom, d int64) error {
	for _, c := range a.children {
		switch {
		case c.open:
			if err := shiftOffsets(c, d); err != nil {
				return err
			}
		case c.kind == "stco" || c.kind == "co64":
			if len(c.data) < 8 {
				return ErrCorrupt
			}
			n := int(binary.BigEndian.Uint32(c.data[4:]))
			w := 4
			if c.kind == "co64" {
				w = 8
			}
			if 8+n*w > len(c.data) {
				return ErrCorrupt
			}
			data := append([]byte(nil), c.data...)
			for i := 0; i < n; i++ {
				p := data[8+i*w:]
				if w == 4 {
					v := int64(binary.BigEndian.Uint32(p)) + d
					if v < 0 || v > 0xffffffff {
						return ErrUnsupported // no cabe en 32 bits
					}
					binary.BigEndian.PutUint32(p, uint32(v))
				} else {
					binary.BigEndian.PutUint64(p, uint64(int64(binary.BigEndian.Uint64(p))+d))
				}
			}
			c.data = data
		}
	}
	return nil
}
