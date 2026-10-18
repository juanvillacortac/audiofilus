# ROCKNIX, ArkOS y dArkOS

audiofilus también corre en **ROCKNIX** y en la familia **ArkOS** (ArkOS y dArkOS). Los tres usan systemd y comparten casi todo (sysfs para el brillo y el governor, logind), así que es una sola plataforma con sus diferencias: `internal/platform/handheld.go`. Se activa sola: ROCKNIX por `/etc/os-release`, ArkOS/dArkOS por su `/usr/local/bin/pause.sh` y su usuario `ark`. Lo que sigue sale de su código ([ROCKNIX/distribution](https://github.com/ROCKNIX/distribution), [christianhaitian/dArkOS](https://github.com/christianhaitian/dArkOS)).

**Sin probar en consolas con estos sistemas todavía.** El inhibidor de logind sí se probó en el PC (`TestLogindInhibitLive`): sale en `systemd-inhibit --list` y desaparece al soltarlo.

| Qué | ROCKNIX | ArkOS / dArkOS |
|---|---|---|
| Quién suspende | logind: `HandleLidSwitch=suspend`, `HandlePowerKey=suspend` | ogage (el demonio de atajos) llama a `/usr/local/bin/pause.sh` (`sudo systemctl suspend`); logind ignora el botón (`HandlePowerKey=ignore`); `auto_suspend.py` suspende tras N minutos sin tocar nada, salvo que `/usr/local/bin/processcheck.sh` diga "Running" (hoy, solo con Kodi o ffplay) |
| Que no suspenda con la app abierta | Un **inhibidor de logind** ("block" de sleep, idle, handle-power-key, handle-suspend-key y handle-lid-switch), por D-Bus, que se suelta al cerrar el archivo que da logind. Es lo que usan los reproductores del escritorio | El inhibidor (por si acaso) y, como en Knulli, **copias montadas** encima de `pause.sh` (no suspende: el botón lo lleva audiofilus, con su sleep) y de `processcheck.sh` (dice "Running"). Se deshacen al cerrar, y tras un cierre inesperado al volver a arrancar (`/tmp/audiofilus-arkos-mounts`) |
| Permisos | Los ports corren como root | Los ports corren como `ark`: montar y escribir en sysfs va con `sudo -n` (ark lo tiene sin contraseña) |
| Pantalla en el sleep | `/sys/class/backlight/*/brightness` a 0 y luego el valor que tenía | Igual |
| Volumen | `pactl` (PipeWire con el protocolo de Pulse) para leerlo; su script `/usr/bin/volume N` para ponerlo (lo guarda) | ALSA: `amixer` con el control "Playback" (RK3326) o "Master" |
| Botones de volumen | Los lleva `input_sense`; el servicio lee el volumen cada pocos segundos | Los lleva ogage; igual |
| Audífonos | pactl | El control "Playback Path" de ALSA (HP o SPK), que cambia su `headphone-audio-switch.sh` |
| Tapa | El sensor del AXP (`hallkey`) o, con su kernel mainline, el interruptor `SW_LID` por evdev (`EVIOCGSW`) | Igual (casi ninguna tiene tapa) |
| Vibración | El motor del AXP si existe (`axp2202-battery/moto`); si no, nada | Igual |
| Governor en el sleep, `gptokeyb` congelado, botón de encendido (`KEY_POWER`) | Igual que en Knulli | Igual, con `sudo` si hace falta |

**Limitación en ROCKNIX:** con su "suspensión falsa" (Ajustes de ROCKNIX: modo de suspensión apagado), al pulsar el botón su `input_sense` ejecuta `rocknix-fake-suspend`, que silencia el audio, congela los procesos y acapara el mando, sin ninguna forma de saltárselo. Con la suspensión normal (la de fábrica), el inhibidor la para y el botón pone el sleep de audiofilus.

El audio no necesita nada: ROCKNIX tiene el protocolo de PulseAudio (PipeWire) y ArkOS va por ALSA, y oto prueba los dos. El lanzador ya arregla el `~/.asoundrc` que algunos ports de ArkOS quitan.
