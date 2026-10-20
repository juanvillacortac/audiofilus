// Package modsrc busca y baja música de los archivos de la escena en
// internet: The Mod Archive (modarchive.org), Modland (ftp.modland.com),
// VGMRips (vgmrips.net) y AMP (amp.dascene.net). Ninguno pide clave: de The
// Mod Archive, VGMRips y AMP se leen sus páginas web; de Modland, el índice
// de todos sus archivos (allmods.zip), que se baja una vez y se busca sin
// conexión. Fetch deja en una carpeta los archivos que audiofilus sabe tocar
// (un módulo, o las pistas de un paquete de VGMRips).
package modsrc

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Item es una canción (o un paquete de canciones) de un archivo.
type Item struct {
	Source string // "modarchive", "modland", "vgmrips", "amp"
	ID     string // estable dentro de su archivo (id del módulo, ruta en Modland, nombre del paquete)
	Title  string // el título (o el nombre del archivo si no se sabe)
	Artist string // quién la compuso ("" si no se sabe)
	Format string // "MOD", "XM", "IT", "S3M", "SPC", "VGM"... (en mayúsculas)
	Size   int64  // bytes, si se sabe (si no, 0)
	Pack   bool   // es un paquete de varias pistas (los de VGMRips)
	Page   string // su página web, para personas (opcional)
	URL    string // la descarga, si se sabe ya (si no, Fetch la busca)
}

// Source es un archivo de música en internet.
type Source interface {
	// Name es su nombre para la interfaz.
	Name() string
	// Search busca por texto (hasta unos 100 resultados).
	Search(ctx context.Context, query string) ([]Item, error)
	// Browse lista "random" (al azar), "top" (lo más bajado) o "new" (lo
	// último); lo que el archivo no tenga devuelve ErrUnsupported.
	Browse(ctx context.Context, what string) ([]Item, error)
	// ByArtist lista lo de un autor.
	ByArtist(ctx context.Context, artist string) ([]Item, error)
}

// ErrUnsupported es lo que devuelve un archivo para lo que no sabe hacer.
var ErrUnsupported = errors.New("modsrc: not supported by this source")

// Sources son los archivos, en orden: The Mod Archive, Modland, VGMRips y
// AMP. cacheDir es donde se guarda lo que conviene no volver a bajar (el
// índice de Modland): <data>/cache/modsrc.
func Sources(cacheDir string) []Source {
	return []Source{&modArchive{}, newModland(cacheDir), &vgmRips{}, &amp{}}
}

// maxResults es cuántos resultados devuelve como mucho una búsqueda.
const maxResults = 100

// maxDownload es el tamaño máximo de una descarga (un paquete grande de
// VGMRips ronda los 20 MB).
const maxDownload = 64 << 20

// userAgent se presenta en cada petición: los archivos son proyectos de
// voluntarios y conviene que sepan quién les pide.
const userAgent = "audiofilus (+https://github.com/juanvillacortac/audiofilus)"

// pageClient es para las páginas; dlClient, para las descargas (que pueden
// tardar en la wifi de una portátil).
var (
	pageClient = &http.Client{Timeout: 30 * time.Second}
	dlClient   = &http.Client{Timeout: 10 * time.Minute}
)

// La cortesía: entre dos peticiones al mismo servidor pasa al menos
// politeGap. Así ni una lista de pasos (varias páginas de resultados) ni
// alguien pulsando muy rápido les cargan de golpe.
var (
	politeMu   sync.Mutex
	politeLast = map[string]time.Time{}
	politeGap  = 400 * time.Millisecond
)

// polite espera lo que haga falta antes de pedir a host.
func polite(ctx context.Context, host string) error {
	politeMu.Lock()
	wait := time.Until(politeLast[host].Add(politeGap))
	if wait < 0 {
		wait = 0
	}
	politeLast[host] = time.Now().Add(wait)
	politeMu.Unlock()
	if wait == 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// request hace una petición con el User-Agent y la cortesía, y falla si no
// contesta 200.
func request(ctx context.Context, c *http.Client, method, u string, body io.Reader, ctype string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if err := polite(ctx, req.URL.Host); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return resp, nil
}

// getPage baja una página web (hasta 4 MB) y dice a qué dirección llegó
// (tras las redirecciones).
func getPage(ctx context.Context, u string) (string, string, error) {
	return postPage(ctx, u, "")
}

// postPage es getPage, enviando form si no está vacío.
func postPage(ctx context.Context, u, form string) (string, string, error) {
	method, body, ctype := "GET", io.Reader(nil), ""
	if form != "" {
		method, body, ctype = "POST", strings.NewReader(form), "application/x-www-form-urlencoded"
	}
	resp, err := request(ctx, pageClient, method, u, body, ctype)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", "", err
	}
	return string(b), resp.Request.URL.String(), nil
}

// formatExt es la extensión de cada formato que audiofilus puede tocar
// (con libxmp los módulos y con libgme la música de consolas). Las variantes
// del MOD de Amiga (FST, STK, NST...) son MOD.
var formatExt = map[string]string{
	"MOD": ".mod", "FST": ".mod", "STK": ".mod", "NST": ".mod", "M15": ".mod",
	"XM": ".xm", "IT": ".it", "S3M": ".s3m", "MPTM": ".mptm", "669": ".669", "MED": ".med",
	"MTM": ".mtm", "STM": ".stm", "FAR": ".far", "ULT": ".ult",
	"NSF": ".nsf", "NSFE": ".nsfe", "SPC": ".spc", "GBS": ".gbs", "VGM": ".vgm", "VGZ": ".vgz",
	"GYM": ".gym", "HES": ".hes", "KSS": ".kss", "AY": ".ay", "SAP": ".sap",
}

