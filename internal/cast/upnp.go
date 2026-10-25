package cast

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// El control de un aparato UPnP: órdenes SOAP a su AVTransport (cargar,
// tocar, pausa, saltar, la siguiente) y a su RenderingControl (volumen).

const (
	avtType = "urn:schemas-upnp-org:service:AVTransport:1"
	rcType  = "urn:schemas-upnp-org:service:RenderingControl:1"
)

// media es lo que se carga en un aparato: la dirección ya servida y sus datos.
type media struct {
	URL      string
	MIME     string
	Title    string
	Artist   string
	Album    string
	Cover    string // URL
	Duration float64
}

// player es un aparato que toca (UPnP o Chromecast).
type player interface {
	Load(ctx context.Context, m media, start float64) error
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, sec float64) error
	SetVolume(ctx context.Context, v float64) error
	Status(ctx context.Context) (Status, error)
	// SetNext deja preparada la siguiente (sin cortes); ErrUnsupported si el
	// aparato no sabe.
	SetNext(ctx context.Context, m media) error
	Close() error
}

// upnpPlayer controla un MediaRenderer.
type upnpPlayer struct {
	dev     Device
	noNext  bool // ya dijo que no tiene SetNextAVTransportURI
	lastVol float64
	// loaded: ya se le mandó algo; loading: a medio Load. Si después
	// queda sin nada puesto (NO_MEDIA_PRESENT), dejó de recibir.
	loaded, loading atomic.Bool
}

func newUPnPPlayer(d Device) *upnpPlayer { return &upnpPlayer{dev: d, lastVol: -1} }

// soap manda una orden y devuelve los valores de la respuesta.
func soap(ctx context.Context, control, service, action string, args [][2]string) (map[string]string, error) {
	if control == "" {
		return nil, ErrUnsupported
	}
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, action, service)
	for _, a := range args {
		fmt.Fprintf(&body, "<%s>%s</%s>", a[0], html.EscapeString(a[1]), a[0])
	}
	fmt.Fprintf(&body, `</u:%s></s:Body></s:Envelope>`, action)
	req, err := http.NewRequestWithContext(ctx, "POST", control, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+service+"#"+action+`"`)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	vals := soapValues(b)
	if resp.StatusCode != http.StatusOK {
		if code := vals["errorCode"]; code == "401" || code == "602" {
			return nil, ErrUnsupported // acción no válida / no implementada
		}
		return nil, fmt.Errorf("upnp %s: %s %s %s", action, resp.Status, vals["errorCode"], vals["errorDescription"])
	}
	return vals, nil
}

// soapValues junta los elementos hoja de una respuesta SOAP (nombre → texto).
func soapValues(b []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(b))
	var name string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name = t.Name.Local
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if t.Name.Local == name {
				out[name] = strings.TrimSpace(text.String())
			}
			name = ""
		}
	}
}

// didl es la descripción DIDL-Lite de una canción (lo que muestra el aparato
// y el tipo de archivo, en protocolInfo).
func didl(m media) string {
	e := html.EscapeString
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
	b.WriteString(`<item id="0" parentID="-1" restricted="1">`)
	fmt.Fprintf(&b, "<dc:title>%s</dc:title>", e(orStr(m.Title, "Audiofilus")))
	if m.Artist != "" {
		fmt.Fprintf(&b, "<dc:creator>%s</dc:creator><upnp:artist>%s</upnp:artist>", e(m.Artist), e(m.Artist))
	}
	if m.Album != "" {
		fmt.Fprintf(&b, "<upnp:album>%s</upnp:album>", e(m.Album))
	}
	if m.Cover != "" {
		fmt.Fprintf(&b, "<upnp:albumArtURI>%s</upnp:albumArtURI>", e(m.Cover))
	}
	b.WriteString("<upnp:class>object.item.audioItem.musicTrack</upnp:class>")
	dur := ""
	if m.Duration > 0 {
		dur = fmt.Sprintf(` duration="%s.000"`, hms(m.Duration))
	}
	fmt.Fprintf(&b, `<res protocolInfo="http-get:*:%s:*"%s>%s</res>`, e(orStr(m.MIME, "audio/mpeg")), dur, e(m.URL))
	b.WriteString("</item></DIDL-Lite>")
	return b.String()
}

