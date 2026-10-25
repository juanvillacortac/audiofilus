package mdns

import (
	"context"
	"testing"
	"time"
)

// fakeBonjour es el Bonjour de la app, con una respuesta fija.
type fakeBonjour struct {
	browsed, published string
	unpublished        int
}

func (f *fakeBonjour) Browse(service string, ms int) string {
	f.browsed = service
	return "Sala\t192.168.1.7\t6600\tapp=audiofilus\tPassword=1\nroto\t\t0\n"
}

func (f *fakeBonjour) Publish(id int, name, service string, port int, txt string) {
	f.published = name + "|" + service + "|" + txt
}

func (f *fakeBonjour) Unpublish(id int) { f.unpublished = id }

// TestNativeBonjour comprueba que con SetBonjour la búsqueda y el anuncio
// van por la app: lo que devuelve se lee como instancias (las rotas, fuera)
// y el anuncio se retira al cerrarlo.
func TestNativeBonjour(t *testing.T) {
	f := &fakeBonjour{}
	SetBonjour(f)
	defer SetBonjour(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	found, err := Browse(ctx, "_mpd._tcp.local")
	if err != nil || len(found) != 1 {
		t.Fatalf("browse: %v %+v", err, found)
	}
	in := found[0]
	if f.browsed != "_mpd._tcp" || in.Name != "Sala" || in.Host() != "192.168.1.7:6600" ||
		in.TXT["app"] != "audiofilus" || in.TXT["password"] != "1" {
		t.Errorf("browse: %q %+v", f.browsed, in)
	}
	r, err := Announce("Teléfono", "_mpd._tcp.local", 6600, map[string]string{"app": "audiofilus"})
	if err != nil {
		t.Fatal(err)
	}
	if f.published != "Teléfono|_mpd._tcp|app=audiofilus" {
		t.Errorf("publish: %q", f.published)
	}
	r.Close()
	if f.unpublished == 0 {
		t.Error("not unpublished")
	}
}
