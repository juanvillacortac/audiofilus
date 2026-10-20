// Package smart son las listas inteligentes: una definición con reglas
// ("artista contiene X", "escuchada en los últimos 30 días", "me gusta"...)
// que se evalúa sobre la biblioteca, el historial de escuchas y las marcas
// (me gusta y valoraciones), y da las rutas de las canciones que cumplen, en
// un orden y con un límite. No depende de la interfaz: recibe Track, que la
// interfaz llena con lo que sabe de cada canción (lo que no sepa queda en
// cero y no cumple las reglas sobre ese campo). Las definiciones se guardan
// en data/smart.json.
package smart

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/history"
)

// Track es una canción de la biblioteca, con lo que se sepa de ella.
type Track struct {
	Path        string
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Format      string // la extensión, sin punto ("flac"); si está vacío, se saca de Path
	Year        int
	Track       int
	Duration    float64   // segundos
	Bitrate     int       // kbps
	Added       time.Time // cuándo entró en la biblioteca (o la fecha del archivo)
}

// Marks son el me gusta y la valoración de cada canción (*history.Marks lo
// es).
type Marks interface {
	Get(path string) history.Mark
}

// Rule es una regla: Field Op Value (y Value2, el final de "between").
type Rule struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  string `json:"value,omitempty"`
	Value2 string `json:"value2,omitempty"`
}

// Sort es el orden: un campo (o "random", distinto cada día) y si va de
// mayor a menor.
type Sort struct {
	Field string `json:"field,omitempty"`
	Desc  bool   `json:"desc,omitempty"`
}

// Def es una lista inteligente.
type Def struct {
	Name  string `json:"name"`
	Match string `json:"match,omitempty"` // "all" (por defecto) o "any"
	Rules []Rule `json:"rules,omitempty"`
	Sort  Sort   `json:"sort,omitempty"`
	Limit int    `json:"limit,omitempty"` // 0: sin límite
	// PlaysDays cuenta las escuchas (el campo plays) solo de los últimos N
	// días; 0, todas.
	PlaysDays int `json:"plays_days,omitempty"`
	// Live: la lista se vuelve a evaluar cada vez que se abre (si no, la
	// interfaz puede guardar lo que salió).
	Live bool `json:"live,omitempty"`
}

// Kind es el tipo de un campo, que dice qué operaciones admite.
type Kind int

const (
	Text Kind = iota
	Number
	Date
	Bool
)

// Field describe un campo para la interfaz: su nombre, su tipo y sus
// operaciones.
type Field struct {
	Name string
	Kind Kind
	Ops  []string
}

var (
	textOps   = []string{"is", "isnot", "contains", "notcontains", "startswith"}
	numberOps = []string{"is", "isnot", "gt", "lt", "between"}
	dateOps   = []string{"inlast", "notinlast", "gt", "lt", "between"}
	boolOps   = []string{"true", "false"}
)

// fieldKinds son los campos y su tipo.
var fieldKinds = map[string]Kind{
	"title": Text, "artist": Text, "album": Text, "albumartist": Text, "genre": Text,
	"path": Text, "folder": Text, "format": Text,
	"year": Number, "track": Number, "duration": Number, "bitrate": Number, "plays": Number, "rating": Number,
	"added": Date, "lastplayed": Date,
	"liked": Bool,
}

// fieldOrder es el orden en que se ofrecen los campos.
var fieldOrder = []string{"title", "artist", "album", "albumartist", "genre", "year", "duration", "format", "bitrate",
	"folder", "path", "track", "added", "plays", "lastplayed", "liked", "rating"}

// Fields son los campos que admiten las reglas, en orden.
func Fields() []Field {
	out := make([]Field, 0, len(fieldOrder))
	for _, n := range fieldOrder {
		k := fieldKinds[n]
		out = append(out, Field{Name: n, Kind: k, Ops: opsOf(k)})
	}
	return out
}

func opsOf(k Kind) []string {
	switch k {
	case Number:
		return numberOps
	case Date:
		return dateOps
	case Bool:
		return boolOps
	}
	return textOps
}

