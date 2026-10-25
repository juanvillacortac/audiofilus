package fetch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxCover es lo más que se baja de una portada.
const maxCover = 10 << 20

// Cover es la portada de la ruta p, como archivo: la de la URL art (la que
// mandó quien puso la canción: el albumArtURI de DLNA, la de otro audiofilus
// por MPD) en un ".img" ya bajado, o si no hay o no se pudo bajar, el
// archivo de la canción ya bajado, que la trae incrustada (quien la lea, la
// interfaz, la saca de sus etiquetas). Mientras no hay nada, "": la primera
// vez empieza a bajar art en segundo plano, y no bloquea.
func (f *Fetcher[M]) Cover(p, art string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entries[p]
	if e == nil {
		return ""
	}
	if strings.HasPrefix(art, "http://") || strings.HasPrefix(art, "https://") {
		switch e.art {
		case fetchIdle:
			e.art = fetchLoading
			go f.downloadArt(p, art)
			return ""
		case fetchLoading:
			return ""
		case fetchDone:
			return e.artFile
		}
	}
	if e.state == fetchDone {
		return e.file
	}
	return ""
}

// downloadArt baja la portada art de la ruta p.
func (f *Fetcher[M]) downloadArt(p, art string) {
	file, err := f.getArt(p, art)
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entries[p]
	switch {
	case e == nil:
		if file != "" {
			_ = os.Remove(file) // la olvidaron mientras se bajaba
		}
	case err != nil:
		e.art = fetchFailed
	default:
		e.art, e.artFile = fetchDone, file
	}
}

// getArt baja art a la carpeta de caché.
func (f *Fetcher[M]) getArt(p, art string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", art, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", f.agent)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("cover: http %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCover+1))
	if err != nil {
		return "", err
	}
	if len(b) == 0 || len(b) > maxCover || !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return "", errors.New("cover: not an image")
	}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(p + art))
	file := filepath.Join(f.dir, hex.EncodeToString(sum[:8])+".img")
	return file, os.WriteFile(file, b, 0o644)
}
