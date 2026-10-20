// Package update busca versiones nuevas de audiofilus en las releases de
// GitHub y las instala encima de la que corre: baja el paquete que le toca a
// esta instalación (el port de PortMaster, la app de muOS, el tar.gz de Linux
// o el zip de Windows), comprueba su SHA256 contra el SHA256SUMS de la
// release y cambia los archivos uno a uno (cada uno se escribe aparte y se
// renombra encima, así un corte no deja nada a medias). La configuración
// (audiofilus.json, data/) no viene en los paquetes y no se toca.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo es el repositorio de las releases.
const Repo = "juanvillacortac/audiofilus"

// apiBase es la API de GitHub (los tests la cambian por un servidor local).
var apiBase = "https://api.github.com"

// client es el cliente HTTP: con un límite para la consulta, no para la
// descarga (que puede ser lenta en la wifi de una portátil).
var client = &http.Client{Timeout: 10 * time.Minute}

// Release es una versión publicada.
type Release struct {
	Tag    string            // "v0.9.0"
	Notes  string            // las notas, en Markdown
	URL    string            // su página
	Assets map[string]string // nombre → dirección de descarga
}

// Latest es la última release publicada.
func Latest(ctx context.Context) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	rel := &Release{Tag: r.Tag, Notes: r.Body, URL: r.URL, Assets: map[string]string{}}
	for _, a := range r.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// parse lee "v1.2.3" (o "1.2.3"); ok es false si no es una versión.
func parse(v string) (n [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// Newer dice si tag es más nueva que current. Con current que no es una
// versión ("dev", una compilación a mano), nunca.
func Newer(current, tag string) bool {
	c, ok1 := parse(current)
	t, ok2 := parse(tag)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if t[i] != c[i] {
			return t[i] > c[i]
		}
	}
	return false
}

// Kind es cómo está instalado audiofilus, es decir, qué paquete le toca.
type Kind int

const (
	None       Kind = iota // no se sabe (go run, una copia suelta): sin actualizaciones
	PortMaster             // el port: audiofilus-portmaster.zip
	MuOS                   // la app de muOS: audiofilus.muxapp
	Linux                  // el escritorio Linux: audiofilus-linux-<arch>.tar.gz
	Windows                // audiofilus-windows-amd64.zip
)

// Detect mira la carpeta del ejecutable para saber cómo se instaló: la app de
// muOS tiene mux_launch.sh, el port de PortMaster port.json; si no, el
// escritorio, si el ejecutable se llama como en su paquete.
func Detect(exe string) Kind {
	dir, base := filepath.Dir(exe), filepath.Base(exe)
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	switch {
	case exists("mux_launch.sh"):
		return MuOS
	case exists("port.json"):
		return PortMaster
	case runtime.GOOS == "windows" && strings.EqualFold(base, "audiofilus.exe"):
		return Windows
	case runtime.GOOS == "linux" && base == "audiofilus":
		return Linux
	}
	return None
}

// Asset es el nombre del paquete que le toca a k.
func Asset(k Kind) string {
	switch k {
	case PortMaster:
		return "audiofilus-portmaster.zip"
	case MuOS:
		return "audiofilus.muxapp"
	case Linux:
		return "audiofilus-linux-" + runtime.GOARCH + ".tar.gz"
	case Windows:
		return "audiofilus-windows-amd64.zip"
	}
	return ""
}

// prefix es la carpeta del paquete cuyo contenido va en la del ejecutable.
func prefix(k Kind) string {
	switch k {
	case PortMaster, MuOS:
		return "audiofilus/"
	case Linux:
		return "audiofilus-linux-" + runtime.GOARCH + "/"
	}
	return ""
}

// Progress recibe cuánto se lleva bajado del paquete (total puede ser -1).
type Progress func(done, total int64)

// Install baja e instala rel en la carpeta del ejecutable exe.
func Install(ctx context.Context, rel *Release, exe string, progress Progress) error {
	k := Detect(exe)
	name := Asset(k)
	if name == "" {
		return errors.New("this installation can't update itself")
	}
	url, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("%s: not in release %s", name, rel.Tag)
	}
	sumsURL, ok := rel.Assets["SHA256SUMS"]
	if !ok {
		return fmt.Errorf("SHA256SUMS: not in release %s", rel.Tag)
	}
	sums, err := download(ctx, sumsURL, nil)
	if err != nil {
		return err
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}
	data, err := download(ctx, url, progress)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s: checksum mismatch", name)
	}
	files, err := extract(k, data)
	if err != nil {
		return err
	}
	return place(k, filepath.Dir(exe), files)
}

