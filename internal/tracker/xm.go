package tracker

import (
	"encoding/binary"
	"errors"
	"math"
)

// Los módulos de FastTracker 2 (XM): hasta 32 canales (se quedan los 8
// primeros), patrones de largo variable, instrumentos con varios samples
// repartidos por el teclado (se usa el de C-4) y envolventes de volumen (se
// aproximan con el ADSR del instrumento). Los efectos van como en mod.go;
// de la columna de volumen se toman el volumen, el paneo y el portamento.

// xmMagic es como empieza un XM.
const xmMagic = "Extended Module: "

// ImportXM lee un módulo XM como canción del tracker.
func ImportXM(data []byte) (*Song, error) {
	if len(data) < 80 || string(data[:17]) != xmMagic {
		return nil, errors.New("not an XM file")
	}
	le := binary.LittleEndian
	hsize := int(le.Uint32(data[60:]))
	if 60+hsize > len(data) || hsize < 20 {
		return nil, errors.New("XM header is truncated")
	}
	h := data[64:]
	songLen := int(le.Uint16(h[0:]))
	channels := int(le.Uint16(h[4:]))
	npat := int(le.Uint16(h[6:]))
	ninst := int(le.Uint16(h[8:]))
	flags := le.Uint16(h[10:])
	m := &modSong{name: cstr(data[17:37]), channels: channels, linear: flags&1 != 0,
		speed: int(le.Uint16(h[12:])), bpm: int(le.Uint16(h[14:]))}
	if m.speed == 0 {
		m.speed = 6
	}
	if m.bpm == 0 {
		m.bpm = 125
	}
	for i := 0; i < min(songLen, 256) && 16+i < hsize-4; i++ {
		m.orders = append(m.orders, int(h[16+i]))
	}
	off := 60 + hsize
	for p := 0; p < npat; p++ {
		if off+9 > len(data) {
			return nil, errors.New("XM file is truncated (patterns)")
		}
		plen := int(le.Uint32(data[off:]))
		rows := int(le.Uint16(data[off+5:]))
		packed := int(le.Uint16(data[off+7:]))
		off += plen
		if rows == 0 {
			rows = 64
		}
		pat := make(modPattern, rows)
		for r := range pat {
			pat[r] = make([]modCell, channels)
			for c := range pat[r] {
				pat[r][c] = modCell{vol: -1, pan: -1, porta: -1}
			}
		}
		end := min(len(data), off+packed)
		pos := off
		next := func() byte {
			if pos >= end {
				return 0
			}
			b := data[pos]
			pos++
			return b
		}
		for r := 0; r < rows && packed > 0; r++ {
			for c := 0; c < channels; c++ {
				var note, inst, vol, fx, param byte
				b := next()
				if b&0x80 != 0 {
					if b&1 != 0 {
						note = next()
					}
					if b&2 != 0 {
						inst = next()
					}
					if b&4 != 0 {
						vol = next()
					}
					if b&8 != 0 {
						fx = next()
					}
					if b&16 != 0 {
						param = next()
					}
				} else {
					note, inst, vol, fx, param = b, next(), next(), next(), next()
				}
				cell := &pat[r][c]
				switch {
				case note == 97:
					cell.note = NoteOff
				case note >= 1 && note <= 96:
					cell.note = int8(note) // los dos empiezan en C-0 = 1
				}
				cell.instr, cell.fx, cell.param = int(inst), fx, param
				switch {
				case vol >= 0x10 && vol <= 0x50:
					cell.vol = int(vol) - 0x10
				case vol >= 0xC0 && vol <= 0xCF:
					cell.pan = int(vol&15) * 17
				case vol >= 0xF0:
					cell.porta = int(vol&15) * 16
				case vol >= 0x60 && vol <= 0x6F:
					cell.slide = -int(vol & 15)
				case vol >= 0x70 && vol <= 0x7F:
					cell.slide = int(vol & 15)
				case vol >= 0x80 && vol <= 0x8F:
					cell.fine = -int(vol & 15)
				case vol >= 0x90 && vol <= 0x9F:
					cell.fine = int(vol & 15)
				}
			}
		}
		off += packed
		m.patterns = append(m.patterns, pat)
	}
	// Los instrumentos: su cabecera, las de sus samples y los datos.
	for i := 0; i < ninst; i++ {
		if off+29 > len(data) {
			break
		}
		isize := int(le.Uint32(data[off:]))
		name := cstr(data[off+4 : off+26])
		nsmp := int(le.Uint16(data[off+27:]))
		ih := data[off:min(len(data), off+max(isize, 29))]
		off += max(isize, 29)
		if nsmp == 0 {
			m.instrs, m.samples, m.defVol = append(m.instrs, nil), append(m.samples, nil), append(m.defVol, 64)
			continue
		}
		type smp struct {
			length, loopStart, loopLen, vol, typ, pan int
			fine, rel                                 int
		}
		smps := make([]smp, nsmp)
		shsize := 40
		if len(ih) >= 33 {
			if v := int(le.Uint32(ih[29:])); v >= 40 {
				shsize = v
			}
		}
		for k := range smps {
			if off+40 > len(data) {
				return nil, errors.New("XM file is truncated (samples)")
			}
			sh := data[off:]
			smps[k] = smp{length: int(le.Uint32(sh[0:])), loopStart: int(le.Uint32(sh[4:])), loopLen: int(le.Uint32(sh[8:])),
				vol: min(64, int(sh[12])), fine: int(int8(sh[13])), typ: int(sh[14]), pan: int(sh[15]), rel: int(int8(sh[16]))}
			off += shsize
		}
		// El sample de C-4 (el del mapa del teclado), o el primero.
		pick := 0
		if len(ih) >= 33+96 {
			pick = int(ih[33+48])
		}
		if pick >= nsmp {
			pick = 0
		}
		var d *SampleData
		var in *Instrument
		dv := 64
		for k, sm := range smps {
			if off+sm.length > len(data) {
				return nil, errors.New("XM file is truncated (sample data)")
			}
			raw := data[off : off+sm.length]
			off += sm.length
			if k != pick || sm.length == 0 {
				continue
			}
			bits16 := sm.typ&16 != 0
			d = &SampleData{Name: name, Bits: 8, Data: undelta(raw, bits16)}
			if bits16 {
				d.Bits = 16
			}
			// A C-4 (49) con nota relativa 0 y sin ajuste fino suena a 8363 Hz.
			d.Rate = int(math.Round(8363 * math.Pow(2, float64(sm.fine)/1536)))
			div := 1
			if bits16 {
				div = 2
			}
			if loop := sm.typ & 3; loop != 0 && sm.loopLen/div > 1 {
				d.LoopStart, d.LoopLen, d.PingPong = sm.loopStart/div, sm.loopLen/div, loop == 2
				if d.LoopStart+d.LoopLen > d.Len() {
					d.LoopLen = max(0, d.Len()-d.LoopStart)
				}
			}
			in = sampleInstrument(name, d.LoopLen > 1)
			dv = sm.vol
			in.Root = int8(max(1, min(NoteMax, 49-sm.rel)))
			if sm.pan < 0x50 {
				in.Pan = -1
			} else if sm.pan > 0xB0 {
				in.Pan = 1
			}
			xmEnvelope(in, ih)
		}
		m.instrs, m.samples, m.defVol = append(m.instrs, in), append(m.samples, d), append(m.defVol, dv)
	}
	return m.toSong()
}

