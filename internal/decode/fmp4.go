package decode

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MP4 fragmentado en memoria, para la radio HLS con trozos fMP4 (#EXT-X-MAP):
// el trozo inicial trae la pista (moov) y cada trozo, cajas moof + mdat con
// las muestras. Usa lo mismo que openMP4 para leer las cajas y la pista.

// FMP4Track es la pista de audio de un trozo inicial fMP4.
type FMP4Track struct {
	ID     uint32 // track_ID (los trozos pueden traer más de una pista)
	Codec  string // "mp4a" (AAC); otros no se entienden
	Config []byte // AudioSpecificConfig
	// Los valores por defecto de mvex/trex, si un trozo no trae los suyos.
	defDur, defSize uint32
}

// ParseFMP4Init lee un trozo inicial fMP4 (ftyp + moov) y devuelve su
// primera pista de audio AAC.
func ParseFMP4Init(init []byte) (*FMP4Track, error) {
	moov := mp4Find(init, "moov")
	if moov == nil {
		return nil, errors.New("fmp4: no moov in init segment")
	}
	be := binary.BigEndian
	for _, trak := range mp4Children(moov) {
		if trak.typ != "trak" {
			continue
		}
		hdlr := mp4Find(trak.data, "mdia", "hdlr")
		if len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
			continue
		}
		t, err := parseTrack(mp4Find(trak.data, "mdia"), true)
		if err != nil {
			return nil, err
		}
		if t.codec != "mp4a" {
			return nil, fmt.Errorf("%w (fMP4 codec %q)", ErrUnsupported, t.codec)
		}
		ft := &FMP4Track{Codec: t.codec, Config: t.config}
		if tkhd := mp4Find(trak.data, "tkhd"); len(tkhd) >= 16 {
			if tkhd[0] == 1 {
				if len(tkhd) >= 24 {
					ft.ID = be.Uint32(tkhd[20:])
				}
			} else {
				ft.ID = be.Uint32(tkhd[12:])
			}
		}
		for _, trex := range mp4Children(mp4Find(moov, "mvex")) {
			if trex.typ == "trex" && len(trex.data) >= 24 && be.Uint32(trex.data[4:]) == ft.ID {
				ft.defDur, ft.defSize = be.Uint32(trex.data[12:]), be.Uint32(trex.data[16:])
			}
		}
		return ft, nil
	}
	return nil, fmt.Errorf("%w (fMP4 init segment without an audio track)", ErrUnsupported)
}

// FMP4Samples saca de un trozo fMP4 (uno o más moof + mdat) las muestras de
// la pista t, en orden.
func FMP4Samples(t *FMP4Track, seg []byte) ([][]byte, error) {
	be := binary.BigEndian
	var out [][]byte
	pos := 0
	for pos+8 <= len(seg) {
		size, h := int(be.Uint32(seg[pos:])), 8
		if size == 1 {
			if pos+16 > len(seg) {
				break
			}
			size, h = int(be.Uint64(seg[pos+8:])), 16
		} else if size == 0 {
			size = len(seg) - pos
		}
		if size < h || pos+size > len(seg) {
			break
		}
		if string(seg[pos+4:pos+8]) == "moof" {
			moofStart := pos
			for _, traf := range mp4Children(seg[pos+h : pos+size]) {
				if traf.typ != "traf" {
					continue
				}
				tfhd := mp4Find(traf.data, "tfhd")
				if len(tfhd) < 8 || (t.ID != 0 && be.Uint32(tfhd[4:]) != t.ID) {
					continue
				}
				flags := be.Uint32(tfhd) & 0xffffff
				// Sin base explícita, los desplazamientos van desde el
				// principio del moof (default-base-is-moof, lo que usa HLS).
				base, sz := int64(moofStart), t.defSize
				i := 8
				if flags&0x1 != 0 && len(tfhd) >= i+8 {
					base = int64(be.Uint64(tfhd[i:]))
					i += 8
				}
				if flags&0x2 != 0 {
					i += 4
				}
				if flags&0x8 != 0 {
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
						j += 4
					}
					for k := 0; k < n; k++ {
						s := sz
						if tf&0x100 != 0 {
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
						if off < 0 || off+int64(s) > int64(len(seg)) {
							return out, errors.New("fmp4: sample outside the segment")
						}
						out = append(out, seg[off:off+int64(s)])
						off += int64(s)
					}
				}
			}
		}
		pos += size
	}
	return out, nil
}

// adtsRates son las frecuencias de los índices de AAC.
var adtsRates = []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// ADTSHeader arma la cabecera ADTS (7 bytes) de un frame AAC de n bytes a
// partir de su AudioSpecificConfig, para pasar AAC de un MP4 al decodificador
// de flujos, que come ADTS. Con HE-AAC (SBR, PS) se usa el perfil y la
// frecuencia del núcleo: el decodificador encuentra la extensión solo.
func ADTSHeader(asc []byte, n int) ([]byte, error) {
	if len(asc) < 2 {
		return nil, errors.New("aac: short AudioSpecificConfig")
	}
	r := bitReader{b: asc}
	obj := r.read(5)
	if obj == 31 {
		obj = 32 + r.read(6)
	}
	freq := r.read(4)
	if freq == 15 {
		rate := r.read(24)
		freq = 15
		for i, v := range adtsRates {
			if v == rate {
				freq = i
			}
		}
	}
	ch := r.read(4)
	if obj == 5 || obj == 29 { // SBR, PS: viene el núcleo después
		r.read(4) // frecuencia de la extensión
		obj = r.read(5)
	}
	if r.bad || freq > 12 || obj < 1 || obj > 4 {
		return nil, fmt.Errorf("%w (AAC object type %d in fMP4)", ErrUnsupported, obj)
	}
	length := n + 7
	return []byte{
		0xFF, 0xF1,
		byte((obj-1)<<6 | freq<<2 | ch>>2),
		byte((ch&3)<<6 | length>>11),
		byte(length >> 3),
		byte((length&7)<<5 | 0x1F),
		0xFC,
	}, nil
}

// bitReader lee bits de un AudioSpecificConfig (bad si se acaban).
type bitReader struct {
	b   []byte
	pos int
	bad bool
}

func (r *bitReader) read(n int) int {
	v := 0
	for ; n > 0; n-- {
		if r.pos >= len(r.b)*8 {
			r.bad = true
			return 0
		}
		v = v<<1 | int(r.b[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v
}
