package esdata

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Scope es de qué son unas opciones: las globales (System nil), las de un
// sistema (Game nil) o las de un juego.
type Scope struct {
	System *System
	Game   *Game
}

// ConfigName es el prefijo de las claves en knulli.conf: "global", el nombre
// del sistema o sistema["archivo"] (sin = ni #, como FileData::getConfigurationName).
func (s Scope) ConfigName() string {
	switch {
	case s.System == nil:
		return "global"
	case s.Game == nil:
		return s.System.Name
	}
	f := strings.NewReplacer("=", "", "#", "").Replace(s.Game.File())
	return s.System.Name + `["` + f + `"]`
}

// OptionKind es el tipo de una fila de opciones.
type OptionKind int

// Los tipos de fila.
const (
	OptChoice   OptionKind = iota // un valor de una lista (izquierda y derecha)
	OptEmulator                   // el emulador y núcleo: cambia las demás filas
	OptSubmenu                    // abre otra lista de opciones
	OptText                       // un texto (se ve; se edita aparte)
	OptFavorite                   // si el juego es favorito (va en su gamelist.xml, no en knulli.conf)
)

// Los nombres de los valores especiales, que la interfaz traduce.
const (
	ValAuto = "AUTO"
	ValOn   = "ON"
	ValOff  = "OFF"
	ValNone = "NONE"
)

// Option es una fila de un menú de opciones.
type Option struct {
	Kind        OptionKind
	Label       string
	Description string
	Group       string
	Key         string   // la clave de knulli.conf (en OptEmulator, el prefijo)
	Values      []Choice // los valores posibles, en orden
	Sub         []Option // las filas del submenú (OptSubmenu)
	// Inherited es el nombre del valor que se usa con AUTO: el del sistema o
	// el global ("" si no hay).
	Inherited string
}

// Current es el índice del valor que tiene ahora la opción en c (el de AUTO,
// el primero, si no tiene ninguno de la lista).
func (o *Option) Current(c *Conf) int {
	v := c.Get(o.Key)
	if o.Kind == OptEmulator {
		v = c.Get(o.Key+".emulator") + "/" + c.Get(o.Key+".core")
		if v == "/" {
			v = ""
		}
	}
	for i, ch := range o.Values {
		if ch.Value == v || (v == "" || v == "auto") && (ch.Value == "" || ch.Value == "auto") {
			return i
		}
	}
	if v != "" {
		// Un número de un deslizador guardado con decimales ("50.000000").
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			for i, ch := range o.Values {
				if g, err := strconv.ParseFloat(ch.Value, 64); err == nil && g == f {
					return i
				}
			}
		}
	}
	return 0
}

// Set pone en c el valor i de la opción.
func (o *Option) Set(c *Conf, i int) {
	if i < 0 || i >= len(o.Values) {
		return
	}
	v := o.Values[i].Value
	if o.Kind == OptEmulator {
		emu, core, _ := strings.Cut(v, "/")
		c.Set(o.Key+".emulator", emu)
		c.Set(o.Key+".core", core)
		return
	}
	c.Set(o.Key, v)
}

// CurrentEmulator es el emulador y núcleo con que se lanzaría en el ámbito s:
// el del juego, si no el del sistema, si no el de por defecto.
func CurrentEmulator(c *Conf, s Scope) (emulator, core string) {
	if s.System == nil {
		return "", ""
	}
	if s.Game != nil {
		n := s.ConfigName()
		if e := c.Get(n + ".emulator"); e != "" {
			return e, c.Get(n + ".core")
		}
	}
	if e := c.Get(s.System.Name + ".emulator"); e != "" {
		return e, c.Get(s.System.Name + ".core")
	}
	return s.System.DefaultEmulator()
}

