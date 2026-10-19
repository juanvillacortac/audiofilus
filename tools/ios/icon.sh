#!/bin/sh
# Hace el icono de la app de iOS (1024×1024) en pixel art: dibujado a 128 px
# y ampliado ×8 sin suavizar. El CD del logo (internal/desktop/icon.png, que
# es el de 32 px ×8) a ×3 con su sombra, sobre un fondo a sangre morado con
# los resplandores del splash (verde agua y rosa) tramados, y unos destellos
# "+" como los del logo.
#   tools/ios/icon.sh
set -eu
cd "$(dirname "$0")/../.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
magick internal/desktop/icon.png -sample 12.5% -sample 300% "$tmp/cd.png"
magick -size 128x128 xc:'#150f22' \
  \( -size 180x180 radial-gradient:'#2fbf9f'-'#150f22' -geometry -60+10 \) -compose screen -composite \
  \( -size 170x170 radial-gradient:'#e2589a'-'#150f22' -geometry +30-75 \) -compose screen -composite \
  -colorspace sRGB -ordered-dither o4x4,10 "$tmp/bg.png"
magick "$tmp/cd.png" -alpha extract -fill black -colorize 100 \( "$tmp/cd.png" -alpha extract \) \
  -compose copyopacity -composite -channel A -evaluate multiply 0.55 +channel "$tmp/shadow.png"
magick "$tmp/bg.png" "$tmp/shadow.png" -geometry +19+21 -compose over -composite \
  "$tmp/cd.png" -geometry +16+16 -compose over -composite \
  -fill '#ffffff' -draw "rectangle 104,14 104,22" -draw "rectangle 100,18 108,18" \
  -fill '#ffd6ea' -draw "rectangle 20,100 20,106" -draw "rectangle 17,103 23,103" \
  -fill '#ffffff' -draw "point 112,96" -draw "point 14,30" \
  -sample 800% -alpha off ios/Audiofilus/Assets.xcassets/AppIcon.appiconset/icon-1024.png
