package ytm

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// zipEntry es un archivo de un zip de prueba: su contenido o, si link, a
// dónde apunta.
type zipEntry struct {
	name, data string
	link       bool
}

// makeZip escribe un zip con esas entradas.
func makeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store}
		if e.link {
			h.SetMode(fs.ModeSymlink | 0o777)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(e.data))
	}
	w.Close()
	f.Close()
}

// Find desempaqueta el zip de yt-dlp (plano como el de la release, o con una
// carpeta arriba) en yt-dlp/, ejecutable, con los enlaces como copias, y
// borra el zip.
func TestUnpackZip(t *testing.T) {
	for _, prefix := range []string{"", "yt-dlp_linux_aarch64/"} {
		base := t.TempDir()
		makeZip(t, filepath.Join(base, "yt-dlp_linux_aarch64.zip"), []zipEntry{
			{name: prefix + "yt-dlp_linux_aarch64", data: "#!/bin/sh\n"},
			{name: prefix + "_internal/libpython3.so.1.0", data: "lib"},
			{name: prefix + "_internal/libpython3.so", data: "libpython3.so.1.0", link: true},
		})
		bin := Find(base)
		if want := filepath.Join(base, "yt-dlp", "yt-dlp_linux_aarch64"); bin != want {
			t.Fatalf("prefix %q: Find = %q, want %q", prefix, bin, want)
		}
		if fi, err := os.Stat(bin); err != nil || runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
			t.Errorf("prefix %q: not executable (%v)", prefix, err)
		}
		if b, _ := os.ReadFile(filepath.Join(base, "yt-dlp", "_internal", "libpython3.so")); string(b) != "lib" {
			t.Errorf("prefix %q: link not copied: %q", prefix, b)
		}
		if _, err := os.Stat(filepath.Join(base, "yt-dlp_linux_aarch64.zip")); err == nil {
			t.Errorf("prefix %q: the zip was kept", prefix)
		}
	}
}

// Un zip con rutas que salen de la carpeta no escribe nada fuera.
func TestUnpackZipSlip(t *testing.T) {
	base := t.TempDir()
	makeZip(t, filepath.Join(base, "yt-dlp.zip"), []zipEntry{
		{name: "yt-dlp_linux_aarch64", data: "x"},
		{name: "../evil", data: "x"},
	})
	Find(base)
	if _, err := os.Stat(filepath.Join(filepath.Dir(base), "evil")); err == nil {
		t.Error("wrote outside the folder")
	}
	if unpacked(filepath.Join(base, "yt-dlp")) != "" {
		t.Error("an unsafe zip was unpacked")
	}
}
