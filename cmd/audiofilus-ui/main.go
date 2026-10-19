// Command audiofilus-ui es la interfaz de audiofilus. Habla con el servicio de
// audio (el binario audiofilus de la misma carpeta, que arranca si hace falta);
// al cerrarla, la música sigue sonando en segundo plano.
//
//	audiofilus-ui                              la interfaz
//	audiofilus-ui -shots <carpeta> -music <m>  capturas de cada pantalla y formato (sin servicio;
//	                                           -album elige qué suena, -frames añade animaciones)
//	audiofilus-ui -scrolltest -music <m>       recorre los álbumes y muestra la memoria (sin servicio)
//	audiofilus-ui -bench lake -music <m>       mide la CPU de un visualizador (sin servicio)
//
// Teclado: flechas, Enter (A), Esc (B), S (X, aleatorio), Tab (Select, pantallas), M (Start, más),
// Espacio (pausa), Q/E (anterior/siguiente), coma/punto (retroceder/adelantar),
// F1 (FN, guía de controles), R (volver a lo que suena).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/juanvillacortac/audiofilus/internal/config"
	"github.com/juanvillacortac/audiofilus/internal/desktop"
	"github.com/juanvillacortac/audiofilus/internal/ui"
)

// memoryLimit es el límite blando de memoria de Go: con él, el recolector
// devuelve memoria al sistema antes de que crezca. La interfaz debe caber en
// portátiles de 512 MB junto al sistema, el frontend y el servicio de audio.
const memoryLimit = 96 << 20

func main() {
	music := flag.String("music", "", "carpeta de música para -shots y -scrolltest")
	shots := flag.String("shots", "", "guarda capturas de cada pantalla y formato aquí y termina")
	scroll := flag.Bool("scrolltest", false, "recorre todos los álbumes solo y muestra la memoria usada")
	bench := flag.String("bench", "", "mide la CPU de un visualizador (drift, lake, trench...) y termina")
	benchProf := flag.String("benchprof", "", "con -bench, guarda aquí un perfil de CPU")
	frames := flag.String("frames", "", "con -shots, guarda también una secuencia de la pantalla de reproducción aquí")
	album := flag.String("album", "", "con -shots, el álbum que suena: el primero cuyo artista o título lo contiene")
	language := flag.String("lang", "", "con -shots, el idioma de las capturas (es, en)")
	flag.Parse()

	if err := run(*music, *shots, *frames, *album, *language, *scroll, *bench, *benchProf); err != nil {
		fmt.Fprintln(os.Stderr, "audiofilus-ui:", err)
		os.Exit(1)
	}
}

func run(music, shots, frames, album, language string, scroll bool, bench, benchProf string) error {
	debug.SetMemoryLimit(memoryLimit)
	if path := os.Getenv("AUDIOFILUS_CPUPROFILE"); path != "" {
		profileCPU(path)
	}
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	if err := ui.LoadFonts(); err != nil {
		return err
	}
	if err := ui.LoadShaders(); err != nil {
		return err
	}
	offline := shots != "" || scroll || bench != ""
	if offline {
		if music == "" {
			return fmt.Errorf("-shots, -scrolltest and -bench need -music")
		}
		// La caché de la biblioteca de la maqueta va aparte, para no pisar la real.
		paths.Data = filepath.Join(os.TempDir(), "audiofilus-mock")
	}
	app, err := ui.NewApp(paths, offline)
	if err != nil {
		return err
	}
	defer app.Close()
	if offline {
		app.SetMusicDir(music)
		if language != "" {
			ui.SetLanguage(language)
		}
	}
	app.ShotsDir = shots
	app.FramesDir = frames
	app.ShotsAlbum = album
	app.ScrollTest = scroll
	app.BenchVisual = bench
	app.BenchProfile = benchProf
	ebiten.SetWindowTitle("Audiofilus")
	ebiten.SetWindowSize(960, 720)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Dentro de gamescope (como se prueba en el PC un formato de consola), la
	// ventana de 4:3 saldría con bandas negras; a pantalla completa toma el
	// formato que tenga gamescope.
	if os.Getenv("GAMESCOPE_WAYLAND_DISPLAY") != "" && !offline {
		ebiten.SetFullscreen(true)
	}
	ebiten.SetWindowIcon(windowIcons())
	// La clase de la ventana es la de la entrada del lanzador (StartupWMClass):
	// así Hyprland y la barra la asocian a audiofilus y a su icono.
	opts := &ebiten.RunGameOptions{X11ClassName: desktop.ID, X11InstanceName: desktop.ID}
	if err := ebiten.RunGameWithOptions(app, opts); err != nil {
		return err
	}
	return app.Err()
}

// profileCPU guarda un perfil de CPU de los 30 primeros segundos tras los
// 10 de arranque en path (AUDIOFILUS_CPUPROFILE): para ver qué cuesta en la
// consola, donde no hay otra forma cómoda de perfilar.
func profileCPU(path string) {
	go func() {
		time.Sleep(10 * time.Second)
		f, err := os.Create(path)
		if err != nil {
			log.Printf("cpu profile: %v", err)
			return
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Printf("cpu profile: %v", err)
			return
		}
		time.Sleep(30 * time.Second)
		pprof.StopCPUProfile()
		log.Printf("cpu profile written to %s", path)
	}()
}

// windowIcons son el icono de la app a 256 y a 32 píxeles (este tomando un
// píxel de cada ocho: el original es pixel art ampliado ×8).
func windowIcons() []image.Image {
	big, err := png.Decode(bytes.NewReader(desktop.Icon))
	if err != nil {
		return nil
	}
	small := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			small.Set(x, y, big.At(x*8+4, y*8+4))
		}
	}
	return []image.Image{big, small}
}
