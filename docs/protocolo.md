# Protocolo de control

Un cliente abre el socket Unix (ver `config.Paths.Socket`), envía **una** petición JSON en una línea, lee **una** respuesta y cierra. Lo implementa `internal/control`.

```json
→ {"cmd": "seek", "args": ["+10"]}
← {"ok": true, "data": { ...estado... }}
← {"ok": false, "error": "usage: seek <seconds>|+<seconds>|-<seconds>"}
```

Casi todos los comandos responden con el estado completo (`daemon.FullStatus`):

```json
{
  "state": "playing",
  "track": {"path": "/.../Wasteland.mp3", "title": "Wasteland", "artist": "10 Years", "album": "The Autumn Effect"},
  "position": 64.2, "duration": 231.0,
  "index": 0, "count": 597,
  "shuffle": false, "repeat": "off", "volume": 1,
  "sleep": false, "awake": false, "platform": "desktop",
  "output": "wired",
  "pad": "Anbernic RG34XX-SP Controller (/dev/input/event1, mapping sdl)"
}
```

`state` es `playing`, `paused` o `stopped`. `position` ya descuenta el audio que aún está en el búfer.

## Comandos

| Comando | Argumentos | Qué hace |
|---|---|---|
| `status` | — | Solo devuelve el estado |
| `play` | `[rutas…]` | Sin rutas y con cola: reanuda. Si no, reemplaza la cola por los archivos, carpetas o `.m3u` (o por `music_dirs`) y suena |
| `load` | `inicio archivos…` | Reemplaza la cola por esos archivos (ya en orden) y suena desde la posición `inicio`. Lo usa la interfaz al elegir un álbum o una canción |
| `add` | `rutas…` | Añade al final de la cola |
| `playnext` | `archivos…` | Los pone justo después de la canción actual, en ese orden |
| `toggle` · `pause` · `resume` | — | Pausa/reanuda |
| `next` · `prev` | — | `prev` reinicia la canción si lleva más de 3 s, como Songo |
| `jump` | `n` | Reproduce la posición `n` de la cola |
| `seek` | `s` · `+s` · `-s` | Salta a `s` segundos o se mueve `±s` |
| `shuffle` | `[on\|off\|toggle]` | Sin argumento, alterna |
| `repeat` | `off\|all\|one` | |
| `volume` | `0..2` | El volumen de audiofilus (1 es el original); la interfaz usa `volstep` |
| `volstep` | `+N` · `-N` | Sube o baja N puntos en la escala única de 0 a 200: el volumen del sistema hasta 100 y el refuerzo de audiofilus por encima (al bajar, primero el refuerzo). Lo llaman los botones de volumen (vía el wrapper de `knulli-audio`) y + y − de la interfaz |
| `queue` | — | Devuelve la lista de rutas en orden de reproducción (no el estado) |
| `sleep` | `[on\|off\|toggle] [oled]` | El pseudo-sleep de la app: gestos y, sin `oled`, retroiluminación apagada (ver [knulli.md](knulli.md)) |
| `awake` | `on [segundos]` · `off` | Permiso de la app para impedir la suspensión mientras está abierta (suene o no); caduca a los 60 s si no se renueva |
| `attach` | — | Lo abre la interfaz y deja la conexión abierta mientras vive. Al cerrarse (al salir o porque murió) el servicio recibe `detached`: si no se pidió `background`, pausa, guarda el estado y se cierra |
| `background` | — | La interfaz se va a cerrar y la música debe seguir: segundo plano. Vale hasta el próximo `attach` |
| `quit` | — | Para el servicio, guarda el estado y deshace todo lo de Knulli |

Las rutas relativas las resuelve la CLI contra la carpeta actual antes de enviarlas; las de `music_dirs` se resuelven contra la carpeta del binario.
