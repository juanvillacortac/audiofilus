// Package cast manda la música a otro aparato de la red: altavoces y teles
// DLNA/UPnP (MediaRenderer) y Chromecast. Busca los aparatos (SSDP para UPnP,
// mDNS para Chromecast), sirve los archivos por HTTP desde este equipo (con
// Range, para que el aparato pueda saltar; y convertidos a WAV los que el
// aparato no sabría tocar: módulos, música de consola, DSD...) y los
// controla: tocar, pausa, saltar, volumen y la siguiente canción.
//
// Uso desde la interfaz o el servicio:
//
//	devs, _ := cast.Discover(ctx, 3*time.Second) // UPnP y Chromecast juntos
//	srv, _ := cast.NewServer("")                 // el servidor HTTP de los archivos
//	s, _ := cast.NewSession(srv, devs[0])
//	_ = s.Cast(ctx, cast.Item{Path: song, Title: ..., Cover: coverPath}, 0)
//	st := s.Status()                              // posición, duración, estado
//	_ = s.Pause(ctx); _ = s.Resume(ctx); _ = s.Seek(ctx, 60); _ = s.SetVolume(ctx, 0.5)
//	_ = s.Queue(ctx, nextItem)                    // la siguiente, sin cortes si el aparato puede
//	for e := range s.Events() { ... }             // cambios de estado, fin de la canción, errores
//	s.Close(ctx)                                  // para y suelta el aparato (vuelve lo local)
package cast

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// Kind es el tipo de aparato.
type Kind string

const (
	UPnP       Kind = "upnp"
	Chromecast Kind = "chromecast"
)

// Device es un aparato al que se puede mandar música.
type Device struct {
	Kind  Kind
	ID    string // UDN en UPnP, el id del TXT en Chromecast
	Name  string // el nombre que le puso su dueño
	Model string
	// Host es host:puerto: en UPnP el de la descripción del aparato, en
	// Chromecast el del protocolo (8009).
	Host string

	// UPnP: dónde está su descripción y las URL de control.
	Location    string
	AVTransport string
	Rendering   string
	Connection  string
}

// Item es lo que se manda: un archivo local (o una ruta virtual de
// audiofilus, como una pista de un CUE) o una dirección http(s).
type Item struct {
	Path     string  // archivo local (o ruta virtual de decode)
	URL      string  // o una dirección http(s) (un servidor, una radio)
	MIME     string  // el tipo de URL, si se sabe
	Title    string  //
	Artist   string  //
	Album    string  //
	Cover    string  // la portada: archivo local o http(s)
	Duration float64 // en segundos, si se sabe
}

// State es en qué está el aparato.
type State string

const (
	Stopped State = "stopped"
	Loading State = "loading"
	Playing State = "playing"
	Paused  State = "paused"
)

// Status es lo que dice el aparato.
type Status struct {
	State    State
	Position float64 // segundos
	Duration float64 // segundos (0 si no se sabe)
	Volume   float64 // 0..1 (-1 si no se sabe)
}

// ErrUnsupported es lo que devuelve una orden que el aparato no tiene.
var ErrUnsupported = errors.New("cast: not supported by this device")

// Discover busca aparatos durante timeout (UPnP y Chromecast a la vez). Lo
// encontrado se guarda: Known devuelve lo último sin volver a buscar.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var (
		mu   sync.Mutex
		out  []Device
		errs []error
		wg   sync.WaitGroup
	)
	for _, find := range []func(context.Context) ([]Device, error){discoverUPnP, discoverChromecast} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			devs, err := find(ctx)
			mu.Lock()
			defer mu.Unlock()
			out = append(out, devs...)
			if err != nil {
				errs = append(errs, err)
			}
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	known.Lock()
	known.devs = out
	known.Unlock()
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// known es lo último que encontró Discover.
var known struct {
	sync.Mutex
	devs []Device
}

// Known son los aparatos de la última búsqueda.
func Known() []Device {
	known.Lock()
	defer known.Unlock()
	return append([]Device(nil), known.devs...)
}
