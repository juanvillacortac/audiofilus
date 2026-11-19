package ui

import "testing"

// TestBidiRuns comprueba el orden en que se ven los tramos: en un párrafo de
// izquierda a derecha, el árabe va en su sitio; en uno de derecha a
// izquierda, los tramos van al revés y el latín sigue de izquierda a
// derecha.
func TestBidiRuns(t *testing.T) {
	old := lang
	defer func() { lang = old }()
	lang = "en"
	got := bidiRuns("Song: أغنية جديدة (Remix)")
	if len(got) != 3 || got[0].rtl || !got[1].rtl || got[2].rtl || string(got[1].s) != "أغنية جديدة" {
		t.Errorf("LTR paragraph: %+v", runsString(got))
	}
	// Empieza en árabe: el párrafo es de derecha a izquierda, y lo latino
	// (MilkDrop) queda a la izquierda.
	got = bidiRuns("إعدادات MilkDrop")
	if len(got) != 2 || got[0].rtl || string(got[0].s) != "MilkDrop" || !got[1].rtl {
		t.Errorf("RTL paragraph: %+v", runsString(got))
	}
	if !needsShaping("संगीत") || !needsShaping("أغنية") || needsShaping("Música ไทย 日本") {
		t.Error("needsShaping wrong")
	}
}

func runsString(rs []bidiRun) []string {
	var out []string
	for _, r := range rs {
		d := "L:"
		if r.rtl {
			d = "R:"
		}
		out = append(out, d+string(r.s))
	}
	return out
}

// TestShapeLine comprueba que el hindi y el árabe se forman con sus fuentes
// (glifos dibujados y un ancho), y que un conjunto de devanagari ocupa menos
// glifos que letras (स्ट es un glifo, no tres).
func TestShapeLine(t *testing.T) {
	if err := LoadFonts(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"प्लेलिस्ट", "قائمة التشغيل", "Mix: संगीत 2"} {
		l := shapeLine(s, SmallFace)
		glyphs := 0
		for _, p := range l.pieces {
			if p.glyph != nil {
				glyphs++
			}
		}
		if l.w <= 0 || glyphs == 0 {
			t.Errorf("%q: width %d, %d glyphs", s, l.w, glyphs)
		}
	}
	l := shapeLine("स्ट", SmallFace)
	if n := len(l.pieces); n == 0 || n >= 3 {
		t.Errorf("स्ट shaped into %d glyphs, want a conjunct", n)
	}
}
