package tagwrite

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	_ "image/jpeg" // para medir las portadas (DecodeConfig)
	_ "image/png"
	"strconv"
	"strings"
)

// Los comentarios de Vorbis (en FLAC, Ogg Vorbis y Ogg Opus): una lista de
// "CLAVE=valor" en orden, con su "vendor". Se conserva el orden y lo que no
// se entiende; al cambiar un campo, sus entradas se quitan y la nueva va
// donde estaba la primera.

type vorbisComments struct {
	vendor  string
	entries []string
	opus    bool // en Opus las ganancias van como R128_*
}

// parseVorbisComments lee el bloque (sin cabecera de paquete ni bit de
// cierre); rest es lo que sobra detrás (en Opus puede haber datos).
func parseVorbisComments(b []byte) (vc *vorbisComments, rest []byte, err error) {
	r := bytes.NewReader(b)
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil || int64(n) > int64(r.Len()) {
		return nil, nil, ErrCorrupt
	}
	vendor := make([]byte, n)
	r.Read(vendor)
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil || int64(count) > int64(r.Len())/4 {
		return nil, nil, ErrCorrupt
	}
	vc = &vorbisComments{vendor: string(vendor)}
	for i := uint32(0); i < count; i++ {
		var l uint32
		if err := binary.Read(r, binary.LittleEndian, &l); err != nil || int64(l) > int64(r.Len()) {
			return nil, nil, ErrCorrupt
		}
		e := make([]byte, l)
		r.Read(e)
		vc.entries = append(vc.entries, string(e))
	}
	rest = b[len(b)-r.Len():]
	return vc, rest, nil
}

// bytes escribe el bloque (sin cabecera ni cierre).
func (vc *vorbisComments) bytes() []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(len(vc.vendor)))
	b.WriteString(vc.vendor)
	binary.Write(&b, binary.LittleEndian, uint32(len(vc.entries)))
	for _, e := range vc.entries {
		binary.Write(&b, binary.LittleEndian, uint32(len(e)))
		b.WriteString(e)
	}
	return b.Bytes()
}

// get es el primer valor de una clave (sin mayúsculas).
func (vc *vorbisComments) get(key string) (string, bool) {
	for _, e := range vc.entries {
		k, v, ok := strings.Cut(e, "=")
		if ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// first es el primer valor de alguna de las claves.
func (vc *vorbisComments) first(keys ...string) string {
	for _, k := range keys {
		if v, ok := vc.get(k); ok {
			return v
		}
	}
	return ""
}

// replace quita las entradas de keys y pone las de add donde estaba la
// primera quitada (o al final).
func (vc *vorbisComments) replace(keys []string, add ...string) {
	at := -1
	var out []string
	for _, e := range vc.entries {
		k, _, _ := strings.Cut(e, "=")
		drop := false
		for _, key := range keys {
			if strings.EqualFold(k, key) {
				drop = true
				break
			}
		}
		if drop {
			if at < 0 {
				at = len(out)
			}
			continue
		}
		out = append(out, e)
	}
	if at < 0 {
		at = len(out)
	}
	out = append(out[:at], append(add, out[at:]...)...)
	vc.entries = out
}

// vorbisKeys son las claves de cada campo (la primera es la que se
// escribe; las demás se leen y se quitan).
var vorbisKeys = map[string][]string{
	"title":       {"TITLE"},
	"artist":      {"ARTIST"},
	"album":       {"ALBUM"},
	"albumartist": {"ALBUMARTIST", "ALBUM ARTIST"},
	"genre":       {"GENRE"},
	"composer":    {"COMPOSER"},
	"comment":     {"COMMENT", "DESCRIPTION"},
}

// tags lee los campos conocidos.
func (vc *vorbisComments) tags() Tags {
	var t Tags
	t.Title = vc.first(vorbisKeys["title"]...)
	t.Artist = vc.first(vorbisKeys["artist"]...)
	t.Album = vc.first(vorbisKeys["album"]...)
	t.AlbumArtist = vc.first(vorbisKeys["albumartist"]...)
	t.Genre = vc.first(vorbisKeys["genre"]...)
	t.Composer = vc.first(vorbisKeys["composer"]...)
	t.Comment = vc.first(vorbisKeys["comment"]...)
	t.Year = parseYear(vc.first("DATE", "YEAR"))
	t.Track, t.TrackTotal = parseNumPair(vc.first("TRACKNUMBER"))
	if n, _ := strconv.Atoi(strings.TrimSpace(vc.first("TRACKTOTAL", "TOTALTRACKS"))); n > 0 {
		t.TrackTotal = n
	}
	t.Disc, t.DiscTotal = parseNumPair(vc.first("DISCNUMBER"))
	if n, _ := strconv.Atoi(strings.TrimSpace(vc.first("DISCTOTAL", "TOTALDISCS"))); n > 0 {
		t.DiscTotal = n
	}
	t.ReplayGain = gainFromFields(func(k string) (string, bool) { return vc.get(k) })
	for _, e := range vc.entries {
		k, v, _ := strings.Cut(e, "=")
		if strings.EqualFold(k, "METADATA_BLOCK_PICTURE") {
			if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v)); err == nil {
				if p, ok := parsePicture(raw); ok && (t.Cover == nil || p.kind == 3) {
					t.Cover = &Cover{MIME: p.mime, Data: p.data}
				}
			}
		}
	}
	return t
}

func (vc *vorbisComments) setText(field, value string) {
	keys := vorbisKeys[field]
	if value == "" {
		vc.replace(keys)
		return
	}
	vc.replace(keys, keys[0]+"="+value)
}

