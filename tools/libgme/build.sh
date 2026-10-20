#!/bin/sh
# Compila libgme (Game_Music_Emu, la música de consolas viejas: ver
# internal/decode/gme.go) para las portátiles y el PC, para llevarla dentro
# del port de PortMaster y de la app de muOS, que no la traen:
#   tools/libgme/build.sh <carpeta de salida>
# deja <salida>/libs.aarch64/libgme.so.0, <salida>/libs.x86_64/libgme.so.0,
# la licencia y de dónde sale el código. build.sh los mete en los paquetes si
# están en build/libgme.
#
# Con zig como compilador (mise install zig): compila contra una glibc vieja
# (2.17), así la biblioteca carga en cualquier sistema, también en los
# antiguos como ArkOS, y lleva dentro libc++ y zlib (para VGZ): solo depende
# de la libc. Necesita zig (en el PATH, o ZIG), cmake, curl y tar.
set -eu
mkdir -p "${1:?usage: tools/libgme/build.sh <output dir>}"
out=$(cd "$1" && pwd)
ZIG=${ZIG:-zig}
# La ruta de verdad de zig: si es un shim de mise, desde las carpetas
# temporales de cmake no sabría qué versión usar.
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
GME_VERSION=0.6.5
ZLIB_VERSION=1.3.1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -sSfL "https://github.com/libgme/game-music-emu/archive/refs/tags/$GME_VERSION.tar.gz" | tar xz -C "$work"
curl -sSfL "https://github.com/madler/zlib/releases/download/v$ZLIB_VERSION/zlib-$ZLIB_VERSION.tar.gz" | tar xz -C "$work"

for arch in aarch64 x86_64; do
  target="$arch-linux-gnu.2.17"
  bin="$work/bin-$arch"
  mkdir -p "$bin"
  # cmake quiere cada herramienta como un solo programa: envoltorios de zig.
  printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$target" >"$bin/cc"
  printf '#!/bin/sh\nexec %s c++ -target %s "$@"\n' "$ZIG" "$target" >"$bin/c++"
  printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
  printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
  chmod +x "$bin"/*
  set -- -DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR="$arch" \
    -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
    -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_SHARED_LINKER_FLAGS=-s

  # zlib, estática, para meterla dentro de libgme.
  zb="$work/zlib-$arch"
  cmake -S "$work/zlib-$ZLIB_VERSION" -B "$zb" "$@" -DZLIB_BUILD_EXAMPLES=OFF >/dev/null
  cmake --build "$zb" --target zlibstatic -j"$(nproc)" >/dev/null
  zlib_a=$(find "$zb" -name 'libz*.a' | head -1)

  gb="$work/gme-$arch"
  cmake -S "$work/game-music-emu-$GME_VERSION" -B "$gb" "$@" -DBUILD_SHARED_LIBS=ON -DENABLE_UBSAN=OFF \
    -DGME_BUILD_TESTING=OFF -DGME_BUILD_EXAMPLES=OFF -DGME_ZLIB=ON \
    -DZLIB_INCLUDE_DIR="$work/zlib-$ZLIB_VERSION;$zb" -DZLIB_LIBRARY="$zlib_a" >/dev/null
  cmake --build "$gb" -j"$(nproc)" >/dev/null
  so=$(find "$gb" -name 'libgme.so.0*' -type f | head -1)
  mkdir -p "$out/libs.$arch"
  cp "$so" "$out/libs.$arch/libgme.so.0"
  echo "$out/libs.$arch/libgme.so.0"
done
# La licencia (LGPL 2.1) y de dónde sale el código, para llevarlas con ella.
src="$work/game-music-emu-$GME_VERSION"
for f in license.txt COPYING LICENSE.txt; do
  [ -f "$src/$f" ] && cp "$src/$f" "$out/libgme-LICENSE.txt" && break
done
printf 'libgme %s (LGPL 2.1): https://github.com/libgme/game-music-emu/releases/tag/%s\nzlib %s (zlib license), built in: https://zlib.net\n' \
  "$GME_VERSION" "$GME_VERSION" "$ZLIB_VERSION" >"$out/libgme-SOURCE.txt"
