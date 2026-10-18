# AGENTS.md

Guía para agentes de código (Claude Code, Codex, etc.) que trabajen en **audiofilus**: un reproductor de música en Go para PC y para portátiles Linux con Knulli (probado en la Anbernic RG34XX-SP). Lo distinto de otros reproductores es que **la música sigue sonando en segundo plano**, aunque se cierre la interfaz o se abra un juego, y que tiene un **modo bolsillo** que gasta muy poca batería. La documentación de cómo está hecho está en [`docs/`](docs/README.md); aquí va lo que necesitas saber antes de tocar nada.

## Estado

- **Hecho:** el servicio de audio con su CLI, la interfaz en Ebitengine (biblioteca, reproductor, visualizadores, juegos, servidores Subsonic/Jellyfin, radio, podcasts y YouTube Music con el yt-dlp de cada quien), el modo bolsillo y la integración con Knulli, muOS, ROCKNIX y ArkOS/dArkOS. Medido en la consola: [docs/consumo.md](docs/consumo.md).
- **Pendiente:** optimizar para PC (ratón, teclado, ventanas), y más adelante Android e iOS.

## Cómo colaborar con el autor

- El autor escribe en español; responde en español.
- En este repo el autor pidió que el agente construya el reproductor, no solo que dé pistas. Aun así, las decisiones de diseño visibles (gestos, aspecto de la interfaz, qué se guarda) se le consultan antes.
- **Antes de instalar algo en la consola, pruébalo en el PC** (`go test`, y el servicio con música real). En la consola, prueba antes lo que pueda romper el arranque.
- Haz commits o push solo cuando el autor lo pida. El repo es público (github.com/juanvillacortac/audiofilus): nada de contraseñas, rutas personales ni música real.

## Comandos

```sh
go vet ./... && go test ./...                          # los tests (sin audio real; un segundo)
go run ./cmd/audiofilus play ~/Music                   # escuchar en el PC (arranca el servicio)
go run ./cmd/audiofilus status                         # qué suena
go run ./cmd/audiofilus bench <archivo.mp3>            # cuánta CPU cuesta decodificarlo
./build.sh                                             # servicio e interfaz, PC y arm64, y dist/audiofilus.zip (PortMaster)
dist/audiofilus-ui                                     # la interfaz (arranca el servicio de dist/)
dist/audiofilus-ui -shots /tmp/s -music ~/Music        # capturas de todas las pantallas y formatos
dist/audiofilus-ui -bench lake -music ~/Music          # CPU y memoria por fotograma de un visualizador
docker build --target dist --output dist .             # lo mismo que build.sh, sin instalar nada
```

Con `go run`, el binario vive en una carpeta temporal; para que la configuración quede en un sitio fijo usa `AUDIOFILUS_HOME=$PWD/run go run ./cmd/audiofilus ...`, o compila con `./build.sh`.

