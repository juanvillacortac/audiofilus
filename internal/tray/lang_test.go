package tray

import "testing"

// TestTrayLang comprueba que cada idioma sale en el suyo y que uno sin
// textos cae en inglés, no en español (#17).
func TestTrayLang(t *testing.T) {
	for _, c := range []struct{ in, want string }{{"pl", "pl"}, {"es", "es"}, {"zh-TW", "zh-TW"}, {"xx", "en"}} {
		if got := trayLang(c.in); got != c.want {
			t.Errorf("trayLang(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "pl_PL.UTF-8")
	if got := trayLang(""); got != "pl" {
		t.Errorf(`trayLang("") with LANG=pl_PL = %q`, got)
	}
	for l, v := range trayStrs {
		for i, s := range v {
			if s == "" {
				t.Errorf("%s: text %d is empty", l, i)
			}
		}
	}
}
