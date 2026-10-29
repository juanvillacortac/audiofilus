package modsrc

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand/v2"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Modland (ftp.modland.com): el archivo más grande, con medio millón de
// archivos de todos los formatos. Publica un índice de todo, allmods.zip
// (unos 6 MB; dentro, allmods.txt con una línea "<tamaño>\t<ruta>" por
// archivo, 30 MB). Se baja una vez (y de nuevo al mes) y se guarda solo lo
// que audiofilus sabe tocar (unas 230 000 líneas); las búsquedas lo leen
// línea a línea, sin cargarlo entero en memoria (en una portátil importa).
// Las rutas son <formato>/<autor>/<archivo> (o .../<autor>/<juego>/<archivo>);
// algunas categorías tienen un nivel más antes del autor: el sistema en
// "Video Game Music" y el programa en "Ad Lib".

// Las direcciones (los tests las cambian por un servidor local).
var (
	modlandIndex = "https://ftp.modland.com/allmods.zip"
	modlandFiles = "https://ftp.modland.com/pub/modules/"
)

// modlandMaxAge es cada cuánto se vuelve a bajar el índice.
const modlandMaxAge = 30 * 24 * time.Hour

type modland struct {
	cacheDir string
	mu       sync.Mutex // una sola descarga del índice a la vez
}

func newModland(cacheDir string) *modland { return &modland{cacheDir: cacheDir} }

func (*modland) Name() string { return "Modland" }

// indexPath es el índice ya filtrado. Su nombre lleva una suma de las
// extensiones que se pueden tocar: si cambian (un formato nuevo), el índice
// viejo no sirve y se vuelve a bajar.
func (m *modland) indexPath() string {
	sum := crc32.ChecksumIEEE([]byte(strings.Join(decode.Extensions, ",")))
	return filepath.Join(m.cacheDir, fmt.Sprintf("modland-%08x.txt", sum))
}

// removeOldIndexes borra los índices de otras extensiones.
func (m *modland) removeOldIndexes() {
	old, _ := filepath.Glob(filepath.Join(m.cacheDir, "modland*.txt"))
	for _, f := range old {
		if f != m.indexPath() {
			os.Remove(f)
		}
	}
}

// ensureIndex baja el índice si no está o es viejo. Si la descarga falla y
// hay uno viejo, se sigue con ese.
func (m *modland) ensureIndex(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := os.Stat(m.indexPath())
	if err == nil && time.Since(st.ModTime()) < modlandMaxAge {
		return nil
	}
	if err2 := m.fetchIndex(ctx); err2 != nil {
		if err == nil {
			return nil // el viejo sirve
		}
		return err2
	}
	return nil
}

