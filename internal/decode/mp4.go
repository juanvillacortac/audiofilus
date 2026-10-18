package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

// Lector mínimo de MP4/M4A en Go puro: encuentra la primera pista de audio y su
// tabla de muestras (dónde empieza y cuánto mide cada bloque comprimido, y
// cuánto dura). Lo usan AAC (aac.go) y ALAC (alac.go), que solo decodifican
// bloques sueltos. También lee MP4 fragmentado (el .m4a que sirve YouTube): la
// tabla sale de las cajas moof en vez de la stbl.

// mp4Track es la pista de audio de un MP4.
type mp4Track struct {
	codec      string // "mp4a" (AAC) o "alac"
	timescale  int64  // unidades de tiempo por segundo de las duraciones
	channels   int
	sampleRate int
	config     []byte // AudioSpecificConfig (AAC) o la "cookie" de ALAC

	offsets []int64  // dónde empieza cada muestra en el archivo
	sizes   []uint32 // cuánto mide
	starts  []int64  // cuándo empieza cada una (en timescale), con una más al final
}

// samples es cuántas muestras (bloques) tiene la pista.
func (t *mp4Track) samples() int { return len(t.sizes) }

// duration es la duración total en unidades de timescale.
func (t *mp4Track) duration() int64 { return t.starts[len(t.starts)-1] }

// sampleAt es la muestra que contiene el instante tm (en timescale).
func (t *mp4Track) sampleAt(tm int64) int {
	i := sort.Search(t.samples(), func(i int) bool { return t.starts[i+1] > tm })
	return min(i, t.samples()-1)
}

// mp4Box es una caja: su tipo y su contenido.
type mp4Box struct {
	typ  string
	data []byte
}

// mp4Children separa las cajas seguidas de b.
func mp4Children(b []byte) []mp4Box {
	var out []mp4Box
	for len(b) >= 8 {
		size := int64(binary.BigEndian.Uint32(b))
		typ := string(b[4:8])
		hdr := int64(8)
		switch size {
		case 1:
			if len(b) < 16 {
				return out
			}
			size, hdr = int64(binary.BigEndian.Uint64(b[8:])), 16
		case 0:
			size = int64(len(b))
		}
		if size < hdr || size > int64(len(b)) {
			return out
		}
		out = append(out, mp4Box{typ, b[hdr:size]})
		b = b[size:]
	}
	return out
}

// mp4Find busca la primera caja con esa ruta de tipos dentro de b.
func mp4Find(b []byte, path ...string) []byte {
	for _, typ := range path {
		found := false
		for _, c := range mp4Children(b) {
			if c.typ == typ {
				b, found = c.data, true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return b
}

// readMoov lee la caja moov del archivo (puede estar antes o después de los datos).
func readMoov(f *os.File) ([]byte, error) {
	var pos int64
	for {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], pos); err != nil {
			return nil, errors.New("mp4: no moov box")
		}
		size := int64(binary.BigEndian.Uint32(hdr[:]))
		h := int64(8)
		if size == 1 {
			if _, err := f.ReadAt(hdr[8:16], pos+8); err != nil {
				return nil, err
			}
			size, h = int64(binary.BigEndian.Uint64(hdr[8:])), 16
		}
		if string(hdr[4:8]) == "moov" {
			if size < h || size > 64<<20 {
				return nil, errors.New("mp4: bad moov size")
			}
			b := make([]byte, size-h)
			if _, err := f.ReadAt(b, pos+h); err != nil {
				return nil, err
			}
			return b, nil
		}
		if size < h {
			return nil, errors.New("mp4: no moov box")
		}
		pos += size
	}
}

// openMP4 lee la pista de audio de f.
func openMP4(f *os.File) (*mp4Track, error) {
	moov, err := readMoov(f)
	if err != nil {
		return nil, err
	}
	for _, trak := range mp4Children(moov) {
		if trak.typ != "trak" {
			continue
		}
		mdia := mp4Find(trak.data, "mdia")
		if hdlr := mp4Find(mdia, "hdlr"); len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
			continue
		}
		mvex := mp4Find(moov, "mvex")
		t, err := parseTrack(mdia, mvex != nil)
		if err != nil {
			return nil, err
		}
		if t.samples() == 0 && mvex != nil {
			if err := t.readFragments(f, mvex); err != nil {
				return nil, err
			}
		}
		if t.samples() == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return t, nil
	}
	return nil, errors.New("mp4: no audio track")
}

