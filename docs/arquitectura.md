# Arquitectura

## Dos procesos

```
┌──────────────────────────┐   socket Unix (JSON)   ┌────────────────────────────────┐
│ Interfaz (Ebitengine)    │ ◄────────────────────► │ Servicio: audiofilus daemon    │
│ - biblioteca, portadas   │   play, next, status…  │ - decodifica y reproduce       │
│ - se puede cerrar        │                        │ - cola, aleatorio, repetición  │
│   (pendiente)            │                        │ - modo bolsillo y gestos       │
└──────────────────────────┘                        │ - tapa, botón, ahorro (Knulli) │
                                                    └────────────────────────────────┘
```

El servicio arranca con `audiofilus daemon -detach` (o solo, la primera vez que se pide `play`) en una sesión nueva (`setsid`), para que sobreviva al lanzador: Knulli mata el grupo de procesos del port al salir. Así la música sigue en el menú de Knulli o dentro de un juego, y PipeWire mezcla el audio de los dos.

La interfaz no tiene estado de reproducción propio: lo pide con `status` y manda comandos. Mientras tanto, la CLI hace ese papel.

## Los dos modos (decisión del autor, 17 de octubre de 2026)

| | **Segundo plano** (interfaz cerrada) | **App abierta** (interfaz de Ebitengine) |
|---|---|---|
| Para qué | Escuchar mientras se juega otra cosa o se navega por Knulli | Escuchar con la app, también en el bolsillo |
| Suspensión de la consola | **Normal**: tapa, botón e inactividad suspenden como siempre; al suspender, deja de sonar | La app la impide mientras está abierta, suene o no (tapa, botón, inactividad): pausada en el bolsillo se reanuda con los gatillos |
| Pantalla apagada con música | No | Sí: el pseudo-sleep de la app, con los gestos de doble toque |
| Controles | **Hotkeys globales** (pausar, adelantar, retrasar, siguiente, anterior), que no choquen con Knulli ni con los emuladores | La interfaz, y los gestos en pseudo-sleep |

Cómo está hecho:

- **`awake`** es un permiso con caducidad: la interfaz manda `awake on` al abrirse y lo renueva (por defecto dura 60 s). El daemon impide la suspensión solo si el permiso está vigente **y** suena música (`updateAwake`). Si la interfaz se cuelga, el permiso caduca y la consola vuelve a suspenderse sola.
- **`sleep`** es el pseudo-sleep de la app (pantalla apagada, gestos de doble toque); lo pide la interfaz.
- **El atajo de segundo plano** está siempre activo: FN tres veces seguidas (cada toque a menos de 400 ms del anterior) y, en 1,5 s, L1 (anterior), R1 (siguiente), L2 (−10 s), R2 (+10 s) o Select (pausar). Una vibración corta confirma que se armó. Otro botón entre los toques de FN lo anula, para no chocar con juegos que usen FN.

## El mando

`internal/sdlmap` carga SDL2 con `purego` (sin cgo, como el backend SDL2 del fork de Ebitengine) solo para preguntar cómo está mapeado el mando: la cadena de mapeo de SDL (`leftshoulder:b4,...`) y su `/dev/input/eventN`. Usa `SDL_GAMECONTROLLERCONFIG_FILE` si viene en el entorno (PortMaster lo exporta) o el `gamecontrollerdb.txt` de PortMaster. Luego `input.FromSDL` traduce los índices `bN`/`aN` a códigos de evdev con la misma numeración que SDL en Linux (primero de `BTN_JOYSTICK` a `KEY_MAX`, luego el resto; los ejes saltando los hats), y el mando se lee de evdev bloqueando: mientras no se pulsa nada no gasta CPU. Esperar eventos de SDL sin ventana obligaría a despertarse muchas veces por segundo.

Si SDL no está o no reconoce el mando, se usan los códigos estándar de Linux (`input.Standard`), que no valen en todas las consolas (la RG34XX-SP usa otros, ver [knulli.md](knulli.md)). `input.device` en `audiofilus.json` fuerza un dispositivo concreto.

## El camino del audio

```
archivo ─► decode.Source ─► resample.Reader ─► Engine.Read ─► oto.Player ─► PulseAudio/PipeWire
           (float32 estéreo,  (a 48 kHz,          (float32 → bytes,
            frecuencia propia) lineal)              avanza la cola)
```

