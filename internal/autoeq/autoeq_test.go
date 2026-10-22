package autoeq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const hd650 = `Preamp: -6.1 dB
Filter 1: ON LSC Fc 105 Hz Gain 6.4 dB Q 0.70
Filter 2: ON PK Fc 8800 Hz Gain 5.1 dB Q 1.42
Filter 3: ON HSC Fc 10000 Hz Gain -2.1 dB Q 0.70
Filter 4: OFF PK Fc 50 Hz Gain 1 dB Q 1
`

// Un ParametricEQ.txt da su preamp y sus filtros (los apagados, no), con
// los tipos de Equalizer APO traducidos.
func TestParseParametric(t *testing.T) {
	bands, pre, err := ParseParametric(hd650)
	if err != nil {
		t.Fatal(err)
	}
	if pre != -6.1 || len(bands) != 3 {
		t.Fatalf("preamp %v, %d bands; want -6.1 and 3", pre, len(bands))
	}
	if b := bands[0]; b.Type != "lowshelf" || b.Freq != 105 || b.Gain != 6.4 || b.Q != 0.7 {
		t.Errorf("first band %+v", b)
	}
	if bands[1].Type != "peak" || bands[2].Type != "highshelf" {
		t.Errorf("types %s %s", bands[1].Type, bands[2].Type)
	}
	if _, _, err := ParseParametric("nothing here"); err == nil {
		t.Error("a file without filters should fail")
	}
}

// El índice se lee, se busca por palabras (las que empiezan por la búsqueda
// primero) y el perfil elegido se baja de su carpeta.
func TestIndexSearchFetch(t *testing.T) {
	index := "# Index\n\n" +
		"- [Sennheiser HD 600](./oratory1990/over-ear/Sennheiser%20HD%20600) by oratory1990\n" +
		"- [Sennheiser HD 650](./oratory1990/over-ear/Sennheiser%20HD%20650) by oratory1990\n" +
		"- [Moondrop Aria (HD 650 tuned)](./crinacle/711%20in-ear/Moondrop%20Aria) by crinacle on 711\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/INDEX.md":
			w.Write([]byte(index))
		case "/oratory1990/over-ear/Sennheiser HD 650/Sennheiser HD 650 ParametricEQ.txt":
			w.Write([]byte(hd650))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := Base
	Base = srv.URL + "/"
	defer func() { Base = old }()

	idx, err := Index(context.Background(), filepath.Join(t.TempDir(), "index.md"))
	if err != nil || len(idx) != 3 {
		t.Fatalf("index: %d entries, %v", len(idx), err)
	}
	res := Search(idx, "hd 650", 10)
	if len(res) != 2 || !strings.HasPrefix(res[0].Name, "Sennheiser") {
		t.Fatalf("search: %+v", res)
	}
	bands, pre, err := Fetch(context.Background(), res[0])
	if err != nil || len(bands) != 3 || pre != -6.1 {
		t.Errorf("fetch: %d bands, preamp %v, %v", len(bands), pre, err)
	}
}