// readFragments arma la tabla de muestras de un MP4 fragmentado: recorre las
// cajas moof del archivo y, de cada traf, saca con tfhd y trun (y los valores
// por defecto de trex, en mvex) dónde empieza, cuánto mide y cuánto dura cada
// muestra. Se supone una sola pista, como en el audio de YouTube.
func (t *mp4Track) readFragments(f *os.File, mvex []byte) error {
	be := binary.BigEndian
	var defDur, defSize uint32
	if trex := mp4Find(mvex, "trex"); len(trex) >= 24 {
		defDur, defSize = be.Uint32(trex[12:]), be.Uint32(trex[16:])
	}
	t.offsets, t.sizes, t.starts = nil, nil, nil
	var tm int64
	var pos int64
	for {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], pos); err != nil {
			break
		}
		size, h := int64(be.Uint32(hdr[:])), int64(8)
		if size == 1 {
			if _, err := f.ReadAt(hdr[8:16], pos+8); err != nil {
				break
			}
			size, h = int64(be.Uint64(hdr[8:])), 16
		}
		if size < h {
			break
		}
		if string(hdr[4:8]) == "moof" && size < 16<<20 {
			moof := make([]byte, size-h)
			if _, err := f.ReadAt(moof, pos+h); err != nil {
				break
			}
			for _, traf := range mp4Children(moof) {
				if traf.typ != "traf" {
					continue
				}
				tfhd := mp4Find(traf.data, "tfhd")
				if len(tfhd) < 8 {
					continue
				}
				flags := be.Uint32(tfhd) & 0xffffff
				base, dur, sz := pos, defDur, defSize // por defecto, el principio del moof
				i := 8
				if flags&0x1 != 0 && len(tfhd) >= i+8 {
					base = int64(be.Uint64(tfhd[i:]))
					i += 8
				}
				if flags&0x2 != 0 {
					i += 4 // sample_description_index
				}
				if flags&0x8 != 0 && len(tfhd) >= i+4 {
					dur = be.Uint32(tfhd[i:])
					i += 4
				}
				if flags&0x10 != 0 && len(tfhd) >= i+4 {
					sz = be.Uint32(tfhd[i:])
				}
				for _, c := range mp4Children(traf.data) {
					if c.typ != "trun" || len(c.data) < 8 {
						continue
					}
					tr := c.data
					tf := be.Uint32(tr) & 0xffffff
					n := int(be.Uint32(tr[4:]))
					j := 8
					off := base
					if tf&0x1 != 0 && len(tr) >= j+4 {
						off = base + int64(int32(be.Uint32(tr[j:])))
						j += 4
					}
					if tf&0x4 != 0 {
						j += 4 // first_sample_flags
					}
					for k := 0; k < n; k++ {
						d, s := dur, sz
						if tf&0x100 != 0 {
							if len(tr) < j+4 {
								break
							}
							d = be.Uint32(tr[j:])
							j += 4
						}
						if tf&0x200 != 0 {
							if len(tr) < j+4 {
								break
							}
							s = be.Uint32(tr[j:])
							j += 4
						}
						if tf&0x400 != 0 {
							j += 4
						}
						if tf&0x800 != 0 {
							j += 4
						}
						t.offsets = append(t.offsets, off)
						t.sizes = append(t.sizes, s)
						t.starts = append(t.starts, tm)
						off += int64(s)
						tm += int64(d)
					}
				}
			}
		}
		pos += size
	}
	t.starts = append(t.starts, tm)
	return nil
}

// parseTrack lee la pista; fragmented dice que sus muestras están en cajas
// moof (y la stbl puede venir vacía).
func parseTrack(mdia []byte, fragmented bool) (*mp4Track, error) {
	t := &mp4Track{}
	mdhd := mp4Find(mdia, "mdhd")
	switch {
	case len(mdhd) >= 24 && mdhd[0] == 0:
		t.timescale = int64(binary.BigEndian.Uint32(mdhd[12:]))
	case len(mdhd) >= 32 && mdhd[0] == 1:
		t.timescale = int64(binary.BigEndian.Uint32(mdhd[20:]))
	}
	if t.timescale <= 0 {
		return nil, errors.New("mp4: bad mdhd")
	}
	stbl := mp4Find(mdia, "minf", "stbl")
	stsd := mp4Find(stbl, "stsd")
	if len(stsd) < 8 {
		return nil, errors.New("mp4: no stsd")
	}
	entries := mp4Children(stsd[8:])
	if len(entries) == 0 || len(entries[0].data) < 28 {
		return nil, errors.New("mp4: no sample description")
	}
	e := entries[0]
	t.codec = e.typ
	t.channels = int(binary.BigEndian.Uint16(e.data[16:]))
	t.sampleRate = int(binary.BigEndian.Uint32(e.data[24:]) >> 16)
	switch e.typ {
	case "mp4a":
		t.config = escConfig(mp4Find(e.data[28:], "esds"))
		if t.config == nil {
			return nil, errors.New("mp4: no AAC config in esds")
		}
	case "alac":
		t.config = mp4Find(e.data[28:], "alac")
		if len(t.config) < 4+24 {
			return nil, errors.New("mp4: no ALAC cookie")
		}
		t.config = t.config[4:] // sin versión ni flags
	default:
		return nil, fmt.Errorf("%w (MP4 codec %q)", ErrUnsupported, e.typ)
	}
	if err := t.parseTables(stbl); err != nil {
		if !fragmented {
			return nil, err
		}
		t.offsets, t.sizes, t.starts = nil, nil, []int64{0}
	}
	return t, nil
}