- **`decode`** elige el decodificador por los primeros bytes del archivo (no por la extensión) y entrega siempre float32 estéreo intercalado a la frecuencia del archivo. Mono se duplica; más de dos canales se reducen a los dos frontales.
- **`resample`** interpola linealmente hasta la frecuencia de salida (por defecto 48 kHz, la de PipeWire en la consola). Si coinciden, no hace nada.
- **`engine`** tiene un único `oto.Player` que vive todo el rato y lee de `Engine.Read`. Cuando una canción se acaba, `Read` abre la siguiente sin dejar hueco.
- **`oto`** habla con PulseAudio por su protocolo, en Go puro. En Knulli todo corre como root sin `XDG_RUNTIME_DIR`; `usePulseSocket` apunta `PULSE_SERVER` a `/var/run/pulse/native`, porque si no `oto` caería a ALSA y chocaría con PipeWire.

## Hilos y lock

- El hilo de audio de `oto` llama a `Engine.Read`; los comandos llegan por el socket. Todo el estado del engine va con `e.mu`.
- **Cambiar de canción o saltar** se hace con `apply`: deja la operación en `e.pending` y llama a `Player.Seek`, que espera a que termine el `Read` en curso, vacía el búfer y llama a `Engine.Seek`, donde se ejecuta la operación. Así no suena el resto del búfer de la canción anterior.
- **Las retrollamadas** (`OnPlayingChanged`, `OnTrackChanged`) se encolan con `emit` y corren en otra gorrutina, nunca con el lock tomado ni en el hilo de audio, porque la de Knulli ejecuta `mount`.
- **En pausa** se llama a `Context.Suspend` para que el dispositivo de audio duerma.

## Estado y configuración

Junto al binario:

| Archivo | Qué es |
|---|---|
| `audiofilus.json` | La configuración; se escribe con los valores por defecto la primera vez (`config.Default`) |
| `data/state.json` | La cola, la posición, el aleatorio, la repetición y el volumen; se guarda al pausar, al salir y cada 15 s mientras suena |
| `data/audiofilus.log` | El log cuando el servicio corre separado (`-detach`) |

Al arrancar, el servicio recupera la cola y la posición de la última vez, en pausa.

El socket va en `/tmp/audiofilus-<hash de la carpeta>.sock`: `/userdata` en Knulli es exFAT y no admite sockets, y el hash evita que dos instalaciones choquen. `AUDIOFILUS_HOME` y `AUDIOFILUS_SOCKET` sustituyen la carpeta y el socket.

## Formatos

Las bibliotecas en C se cargan en tiempo de ejecución con purego (`decode/native.go`), sin cgo, igual que SDL2: Knulli las trae y casi todos los Linux de escritorio también. Si falta una, ese formato da `ErrUnsupported` (y la biblioteca lo salta); MP3 vuelve a go-mp3. `AUDIOFILUS_DECODER=go` obliga a no usarlas (para comparar, y en los tests).

| Formato | Decodificador | CPU en la RG34XX-SP* | Notas |
|---|---|---|---|
| MP3 | `libmpg123` (C); si no, `hajimehoshi/go-mp3` | 0,6 % (go-mp3: ~18 %) | |
| Opus (`.opus`, `.ogg`) | `libopusfile` (C) | 1,4 % | Siempre 48 kHz |
| AAC (`.m4a`, `.mp4`) | `libfdk-aac` (C) + lector de MP4 propio (`decode/mp4.go`) | 2,0 % | HE-AAC sale al doble de frecuencia |
| ALAC (`.m4a`) | Go puro (`decode/alac`, de `alicebob/alac`, MIT) + `mp4.go` | 3,7 % | 16 y 24 bits, mono y estéreo |
| Ogg Vorbis | Go puro (`jfreymuth/oggvorbis`) | 2,7 % | |
| FLAC | Go puro (`mewkiz/flac`) | 5,3 % | El salto cae al principio del bloque y se descarta hasta el frame pedido |
| WAV, AIFF | propio (`decode/wav.go`, `decode/aiff.go`) | 0,5 % | PCM de 8/16/24/32 bits y float de 32; AIFF-C sin comprimir |
| Módulos (`.mod`, `.xm`, `.it`, `.s3m`…) | `libxmp` (C) | — | Se reconocen por la extensión; se tocan una vez, a 48 kHz |

\* Solo decodificar, en % de un núcleo a 1,5 GHz (`audiofilus bench`, 2026-10-17), con un minuto de una canción real.

Las etiquetas (título, artista, álbum) las lee `dhowden/tag`.
