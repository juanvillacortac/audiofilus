package ui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/tracker"
)

// Importar y exportar canciones del tracker en otros formatos: los módulos
// MOD y XM (de ProTracker, FastTracker y compañía) y las canciones de LSDJ
// (.lsdsng, o todas las de un .sav). Para importar se buscan esos archivos en
// las carpetas de música y en data/tracker/import/; la canción importada se
// guarda como una más. Exportar deja el .xm o el .mod junto a los WAV, en
// "audiofilus tracker/" de la carpeta de música (y la biblioteca los toca,
// con libxmp).

// importExts son las extensiones que se pueden importar.
var importExts = map[string]bool{".mod": true, ".xm": true, ".lsdsng": true, ".sav": true}

// importMax es cuántos archivos se listan como mucho (las carpetas de música
// pueden ser enormes).
const importMax = 300

// importFound es lo que vuelve de buscar archivos para importar.
type importFound struct{ files []string }

// importDone es lo que vuelve de importar un archivo.
type importDone struct {
	songs []*tracker.Song
	file  string
	err   error
}

// trackerImportDir es la carpeta propia para dejar archivos que importar.
func (a *App) trackerImportDir() string { return filepath.Join(a.trackerDir(), "import") }

// openTrackerImport busca los archivos que se pueden importar (en segundo
// plano: recorre las carpetas de música) y luego los ofrece en un menú.
func (a *App) openTrackerImport() {
	t := a.tracker
	a.flash(T("tk_searching"))
	dirs := append([]string{a.trackerImportDir()}, a.cfg.MusicDirs...)
	go func() {
		var out []string
		for _, d := range dirs {
			_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
				switch {
				case err != nil:
					return nil // una carpeta que no se puede leer: se sigue con las demás
				case len(out) >= importMax:
					return fs.SkipAll
				}
				if e.IsDir() && strings.HasPrefix(e.Name(), ".") && p != d {
					return filepath.SkipDir
				}
				if !e.IsDir() && importExts[strings.ToLower(filepath.Ext(p))] {
					out = append(out, p)
				}
				return nil
			})
		}
		sort.Slice(out, func(i, j int) bool {
			return strings.ToLower(filepath.Base(out[i])) < strings.ToLower(filepath.Base(out[j]))
		})
		t.files <- importFound{out}
		ebiten.ScheduleFrame()
	}()
}

// importMenu ofrece los archivos encontrados.
func (a *App) importMenu(files []string) {
	if len(files) == 0 {
		a.flash(T("tk_import_none"))
		return
	}
	items := make([]menuItem, 0, len(files))
	for _, f := range files {
		items = append(items, menuItem{label: filepath.Base(f), icon: "note", do: func() { a.importTracker(f) }})
	}
	a.menu = &menu{title: T("tk_import"), subtitle: fmt.Sprintf("%d · MOD XM LSDJ", len(files)), items: items}
}

// importTracker importa un archivo en segundo plano.
func (a *App) importTracker(file string) {
	t := a.tracker
	a.flash(T("tk_importing"))
	go func() {
		r := importDone{file: file}
		data, err := os.ReadFile(file)
		if err == nil {
			r.songs, err = importSongs(file, data)
		}
		r.err = err
		t.imported <- r
		ebiten.ScheduleFrame()
	}()
}

// importSongs lee las canciones de un archivo según su extensión.
func importSongs(file string, data []byte) ([]*tracker.Song, error) {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	one := func(s *tracker.Song, err error) ([]*tracker.Song, error) {
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s.Name) == "" {
			s.Name = base
		}
		return []*tracker.Song{s}, nil
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".mod":
		return one(tracker.ImportMOD(data))
	case ".xm":
		return one(tracker.ImportXM(data))
	case ".lsdsng":
		return one(tracker.ImportLSDSNG(data))
	case ".sav":
		return tracker.ImportLSDJSav(data)
	}
	return nil, fmt.Errorf("%s: unknown format", filepath.Base(file))
}

// pollTrackerFiles atiende lo que vuelve de buscar e importar.
func (a *App) pollTrackerFiles() {
	t := a.tracker
	select {
	case f := <-t.files:
		a.importMenu(f.files)
	case r := <-t.imported:
		if r.err != nil {
			a.flash(T("tk_import_failed", r.err))
			return
		}
		// Cada canción se guarda como una nueva; se abre la primera.
		stamp := time.Now().Unix()
		for i := len(r.songs) - 1; i >= 0; i-- {
			s := r.songs[i]
			s.Fix()
			a.openTrackerSong(s, fmt.Sprintf("song-%d-%d.json", stamp, i), true)
		}
		if len(r.songs) == 1 {
			a.flash(T("tk_imported", r.songs[0].Name))
		} else {
			a.flash(T("tk_imported_n", len(r.songs)))
		}
	default:
	}
}

// exportModule pasa la canción a XM o MOD en la carpeta de música, como el
// WAV.
func (a *App) exportModule(ext string) {
	t := a.tracker
	if len(a.cfg.MusicDirs) == 0 {
		a.flash(T("tk_no_folder"))
		return
	}
	song := t.song.Clone()
	dir := filepath.Join(a.cfg.MusicDirs[0], "audiofilus tracker")
	name := t.song.Name + " " + time.Now().Format("2006-01-02 1504") + ext
	a.flash(T("tk_exporting"))
	go func() {
		var data []byte
		var err error
		if ext == ".mod" {
			data, err = tracker.ExportMOD(song)
		} else {
			data, err = tracker.ExportXM(song)
		}
		if err == nil {
			err = os.MkdirAll(dir, 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), data, 0o644)
		}
		msg := T("tk_exported", name)
		if err != nil {
			msg = T("tk_export_failed", err)
		}
		t.exported <- msg
		ebiten.ScheduleFrame()
	}()
}
