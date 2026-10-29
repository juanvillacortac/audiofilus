package chip

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Un .sid no dice cuánto dura cada canción. La HVSC trae la lista en
// DOCUMENTS/Songlengths.md5 (el MD5 de cada archivo y lo que dura cada una
// de sus canciones); si la colección está entera, se busca hacia arriba
// desde la carpeta del archivo. Sin ella, cada canción dura sidDefault.

// songlengthDBs guarda cada lista ya leída (por su ruta; nil si no está).
var songlengthDBs sync.Map

// songlengthDepth es cuántas carpetas se sube buscando la lista (en la HVSC,
// MUSICIANS/H/Hubbard_Rob está a tres de la raíz).
const songlengthDepth = 6

// songLengths es lo que duran las canciones del .sid de path con
// contenido b, o nil si no se sabe.
func songLengths(path string, b []byte) []time.Duration {
	dir := filepath.Dir(path)
	for i := 0; i < songlengthDepth; i++ {
		db := songlengthDB(filepath.Join(dir, "DOCUMENTS", "Songlengths.md5"))
		if db != nil {
			sum := md5.Sum(b)
			return db[hex.EncodeToString(sum[:])]
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return nil
}

// songlengthDB lee (una vez) la lista de path.
func songlengthDB(path string) map[string][]time.Duration {
	if v, ok := songlengthDBs.Load(path); ok {
		return v.(map[string][]time.Duration)
	}
	var db map[string][]time.Duration
	if f, err := os.Open(path); err == nil {
		db = parseSonglengths(f)
		f.Close()
	}
	songlengthDBs.Store(path, db)
	return db
}

// parseSonglengths lee las líneas "md5=m:ss m:ss.sss ..." (lo demás son
// comentarios o secciones).
func parseSonglengths(f *os.File) map[string][]time.Duration {
	db := map[string][]time.Duration{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok || len(key) != 32 {
			continue
		}
		var ls []time.Duration
		for _, f := range strings.Fields(val) {
			ls = append(ls, parseLength(f))
		}
		db[strings.ToLower(key)] = ls
	}
	return db
}

// parseLength lee "m:ss" o "m:ss.sss" (con, quizá, una marca entre
// paréntesis detrás); 0 si no se entiende.
func parseLength(s string) time.Duration {
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	m, sec, ok := strings.Cut(s, ":")
	if !ok {
		return 0
	}
	mi, err1 := strconv.Atoi(m)
	se, err2 := strconv.ParseFloat(sec, 64)
	if err1 != nil || err2 != nil {
		return 0
	}
	return time.Duration(mi)*time.Minute + time.Duration(se*float64(time.Second))
}