// escConfig saca el AudioSpecificConfig de una caja esds.
func escConfig(esds []byte) []byte {
	if len(esds) < 4 {
		return nil
	}
	b := esds[4:]
	// desc lee un descriptor: su etiqueta y su contenido.
	desc := func(b []byte) (byte, []byte, []byte) {
		if len(b) < 2 {
			return 0, nil, nil
		}
		tag, n, i := b[0], 0, 1
		for ; i < len(b) && i <= 4; i++ {
			n = n<<7 | int(b[i]&0x7f)
			if b[i]&0x80 == 0 {
				i++
				break
			}
		}
		if i+n > len(b) {
			return 0, nil, nil
		}
		return tag, b[i : i+n], b[i+n:]
	}
	tag, es, _ := desc(b)
	if tag != 0x03 || len(es) < 3 {
		return nil
	}
	flags := es[2]
	es = es[3:]
	if flags&0x80 != 0 {
		es = es[min(2, len(es)):]
	}
	if flags&0x40 != 0 && len(es) > 0 {
		es = es[min(1+int(es[0]), len(es)):]
	}
	if flags&0x20 != 0 {
		es = es[min(2, len(es)):]
	}
	tag, dc, _ := desc(es)
	if tag != 0x04 || len(dc) < 13 {
		return nil
	}
	tag, asc, _ := desc(dc[13:])
	if tag != 0x05 {
		return nil
	}
	return asc
}

// parseTables expande stsz, stsc, stco/co64 y stts en offsets, sizes y starts.
func (t *mp4Track) parseTables(stbl []byte) error {
	be := binary.BigEndian
	stsz := mp4Find(stbl, "stsz")
	if len(stsz) < 12 {
		return errors.New("mp4: no stsz")
	}
	fixed, n := be.Uint32(stsz[4:]), int(be.Uint32(stsz[8:]))
	if fixed == 0 && len(stsz) < 12+4*n {
		return errors.New("mp4: short stsz")
	}
	t.sizes = make([]uint32, n)
	for i := range t.sizes {
		if fixed != 0 {
			t.sizes[i] = fixed
		} else {
			t.sizes[i] = be.Uint32(stsz[12+4*i:])
		}
	}
	var chunks []int64
	if stco := mp4Find(stbl, "stco"); len(stco) >= 8 {
		c := int(be.Uint32(stco[4:]))
		for i := 0; i < c && 8+4*i+4 <= len(stco); i++ {
			chunks = append(chunks, int64(be.Uint32(stco[8+4*i:])))
		}
	} else if co64 := mp4Find(stbl, "co64"); len(co64) >= 8 {
		c := int(be.Uint32(co64[4:]))
		for i := 0; i < c && 8+8*i+8 <= len(co64); i++ {
			chunks = append(chunks, int64(be.Uint64(co64[8+8*i:])))
		}
	}
	stsc := mp4Find(stbl, "stsc")
	if len(chunks) == 0 || len(stsc) < 8 {
		return errors.New("mp4: no chunk tables")
	}
	runs := int(be.Uint32(stsc[4:]))
	t.offsets = make([]int64, 0, n)
	for r := 0; r < runs && 8+12*r+12 <= len(stsc); r++ {
		first := int(be.Uint32(stsc[8+12*r:])) - 1
		per := int(be.Uint32(stsc[8+12*r+4:]))
		last := len(chunks)
		if r+1 < runs && 8+12*(r+1)+4 <= len(stsc) {
			last = int(be.Uint32(stsc[8+12*(r+1):])) - 1
		}
		for c := first; c < last && c < len(chunks); c++ {
			off := chunks[c]
			for k := 0; k < per && len(t.offsets) < n; k++ {
				t.offsets = append(t.offsets, off)
				off += int64(t.sizes[len(t.offsets)-1])
			}
		}
	}
	if len(t.offsets) < n {
		n = len(t.offsets)
		t.sizes = t.sizes[:n]
	}
	stts := mp4Find(stbl, "stts")
	if len(stts) < 8 {
		return errors.New("mp4: no stts")
	}
	t.starts = make([]int64, 0, n+1)
	var tm int64
	for e := 0; e < int(be.Uint32(stts[4:])) && 8+8*e+8 <= len(stts); e++ {
		count, delta := int(be.Uint32(stts[8+8*e:])), int64(be.Uint32(stts[8+8*e+4:]))
		for k := 0; k < count && len(t.starts) < n; k++ {
			t.starts = append(t.starts, tm)
			tm += delta
		}
	}
	for len(t.starts) < n { // stts corta: se supone la última duración
		t.starts = append(t.starts, tm)
	}
	t.starts = append(t.starts, tm)
	if n == 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}
