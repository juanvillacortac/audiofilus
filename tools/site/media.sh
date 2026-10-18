#!/bin/sh
# Rehace las imágenes de la web y del README (site/img): capturas en inglés,
# GIFs de los visualizadores, juegos y reproductor, y el logo (fijo y la
# entrada animada del splash, con fondo transparente). Todo sale de la propia
# interfaz con -shots y -frames, ×3 de 320×240, y se reduce sin suavizar.
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

# El logo: fijo (recortado) y la entrada del splash, que se reproduce una vez.
# El recorte de la animación es el mismo en todos los frames (el de su unión)
# para que no salte; -dispose Background evita restos de frames anteriores.
magick "$tmp/s/logo.png" -trim +repage -filter point -resize 66.667% "$out/logo.png"
box=$(magick "$tmp"/f/logo/f*.png -layers trim-bounds -flatten -trim -format '%wx%h%O' info:)
magick -dispose Background -delay 3.333 "$tmp"/f/logo/f*.png -crop "$box" +repage \
  -filter point -resize 66.667% -loop 1 -define webp:lossless=true "$out/logo-intro.webp"
magick "$tmp/s/portada-portmaster.png" -filter point -resize 50% "$out/og.png"
ls -la "$out"
