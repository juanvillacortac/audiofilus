// Package ico lee los iconos .ico de Windows (los favicon.ico de las webs,
// que muchas listas de radio dan como logo) y se registra en image, así
// image.Decode los entiende. De los que trae, elige el más grande. Cada uno
// puede ser un PNG (los modernos) o un mapa de bits sin cabecera de archivo
// (DIB) de 1, 4, 8, 24 o 32 bits, con su máscara de transparencia.
package ico

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
)

func init() {
	image.RegisterFormat("ico", "\x00\x00\x01\x00", Decode, DecodeConfig)
}

// errFormat es lo que devuelve un archivo que no es un icono que se entienda.
var errFormat = errors.New("ico: unsupported or corrupt icon")

// entry es un icono del directorio: su tamaño y dónde están sus datos.
type entry struct {
	w, h         int
	size, offset uint32
}

// largest lee el directorio y devuelve los datos del icono más grande.
func largest(r io.Reader) ([]byte, entry, error) {
	b, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return nil, entry{}, err
	}
	if len(b) < 6 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		return nil, entry{}, errFormat
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	var best entry
	for i := 0; i < n; i++ {
		p := 6 + 16*i
		if p+16 > len(b) {
			return nil, entry{}, errFormat
		}
		e := entry{w: int(b[p]), h: int(b[p+1]),
			size: binary.LittleEndian.Uint32(b[p+8:]), offset: binary.LittleEndian.Uint32(b[p+12:])}
		if e.w == 0 {
			e.w = 256 // 0 es 256
		}
		if e.h == 0 {
			e.h = 256
		}
		if e.w*e.h > best.w*best.h {
			best = e
		}
	}
	end := uint64(best.offset) + uint64(best.size)
	if best.w == 0 || end > uint64(len(b)) {
		return nil, entry{}, errFormat
	}
	return b[best.offset:end], best, nil
}

// DecodeConfig da el tamaño del icono más grande.
func DecodeConfig(r io.Reader) (image.Config, error) {
	_, e, err := largest(r)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: e.w, Height: e.h}, nil
}

// Decode lee el icono más grande.
func Decode(r io.Reader) (image.Image, error) {
	data, _, err := largest(r)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return png.Decode(bytes.NewReader(data))
	}
	return dib(data)
}

// dib lee un mapa de bits de icono: la cabecera BITMAPINFOHEADER (con el alto
// doble: el color y, debajo, la máscara), la paleta si tiene, las filas de
// color de abajo arriba y la máscara AND de un bit (1: transparente).
func dib(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, errFormat
	}
	hdr := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	comp := binary.LittleEndian.Uint32(b[16:])
	used := int(binary.LittleEndian.Uint32(b[32:]))
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 || comp != 0 && comp != 3 || hdr < 40 || hdr > len(b) {
		return nil, errFormat
	}
	var pal []color.NRGBA
	p := hdr
	if bpp <= 8 {
		if used == 0 {
			used = 1 << bpp
		}
		for i := 0; i < used; i++ {
			if p+4 > len(b) {
				return nil, errFormat
			}
			pal = append(pal, color.NRGBA{b[p+2], b[p+1], b[p], 0xff})
			p += 4
		}
	} else if comp == 3 && hdr == 40 {
		p += 12 // las máscaras de color de BI_BITFIELDS (se asume BGRA)
	}
	stride := (w*bpp + 31) / 32 * 4
	maskStride := (w + 31) / 32 * 4
	maskAt := p + stride*h
	if maskAt > len(b) {
		return nil, errFormat
	}
	hasMask := maskAt+maskStride*h <= len(b)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := 0; y < h; y++ {
		row := b[p+(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			var c color.NRGBA
			switch bpp {
			case 32:
				c = color.NRGBA{row[4*x+2], row[4*x+1], row[4*x], row[4*x+3]}
				anyAlpha = anyAlpha || c.A != 0
			case 24:
				c = color.NRGBA{row[3*x+2], row[3*x+1], row[3*x], 0xff}
			case 8, 4, 1:
				bit := x * bpp
				i := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if i < len(pal) {
					c = pal[i]
				}
			default:
				return nil, errFormat
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// La máscara manda si no hay alfa (en los de 32 bits con alfa, se ignora).
	if hasMask && (bpp != 32 || !anyAlpha) {
		for y := 0; y < h; y++ {
			row := b[maskAt+(h-1-y)*maskStride:]
			for x := 0; x < w; x++ {
				if row[x/8]>>(7-x%8)&1 == 1 {
					img.SetNRGBA(x, y, color.NRGBA{})
				}
			}
		}
	} else if bpp == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff // sin alfa ni máscara: opaco
		}
	}
	return img, nil
}
