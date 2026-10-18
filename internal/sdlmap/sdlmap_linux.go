// Package sdlmap le pregunta a SDL2 cómo está mapeado el mando: qué botón es L1,
// R1, Start... Así audiofilus entiende el mando de cualquier consola igual que
// los juegos de PortMaster (que pasan SDL_GAMECONTROLLERCONFIG o su
// gamecontrollerdb.txt), y en el PC cualquier mando que SDL conozca.
//
// SDL se carga en tiempo de ejecución con purego (sin cgo), como el backend
// SDL2 del fork de Ebitengine del autor, y solo se usa al arrancar: después el
// mando se lee directamente de evdev, bloqueando, para no despertar la CPU.
package sdlmap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const sdlInitGameController = 0x00002000

// Controller es un mando que SDL reconoce como gamepad.
type Controller struct {
	Name    string
	Path    string // /dev/input/eventN; vacío si SDL es anterior a 2.24
	Mapping string // cadena de mapeo de SDL: "guid,nombre,a:b0,...,"
}

var (
	sdlSetHint               func(name, value string) bool
	sdlInit                  func(flags uint32) int32
	sdlQuit                  func()
	sdlGetError              func() string
	sdlNumJoysticks          func() int32
	sdlIsGameController      func(index int32) bool
	sdlJoystickNameForIndex  func(index int32) string
	sdlMappingForDeviceIndex func(index int32) *byte
	sdlFree                  func(p *byte)
	sdlJoystickPathForIndex  func(index int32) string
	hasJoystickPathForIndex  bool
	loadErr                  error
	loaded                   bool
)

// load abre libSDL2 y busca las funciones necesarias.
func load() error {
	if loaded {
		return loadErr
	}
	loaded = true
	var lib uintptr
	var errs []error
	for _, name := range []string{"libSDL2-2.0.so.0", "libSDL2-2.0.so", "libSDL2.so"} {
		l, err := purego.Dlopen(name, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err == nil {
			lib = l
			break
		}
		errs = append(errs, err)
	}
	if lib == 0 {
		loadErr = fmt.Errorf("sdlmap: failed to load libSDL2: %w", errors.Join(errs...))
		return loadErr
	}
	for _, f := range []struct {
		ptr  any
		name string
	}{
		{&sdlSetHint, "SDL_SetHint"},
		{&sdlInit, "SDL_Init"},
		{&sdlQuit, "SDL_Quit"},
		{&sdlGetError, "SDL_GetError"},
		{&sdlNumJoysticks, "SDL_NumJoysticks"},
		{&sdlIsGameController, "SDL_IsGameController"},
		{&sdlJoystickNameForIndex, "SDL_JoystickNameForIndex"},
		{&sdlMappingForDeviceIndex, "SDL_GameControllerMappingForDeviceIndex"},
		{&sdlFree, "SDL_free"},
	} {
		if _, err := purego.Dlsym(lib, f.name); err != nil {
			loadErr = fmt.Errorf("sdlmap: %s not found in libSDL2: %w", f.name, err)
			return loadErr
		}
		purego.RegisterLibFunc(f.ptr, lib, f.name)
	}
	// SDL_JoystickPathForIndex existe desde SDL 2.24.
	if _, err := purego.Dlsym(lib, "SDL_JoystickPathForIndex"); err == nil {
		purego.RegisterLibFunc(&sdlJoystickPathForIndex, lib, "SDL_JoystickPathForIndex")
		hasJoystickPathForIndex = true
	}
	return nil
}

// portMasterDirs son las carpetas donde suele estar PortMaster.
var portMasterDirs = []string{
	"/userdata/system/.local/share/PortMaster", // Knulli
	"/roms/ports/PortMaster",
	"/opt/system/Tools/PortMaster",
	"/opt/tools/PortMaster",
	"/mnt/SDCARD/Apps/PortMaster/PortMaster",
}

// Controllers devuelve los mandos que SDL reconoce como gamepad, con su mapeo.
func Controllers() ([]Controller, error) {
	if err := load(); err != nil {
		return nil, err
	}
	// Sin manejadores de señales: las señales son del daemon, no de SDL. Los
	// mandos USB por evdev y no por HIDAPI, para que la numeración de botones
	// del mapeo coincida con los códigos de evdev.
	sdlSetHint("SDL_NO_SIGNAL_HANDLERS", "1")
	sdlSetHint("SDL_JOYSTICK_HIDAPI", "0")
	sdlSetHint("SDL_JOYSTICK_ALLOW_BACKGROUND_EVENTS", "1")
	if os.Getenv("SDL_GAMECONTROLLERCONFIG_FILE") == "" {
		for _, p := range dbPaths() {
			if _, err := os.Stat(p); err == nil {
				sdlSetHint("SDL_GAMECONTROLLERCONFIG_FILE", p)
				break
			}
		}
	}
	if sdlInit(sdlInitGameController) != 0 {
		return nil, fmt.Errorf("sdlmap: SDL_Init: %s", sdlGetError())
	}
	defer sdlQuit()

	var out []Controller
	n := sdlNumJoysticks()
	for i := int32(0); i < n; i++ {
		if !sdlIsGameController(i) {
			continue
		}
		c := Controller{Name: sdlJoystickNameForIndex(i)}
		if p := sdlMappingForDeviceIndex(i); p != nil {
			c.Mapping = unix.BytePtrToString(p)
			sdlFree(p)
		}
		if hasJoystickPathForIndex {
			c.Path = sdlJoystickPathForIndex(i)
		}
		out = append(out, c)
	}
	return out, nil
}

// dbPaths son los gamecontrollerdb.txt a probar, en el orden de PortMaster
// (control.txt): primero los de la consola, después el de la CFW
// (PortMaster/<cfw>/, como knulli/) y al final el general. El general no basta:
// la RG34XX-SP comparte identificador de SDL con la ODROID Go 2 y ahí sale su
// mapeo, que no es el suyo.
func dbPaths() []string {
	var paths []string
	if h, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(h, ".config", "gamecontrollerdb.txt"))
	}
	paths = append(paths, "/tmp/gamecontrollerdb.txt")
	dirs := append([]string(nil), portMasterDirs...)
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		dirs = append(dirs, filepath.Join(d, "PortMaster"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".local", "share", "PortMaster"))
	}
	for _, d := range dirs {
		cfw, _ := filepath.Glob(filepath.Join(d, "*", "gamecontrollerdb.txt"))
		paths = append(paths, cfw...)
	}
	for _, d := range dirs {
		paths = append(paths, filepath.Join(d, "gamecontrollerdb.txt"))
	}
	return paths
}
