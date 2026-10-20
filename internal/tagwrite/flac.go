package tagwrite

import (
	"bytes"
	"io"
)

// FLAC: "fLaC" y los bloques de metadatos (cabecera de 4 bytes: el bit de
// último, el tipo en 7 bits y el tamaño en 24), y luego los frames de audio,
// que se copian tal cual. Se cambian el VORBIS_COMMENT (tipo 4) y los
// PICTURE de portada (tipo 6, imagen tipo 3); el resto de bloques se
// conservan. Si lo nuevo cabe en el espacio que ocupaban los metadatos (con
// el PADDING, tipo 1), el audio queda en el mismo sitio; si no, se deja 4 KB
// de PADDING para la próxima vez.

const (
	flacPadding = 1
	flacComment = 4
	flacPicture = 6
	flacMaxLen  = 1<<24 - 1
)

// flacBlock es un bloque de metadatos.
type flacBlock struct {
	kind byte
	data []byte
}

type flacFile struct {
	prefix       []byte // un ID3v2 delante (raro, pero existe): se conserva
	blocks       []flacBlock
	meta         int // los bytes de metadatos (sin "fLaC")
	audio        []byte
	vc           *vorbisComments
	cover        *Cover
	coverChanged bool
}

func parseFLAC(data []byte) (*flacFile, error) {
	f := &flacFile{}
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		n := 10 + synchsafe(data[6:10])
		if data[5]&0x10 != 0 {
			n += 10
		}
		if n > len(data) {
			return nil, ErrCorrupt
		}
		f.prefix, data = data[:n], data[n:]
	}
	if len(data) < 4 || string(data[:4]) != "fLaC" {
		return nil, ErrCorrupt
	}
	pos := 4
	for {
		if pos+4 > len(data) {
			return nil, ErrCorrupt
		}
		h := data[pos]
		n := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		if pos+4+n > len(data) {
			return nil, ErrCorrupt
		}
		f.blocks = append(f.blocks, flacBlock{kind: h & 0x7f, data: data[pos+4 : pos+4+n]})
		pos += 4 + n
		if h&0x80 != 0 {
			break
		}
	}
	if len(f.blocks) == 0 || f.blocks[0].kind != 0 { // STREAMINFO primero
		return nil, ErrCorrupt
	}
	f.meta = pos - 4
	f.audio = data[pos:]
	for _, b := range f.blocks {
		switch b.kind {
		case flacComment:
			if f.vc == nil {
				vc, _, err := parseVorbisComments(b.data)
				if err != nil {
					return nil, err
				}
				f.vc = vc
			}
		case flacPicture:
			if p, ok := parsePicture(b.data); ok && (f.cover == nil || p.kind == 3) {
				f.cover = &Cover{MIME: p.mime, Data: p.data}
			}
		}
	}
	if f.vc == nil {
		f.vc = &vorbisComments{vendor: "audiofilus"}
	}
	return f, nil
}

func (f *flacFile) tags() Tags {
	t := f.vc.tags()
	t.Cover = f.cover
	return t
}

func (f *flacFile) setText(field, v string) { f.vc.setText(field, v) }
func (f *flacFile) setYear(y int)           { f.vc.setYear(y) }
func (f *flacFile) setTrack(n, total int)   { f.vc.setTrack(n, total) }
func (f *flacFile) setDisc(n, total int)    { f.vc.setDisc(n, total) }
func (f *flacFile) setGain(g *Gain)         { f.vc.setGain(g) }
func (f *flacFile) setCover(c *Cover)       { f.cover, f.coverChanged = c, true }

func (f *flacFile) write(w io.Writer) error {
	// Los bloques nuevos: los de antes sin PADDING, con el comentario
	// nuevo en su sitio (o detrás de STREAMINFO) y las portadas cambiadas.
	var blocks []flacBlock
	comment := flacBlock{kind: flacComment, data: f.vc.bytes()}
	placed := false
	for _, b := range f.blocks {
		switch {
		case b.kind == flacPadding:
			continue
		case b.kind == flacComment:
			if !placed {
				blocks = append(blocks, comment)
				placed = true
			}
			continue
		case b.kind == flacPicture && f.coverChanged:
			if p, ok := parsePicture(b.data); !ok || p.kind == 3 || f.isOnlyCover() {
				continue // la portada vieja (la nueva va al final)
			}
		}
		blocks = append(blocks, b)
	}
	if !placed {
		blocks = append(blocks[:1], append([]flacBlock{comment}, blocks[1:]...)...)
	}
	if f.coverChanged && f.cover != nil {
		blocks = append(blocks, flacBlock{kind: flacPicture, data: pictureBlock(f.cover)})
	}
	used := 0
	for _, b := range blocks {
		if len(b.data) > flacMaxLen {
			return ErrCorrupt // un bloque de más de 16 MB (una portada enorme)
		}
		used += 4 + len(b.data)
	}
	// El relleno: lo que quede del espacio de antes (si cabe al menos su
	// cabecera) o 4 KB nuevos.
	pad := 4096
	switch free := f.meta - used; {
	case free == 0:
		pad = -1
	case free >= 4:
		pad = free - 4
	}
	if pad >= 0 {
		blocks = append(blocks, flacBlock{kind: flacPadding, data: make([]byte, pad)})
	}
	var b bytes.Buffer
	b.Write(f.prefix)
	b.WriteString("fLaC")
	for i, bl := range blocks {
		h := bl.kind
		if i == len(blocks)-1 {
			h |= 0x80
		}
		n := len(bl.data)
		b.Write([]byte{h, byte(n >> 16), byte(n >> 8), byte(n)})
		b.Write(bl.data)
	}
	if _, err := w.Write(b.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(f.audio)
	return err
}

// isOnlyCover dice si el archivo tiene una sola imagen (de cualquier tipo):
// esa es la portada y se cambia aunque no sea la "delantera".
func (f *flacFile) isOnlyCover() bool {
	n := 0
	for _, b := range f.blocks {
		if b.kind == flacPicture {
			n++
		}
	}
	return n == 1
}

// synchsafe lee un entero de 28 bits en 4 bytes de 7 bits (ID3v2).
func synchsafe(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}
