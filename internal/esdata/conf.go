package esdata

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// Conf es knulli.conf: los ajustes de Knulli y de cada sistema y juego
// (snes.shaderset=..., gba["juego.zip"].core=...). Se guarda como ES
// (SystemConf::saveSystemConf): solo se tocan las líneas de las claves
// cambiadas; una clave vacía o "auto" se borra del archivo; las demás líneas,
// comentarios incluidos, quedan como estaban. Así no pisa lo que ES (u otro)
// haya escrito entre medias.
type Conf struct {
	path    string
	values  map[string]string
	changed map[string]bool
}

// LoadConf lee el knulli.conf de la raíz (vacío si no existe).
func (r Root) LoadConf() (*Conf, error) {
	c := &Conf{path: r.ConfPath(), values: map[string]string{}, changed: map[string]bool{}}
	b, err := os.ReadFile(c.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			c.values[strings.TrimSpace(k)] = v
		}
	}
	return c, nil
}

// Get es el valor de la clave ("" si no está).
func (c *Conf) Get(key string) string { return c.values[key] }

// Set cambia la clave; "" o "auto" la quitan. Se escribe con Save.
func (c *Conf) Set(key, value string) {
	if value == "auto" {
		value = ""
	}
	if c.values[key] == value {
		return
	}
	if value == "" {
		delete(c.values, key)
	} else {
		c.values[key] = value
	}
	c.changed[key] = true
}

// Dirty dice si hay cambios sin guardar.
func (c *Conf) Dirty() bool { return len(c.changed) > 0 }

// Save escribe los cambios: relee el archivo (por si otro lo cambió),
// reemplaza o quita las líneas de las claves cambiadas (también si estaban
// comentadas) y añade al final las que no estaban. Escribe en un temporal y
// lo renombra, para no dejarlo a medias.
func (c *Conf) Save() error {
	if len(c.changed) == 0 {
		return nil
	}
	old, err := os.ReadFile(c.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	if len(old) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")
	}
	done := map[string]bool{}
	out := lines[:0:0]
	for _, line := range lines {
		t := strings.TrimSpace(line)
		bare := strings.TrimLeft(t, "#;")
		if k, _, ok := strings.Cut(bare, "="); ok && c.changed[strings.TrimSpace(k)] {
			k = strings.TrimSpace(k)
			if done[k] {
				continue // una repetida: ya se escribió
			}
			commented := bare != t
			v, set := c.values[k]
			switch {
			case set:
				out = append(out, k+"="+v)
				done[k] = true
			case commented:
				out = append(out, line) // quitarla comentada: se deja el comentario
			}
			continue
		}
		out = append(out, line)
	}
	for k := range c.changed {
		if v, set := c.values[k]; set && !done[k] {
			out = append(out, k+"="+v)
		}
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		os.Remove(tmp)
		return err
	}
	c.changed = map[string]bool{}
	return nil
}
