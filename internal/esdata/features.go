package esdata

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Feature es una opción de es_features.cfg: lo que ES enseña en las opciones
// de un sistema o de un juego, y la clave de knulli.conf en la que se guarda
// (sistema.Key, o sistema["juego"].Key).
type Feature struct {
	Name        string // el texto, en inglés (como en ES)
	Key         string // el "value" del archivo: la clave
	Description string
	Group       string // la sección del menú ("" la de por defecto)
	Submenu     string // si está, va dentro de un submenú con ese nombre
	Preset      string // switch, switchauto, slider, shaderset, bezel...
	PresetArgs  string // los de slider: desde, hasta, paso, por defecto, sufijo
	Order       int
	Choices     []Choice
}

// Choice es un valor posible de una opción.
type Choice struct {
	Name  string
	Value string
}

// Flags son las capacidades de un emulador o núcleo que no son opciones (las
// del atributo features que no son una opción compartida).
type Flags uint32

// Las capacidades que se leen.
const (
	FlagAutosave Flags = 1 << iota
	FlagNetplay
	FlagCheevos
	FlagPadToKeyboard
)

// featureSet son las opciones y capacidades de un nodo (emulador, núcleo o
// sistema dentro de ellos).
type featureSet struct {
	flags  Flags
	custom []Feature
}

// coreFeatures son las de un núcleo, y las suyas para cada sistema.
type coreFeatures struct {
	featureSet
	systems map[string]*featureSet
}

// emulatorFeatures son las de un emulador, las de sus núcleos y las suyas
// para cada sistema.
type emulatorFeatures struct {
	featureSet
	cores   map[string]*coreFeatures
	systems map[string]*featureSet
}

// Features es es_features.cfg leído.
type Features struct {
	Shared    []Feature
	Global    []Feature // las de "ajustes globales" (clave global.X)
	emulators map[string]*emulatorFeatures
}

// node es un elemento cualquiera del XML, con sus atributos y sus hijos.
type node struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []node     `xml:",any"`
}

// attr es el atributo name de n ("" si no lo tiene).
func (n *node) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// has dice si n tiene el atributo name.
func (n *node) has(name string) bool {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return true
		}
	}
	return false
}

// child es el primer hijo de n llamado name, o nil.
func (n *node) child(name string) *node {
	for i := range n.Children {
		if n.Children[i].XMLName.Local == name {
			return &n.Children[i]
		}
	}
	return nil
}

// LoadFeatures lee es_features.cfg (el del usuario si lo hay).
func (r Root) LoadFeatures() (*Features, error) {
	path := r.firstExisting(userESDir+"/es_features.cfg", systemDir+"/es_features.cfg")
	if path == "" {
		return nil, fmt.Errorf("es_features.cfg not found under %s", r.Path(systemDir))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFeatures(b)
}

// ParseFeatures lee el contenido de un es_features.cfg, como ES
// (CustomFeatures::loadEsFeaturesFile).
func ParseFeatures(b []byte) (*Features, error) {
	var root node
	if err := xml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("es_features.cfg: %w", err)
	}
	if root.XMLName.Local != "features" {
		return nil, fmt.Errorf("es_features.cfg: missing <features>")
	}
	f := &Features{emulators: map[string]*emulatorFeatures{}}
	if n := root.child("sharedFeatures"); n != nil {
		f.Shared = f.custom(n)
	}
	if n := root.child("globalFeatures"); n != nil {
		f.Global = f.custom(n)
		sortFeatures(f.Global)
	}
	for i := range root.Children {
		en := &root.Children[i]
		if en.XMLName.Local != "emulator" || en.attr("name") == "" {
			continue
		}
		for _, name := range splitNames(en.attr("name")) {
			f.addEmulator(name, en)
		}
	}
	return f, nil
}

