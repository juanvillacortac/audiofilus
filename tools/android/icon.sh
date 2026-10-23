#!/bin/sh
# Hace el icono de la app de Android en pixel art, como el de iOS
# (tools/ios/icon.sh): un icono adaptativo de 108×108 dp dibujado en 72 px
# de arte (1 px = 1,5 dp) y ampliado sin suavizar a cada densidad.
#   - fondo: el morado a sangre con los resplandores del splash (verde agua y
#     rosa) tramados; cada launcher lo recorta con su forma;
#   - frente: el CD del logo (internal/desktop/icon.png, el de 32 px ×8) a ×1
#     (48 dp) con su sombra y los destellos "+", dentro de los 66 dp del
#     centro que ningún recorte toca; los launchers muestran unos 72 dp;
#   - monocromo: la silueta del CD, para los iconos temáticos (Android 13+);
#   - ic_launcher.png: los tres juntos en un cuadrado redondeado, para
#     Android 7 (sin iconos adaptativos);
#   - banner.png: el de Android TV (320×180), en 160×90 de arte ×2: el mismo
#     fondo, el CD y "audiofilus" en Jersey 10 (la fuente de los títulos).
#   tools/android/icon.sh
set -eu
cd "$(dirname "$0")/../.."
res=android/app/src/main/res
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

magick internal/desktop/icon.png -sample 12.5% "$tmp/cd.png" # 32×32
magick -size 72x72 xc:'#150f22' \
  \( -size 100x100 radial-gradient:'#2fbf9f'-black -geometry -37+3 \) -compose screen -composite \
  \( -size 94x94 radial-gradient:'#e2589a'-black -geometry +17-43 \) -compose screen -composite \
  -colorspace sRGB -ordered-dither o4x4,10 "$tmp/bg.png"
magick "$tmp/cd.png" -alpha extract -fill black -colorize 100 \( "$tmp/cd.png" -alpha extract \) \
  -compose copyopacity -composite -channel A -evaluate multiply 0.55 +channel "$tmp/shadow.png"
magick -size 72x72 xc:none \
  "$tmp/shadow.png" -geometry +22+23 -compose over -composite \
  "$tmp/cd.png" -geometry +20+20 -compose over -composite \
  -fill '#ffffff' -draw "rectangle 50,20 50,24" -draw "rectangle 48,22 52,22" \
  -fill '#ffd6ea' -draw "rectangle 23,48 23,52" -draw "rectangle 21,50 25,50" \
  -fill '#ffffff' -draw "point 53,48" -draw "point 19,25" "$tmp/fg.png"
magick -size 72x72 xc:none "$tmp/cd.png" -geometry +20+20 -compose over -composite \
  -alpha extract -background white -alpha shape "$tmp/mono.png"
# El de Android 7: el centro (48 px, 72 dp) de fondo y frente, con esquinas
# redondeadas.
magick "$tmp/bg.png" "$tmp/fg.png" -compose over -composite -gravity center -crop 50x50+0+0 +repage \
  \( -size 50x50 xc:none -fill white -draw "roundrectangle 0,0 49,49 9,9" \) \
  -compose dstin -composite "$tmp/legacy.png"

for d in mdpi:1 hdpi:1.5 xhdpi:2 xxhdpi:3 xxxhdpi:4; do
  n=${d%%:*} f=${d##*:}
  dir=$res/mipmap-$n
  mkdir -p "$dir"
  pct=$(awk "BEGIN{print $f*150}") # 72 px de arte -> 108 dp
  for l in bg fg mono; do
    magick "$tmp/$l.png" -sample "$pct%" "$dir/ic_launcher_$l.png"
  done
  # 48 dp por densidad.
  magick "$tmp/legacy.png" -filter point -resize "$(awk "BEGIN{print int(48*$f)}")x" "$dir/ic_launcher.png"
done
mkdir -p "$res/mipmap-anydpi-v26"
cat >"$res/mipmap-anydpi-v26/ic_launcher.xml" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<!-- Lo genera tools/android/icon.sh. -->
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@mipmap/ic_launcher_bg" />
    <foreground android:drawable="@mipmap/ic_launcher_fg" />
    <monochrome android:drawable="@mipmap/ic_launcher_mono" />
</adaptive-icon>
EOF
# El banner de Android TV.
magick -size 160x90 xc:'#150f22' \
  \( -size 150x150 radial-gradient:'#2fbf9f'-black -geometry -60-10 \) -compose screen -composite \
  \( -size 140x140 radial-gradient:'#e2589a'-black -geometry +80-75 \) -compose screen -composite \
  -colorspace sRGB -ordered-dither o4x4,10 "$tmp/banner-bg.png"
magick -background none +antialias -fill '#f2f0ea' -font internal/ui/assets/fonts/Jersey10-Regular.ttf \
  -pointsize 18.667 -density 72 label:audiofilus -trim +repage "$tmp/title.png"
tw=$(magick identify -format %w "$tmp/title.png")
th=$(magick identify -format %h "$tmp/title.png")
gap=6
x0=$(( (160 - 32 - gap - tw) / 2 ))
ty=$(( (90 - th) / 2 ))
magick "$tmp/banner-bg.png" \
  "$tmp/shadow.png" -geometry +$((x0 + 2))+31 -compose over -composite \
  "$tmp/cd.png" -geometry +$x0+29 -compose over -composite \
  \( "$tmp/title.png" -fill black -colorize 100 -channel A -evaluate multiply 0.55 +channel \) -geometry +$((x0 + 32 + gap + 1))+$((ty + 1)) -compose over -composite \
  "$tmp/title.png" -geometry +$((x0 + 32 + gap))+$ty -compose over -composite \
  -fill '#ffffff' -draw "rectangle $((x0 + 30)),24 $((x0 + 30)),28" -draw "rectangle $((x0 + 28)),26 $((x0 + 32)),26" \
  -sample 200% -depth 8 "$res/drawable-xhdpi/banner.png"
magick "$tmp/bg.png" "$tmp/fg.png" -compose over -composite -sample 600% "$tmp/preview.png"
[ -n "${PREVIEW:-}" ] && cp "$tmp/preview.png" "$PREVIEW" && cp "$res/drawable-xhdpi/banner.png" "${PREVIEW%.png}-banner.png"
echo "$res/mipmap-anydpi-v26/ic_launcher.xml"
