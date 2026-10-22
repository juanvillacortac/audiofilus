// Package autoeq trae los perfiles de audífonos de AutoEq
// (github.com/jaakkopasanen/AutoEq, libre): para miles de modelos, el
// ecualizador paramétrico que los lleva a una respuesta neutra. Se busca en
// su índice (results/INDEX.md, que se guarda una semana) y se baja el
// "ParametricEQ.txt" del elegido; también se leen esos archivos sueltos.
package autoeq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Base es de dónde salen el índice y los perfiles.
var Base = "https://raw.githubusercontent.com/jaakkopasanen/AutoEq/master/results/"

// indexMaxAge es cuánto se usa el índice guardado antes de volver a bajarlo.
const indexMaxAge = 7 * 24 * time.Hour

var client = &http.Client{Timeout: 30 * time.Second}

// Profile es un perfil del índice: el modelo, quién lo midió y dónde está.
type Profile struct {
	Name   string
	Source string // "oratory1990", "crinacle on GRAS 43AG-7"...
	Path   string // la carpeta, relativa a results/ (con %20)
}

// indexLine es una línea del índice:
// - [Sennheiser HD 650](./oratory1990/over-ear/Sennheiser%20HD%20650) by oratory1990
var indexLine = regexp.MustCompile(`^- \[(.+)\]\(\./(.+)\) by (.+)$`)

// ParseIndex lee el índice de AutoEq.
func ParseIndex(r io.Reader) []Profile {
	var out []Profile
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if m := indexLine.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			out = append(out, Profile{Name: m[1], Path: m[2], Source: m[3]})
		}
	}
	return out
}

// Search devuelve los perfiles cuyo nombre lleva todas las palabras de
// query (sin distinguir mayúsculas), los que empiezan por ella primero;
// como mucho max.
func Search(index []Profile, query string, max int) []Profile {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var out []Profile
	for _, p := range index {
		name := strings.ToLower(p.Name)
		ok := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, p)
		}
	}
	q := strings.ToLower(query)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.HasPrefix(strings.ToLower(out[i].Name), q) && !strings.HasPrefix(strings.ToLower(out[j].Name), q)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// Index devuelve el índice: el guardado en cache si es reciente, si no se
// baja (y se guarda). Sin red, el guardado aunque sea viejo.
func Index(ctx context.Context, cache string) ([]Profile, error) {
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < indexMaxAge {
		if f, err := os.Open(cache); err == nil {
			defer f.Close()
			if idx := ParseIndex(f); len(idx) > 0 {
				return idx, nil
			}
		}
	}
	b, err := get(ctx, Base+"INDEX.md")
	if err != nil {
		if f, ferr := os.Open(cache); ferr == nil {
			defer f.Close()
			if idx := ParseIndex(f); len(idx) > 0 {
				return idx, nil
			}
		}
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
		_ = os.WriteFile(cache, b, 0o644)
	}
	idx := ParseIndex(strings.NewReader(string(b)))
	if len(idx) == 0 {
		return nil, errors.New("autoeq: empty index")
	}
	return idx, nil
}

// Fetch baja el ecualizador paramétrico del perfil p.
func Fetch(ctx context.Context, p Profile) ([]engine.PEQBand, float64, error) {
	name, err := url.PathUnescape(p.Path[strings.LastIndex(p.Path, "/")+1:])
	if err != nil {
		return nil, 0, err
	}
	b, err := get(ctx, Base+p.Path+"/"+url.PathEscape(name+" ParametricEQ.txt"))
	if err != nil {
		return nil, 0, err
	}
	return ParseParametric(string(b))
}

// get baja una dirección.
func get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "audiofilus")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("autoeq: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// filterTypes traduce los tipos de filtro de AutoEq (los de Equalizer APO).
var filterTypes = map[string]string{
	"PK": "peak", "PEQ": "peak", "LSC": "lowshelf", "LS": "lowshelf", "LSQ": "lowshelf",
	"HSC": "highshelf", "HS": "highshelf", "HSQ": "highshelf", "LP": "lowpass", "LPQ": "lowpass",
	"HP": "highpass", "HPQ": "highpass",
}

// filterLine es "Filter 1: ON PK Fc 105 Hz Gain 2.6 dB Q 0.70".
var filterLine = regexp.MustCompile(`(?i)^Filter\s*\d*\s*:\s*ON\s+(\w+)\s+Fc\s+([\d.]+)\s*Hz(?:\s+Gain\s+(-?[\d.]+)\s*dB)?(?:\s+Q\s+([\d.]+))?`)

// ParseParametric lee un ParametricEQ.txt de AutoEq (el formato de Equalizer
// APO): el preamp y hasta engine.PEQMax filtros.
func ParseParametric(text string) ([]engine.PEQBand, float64, error) {
	var bands []engine.PEQBand
	preamp := 0.0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Preamp:"); ok {
			v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "dB"))
			preamp, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
			continue
		}
		m := filterLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		typ, ok := filterTypes[strings.ToUpper(m[1])]
		if !ok {
			continue
		}
		f, _ := strconv.ParseFloat(m[2], 64)
		g, _ := strconv.ParseFloat(m[3], 64)
		q := 0.707
		if m[4] != "" {
			q, _ = strconv.ParseFloat(m[4], 64)
		}
		bands = append(bands, engine.PEQBand{Type: typ, Freq: f, Gain: g, Q: q})
		if len(bands) == engine.PEQMax {
			break
		}
	}
	if len(bands) == 0 {
		return nil, 0, errors.New("autoeq: no filters in the profile")
	}
	return bands, preamp, nil
}
