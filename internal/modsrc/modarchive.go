package modsrc

import (
	"context"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The Mod Archive (modarchive.org): su API en XML pide clave, así que se
// leen sus páginas, que listan cada módulo con un enlace de descarga
// "downloads.php?moduleid=<id>#<archivo>" (eso basta para el id, el nombre
// del archivo y, por su extensión, el formato). La descarga, por ese mismo
// enlace de api.modarchive.org, no pide clave.

// Las direcciones (los tests las cambian por un servidor local).
var (
	modArchiveBase = "https://modarchive.org"
	modArchiveDL   = "https://api.modarchive.org/downloads.php?moduleid="
)

type modArchive struct{}

func (*modArchive) Name() string { return "The Mod Archive" }

// Search busca por nombre de archivo o título, en hasta tres páginas.
func (m *modArchive) Search(ctx context.Context, query string) ([]Item, error) {
	q := url.Values{"request": {"search"}, "query": {query}, "submit": {"Find"}, "search_type": {"filename_or_songtitle"}}
	return m.pages(ctx, "/index.php?"+q.Encode(), "")
}

// Browse: al azar (los diez que sugiere su buscador), lo más bajado (su
// lista de éxitos) y lo último que subieron.
func (m *modArchive) Browse(ctx context.Context, what string) ([]Item, error) {
	switch what {
	case "random":
		return m.page(ctx, "/index.php?request=view_searchbox", "")
	case "top":
		return m.page(ctx, "/index.php?request=view_chart&query=tophits", "")
	case "new":
		return m.page(ctx, "/index.php?request=view_actions_uploads", "")
	}
	return nil, ErrUnsupported
}

// memberRe saca los artistas de su buscador de artistas.
var memberRe = regexp.MustCompile(`(?s)<a href="member\.php\?(\d+)" class='search-result-link'>(.*?)</a>`)

// ByArtist busca el artista (el de nombre igual, o el primero) y lista sus
// módulos.
func (m *modArchive) ByArtist(ctx context.Context, artist string) ([]Item, error) {
	q := url.Values{"request": {"search"}, "query": {artist}, "submit": {"Find"}, "search_type": {"search_artist"}}
	page, _, err := getPage(ctx, modArchiveBase+"/index.php?"+q.Encode())
	if err != nil {
		return nil, err
	}
	id, name := "", ""
	for _, mm := range memberRe.FindAllStringSubmatch(page, -1) {
		n := text(mm[2])
		if id == "" || strings.EqualFold(n, artist) {
			id, name = mm[1], n
		}
		if strings.EqualFold(n, artist) {
			break
		}
	}
	if id == "" {
		return nil, nil
	}
	return m.pages(ctx, "/index.php?request=view_artist_modules&query="+id, name)
}

// pages lee una lista y, si tiene más páginas, hasta dos más.
func (m *modArchive) pages(ctx context.Context, rel, artist string) ([]Item, error) {
	var out []Item
	for p := 1; p <= 3 && len(out) < maxResults; p++ {
		u := rel
		if p > 1 {
			u += fmt.Sprintf("&page=%d", p)
		}
		page, _, err := getPage(ctx, modArchiveBase+u)
		if err != nil {
			if p > 1 {
				break // las páginas de más no son imprescindibles
			}
			return nil, err
		}
		items := parseModArchive(page, artist)
		n := len(out)
		out = dedupe(append(out, items...))
		if len(out) == n || !strings.Contains(page, fmt.Sprintf("page=%d", p+1)) {
			break
		}
	}
	if len(out) > maxResults {
		out = out[:maxResults]
	}
	return out, nil
}

// page lee una sola lista.
func (m *modArchive) page(ctx context.Context, rel, artist string) ([]Item, error) {
	page, _, err := getPage(ctx, modArchiveBase+rel)
	if err != nil {
		return nil, err
	}
	items := parseModArchive(page, artist)
	if len(items) > maxResults {
		items = items[:maxResults]
	}
	return items, nil
}

var (
	maDownRe   = regexp.MustCompile(`downloads\.php\?moduleid=(\d+)#([^"'<>\s]+)`)
	maFormatRe = regexp.MustCompile(`<span class="format-icon">([^<]+)</span>`)
	maTitleRe  = regexp.MustCompile(`(?s)<span class=["']module-listing["']>(.*?)</span>`)
	maLinkRe   = regexp.MustCompile(`(?s)<a class=["'](?:standard-link|module-listing)["'] href=["'][^"']*(?:module\.php\?|view_by_moduleid&amp;query=)(\d+)["'][^>]*title=["']([^"']*)["']`)
	maSizeRe   = regexp.MustCompile(`Uncompressed Size: ([0-9.]+\s*[KMG]?B)`)
	maH1Re     = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
)

// parseModArchive saca los módulos de una página de The Mod Archive: cada
// enlace de descarga (el primero de cada id) y, en el trozo hasta el
// siguiente, su formato y su título si están. artist, si no está vacío, es
// el de todos.
func parseModArchive(page, artist string) []Item {
	locs := maDownRe.FindAllStringSubmatchIndex(page, -1)
	var out []Item
	seen := map[string]bool{}
	for i, loc := range locs {
		id, file := page[loc[2]:loc[3]], page[loc[4]:loc[5]]
		if seen[id] {
			continue
		}
		seen[id] = true
		end := len(page)
		for _, l := range locs[i+1:] {
			if page[l[2]:l[3]] != id {
				end = l[0]
				break
			}
		}
		block := page[loc[0]:end]
		file, _ = url.PathUnescape(file)
		it := Item{Source: "modarchive", ID: id, Title: file, Artist: artist,
			Page: modArchiveBase + "/index.php?request=view_by_moduleid&query=" + id,
			URL:  modArchiveDL + id}
		it.Format = strings.ToUpper(strings.TrimPrefix(path.Ext(file), "."))
		if f := maFormatRe.FindStringSubmatch(block); f != nil {
			it.Format = strings.ToUpper(text(f[1]))
		}
		if l := maLinkRe.FindStringSubmatch(block); l != nil && l[1] == id && text(l[2]) != "" {
			it.Title = text(l[2])
		}
		if t := maTitleRe.FindStringSubmatch(block); t != nil && text(t[1]) != "" && !strings.Contains(t[1], "<a") {
			it.Title = text(t[1])
		}
		if s := maSizeRe.FindStringSubmatch(block); s != nil {
			it.Size = parseSize(s[1])
		}
		out = append(out, it)
	}
	// La página de un solo módulo (la de "al azar") dice su título en el h1.
	if len(out) == 1 && strings.Contains(page, "mod-page-archive-info") {
		if h := maH1Re.FindStringSubmatch(page); h != nil && text(h[1]) != "" {
			out[0].Title = text(h[1])
		}
		if s := maSizeRe.FindStringSubmatch(page); s != nil {
			out[0].Size = parseSize(s[1])
		}
	}
	return keepPlayable(out)
}

// fetchModArchive baja un módulo de The Mod Archive a dir/modarchive/<id>/,
// con su nombre (del Content-Disposition si viene): en una carpeta por id,
// así el archivo conserva su nombre limpio (y el título que se ve sin el
// número delante).
func fetchModArchive(ctx context.Context, it Item, dir string, progress func(done, total int64)) ([]string, error) {
	dir = filepath.Join(dir, "modarchive", safeName(it.ID))
	if m, _ := filepath.Glob(filepath.Join(dir, "*")); len(m) > 0 {
		for _, p := range m {
			if !strings.HasSuffix(p, ".part") {
				return []string{p}, nil
			}
		}
	}
	u := it.URL
	if u == "" {
		u = modArchiveDL + url.QueryEscape(it.ID)
	}
	data, resp, err := download(ctx, u, progress)
	if err != nil {
		return nil, err
	}
	name := it.Title
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	if !playableExt(name) {
		if ext := formatExt[strings.ToUpper(it.Format)]; ext != "" {
			name += ext
		}
	}
	if a := isArchive(data); a != "" {
		return nil, fmt.Errorf("%s: %s archives are not supported", name, a)
	}
	p := filepath.Join(dir, safeName(name))
	if err := writeFile(p, data); err != nil {
		return nil, err
	}
	_ = os.Remove(p + ".part")
	return []string{p}, nil
}
