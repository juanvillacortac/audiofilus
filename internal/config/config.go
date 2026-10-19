// Package config guarda la configuración y el estado junto al binario, para
// que una instalación copiada a otro sitio conserve su configuración.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config es audiofilus.json, junto al binario. Sus rutas relativas se resuelven
// contra la carpeta del binario.
type Config struct {
	// MusicDirs son las carpetas que se reproducen cuando no se indican rutas.
	MusicDirs []string `json:"music_dirs"`
	// SampleRate es la frecuencia de salida; toda canción se remuestrea a ella.
	SampleRate int `json:"sample_rate"`
	// BufferMS es el búfer de audio. Uno mayor despierta la CPU menos veces.
	BufferMS int     `json:"buffer_ms"`
	Volume   float64 `json:"volume"`
	// Language es el idioma de la interfaz ("es", "en"); vacío lo elige por $LANG.
	Language string  `json:"language"`
	Sleep    Sleep   `json:"sleep"`
	UI       UI      `json:"ui"`
	Input    Input   `json:"input"`
	Hotkeys  Hotkeys `json:"hotkeys"`
	// Servers son las bibliotecas remotas (Subsonic/OpenSubsonic: Navidrome,
	// gonic, Airsonic...), ver internal/remote.
	Servers []Server `json:"servers,omitempty"`
}

// Server es una biblioteca remota.
type Server struct {
	// Name es como sale en la barra de pantallas; también nombra su caché.
	Name string `json:"name"`
	// Type es el protocolo: "subsonic" (vacío también) o "jellyfin" (también
	// Emby).
	Type     string `json:"type,omitempty"`
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	// Quality es lo que se pide al escuchar: "" (MP3 a 192 kbps: poco que
	// bajar y lo más barato de decodificar), "original", o los kbps del MP3
	// ("320", "128", "96").
	Quality string `json:"quality,omitempty"`
	// CacheMB es cuánto puede ocupar lo escuchado en data/cache (0: 400 MB).
	CacheMB int `json:"cache_mb,omitempty"`
}

// Sleep configura el sleep de la app: pantalla apagada y gestos en los gatillos.
type Sleep struct {
	DoubleTapMS int `json:"double_tap_ms"`
	ChordMS     int `json:"chord_ms"`
	// Governor es el governor de CPU durante el sleep. Vacío es "powersave"
	// (lo mismo que el modo Power Saver de Knulli): con libmpg123 decodificar
	// a 480 MHz cuesta ~2 % de un núcleo, y pausada en el bolsillo la consola
	// gasta ~25 mA menos que con "performance". "keep" no lo toca.
	Governor      string `json:"governor"`
	RumbleMS      int    `json:"rumble_ms"`
	PauseRumbleMS int    `json:"pause_rumble_ms"`
}

// UI son las opciones de la interfaz.
type UI struct {
	// SleepAfter: segundos sin tocar nada, mientras suena, hasta el sleep de la
	// app (pantalla apagada); 0 es nunca.
	SleepAfter int  `json:"sleep_after_seconds"`
	Clock24    bool `json:"clock_24h"`
	// SwapAB intercambia A y B (confirmar y volver).
	SwapAB bool `json:"swap_ab"`
	// SleepMode es cómo se apaga la pantalla: "backlight" (apaga la
	// retroiluminación; en el PC no hace nada) u "oled" (solo la pinta de negro,
	// que en una OLED es tener los píxeles apagados).
	SleepMode string `json:"sleep_mode"`
	// Sounds es el volumen de los sonidos de la música (pausa, siguiente,
	// anterior, aleatorio, al azar; también los del atajo de FN y los gestos
	// del sleep, que pone el servicio): "off", "low", "" (medio) o "high"
	// (ver internal/sfx).
	Sounds string `json:"sounds,omitempty"`
	// UISounds es el de los de la interfaz (moverse, aceptar, menús,
	// pantallas, escribir, el splash), con los mismos valores.
	UISounds string `json:"ui_sounds,omitempty"`
	// RadioCountry es el país de las emisoras populares: "" el de la zona
	// horaria, "world" todo el mundo, o un código de dos letras ("VE").
	RadioCountry string `json:"radio_country,omitempty"`
	// GameBoy es el modo Game Boy (el código Konami): todo en cuatro verdes.
	GameBoy bool `json:"game_boy,omitempty"`
}

// Input elige el mando. Lo normal es dejarlo vacío: el mando y su mapeo salen
// de SDL, igual que en los juegos de PortMaster.
type Input struct {
	// Device es un dispositivo evdev concreto (/dev/input/eventN); vacío elige el mando.
	Device string `json:"device"`
}