// Options son las filas de opciones del ámbito s: el emulador (si hay donde
// elegir) y las de es_features para él, agrupadas (la sección sin nombre
// primero) y con los submenús juntos en una fila.
func (r Root) Options(f *Features, c *Conf, s Scope) []Option {
	name := s.ConfigName()
	var out []Option
	var feats []Feature
	if s.System == nil {
		feats = f.Global
	} else {
		if s.System.HasEmulatorChoice() {
			out = append(out, emulatorOption(s.System, name))
		}
		emu, core := CurrentEmulator(c, s)
		feats = f.For(s.System.Name, emu, core)
	}
	groups := map[string][]Feature{}
	var order []string
	for _, ft := range feats {
		if _, ok := groups[ft.Group]; !ok {
			order = append(order, ft.Group)
		}
		groups[ft.Group] = append(groups[ft.Group], ft)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i] == "" || order[j] == "" {
			return order[i] == ""
		}
		return order[i] < order[j]
	})
	for _, g := range order {
		subs := map[string]bool{}
		for _, ft := range groups[g] {
			if ft.Preset == "hidden" {
				continue
			}
			if ft.Submenu == "" {
				out = append(out, r.option(ft, name, c, g))
				continue
			}
			if subs[ft.Submenu] {
				continue
			}
			subs[ft.Submenu] = true
			sub := Option{Kind: OptSubmenu, Label: ft.Submenu, Group: g}
			for _, it := range feats {
				if it.Submenu == ft.Submenu && it.Preset != "hidden" {
					sub.Sub = append(sub.Sub, r.option(it, name, c, ""))
				}
			}
			out = append(out, sub)
		}
	}
	return out
}

// emulatorOption es la fila del emulador de un sistema: AUTO y cada
// emulador/núcleo.
func emulatorOption(s *System, key string) Option {
	o := Option{Kind: OptEmulator, Label: "EMULATOR", Key: key, Values: []Choice{{ValAuto, ""}}}
	de, dc := s.DefaultEmulator()
	o.Inherited = label(de, dc)
	for _, e := range s.Emulators {
		if len(e.Cores) == 0 {
			o.Values = append(o.Values, Choice{e.Name, e.Name + "/"})
			continue
		}
		for _, c := range e.Cores {
			o.Values = append(o.Values, Choice{label(e.Name, c.Name), e.Name + "/" + c.Name})
		}
	}
	return o
}

// label es el nombre de un emulador y núcleo: "libretro: mgba", o solo uno si
// son iguales.
func label(emu, core string) string {
	if core == "" || core == emu {
		return emu
	}
	return emu + ": " + strings.ReplaceAll(core, "_", " ")
}

// option es la fila de la opción ft, guardada en name.key.
func (r Root) option(ft Feature, name string, c *Conf, group string) Option {
	key := name + "." + ft.Key
	if name == "global" && strings.HasPrefix(ft.Key, "global.") {
		key = ft.Key
	}
	o := Option{Kind: OptChoice, Label: ft.Name, Description: ft.Description, Group: group, Key: key}
	switch ft.Preset {
	case "input", "password", "image", "video", "folder", "document", "files":
		o.Kind = OptText
		return o
	case "switch", "switch_default_off":
		o.Values = []Choice{{ValOff, ""}, {ValOn, "1"}}
	case "switchauto":
		o.Values = []Choice{{ValAuto, ""}, {ValOn, "1"}, {ValOff, "0"}}
	case "switchon", "switch_default_on":
		o.Values = []Choice{{ValOn, ""}, {ValOff, "0"}}
	case "switchoff", "switch_default_off_reverse_value":
		o.Values = []Choice{{ValOn, ""}, {ValOff, "1"}}
	case "slider", "sliderauto":
		o.Values = sliderValues(ft.PresetArgs, ft.Preset == "sliderauto")
	case "shaders", "shaderset":
		o.Values = []Choice{{ValAuto, "auto"}}
		if sh := r.Shaders(); len(sh) > 0 {
			o.Values = append(o.Values, Choice{ValNone, "none"})
			for _, s := range sh {
				o.Values = append(o.Values, Choice{strings.ToUpper(s), s})
			}
		}
	case "decorations", "bezel":
		o.Values = []Choice{{ValAuto, "auto"}}
		if d := r.Decorations(); len(d) > 0 {
			o.Values = append(o.Values, Choice{ValNone, "none"})
			for _, s := range d {
				o.Values = append(o.Values, Choice{s, s})
			}
		}
	case "videomodes", "videomode", "runners":
		o.Values = []Choice{{ValAuto, "auto"}}
	default:
		o.Values = append([]Choice{{ValAuto, ""}}, ft.Choices...)
	}
	o.Inherited = inherited(c, key, o.Values)
	return o
}

