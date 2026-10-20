package tagwrite

import (
	"bytes"
	"encoding/binary"
	"io"
)

// Ogg Vorbis y Ogg Opus: las etiquetas son el segundo paquete de cabecera
// (en Vorbis, 0x03 "vorbis" + comentarios + un bit de cierre; en Opus,
// "OpusTags" + comentarios + lo que venga detrás). Se reescriben las
// páginas de cabecera desde ese paquete (en Vorbis comparte páginas con el
// tercero, el de "setup", que se copia igual) y las de audio se copian con
// el número de secuencia corregido (si cambió cuántas páginas de cabecera
// hay) y su CRC recalculado: sus datos y sus posiciones (granule) no
// cambian. Solo un flujo lógico (sin multiplexar ni encadenar delante).

type oggPage struct {
	flags   byte
	granule uint64
	serial  uint32
	seq     uint32
	lacing  []byte
	body    []byte
}

// parseOggPage lee una página en data[pos:]; devuelve la siguiente
// posición.
func parseOggPage(data []byte, pos int) (oggPage, int, error) {
	var p oggPage
	if pos+27 > len(data) || string(data[pos:pos+4]) != "OggS" || data[pos+4] != 0 {
		return p, 0, ErrCorrupt
	}
	p.flags = data[pos+5]
	p.granule = binary.LittleEndian.Uint64(data[pos+6:])
	p.serial = binary.LittleEndian.Uint32(data[pos+14:])
	p.seq = binary.LittleEndian.Uint32(data[pos+18:])
	n := int(data[pos+26])
	if pos+27+n > len(data) {
		return p, 0, ErrCorrupt
	}
	p.lacing = data[pos+27 : pos+27+n]
	size := 0
	for _, l := range p.lacing {
		size += int(l)
	}
	start := pos + 27 + n
	if start+size > len(data) {
		return p, 0, ErrCorrupt
	}
	p.body = data[start : start+size]
	return p, start + size, nil
}

// bytes escribe la página con su CRC.
func (p oggPage) bytes() []byte {
	b := make([]byte, 27+len(p.lacing)+len(p.body))
	copy(b, "OggS")
	b[5] = p.flags
	binary.LittleEndian.PutUint64(b[6:], p.granule)
	binary.LittleEndian.PutUint32(b[14:], p.serial)
	binary.LittleEndian.PutUint32(b[18:], p.seq)
	b[26] = byte(len(p.lacing))
	copy(b[27:], p.lacing)
	copy(b[27+len(p.lacing):], p.body)
	binary.LittleEndian.PutUint32(b[22:], oggCRC(b))
	return b
}

type oggFile struct {
	first   oggPage   // la página del primer paquete (identificación), tal cual
	packets [][]byte  // los paquetes de cabecera después del primero
	audio   []oggPage // las páginas de audio, tal cual (salvo la secuencia)
	nHeader int       // cuántas páginas de cabecera había después de la primera
	opus    bool
	vc      *vorbisComments
	rest    []byte // lo que va detrás de los comentarios en OpusTags
	tail    []byte // bytes detrás de la última página (basura): se conservan
}

func parseOgg(data []byte) (*oggFile, error) {
	f := &oggFile{}
	first, pos, err := parseOggPage(data, 0)
	if err != nil {
		return nil, err
	}
	if first.flags&0x02 == 0 || len(first.lacing) == 0 || first.lacing[len(first.lacing)-1] == 255 {
		return nil, ErrCorrupt // la primera página: solo el paquete de identificación
	}
	f.first = first
	switch {
	case bytes.HasPrefix(first.body, []byte("\x01vorbis")):
	case bytes.HasPrefix(first.body, []byte("OpusHead")):
		f.opus = true
	default:
		return nil, ErrUnsupported
	}
	want := 2 // los paquetes de cabecera que faltan: comentarios y setup (Vorbis)
	if f.opus {
		want = 1
	}
	var cur []byte
	inPacket := false
	for len(f.packets) < want {
		p, next, err := parseOggPage(data, pos)
		if err != nil {
			return nil, err
		}
		if p.serial != first.serial {
			return nil, ErrUnsupported // otro flujo mezclado en las cabeceras
		}
		pos = next
		f.nHeader++
		off := 0
		for i, l := range p.lacing {
			cur = append(cur, p.body[off:off+int(l)]...)
			off += int(l)
			inPacket = true
			if l < 255 {
				f.packets = append(f.packets, cur)
				cur, inPacket = nil, false
				if len(f.packets) == want && i != len(p.lacing)-1 {
					return nil, ErrUnsupported // audio en la misma página que la cabecera
				}
			}
		}
	}
	if inPacket {
		return nil, ErrCorrupt
	}
	for pos < len(data) {
		if pos+4 > len(data) || string(data[pos:pos+4]) != "OggS" {
			f.tail = data[pos:]
			break
		}
		p, next, err := parseOggPage(data, pos)
		if err != nil {
			return nil, err
		}
		f.audio = append(f.audio, p)
		pos = next
	}
	c := f.packets[0]
	switch {
	case !f.opus && bytes.HasPrefix(c, []byte("\x03vorbis")):
		vc, rest, err := parseVorbisComments(c[7:])
		if err != nil {
			return nil, err
		}
		if len(rest) < 1 || rest[0]&1 == 0 {
			return nil, ErrCorrupt // sin el bit de cierre
		}
		f.vc = vc
	case f.opus && bytes.HasPrefix(c, []byte("OpusTags")):
		vc, rest, err := parseVorbisComments(c[8:])
		if err != nil {
			return nil, err
		}
		f.vc, f.rest = vc, rest
		f.vc.opus = true
	default:
		return nil, ErrCorrupt
	}
	return f, nil
}