// Validate dice si una definición tiene sentido (campos, operaciones y
// valores que se entienden).
func Validate(d Def) error {
	if d.Match != "" && d.Match != "all" && d.Match != "any" {
		return fmt.Errorf("smart: match %q (want all or any)", d.Match)
	}
	for _, r := range d.Rules {
		k, ok := fieldKinds[r.Field]
		if !ok {
			return fmt.Errorf("smart: unknown field %q", r.Field)
		}
		if !contains(opsOf(k), r.Op) {
			return fmt.Errorf("smart: %s can't use %q", r.Field, r.Op)
		}
		if _, err := compile(r, time.Now()); err != nil {
			return err
		}
	}
	if s := d.Sort.Field; s != "" && s != "random" {
		if _, ok := fieldKinds[s]; !ok {
			return fmt.Errorf("smart: unknown sort field %q", s)
		}
	}
	if d.Limit < 0 || d.PlaysDays < 0 {
		return errors.New("smart: negative limit or plays_days")
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// stat es lo que dice el historial de una canción.
type stat struct {
	plays int // en la ventana de PlaysDays
	last  time.Time
}

// row es una canción con todo lo necesario para las reglas y el orden.
type row struct {
	t    *Track
	st   stat
	mark history.Mark
}

// Evaluate da las rutas de las canciones de lib que cumplen def, ordenadas
// y limitadas. ls es el historial (de history.Load); marks, las marcas (o
// nil). now es la hora de referencia de "en los últimos N días" (y la
// semilla del orden al azar, que cambia una vez al día).
func Evaluate(def Def, lib []Track, ls []history.Listen, marks Marks, now time.Time) []string {
	stats := playStats(ls, def.PlaysDays, now)
	preds := make([]pred, 0, len(def.Rules))
	for _, r := range def.Rules {
		p, err := compile(r, now)
		if err != nil {
			continue // una regla que no se entiende no filtra (Validate lo avisa)
		}
		preds = append(preds, p)
	}
	anyMatch := def.Match == "any"
	rows := make([]row, 0, len(lib)/4)
	for i := range lib {
		t := &lib[i]
		r := row{t: t, st: stats[t.Path]}
		if marks != nil {
			r.mark = marks.Get(t.Path)
		}
		if matches(&r, preds, anyMatch) {
			rows = append(rows, r)
		}
	}
	sortRows(rows, def, now)
	if def.Limit > 0 && len(rows) > def.Limit {
		rows = rows[:def.Limit]
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.t.Path
	}
	return out
}

// matches dice si una canción cumple las reglas (todas, o alguna con any;
// sin reglas, todas las canciones).
func matches(r *row, preds []pred, anyMatch bool) bool {
	if len(preds) == 0 {
		return true
	}
	for _, p := range preds {
		ok := p(r)
		if anyMatch && ok {
			return true
		}
		if !anyMatch && !ok {
			return false
		}
	}
	return !anyMatch
}

// playStats cuenta las escuchas de cada canción (las de los últimos days
// días, o todas) y la última.
func playStats(ls []history.Listen, days int, now time.Time) map[string]stat {
	out := make(map[string]stat, 256)
	var since int64
	if days > 0 {
		since = now.AddDate(0, 0, -days).Unix()
	}
	for _, l := range ls {
		s := out[l.Path]
		if l.At >= since {
			s.plays++
		}
		if at := time.Unix(l.At, 0); at.After(s.last) {
			s.last = at
		}
		out[l.Path] = s
	}
	return out
}

// pred es una regla compilada.
type pred func(r *row) bool

// compile prepara una regla: el valor se lee una vez, no por canción.
func compile(r Rule, now time.Time) (pred, error) {
	k, ok := fieldKinds[r.Field]
	if !ok {
		return nil, fmt.Errorf("smart: unknown field %q", r.Field)
	}
	switch k {
	case Text:
		get := textGetter(r.Field)
		v := fold(r.Value)
		var f func(string) bool
		switch r.Op {
		case "is":
			f = func(s string) bool { return s == v }
		case "isnot":
			f = func(s string) bool { return s != v }
		case "contains":
			f = func(s string) bool { return strings.Contains(s, v) }
		case "notcontains":
			f = func(s string) bool { return !strings.Contains(s, v) }
		case "startswith":
			f = func(s string) bool { return strings.HasPrefix(s, v) }
		default:
			return nil, fmt.Errorf("smart: %s can't use %q", r.Field, r.Op)
		}
		return func(rw *row) bool { return f(fold(get(rw))) }, nil
	case Number:
		get := numberGetter(r.Field)
		a, err := num(r.Value)
		if err != nil {
			return nil, fmt.Errorf("smart: %s %s: %w", r.Field, r.Op, err)
		}
		switch r.Op {
		case "is":
			return func(rw *row) bool { return get(rw) == a }, nil
		case "isnot":
			return func(rw *row) bool { return get(rw) != a }, nil
		case "gt":
			return func(rw *row) bool { return get(rw) > a }, nil
		case "lt":
			return func(rw *row) bool { return get(rw) < a }, nil
		case "between":
			b, err := num(r.Value2)
			if err != nil {
				return nil, fmt.Errorf("smart: %s between: %w", r.Field, err)
			}
			lo, hi := min(a, b), max(a, b)
			return func(rw *row) bool { v := get(rw); return v >= lo && v <= hi }, nil
		}
	case Date:
		get := dateGetter(r.Field)
		switch r.Op {
		case "inlast", "notinlast":
			d, err := num(r.Value)
			if err != nil {
				return nil, fmt.Errorf("smart: %s %s: %w", r.Field, r.Op, err)
			}
			since := now.Add(-time.Duration(d * 24 * float64(time.Hour)))
			in := func(rw *row) bool { t := get(rw); return !t.IsZero() && !t.Before(since) }
			if r.Op == "inlast" {
				return in, nil
			}
			// Sin fecha (nunca escuchada) cuenta como "no en los últimos días".
			return func(rw *row) bool { return !in(rw) }, nil
		case "gt", "lt", "between":
			a, err := day(r.Value)
			if err != nil {
				return nil, fmt.Errorf("smart: %s %s: %w", r.Field, r.Op, err)
			}
			switch r.Op {
			case "gt":
				return func(rw *row) bool { t := get(rw); return !t.IsZero() && t.After(a) }, nil
			case "lt":
				return func(rw *row) bool { t := get(rw); return !t.IsZero() && t.Before(a) }, nil
			}
			b, err := day(r.Value2)
			if err != nil {
				return nil, fmt.Errorf("smart: %s between: %w", r.Field, err)
			}
			if b.Before(a) {
				a, b = b, a
			}
			b = b.Add(24*time.Hour - time.Nanosecond) // el último día entero
			return func(rw *row) bool { t := get(rw); return !t.IsZero() && !t.Before(a) && !t.After(b) }, nil
		}
	case Bool:
		switch r.Op {
		case "true":
			return func(rw *row) bool { return rw.mark.Fav }, nil
		case "false":
			return func(rw *row) bool { return !rw.mark.Fav }, nil
		}
	}
	return nil, fmt.Errorf("smart: %s can't use %q", r.Field, r.Op)
}

// fold es el texto para comparar: minúsculas y sin espacios alrededor.
func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func num(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }

func day(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(s), time.Local)
}

func textGetter(f string) func(*row) string {
	switch f {
	case "title":
		return func(r *row) string { return r.t.Title }
	case "artist":
		return func(r *row) string { return r.t.Artist }
	case "album":
		return func(r *row) string { return r.t.Album }
	case "albumartist":
		return func(r *row) string {
			if r.t.AlbumArtist != "" {
				return r.t.AlbumArtist
			}
			return r.t.Artist
		}
	case "genre":
		return func(r *row) string { return r.t.Genre }
	case "path":
		return func(r *row) string { return r.t.Path }
	case "folder":
		return func(r *row) string { return folderOf(r.t.Path) }
	case "format":
		return func(r *row) string { return formatOf(r.t) }
	}
	return func(*row) string { return "" }
}

// folderOf es la carpeta de una ruta (local o remota, con / o \).
func folderOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return ""
}

