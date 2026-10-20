package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/control"
	"github.com/juanvillacortac/audiofilus/internal/daemon"
)

// Abrir archivos o carpetas desde el explorador ("Abrir con audiofilus", o
// arrastrarlos al lanzador): llegan como argumentos, rutas absolutas o URI
// file:// (según el explorador). Suenan ya en el servicio (comando "open")
// y se abre la interfaz en Reproduciendo; si ya estaba abierta, la cola se
// le cambia y este proceso termina.

// openArgs son las rutas de args si todos son archivos o carpetas que
// existen (con su ruta: un nombre suelto podría ser un comando).
func openArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		if u, ok := strings.CutPrefix(a, "file://"); ok {
			p, err := url.PathUnescape(u)
			if err != nil {
				return nil, false
			}
			a = filepath.FromSlash(p)
		}
		if strings.HasPrefix(a, "-") || !strings.ContainsAny(a, `/\`) {
			return nil, false
		}
		if _, err := os.Stat(a); err != nil {
			return nil, false
		}
		abs, err := filepath.Abs(a)
		if err != nil {
			return nil, false
		}
		out = append(out, abs)
	}
	return out, true
}

// runOpen hace sonar files y abre la interfaz si no lo estaba.
func runOpen(files []string) {
	paths, err := config.Locate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		os.Exit(1)
	}
	if _, err := control.Call(paths.Socket, control.Request{Cmd: "status"}); errors.Is(err, control.ErrNotRunning) {
		if err := startDetached(paths); err != nil {
			fmt.Fprintln(os.Stderr, "audiofilus:", err)
			os.Exit(1)
		}
	}
	resp, err := control.Call(paths.Socket, control.Request{Cmd: "open", Args: files})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus:", err)
		os.Exit(1)
	}
	var r daemon.OpenResult
	_ = json.Unmarshal(resp.Data, &r)
	if r.UIOpen {
		return // la interfaz abierta ya lo ve
	}
	runUI(nil)
}
