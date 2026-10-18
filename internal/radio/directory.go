package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Station es una emisora del directorio.
type Station struct {
	UUID    string `json:"stationuuid"`
	Name    string `json:"name"`
	URL     string `json:"url_resolved"`
	Favicon string `json:"favicon"`
	Country string `json:"countrycode"`
	Tags    string `json:"tags"`
	Codec   string `json:"codec"`
	Bitrate int    `json:"bitrate"`
	Votes   int    `json:"votes"`
}

// Playable dice si audiofilus sabe decodificarla (MP3 y AAC: casi todas).
func (s Station) Playable() bool { return codecOf(s.Codec, "") != "" }

// Path es la ruta de la emisora en la cola: "radio://<uuid>".
func (s Station) Path() string { return Scheme + s.UUID }

// Scheme es el prefijo de las emisoras.
const Scheme = "radio://"

// IsRadio dice si path es una emisora.
func IsRadio(path string) bool { return strings.HasPrefix(path, Scheme) }

// servers son los espejos de radio-browser; se prueban en orden.
var servers = []string{"de1.api.radio-browser.info", "de2.api.radio-browser.info", "fi1.api.radio-browser.info", "all.api.radio-browser.info"}

var http1 = &http.Client{Timeout: 15 * time.Second}

// get pide path al directorio (con q) y deja la respuesta en out.
func get(ctx context.Context, path string, q url.Values, out any) error {
	var last error
	for _, host := range servers {
		u := "https://" + host + "/json/" + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err := http1.Do(req)
		if err != nil {
			last = err
			continue
		}
		err = func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("radio-browser: %s", resp.Status)
			}
			return json.NewDecoder(resp.Body).Decode(out)
		}()
		if err == nil {
			return nil
		}
		last = err
	}
	return last
}

// query son los filtros de siempre: solo las que funcionan, las más
// escuchadas primero.
func query(limit int) url.Values {
	return url.Values{"hidebroken": {"true"}, "order": {"clickcount"}, "reverse": {"true"}, "limit": {fmt.Sprint(limit)}}
}

// playable se queda con las emisoras que se pueden decodificar.
func playable(in []Station) []Station {
	out := in[:0]
	for _, s := range in {
		if s.Playable() && s.URL != "" {
			out = append(out, s)
		}
	}
	return out
}

// Search busca emisoras por nombre.
func Search(ctx context.Context, name string) ([]Station, error) {
	q := query(100)
	q.Set("name", name)
	var out []Station
	err := get(ctx, "stations/search", q, &out)
	return playable(out), err
}

// Top son las más escuchadas de un país (código de dos letras; "" todas).
func Top(ctx context.Context, country string) ([]Station, error) {
	q := query(100)
	if country != "" {
		q.Set("countrycode", country)
	}
	var out []Station
	err := get(ctx, "stations/search", q, &out)
	return playable(out), err
}

// ByTag son las más escuchadas de un género.
func ByTag(ctx context.Context, tag string) ([]Station, error) {
	q := query(100)
	q.Set("tag", tag)
	q.Set("tagExact", "true")
	var out []Station
	err := get(ctx, "stations/search", q, &out)
	return playable(out), err
}

// Tag es un género con cuántas emisoras tiene.
type Tag struct {
	Name     string `json:"name"`
	Stations int    `json:"stationcount"`
}

// Tags son los géneros con más emisoras.
func Tags(ctx context.Context, limit int) ([]Tag, error) {
	q := url.Values{"order": {"stationcount"}, "reverse": {"true"}, "limit": {fmt.Sprint(limit)}, "hidebroken": {"true"}}
	var out []Tag
	err := get(ctx, "tags", q, &out)
	return out, err
}

// Click avisa al directorio de que se escucha una emisora (lo pide
// radio-browser a quien lo usa: así ordena por popularidad).
func Click(ctx context.Context, uuid string) {
	var out any
	_ = get(ctx, "url/"+url.PathEscape(uuid), nil, &out)
}

// Stations son las emisoras que se conocen (las que la interfaz puso en la
// cola o en favoritas), en data/cache/radio/stations.json: el servicio las lee
// de ahí para saber a dónde conectar.
type Stations struct {
	file string
	mu   sync.Mutex
	m    map[string]Station
	at   time.Time
}

// NewStations abre el archivo de emisoras de dataDir.
func NewStations(dataDir string) *Stations {
	return &Stations{file: filepath.Join(dataDir, "cache", "radio", "stations.json"), m: map[string]Station{}}
}

// Get es la emisora de uuid (se relee el archivo si cambió).
func (st *Stations) Get(uuid string) (Station, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if info, err := os.Stat(st.file); err == nil && info.ModTime() != st.at {
		if b, err := os.ReadFile(st.file); err == nil {
			m := map[string]Station{}
			if json.Unmarshal(b, &m) == nil {
				st.m, st.at = m, info.ModTime()
			}
		}
	}
	s, ok := st.m[uuid]
	return s, ok
}

// Put guarda emisoras (las que se van a escuchar).
func (st *Stations) Put(list ...Station) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if b, err := os.ReadFile(st.file); err == nil {
		_ = json.Unmarshal(b, &st.m)
	}
	for _, s := range list {
		st.m[s.UUID] = s
	}
	b, err := json.Marshal(st.m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(st.file+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(st.file+".tmp", st.file)
}

// Manager da a el engine el flujo de cada emisora: el primero que la pide la
// abre; mientras conecta, Ready es false.
type Manager struct {
	stations *Stations
	mu       sync.Mutex
	open     map[string]*Stream
}

// NewManager prepara las emisoras del servicio.
func NewManager(st *Stations) *Manager {
	return &Manager{stations: st, open: map[string]*Stream{}}
}

// Source es el flujo de path ("radio://<uuid>"): ready si ya puede sonar.
// Las emisoras pedidas que no sean esta se cierran (solo suena una).
func (m *Manager) Source(path string) (*Stream, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.open[path]
	if !ok {
		st, found := m.stations.Get(strings.TrimPrefix(path, Scheme))
		if !found {
			return nil, false, fmt.Errorf("radio: unknown station %s", path)
		}
		for p, other := range m.open {
			other.Close()
			delete(m.open, p)
		}
		s = Open(st.URL, st.Codec)
		m.open[path] = s
	}
	ready, err := s.Ready()
	if err != nil {
		s.Close()
		delete(m.open, path)
		return nil, false, err
	}
	if ready {
		delete(m.open, path) // ya es del engine, que la cerrará
	}
	return s, ready, nil
}

// Playing es la emisora que suena, para el título (la pone el servicio).
func (m *Manager) Station(path string) (Station, bool) {
	return m.stations.Get(strings.TrimPrefix(path, Scheme))
}
