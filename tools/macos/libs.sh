#!/bin/sh
# Compila con zig todos los decodificadores en C de la app de macOS, para
# Apple Silicon y para Intel, desde cualquier sistema (un Mac, Linux, el CI):
#   tools/macos/libs.sh <carpeta de salida>
# deja <salida>/aarch64/*.dylib y <salida>/x86_64/*.dylib con los nombres que
# busca internal/decode (libmpg123.0.dylib...), y en <salida>/licenses sus
# licencias y de dónde sale el código. tools/macos/dmg.sh los mete en la app.
#
# Aquí se compilan los que en Linux trae el sistema: mpg123 (MP3), opusfile
# con libogg y libopus dentro (Opus) y libxmp (módulos tracker). Los demás
# (libgme, fdk-aac, libavshim y libvgmshim) salen de sus tools/<nombre>/build.sh
# con el destino macos. Cada .dylib lleva todo dentro: solo depende de las
# bibliotecas del sistema, y su install name es @rpath/<nombre>, el de la app.
# Al solo enlazar, sin -O2: con él el enlazador de zig 0.15.2 se cae (SIGSEGV)
# con libxmp, y ahí no sirve de nada.
# Necesita zig (en el PATH, o ZIG), cmake, curl y tar.
set -eu
mkdir -p "${1:?usage: tools/macos/libs.sh <output dir>}"
out=$(cd "$1" && pwd)
tools=$(cd "$(dirname "$0")/.." && pwd)
ZIG=${ZIG:-zig}
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
export ZIG
MPG123_VERSION=1.32.10
OGG_VERSION=1.3.6
OPUS_VERSION=1.5.2
OPUSFILE_VERSION=0.12
XMP_VERSION=4.6.3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu)

get() { curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 "$1"; }
get "https://www.mpg123.de/download/mpg123-$MPG123_VERSION.tar.bz2" | tar xj -C "$work"
get "https://downloads.xiph.org/releases/ogg/libogg-$OGG_VERSION.tar.gz" | tar xz -C "$work"
get "https://downloads.xiph.org/releases/opus/opus-$OPUS_VERSION.tar.gz" | tar xz -C "$work"
get "https://downloads.xiph.org/releases/opus/opusfile-$OPUSFILE_VERSION.tar.gz" | tar xz -C "$work"
get "https://github.com/libxmp/libxmp/releases/download/libxmp-$XMP_VERSION/libxmp-$XMP_VERSION.tar.gz" | tar xz -C "$work"

# build <carpeta de cmake> <fuente> [opciones]: una estática con cmake, sin
# mostrar nada salvo los errores.
build() {
  b=$1 s=$2
  shift 2
  cmake -S "$s" -B "$b" "$@" >"$b.log" 2>&1 && cmake --build "$b" -j"$jobs" >>"$b.log" 2>&1 ||
    { grep -B2 -A4 -i "error" "$b.log" | head -40 >&2; exit 1; }
}

