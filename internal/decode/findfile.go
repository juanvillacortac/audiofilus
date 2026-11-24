package decode

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// FindFile devuelve la ruta en el disco de path: la misma si existe; si no,
// la busca carpeta a carpeta comparando los nombres con SameName. Así se
// encuentra un archivo cuyo nombre no es UTF-8 válido (Latin-1 en un disco
// viejo) después de que su ruta pasara por JSON (el socket, la caché de la
// biblioteca, el historial), que cambia cada byte inválido por U+FFFD; y uno
// guardado en NFD (macOS, algunas tarjetas exFAT) nombrado en NFC por una
// lista .m3u o una hoja CUE, o al revés. Si no lo encuentra, devuelve path.
func FindFile(path string) string {
	if _, err := os.Lstat(path); err == nil || isASCII(path) {
		return path
	}
	vol := filepath.VolumeName(path)
	rest := path[len(vol):]
	cur := vol
	if strings.HasPrefix(rest, string(filepath.Separator)) {
		cur += string(filepath.Separator)
	}
	parts := strings.Split(strings.Trim(rest, string(filepath.Separator)), string(filepath.Separator))
	for _, part := range parts {
		next := filepath.Join(cur, part)
		if _, err := os.Lstat(next); err != nil {
			dir := cur
			if dir == "" {
				dir = "." // una ruta relativa
			}
			ents, err := os.ReadDir(dir)
			if err != nil {
				return path
			}
			found := ""
			for _, e := range ents {
				if SameName(e.Name(), part) {
					found = e.Name()
					break
				}
			}
			if found == "" {
				return path
			}
			next = filepath.Join(cur, found)
		}
		cur = next
	}
	return cur
}

// SameName dice si disk, el nombre de un archivo en el disco, es el que
// busca want aunque no sean iguales byte a byte: con los bytes que no son
// UTF-8 cambiados por U+FFFD (como hace JSON) o con otra normalización
// Unicode (NFC frente a NFD).
func SameName(disk, want string) bool {
	if disk == want {
		return true
	}
	if !utf8.ValidString(disk) && jsonSafe(disk) == want {
		return true
	}
	return norm.NFC.String(disk) == norm.NFC.String(want)
}

// jsonSafe es s con cada byte que no es UTF-8 válido cambiado por U+FFFD,
// lo mismo que hace encoding/json al escribirlo.
func jsonSafe(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// isASCII dice si s solo tiene caracteres ASCII: entonces no hay nada que
// FindFile pueda arreglar.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