// sliderValues son los valores de un deslizador con "desde hasta paso
// [por defecto] [sufijo]" (en sliderauto no hay valor por defecto), con
// AUTO delante si auto.
func sliderValues(args string, auto bool) []Choice {
	t := strings.Fields(args)
	num := func(i int, def float64) float64 {
		if i < len(t) {
			if f, err := strconv.ParseFloat(t[i], 64); err == nil {
				return f
			}
		}
		return def
	}
	from, to, step := num(0, 0), num(1, 100), num(2, 1)
	suffix, at := "", 4 // el sufijo va tras el valor por defecto
	if auto {
		at = 3 // sliderauto no tiene valor por defecto
	}
	if at < len(t) {
		suffix = t[at]
	}
	var out []Choice
	if auto {
		out = append(out, Choice{ValAuto, ""})
	} else {
		// Sin AUTO: "" es el valor por defecto del deslizador.
		def := num(3, from)
		out = append(out, Choice{fmtNum(def) + suffix, ""})
	}
	if step <= 0 {
		step = 1
	}
	for v, n := from, 0; v <= to+step/1000 && n < 500; v, n = v+step, n+1 {
		out = append(out, Choice{fmtNum(v) + suffix, strconv.FormatFloat(v, 'f', 6, 64)})
	}
	return out
}

// fmtNum es un número sin decimales de sobra.
func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// inherited es el nombre del valor que hereda key si se deja en AUTO: el del
// sistema (si key es de un juego), si no el global; como GuiMenu::addFeatureItem.
func inherited(c *Conf, key string, values []Choice) string {
	if strings.HasPrefix(key, "global.") {
		return ""
	}
	sysKey, perGame := key, false
	if i := strings.Index(key, `["`); i >= 0 {
		if j := strings.Index(key, `"]`); j > i {
			sysKey, perGame = key[:i]+key[j+2:], true
		}
	}
	val := ""
	if dot := strings.Index(sysKey, "."); dot >= 0 {
		if g := c.Get("global." + sysKey[dot+1:]); g != "" && g != "auto" {
			val = g
		}
	}
	if perGame {
		if s := c.Get(sysKey); s != "" && s != "auto" {
			val = s
		}
	}
	if val == "" {
		return ""
	}
	for _, ch := range values {
		if ch.Value == val {
			return ch.Name
		}
	}
	return val
}

// Shaders son los juegos de shaders: las carpetas con un
// rendering-defaults.yml en las de shaders del usuario y de Knulli.
func (r Root) Shaders() []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range []string{"/userdata/shaders/configs", "/usr/share/knulli/shaders/configs"} {
		ents, _ := os.ReadDir(r.Path(dir))
		for _, e := range ents {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if _, err := os.Stat(filepath.Join(r.Path(dir), e.Name(), "rendering-defaults.yml")); err == nil {
				seen[e.Name()] = true
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// Decorations son los juegos de marcos (bezels): las carpetas de decoraciones
// del usuario y de Knulli.
func (r Root) Decorations() []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range []string{"/userdata/decorations", "/usr/share/knulli/datainit/decorations"} {
		ents, _ := os.ReadDir(r.Path(dir))
		for _, e := range ents {
			if e.IsDir() && !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e.Name())
			}
		}
	}
	return out
}
