// Package tags lee las etiquetas de una canción: las de dhowden/tag (ID3,
// MP4, Vorbis, FLAC) y, además, las de ASF (WMA), que esa biblioteca no
// entiende. ReadFrom sustituye a tag.ReadFrom en todo audiofilus.
package tags

import (
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/dhowden/tag"
)

// Lo que se devuelve con ASF como formato y tipo de archivo (dhowden/tag no
// tiene constantes para ellos).
const (
	ASF tag.Format   = "ASF"
	WMA tag.FileType = "WMA"
)

// ReadFrom lee las etiquetas de r: si es un ASF, aquí; si no, con
// dhowden/tag.
func ReadFrom(r io.ReadSeeker) (tag.Metadata, error) {
	var magic [16]byte
	n, _ := io.ReadFull(r, magic[:])
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if n == 16 && magic == guidHeader {
		return readASF(r)
	}
	return tag.ReadFrom(r)
}

// guid pasa un GUID escrito como texto a sus bytes en el archivo (los tres
// primeros grupos van al revés: little-endian).
func guid(s string) [16]byte {
	h := strings.ReplaceAll(s, "-", "")
	var b [16]byte
	for i := range b {
		v, _ := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
		b[i] = byte(v)
	}
	b[0], b[1], b[2], b[3] = b[3], b[2], b[1], b[0]
	b[4], b[5] = b[5], b[4]
	b[6], b[7] = b[7], b[6]
	return b
}

// Los objetos de la cabecera de ASF que llevan etiquetas.
var (
	guidHeader      = guid("75B22630-668E-11CF-A6D9-00AA0062CE6C")
	guidContent     = guid("75B22633-668E-11CF-A6D9-00AA0062CE6C") // título, autor...
	guidExtContent  = guid("D2D0A440-E307-11D2-97F0-00A0C95EA850") // WM/AlbumTitle...
	guidHeaderExt   = guid("5FBF03B5-A92E-11CF-8EE3-00C00C205365") // contiene los dos siguientes
	guidMetadata    = guid("C5F8CBEA-5BAF-4877-8467-AA8C44FA4CCA")
	guidMetadataLib = guid("44231C94-9498-49D1-A141-1D134E457054") // las imágenes de más de 64 KB
)

// asfMaxHeader es lo más grande que se acepta de cabecera (con carátulas
// dentro): más es un archivo roto.
const asfMaxHeader = 64 << 20

var errASF = errors.New("asf: malformed header")

// asfTags son las etiquetas de un ASF: los campos con su nombre (los de la
// descripción con el de su equivalente WM/, para leerlos igual) y la
// carátula.
type asfTags struct {
	raw map[string]any
	pic *tag.Picture
}

// readASF lee la cabecera de un ASF y sus etiquetas.
func readASF(r io.Reader) (tag.Metadata, error) {
	var hdr [30]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint64(hdr[16:])
	if size < 30 || size > asfMaxHeader {
		return nil, errASF
	}
	body := make([]byte, size-30)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	t := &asfTags{raw: map[string]any{}}
	if err := t.objects(body); err != nil {
		return nil, err
	}
	return t, nil
}

// objects recorre una lista de objetos (GUID, tamaño, datos).
func (t *asfTags) objects(b []byte) error {
	for len(b) >= 24 {
		var id [16]byte
		copy(id[:], b)
		size := binary.LittleEndian.Uint64(b[16:])
		if size < 24 || size > uint64(len(b)) {
			return errASF
		}
		data := b[24:size]
		switch id {
		case guidContent:
			t.content(data)
		case guidExtContent:
			t.extContent(data)
		case guidHeaderExt:
			// Dos reservados (GUID y uint16), el tamaño, y más objetos.
			if len(data) >= 22 {
				n := int(binary.LittleEndian.Uint32(data[18:]))
				if n <= len(data)-22 {
					if err := t.objects(data[22 : 22+n]); err != nil {
						return err
					}
				}
			}
		case guidMetadata, guidMetadataLib:
			t.metadata(data)
		}
		b = b[size:]
	}
	return nil
}

// content lee el objeto de descripción: título, autor, copyright,
// descripción y valoración, cada uno con su largo delante.
func (t *asfTags) content(b []byte) {
	if len(b) < 10 {
		return
	}
	names := []string{"Title", "Author", "Copyright", "Description", "Rating"}
	p := 10
	for i, name := range names {
		n := int(binary.LittleEndian.Uint16(b[i*2:]))
		if p+n > len(b) {
			return
		}
		if s := utf16String(b[p : p+n]); s != "" {
			t.set(name, s)
		}
		p += n
	}
}

// extContent lee la descripción extendida: nombre, tipo y valor de cada
// campo (WM/AlbumTitle, WM/TrackNumber, WM/Picture, REPLAYGAIN_*...).
func (t *asfTags) extContent(b []byte) {
	if len(b) < 2 {
		return
	}
	count := int(binary.LittleEndian.Uint16(b))
	p := 2
	for range count {
		if p+2 > len(b) {
			return
		}
		nl := int(binary.LittleEndian.Uint16(b[p:]))
		p += 2
		if p+nl+4 > len(b) {
			return
		}
		name := utf16String(b[p : p+nl])
		p += nl
		typ := binary.LittleEndian.Uint16(b[p:])
		vl := int(binary.LittleEndian.Uint16(b[p+2:]))
		p += 4
		if p+vl > len(b) {
			return
		}
		t.value(name, typ, b[p:p+vl])
		p += vl
	}
}

