package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/decode"
)

// Picker es el explorador de carpetas para elegir dónde está la música: se
// abre la primera vez y desde Ajustes. Se maneja con la cruceta: A entra, B
// sube, X usa la carpeta actual.
type Picker struct {
	scr     scroller // el desplazamiento con el dedo
	dir     string   // carpeta actual; "" es la lista de lugares
	entries []pickEntry
	sel     int
	songs   int // canciones directamente en dir
}

type pickEntry struct {
	name, path string
}

// pickRoots son los lugares desde donde empezar, si existen: la música del
// usuario en el PC y las tarjetas de las portátiles.
func pickRoots() []pickEntry {
	home, _ := os.UserHomeDir()
	var out []pickEntry
	add := func(name, p string) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			for _, e := range out {
				if e.path == p {
					return
				}
			}
			out = append(out, pickEntry{name, p})
		}
	}
	add("Music", filepath.Join(home, "Music"))
	add("Música", filepath.Join(home, "Música"))
	add("/userdata/MUSIC", "/userdata/MUSIC") // Knulli
	add("/userdata/music", "/userdata/music")
	add("/userdata", "/userdata")
	add("/roms", "/roms")
	add("/storage/roms", "/storage/roms") // ROCKNIX
	add("/roms2", "/roms2")               // ArkOS con dos tarjetas
	add("/mnt/sdcard", "/mnt/sdcard")     // muOS
	add("/mnt/mmc", "/mnt/mmc")
	add("/mnt/SDCARD", "/mnt/SDCARD")
	add("/media", "/media")
	add("/run/media", "/run/media")
	add(home, home)
	add("/", "/")
	return out
}

// newPicker abre el explorador en start, o en la lista de lugares.
func newPicker(start string) *Picker {
	p := &Picker{}
	p.open(start)
	return p
}

// open muestra las subcarpetas de dir ("" = lugares) y cuenta sus canciones.
func (p *Picker) open(dir string) {
	p.dir, p.sel, p.songs = dir, 0, 0
	p.entries = nil
	if dir == "" {
		p.entries = pickRoots()
		return
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var dirs []pickEntry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if de.IsDir() || (de.Type()&os.ModeSymlink != 0 && isDir(full)) {
			dirs = append(dirs, pickEntry{name, full})
		} else if decode.IsAudioFile(name) {
			p.songs++
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	p.entries = dirs
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// up sube a la carpeta de arriba, o a los lugares desde una raíz.
func (p *Picker) up() {
	if p.dir == "" {
		return
	}
	for _, r := range pickRoots() {
		if r.path == p.dir {
			p.open("")
			return
		}
	}
	parent := filepath.Dir(p.dir)
	prev := p.dir
	p.open(parent)
	for i, e := range p.entries {
		if e.path == prev {
			p.sel = i
		}
	}
}

// update atiende los botones; devuelve la carpeta elegida, o "".
func (p *Picker) update(in *Input) string {
	n := len(p.entries)
	switch {
	case in.Repeat(ActDown) && n > 0:
		p.sel = (p.sel + 1) % n
	case in.Repeat(ActUp) && n > 0:
		p.sel = (p.sel - 1 + n) % n
	case in.Pressed(ActConfirm) && n > 0:
		p.open(p.entries[p.sel].path)
	case in.Pressed(ActBack):
		p.up()
	case in.Pressed(ActShuffle) && p.dir != "":
		return p.dir
	}
	return ""
}

// draw pinta el explorador.
func (a *App) drawPicker(dst *ebiten.Image, p *Picker, first bool) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	th := a.theme
	y := topH + 8
	if first {
		drawTextShadow(dst, T("welcome"), BigFace, margin, y, th.Text, th.Shadow)
		y += 18
	}
	use := a.btn("X")
	if a.input.touch {
		use = "\"" + T("h_use") + "\"" // con el dedo, el botón de abajo
	}
	drawText(dst, T("pick_hint", use), SmallFace, margin, y, th.Accent)
	y += 13

	// La ruta, recortada por el principio si no cabe.
	path := T("pick_roots")
	if p.dir != "" {
		path = shortPath(p.dir, func(s string) bool { return textWidth(s, SmallFace) <= w-2*margin-8 })
	}
	panel(dst, margin-4, y-4, w-2*margin+8, 13, withAlpha(th.Bg2, 0xf0))
	drawIcon(dst, "folder", margin, y-1, th.Accent2)
	drawText(dst, path, SmallFace, margin+10, y, th.Text)
	y += 15

	listBottom := h - bottomH - 18
	rowH := 13
	rows := max(1, (listBottom-y)/rowH)
	if len(p.entries) == 0 {
		drawText(dst, T("pick_empty"), SmallFace, margin+10, y+2, th.Dim)
	}
	// Con el dedo: se desplaza y tocar una carpeta la abre.
	first0, off, clip := a.touchList(dst, &p.scr, &p.sel, len(p.entries), margin-3, y-4, w-2*margin+6, rows*rowH, rowH, func() {
		a.input.inject(ActConfirm)
	})
	whole := dst
	dst = clip
	for i := 0; i <= rows && first0+i < len(p.entries); i++ {
		e := p.entries[first0+i]
		ry := y + i*rowH - off
		col := th.Text
		if first0+i == p.sel {
			panel(dst, margin-3, ry-4, w-2*margin+6, rowH, mix(th.Bg2, th.Accent, 0.35))
			rect(dst, margin-3, ry-4, 2, rowH, th.Accent)
		} else {
			col = th.Dim
		}
		drawIcon(dst, "folder", margin+2, ry-1, th.Accent)
		drawTextW(dst, e.name, SmallFace, margin+12, ry, w-2*margin-16, col)
	}
	dst = whole

	// Cuántas canciones hay aquí: ayuda a saber si es la carpeta buena.
	count := T("pick_none")
	if p.songs > 0 {
		count = T("pick_here", p.songs)
	}
	if p.dir != "" {
		drawIcon(dst, "note", margin, listBottom+3, th.Accent2)
		drawText(dst, count, SmallFace, margin+10, listBottom+4, th.Text)
	}
	a.drawTopBar(dst, T("pick_title"))
	hints := []hint{{"A", T("h_open")}, {"B", T("h_up")}}
	if p.dir != "" {
		hints = append(hints, hint{"X", T("h_use")})
	}
	a.drawHints(dst, hints)
}

// shortPath recorta path por el principio ("...", y lo que queda desde una
// "/") hasta que fits diga que cabe o no se pueda recortar más. Cada vuelta
// quita una carpeta: antes podía dejar la misma ruta y no terminar (en iOS,
// con rutas largas y 240 de ancho, la interfaz se colgaba).
func shortPath(path string, fits func(string) bool) string {
	for !fits(path) {
		rest := strings.TrimPrefix(path, "...")
		if len(rest) < 2 {
			break
		}
		i := strings.Index(rest[1:], "/")
		if i < 0 {
			break
		}
		path = "..." + rest[i+1:]
	}
	return path
}
