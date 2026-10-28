package ico

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// icon arma un .ico con un solo icono de w×h y los datos dados.
func icon(w, h int, data []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, []uint16{0, 1, 1})
	b.Write([]byte{byte(w), byte(h), 0, 0})
	binary.Write(&b, binary.LittleEndian, []uint16{1, 32})
	binary.Write(&b, binary.LittleEndian, []uint32{uint32(len(data)), 22})
	b.Write(data)
	return b.Bytes()
}

// dibHeader es la cabecera BITMAPINFOHEADER de un icono (alto doble).
func dibHeader(w, h, bpp, colors int) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, []int32{40, int32(w), int32(2 * h)})
	binary.Write(&b, binary.LittleEndian, []uint16{1, uint16(bpp)})
	binary.Write(&b, binary.LittleEndian, []uint32{0, 0, 0, 0, uint32(colors), 0})
	return b.Bytes()
}

// Un icono de 32 bits con alfa: los colores en BGRA, de abajo arriba.
func TestDecode32(t *testing.T) {
	d := dibHeader(2, 2, 32, 0)
	d = append(d, 0, 0, 255, 255, 0, 255, 0, 128) // fila de abajo: rojo, verde a medias
	d = append(d, 255, 0, 0, 255, 0, 0, 0, 0)     // fila de arriba: azul, transparente
	d = append(d, make([]byte, 8)...)             // la máscara (se ignora: hay alfa)
	img, format, err := image.Decode(bytes.NewReader(icon(2, 2, d)))
	if err != nil || format != "ico" {
		t.Fatalf("decode: %v, %q", err, format)
	}
	want := map[[2]int]color.NRGBA{
		{0, 1}: {255, 0, 0, 255}, {1, 1}: {0, 255, 0, 128},
		{0, 0}: {0, 0, 255, 255}, {1, 0}: {},
	}
	for p, c := range want {
		if got := color.NRGBAModel.Convert(img.At(p[0], p[1])).(color.NRGBA); got != c && !(c.A == 0 && got.A == 0) {
			t.Errorf("pixel %v = %v, want %v", p, got, c)
		}
	}
}

// Un icono de 1 bit con paleta: la máscara AND hace transparente su píxel.
func TestDecodePaletteMask(t *testing.T) {
	d := dibHeader(2, 1, 1, 2)
	d = append(d, 0, 0, 0, 0, 255, 255, 255, 0) // negro y blanco
	d = append(d, 0b01000000, 0, 0, 0)          // la fila: negro, blanco
	d = append(d, 0b10000000, 0, 0, 0)          // máscara: el primero, transparente
	img, err := Decode(bytes.NewReader(icon(2, 1, d)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("masked pixel is not transparent")
	}
	if r, _, _, a := img.At(1, 0).RGBA(); r != 0xffff || a != 0xffff {
		t.Error("second pixel is not opaque white")
	}
}

// Un icono que lleva un PNG dentro se lee como PNG, y se elige el más grande
// (DecodeConfig dice su tamaño).
func TestDecodePNGInside(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	src.SetNRGBA(1, 1, color.NRGBA{9, 8, 7, 255})
	var p bytes.Buffer
	png.Encode(&p, src)
	ico := icon(3, 3, p.Bytes())
	cfg, err := DecodeConfig(bytes.NewReader(ico))
	if err != nil || cfg.Width != 3 || cfg.Height != 3 {
		t.Fatalf("config %+v, %v", cfg, err)
	}
	img, err := Decode(bytes.NewReader(ico))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(1, 1)).(color.NRGBA); got != (color.NRGBA{9, 8, 7, 255}) {
		t.Errorf("center %v", got)
	}
	if _, err := Decode(bytes.NewReader(ico[:30])); err == nil {
		t.Error("a truncated icon decoded")
	}
}
