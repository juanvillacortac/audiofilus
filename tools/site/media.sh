#!/bin/sh
# Rehace las imágenes de la web y del README (site/img): capturas en inglés,
# GIFs de los visualizadores, juegos y reproductor, el logo y el splash. Todo
# sale de la propia interfaz con -shots y -frames, ×3 de 320×240, y se reduce
# sin suavizar.
#   tools/site/media.sh <carpeta de música> [álbum]
set -eu
cd "$(dirname "$0")/../.."
music=${1:?usage: tools/site/media.sh <music folder> [album]}
album=${2:-}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
out=site/img
./build.sh >/dev/null
dist/audiofilus-ui -music "$music" -album "$album" -lang en -shots "$tmp/s" -frames "$tmp/f"

# Capturas fijas, a ×2 (720×480).
for pair in reproduciendo:player albumes:albums buscar-youtube:search-youtube \
  reproduciendo-radio:radio reproduciendo-streaming:streaming guia:guide menu:menu \
  pantallas:dock lista-anadir:playlist-add cola:queue; do
  magick "$tmp/s/${pair%%:*}-3x2.png" -filter point -resize 720x480 -strip "$out/${pair##*:}.png"
done
for r in 1x1 4x3 3x2 16x9 21x9; do
  magick "$tmp/s/reproduciendo-$r.png" -filter point -resize x480 -strip "$out/width-$r.png"
done

# GIFs a 1:1 (320×240); la web los amplía con image-rendering: pixelated.
gif() { magick -delay "$1" -loop 0 $2 -filter point -resize 320x240 -layers Optimize "$out/$3"; }
for v in lake drift trench plasma tunnel waves bars; do gif 5 "$tmp/f/visual/$v/f*.png" "vis-$v.gif"; done
gif 3 "$tmp/f/splash/f*.png" splash.gif
gif 5 "$tmp/f/ritmo/f*.png" rhythm.gif
gif 5 "$tmp/f/bloques/f*.png" blocks.gif
gif 5 "$tmp/f/f0*.png" player.gif

# El logo, recortado, y el splash de la web: el de la app en 16:9 a 30
# cuadros por segundo, a 1:1, que se reproduce una vez (WebP sin pérdida).
magick "$tmp/s/logo.png" -trim +repage -filter point -resize 66.667% "$out/logo.png"
magick -dispose Background -delay 3.333 "$tmp"/f/splash-web/f*.png -sample 33.3334% \
  -loop 1 -define webp:lossless=true "$out/splash-web.webp"
magick "$tmp/s/portada-portmaster.png" -filter point -resize 50% "$out/og.png"
ls -la "$out"