// hms es una duración como H:MM:SS (como la quiere UPnP).
func hms(sec float64) string {
	s := int(math.Max(0, sec))
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

// parseHMS lee H:MM:SS(.fff); 0 si no se entiende ("NOT_IMPLEMENTED").
func parseHMS(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0
		}
		total = total*60 + v
	}
	return total
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

var instance = [2]string{"InstanceID", "0"}

func (p *upnpPlayer) avt(ctx context.Context, action string, args ...[2]string) (map[string]string, error) {
	return soap(ctx, p.dev.AVTransport, avtType, action, append([][2]string{instance}, args...))
}

func (p *upnpPlayer) Load(ctx context.Context, m media, start float64) error {
	p.loading.Store(true)
	defer p.loading.Store(false)
	_, _ = p.avt(ctx, "Stop") // algunos no aceptan otra canción sonando
	if _, err := p.avt(ctx, "SetAVTransportURI", [2]string{"CurrentURI", m.URL}, [2]string{"CurrentURIMetaData", didl(m)}); err != nil {
		return err
	}
	if err := p.Play(ctx); err != nil {
		return err
	}
	p.loaded.Store(true)
	if start > 1 {
		_ = p.Seek(ctx, start)
	}
	return nil
}

func (p *upnpPlayer) Play(ctx context.Context) error {
	_, err := p.avt(ctx, "Play", [2]string{"Speed", "1"})
	return err
}

func (p *upnpPlayer) Pause(ctx context.Context) error {
	_, err := p.avt(ctx, "Pause")
	return err
}

func (p *upnpPlayer) Stop(ctx context.Context) error {
	_, err := p.avt(ctx, "Stop")
	return err
}

func (p *upnpPlayer) Seek(ctx context.Context, sec float64) error {
	_, err := p.avt(ctx, "Seek", [2]string{"Unit", "REL_TIME"}, [2]string{"Target", hms(sec)})
	return err
}

func (p *upnpPlayer) SetVolume(ctx context.Context, v float64) error {
	v = math.Max(0, math.Min(1, v))
	_, err := soap(ctx, p.dev.Rendering, rcType, "SetVolume", [][2]string{instance, {"Channel", "Master"}, {"DesiredVolume", strconv.Itoa(int(math.Round(v * 100)))}})
	if err == nil {
		p.lastVol = v
	}
	return err
}

func (p *upnpPlayer) SetNext(ctx context.Context, m media) error {
	if p.noNext {
		return ErrUnsupported
	}
	_, err := p.avt(ctx, "SetNextAVTransportURI", [2]string{"NextURI", m.URL}, [2]string{"NextURIMetaData", didl(m)})
	if err == ErrUnsupported {
		p.noNext = true
	}
	return err
}

func (p *upnpPlayer) Status(ctx context.Context) (Status, error) {
	st := Status{Volume: p.lastVol}
	info, err := p.avt(ctx, "GetTransportInfo")
	if err != nil {
		return st, err
	}
	switch info["CurrentTransportState"] {
	case "NO_MEDIA_PRESENT":
		if p.loaded.Load() && !p.loading.Load() {
			return st, ErrReleased
		}
		st.State = Stopped
	case "PLAYING":
		st.State = Playing
	case "PAUSED_PLAYBACK", "PAUSED_RECORDING":
		st.State = Paused
	case "TRANSITIONING":
		st.State = Loading
	default:
		st.State = Stopped
	}
	if pos, err := p.avt(ctx, "GetPositionInfo"); err == nil {
		st.Position = parseHMS(pos["RelTime"])
		st.Duration = parseHMS(pos["TrackDuration"])
	}
	if p.dev.Rendering != "" {
		if v, err := soap(ctx, p.dev.Rendering, rcType, "GetVolume", [][2]string{instance, {"Channel", "Master"}}); err == nil {
			if n, err := strconv.Atoi(v["CurrentVolume"]); err == nil {
				st.Volume = float64(n) / 100
				p.lastVol = st.Volume
			}
		}
	}
	return st, nil
}

func (p *upnpPlayer) Close() error { return nil }
