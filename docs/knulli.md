# Knulli

Lo que sigue se aprendió parcheando Songo #5 en la RG34XX-SP (Knulli sobre Batocera 42, octubre de 2026) y está en `internal/platform/knulli.go`. Se activa solo si existen `/boot/boot/knulli.board.capability` y `/usr/bin/knulli-brightness`.

## Mientras la app pide mantenerla despierta

Knulli suspende la consola por tres caminos. El servicio los neutraliza solo cuando la interfaz lo pide (`awake`) **y** suena música (`SetAwake(true)`), y los devuelve al pausar, al caducar el permiso o al salir. En segundo plano no toca nada: la consola se suspende como siempre.

| Camino | Cómo funciona en Knulli | Qué hace audiofilus |
|---|---|---|
| Inactividad | `idlewatcher` a los 5 min atenúa y **silencia el audio** (`system.batterysaver.mute=1`); a los 15 min suspende. Sus hooks (`/etc/idlewatcher/*.d`) no hacen nada si hay un `*.pause` en `/var/run/battery-saver/` | Crea `/var/run/battery-saver/audiofilus.pause` |
| Tapa | Cerrar la tapa manda `KEY_INSERT`; `triggerhappy` ejecuta `/usr/bin/lid-control close`, que solo actúa si `knulli-board-capability lid` (lee `/boot/boot/knulli.board.capability`) | Monta con `--bind` una copia del archivo sin la palabra `lid` |
| Botón de encendido | `KEY_POWER` → `/usr/bin/power-button`: pulsación corta = `knulli-suspend`, larga (2 s) = apagar | Monta una copia del script con `knulli-suspend &` cambiado por `:`. Mantener pulsado sigue apagando |

Notas:

- Knulli **no trae `mountpoint` ni `strings`**; los montajes se comprueban leyendo `/proc/mounts`.
- Si el archivo ya está tapado por otra app (por ejemplo Songo parcheado), no se monta encima.
- Los montajes se anotan en `/tmp/audiofilus-mounts`. Si el servicio muere sin limpiar, el siguiente arranque los deshace (`unmountRecorded`). Como `/tmp` es tmpfs, un reinicio también lo limpia todo.
- Con la música en pausa y la tapa **ya cerrada**, la consola no se suspende: Knulli solo actúa cuando la tapa *se cierra*.

## El sleep de la app

`audiofilus sleep on` (lo pedirá la interfaz):

1. Para `idlewatcher` (`/etc/init.d/S96idlewatcher-daemon stop`) si estaba corriendo: su hook "active" no mira los `*.pause` y, al detectar un botón, restauraría el brillo y el governor. Se vuelve a arrancar al salir.
2. `knulli-brightness dispoff`: apaga la retroiluminación y deja en negro el framebuffer.
3. Pone el governor `sleep.governor` (vacío: `powersave`, como el modo Power Saver de Knulli; `keep` no lo toca) y guarda el anterior para el despertar. Con libmpg123, decodificar MP3 a 480 MHz cuesta ~2 % de un núcleo; con go-mp3 eran ~55 % y por eso antes no se tocaba.

EmulationStation **no se congela**: con un port abierto ya está en espera.

Mientras dura, se reconocen estos gestos (los botones salen del mapeo de SDL):

| Gesto | Acción | Vibración |
|---|---|---|
| Doble toque de L1 / R1 (≤ 400 ms) | Anterior / siguiente | 150 ms |
| Doble toque de L2 / R2 | −10 s / +10 s | 150 ms |
| L1+L2 juntos, dos veces (≤ 600 ms) | Pausa / reanuda | 400 ms al pausar, 150 al reanudar |
| R1+R2 juntos, dos veces (≤ 600 ms) | Una canción al azar de toda la biblioteca (el aleatorio queda como estaba) | 150 ms |
| Start | Sale del sleep | — |
| Botón de encendido (toque) | Sale del sleep, **salvo con la tapa cerrada** (`hallkey` del AXP: 0 cerrada) | — |

Un toque o un acorde sueltos no hacen nada. Con la tapa cerrada solo se alcanzan L1, L2, R1 y R2. La vibración escribe en `/sys/class/power_supply/axp2202-battery/moto`.

Con la app abierta y despierta, un toque del botón de encendido entra en sleep (el servicio sube `power_seq` en el estado y la interfaz hace su fundido). El servicio lee la tecla `KEY_POWER` del dispositivo que la tenga y no sea un mando (`axp2202-pek`, `input.PowerButtons`), sin acapararla; un toque es soltarla antes de 1,5 s. Mantenerlo sigue apagando: eso lo hace el script de Knulli, del que la copia montada solo quita la suspensión. Con la app cerrada no se lee nada distinto: Knulli suspende como siempre.

## Botones de los audífonos