// Playable dice si audiofilus sabe tocar ese formato (por la lista de
// extensiones de sus decodificadores). El SID, por ejemplo, no.
func Playable(format string) bool {
	ext, ok := formatExt[strings.ToUpper(format)]
	if !ok {
		return false
	}
	for _, e := range decode.Extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// playableExt dice si un archivo, por su extensión, se puede tocar.
func playableExt(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, e := range decode.Extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// keepPlayable deja lo que se puede tocar (los paquetes, siempre).
func keepPlayable(items []Item) []Item {
	out := items[:0]
	for _, it := range items {
		if it.Pack || Playable(it.Format) {
			out = append(out, it)
		}
	}
	return out
}

// Fetch baja it a dir (creándola) y devuelve los archivos que se pueden
// tocar: uno para un módulo, todas las pistas (en orden) para un paquete. Lo
// ya bajado no se vuelve a bajar. progress, si no es nil, recibe lo bajado y
// el total (-1 si no se sabe).
func Fetch(ctx context.Context, it Item, dir string, progress func(done, total int64)) ([]string, error) {
	switch it.Source {
	case "modarchive":
		return fetchModArchive(ctx, it, dir, progress)
	case "modland":
		return fetchModland(ctx, it, dir, progress)
	case "vgmrips":
		return fetchVGMRips(ctx, it, dir, progress)
	case "amp":
		return fetchAMP(ctx, it, dir, progress)
	}
	return nil, fmt.Errorf("modsrc: unknown source %q", it.Source)
}

// download baja u entera a memoria (hasta maxDownload), con su progreso, y
// devuelve también la respuesta (por sus cabeceras y la dirección final).
func download(ctx context.Context, u string, progress func(done, total int64)) ([]byte, *http.Response, error) {
	resp, err := request(ctx, dlClient, "GET", u, nil, "")
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxDownload {
		return nil, nil, fmt.Errorf("%s: too big (%d bytes)", path.Base(resp.Request.URL.Path), resp.ContentLength)
	}
	var buf bytes.Buffer
	chunk := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(chunk)
		buf.Write(chunk[:n])
		if buf.Len() > maxDownload {
			return nil, nil, fmt.Errorf("%s: too big", path.Base(resp.Request.URL.Path))
		}
		if progress != nil && n > 0 {
			progress(int64(buf.Len()), resp.ContentLength)
		}
		if err == io.EOF {
			return buf.Bytes(), resp, nil
		}
		if err != nil {
			return nil, nil, err
		}
	}
}

// gunzipIfNeeded descomprime data si viene en gzip (los de AMP).
func gunzipIfNeeded(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(zr, maxDownload))
}

// isArchive reconoce los comprimidos que no se saben abrir (LHA, LZX), para
// avisar en vez de guardar algo que no suena.
func isArchive(data []byte) string {
	switch {
	case len(data) > 7 && data[2] == '-' && data[3] == 'l' && data[4] == 'h' && data[6] == '-':
		return "LHA"
	case len(data) > 3 && string(data[:3]) == "LZX":
		return "LZX"
	}
	return ""
}

// safeName deja un nombre de archivo sin carpetas ni caracteres que den
// problemas en las tarjetas FAT/exFAT de las portátiles.
func safeName(s string) string {
	s = path.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32, strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(strings.TrimSpace(s), ".")
	if s == "" {
		s = "untitled"
	}
	return s
}

// writeFile escribe data en p (aparte y luego encima, así una descarga
// cortada no deja un archivo a medias que parezca bueno).
func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// unzipPlayable saca de un zip las pistas que se pueden tocar a dir (sin sus
// carpetas: solo el nombre del archivo, así nada sale de dir) y las devuelve
// en orden.
func unzipPlayable(data []byte, dir string) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var out []string
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !f.Mode().IsRegular() || !playableExt(f.Name) {
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxDownload {
			return nil, errors.New("pack too big")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxDownload))
		rc.Close()
		if err != nil {
			return nil, err
		}
		p := filepath.Join(dir, safeName(f.Name))
		if err := writeFile(p, b); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("no playable tracks in the pack")
	}
	sort.Strings(out)
	return out, nil
}

// cached devuelve los archivos que se pueden tocar de dir, si ya están.
func cached(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && playableExt(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// Lo de leer HTML: las páginas se leen con expresiones regulares acotadas a
// cada resultado (sin una biblioteca de HTML, que el repo no tiene).
var (
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
	tdRe    = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
)

// text es el texto visible de un trozo de HTML: sin etiquetas, con las
// entidades resueltas y los espacios juntos.
func text(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// parseSize lee "96Kb", "848.96 KB", "1.2 MB", "178.00KB" a bytes (0 si no se
// entiende).
func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := 1.0
	for _, u := range []struct {
		suf string
		m   float64
	}{{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"K", 1 << 10}, {"M", 1 << 20}, {"B", 1}} {
		if strings.HasSuffix(s, u.suf) {
			s, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suf)), u.m
			break
		}
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		return 0
	}
	return int64(f * mult)
}

// splitBlocks parte s en trozos que empiezan por cada aparición de sep (lo
// de antes de la primera se descarta).
func splitBlocks(s, sep string) []string {
	parts := strings.Split(s, sep)
	if len(parts) < 2 {
		return nil
	}
	return parts[1:]
}

// dedupe quita los repetidos (por fuente e id), conservando el orden.
func dedupe(items []Item) []Item {
	seen := map[string]bool{}
	out := items[:0]
	for _, it := range items {
		k := it.Source + "\x00" + it.ID
		if !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}