// formatOf es el formato de una canción: el suyo o la extensión de la ruta.
func formatOf(t *Track) string {
	if t.Format != "" {
		return strings.TrimPrefix(t.Format, ".")
	}
	return strings.TrimPrefix(path.Ext(filepath.ToSlash(t.Path)), ".")
}

func numberGetter(f string) func(*row) float64 {
	switch f {
	case "year":
		return func(r *row) float64 { return float64(r.t.Year) }
	case "track":
		return func(r *row) float64 { return float64(r.t.Track) }
	case "duration":
		return func(r *row) float64 { return r.t.Duration }
	case "bitrate":
		return func(r *row) float64 { return float64(r.t.Bitrate) }
	case "plays":
		return func(r *row) float64 { return float64(r.st.plays) }
	case "rating":
		return func(r *row) float64 { return float64(r.mark.Rating) }
	}
	return func(*row) float64 { return 0 }
}

func dateGetter(f string) func(*row) time.Time {
	if f == "lastplayed" {
		return func(r *row) time.Time { return r.st.last }
	}
	return func(r *row) time.Time { return r.t.Added }
}

// sortRows ordena las canciones. Sin campo, el orden de la biblioteca; con
// "random", al azar pero igual todo el día (la semilla es el día y el
// nombre de la lista).
func sortRows(rows []row, def Def, now time.Time) {
	switch f := def.Sort.Field; {
	case f == "":
		if def.Sort.Desc {
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	case f == "random":
		h := fnv.New64a()
		h.Write([]byte(def.Name))
		y, m, d := now.Date()
		r := rand.New(rand.NewPCG(h.Sum64(), uint64(y*10000+int(m)*100+d)))
		r.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	default:
		less := lessBy(f)
		desc := def.Sort.Desc
		sort.SliceStable(rows, func(i, j int) bool {
			if desc {
				return less(&rows[j], &rows[i])
			}
			return less(&rows[i], &rows[j])
		})
	}
}

// lessBy compara por un campo.
func lessBy(f string) func(a, b *row) bool {
	switch fieldKinds[f] {
	case Number:
		get := numberGetter(f)
		return func(a, b *row) bool { return get(a) < get(b) }
	case Date:
		get := dateGetter(f)
		return func(a, b *row) bool { return get(a).Before(get(b)) }
	case Bool:
		return func(a, b *row) bool { return !a.mark.Fav && b.mark.Fav }
	}
	get := textGetter(f)
	return func(a, b *row) bool { return fold(get(a)) < fold(get(b)) }
}

// Presets son las listas que vienen hechas.
func Presets() []Def {
	return []Def{
		{Name: "Most played (30 days)", Rules: []Rule{{Field: "plays", Op: "gt", Value: "0"}}, PlaysDays: 30,
			Sort: Sort{Field: "plays", Desc: true}, Limit: 100, Live: true},
		{Name: "Recently added", Rules: []Rule{{Field: "added", Op: "inlast", Value: "30"}},
			Sort: Sort{Field: "added", Desc: true}, Limit: 200, Live: true},
		{Name: "Never played", Rules: []Rule{{Field: "plays", Op: "is", Value: "0"}}, Sort: Sort{Field: "random"}, Limit: 200, Live: true},
		{Name: "Liked", Rules: []Rule{{Field: "liked", Op: "true"}}, Sort: Sort{Field: "artist"}, Live: true},
		{Name: "Top rated", Rules: []Rule{{Field: "rating", Op: "gt", Value: "3"}},
			Sort: Sort{Field: "rating", Desc: true}, Limit: 200, Live: true},
		// Las que gustaban (me gusta o muy escuchadas) y no suenan desde
		// hace tres meses.
		{Name: "Forgotten favorites", Match: "all", Rules: []Rule{
			{Field: "plays", Op: "gt", Value: "4"},
			{Field: "lastplayed", Op: "notinlast", Value: "90"},
		}, Sort: Sort{Field: "plays", Desc: true}, Limit: 100, Live: true},
		{Name: "Random mix (today)", Sort: Sort{Field: "random"}, Limit: 100, Live: true},
	}
}

// file es data/smart.json.
func file(dataDir string) string { return filepath.Join(dataDir, "smart.json") }

// Load lee las listas guardadas. Sin archivo, los presets.
func Load(dataDir string) ([]Def, error) {
	b, err := os.ReadFile(file(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return Presets(), nil
	}
	if err != nil {
		return nil, err
	}
	var defs []Def
	if err := json.Unmarshal(b, &defs); err != nil {
		return nil, fmt.Errorf("smart.json: %w", err)
	}
	return defs, nil
}

// Save guarda las listas (aparte y luego encima: un corte no lo deja roto).
func Save(dataDir string, defs []Def) error {
	b, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	tmp := file(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file(dataDir))
}
