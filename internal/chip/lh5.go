package chip

import (
	"encoding/binary"
	"errors"
)

// Los YM y los VTX vienen comprimidos con el método -lh5- de LHA (LZSS con
// una ventana de 8 KB y bloques con sus propios códigos de Huffman). Los YM
// son un archivo LHA entero (cabecera y datos); los VTX, solo los datos.

var errLH5 = errors.New("chip: corrupt lh5 data")

// lhaExtract saca el primer archivo de un LHA (-lh5- o -lh0-, sin
// comprimir), con cabeceras de nivel 0, 1 o 2.
func lhaExtract(b []byte) ([]byte, error) {
	if len(b) < 22 || b[2] != '-' || b[3] != 'l' || b[4] != 'h' || b[6] != '-' {
		return nil, errors.New("chip: not an lha archive")
	}
	method := b[5]
	packed := int(binary.LittleEndian.Uint32(b[7:]))
	size := int(binary.LittleEndian.Uint32(b[11:]))
	var at int
	switch b[20] {
	case 0, 1:
		at = 2 + int(b[0])
		if b[20] == 1 {
			// Las cabeceras extendidas van detrás y cuentan como datos.
			next := int(binary.LittleEndian.Uint16(b[at-2:]))
			for next != 0 {
				if at+next > len(b) {
					return nil, errLH5
				}
				at += next
				packed -= next
				next = int(binary.LittleEndian.Uint16(b[at-2:]))
			}
		}
	case 2:
		at = int(binary.LittleEndian.Uint16(b))
	default:
		return nil, errors.New("chip: unsupported lha header level")
	}
	if at > len(b) || packed < 0 || size < 0 || size > 64<<20 {
		return nil, errLH5
	}
	data := b[at:min(len(b), at+packed)]
	switch method {
	case '0':
		if len(data) < size {
			return nil, errLH5
		}
		return data[:size], nil
	case '5':
		return lh5Decode(data, size)
	}
	return nil, errors.New("chip: unsupported lha method")
}

// bitReader lee bits de mayor a menor peso; pasado el final da ceros.
type bitReader struct {
	b   []byte
	pos int // bit
}

func (r *bitReader) bits(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v <<= 1
		if p := r.pos >> 3; p < len(r.b) {
			v |= int(r.b[p]>>(7-r.pos&7)) & 1
		}
		r.pos++
	}
	return v
}

// huffman es un código canónico: cuántos códigos hay de cada largo y los
// símbolos en orden. Si solo hay un símbolo (single >= 0), no lee bits.
type huffman struct {
	count  [17]int
	sorted []int
	single int
}

func newHuffman(lens []int) huffman {
	h := huffman{single: -1}
	for _, l := range lens {
		if l > 16 {
			l = 16
		}
		h.count[l]++
	}
	h.count[0] = 0
	for l := 1; l <= 16; l++ {
		for s, sl := range lens {
			if sl == l {
				h.sorted = append(h.sorted, s)
			}
		}
	}
	return h
}

// decode lee un símbolo.
func (h *huffman) decode(r *bitReader) (int, error) {
	if h.single >= 0 {
		return h.single, nil
	}
	code, first, index := 0, 0, 0
	for l := 1; l <= 16; l++ {
		code |= r.bits(1)
		if code-first < h.count[l] {
			return h.sorted[index+code-first], nil
		}
		index += h.count[l]
		first = (first + h.count[l]) << 1
		code <<= 1
	}
	return 0, errLH5
}

// Los tamaños de -lh5-.
const (
	lhNC   = 510 // literales (256) y largos de copia (de 3 a 256)
	lhNT   = 19  // los largos de los largos
	lhNP   = 14  // los grupos de distancias (ventana de 8 KB)
	lhCBit = 9
	lhTBit = 5
	lhPBit = 4
)

// readPT lee los largos de la tabla de largos (nn símbolos de nbit bits) o
// de la de distancias; special es el símbolo tras el que vienen 2 bits de
// ceros (en la primera, 3; en la de distancias, ninguno: -1).
func readPT(r *bitReader, nn, nbit, special int) huffman {
	n := r.bits(nbit)
	if n == 0 {
		h := huffman{single: r.bits(nbit)}
		return h
	}
	lens := make([]int, nn)
	for i := 0; i < n && i < nn; {
		c := r.bits(3)
		if c == 7 {
			for r.bits(1) == 1 {
				c++
			}
		}
		lens[i] = c
		i++
		if i == special {
			for z := r.bits(2); z > 0 && i < nn; z-- {
				lens[i] = 0
				i++
			}
		}
	}
	return newHuffman(lens)
}

// readC lee la tabla de literales y largos, cuyos largos vienen codificados
// con pt.
func readC(r *bitReader, pt *huffman) (huffman, error) {
	n := r.bits(lhCBit)
	if n == 0 {
		return huffman{single: r.bits(lhCBit)}, nil
	}
	lens := make([]int, lhNC)
	for i := 0; i < n && i < lhNC; {
		c, err := pt.decode(r)
		if err != nil {
			return huffman{}, err
		}
		if c <= 2 {
			switch c {
			case 0:
				c = 1
			case 1:
				c = r.bits(4) + 3
			default:
				c = r.bits(lhCBit) + 20
			}
			for ; c > 0 && i < lhNC; c-- {
				lens[i] = 0
				i++
			}
			continue
		}
		lens[i] = c - 2
		i++
	}
	return newHuffman(lens), nil
}

// lh5Decode descomprime size bytes de un flujo -lh5-.
func lh5Decode(b []byte, size int) ([]byte, error) {
	r := &bitReader{b: b}
	out := make([]byte, 0, size)
	var ch, ph huffman
	block := 0
	for len(out) < size {
		if r.pos > len(b)*8+64 {
			return nil, errLH5
		}
		if block == 0 {
			block = r.bits(16)
			pt := readPT(r, lhNT, lhTBit, 3)
			var err error
			if ch, err = readC(r, &pt); err != nil {
				return nil, err
			}
			ph = readPT(r, lhNP, lhPBit, -1)
		}
		block--
		c, err := ch.decode(r)
		if err != nil {
			return nil, err
		}
		if c < 256 {
			out = append(out, byte(c))
			continue
		}
		n := c - 256 + 3
		p, err := ph.decode(r)
		if err != nil {
			return nil, err
		}
		if p > 1 {
			p = 1<<(p-1) + r.bits(p-1)
		}
		from := len(out) - p - 1
		if from < 0 {
			return nil, errLH5
		}
		for i := 0; i < n && len(out) < size; i++ {
			out = append(out, out[from+i])
		}
	}
	return out, nil
}
