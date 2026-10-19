#!/bin/sh
# Rehace las imágenes del port (portmaster/audiofilus/cover.png y
# screenshot.png, 640×480): la portada la dibuja la interfaz (portart.go) y la
# captura es la pantalla de reproducción en 4:3. Las capturas salen ×3 de
# 320×240; se reducen a 1:1 tomando un píxel de cada tres (exacto, porque se
# ampliaron sin suavizar) y se amplían ×2.
#   tools/portmaster/art.sh <carpeta de música> [álbum]
set -eu
cd "$(dirname "$0")/../.."
music=${1:?usage: tools/portmaster/art.sh <music folder> [album]}
album=${2:-}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
./build.sh >/dev/null
dist/audiofilus -music "$music" -album "$album" -lang en -shots "$tmp" # en inglés: lo que ve la mayoría en PortMaster
for pair in portada-portmaster:cover reproduciendo-4x3:screenshot; do
  magick "$tmp/${pair%%:*}.png" -sample 33.3334% -sample 200% "portmaster/audiofilus/${pair##*:}.png"
done
file portmaster/audiofilus/cover.png portmaster/audiofilus/screenshot.png