func (f *oggFile) tags() Tags              { return f.vc.tags() }
func (f *oggFile) setText(field, v string) { f.vc.setText(field, v) }
func (f *oggFile) setYear(y int)           { f.vc.setYear(y) }
func (f *oggFile) setTrack(n, total int)   { f.vc.setTrack(n, total) }
func (f *oggFile) setDisc(n, total int)    { f.vc.setDisc(n, total) }
func (f *oggFile) setGain(g *Gain)         { f.vc.setGain(g) }
func (f *oggFile) setCover(c *Cover)       { f.vc.setCover(c) }

func (f *oggFile) write(w io.Writer) error {
	// El paquete de comentarios nuevo y los demás de cabecera, igual.
	var comment []byte
	if f.opus {
		comment = append([]byte("OpusTags"), f.vc.bytes()...)
		comment = append(comment, f.rest...)
	} else {
		comment = append([]byte("\x03vorbis"), f.vc.bytes()...)
		comment = append(comment, 1)
	}
	packets := append([][]byte{comment}, f.packets[1:]...)
	header := paginate(packets, f.first.serial, 1)
	if _, err := w.Write(f.first.bytes()); err != nil {
		return err
	}
	for _, p := range header {
		if _, err := w.Write(p.bytes()); err != nil {
			return err
		}
	}
	// Las de audio, con la secuencia corrida (si cambió).
	delta := uint32(len(header) - f.nHeader)
	for _, p := range f.audio {
		if p.serial == f.first.serial {
			p.seq += delta
		}
		if _, err := w.Write(p.bytes()); err != nil {
			return err
		}
	}
	_, err := w.Write(f.tail)
	return err
}

// paginate reparte paquetes en páginas (de hasta 255 segmentos), con la
// posición 0 (las de cabecera) y secuencia desde seq: el último paquete
// termina su página.
func paginate(packets [][]byte, serial, seq uint32) []oggPage {
	var pages []oggPage
	cur := oggPage{serial: serial, seq: seq}
	flush := func(cont bool) {
		pages = append(pages, cur)
		seq++
		cur = oggPage{serial: serial, seq: seq}
		if cont {
			cur.flags = 0x01 // sigue un paquete de la página anterior
		}
	}
	for _, pk := range packets {
		rest := pk
		for {
			n := min(len(rest), 255)
			if len(cur.lacing) == 255 {
				flush(true)
			}
			cur.lacing = append(cur.lacing, byte(n))
			cur.body = append(cur.body, rest[:n]...)
			rest = rest[n:]
			if n < 255 {
				break // fin del paquete (un segmento de menos de 255)
			}
			if len(rest) == 0 {
				// Un paquete múltiplo de 255: termina con un segmento de 0.
				if len(cur.lacing) == 255 {
					flush(true)
				}
				cur.lacing = append(cur.lacing, 0)
				break
			}
		}
	}
	if len(cur.lacing) > 0 {
		pages = append(pages, cur)
	}
	return pages
}

// oggCRC es el CRC de una página (polinomio 0x04c11db7, sin reflejar, con
// el campo del CRC a cero).
func oggCRC(b []byte) uint32 {
	var crc uint32
	for i, c := range b {
		if i >= 22 && i < 26 {
			c = 0
		}
		crc = crc<<8 ^ oggTable[byte(crc>>24)^c]
	}
	return crc
}

var oggTable = func() [256]uint32 {
	var t [256]uint32
	for i := range t {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()
