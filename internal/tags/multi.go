package tags

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/dhowden/tag"
)

// Una canción puede tener varios artistas guardados como valores aparte del
// mismo campo: varios ARTIST en los comentarios Vorbis (FLAC, Ogg, Opus) o
// un TPE1 de ID3v2.4 con los nombres separados por ceros. dhowden/tag se
// queda con el último ARTIST de Vorbis y pega los de ID3 sin separarlos
// ("AB"), así que aquí se vuelven a leer esos campos y se muestran todos
// unidos con ", " (lo que ya escriben como un solo texto muchos
// etiquetadores y servicios). Un solo valor ("A feat. B", "A & B") queda tal
// cual.

// artistSep es lo que une los varios artistas de una canción para
// mostrarlos.
const artistSep = ", "

// maxTagBlock es lo más grande que se lee de un bloque de etiquetas para
// buscar artistas (con una carátula dentro pueden pesar varios MB): más es
// un archivo roto.
const maxTagBlock = 64 << 20

// multiArtist son las etiquetas de dhowden/tag con el artista y el del
// álbum que tienen varios valores ya unidos.
type multiArtist struct {
	tag.Metadata
	artist, albumArtist string
}

func (m multiArtist) Artist() string      { return m.artist }
func (m multiArtist) AlbumArtist() string { return m.albumArtist }

// withArtists devuelve m con los artistas de varios valores unidos (ver
// arriba); r es el archivo, con su comienzo (donde empiezan las etiquetas que
// leyó m) en base.
func withArtists(r io.ReadSeeker, base int64, m tag.Metadata) tag.Metadata {
	var artist, albumArtist []string
	switch {
	case m.FileType() == tag.FLAC:
		c := flacComments(r, base)
		artist, albumArtist = vorbisArtists(c)
	case m.FileType() == tag.OGG:
		c := oggComments(r, base)
		artist, albumArtist = vorbisArtists(c)
	case m.Format() == tag.ID3v2_2 || m.Format() == tag.ID3v2_3 || m.Format() == tag.ID3v2_4:
		f := id3TextFrames(r, base, "TPE1", "TP1", "TPE2", "TP2")
		artist, albumArtist = append(f["TPE1"], f["TP1"]...), append(f["TPE2"], f["TP2"]...)
	}
	// Con un solo valor manda lo que leyó dhowden/tag; los de aquí, si hay
	// varios o si ella no encontró ninguno.
	a, aa := m.Artist(), m.AlbumArtist()
	if vals := uniqueArtists(artist); len(vals) > 1 || (a == "" && len(vals) > 0) {
		a = strings.Join(vals, artistSep)
	}
	if vals := uniqueArtists(albumArtist); len(vals) > 1 || (aa == "" && len(vals) > 0) {
		aa = strings.Join(vals, artistSep)
	}
	if a == m.Artist() && aa == m.AlbumArtist() {
		return m
	}
	return multiArtist{Metadata: m, artist: a, albumArtist: aa}
}

// vorbisArtists saca de los comentarios Vorbis c los artistas (ARTIST, o
// ARTISTS, la lista que escribe MusicBrainz Picard, si no hay ARTIST) y los
// del álbum (ALBUMARTIST, o "ALBUM ARTIST").
func vorbisArtists(c map[string][]string) (artist, albumArtist []string) {
	artist = c["artist"]
	if len(artist) == 0 {
		artist = c["artists"]
	}
	albumArtist = c["albumartist"]
	if len(albumArtist) == 0 {
		albumArtist = c["album artist"]
	}
	return artist, albumArtist
}

// uniqueArtists son los artistas de vals sin espacios de sobra, vacíos ni
// repetidos, en orden.
func uniqueArtists(vals []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// flacComments lee los comentarios Vorbis del FLAC que empieza en base de r
// (el bloque VORBIS_COMMENT de sus metadatos), con todos sus valores.
func flacComments(r io.ReadSeeker, base int64) map[string][]string {
	if _, err := r.Seek(base, io.SeekStart); err != nil {
		return nil
	}
	var sig [4]byte
	if _, err := io.ReadFull(r, sig[:]); err != nil || string(sig[:]) != "fLaC" {
		return nil
	}
	for {
		var h [4]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil
		}
		size := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if h[0]&0x7f == 4 { // VORBIS_COMMENT
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil
			}
			return vorbisComments(b)
		}
		if h[0]&0x80 != 0 {
			return nil // era el último bloque
		}
		if _, err := r.Seek(size, io.SeekCurrent); err != nil {
			return nil
		}
	}
}

// oggComments lee los comentarios Vorbis del Ogg (Vorbis u Opus) que empieza
// en base de r: son el segundo paquete del flujo, que puede ocupar varias
// páginas.
func oggComments(r io.ReadSeeker, base int64) map[string][]string {
	if _, err := r.Seek(base, io.SeekStart); err != nil {
		return nil
	}
	var pkt []byte
	n := 0 // paquetes terminados
	for {
		var h [27]byte
		if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "OggS" {
			return nil
		}
		segs := make([]byte, h[26])
		if _, err := io.ReadFull(r, segs); err != nil {
			return nil
		}
		for _, s := range segs {
			data := make([]byte, s)
			if _, err := io.ReadFull(r, data); err != nil {
				return nil
			}
			if n == 1 {
				pkt = append(pkt, data...)
				if len(pkt) > maxTagBlock {
					return nil
				}
			}
			if s < 255 {
				n++
				if n == 2 {
					switch {
					case bytes.HasPrefix(pkt, []byte("\x03vorbis")):
						return vorbisComments(pkt[7:])
					case bytes.HasPrefix(pkt, []byte("OpusTags")):
						return vorbisComments(pkt[8:])
					}
					return nil
				}
			}
		}
	}
}