// Hotkeys configura el atajo de segundo plano: FN pulsado Taps veces y luego
// L1 (anterior), R1 (siguiente), L2 (retroceder), R2 (adelantar) o Select (pausar).
type Hotkeys struct {
	Enabled  bool `json:"enabled"`
	Taps     int  `json:"taps"`
	TapMS    int  `json:"tap_ms"`
	WindowMS int  `json:"window_ms"`
	SeekSec  int  `json:"seek_seconds"`
}

// Default es la configuración que se usa (y se escribe) si no existe
// audiofilus.json.
func Default() Config {
	return Config{
		MusicDirs:  []string{}, // vacío: la interfaz pide la carpeta la primera vez
		SampleRate: 48000,
		BufferMS:   250,
		Volume:     1,
		Sleep: Sleep{
			DoubleTapMS:   400,
			ChordMS:       600,
			RumbleMS:      150,
			PauseRumbleMS: 400,
		},
		UI:      UI{SleepAfter: 30, Clock24: true, SleepMode: "backlight"},
		Hotkeys: Hotkeys{Enabled: true, Taps: 3, TapMS: 400, WindowMS: 1500, SeekSec: 10},
	}
}

// Paths ubica cada archivo respecto al binario.
type Paths struct {
	Base   string // carpeta del binario
	Config string // audiofilus.json
	Data   string // data/: estado y logs
	State  string // data/state.json
	Log    string // data/audiofilus.log
	Socket string // socket de control, en /tmp (FAT/exFAT no admiten sockets)
}

// Locate encuentra la carpeta del binario. AUDIOFILUS_HOME la sustituye (útil
// con `go run`, cuyo binario vive en una carpeta temporal).
func Locate() (Paths, error) {
	base := os.Getenv("AUDIOFILUS_HOME")
	if base == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		base = homeFor(filepath.Dir(exe))
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return Paths{}, err
	}
	sum := sha256.Sum256([]byte(base))
	sock := os.Getenv("AUDIOFILUS_SOCKET")
	if sock == "" {
		sock = filepath.Join(os.TempDir(), "audiofilus-"+hex.EncodeToString(sum[:4])+".sock")
	}
	data := filepath.Join(base, "data")
	return Paths{
		Base:   base,
		Config: filepath.Join(base, "audiofilus.json"),
		Data:   data,
		State:  filepath.Join(data, "state.json"),
		Log:    filepath.Join(data, "audiofilus.log"),
		Socket: sock,
	}, nil
}

// Resolve hace absoluta una ruta respecto a la carpeta del binario.
func (p Paths) Resolve(path string) string {
	if filepath.IsAbs(path) || strings.Contains(path, "://") { // las remotas, tal cual
		return path
	}
	return filepath.Join(p.Base, path)
}

// Load lee audiofilus.json; la primera vez lo escribe con los valores por defecto.
func Load(p Paths) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(p.Config)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, writeJSON(p.Config, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save escribe audiofilus.json.
func Save(p Paths, cfg Config) error {
	return writeJSON(p.Config, cfg)
}

// State es lo que audiofilus recuerda entre ejecuciones.
type State struct {
	Queue    []string `json:"queue"`
	Index    int      `json:"index"`
	Position float64  `json:"position"`
	Shuffle  bool     `json:"shuffle"`
	Repeat   string   `json:"repeat"`
	Volume   float64  `json:"volume"`
}

// LoadState lee data/state.json; si no existe, el estado está vacío.
func LoadState(p Paths) State {
	var s State
	if b, err := os.ReadFile(p.State); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// SaveState escribe data/state.json de forma atómica.
func SaveState(p Paths, s State) error {
	if err := os.MkdirAll(p.Data, 0o755); err != nil {
		return err
	}
	return writeJSON(p.State, s)
}

// writeJSON escribe v en path a través de un archivo temporal y un rename,
// para no dejar el archivo a medias si se corta la corriente.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// homeFor es la carpeta de audiofilus para un binario en dir: la misma, salvo
// en el port de PortMaster, donde los binarios van en bin/<arquitectura>/ y
// la configuración dos carpetas más arriba.
func homeFor(dir string) string {
	if arch := filepath.Base(dir); (arch == "aarch64" || arch == "x86_64") && filepath.Base(filepath.Dir(dir)) == "bin" {
		return filepath.Dir(filepath.Dir(dir))
	}
	return dir
}
