package ytm

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// unpackZip desempaqueta el yt-dlp en zip (yt-dlp_linux_aarch64.zip: el
// ejecutable y su carpeta _internal) que el usuario dejó junto a audiofilus
// (en base o en base/yt-dlp/) en base/yt-dlp/, si ahí no hay ya uno. Así
// arranca en ~3 s en vez de ~11, y no se desempaqueta en /tmp (en la RAM de
// las portátiles) cada vez que corre, como el de un solo archivo. El zip se
// borra al terminar bien.
func unpackZip(base string) {
	dest := filepath.Join(base, "yt-dlp")
	if unpacked(dest) != "" {
		return
	}
	var zips []string
	for _, dir := range []string{base, dest} {
		m, _ := filepath.Glob(filepath.Join(dir, "yt-dlp*.zip"))
		zips = append(zips, m...)
	}
	for _, z := range zips {
		if err := extract(z, dest); err != nil {
			log.Printf("yt-dlp: unpacking %s: %v", filepath.Base(z), err)
			continue
		}
		log.Printf("yt-dlp: unpacked %s in %s", filepath.Base(z), dest)
		_ = os.Remove(z)
		return
	}
}

// unpacked es el ejecutable de un yt-dlp desempaquetado en dir ("" si no
// hay).
func unpacked(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "yt-dlp") && !strings.HasSuffix(e.Name(), ".zip") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// extract desempaqueta zipPath en dest. Si el zip trae una carpeta de arriba
// (yt-dlp_linux_aarch64/...), se le quita. Los enlaces se copian como el
// archivo al que apuntan (exFAT no tiene enlaces), y nada sale de dest.
func extract(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	strip := commonRoot(r.File)
	tmp := dest + ".unpacking"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	links := map[string]string{} // ruta → a dónde apunta (dentro del zip)
	for _, f := range r.File {
		name := strings.TrimPrefix(path.Clean(f.Name), strip)
		if name == "" || name == "." {
			continue
		}
		if !fs.ValidPath(name) {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("unsafe path %q", f.Name)
		}
		out := filepath.Join(tmp, filepath.FromSlash(name))
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
		case f.Mode()&fs.ModeSymlink != 0:
			target, err := readAll(f)
			if err != nil {
				return err
			}
			links[name] = path.Join(path.Dir(name), string(target))
		default:
			if err := writeEntry(f, out); err != nil {
				_ = os.RemoveAll(tmp)
				return err
			}
		}
	}
	for name, target := range links {
		if !fs.ValidPath(target) {
			continue
		}
		in, err := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(target)))
		if err != nil {
			continue // apunta a algo que no está: se ignora
		}
		out := filepath.Join(tmp, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		if err := os.WriteFile(out, in, 0o755); err != nil {
			return err
		}
	}
	if unpacked(tmp) == "" {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("no yt-dlp executable in the zip")
	}
	// En su sitio: lo que hubiera en dest (cookies.txt, el zip) se conserva.
	entries, _ := os.ReadDir(tmp)
	_ = os.MkdirAll(dest, 0o755)
	for _, e := range entries {
		to := filepath.Join(dest, e.Name())
		_ = os.RemoveAll(to)
		if err := os.Rename(filepath.Join(tmp, e.Name()), to); err != nil {
			return err
		}
	}
	return os.RemoveAll(tmp)
}

// commonRoot es la carpeta de arriba que comparten todos los archivos del
// zip, con su barra ("" si alguno está suelto arriba o hay varias).
func commonRoot(files []*zip.File) string {
	root := ""
	for _, f := range files {
		name := path.Clean(f.Name)
		first, _, inside := strings.Cut(name, "/")
		if !inside && !f.FileInfo().IsDir() {
			return "" // un archivo suelto arriba
		}
		if root != "" && first != root {
			return ""
		}
		root = first
	}
	if root == "" {
		return ""
	}
	return root + "/"
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 4096))
}

// writeEntry escribe un archivo del zip en out, ejecutable si es el yt-dlp o
// si el zip lo marca así.
func writeEntry(f *zip.File, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := os.FileMode(0o644)
	if f.Mode()&0o111 != 0 || strings.HasPrefix(filepath.Base(out), "yt-dlp") {
		mode = 0o755
	}
	w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
