package esdata

import (
	"errors"
	"html"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Los favoritos y las partidas jugadas van en el gamelist.xml de cada
// sistema, con las mismas etiquetas que escribe EmulationStation (favorite,
// playcount, lastplayed, gametime): así se comparten con él. Se cambia solo
// la entrada del juego, a mano sobre el texto (sin volver a escribir el
// archivo entero desde el XML leído, que perdería lo que no se entiende
// aquí).

var (
	// gameBlock es una entrada <game>...</game> (no <gameList>).
	gameBlock = regexp.MustCompile(`(?s)<game(?:\s[^>]*)?>.*?</game>`)
	pathTag   = regexp.MustCompile(`(?s)<path>(.*?)</path>`)
)

// SetFavorite marca (o desmarca) el juego g del sistema s como favorito en
// su gamelist.xml. Si el juego no está en él (un archivo que se encontró en
// la carpeta), se añade su entrada.
func (r Root) SetFavorite(s *System, g *Game, fav bool) error {
	value := ""
	if fav {
		value = "true"
	}
	if err := r.editGame(s.Path, g.Path, g.Name, map[string]string{"favorite": value}, fav); err != nil {
		return err
	}
	g.Favorite = fav
	return nil
}

// RecordPlay apunta en el gamelist.xml de la carpeta dir que se jugó al
// juego rom durante played, como lo hace ES al volver de un juego: una
// partida más (playcount), la fecha (lastplayed, en la hora local) y el
// tiempo jugado (gametime, en segundos, sumado).
func (r Root) RecordPlay(dir, rom, name string, played time.Duration, now time.Time) error {
	var plays, secs int
	if b, err := os.ReadFile(r.Path(path.Join(dir, "gamelist.xml"))); err == nil {
		if block := findGame(string(b), dir, rom); block != "" {
			plays, _ = strconv.Atoi(tagValue(block, "playcount"))
			secs, _ = strconv.Atoi(tagValue(block, "gametime"))
		}
	}
	return r.editGame(dir, rom, name, map[string]string{
		"playcount":  strconv.Itoa(plays + 1),
		"lastplayed": now.Format("20060102T150405"),
		"gametime":   strconv.Itoa(secs + int(played.Seconds())),
	}, true)
}

// findGame es la entrada del juego rom en el texto de un gamelist.xml de
// la carpeta dir ("" si no está).
func findGame(text, dir, rom string) string {
	for _, block := range gameBlock.FindAllString(text, -1) {
		if m := pathTag.FindStringSubmatch(block); m != nil && resolve(dir, html.UnescapeString(strings.TrimSpace(m[1]))) == rom {
			return block
		}
	}
	return ""
}

// tagValue es el texto de la etiqueta tag de una entrada ("" si no tiene).
func tagValue(block, tag string) string {
	re := regexp.MustCompile(`(?s)<` + tag + `>(.*?)</` + tag + `>`)
	if m := re.FindStringSubmatch(block); m != nil {
		return html.UnescapeString(strings.TrimSpace(m[1]))
	}
	return ""
}

// editGame cambia las etiquetas tags (el valor "" la quita) de la entrada
// del juego rom en el gamelist.xml de la carpeta dir; si no está y add, la
// añade (con su nombre). Solo cambia esa entrada, y lo guarda en un
// temporal que luego se renombra.
func (r Root) editGame(dir, rom, name string, tags map[string]string, add bool) error {
	file := r.Path(path.Join(dir, "gamelist.xml"))
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		b, err = []byte("<?xml version=\"1.0\"?>\n<gameList>\n</gameList>\n"), nil
	}
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	text := string(b)
	found := false
	text = gameBlock.ReplaceAllStringFunc(text, func(block string) string {
		m := pathTag.FindStringSubmatch(block)
		if found || m == nil || resolve(dir, html.UnescapeString(strings.TrimSpace(m[1]))) != rom {
			return block
		}
		found = true
		// Con la sangría de las demás etiquetas de la entrada.
		indent := "\t\t"
		if i := strings.Index(block, "<path>"); i > 0 {
			if j := strings.LastIndex(block[:i], "\n"); j >= 0 {
				indent = block[j+1 : i]
			}
		}
		for _, k := range keys {
			re := regexp.MustCompile(`(?s)[ \t]*<` + k + `>.*?</` + k + `>[ \t]*\r?\n?`)
			block = re.ReplaceAllString(block, "")
			if v := tags[k]; v != "" {
				end := strings.LastIndex(block, "</game>")
				before := strings.TrimRight(block[:end], " \t")
				closing := block[len(before):end]
				block = before + indent + "<" + k + ">" + html.EscapeString(v) + "</" + k + ">\n" + closing + "</game>"
			}
		}
		return block
	})
	if !found {
		if !add {
			return nil
		}
		rel := "./" + strings.TrimPrefix(strings.TrimPrefix(rom, dir), "/")
		entry := "\t<game>\n\t\t<path>" + html.EscapeString(rel) + "</path>\n\t\t<name>" + html.EscapeString(name) + "</name>\n"
		for _, k := range keys {
			if v := tags[k]; v != "" {
				entry += "\t\t<" + k + ">" + html.EscapeString(v) + "</" + k + ">\n"
			}
		}
		entry += "\t</game>\n"
		i := strings.LastIndex(text, "</gameList>")
		if i < 0 {
			return errors.New("gamelist.xml without </gameList>")
		}
		text = text[:i] + entry + text[i:]
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