// fetchIndex baja allmods.zip y escribe el índice con lo que se puede
// tocar.
func (m *modland) fetchIndex(ctx context.Context) error {
	data, _, err := download(ctx, modlandIndex, nil)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var f *zip.File
	for _, zf := range zr.File {
		if strings.HasSuffix(strings.ToLower(zf.Name), ".txt") {
			f = zf
			break
		}
	}
	if f == nil {
		return errors.New("modland: allmods.txt not in the index")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(m.cacheDir, 0o755); err != nil {
		return err
	}
	tmp := m.indexPath() + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		line := sc.Text()
		if _, p, ok := strings.Cut(line, "\t"); ok && playableExt(p) {
			w.WriteString(line)
			w.WriteByte('\n')
			n++
		}
	}
	if err := sc.Err(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := w.Flush(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	if n == 0 {
		os.Remove(tmp)
		return errors.New("modland: empty index")
	}
	if err := os.Rename(tmp, m.indexPath()); err != nil {
		return err
	}
	m.removeOldIndexes()
	return nil
}

// scan recorre el índice llamando a fn con cada línea; fn devuelve false
// para parar.
func (m *modland) scan(ctx context.Context, fn func(size int64, p string) bool) error {
	if err := m.ensureIndex(ctx); err != nil {
		return err
	}
	f, err := os.Open(m.indexPath())
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for i := 0; sc.Scan(); i++ {
		if i%4096 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		s, p, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		size, _ := strconv.ParseInt(s, 10, 64)
		if !fn(size, p) {
			break
		}
	}
	return sc.Err()
}

// nestedModland son las categorías de Modland con un nivel antes del autor.
var nestedModland = map[string]bool{"Video Game Music": true, "Ad Lib": true}

// modlandItem es el resultado de una ruta del índice.
func modlandItem(size int64, p string) Item {
	parts := strings.Split(p, "/")
	artist := ""
	switch {
	case len(parts) >= 4 && nestedModland[parts[0]]:
		artist = parts[2]
	case len(parts) >= 3:
		artist = parts[1]
	}
	if strings.HasPrefix(artist, "- ") { // "- unknown"
		artist = ""
	}
	file := path.Base(p)
	return Item{Source: "modland", ID: p, Title: strings.TrimSuffix(file, path.Ext(file)), Artist: artist,
		Format: strings.ToUpper(strings.TrimPrefix(path.Ext(file), ".")), Size: size,
		Page: modlandFiles + escapePath(path.Dir(p)) + "/", URL: modlandFiles + escapePath(p)}
}

// escapePath escapa cada parte de una ruta para una dirección.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// Search busca las rutas que tienen todas las palabras (sin distinguir
// mayúsculas).
func (m *modland) Search(ctx context.Context, query string) ([]Item, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, nil
	}
	var out []Item
	err := m.scan(ctx, func(size int64, p string) bool {
		lp := strings.ToLower(p)
		for _, w := range words {
			if !strings.Contains(lp, w) {
				return true
			}
		}
		out = append(out, modlandItem(size, p))
		return len(out) < maxResults
	})
	return out, err
}

// Browse: solo al azar (50 de todo el índice; Modland no tiene listas de lo
// más bajado ni de lo nuevo).
func (m *modland) Browse(ctx context.Context, what string) ([]Item, error) {
	if what != "random" {
		return nil, ErrUnsupported
	}
	const k = 50
	var pick []Item
	n := 0
	err := m.scan(ctx, func(size int64, p string) bool {
		n++
		switch {
		case len(pick) < k:
			pick = append(pick, modlandItem(size, p))
		default:
			if j := rand.IntN(n); j < k {
				pick[j] = modlandItem(size, p)
			}
		}
		return true
	})
	return pick, err
}

// ByArtist lista lo del autor de nombre igual (sin distinguir mayúsculas),
// en todos los formatos.
func (m *modland) ByArtist(ctx context.Context, artist string) ([]Item, error) {
	want := strings.ToLower(strings.TrimSpace(artist))
	if want == "" {
		return nil, nil
	}
	var out []Item
	err := m.scan(ctx, func(size int64, p string) bool {
		it := modlandItem(size, p)
		if strings.ToLower(it.Artist) == want {
			out = append(out, it)
		}
		return len(out) < 3*maxResults
	})
	return out, err
}

// fetchModland baja un archivo de Modland a dir/modland/<su ruta>.
func fetchModland(ctx context.Context, it Item, dir string, progress func(done, total int64)) ([]string, error) {
	var parts []string
	for _, s := range strings.Split(it.ID, "/") {
		if s == "" || s == "." || s == ".." {
			return nil, fmt.Errorf("modland: bad path %q", it.ID)
		}
		parts = append(parts, safeName(s))
	}
	p := filepath.Join(append([]string{dir, "modland"}, parts...)...)
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return []string{p}, nil
	}
	u := it.URL
	if u == "" {
		u = modlandFiles + escapePath(it.ID)
	}
	data, _, err := download(ctx, u, progress)
	if err != nil {
		return nil, err
	}
	if err := writeFile(p, data); err != nil {
		return nil, err
	}
	return []string{p}, nil
}
