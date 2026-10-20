package cast

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Búsqueda de aparatos UPnP: un M-SEARCH por multicast (SSDP) pidiendo
// MediaRenderer, y la descripción XML de cada uno que conteste (su nombre y
// las URL de control de AVTransport y RenderingControl).

const (
	ssdpAddr     = "239.255.255.250:1900"
	rendererType = "urn:schemas-upnp-org:device:MediaRenderer:1"
)

// httpClient es el cliente de las peticiones a los aparatos.
var httpClient = &http.Client{Timeout: 8 * time.Second}

// discoverUPnP busca MediaRenderers hasta que se acabe ctx.
func discoverUPnP(ctx context.Context) ([]Device, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}
	msg := "M-SEARCH * HTTP/1.1\r\nHOST: " + ssdpAddr + "\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + rendererType + "\r\n\r\n"
	// Dos veces, por si se pierde un paquete (UDP).
	for range 2 {
		if _, err := conn.WriteTo([]byte(msg), dst); err != nil {
			return nil, err
		}
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	locations := map[string]bool{}
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break // se acabó el tiempo
		}
		if loc := ssdpLocation(buf[:n]); loc != "" {
			locations[loc] = true
		}
	}
	var (
		mu  sync.Mutex
		out []Device
		wg  sync.WaitGroup
	)
	dctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for loc := range locations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchDescription(dctx, loc)
			if err != nil || d.AVTransport == "" {
				return
			}
			mu.Lock()
			out = append(out, d)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return dedupe(out), nil
}

// dedupe quita los aparatos repetidos (un aparato puede contestar varias
// veces, o por varias interfaces).
func dedupe(devs []Device) []Device {
	seen := map[string]bool{}
	var out []Device
	for _, d := range devs {
		key := string(d.Kind) + "|" + d.ID
		if d.ID == "" {
			key = string(d.Kind) + "|" + d.Host
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, d)
		}
	}
	return out
}

// ssdpLocation saca el LOCATION de una respuesta SSDP (si es de un
// MediaRenderer).
func ssdpLocation(b []byte) string {
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(b)), nil)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	if st := resp.Header.Get("ST"); st != "" && !strings.Contains(st, "MediaRenderer") {
		return ""
	}
	return resp.Header.Get("Location")
}

// La descripción de un aparato UPnP (lo que se usa).
type xmlDescription struct {
	URLBase string    `xml:"URLBase"`
	Device  xmlDevice `xml:"device"`
}

type xmlDevice struct {
	DeviceType   string       `xml:"deviceType"`
	FriendlyName string       `xml:"friendlyName"`
	ModelName    string       `xml:"modelName"`
	UDN          string       `xml:"UDN"`
	Services     []xmlService `xml:"serviceList>service"`
	Devices      []xmlDevice  `xml:"deviceList>device"`
}

type xmlService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// fetchDescription baja y lee la descripción de un aparato.
func fetchDescription(ctx context.Context, location string) (Device, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", location, nil)
	if err != nil {
		return Device{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Device{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Device{}, fmt.Errorf("%s: %s", location, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Device{}, err
	}
	return parseDescription(b, location)
}

// parseDescription lee una descripción UPnP: el MediaRenderer (que puede
// venir dentro de otro aparato) y sus URL de control, absolutas.
func parseDescription(b []byte, location string) (Device, error) {
	var desc xmlDescription
	if err := xml.Unmarshal(b, &desc); err != nil {
		return Device{}, err
	}
	base, err := url.Parse(location)
	if err != nil {
		return Device{}, err
	}
	if desc.URLBase != "" {
		if u, err := url.Parse(strings.TrimSpace(desc.URLBase)); err == nil {
			base = u
		}
	}
	dev := findRenderer(desc.Device)
	if dev == nil {
		return Device{}, fmt.Errorf("%s: no MediaRenderer", location)
	}
	d := Device{Kind: UPnP, ID: strings.TrimSpace(dev.UDN), Name: strings.TrimSpace(dev.FriendlyName),
		Model: strings.TrimSpace(dev.ModelName), Host: base.Host, Location: location}
	resolve := func(p string) string {
		u, err := base.Parse(strings.TrimSpace(p))
		if err != nil {
			return ""
		}
		return u.String()
	}
	for _, s := range dev.Services {
		switch {
		case strings.Contains(s.ServiceType, ":AVTransport:"):
			d.AVTransport = resolve(s.ControlURL)
		case strings.Contains(s.ServiceType, ":RenderingControl:"):
			d.Rendering = resolve(s.ControlURL)
		case strings.Contains(s.ServiceType, ":ConnectionManager:"):
			d.Connection = resolve(s.ControlURL)
		}
	}
	if d.Name == "" {
		d.Name = d.Host
	}
	return d, nil
}

// findRenderer busca el MediaRenderer en el árbol de aparatos (o el primero
// que tenga AVTransport).
func findRenderer(d xmlDevice) *xmlDevice {
	if strings.Contains(d.DeviceType, "MediaRenderer") {
		return &d
	}
	for _, sub := range d.Devices {
		if r := findRenderer(sub); r != nil {
			return r
		}
	}
	for _, s := range d.Services {
		if strings.Contains(s.ServiceType, ":AVTransport:") {
			return &d
		}
	}
	return nil
}