for arch in aarch64 x86_64; do
  bin="$work/bin-$arch" dir="$out/$arch" w="$work/$arch"
  mkdir -p "$bin" "$dir" "$w"
  target="$arch-macos.12.0-none"
  printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$target" >"$bin/cc"
  printf '#!/bin/sh\nexec %s c++ -target %s "$@"\n' "$ZIG" "$target" >"$bin/c++"
  printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
  printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
  chmod +x "$bin"/*
  set -- -DCMAKE_SYSTEM_NAME=Darwin -DCMAKE_SYSTEM_PROCESSOR="$arch" \
    -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
    -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DBUILD_SHARED_LIBS=OFF

  # mpg123, sin libout123 (la salida de audio y los programas).
  build "$w/mpg123" "$work/mpg123-$MPG123_VERSION/ports/cmake" "$@" -DBUILD_LIBOUT123=OFF
  "$bin/cc" -shared -Wl,-install_name,@rpath/libmpg123.0.dylib -o "$dir/libmpg123.0.dylib" \
    -Wl,-force_load,"$(find "$w/mpg123" -name 'libmpg123.a' | head -1)"

  # opusfile con libogg y libopus dentro. opusfile no trae cmake: sus cuatro
  # .c, sin http.c (las radios las baja audiofilus).
  build "$w/ogg" "$work/libogg-$OGG_VERSION" "$@" -DINSTALL_DOCS=OFF -DBUILD_TESTING=OFF
  build "$w/opus" "$work/opus-$OPUS_VERSION" "$@" -DOPUS_BUILD_TESTING=OFF -DOPUS_BUILD_PROGRAMS=OFF
  of="$work/opusfile-$OPUSFILE_VERSION"
  "$bin/cc" -shared -O2 -fPIC -Wl,-install_name,@rpath/libopusfile.0.dylib -o "$dir/libopusfile.0.dylib" \
    -I"$of/include" -I"$work/libogg-$OGG_VERSION/include" -I"$w/ogg/include" -I"$work/opus-$OPUS_VERSION/include" \
    "$of/src/info.c" "$of/src/internal.c" "$of/src/opusfile.c" "$of/src/stream.c" \
    "$(find "$w/opus" -name 'libopus.a' | head -1)" "$(find "$w/ogg" -name 'libogg.a' | head -1)"

  # libxmp, solo la estática. Sin -fvisibility=hidden (HAVE_VISIBILITY=OFF):
  # la estática no marca para exportar sus funciones, y en la .dylib
  # quedarían ocultas.
  build "$w/xmp" "$work/libxmp-$XMP_VERSION" "$@" -DBUILD_STATIC=ON -DBUILD_SHARED=OFF -DLIBXMP_PIC=ON \
    -DHAVE_VISIBILITY=OFF
  "$bin/cc" -shared -Wl,-install_name,@rpath/libxmp.4.dylib -o "$dir/libxmp.4.dylib" \
    -Wl,-force_load,"$(find "$w/xmp" -name 'libxmp.a' | head -1)"
  ls "$dir"/*.dylib
done

# Los que tienen su propio script, y sus licencias.
mkdir -p "$out/licenses"
for t in libgme:libgme.0 fdk-aac:libfdk-aac.2 ffmpeg:libavshim libvgm:libvgmshim; do
  name=${t%%:*} lib=${t#*:}
  "$tools/$name/build.sh" "$work/$name" macos >/dev/null
  for arch in aarch64 x86_64; do
    cp "$work/$name/macos.$arch/$lib.dylib" "$out/$arch/"
  done
  cp "$work/$name"/*-LICENSE.txt "$work/$name"/*-SOURCE.txt "$out/licenses/"
done
cp "$tools/libvgm/GPL-2.0.txt" "$out/licenses/"

# Las licencias de los de aquí (LGPL-2.1 mpg123, BSD libogg, libopus y
# opusfile, MIT libxmp) y de dónde sale el código.
cp "$work/mpg123-$MPG123_VERSION/COPYING" "$out/licenses/mpg123-LICENSE.txt"
cp "$work/libogg-$OGG_VERSION/COPYING" "$out/licenses/libogg-LICENSE.txt"
cp "$work/opus-$OPUS_VERSION/COPYING" "$out/licenses/opus-LICENSE.txt"
cp "$work/opusfile-$OPUSFILE_VERSION/COPYING" "$out/licenses/opusfile-LICENSE.txt"
cp "$work/libxmp-$XMP_VERSION/docs/COPYING" "$out/licenses/libxmp-LICENSE.txt"
{
  printf 'mpg123 %s (LGPL-2.1): https://www.mpg123.de/download/mpg123-%s.tar.bz2\n' "$MPG123_VERSION" "$MPG123_VERSION"
  printf 'opusfile %s, with libogg %s and opus %s built in (BSD): https://downloads.xiph.org/releases/\n' "$OPUSFILE_VERSION" "$OGG_VERSION" "$OPUS_VERSION"
  printf 'libxmp %s (MIT): https://github.com/libxmp/libxmp/releases/tag/libxmp-%s\n' "$XMP_VERSION" "$XMP_VERSION"
  printf 'Built by tools/macos/libs.sh: https://github.com/juanvillacortac/audiofilus/tree/main/tools/macos\n'
} >"$out/licenses/macos-SOURCE.txt"
