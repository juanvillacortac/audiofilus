package cast

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeMPD es un servidor MPD de mentira: apunta los comandos y contesta lo
// justo (un id para addid, un estado fijo).
type fakeMPD struct {
	mu   sync.Mutex
	cmds []string
}

func (f *fakeMPD) serve(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				fmt.Fprint(c, "OK MPD 0.23.5\n")
				r := bufio.NewReader(c)
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					l = strings.TrimSpace(l)
					f.mu.Lock()
					f.cmds = append(f.cmds, l)
					f.mu.Unlock()
					switch {
					case strings.HasPrefix(l, "addid"):
						fmt.Fprint(c, "Id: 7\nOK\n")
					case l == "status":
						fmt.Fprint(c, "volume: 40\nstate: play\nsong: 0\nplaylistlength: 2\nelapsed: 12.500\nduration: 200.000\nOK\n")
					default:
						fmt.Fprint(c, "OK\n")
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// Mandar a un MPD: vacía su cola, añade la URL con sus etiquetas, la toca
// desde el segundo pedido; la siguiente va detrás (quitando la que había);
// el estado y el volumen se leen de status.
func TestMPDPlayer(t *testing.T) {
	f := &fakeMPD{}
	p := newMPDPlayer(Device{Kind: MPD, Host: f.serve(t)})
	defer p.Close()
	ctx := context.Background()
	if err := p.Load(ctx, media{URL: "http://pc/m/a.flac", Title: "Song", Artist: "Band"}, 30); err != nil {
		t.Fatal(err)
	}
	if err := p.SetNext(ctx, media{URL: "http://pc/m/b.flac"}); err != nil {
		t.Fatal(err)
	}
	_ = p.SetVolume(ctx, 0.5)
	st, err := p.Status(ctx)
	if err != nil || st.State != Playing || st.Position != 12.5 || st.Duration != 200 || st.Volume != 0.4 {
		t.Errorf("status = %+v, %v", st, err)
	}
	want := []string{
		"clear", `addid "http://pc/m/a.flac"`, `addtagid "7" "Title" "Song"`, `addtagid "7" "Artist" "Band"`,
		`playid "7"`, `seekid "7" "30.000"`, "status", `delete "1:"`, `addid "http://pc/m/b.flac"`, `setvol "50"`, "status",
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(f.cmds, "\n"), strings.Join(want, "\n"))
	}
}