// undelta deshace la codificación delta de los samples de XM.
func undelta(raw []byte, bits16 bool) []byte {
	out := make([]byte, len(raw))
	if bits16 {
		var acc int16
		for i := 0; i+1 < len(raw); i += 2 {
			acc += int16(binary.LittleEndian.Uint16(raw[i:]))
			binary.LittleEndian.PutUint16(out[i:], uint16(acc))
		}
		return out[:len(raw)&^1]
	}
	var acc int8
	for i, b := range raw {
		acc += int8(b)
		out[i] = byte(acc)
	}
	return out
}

// xmEnvelope aproxima la envolvente de volumen de un instrumento XM (ih es
// su cabecera) con el ADSR: el ataque hasta el punto más alto, la caída
// hasta el de sostener (o el último) y lo que dura el fadeout al soltar.
// Los ticks del XM duran lo mismo que los del tracker con el mismo BPM.
func xmEnvelope(in *Instrument, ih []byte) {
	if len(ih) < 243 {
		return
	}
	le := binary.LittleEndian
	n := min(12, int(ih[225]))
	typ := ih[233]
	fade := int(le.Uint16(ih[239:]))
	if fade > 0 {
		in.Env.Release = max(1, min(255, 32768/fade))
	}
	if typ&1 == 0 || n < 2 {
		return
	}
	pt := func(i int) (x, y int) {
		return int(le.Uint16(ih[129+4*i:])), int(le.Uint16(ih[131+4*i:]))
	}
	peak, px := 0, 0
	for i := 0; i < n; i++ {
		x, y := pt(i)
		if y > peak {
			peak, px = y, x
		}
	}
	sus := n - 1
	if typ&2 != 0 && int(ih[227]) < n {
		sus = int(ih[227])
	}
	sx, sy := pt(sus)
	if peak == 0 {
		peak = 64
	}
	in.Env.Attack = max(0, min(255, px))
	in.Env.Decay = max(0, min(255, sx-px))
	in.Env.Sustain = int8(max(0, min(15, int(math.Round(float64(sy)*15/float64(peak))))))
}
