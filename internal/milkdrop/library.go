package milkdrop

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Los presets: los de serie (presets/*.milk, escritos para audiofilus, MIT)
// y los del usuario, en cualquier subcarpeta de la carpeta que se le dé
// (<AUDIOFILUS_HOME>/milkdrop). La carpeta del usuario se recorre la primera
// vez que hace falta (no al arrancar), y los presets se leen al elegirlos.

//go:embed presets/*.milk
var builtin embed.FS

// Entry es un preset de la lista.
type Entry struct {
	// Name es el nombre que se enseña (el del archivo, sin .milk).
	Name string
	// Path es su ruta: en los de serie, dentro de presets/; en los del
	// usuario, la del disco.
	Path string
	// Builtin dice si es de serie.
	Builtin bool
}

// Library es la lista de presets.
type Library struct {
	userDir string
	scanned bool
	entries []Entry
}

// NewLibrary crea la lista con los presets de serie y, al pedirla, los de
// userDir (vacío: solo los de serie).
func NewLibrary(userDir string) *Library {
	return &Library{userDir: userDir}
}

// Builtin son los presets de serie.
func Builtin() []Entry {
	names, _ := fs.Glob(builtin, "presets/*.milk")
	sort.Strings(names)
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		out = append(out, Entry{Name: prettyName(strings.TrimSuffix(path.Base(n), ".milk")), Path: n, Builtin: true})
	}
	return out
}

// Entries son todos los presets: los de serie y luego los del usuario, por
// nombre.
func (l *Library) Entries() []Entry {
	if !l.scanned {
		l.scanned = true
		l.entries = Builtin()
		l.entries = append(l.entries, scanDir(l.userDir)...)
	}
	return l.entries
}

// Rescan vuelve a recorrer la carpeta del usuario la próxima vez.
func (l *Library) Rescan() { l.scanned = false }

// scanDir busca los .milk de dir y sus subcarpetas.
func scanDir(dir string) []Entry {
	if dir == "" {
		return nil
	}
	var out []Entry
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".milk") {
			out = append(out, Entry{Name: strings.TrimSuffix(d.Name(), filepath.Ext(p)), Path: p})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	return out
}

// Load lee y analiza un preset.
func Load(e Entry) (*Preset, error) {
	var data []byte
	var err error
	if e.Builtin {
		data, err = builtin.ReadFile(e.Path)
	} else {
		data, err = os.ReadFile(e.Path)
	}
	if err != nil {
		return nil, err
	}
	return Parse(e.Name, data)
}

// prettyName pasa "ember-tunnel" a "Ember Tunnel" (los de serie).
func prettyName(s string) string {
	words := strings.Split(s, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
