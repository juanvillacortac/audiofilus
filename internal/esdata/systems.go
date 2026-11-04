package esdata

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// System es un sistema de es_systems.cfg: una consola, los ports, etc.
type System struct {
	Name         string // el nombre corto (gba): el de las claves de knulli.conf
	FullName     string
	Manufacturer string
	Release      string
	Hardware     string // console, portable, computer, arcade...
	Path         string // la carpeta de sus juegos (como la ve la consola)
	Extensions   []string
	Command      string
	Platform     string
	Theme        string
	// Group es el grupo en que ES lo muestra (los ports sueltos van en
	// "ports", la 32X en "megadrive"...); "" si va solo. Ver Grouped.
	Group     string
	Emulators []Emulator
}

// Emulator es un emulador posible de un sistema, con sus núcleos.
type Emulator struct {
	Name  string
	Cores []Core
}

// Core es un núcleo de un emulador; Default, el que se usa si no se elige.
type Core struct {
	Name    string
	Default bool
}

// xmlSystem es un <system> tal como viene en el archivo.
type xmlSystem struct {
	Name         string `xml:"name"`
	FullName     string `xml:"fullname"`
	Manufacturer string `xml:"manufacturer"`
	Release      string `xml:"release"`
	Hardware     string `xml:"hardware"`
	Path         string `xml:"path"`
	Extension    string `xml:"extension"`
	Command      string `xml:"command"`
	Platform     string `xml:"platform"`
	Theme        string `xml:"theme"`
	Group        string `xml:"group"`
	Emulators    *struct {
		Emulator []struct {
			Name  string `xml:"name,attr"`
			Cores struct {
				Core []struct {
					Name    string `xml:",chardata"`
					Default string `xml:"default,attr"`
				} `xml:"core"`
			} `xml:"cores"`
		} `xml:"emulator"`
	} `xml:"emulators"`
}

// readSystemsFile lee un es_systems*.cfg.
func readSystemsFile(path string) ([]xmlSystem, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Systems []xmlSystem `xml:"system"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc.Systems, nil
}

// convert pasa un <system> leído a System.
func (x xmlSystem) convert() System {
	s := System{
		Name: strings.TrimSpace(x.Name), FullName: strings.TrimSpace(x.FullName),
		Manufacturer: strings.TrimSpace(x.Manufacturer), Release: strings.TrimSpace(x.Release),
		Hardware: strings.TrimSpace(x.Hardware), Path: strings.TrimSpace(x.Path),
		Command: strings.TrimSpace(x.Command), Platform: strings.TrimSpace(x.Platform),
		Theme: strings.TrimSpace(x.Theme), Extensions: strings.Fields(strings.ToLower(x.Extension)),
		Group: strings.TrimSpace(x.Group),
	}
	s.Emulators = x.emulators()
	return s
}

// emulators son los emuladores de un <system> leído (nil si no trae).
func (x xmlSystem) emulators() []Emulator {
	if x.Emulators == nil {
		return nil
	}
	var out []Emulator
	for _, e := range x.Emulators.Emulator {
		em := Emulator{Name: strings.TrimSpace(e.Name)}
		for _, c := range e.Cores.Core {
			em.Cores = append(em.Cores, Core{Name: strings.TrimSpace(c.Name), Default: c.Default == "true"})
		}
		out = append(out, em)
	}
	return out
}

// LoadSystems lee los sistemas: el es_systems.cfg del usuario si lo hay (si
// no, el de Knulli), y encima los es_systems_*.cfg del usuario, que cambian
// los emuladores de un sistema (como hace ES con el de PICO-8).
func (r Root) LoadSystems() ([]System, error) {
	main := r.firstExisting(userESDir+"/es_systems.cfg", systemDir+"/es_systems.cfg")
	if main == "" {
		return nil, fmt.Errorf("es_systems.cfg not found under %s", r.Path(systemDir))
	}
	xs, err := readSystemsFile(main)
	if err != nil {
		return nil, err
	}
	var systems []System
	index := map[string]int{}
	for _, x := range xs {
		s := x.convert()
		if s.Name == "" {
			continue
		}
		index[s.Name] = len(systems)
		systems = append(systems, s)
	}
	extra, _ := filepath.Glob(filepath.Join(r.Path(userESDir), "es_systems_*.cfg"))
	sort.Strings(extra)
	for _, path := range extra {
		xs, err := readSystemsFile(path)
		if err != nil {
			continue // uno roto no impide ver los demás
		}
		for _, x := range xs {
			i, ok := index[strings.TrimSpace(x.Name)]
			if !ok {
				continue
			}
			if em := x.emulators(); em != nil {
				systems[i].Emulators = em
			}
		}
	}
	return systems, nil
}

// DefaultEmulator es el emulador y el núcleo por defecto del sistema: el
// núcleo marcado default (o, si ninguno, el primero del primer emulador).
func (s *System) DefaultEmulator() (emulator, core string) {
	for _, e := range s.Emulators {
		for _, c := range e.Cores {
			if c.Default {
				return e.Name, c.Name
			}
		}
	}
	if len(s.Emulators) > 0 {
		e := s.Emulators[0]
		if len(e.Cores) > 0 {
			return e.Name, e.Cores[0].Name
		}
		return e.Name, ""
	}
	return "", ""
}

// HasEmulatorChoice dice si hay más de una combinación de emulador y núcleo
// para elegir (si no, ES no ofrece la opción).
func (s *System) HasEmulatorChoice() bool {
	n := 0
	for _, e := range s.Emulators {
		n += max(1, len(e.Cores))
	}
	return n > 1
}

// HiddenSystems son los sistemas que el usuario ocultó en ES (Ajustes de la
// interfaz → sistemas a mostrar): el HiddenSystems de es_settings.cfg, una
// lista separada por ";". Vacío si no hay.
func (r Root) HiddenSystems() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(r.Path(userESDir + "/es_settings.cfg"))
	if err != nil {
		return out
	}
	// es_settings.cfg no tiene un elemento raíz (son varios sueltos): se
	// lee elemento por elemento.
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		el, ok := tok.(xml.StartElement)
		if !ok || el.Name.Local != "string" {
			continue
		}
		var name, value string
		for _, at := range el.Attr {
			switch at.Name.Local {
			case "name":
				name = at.Value
			case "value":
				value = at.Value
			}
		}
		if name != "HiddenSystems" {
			continue
		}
		for _, s := range strings.Split(value, ";") {
			if s = strings.TrimSpace(s); s != "" {
				out[s] = true
			}
		}
	}
}

// Grouped dice si el sistema s va dentro de su grupo, como en ES: si tiene
// uno y en knulli.conf no se pidió separarlo (<grupo>.ungroup=1).
func (s *System) Grouped(conf *Conf) bool {
	if s.Group == "" {
		return false
	}
	return conf == nil || conf.Get(s.Group+".ungroup") != "1"
}