Los botones multimedia de los audífonos de bluetooth (BlueZ crea un dispositivo "(AVRCP)" al conectarlos) y de los de cable, si el chip de audio los ve (en la RG34XX-SP no: el H700 solo detecta si están enchufados), controlan la música con la app abierta o en segundo plano (`input/media_linux.go`, `Daemon.startMedia`): reproducir/pausa, reproducir, pausa, parar, siguiente, anterior, adelantar y retroceder. El botón único de los de cable (`KEY_MEDIA`) sigue la costumbre de Android: una pulsación pausa, dos pasan a la siguiente, tres a la anterior. No gasta batería: los dispositivos se buscan al arrancar y cada vez que aparece uno en `/dev/input` (inotify), y cada uno se lee con una lectura bloqueante; en reposo no hay ningún despertar. En el PC no se usa: ahí llegan por MPRIS. Vale en todos los sistemas (Knulli, muOS, ROCKNIX, ArkOS).

## Atajo de segundo plano

Siempre activo, también dentro de los juegos: **FN tres veces** y luego L1 / R1 / L2 / R2 / **Select (pausar)**. Ver [arquitectura.md](arquitectura.md).

## Prueba en la consola (17 de octubre de 2026)

- El servicio suena, lee etiquetas, crea `audiofilus.json` y `data/` junto al binario, y aplica los tres overrides y el `.pause`.
- CPU sonando a 1,5 GHz: 25 % de un núcleo (decodificar MP3 con `go-mp3`: 18 %).
- El mando es `event1` y tiene `BTN_TL`, `BTN_TR`, `BTN_TL2`, `BTN_TR2` y `BTN_START` (bits 310–315 de `B: KEY`).
- **Modo bolsillo:** la música siguió sonando con la pantalla apagada, pero **los gestos no hicieron nada** y la pantalla **se encendió sola** al pulsar R2. Causa: el mando de la RG34XX-SP **no usa los códigos estándar** (ver la tabla). R2 manda 0x13b, el `BTN_START` estándar, así que el servicio lo tomó por "despertar". La consola se quedó además sin red (el wifi dejó de responder a ARP) hasta reconectarlo.
- Al hacer `quit`, el servicio deshizo todo (montajes, `.pause`, governor).

### Códigos reales del mando de la RG34XX-SP

Capturados de `/dev/input/event1` (`Anbernic RG34XX-SP Controller`):

| Botón | Código | Nombre en Linux |
|---|---|---|
| L1 | 0x134 (308) | `BTN_WEST` |
| R1 | 0x135 (309) | `BTN_Z` |
| L2 | 0x13a (314) | `BTN_SELECT` |
| R2 | 0x13b (315) | `BTN_START` |
| Start | 0x137 (311) | `BTN_TR` |

Por eso el mapeo sale de SDL (`internal/sdlmap`), no de los códigos estándar.

### El mapeo de SDL en Knulli

- El identificador de SDL del mando es genérico (`19000000010000000100000000010000`, bus interno, fabricante 1, producto 1) y **choca con el de la ODROID Go 2** en el `gamecontrollerdb.txt` general de PortMaster. El bueno ("ANBERNIC-keys") está en `PortMaster/knulli/gamecontrollerdb.txt`; `sdlmap` busca en el mismo orden que el `control.txt` de PortMaster (`~/.config`, `/tmp`, la carpeta de la CFW y al final el general). Lanzado desde un port, el daemon hereda `SDL_GAMECONTROLLERCONFIG` de `get_controls`, como Trancazo.
- SDL 2.32 numera los botones por orden de código de todas las teclas del dispositivo: en la RG34XX-SP, b0=Escape, b1/b2=volumen, b3=0x130… `leftshoulder:b7` es 0x134 (L1). Comprobado con `audiofilus pad` y con los códigos capturados (test `TestFromSDLNumbering`).
- **Atajo de FN probado en la consola (17 de octubre):** cambiar de canción, pausar y saltar funcionan con FN ×3 + botón.
- El wifi de la consola se cae a menudo (deja de responder a ARP); las sesiones SSH largas se cortan.

## Riesgos sin comprobar

- **El hook "active" de `idlewatcher`** (`/etc/idlewatcher/active.d/01active-hooks`) no mira los `*.pause`: al detectar un botón hace `S31emulationstation resume`, restaura el governor y `knulli-brightness undim`. Por eso el sleep de la app para `idlewatcher` mientras dura (sin probar aún en la consola).
 deja en su cola los botones pulsados mientras tanto. Al reanudarlo los procesa; los dobles toques de L1/R1 podrían mover listas del menú.
- **Que Knulli no mate el servicio** al salir de un port: por eso `setsid`, pero falta probarlo con el lanzador de PortMaster.
- **Emuladores que abran ALSA directamente** podrían no compartir el audio con PipeWire.

## Acceso por SSH

La consola es `root@knulli.local` y solo acepta contraseña: `ssh-copy-id` no tiene efecto. El wifi se duerme cuando la consola no se usa y `knulli.local` deja de resolver; hay que despertarla. Ver la skill [`probar-en-knulli`](../.claude/skills/probar-en-knulli/SKILL.md).
