package ui

import (
	"testing"

	"golang.org/x/image/math/fixed"
)

// vietnamese son todas las letras del vietnamita, en minúscula y mayúscula.
const vietnamese = "ăâđêôơưĂÂĐÊÔƠƯàáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵÀÁẢÃẠẰẮẲẴẶẦẤẨẪẬÈÉẺẼẸỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌỒỐỔỖỘỜỚỞỠỢÙÚỦŨỤỪỨỬỮỰỲÝỶỸỴ"

// TestComposeVietnamese comprueba que Tiny5 y Jersey, con lo que se arma
// (compose.go), tienen todas las letras del vietnamita, y que las armadas
// tienen algo dibujado encima o debajo de su base.
func TestComposeVietnamese(t *testing.T) {
	big, err := loadFace("fonts/Jersey10-Regular.ttf", 1400.0/75, nil)
	if err != nil {
		t.Fatal(err)
	}
	small, err := loadFace("fonts/Tiny5-Regular.ttf", 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]*composeFace{"Jersey": newComposeFace(big, 2), "Tiny5": newComposeFace(small, 1)} {
		for _, r := range vietnamese {
			if _, ok := f.GlyphAdvance(r); !ok {
				t.Errorf("%s: no glyph for %q", name, r)
				continue
			}
			dr, mask, _, _, ok := f.Glyph(fixed.Point26_6{}, r)
			if !ok || mask == nil || dr.Empty() {
				t.Errorf("%s: empty glyph for %q", name, r)
			}
		}
		// Lo que no es una letra con marcas no se inventa.
		if _, ok := f.GlyphAdvance('ก'); ok {
			t.Errorf("%s: claims a Thai glyph it can't compose", name)
		}
	}
}

// TestThaiFont comprueba que el tailandés de Silver carga y tiene sus letras.
func TestThaiFont(t *testing.T) {
	f, err := parseFont(thaiFont)
	if err != nil {
		t.Fatal(err)
	}
	face, err := newFace(f, thaiFont, thaiPx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "กขคสวัสดีเพลงน้ำ" {
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("Silver has no %q", r)
		}
	}
}
