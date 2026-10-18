# El port de PortMaster

`./build.sh` deja, además de los binarios, `dist/audiofilus.zip`: el port para las portátiles Linux con PortMaster. Lleva lo de `portmaster/`, los binarios de arm64 y las licencias de las fuentes (la OFL pide que viajen con el programa):

```
Audiofilus.sh              el lanzador (comentarios en inglés, como los de PortMaster)
gameinfo.xml               para el menú de EmulationStation
audiofilus/port.json       los datos del port para PortMaster
audiofilus/cover.png       la portada (640×480)
audiofilus/screenshot.png  una captura (640×480)
audiofilus/audiofilus      el servicio de audio (arm64)
audiofilus/audiofilus-ui   la interfaz (arm64)
audiofilus/licenses/       OFL de Jersey 10 y Tiny5
```

## El lanzador

- Usa el `control.txt` de PortMaster como cualquier port (`get_controls`, `$GPTOKEYB`, `pm_finish`).
- `AUDIOFILUS_HOME` apunta a la carpeta del port: `audiofilus.json`, `data/` y el registro del servicio (`data/audiofilus.log`) quedan ahí. `log.txt` es la salida de la interfaz.
- `EBITENGINE_UI_BACKEND=sdl2` y `SDL_GAMECONTROLLERCONFIG` con el mapeo de PortMaster para la consola; el servicio, que arranca la interfaz, hereda ese entorno.
- gptokeyb solo da el atajo de salida de PortMaster (Select+Start): mata la interfaz y, como no se pidió segundo plano, el servicio para la música.
- En ArkOS y dArkOS restaura `~/.asoundrc` si falta: allí no hay PulseAudio y el audio va por ALSA.

## Las imágenes

`tools/portmaster/art.sh <carpeta de música> [álbum]` rehace `cover.png` y `screenshot.png`:

- La **portada** la dibuja la interfaz (`internal/ui/portart.go`): un vinilo que sale de su funda, el logo del splash con su ecualizador, la frase y la firma. No usa portadas de discos de verdad, para poder publicarla. La funda es un degradado de cinco colores con trama de Bayer, como el resto de la estética.
- La **captura** es la pantalla de reproducción en 4:3, **en inglés** (`-lang en`; es lo que ve la mayoría en PortMaster), con el álbum que se pida (`-album`, p. ej. `"Wonder What"`).

Las capturas salen ×3 de 320×240; el script las baja a 1:1 tomando un píxel de cada tres (exacto, porque se ampliaron sin suavizar) y las sube ×2. Antes de las capturas se dibuja todo una vez y se tira: el primer uso de un shader en el proceso a veces no llegaba a la imagen (salía la captura sin vinilo).

**Para publicar el port**, la captura no puede llevar portadas de discos ajenos: hay que hacerla con música propia o libre.

## Instalar en la consola

Copiar `dist/audiofilus.zip` a la carpeta `autoinstall` de PortMaster y abrir PortMaster, que lo instala en `ports/`. Ver la skill [`probar-en-knulli`](../.claude/skills/probar-en-knulli/SKILL.md).

## Otros sistemas

Solo se ha probado en Knulli. Lo que no depende del sistema (el servicio, la interfaz, el segundo plano explícito, los atajos FN×3, el sleep OLED) debería ir en todos. Lo de impedir la suspensión, apagar la retroiluminación y capturar los botones de volumen solo está hecho para Knulli (`internal/platform/knulli.go`); en los demás no hace nada. ROCKNIX y muOS usan PipeWire con compatibilidad de PulseAudio; ArkOS y dArkOS, ALSA directo, donde la música en segundo plano y el sonido del juego podrían no poder sonar a la vez.