// vorbisComments lee un bloque de comentarios Vorbis (el fabricante y los
// "CAMPO=valor"), con los nombres en minúsculas y todos los valores de cada
// uno en orden.
func vorbisComments(b []byte) map[string][]string {
	next := func() (string, bool) {
		if len(b) < 4 {
			return "", false
		}
		n := binary.LittleEndian.Uint32(b)
		if uint64(n) > uint64(len(b)-4) {
			return "", false
		}
		s := string(b[4 : 4+n])
		b = b[4+n:]
		return s, true
	}
	if _, ok := next(); !ok { // el fabricante
		return nil
	}
	if len(b) < 4 {
		return nil
	}
	count := binary.LittleEndian.Uint32(b)
	b = b[4:]
	c := map[string][]string{}
	for range count {
		s, ok := next()
		if !ok {
			break
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(k)
		c[k] = append(c[k], v)
	}
	return c
}

// id3TextFrames lee de la etiqueta ID3v2 que empieza en base de r los
// marcos de texto ids, cada uno con todos sus valores (separados por ceros
// en ID3v2.4, y en algunos ID3v2.3 que lo imitan).
func id3TextFrames(r io.ReadSeeker, base int64, ids ...string) map[string][]string {
	if _, err := r.Seek(base, io.SeekStart); err != nil {
		return nil
	}
	var h [10]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:3]) != "ID3" {
		return nil
	}
	ver, flags := h[3], h[5]
	size := synchsafe(h[6:10])
	if size > maxTagBlock {
		return nil
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil
	}
	if ver < 4 && flags&0x80 != 0 {
		b = unsync(b) // en 2.2 y 2.3, la desincronización es de toda la etiqueta
	}
	if flags&0x40 != 0 && ver >= 3 && len(b) >= 4 { // cabecera extendida
		n := int(binary.BigEndian.Uint32(b)) + 4
		if ver == 4 {
			n = synchsafe(b[:4])
		}
		if n > len(b) {
			return nil
		}
		b = b[n:]
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string][]string{}
	hdr, idLen := 10, 4
	if ver == 2 {
		hdr, idLen = 6, 3
	}
	for len(b) >= hdr && b[0] != 0 {
		id := string(b[:idLen])
		var n int
		var fl byte // el segundo byte de banderas (formato)
		switch ver {
		case 2:
			n = int(b[3])<<16 | int(b[4])<<8 | int(b[5])
		case 3:
			n = int(binary.BigEndian.Uint32(b[4:]))
			fl = b[9]
		default:
			n = synchsafe(b[4:8])
			fl = b[9]
		}
		if n < 0 || n > len(b)-hdr {
			break
		}
		data := b[hdr : hdr+n]
		b = b[hdr+n:]
		if !want[id] {
			continue
		}
		switch ver {
		case 3:
			if fl&0xc0 != 0 { // comprimido o cifrado
				continue
			}
			if fl&0x20 != 0 && len(data) > 0 { // byte de grupo
				data = data[1:]
			}
		case 4:
			if fl&0x0c != 0 {
				continue
			}
			if fl&0x40 != 0 && len(data) > 0 {
				data = data[1:]
			}
			if fl&0x01 != 0 && len(data) >= 4 { // largo de los datos
				data = data[4:]
			}
			if fl&0x02 != 0 {
				data = unsync(data)
			}
		}
		out[id] = append(out[id], id3Texts(data)...)
	}
	return out
}

// id3Texts lee los valores de un marco de texto de ID3v2: la codificación
// y los textos separados por ceros (dos en UTF-16), cada uno de UTF-16
// con su propia marca de orden de bytes.
func id3Texts(b []byte) []string {
	if len(b) < 1 {
		return nil
	}
	enc, b := b[0], b[1:]
	var parts [][]byte
	if enc == 1 || enc == 2 {
		for len(b) > 0 {
			i := 0
			for i+1 < len(b) && (b[i] != 0 || b[i+1] != 0) {
				i += 2
			}
			if i+1 >= len(b) {
				parts = append(parts, b)
				break
			}
			parts = append(parts, b[:i])
			b = b[i+2:]
		}
	} else {
		parts = bytes.Split(b, []byte{0})
	}
	var out []string
	for _, p := range parts {
		var s string
		switch enc {
		case 0:
			r := make([]rune, len(p))
			for i, c := range p {
				r[i] = rune(c) // Latin-1
			}
			s = string(r)
		case 1, 2:
			s = utf16Text(p, enc == 2)
		default:
			s = string(p)
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// utf16Text pasa a UTF-8 un texto UTF-16 con marca de orden de bytes, o en
// big-endian sin ella si be.
func utf16Text(b []byte, be bool) string {
	if len(b) >= 2 && !be {
		switch {
		case b[0] == 0xff && b[1] == 0xfe:
			b = b[2:]
		case b[0] == 0xfe && b[1] == 0xff:
			b, be = b[2:], true
		}
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if be {
			u[i] = binary.BigEndian.Uint16(b[i*2:])
		} else {
			u[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
	}
	return string(utf16.Decode(u))
}

// synchsafe lee un entero de 28 bits guardado en 4 bytes de 7.
func synchsafe(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

// unsync deshace la desincronización de ID3v2: un 0x00 detrás de cada 0xFF
// se quita.
func unsync(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte{0xff, 0}, []byte{0xff})
}
