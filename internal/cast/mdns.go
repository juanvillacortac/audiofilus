package cast

import (
	"context"

	"github.com/juanvillacortac/audiofilus/internal/mdns"
)

// Búsqueda de Chromecasts por mDNS (internal/mdns): las instancias de
// _googlecast._tcp, con su nombre (TXT fn=), modelo (md=) e id.

// castService es el servicio de los Chromecasts.
const castService = "_googlecast._tcp.local"

// discoverChromecast busca Chromecasts hasta que se acabe ctx.
func discoverChromecast(ctx context.Context) ([]Device, error) {
	found, err := mdns.Browse(ctx, castService)
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, in := range found {
		d := Device{Kind: Chromecast, ID: in.TXT["id"], Name: in.TXT["fn"], Model: in.TXT["md"], Host: in.Host()}
		if d.Name == "" {
			d.Name = in.Name
		}
		out = append(out, d)
	}
	return dedupe(out), nil
}