func (vc *vorbisComments) setYear(y int) {
	if y <= 0 {
		vc.replace([]string{"DATE", "YEAR"})
		return
	}
	vc.replace([]string{"DATE", "YEAR"}, "DATE="+strconv.Itoa(y))
}

func (vc *vorbisComments) setTrack(n, total int) {
	var add []string
	if n > 0 {
		add = append(add, "TRACKNUMBER="+strconv.Itoa(n))
	}
	if total > 0 {
		add = append(add, "TRACKTOTAL="+strconv.Itoa(total))
	}
	vc.replace([]string{"TRACKNUMBER", "TRACKTOTAL", "TOTALTRACKS"}, add...)
}

func (vc *vorbisComments) setDisc(n, total int) {
	var add []string
	if n > 0 {
		add = append(add, "DISCNUMBER="+strconv.Itoa(n))
	}
	if total > 0 {
		add = append(add, "DISCTOTAL="+strconv.Itoa(total))
	}
	vc.replace([]string{"DISCNUMBER", "DISCTOTAL", "TOTALDISCS"}, add...)
}

// setGain escribe la ganancia: REPLAYGAIN_* en FLAC y Vorbis; en Opus, las
// ganancias como R128_* (lo que pide su especificación: un reproductor de
// Opus no debe usar REPLAYGAIN_*_GAIN) y los picos como REPLAYGAIN_*_PEAK
// (R128 no tiene picos).
func (vc *vorbisComments) setGain(g *Gain) {
	keys := []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK", "REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK",
		"R128_TRACK_GAIN", "R128_ALBUM_GAIN"}
	if g == nil {
		vc.replace(keys)
		return
	}
	var add []string
	if vc.opus {
		add = append(add, "R128_TRACK_GAIN="+r128(g.TrackGain))
		if g.HasAlbum {
			add = append(add, "R128_ALBUM_GAIN="+r128(g.AlbumGain))
		}
		if g.TrackPeak > 0 {
			add = append(add, "REPLAYGAIN_TRACK_PEAK="+formatPeak(g.TrackPeak))
		}
		if g.HasAlbum && g.AlbumPeak > 0 {
			add = append(add, "REPLAYGAIN_ALBUM_PEAK="+formatPeak(g.AlbumPeak))
		}
	} else {
		add = append(add, "REPLAYGAIN_TRACK_GAIN="+formatGain(g.TrackGain), "REPLAYGAIN_TRACK_PEAK="+formatPeak(g.TrackPeak))
		if g.HasAlbum {
			add = append(add, "REPLAYGAIN_ALBUM_GAIN="+formatGain(g.AlbumGain), "REPLAYGAIN_ALBUM_PEAK="+formatPeak(g.AlbumPeak))
		}
	}
	vc.replace(keys, add...)
}

// setCover pone la portada como METADATA_BLOCK_PICTURE (Ogg); quita las que
// hubiera (también las COVERART antiguas).
func (vc *vorbisComments) setCover(c *Cover) {
	keys := []string{"METADATA_BLOCK_PICTURE", "COVERART", "COVERARTMIME"}
	if c == nil {
		vc.replace(keys)
		return
	}
	vc.replace(keys, "METADATA_BLOCK_PICTURE="+base64.StdEncoding.EncodeToString(pictureBlock(c)))
}

// picture es un bloque PICTURE de FLAC (también dentro de
// METADATA_BLOCK_PICTURE).
type picture struct {
	kind uint32
	mime string
	data []byte
}

// parsePicture lee un bloque PICTURE.
func parsePicture(b []byte) (picture, bool) {
	r := bytes.NewReader(b)
	var p picture
	read32 := func() (uint32, bool) {
		var v uint32
		return v, binary.Read(r, binary.BigEndian, &v) == nil
	}
	readStr := func() ([]byte, bool) {
		n, ok := read32()
		if !ok || int64(n) > int64(r.Len()) {
			return nil, false
		}
		s := make([]byte, n)
		r.Read(s)
		return s, true
	}
	var ok bool
	if p.kind, ok = read32(); !ok {
		return p, false
	}
	m, ok := readStr()
	if !ok {
		return p, false
	}
	p.mime = string(m)
	if _, ok = readStr(); !ok { // descripción
		return p, false
	}
	for i := 0; i < 4; i++ { // ancho, alto, bits, colores
		if _, ok = read32(); !ok {
			return p, false
		}
	}
	if p.data, ok = readStr(); !ok {
		return p, false
	}
	return p, true
}

// pictureBlock escribe un bloque PICTURE de portada delantera (tipo 3), con
// sus medidas si se pueden leer.
func pictureBlock(c *Cover) []byte {
	mime := c.MIME
	if mime == "" {
		mime = sniffMIME(c.Data)
	}
	var w, h, depth uint32
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(c.Data)); err == nil {
		w, h, depth = uint32(cfg.Width), uint32(cfg.Height), 24
	}
	var b bytes.Buffer
	put := func(v uint32) { binary.Write(&b, binary.BigEndian, v) }
	put(3)
	put(uint32(len(mime)))
	b.WriteString(mime)
	put(0) // sin descripción
	put(w)
	put(h)
	put(depth)
	put(0)
	put(uint32(len(c.Data)))
	b.Write(c.Data)
	return b.Bytes()
}

// sniffMIME adivina el tipo de una imagen por sus primeros bytes.
func sniffMIME(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF8")):
		return "image/gif"
	case len(b) > 12 && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return "image/jpeg"
}
