package modsrc

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// AMP, Amiga Music Preservation (amp.dascene.net): los módulos de la escena
// Amiga, ordenados por autor. Se leen sus páginas: la búsqueda (por título
// de módulo o por autor, un formulario POST a newresult.php), la lista de
// módulos de cada autor y la de lo último añadido. Cada módulo es una fila de
// tabla con su enlace downmod.php?index=<id>, el autor, el formato y el
// tamaño; la descarga redirige a un .gz con el nombre al estilo Amiga
// ("MOD.titulo.gz"), que se descomprime.

// ampBase es la dirección (los tests la cambian por un servidor local).
var ampBase = "https://amp.dascene.net"

type amp struct{}

func (*amp) Name() string { return "AMP" }

// Search busca módulos por título.
func (a *amp) Search(ctx context.Context, query string) ([]Item, error) {
	form := url.Values{"search": {query}, "request": {"module"}, "Submit": {"Search"}}.Encode()
	page, _, err := postPage(ctx, ampBase+"/newresult.php", form)
	if err != nil {
		return nil, err
	}
	return capItems(parseAMP(page)), nil
}

// Browse: solo lo último añadido.
func (a *amp) Browse(ctx context.Context, what string) ([]Item, error) {
	if what != "new" {
		return nil, ErrUnsupported
	}
	page, _, err := getPage(ctx, ampBase+"/added.php?sub=mus")
	if err != nil {
		return nil, err
	}
	return capItems(parseAMP(page)), nil
}

// ampHandleRe saca los autores de una búsqueda por nombre ("Handle").
var ampHandleRe = regexp.MustCompile(`(?s)Handle: </td><td><a href="detail\.php\?view=(\d+)">(.*?)</a>`)

// ByArtist busca el autor (el de nombre igual, o el primero) y lista sus
// módulos.
func (a *amp) ByArtist(ctx context.Context, artist string) ([]Item, error) {
	form := url.Values{"search": {artist}, "request": {"handle"}, "Submit": {"Search"}}.Encode()
	page, _, err := postPage(ctx, ampBase+"/newresult.php", form)
	if err != nil {
		return nil, err
	}
	id := ""
	for _, m := range ampHandleRe.FindAllStringSubmatch(page, -1) {
		if id == "" {
			id = m[1]
		}
		if strings.EqualFold(text(m[2]), artist) {
			id = m[1]
			break
		}
	}
	if id == "" {
		return nil, nil
	}
	page, _, err = getPage(ctx, ampBase+"/detail.php?detail=modules&view="+id)
	if err != nil {
		return nil, err
	}
	items := parseAMP(page)
	if len(items) > 3*maxResults {
		items = items[:3*maxResults]
	}
	return items, nil
}

// capItems acota a maxResults.
func capItems(items []Item) []Item {
	if len(items) > maxResults {
		return items[:maxResults]
	}
	return items
}

// ampDownRe es el enlace de descarga de un módulo, con su título.
var ampDownRe = regexp.MustCompile(`(?s)href="downmod\.php\?index=(\d+)[^"]*"[^>]*>(.*?)</a>`)

// parseAMP saca los módulos de una tabla de AMP. Las columnas cambian entre
// páginas (lo último añadido tiene antes la fecha), así que se toman a partir
// de la del enlace: el autor, el formato y el tamaño.
func parseAMP(page string) []Item {
	var out []Item
	for _, row := range splitBlocks(page, `<tr class="tr`) {
		if end := strings.Index(row, "</tr>"); end >= 0 {
			row = row[:end]
		}
		tds := tdRe.FindAllStringSubmatch(row, -1)
		for i, td := range tds {
			m := ampDownRe.FindStringSubmatch(td[1])
			if m == nil {
				continue
			}
			it := Item{Source: "amp", ID: m[1], Title: text(m[2]),
				Page: ampBase + "/analyzer2.php?idx=" + m[1], URL: ampBase + "/downmod.php?index=" + m[1]}
			if i+1 < len(tds) {
				it.Artist = strings.ReplaceAll(text(tds[i+1][1]), " & ", ", ")
			}
			if i+2 < len(tds) {
				it.Format = strings.ToUpper(text(tds[i+2][1]))
			}
			if i+3 < len(tds) {
				it.Size = parseSize(text(tds[i+3][1]))
			}
			out = append(out, it)
			break
		}
	}
	return keepPlayable(dedupe(out))
}

// fetchAMP baja un módulo de AMP a dir/amp/<id> <título>.<ext>,
// descomprimiéndolo.
func fetchAMP(ctx context.Context, it Item, dir string, progress func(done, total int64)) ([]string, error) {
	ext := formatExt[strings.ToUpper(it.Format)]
	if ext == "" {
		return nil, fmt.Errorf("amp: format %q is not playable", it.Format)
	}
	p := filepath.Join(dir, "amp", safeName(it.ID+" "+it.Title)+ext)
	if c := cached(filepath.Dir(p)); len(c) > 0 {
		for _, f := range c {
			if f == p {
				return []string{p}, nil
			}
		}
	}
	u := it.URL
	if u == "" {
		u = ampBase + "/downmod.php?index=" + url.QueryEscape(it.ID)
	}
	data, _, err := download(ctx, u, progress)
	if err != nil {
		return nil, err
	}
	if a := isArchive(data); a != "" {
		return nil, fmt.Errorf("amp: %s archives are not supported", a)
	}
	if data, err = gunzipIfNeeded(data); err != nil {
		return nil, err
	}
	if a := isArchive(data); a != "" {
		return nil, fmt.Errorf("amp: %s archives are not supported", a)
	}
	if err := writeFile(p, data); err != nil {
		return nil, err
	}
	return []string{p}, nil
}