Antes de dar un cambio por terminado: `gofmt -l .` vacío, `go vet ./...` y `go test ./...` en verde, y que compile para la consola **sin cgo**:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/audiofilus
```

## El mapa

| Carpeta | Qué hay |
|---|---|
| `cmd/audiofilus/` | El binario: `daemon`, los comandos de la CLI y `bench` |
| `internal/decode/` | Decodificadores a float32 estéreo: Go puro (Vorbis, FLAC, WAV, AIFF, ALAC, MP4) y bibliotecas en C por purego (mpg123, opusfile, fdk-aac, libxmp) |
| `internal/resample/` | Remuestreo lineal a la frecuencia de salida (48 kHz) |
| `internal/engine/` | La reproducción con `oto`: un player, la cola, saltos sin cortes |
| `internal/queue/` | El orden: aleatorio, repetición, anterior/siguiente, "reproducir después" |
| `internal/library/` | Buscar archivos (carpetas, `.m3u`) y leer etiquetas |
| `internal/control/` | El protocolo del socket entre el servicio y sus clientes |
| `internal/daemon/` | El servicio: une engine, plataforma, modo bolsillo y socket |
| `internal/platform/` | Escritorio (no hace nada), Knulli, muOS, ROCKNIX y ArkOS/dArkOS (tapa, botón, ahorro de batería, sleep de la app; ver [docs/knulli.md](docs/knulli.md), [docs/muos.md](docs/muos.md) y [docs/rocknix-arkos.md](docs/rocknix-arkos.md)) |
| `internal/input/` | Botones lógicos desde el mapeo de SDL, lectura del mando (evdev) y detección de gestos y atajos |
| `internal/sdlmap/` | Pregunta a SDL2 (vía `purego`) el mapeo del mando |
| `internal/ui/` | La interfaz en Ebitengine: pantallas, cliente del servicio, biblioteca con caché, portadas en segundo plano, idiomas |
| `cmd/audiofilus-ui/` | El binario de la interfaz |
| `internal/config/` | `audiofilus.json` y `data/`, **junto al binario** |
| `portmaster/` | El port de PortMaster: lanzador, `gameinfo.xml`, `port.json` e imágenes ([docs/portmaster.md](docs/portmaster.md)) |
| `tools/portmaster/` | `art.sh`: rehace la portada y la captura del port |
| `site/` | La web (GitHub Pages), en inglés; sus capturas y GIFs salen de `-shots`/`-frames` con `-lang en` |
| `.github/workflows/` | CI (gofmt, vet, tests, arm64 sin cgo), releases al subir una etiqueta `v*` (con el `Dockerfile`) y la web |
| `docs/` | La documentación, en español |
| `.claude/skills/` | Procedimientos: probar en la consola, medir consumo |

Reglas de arquitectura:

- **El audio y todo lo que ahorra energía viven en el servicio**, no en la interfaz. La interfaz será otro proceso que habla por el socket; se puede cerrar y la música sigue. Ver [docs/arquitectura.md](docs/arquitectura.md).
- **Sin cgo.** Todo se compila con `CGO_ENABLED=0`, igual que Trancazo. Las bibliotecas en C (SDL2, mpg123, opusfile, fdk-aac, libxmp) se cargan en tiempo de ejecución con purego, y si faltan se sigue sin ellas ([docs/arquitectura.md](docs/arquitectura.md#formatos)).
- **La configuración es portable**: `audiofilus.json` y `data/` van junto al binario (`config.Locate`). La única excepción es el socket, que va en `/tmp` porque `/userdata` en Knulli es exFAT y no admite sockets.
- **En segundo plano la consola se suspende como siempre.** Impedirlo es cosa de la app abierta (`awake`, un permiso que caduca), nunca del servicio por su cuenta.
- **Los botones salen del mapeo de SDL**, no de códigos fijos: cada consola usa los suyos.
- **Lo de cada sistema (Knulli, muOS, ROCKNIX, ArkOS) solo se activa si se detecta** (`platform.Detect`); en el PC todo es un no-op, así se prueba lo mismo en los dos sitios.

## Knulli

Lo que el servicio toca en la consola, cómo se limpia y los riesgos conocidos están en [docs/knulli.md](docs/knulli.md). Lo esencial:

- Mientras suena: `/var/run/battery-saver/audiofilus.pause`, y montajes `--bind` sobre `/boot/boot/knulli.board.capability` (tapa) y `/usr/bin/power-button` (botón). Se deshacen al pausar o salir, y tras un cierre inesperado al volver a arrancar (`/tmp/audiofilus-mounts`).
- La consola solo acepta contraseña por SSH; ver la skill [`probar-en-knulli`](.claude/skills/probar-en-knulli/SKILL.md).

## Convenciones de código

- **Comentarios y documentación en español**, en frases completas: cada tipo, función y constante dice qué es y por qué. Los comentarios de doc empiezan por el nombre (`// Load lee audiofilus.json...`). Imita la densidad del archivo que tocas.
- **Los mensajes de error (`fmt.Errorf`, `errors.New`), de los tests (`t.Errorf`) y del log (`log.Printf`) van en inglés.** Los textos que ve el usuario en la interfaz salen de `internal/ui/lang.go` (español e inglés).
- El engine se llama desde dos hilos (el de audio de `oto` y los comandos): todo lo que toca su estado va con `e.mu`, y las retrollamadas salen por `emit`, nunca con el lock tomado. Para cambiar de canción o saltar, usa `apply` (vacía el búfer de audio con `Player.Seek`).
- **Al matar procesos en pruebas, no uses `pgrep -f`/`pkill -f` con un texto que también aparezca en tu propio comando**: se mata la shell que lo ejecuta. Busca por nombre exacto (`pgrep -x`) y filtra por `/proc/<pid>/exe`.
- Un test por comportamiento, con un comentario que diga qué comprueba. Los audios de prueba son tonos sintéticos (ver `internal/decode/testdata/README.md`); **no metas música real en el repo**.