// splitNames separa una lista de nombres con comas ("a, b").
func splitNames(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// set son las opciones y capacidades del nodo n.
func (f *Features) set(n *node) featureSet {
	s := featureSet{custom: f.custom(n)}
	if n.has("features") {
		s.flags = f.parseFlags(n.attr("features"))
	}
	for _, c := range s.custom {
		if c.Key == "autosave" {
			s.flags |= FlagAutosave // el autosave como opción compartida
		}
	}
	return s
}

// addEmulator suma lo del nodo <emulator> en al emulador name.
func (f *Features) addEmulator(name string, en *node) {
	em := f.emulators[name]
	if em == nil {
		em = &emulatorFeatures{cores: map[string]*coreFeatures{}, systems: map[string]*featureSet{}}
		f.emulators[name] = em
	}
	es := f.set(en)
	em.flags |= es.flags
	em.custom = append(em.custom, es.custom...)

	cores := en.child("cores")
	if cores == nil {
		cores = en
	}
	for i := range cores.Children {
		cn := &cores.Children[i]
		if cn.XMLName.Local != "core" || cn.attr("name") == "" {
			continue
		}
		for _, cname := range splitNames(cn.attr("name")) {
			c := em.cores[cname]
			if c == nil {
				c = &coreFeatures{systems: map[string]*featureSet{}}
				em.cores[cname] = c
			}
			cs := f.set(cn)
			c.flags |= cs.flags
			c.custom = append(c.custom, cs.custom...)
			f.addSystems(c.systems, cn)
		}
	}
	f.addSystems(em.systems, en)
}

// addSystems suma a into las opciones de los <system> de n (dentro de su
// <systems>, o sueltos).
func (f *Features) addSystems(into map[string]*featureSet, n *node) {
	sys := n.child("systems")
	if sys == nil {
		sys = n
	}
	for i := range sys.Children {
		sn := &sys.Children[i]
		if sn.XMLName.Local != "system" || sn.attr("name") == "" {
			continue
		}
		ss := f.set(sn)
		if ss.flags == 0 && len(ss.custom) == 0 {
			continue
		}
		name := sn.attr("name")
		cur := into[name]
		if cur == nil {
			cur = &featureSet{}
			into[name] = cur
		}
		cur.flags |= ss.flags
		cur.custom = append(cur.custom, ss.custom...)
	}
}

// parseFlags lee las capacidades del atributo features.
func (f *Features) parseFlags(s string) Flags {
	var out Flags
	for _, name := range splitNames(s) {
		switch name {
		case "autosave":
			out |= FlagAutosave
		case "netplay":
			out |= FlagNetplay
		case "cheevos":
			out |= FlagCheevos
		case "padtokeyboard", "joystick2pad":
			out |= FlagPadToKeyboard
		}
	}
	return out
}

// shared es la opción compartida con ese nombre (byName) o esa clave.
func (f *Features) shared(name, key string) (Feature, bool) {
	if name != "" {
		for _, s := range f.Shared {
			if s.Name == name {
				return s, true
			}
		}
	}
	if key != "" {
		for _, s := range f.Shared {
			if s.Key == key {
				return s, true
			}
		}
	}
	return Feature{}, false
}

// custom son las opciones del nodo n: las compartidas que nombra su atributo
// features, y sus <feature> y <sharedFeature> (dentro de <features>, o
// sueltos), como CustomFeatures::loadCustomFeatures.
func (f *Features) custom(n *node) []Feature {
	var out []Feature
	if n.has("features") {
		for _, key := range splitNames(n.attr("features")) {
			if s, ok := f.shared("", key); ok {
				out = append(out, s)
			}
		}
	}
	list := n.child("features")
	if list == nil {
		list = n
	}
	for i := range list.Children {
		fn := &list.Children[i]
		switch fn.XMLName.Local {
		case "sharedFeature":
			s, ok := f.shared(fn.attr("name"), fn.attr("value"))
			if !ok {
				continue
			}
			if fn.has("submenu") {
				s.Submenu = fn.attr("submenu")
			}
			if fn.has("preset") {
				s.Preset = fn.attr("preset")
			}
			if fn.has("preset-parameters") {
				s.PresetArgs = fn.attr("preset-parameters")
			}
			if fn.has("group") {
				s.Group = fn.attr("group")
			}
			if fn.has("order") {
				s.Order, _ = strconv.Atoi(fn.attr("order"))
			}
			out = append(out, s)
		case "feature":
			if fn.attr("name") == "" {
				continue
			}
			ft := Feature{
				Name: fn.attr("name"), Description: fn.attr("description"), Submenu: fn.attr("submenu"),
				Preset: fn.attr("preset"), PresetArgs: fn.attr("preset-parameters"), Group: fn.attr("group"),
			}
			ft.Order, _ = strconv.Atoi(fn.attr("order"))
			ft.Key = fn.attr("value")
			if !fn.has("value") {
				ft.Key = strings.ReplaceAll(ft.Name, " ", "_")
			}
			for j := range fn.Children {
				cn := &fn.Children[j]
				if cn.XMLName.Local != "choice" || cn.attr("name") == "" {
					continue
				}
				c := Choice{Name: cn.attr("name"), Value: cn.attr("value")}
				if !cn.has("value") {
					c.Value = strings.ReplaceAll(c.Name, " ", "_")
				}
				ft.Choices = append(ft.Choices, c)
			}
			if len(ft.Choices) > 0 || ft.Preset != "" {
				out = append(out, ft)
			}
		}
	}
	return out
}

// sortFeatures ordena por Order sin cambiar el orden de las que empatan.
func sortFeatures(fs []Feature) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Order < fs[j].Order })
}

// emulatorName es el nombre con el que se busca un emulador: los "lr-*" son
// núcleos de libretro.
func emulatorName(name string) string {
	if strings.HasPrefix(name, "lr-") {
		return "libretro"
	}
	return name
}

// For son las opciones del sistema system con ese emulador y núcleo: las del
// emulador (y las suyas para el sistema) y luego las del núcleo (y las suyas
// para el sistema), ordenadas; como SystemData::getCustomFeatures.
func (f *Features) For(system, emulator, core string) []Feature {
	var out []Feature
	em := f.emulators[emulatorName(emulator)]
	if em == nil {
		return nil
	}
	out = append(out, em.custom...)
	if s := em.systems[system]; s != nil {
		out = append(out, s.custom...)
	}
	if c := em.cores[core]; c != nil {
		out = append(out, c.custom...)
		if s := c.systems[system]; s != nil {
			out = append(out, s.custom...)
		}
	}
	sortFeatures(out)
	return out
}

// Supports dice si el núcleo (o, si no, el emulador) tiene la capacidad flag
// en el sistema, como SystemData::isFeatureSupported.
func (f *Features) Supports(system, emulator, core string, flag Flags) bool {
	em := f.emulators[emulatorName(emulator)]
	if em == nil {
		return false
	}
	if c := em.cores[core]; c != nil {
		fl := c.flags
		if s := c.systems[system]; s != nil {
			fl |= s.flags
		}
		if fl&flag != 0 {
			return true
		}
	}
	fl := em.flags
	if s := em.systems[system]; s != nil {
		fl |= s.flags
	}
	return fl&flag != 0
}