// metadata lee un objeto Metadata o Metadata Library (de la extensión de la
// cabecera): como la descripción extendida, pero con el largo del valor en
// 32 bits (ahí van las carátulas grandes).
func (t *asfTags) metadata(b []byte) {
	if len(b) < 2 {
		return
	}
	count := int(binary.LittleEndian.Uint16(b))
	p := 2
	for range count {
		if p+12 > len(b) {
			return
		}
		nl := int(binary.LittleEndian.Uint16(b[p+4:]))
		typ := binary.LittleEndian.Uint16(b[p+6:])
		vl := int(binary.LittleEndian.Uint32(b[p+8:]))
		p += 12
		if nl < 0 || vl < 0 || p+nl > len(b) || vl > len(b)-p-nl {
			return
		}
		name := utf16String(b[p : p+nl])
		p += nl
		t.value(name, typ, b[p:p+vl])
		p += vl
	}
}

// value guarda un campo según su tipo: texto, bytes, o un número (bool,
// 32, 64 o 16 bits), que se guarda como texto.
func (t *asfTags) value(name string, typ uint16, v []byte) {
	switch typ {
	case 0:
		t.set(name, utf16String(v))
	case 1:
		if name == "WM/Picture" && t.pic == nil {
			t.pic = picture(v)
			return
		}
		t.set(name, v)
	case 2, 3:
		if len(v) >= 4 {
			t.set(name, strconv.FormatUint(uint64(binary.LittleEndian.Uint32(v)), 10))
		}
	case 4:
		if len(v) >= 8 {
			t.set(name, strconv.FormatUint(binary.LittleEndian.Uint64(v), 10))
		}
	case 5:
		if len(v) >= 2 {
			t.set(name, strconv.FormatUint(uint64(binary.LittleEndian.Uint16(v)), 10))
		}
	}
}

// set guarda un campo; si ya estaba (repetido en dos objetos), se queda el
// primero.
func (t *asfTags) set(name string, v any) {
	if _, ok := t.raw[name]; !ok {
		t.raw[name] = v
	}
}

// picture lee un WM/Picture: el tipo, el tamaño, el MIME y la descripción
// (UTF-16 terminados en cero) y la imagen.
func picture(b []byte) *tag.Picture {
	if len(b) < 5 {
		return nil
	}
	size := int(binary.LittleEndian.Uint32(b[1:]))
	p := 5
	mime, n := utf16Z(b[p:])
	p += n
	desc, n := utf16Z(b[p:])
	p += n
	if size < 0 || p > len(b) || size > len(b)-p {
		return nil
	}
	ext := "jpg"
	if strings.Contains(mime, "png") {
		ext = "png"
	}
	return &tag.Picture{Ext: ext, MIMEType: mime, Type: "Cover (front)", Description: desc, Data: b[p : p+size]}
}

// utf16String lee texto UTF-16LE, sin el cero del final.
func utf16String(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return strings.TrimSpace(string(utf16.Decode(u)))
}

// utf16Z lee texto UTF-16LE terminado en cero; n son los bytes que ocupa
// (con el cero).
func utf16Z(b []byte) (string, int) {
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			return utf16String(b[:i]), i + 2
		}
	}
	return utf16String(b), len(b)
}

// str es el primer campo de texto de names que tenga algo.
func (t *asfTags) str(names ...string) string {
	for _, n := range names {
		if s, ok := t.raw[n].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func (t *asfTags) Format() tag.Format     { return ASF }
func (t *asfTags) FileType() tag.FileType { return WMA }
func (t *asfTags) Title() string          { return t.str("Title", "WM/Title") }
func (t *asfTags) Album() string          { return t.str("WM/AlbumTitle") }
func (t *asfTags) Artist() string         { return t.str("Author", "WM/AlbumArtist") }
func (t *asfTags) AlbumArtist() string    { return t.str("WM/AlbumArtist") }
func (t *asfTags) Composer() string       { return t.str("WM/Composer") }
func (t *asfTags) Genre() string          { return t.str("WM/Genre") }
func (t *asfTags) Lyrics() string         { return t.str("WM/Lyrics") }
func (t *asfTags) Comment() string        { return t.str("Description") }
func (t *asfTags) Picture() *tag.Picture  { return t.pic }
func (t *asfTags) Raw() map[string]any    { return t.raw }

// Year es el año de WM/Year (que puede ser una fecha entera: "2004-05-01").
func (t *asfTags) Year() int {
	s := t.str("WM/Year", "WM/OriginalReleaseYear")
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

// Track es el número de pista: WM/TrackNumber (desde 1) o el viejo
// WM/Track (desde 0); el total, si viene como "3/12".
func (t *asfTags) Track() (int, int) {
	if n, total := numTotal(t.str("WM/TrackNumber")); n > 0 {
		return n, total
	}
	if s := t.str("WM/Track"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n + 1, 0
		}
	}
	return 0, 0
}

// Disc es el disco de WM/PartOfSet ("1/2").
func (t *asfTags) Disc() (int, int) { return numTotal(t.str("WM/PartOfSet")) }

// numTotal lee "n" o "n/total".
func numTotal(s string) (int, int) {
	a, b, _ := strings.Cut(s, "/")
	n, _ := strconv.Atoi(strings.TrimSpace(a))
	total, _ := strconv.Atoi(strings.TrimSpace(b))
	return n, total
}
