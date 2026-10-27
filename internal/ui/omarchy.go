package ui

import (
	"bufio"
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// omarchyColors es el archivo donde Omarchy deja los colores del tema activo;
// `omarchy theme set` lo reemplaza al cambiar de tema. Solo existe en Omarchy,
// así que en las consolas, Windows y el móvil la opción Sistema no aparece.
func omarchyColors() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local/state/omarchy/current/theme/colors.toml")
}

// sysEvery es cada cuánto se vuelve a leer el tema del sistema, para seguir
// los cambios de tema sin reiniciar (el archivo es pequeño).
const sysEvery = time.Second

// sysCache guarda el último tema del sistema leído. Solo se usa desde el hilo
// de Update, así que no lleva cerrojo.
var sysCache struct {
	checked time.Time
	raw     []byte
	theme   Theme
	ok      bool
}

// systemTheme es el tema de Omarchy, si lo hay, releído como mucho una vez por
// segundo.
func systemTheme() (Theme, bool) {
	now := time.Now()
	if !sysCache.checked.IsZero() && now.Sub(sysCache.checked) < sysEvery {
		return sysCache.theme, sysCache.ok
	}
	sysCache.checked = now
	path := omarchyColors()
	raw, err := os.ReadFile(path)
	if path == "" || err != nil {
		sysCache.raw, sysCache.ok = nil, false
		return Theme{}, false
	}
	if !bytes.Equal(raw, sysCache.raw) {
		sysCache.raw = raw
		sysCache.theme, sysCache.ok = omarchyTheme(parseColors(raw))
	}
	return sysCache.theme, sysCache.ok
}

// parseColors lee las líneas `clave = "#rrggbb"` de un colors.toml de Omarchy;
// lo demás se ignora.
func parseColors(raw []byte) map[string]color.RGBA {
	m := map[string]color.RGBA{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.TrimPrefix(strings.Trim(strings.TrimSpace(v), `"'`), "#")
		n, err := strconv.ParseUint(v, 16, 32)
		if len(v) != 6 || err != nil {
			continue
		}
		m[strings.TrimSpace(k)] = color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 0xff}
	}
	return m
}

// omarchyTheme convierte los colores de un tema de Omarchy en el de la
// interfaz: fondo, texto y acento, claros u oscuros, así se parece a la
// terminal y a la barra.
func omarchyTheme(m map[string]color.RGBA) (Theme, bool) {
	bg, okBg := m["background"]
	fg, okFg := m["foreground"]
	accent, ok := m["accent"]
	if !ok {
		accent, ok = m["color12"]
	}
	if !ok || !okBg || !okFg {
		return Theme{}, false
	}
	accent2 := secondTone(m, accent, fg)
	shadow := mix(bg, color.RGBA{A: 0xff}, 0.6)
	if luma(bg) > 0.5 {
		// En un tema claro la "sombra" (barras, paneles, sombras del texto)
		// es un poco más oscura que el fondo, no negra: el texto oscuro se
		// sigue leyendo encima.
		shadow = mix(bg, fg, 0.15)
	}
	return Theme{
		Bg:      bg,
		Bg2:     mix(bg, fg, 0.08),
		Text:    fg,
		Dim:     mix(fg, bg, 0.4),
		Accent:  accent,
		Accent2: accent2,
		Shadow:  shadow,
	}, true
}

// secondTone elige el segundo tono entre los colores vivos de la paleta
// (color9 a color14): el de tono más lejano al acento. Sin ninguno vivo, una
// mezcla del acento con el texto.
func secondTone(m map[string]color.RGBA, accent, fg color.RGBA) color.RGBA {
	best, dist := mix(accent, fg, 0.5), -1.0
	for i := 9; i <= 14; i++ {
		c, ok := m["color"+strconv.Itoa(i)]
		if !ok {
			continue
		}
		_, s, v := hsv(c)
		if s < 0.25 || v < 0.35 {
			continue
		}
		if d := hueDist(hue(c), hue(accent)); d > dist {
			best, dist = c, d
		}
	}
	return best
}

// luma es el brillo percibido de un color (0..1).
func luma(c color.RGBA) float64 {
	return (0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)) / 255
}
