// Package tray pone audiofilus en la bandeja del sistema del escritorio: el
// icono del CD del logo; clic, abrir la interfaz; clic central, pausar o
// seguir; y un menú con lo que suena, reproducir/pausar, siguiente,
// anterior, aleatorio, abrir y salir. Vive en el servicio, así sigue ahí con
// la ventana cerrada y la música en segundo plano.
//
// En Linux, con el estándar StatusNotifierItem por D-Bus (la barra de Omarchy,
// Waybar, KDE, GNOME con AppIndicator; tray_linux.go); en Windows, en el área
// de notificación (Shell_NotifyIcon; tray_windows.go). Lo común está aquí.
package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/juanvillacortac/audiofilus/internal/desktop"
	"github.com/juanvillacortac/audiofilus/internal/engine"
)

// Options es lo que la bandeja necesita del servicio.
type Options struct {
	Home   string      // AUDIOFILUS_HOME, para abrir la interfaz con la misma carpeta
	UIOpen func() bool // si hay una interfaz conectada
	Quit   func()      // salir del todo (como "Salir" en la interfaz)
	Lang   string      // "es" o "en"
}

// Los elementos del menú, en orden.
const (
	idNow = iota + 1
	idToggle
	idNext
	idPrev
	idShuffle
	idSep1
	idOpen
	idSep2
	idQuit
	idLast = idQuit
)

// base es lo común a las bandejas de cada sistema.
type base struct {
	eng *engine.Engine
	opt Options
}

// nowPlaying es "Artista — Título" (o el nombre del archivo), con "(en
// pausa)" si lo está.
func (b *base) nowPlaying() string {
	st := b.eng.Status()
	if st.Track.Path == "" {
		return b.tr("Nada sonando", "Nothing playing")
	}
	s := st.Track.Title
	if s == "" {
		s = st.Track.Path[strings.LastIndexAny(st.Track.Path, `/\`)+1:]
	}
	if st.Track.Artist != "" {
		s = st.Track.Artist + " — " + s
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	if st.State != "playing" {
		s += b.tr(" (en pausa)", " (paused)")
	}
	return s
}

// tr elige el texto del idioma de la interfaz.
func (b *base) tr(es, en string) string {
	if b.opt.Lang == "en" || b.opt.Lang == "" && !strings.HasPrefix(os.Getenv("LANG"), "es") {
		return en
	}
	return es
}

// label es el texto de un elemento del menú ("" en los separadores).
func (b *base) label(id int) string {
	switch id {
	case idNow:
		return b.nowPlaying()
	case idToggle:
		if b.eng.Status().State == "playing" {
			return b.tr("Pausar", "Pause")
		}
		return b.tr("Reproducir", "Play")
	case idNext:
		return b.tr("Siguiente", "Next")
	case idPrev:
		return b.tr("Anterior", "Previous")
	case idShuffle:
		return b.tr("Aleatorio", "Shuffle")
	case idOpen:
		return b.tr("Abrir Audiofilus", "Open Audiofilus")
	case idQuit:
		return b.tr("Salir", "Quit")
	}
	return ""
}

// separator dice si el elemento es una raya.
func separator(id int) bool { return id == idSep1 || id == idSep2 }

// checked dice si el elemento lleva la casilla marcada (Aleatorio).
func (b *base) checked(id int) bool { return id == idShuffle && b.eng.Status().Shuffle }

// do hace lo del elemento id; changed dice si el menú cambió (la casilla).
func (b *base) do(id int) (changed bool) {
	switch id {
	case idToggle:
		b.eng.Toggle()
	case idNext:
		b.eng.Next()
	case idPrev:
		b.eng.Prev()
	case idShuffle:
		b.eng.SetShuffle(!b.eng.Status().Shuffle)
		return true
	case idOpen:
		b.open()
	case idQuit:
		b.opt.Quit()
	}
	return false
}

// open abre la interfaz (este mismo binario, sin argumentos), si no está ya
// abierta, separada del servicio (detach, en cada sistema): es del usuario.
func (b *base) open() {
	if b.opt.UIOpen() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("tray: %v", err)
		return
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "AUDIOFILUS_HOME="+b.opt.Home)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("tray: opening the interface: %v", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// iconAt es el icono de la app (el CD de 256×256) reducido a size×size sin
// suavizar, tomando un píxel de cada bloque (es pixel art).
func iconAt(size int) *image.NRGBA {
	src, err := png.Decode(bytes.NewReader(desktop.Icon))
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	if err != nil {
		return out
	}
	r := src.Bounds()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := color.NRGBAModel.Convert(src.At(r.Min.X+x*r.Dx()/size+r.Dx()/size/2, r.Min.Y+y*r.Dy()/size+r.Dy()/size/2))
			out.Set(x, y, c)
		}
	}
	return out
}
