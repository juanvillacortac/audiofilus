#!/bin/sh
# Rehace las imágenes de la web y del README (site/img): capturas en inglés,
# animaciones de los visualizadores, juegos y reproductor, el logo y el
# splash. Todo sale de la propia interfaz con -shots y -frames (×3); se lleva
# a 1:1 y se publica a ×4 con píxeles duros: el navegador solo lo reduce,
# suavizado, y así se ve parejo a cualquier tamaño y escala de pantalla.
#   tools/site/media.sh <carpeta de música> [álbum]
set -eu
cd "$(dirname "$0")/../.."
music=${1:?usage: tools/site/media.sh <music folder> [album]}
album=${2:-}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
out=site/img
./build.sh >/dev/null
dist/audiofilus -music "$music" -album "$album" -lang en -shots "$tmp/s" -frames "$tmp/f"

# Capturas fijas, a ×4 (1440×960).
for pair in reproduciendo:player albumes:albums buscar-youtube:search-youtube \
  reproduciendo-radio:radio reproduciendo-streaming:streaming guia:guide menu:menu \
  pantallas:dock lista-anadir:playlist-add cola:queue audiolibro:audiobook audiolibro-capitulos:audiobook-chapters \
  audiolibro-menu:audiobook-menu alarma:alarm autoeq:autoeq autoeq-aplicado:autoeq-applied color:theme-color; do
  magick "$tmp/s/${pair%%:*}-3x2.png" -sample 33.3334% -sample 400% -strip "$out/${pair##*:}.png"
done
for r in 1x1 4x3 3x2 16x9 21x9; do
  magick "$tmp/s/reproduciendo-$r.png" -sample 33.3334% -sample 400% -strip "$out/width-$r.png"
done

# Las animaciones, en WebP sin pérdida a ×2 (a 20 cuadros por segundo): a ×4
# el navegador tarda en decodificar varias a la vez y se ven lentas.
anim() { magick -delay "$1" $2 -sample 33.3334% -sample 200% -define webp:lossless=true -loop 0 "$out/$3"; }
for v in lake drift trench plasma tunnel waves bars ridges; do anim 5 "$tmp/f/visual/$v/f*.png" "vis-$v.webp"; done
# MilkDrop, con pérdida: sus degradados sin pérdida pesan más de 3 MB.
magick -delay 5 "$tmp/f/visual/milk/kaleido-bloom/f*.png" -sample 33.3334% -sample 200% -quality 65 -define webp:method=6 -loop 0 "$out/vis-milkdrop.webp"
anim 5 "$tmp/f/ritmo/f*.png" rhythm.webp
anim 5 "$tmp/f/bloques/f*.png" blocks.webp
anim 5 "$tmp/f/f0*.png" player.webp
anim 5 "$tmp/f/cripta/f*.png" crypt.webp
anim 5 "$tmp/f/colores/f*.png" colors.webp
magick "$tmp/s/juegos-3x2.png" -sample 33.3334% -sample 400% -strip "$out/games.png"
anim 5 "$tmp/f/tracker/f*.png" tracker.webp
anim 5 "$tmp/f/serpiente/f*.png" snake.webp
anim 5 "$tmp/f/naves/f*.png" ships.webp
anim 5 "$tmp/f/duelo/f*.png" duel.webp
anim 5 "$tmp/f/carrera/f*.png" runner.webp
anim 5 "$tmp/f/letras/f*.png" lyrics.webp
magick "$tmp/s/adivina-3x2.png" -sample 33.3334% -sample 400% -strip "$out/quiz.png"

# El móvil: como en un iPhone 15 (zona segura simulada, modo táctil y sin lo
# que muestra el sistema), Reproduciendo, la biblioteca y el desplegable de
# pestañas.
AUDIOFILUS_MOBILE=1 AUDIOFILUS_TOUCH=1 AUDIOFILUS_SAFE=36,21 \
  dist/audiofilus -music "$music" -album "$album" -lang en -shots "$tmp/m" -frames "$tmp/mf"
magick "$tmp/m/reproduciendo-iphone.png" -sample 33.3334% -sample 200% -strip "$out/phone-player.png"
magick "$tmp/m/canciones-iphone.png" -sample 33.3334% -sample 200% -strip "$out/phone-library.png"
anim 5 "$tmp/mf/pestanas/f*.png" phone-tabs.webp

# El logo, recortado, y el splash de la web en sus dos capas (el fondo y lo
# de encima, sobre transparente), a 30 cuadros por segundo y a 1:1, que se
# reproducen una vez (WebP sin pérdida).
magick "$tmp/s/logo.png" -trim +repage -sample 33.3334% -sample 400% "$out/logo.png"
magick -dispose Background -delay 3.333 "$tmp"/f/splash-bg/f*.png -sample 33.3334% \
  -loop 1 -define webp:lossless=true "$out/splash-bg.webp"
# Lo de encima, recortado a su contenido (el mismo recorte en todos los cuadros).
box=$(magick "$tmp"/f/splash-fg/f*.png -layers trim-bounds -flatten -trim -format '%wx%h%O' info:)
magick -dispose Background -delay 3.333 "$tmp"/f/splash-fg/f*.png -crop "$box" +repage \
  -sample 33.3334% -sample 400% -loop 1 -define webp:lossless=true "$out/splash-fg.webp"
magick "$tmp/s/portada-portmaster.png" -filter point -resize 50% "$out/og.png"
ls -la "$out"
