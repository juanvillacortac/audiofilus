package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// zipOf arma un zip con esos archivos.
func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(data))
	}
	w.Close()
	return buf.Bytes()
}

// fakeGitHub sirve una release v9.9.9 con un paquete y su SHA256SUMS (el de
// sum, si no está vacío, en vez del verdadero).
func fakeGitHub(t *testing.T, name string, pkg []byte, sum string) {
	if sum == "" {
		h := sha256.Sum256(pkg)
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.9.9", "html_url": srv.URL,
			"assets": []map[string]string{
				{"name": name, "browser_download_url": srv.URL + "/dl/" + name},
				{"name": "SHA256SUMS", "browser_download_url": srv.URL + "/dl/SHA256SUMS"},
			},
		})
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(pkg) })
	mux.HandleFunc("/dl/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  other.zip\n%s  %s\n", sum, sum, name)
	})
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
}

// portDir arma una instalación del port: Audiofilus.sh, y en audiofilus/ el
// binario, port.json y la configuración.
func portDir(t *testing.T) (root, exe string) {
	root = t.TempDir()
	dir := filepath.Join(root, "audiofilus")
	os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	exe = filepath.Join(dir, "audiofilus.aarch64")
	for name, data := range map[string]string{
		"audiofilus.aarch64": "old binary", "port.json": "{}", "audiofilus.json": "my config", "data/history.jsonl": "my history",
	} {
		os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644)
	}
	os.WriteFile(filepath.Join(root, "Audiofilus.sh"), []byte("old launcher"), 0o755)
	return root, exe
}

// Una actualización del port cambia el binario, las libs y el lanzador, deja
// el binario ejecutable y no toca la configuración.
func TestInstallPortMaster(t *testing.T) {
	root, exe := portDir(t)
	pkg := zipOf(t, map[string]string{
		"audiofilus/audiofilus.aarch64":       "new binary",
		"audiofilus/port.json":                "{}",
		"audiofilus/libs.aarch64/libgme.so.0": "lib",
		"audiofilus/audiofilus.json":          "packaged config",
		"Audiofilus.sh":                       "new launcher",
		"gameinfo.xml":                        "<game/>",
	})
	fakeGitHub(t, "audiofilus-portmaster.zip", pkg, "")
	rel, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), rel, exe, nil); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string { b, _ := os.ReadFile(filepath.Join(root, p)); return string(b) }
	if got := read("audiofilus/audiofilus.aarch64"); got != "new binary" {
		t.Errorf("binary = %q", got)
	}
	if got := read("audiofilus/libs.aarch64/libgme.so.0"); got != "lib" {
		t.Errorf("lib = %q", got)
	}
	if got := read("Audiofilus.sh"); got != "new launcher" {
		t.Errorf("launcher = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "gameinfo.xml")); err == nil {
		t.Errorf("gameinfo.xml was created next to the port although it wasn't there")
	}
	if got := read("audiofilus/audiofilus.json"); got != "my config" {
		t.Errorf("config overwritten: %q", got)
	}
	if got := read("audiofilus/data/history.jsonl"); got != "my history" {
		t.Errorf("data touched: %q", got)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(exe); st.Mode()&0o111 == 0 {
			t.Errorf("binary not executable: %v", st.Mode())
		}
	}
}

// Si el paquete no coincide con su SHA256 no se instala nada.
func TestInstallBadChecksum(t *testing.T) {
	root, exe := portDir(t)
	pkg := zipOf(t, map[string]string{"audiofilus/audiofilus.aarch64": "evil"})
	fakeGitHub(t, "audiofilus-portmaster.zip", pkg, "00")
	rel, _ := Latest(context.Background())
	if err := Install(context.Background(), rel, exe, nil); err == nil {
		t.Fatal("installed a package with a wrong checksum")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "audiofilus", "audiofilus.aarch64")); string(b) != "old binary" {
		t.Errorf("binary changed: %q", b)
	}
}

// Un paquete con rutas que salen de la instalación no escribe fuera.
func TestInstallZipSlip(t *testing.T) {
	root, exe := portDir(t)
	pkg := zipOf(t, map[string]string{
		"audiofilus/audiofilus.aarch64": "new binary",
		"audiofilus/../../escape":       "x",
	})
	fakeGitHub(t, "audiofilus-portmaster.zip", pkg, "")
	rel, _ := Latest(context.Background())
	if err := Install(context.Background(), rel, exe, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape")); err == nil {
		t.Fatal("a file was written outside the installation")
	}
}

// El tar.gz de Linux va en la carpeta del ejecutable "audiofilus".
func TestInstallLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "audiofilus")
	os.WriteFile(exe, []byte("old"), 0o755)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	top := "audiofilus-linux-" + runtime.GOARCH + "/"
	for name, data := range map[string]string{top + "audiofilus": "new", top + "licenses/LICENSE": "MIT"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
		tw.Write([]byte(data))
	}
	tw.Close()
	zw.Close()
	fakeGitHub(t, "audiofilus-linux-"+runtime.GOARCH+".tar.gz", buf.Bytes(), "")
	if Detect(exe) != Linux {
		t.Fatalf("Detect = %v, want Linux", Detect(exe))
	}
	rel, _ := Latest(context.Background())
	if err := Install(context.Background(), rel, exe, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("binary = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "licenses", "LICENSE")); string(b) != "MIT" {
		t.Errorf("license = %q", b)
	}
}

// Detect reconoce la app de muOS y una copia suelta (sin actualizaciones).
func TestDetect(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mux_launch.sh"), nil, 0o755)
	if k := Detect(filepath.Join(dir, "audiofilus.aarch64")); k != MuOS {
		t.Errorf("muOS: %v", k)
	}
	if k := Detect(filepath.Join(t.TempDir(), "go-build123")); k != None {
		t.Errorf("loose binary: %v", k)
	}
}

// Newer compara versiones; "dev" nunca se actualiza.
func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, tag string
		want     bool
	}{
		{"v0.9.0", "v0.9.1", true}, {"v0.9.0", "v0.10.0", true}, {"v0.9.0", "v1.0.0", true},
		{"v0.9.1", "v0.9.0", false}, {"v0.9.0", "v0.9.0", false}, {"dev", "v9.9.9", false}, {"v0.9.0", "nightly", false},
	} {
		if got := Newer(c.cur, c.tag); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.tag, got, c.want)
		}
	}
}
