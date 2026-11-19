package esdata

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
)

// LaunchArgs es la orden con que ES lanzaría el juego g del sistema s: el
// <command> de es_systems.cfg con sus marcadores puestos
// (FileData::getlaunchCommand), ya separada en argumentos. Se separa antes
// de poner los marcadores, así una ruta con espacios sigue siendo un solo
// argumento sin comillas. controllers son los argumentos de los mandos
// (-p1index 0 -p1guid ...) que van en %CONTROLLERSCONFIG%. La información
// del juego en XML (%GAMEINFOXML%) no se escribe: va vacía, como en ES cuando
// no puede guardarla.
func LaunchArgs(s *System, g *Game, emulator, core string, controllers []string) []string {
	repl := map[string]string{
		"%SYSTEM%":     s.Name,
		"%ROM%":        g.Path,
		"%ROM_RAW%":    g.Path,
		"%BASENAME%":   strings.TrimSuffix(path.Base(g.Path), path.Ext(g.Path)),
		"%EMULATOR%":   emulator,
		"%CORE%":       core,
		"%HOME%":       "/userdata/system",
		"%GAMENAME%":   g.Name,
		"%SYSTEMNAME%": s.FullName,
	}
	var out []string
	for _, tok := range strings.Fields(s.Command) {
		switch tok {
		case "%CONTROLLERSCONFIG%":
			out = append(out, controllers...)
			continue
		case "%GAMEINFOXML%", "%NETPLAY%":
			// Sin valor: también se quita el -gameinfoxml de antes.
			if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "-") {
				out = out[:n-1]
			}
			continue
		}
		for k, v := range repl {
			tok = strings.ReplaceAll(tok, k, v)
		}
		out = append(out, tok)
	}
	return out
}

// RequestPath es donde la interfaz deja el juego a lanzar al cerrarse; el
// lanzador del port lo ve y llama a `audiofilus launch` (en /tmp: se pierde
// al apagar, así no se lanza nada viejo al arrancar).
const RequestPath = "/tmp/audiofilus-launch.json"

// ControllersToken es el marcador que va en Args donde van los mandos: los
// pone `audiofilus launch`, que puede preguntarle a SDL (la interfaz no:
// ya lo usa ella).
const ControllersToken = "%CONTROLLERSCONFIG%"

// Request es un juego a lanzar.
type Request struct {
	Args   []string `json:"args"` // la orden, con ControllersToken
	System string   `json:"system"`
	Rom    string   `json:"rom"`
	Name   string   `json:"name"`
	// Dir es la carpeta del sistema (la de su gamelist.xml), donde se apunta
	// la partida al volver.
	Dir string `json:"dir,omitempty"`
}

// Save escribe la petición en RequestPath.
func (q *Request) Save() error {
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return os.WriteFile(RequestPath, b, 0o644)
}

// LoadRequest lee la petición de path.
func LoadRequest(path string) (*Request, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var q Request
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, err
	}
	if len(q.Args) == 0 {
		return nil, errors.New("launch request without a command")
	}
	return &q, nil
}

// launcherMark es lo que tiene el lanzador del port de Audiofilus
// (portmaster/Audiofilus.sh): así se reconoce aunque se llame de otra forma.
const launcherMark = "AUDIOFILUS_LAUNCHER=1"

// IsOwnLauncher dice si file (un archivo de la copia o de la consola) es el
// lanzador de Audiofilus: un .sh con launcherMark. Lanzarlo desde el modo
// juego abriría Audiofilus dentro de sí mismo.
func IsOwnLauncher(file string) bool {
	if !strings.EqualFold(path.Ext(file), ".sh") {
		return false
	}
	b, err := os.ReadFile(file)
	return err == nil && strings.Contains(string(b), launcherMark)
}
