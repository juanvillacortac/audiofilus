# Consumo

El objetivo del proyecto es gastar menos batería que Songo #5 en el bolsillo. Estas son las mediciones hasta ahora y cómo se hicieron.

## Referencia: la RG34XX-SP con Songo #5 (octubre de 2026)

La batería es de 3000 mAh. "Tiempo restante" es `time_to_empty_now` del indicador de batería; la caída de porcentaje es más fiable que esa estimación (ver abajo).

| Situación | Consumo | % por hora |
|---|---|---|
| Songo, pantalla encendida (brillo 33) | ~630 mA | ~21 % |
| Songo, pantalla apagada ("Bright Fade"), CPU fija a 1,5 GHz | ~460 mA | ~15 % |
| Ídem con `balanced` (`schedutil`) | ~370 mA | ~12 % |
| Ídem limitando Songo a 10 FPS en sleep ("Power Saver") | **~255 mA** | **~8,5 %** |
| Menú de Knulli, pantalla apagada, sin nada abierto | ~300–330 mA | ~10–11 % |

Lo que quedaba de Songo en sleep: ~38 % de un núcleo (dibujo a 10 FPS 16 %, audio SDL 11 %, decodificación 7 %).

## audiofilus en el PC (Ryzen 7 5700U)

| Medición | CPU de un núcleo |
|---|---|
| Servicio reproduciendo MP3, estable (20 s) | 2,4 % |
| `bench`: solo decodificar MP3 | 1,45 % |
| `bench`: + remuestreo a 48 kHz + conversión a bytes | ~1,5 % |

Casi todo el coste es `go-mp3`; el resto (~1 %) es el mezclador de `oto` y el envío a PulseAudio.

## audiofilus en la consola (RG34XX-SP, `schedutil` a 1,5 GHz)

| Medición | CPU de un núcleo |
|---|---|
| `bench`: decodificar MP3 (`go-mp3`) | **18 %** |
| Servicio reproduciendo, estable (20 s) | **25 %** |

`go-mp3` cuesta ~2,5 veces lo que costaba FFmpeg en Songo (7 %): es el punto a optimizar. A 480 MHz (`powersave`) serían ~55 % de un núcleo. El consumo de batería en modo bolsillo quedó sin medir (la prueba se cortó).

## audiofilus en la consola con libmpg123 (18 de octubre de 2026)

Decodificar MP3 con libmpg123 cuesta 0,6 % de un núcleo a 1,5 GHz (go-mp3: 18 %); ver la tabla de formatos en [arquitectura.md](arquitectura.md#formatos).

Sleep de la app con la música **en pausa**, pantalla apagada (`backlight(0)`), wifi encendido (conectado por SSH), estimación rápida con `time_to_empty_now` durante 3 min:

| Governor | Consumo | CPU (servicio + interfaz) |
|---|---|---|
| `performance` (1,5 GHz fijos) | ~295 mA | ~1 % + 0,4 % de un núcleo |
| `powersave` (480 MHz) | **~270 mA** | |

Desde entonces el sleep pone `powersave` por defecto (`sleep.governor`). La CPU de audiofilus ya casi no pesa: lo que queda es la base del sistema con la pantalla apagada (el menú de Knulli en esa situación gasta ~300–330 mA con `performance`) y el wifi. Falta medir sonando y sin wifi, con el método fiable (caída de `capacity`).

### Sonando en sleep (18 de octubre de 2026)

Con MP3 sonando, sleep, `powersave` y wifi encendido, ~260 mA según `time_to_empty_now` (8,9 h con el 77 %), casi lo mismo que Songo (~255 mA). Por proceso, 30 s:

| Proceso | CPU de un núcleo a 480 MHz |
|---|---|
| Servicio (decodificar, remuestrear, oto) | ~8–10 % |
| `gptokeyb` de PortMaster | ~5 % |
| Interfaz | ~1 % |
| `knulli-audio getSystemVolume` (cada 5 s) | procesos cortos: no salen en la tabla, pero el total de la consola era ~47 % de un núcleo contra ~30 % sumando procesos |

Cambios: en sleep el servicio ya no lee el volumen del sistema (nadie lo ve y desde fuera no se puede cambiar), y congela con SIGSTOP el `gptokeyb` de la interfaz (solo da el atajo Select+Start de PortMaster) y lo reanuda al despertar. Falta medir después de estos cambios y perfilar el servicio: `touch data/daemon-cpuprofile` graba 20 s en `data/daemon-cpu.prof` (el de la interfaz, `data/cpuprofile`, no sirve en sleep porque la interfaz casi no corre).

Medido después (mismas condiciones, 60 s): la consola entera pasó de ~47 % a **~18 % de un núcleo**; el servicio, ~8 %; la interfaz, ~1,7 %; `gptokeyb`, 0 (congelado). El perfil del servicio en sleep: ~30 % en llamadas a C (mpg123 y el envío a PulseAudio), **16 % en el medidor** (nivel y graves, que en sleep nadie mira) y el resto en la mezcla de oto. Desde entonces el medidor entero (no solo el espectro) solo se calcula con la app abierta y despierta. Falta medir con el método fiable (caída de `capacity`) y compararlo con Songo.

## Cómo medir

Ver la skill [`medir-consumo`](../.claude/skills/medir-consumo/SKILL.md). En resumen:

- **CPU de un proceso:** diferencia de `utime+stime` en `/proc/<pid>/stat` durante 20 s o más, dividida por `CLK_TCK` y el tiempo. `ps %CPU` es el promedio desde que arrancó el proceso e incluye el arranque; no sirve.
- **Batería, rápido:** `time_to_empty_now` cada 10 s durante 2–3 min. Se mueve despacio tras un cambio y salta mucho entre lecturas.
- **Batería, fiable:** cuánto tarda en bajar un 1 % `capacity` (1 % ≈ 30 mAh), mejor con varios puntos en una hora. El porcentaje sube durante la carga y la estimación se congela unos minutos al desconectar el cargador.
- **Frames realmente dibujados:** contar `FBIOPAN_DISPLAY` sobre `/dev/fb0` con `strace -f -p <pid> -e trace=ioctl`. El `fps:60.2` de `/sys/class/disp/disp/attr/sys` es el refresco del panel, no el de la aplicación.
