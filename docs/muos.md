# muOS

audiofilus también corre en **muOS** (MustardOS), que funciona en las mismas Anbernic H700 que Knulli (la RG34XX-SP entre ellas). Se instala igual, por PortMaster. Todo lo que en Knulli es especial tiene aquí su equivalente, sacado del código de muOS ([MustardOS/internal](https://github.com/MustardOS/internal)); la implementación está en `internal/platform/muos.go` y se activa sola si existen sus scripts (`/opt/muos/script/device/bright.sh`).

**Sin probar en una consola con muOS todavía** (el autor no tiene una). Lo que sigue es lo que dice su código.

| Qué | Knulli | muOS |
|---|---|---|
| Que no se suspenda con la app abierta (botón, tapa, inactividad) | `.pause` de battery-saver y montar copias de `knulli.board.capability` y `power-button` | Existir `/run/muos/caffeine`: su `SLEEP()` (en `script/mux/hotkey.sh`) no suspende por nada mientras esté |
| Que no atenúe la pantalla ni **silencie** la música por inactividad | Parar idlewatcher en el sleep | Cambiar cada 3 s el contador `/run/muos/input_activity`: su vigilante (`script/mux/idle.sh`) lo mira cada 5 s y, si cambió, inhibe la atenuación y el sleep. Es lo que usan los juegos. Sin esto, con `idle_mute` encendido, la música se callaría en el bolsillo |
| Apagar y encender la pantalla en el sleep | `knulli-brightness dispoff/dispon` | `script/device/bright.sh 0` (luz a 0 y pantalla en negro, sin guardarlo) y `bright.sh <brillo guardado>` (`/opt/muos/config/settings/general/brightness`) |
| Volumen del sistema | `knulli-audio getSystemVolume/setSystemVolume` | `script/device/audio.sh I` (dice "75%") y `audio.sh 75` (como sus botones; lo guarda) |
| Botones de volumen por audiofilus (para pasar de 100 %) | Un `knulli-audio` envuelto | No: los lleva su `muhotkey`. El servicio lee el volumen cada pocos segundos y la interfaz lo enseña; por encima de 100 % se sube desde la app |
| Vibración | `/sys/class/power_supply/axp2202-battery/moto` | La ruta de `/opt/muos/device/config/board/rumble` (en la RG34XX-SP, la misma) |
| Tapa | El mando manda Insert; y `hallkey` | La ruta de `board/hall` (la misma `hallkey`). El servicio la lee y la manda en el estado (`lid`): la interfaz entra en sleep al cerrarla, igual que en Knulli |
| Audífonos | pactl y el pin de debugfs | `/run/muos/headphones` (lo crean sus scripts al enchufarlos); bluetooth, si la salida de PipeWire es de bluez (`wpctl inspect`) |
| Audio | PulseAudio | PipeWire **sin** el protocolo de PulseAudio: oto prueba Pulse y cae a ALSA, cuya salida por defecto (`/etc/asound.conf` de muOS) va a PipeWire. Hace falta `XDG_RUNTIME_DIR=/run`, que audiofilus pone si falta |
| Governor en el sleep, `gptokeyb` congelado, botón de encendido (`KEY_POWER`) | — | Igual que en Knulli (es sysfs y evdev) |

Si algo no funcionara en muOS, lo más probable: que su `muhotkey` acapare el botón de encendido (entonces el toque no pondría el sleep de la app, pero tampoco suspendería, por `caffeine`) o el mando (los gestos del sleep llegan por el dispositivo que ve SDL, que en muOS puede ser su mando virtual "muOS-Keys"). Lo primero que hay que mirar es `data/audiofilus.log` (dice qué mando y qué botón de encendido encontró).
