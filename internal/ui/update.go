package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/proc"
	"github.com/juanvillacortac/audiofilus/internal/update"
	"github.com/juanvillacortac/audiofilus/internal/version"
)

// Las actualizaciones: al abrir la app (como mucho una vez al día) se mira en
// las releases de GitHub si hay una versión nueva; si la hay, se avisa una
// vez, y en Ajustes → Actualizar se instala con A: baja el paquete de esta
// instalación, comprueba su SHA256, cambia los archivos y reinicia el
// servicio y la interfaz (la música sigue donde iba). En el móvil no: allí
// actualiza AltStore o la tienda.

// updateState es lo que se sabe de las actualizaciones.
type updateState struct {
	exe      string
	kind     update.Kind
	started  bool
	checking bool
	latest   *update.Release // la última release (si ya se miró)
	err      string
	done     chan updateResult
	// La instalación: bajando (done/total en bytes, desde la goroutine).
	installing     bool
	dlDone, dlSize atomic.Int64
}

// updateResult es lo que devuelve la goroutine: una release mirada o una
// instalación terminada.
type updateResult struct {
	rel       *update.Release
	installed bool
	err       error
}

// updateFile guarda cuándo se miró por última vez y de qué versión ya se
// avisó (data/update.json).
type updateFile struct {
	Checked  time.Time `json:"checked"`
	Notified string    `json:"notified,omitempty"`
}

func (a *App) updatePath() string { return filepath.Join(a.paths.Data, "update.json") }

func (a *App) loadUpdateFile() updateFile {
	var f updateFile
	if b, err := os.ReadFile(a.updatePath()); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

func (a *App) saveUpdateFile(f updateFile) {
	if b, err := json.Marshal(f); err == nil {
		_ = os.MkdirAll(a.paths.Data, 0o755)
		_ = os.WriteFile(a.updatePath(), b, 0o644)
	}
}

// canUpdate dice si esta instalación se actualiza sola: no en el móvil, ni
// sin versión (go run, compilaciones a mano), ni si no se sabe cómo se
// instaló.
func (a *App) canUpdate() bool {
	u := &a.upd
	if !u.started {
		u.started = true
		u.done = make(chan updateResult, 1)
		u.exe, _ = proc.Executable()
		u.kind = update.Detect(u.exe)
		update.Cleanup(u.exe)
	}
	return !a.mobile() && !a.offline && u.kind != update.None && version.Version != "dev"
}

// checkUpdate mira la última release en segundo plano.
func (a *App) checkUpdate() {
	u := &a.upd
	if u.checking || u.installing {
		return
	}
	u.checking, u.err = true, ""
	go func(done chan updateResult) {
		rel, err := update.Latest(context.Background())
		done <- updateResult{rel: rel, err: err}
		ebiten.ScheduleFrame()
	}(u.done)
}

// pollUpdate atiende las actualizaciones en cada fotograma: la consulta del
// día al arrancar (pasado el splash) y lo que vuelve de las goroutines.
func (a *App) pollUpdate() {
	if a.splashing || !a.canUpdate() {
		return
	}
	u := &a.upd
	if u.latest == nil && !u.checking && u.err == "" {
		if f := a.loadUpdateFile(); time.Since(f.Checked) > 24*time.Hour {
			a.checkUpdate()
		}
	}
	select {
	case r := <-u.done:
		u.checking = false
		if r.installed {
			a.finishUpdate(r.err)
			return
		}
		if r.err != nil {
			u.err = r.err.Error()
			log.Printf("update check: %v", r.err)
			return
		}
		u.latest = r.rel
		f := a.loadUpdateFile()
		f.Checked = time.Now()
		if update.Newer(version.Version, r.rel.Tag) && f.Notified != r.rel.Tag {
			f.Notified = r.rel.Tag
			a.flash(T("upd_available", r.rel.Tag))
		}
		a.saveUpdateFile(f)
	default:
	}
}

// updateAvailable dice si hay una versión nueva ya mirada.
func (a *App) updateAvailable() bool {
	return a.upd.latest != nil && update.Newer(version.Version, a.upd.latest.Tag)
}

// pressUpdate es A en la baldosa: mira si no se sabe nada; si hay versión
// nueva, la instala.
func (a *App) pressUpdate() {
	u := &a.upd
	switch {
	case !a.canUpdate() || u.checking || u.installing:
	case a.updateAvailable():
		u.installing = true
		u.dlDone.Store(0)
		u.dlSize.Store(-1)
		rel, exe := u.latest, u.exe
		go func(done chan updateResult) {
			err := update.Install(context.Background(), rel, exe, func(d, t int64) {
				u.dlDone.Store(d)
				u.dlSize.Store(t)
				ebiten.ScheduleFrame()
			})
			done <- updateResult{installed: true, err: err}
			ebiten.ScheduleFrame()
		}(u.done)
	default:
		a.checkUpdate()
	}
}

// finishUpdate cierra una instalación: con error, avisa; si fue bien,
// reinicia el servicio (que sigue donde iba) y la interfaz, ya con el
// binario nuevo.
func (a *App) finishUpdate(err error) {
	u := &a.upd
	u.installing = false
	if err != nil {
		log.Printf("update install: %v", err)
		u.err = err.Error()
		a.flash(T("upd_failed", err))
		return
	}
	log.Printf("updated to %s; restarting", u.latest.Tag)
	a.send("restart")
	time.Sleep(300 * time.Millisecond) // que el servicio reciba el pedido antes de irnos
	if err := proc.Reexec(); err != nil {
		u.err = err.Error()
		a.flash(T("upd_restart", u.latest.Tag))
	}
}

// updateValue es lo que dice la baldosa.
func (a *App) updateValue() string {
	u := &a.upd
	switch {
	case !a.canUpdate():
		return version.Version
	case u.installing:
		if t := u.dlSize.Load(); t > 0 {
			if d := u.dlDone.Load(); d < t {
				return T("upd_downloading", int(d*100/t))
			}
			return T("upd_installing")
		}
		return T("upd_downloading", 0)
	case u.checking:
		return T("upd_checking")
	case a.updateAvailable():
		return T("upd_install", u.latest.Tag)
	case u.err != "":
		return T("upd_error")
	case u.latest != nil:
		return T("upd_current", version.Version)
	}
	return fmt.Sprintf("%s · %s", version.Version, T("upd_check"))
}
