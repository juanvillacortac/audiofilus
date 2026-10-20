package lyrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store guarda, en data/lyrics, qué letra se eligió para cada canción, su
// desfase, y las bajadas de internet (para no volver a pedirlas).
type Store struct {
	dir string

	mu      sync.Mutex
	loaded  bool
	choices map[string]Choice
}

// Choice es lo guardado de una canción.
type Choice struct {
	// Source es el ID de la opción elegida (ver Option.ID). Vacío: la que
	// salga sola.
	Source string `json:"source,omitempty"`
	// Auto: Source la eligió audiofilus (de internet), no la persona; si
	// aparece un .lrc o una etiqueta sincronizada, gana esa.
	Auto bool `json:"auto,omitempty"`
	// OffsetMS adelanta (positivo) o atrasa la letra.
	OffsetMS int `json:"offset_ms,omitempty"`
	// Missed es cuándo se buscó en internet sin encontrar nada, para no
	// preguntar cada vez (se vuelve a intentar pasado missRetry).
	Missed time.Time `json:"missed,omitzero"`
}

// missRetry es cada cuánto se vuelve a buscar en internet una letra que no
// estaba.
const missRetry = 7 * 24 * time.Hour

// NewStore usa la carpeta dir (data/lyrics).
func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) file() string { return filepath.Join(s.dir, "choices.json") }

func (s *Store) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.choices = map[string]Choice{}
	if b, err := os.ReadFile(s.file()); err == nil {
		_ = json.Unmarshal(b, &s.choices)
	}
}

// Choice es lo guardado para path.
func (s *Store) Choice(path string) Choice {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.choices[path]
}

// Update cambia lo guardado para path y lo escribe.
func (s *Store) Update(path string, f func(*Choice)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	c := s.choices[path]
	f(&c)
	if c == (Choice{}) {
		delete(s.choices, path)
	} else {
		s.choices[path] = c
	}
	b, err := json.MarshalIndent(s.choices, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}

func (s *Store) foundFile(id int) string {
	return filepath.Join(s.dir, "lrclib-"+strconv.Itoa(id)+".json")
}

// saveFound guarda una letra bajada de LRCLIB.
func (s *Store) saveFound(f Found) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(s.foundFile(f.ID), b, 0o644)
}

// loadFound lee una letra de LRCLIB guardada.
func (s *Store) loadFound(id int) (Found, bool) {
	var f Found
	b, err := os.ReadFile(s.foundFile(id))
	if err != nil || json.Unmarshal(b, &f) != nil {
		return Found{}, false
	}
	return f, true
}

// Remember guarda la opción elegida a mano para path (las de internet se
// guardan también enteras, para no volver a bajarlas).
func (s *Store) Remember(path string, o Option, found []Found) error {
	if o.Kind() == "lrclib" {
		id, _ := strconv.Atoi(strings.TrimPrefix(o.ID, "lrclib:"))
		for _, f := range found {
			if f.ID == id {
				if err := s.saveFound(f); err != nil {
					return err
				}
			}
		}
	}
	return s.Update(path, func(c *Choice) {
		c.Source, c.Auto, c.Missed = o.ID, false, time.Time{}
	})
}

// Finder busca la letra de una canción.
type Finder struct {
	Store *Store
	// Web es el cliente de LRCLIB; nil no busca en internet.
	Web *LRCLIB
}

// Find devuelve la letra de la canción en path (con lo que se sabe de ella
// en q): la elegida a mano si la hay; si no, la mejor sincronizada a mano
// (archivo o etiqueta), la de internet, o la mejor sin sincronizar. Sin
// letra, la opción es "none".
func (fd *Finder) Find(ctx context.Context, path string, q Query) Option {
	ch := fd.Store.Choice(path)
	local := Local(path)
	if ch.Source == "none" {
		return Option{ID: "none"}
	}
	if ch.Source != "" && !ch.Auto {
		if o, ok := fd.load(ctx, ch.Source, local); ok {
			return o
		}
	}
	for _, o := range local {
		if o.Synced() && !o.Guess {
			return o
		}
	}
	// Lo de internet que se eligió solo antes, ya guardado.
	if ch.Auto && ch.Source != "" {
		if o, ok := fd.load(ctx, ch.Source, nil); ok {
			return o
		}
	}
	if fd.Web != nil && (ch.Missed.IsZero() || time.Since(ch.Missed) > missRetry) {
		found, err := fd.Web.Search(ctx, q)
		if f, ok := Best(found, q); ok {
			o, _ := OptionOf(f)
			// Una sin sincronizar de internet no gana a la que ya hay a mano.
			if o.Synced() || o.Lyrics.Instrumental || len(sure(local)) == 0 {
				if fd.Store.saveFound(f) == nil {
					_ = fd.Store.Update(path, func(c *Choice) { c.Source, c.Auto, c.Missed = o.ID, true, time.Time{} })
				}
				return o
			}
		} else if err == nil && ctx.Err() == nil {
			_ = fd.Store.Update(path, func(c *Choice) { c.Missed = time.Now() })
		}
	}
	if s := sure(local); len(s) > 0 {
		return s[0]
	}
	return Option{ID: "none"}
}

// sure quita las opciones adivinadas (los .lrc con otro nombre).
func sure(opts []Option) []Option {
	var out []Option
	for _, o := range opts {
		if !o.Guess {
			out = append(out, o)
		}
	}
	return out
}

// load abre la opción guardada id.
func (fd *Finder) load(ctx context.Context, id string, local []Option) (Option, bool) {
	if strings.HasPrefix(id, "lrclib:") {
		n, err := strconv.Atoi(strings.TrimPrefix(id, "lrclib:"))
		if err != nil {
			return Option{}, false
		}
		if f, ok := fd.Store.loadFound(n); ok {
			return OptionOf(f)
		}
		if fd.Web == nil {
			return Option{}, false
		}
		var f Found
		if err := fd.Web.get(ctx, "/api/get/"+strconv.Itoa(n), &f); err != nil {
			return Option{}, false
		}
		_ = fd.Store.saveFound(f)
		return OptionOf(f)
	}
	for _, o := range local {
		if o.ID == id {
			return o, true
		}
	}
	// Un archivo elegido a mano que no está junto a la canción.
	if p, ok := strings.CutPrefix(id, "file:"); ok {
		if b, err := os.ReadFile(p); err == nil {
			if l := Parse(string(b)); !l.Empty() {
				return Option{ID: id, Label: filepath.Base(p), Lyrics: l}, true
			}
		}
	}
	return Option{}, false
}