// download baja url entera a memoria (los paquetes son de unos 20 MB).
func download(ctx context.Context, url string, progress Progress) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", path.Base(url), resp.Status)
	}
	var buf bytes.Buffer
	chunk := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(chunk)
		buf.Write(chunk[:n])
		if progress != nil {
			progress(int64(buf.Len()), resp.ContentLength)
		}
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// sumFor busca el SHA256 de name en un SHA256SUMS ("<hash>  <nombre>").
func sumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s: no checksum in SHA256SUMS", name)
}

// file es un archivo del paquete: su ruta dentro de la instalación y su
// contenido.
type file struct {
	path string // relativa a la carpeta del ejecutable ("../Audiofilus.sh" para el lanzador del port)
	data []byte
	exec bool
}

// extract saca del paquete los archivos que van en la instalación.
func extract(k Kind, data []byte) ([]file, error) {
	var out []file
	add := func(name string, mode os.FileMode, r io.Reader) error {
		rel, ok := strings.CutPrefix(name, prefix(k))
		switch {
		case k == PortMaster && (name == "Audiofilus.sh" || name == "gameinfo.xml"):
			rel = "../" + name // junto a la carpeta del port (lo único fuera de ella)
		case !ok, rel == "", strings.HasSuffix(rel, "/"):
			return nil
		case strings.Contains(rel, ".."), path.IsAbs(rel):
			return nil // un paquete que quiere escribir fuera de la instalación
		case rel == "audiofilus.json", strings.HasPrefix(rel, "data/"):
			return nil // la configuración no viene en los paquetes, pero por si acaso
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		out = append(out, file{rel, b, mode&0o111 != 0 || isExec(rel)})
		return nil
	}
	if k == Linux {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(zr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag == tar.TypeReg {
				if err := add(h.Name, os.FileMode(h.Mode), tr); err != nil {
					return nil, err
				}
			}
		}
	} else {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = add(f.Name, f.Mode(), rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the package is empty")
	}
	return out, nil
}

// isExec dice si un archivo del paquete se ejecuta (los zip pueden no
// guardar el permiso).
func isExec(rel string) bool {
	base := path.Base(rel)
	return strings.HasSuffix(base, ".aarch64") || strings.HasSuffix(base, ".x86_64") ||
		strings.HasSuffix(base, ".sh") || base == "audiofilus" || strings.HasSuffix(base, ".exe")
}

// place escribe los archivos en dir: cada uno aparte (.new) y luego encima.
// En Windows el ejecutable en uso no se puede pisar, pero sí renombrar: el
// viejo queda como .old y se borra en el siguiente arranque (Cleanup). El
// lanzador del port y gameinfo.xml solo se cambian si ya estaban.
func place(k Kind, dir string, files []file) error {
	for _, f := range files {
		dst := filepath.Join(dir, filepath.FromSlash(f.path))
		if strings.HasPrefix(f.path, "../") {
			if _, err := os.Stat(dst); err != nil {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.exec {
			mode = 0o755
		}
		tmp := dst + ".new"
		if err := os.WriteFile(tmp, f.data, mode); err != nil {
			return err
		}
		if k == Windows {
			if _, err := os.Stat(dst); err == nil {
				_ = os.Remove(dst + ".old")
				if err := os.Rename(dst, dst+".old"); err != nil {
					os.Remove(tmp)
					return err
				}
			}
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return nil
}

// Cleanup borra lo que dejó una actualización en Windows (los .old).
func Cleanup(exe string) {
	if runtime.GOOS != "windows" {
		return
	}
	olds, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), "*.old"))
	for _, o := range olds {
		_ = os.Remove(o)
	}
}
