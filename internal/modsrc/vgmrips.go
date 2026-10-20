package modsrc

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// VGMRips (vgmrips.net): música de consolas y arcades en VGM, en paquetes
// (un zip por juego, con sus pistas .vgz). Se leen sus páginas: la búsqueda,
// lo último, lo más bajado y las de cada compositor listan los paquetes con
// su enlace de descarga; "al azar" lleva a la página de un paquete.

// vgmRipsBase es la dirección (los tests la cambian por un servidor local).
var vgmRipsBase = "https://vgmrips.net"

type vgmRips struct{}

func (*vgmRips) Name() string { return "VGMRips" }

// Search busca paquetes (VGMRips da como mucho 20 por búsqueda, sin más
// páginas).
func (v *vgmRips) Search(ctx context.Context, query string) ([]Item, error) {
	return v.list(ctx, "/packs/search?"+url.Values{"q": {query}}.Encode())
}

// Browse: lo último, lo más bajado y unos cuantos al azar (la página de
// "al azar" es la de un solo paquete: se pide cinco veces).
func (v *vgmRips) Browse(ctx context.Context, what string) ([]Item, error) {
	switch what {
	case "new":
		return v.list(ctx, "/packs/latest")
	case "top":
		return v.list(ctx, "/packs/top")
	case "random":
		var out []Item
		for i := 0; i < 5; i++ {
			page, final, err := getPage(ctx, vgmRipsBase+"/packs/random")
			if err != nil {
				if len(out) > 0 {
					break
				}
				return nil, err
			}
			if it, ok := parseVGMRipsPack(page, final); ok {
				out = append(out, it)
			}
		}
		return dedupe(out), nil
	}
	return nil, ErrUnsupported
}

// ByArtist lista los paquetes de un compositor (su página se llama como su
// nombre en minúsculas y con guiones).
func (v *vgmRips) ByArtist(ctx context.Context, artist string) ([]Item, error) {
	return v.list(ctx, "/packs/composer/"+slugify(artist))
}

// list lee una lista de paquetes.
func (v *vgmRips) list(ctx context.Context, rel string) ([]Item, error) {
	page, _, err := getPage(ctx, vgmRipsBase+rel)
	if err != nil {
		return nil, err
	}
	return parseVGMRips(page), nil
}

// slugify es como VGMRips nombra las páginas: "Masato Nakamura" →
// "masato-nakamura".
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

var (
	vrPackRe     = regexp.MustCompile(`(?s)<a href="([^"#]*/packs/pack/([^"#/]+))">(.*?)</a>`)
	vrDownRe     = regexp.MustCompile(`(?s)<a class="download" href="([^"]+\.zip)"><small>(.*?)</small>`)
	vrComposerRe = regexp.MustCompile(`(?s)<tr class="composers">(.*?)</tr>`)
	vrBadgeRe    = regexp.MustCompile(`(?s)<a class="badge"[^>]*>(.*?)</a>`)
	vrSizeRe     = regexp.MustCompile(`([0-9.]+\s*[KMG]B)`)
	vrH1Re       = regexp.MustCompile(`(?s)<h1>(.*?)</h1>`)
	vrZipRe      = regexp.MustCompile(`href="([^"]+/files/[^"]+\.zip)"`)
	vrComposerA  = regexp.MustCompile(`(?s)<a href="[^"]*/packs/composer/[^"]*">(.*?)</a>`)
)

// parseVGMRips saca los paquetes de una lista de VGMRips (cada uno, un
// bloque "result row").
func parseVGMRips(page string) []Item {
	var out []Item
	for _, b := range splitBlocks(page, `<div class="result row"`) {
		// El primer enlace al paquete es el de su imagen: el título es el
		// primero con texto.
		var pm []string
		for _, m := range vrPackRe.FindAllStringSubmatch(b, -1) {
			if text(m[3]) != "" {
				pm = m
				break
			}
		}
		dm := vrDownRe.FindStringSubmatch(b)
		if pm == nil || dm == nil {
			continue
		}
		it := Item{Source: "vgmrips", ID: pm[2], Title: text(pm[3]), Format: "VGM", Pack: true,
			Page: pm[1], URL: html2url(dm[1])}
		if s := vrSizeRe.FindStringSubmatch(text(dm[2])); s != nil {
			it.Size = parseSize(s[1])
		}
		if c := vrComposerRe.FindStringSubmatch(b); c != nil {
			var names []string
			for _, a := range vrBadgeRe.FindAllStringSubmatch(c[1], -1) {
				names = append(names, text(a[1]))
			}
			it.Artist = strings.Join(names, ", ")
		}
		out = append(out, it)
	}
	return out
}

// parseVGMRipsPack lee la página de un paquete (la de "al azar").
func parseVGMRipsPack(page, final string) (Item, bool) {
	h := vrH1Re.FindStringSubmatch(page)
	z := vrZipRe.FindStringSubmatch(page)
	slug := final[strings.LastIndex(final, "/")+1:]
	if h == nil || z == nil || !strings.Contains(final, "/packs/pack/") {
		return Item{}, false
	}
	it := Item{Source: "vgmrips", ID: slug, Title: text(h[1]), Format: "VGM", Pack: true, Page: final, URL: html2url(z[1])}
	var names []string
	for _, a := range vrComposerA.FindAllStringSubmatch(page, -1) {
		names = append(names, text(a[1]))
	}
	it.Artist = strings.Join(names, ", ")
	return it, true
}

// html2url resuelve las entidades de una dirección sacada de un atributo.
func html2url(s string) string { return strings.ReplaceAll(s, "&amp;", "&") }

// fetchVGMRips baja un paquete y saca sus pistas a dir/vgmrips/<paquete>/.
func fetchVGMRips(ctx context.Context, it Item, dir string, progress func(done, total int64)) ([]string, error) {
	dir = filepath.Join(dir, "vgmrips", safeName(it.ID))
	if c := cached(dir); len(c) > 0 {
		return c, nil
	}
	u := it.URL
	if u == "" {
		// Solo el nombre: la dirección del zip está en su página.
		page, final, err := getPage(ctx, vgmRipsBase+"/packs/pack/"+url.PathEscape(it.ID))
		if err != nil {
			return nil, err
		}
		p, ok := parseVGMRipsPack(page, final)
		if !ok {
			return nil, fmt.Errorf("vgmrips: no download for %s", it.ID)
		}
		u = p.URL
	}
	data, _, err := download(ctx, u, progress)
	if err != nil {
		return nil, err
	}
	return unzipPlayable(data, dir)
}
