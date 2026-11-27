#!/bin/sh
# Compila libvgm (ValleyBell: emula casi todos los chips de sonido de los VGM,
# también los de las placas arcade, la NES y la Game Boy, que libgme no; ver
# internal/decode/vgm.go) con la capa en C de shim.cpp, como una sola
# biblioteca para las portátiles y el PC:
#   tools/libvgm/build.sh <carpeta de salida> [linux|macos]
# linux (por defecto) deja <salida>/libs.aarch64/libvgmshim.so y
# <salida>/libs.x86_64/libvgmshim.so; macos, <salida>/macos.aarch64/ y
# <salida>/macos.x86_64/libvgmshim.dylib. Además, las licencias y de dónde
# sale el código. build.sh los mete en los paquetes si están en build/libvgm;
# tools/macos/libs.sh junta las de macOS.
#
# Como tools/libgme/build.sh: con zig (mise install zig) contra una glibc
# vieja (2.17), o macOS 12, con libc++, libvgm y zlib dentro: solo depende de
# la libc (y en macOS de libiconv, que traen todos). Desde cualquier sistema,
# para los dos. Necesita zig (en el PATH, o ZIG), cmake, curl y tar.
set -eu
mkdir -p "${1:?usage: tools/libvgm/build.sh <output dir> [linux|macos]}"
out=$(cd "$1" && pwd)
os=${2:-linux}
case $os in linux | macos) ;; *) echo "unknown target $os (linux or macos)" >&2; exit 2 ;; esac
here=$(cd "$(dirname "$0")" && pwd)
ZIG=${ZIG:-zig}
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
# libvgm no tiene versiones: un commit fijo.
VGM_COMMIT=a6eed945ecf4e5a25cf82004779c6f7e4fd5c319
ZLIB_VERSION=1.3.1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu)

curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 "https://github.com/ValleyBell/libvgm/archive/$VGM_COMMIT.tar.gz" | tar xz -C "$work"
curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 "https://github.com/madler/zlib/releases/download/v$ZLIB_VERSION/zlib-$ZLIB_VERSION.tar.gz" | tar xz -C "$work"
src="$work/libvgm-$VGM_COMMIT"

for arch in aarch64 x86_64; do
  if [ "$os" = macos ]; then
    target="$arch-macos.12.0-none" system=Darwin lib="$out/macos.$arch/libvgmshim.dylib"
  else
    target="$arch-linux-gnu.2.17" system=Linux lib="$out/libs.$arch/libvgmshim.so"
  fi
  bin="$work/bin-$arch"
  mkdir -p "$bin"
  printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$target" >"$bin/cc"
  printf '#!/bin/sh\nexec %s c++ -target %s "$@"\n' "$ZIG" "$target" >"$bin/c++"
  printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
  printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
  chmod +x "$bin"/*
  set -- -DCMAKE_SYSTEM_NAME=$system -DCMAKE_SYSTEM_PROCESSOR="$arch" \
    -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
    -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON

  zb="$work/zlib-$arch"
  cmake -S "$work/zlib-$ZLIB_VERSION" -B "$zb" "$@" -DZLIB_BUILD_EXAMPLES=OFF >/dev/null
  cmake --build "$zb" --target zlibstatic -j"$jobs" >/dev/null
  zlib_a=$(find "$zb" -name 'libz*.a' | head -1)

  # libvgm, estática y sin lo que no hace falta (la salida de audio, los
  # programas de ejemplo). iconv se dice a mano, porque al compilar para otro
  # sistema cmake no lo puede comprobar: en Linux viene en la glibc; en macOS
  # se enlaza abajo.
  vb="$work/vgm-$arch"
  cmake -S "$src" -B "$vb" "$@" -DBUILD_SHARED_LIBS=OFF -DBUILD_LIBAUDIO=OFF -DBUILD_TESTS=OFF \
    -DBUILD_PLAYER=OFF -DBUILD_VGM2WAV=OFF -DUSE_SANITIZERS=OFF -DUTIL_CHARCNV_ICONV=ON \
    -DIconv_IS_BUILT_IN=TRUE -DIconv_LIBRARY=c \
    -DZLIB_INCLUDE_DIR="$work/zlib-$ZLIB_VERSION;$zb" -DZLIB_LIBRARY="$zlib_a" >/dev/null
  cmake --build "$vb" -j"$jobs" >/dev/null
  libs=$(find "$vb" -name 'libvgm-player*.a' -o -name 'libvgm-emu*.a' -o -name 'libvgm-utils*.a' | sort)

  # La capa en C y todo dentro de una biblioteca: el orden importa (player
  # usa emu y utils).
  mkdir -p "$(dirname "$lib")"
  set -- -I"$src" -I"$vb" "$here/shim.cpp" \
    $(echo "$libs" | grep player) $(echo "$libs" | grep emu) $(echo "$libs" | grep utils) "$zlib_a"
  if [ "$os" = macos ]; then
    # zig trae libSystem pero no libiconv: libiconv.tbd le dice que iconv
    # está en /usr/lib/libiconv.2.dylib. El install name con @rpath, como el
    # resto de la app.
    "$bin/c++" -shared -O2 -Wl,-install_name,@rpath/libvgmshim.dylib -o "$lib" "$@" -L"$here" -liconv
  else
    "$bin/c++" -shared -O2 -fPIC -s -o "$lib" "$@" -Wl,--no-undefined -lm -lpthread
  fi
  echo "$lib"
done
# libvgm no trae una licencia para todo: cada archivo dice la suya en su
# cabecera (la mayoría BSD-3-Clause; algunos núcleos GPL-2.0 o LGPL-2.1+,
# así que la biblioteca entera va con la GPL-2.0).
# libvgm-LICENSE.txt las lista, sacadas del código de este commit.
{
  printf 'libvgm %s has no single license: each source file states its own.\n' "$VGM_COMMIT"
  printf 'Some sound cores are under the GPL (version 2, or 2 or later) or the LGPL-2.1+, so\n'
  printf 'libvgmshim.so as a whole is distributed under the GNU GPL version 2. Source: libvgm-SOURCE.txt.\n\n'
  (cd "$src" && for f in $(find emu player utils -name '*.c' -o -name '*.cpp' | sort); do
    l=$(grep -o -m1 'license:[A-Za-z0-9.+-]*' "$f" | sed 's/license://')
    [ -z "$l" ] && grep -q -m1 'General Public License' "$f" && l="GPL/LGPL (see file header)"
    printf '%s: %s\n' "$f" "${l:-no license header}"
  done)
} >"$out/libvgm-LICENSE.txt"
printf 'libvgm %s (licenses per file: see libvgm-LICENSE.txt): https://github.com/ValleyBell/libvgm/tree/%s\nshim.cpp (MIT, audiofilus): tools/libvgm/shim.cpp\nzlib %s (zlib license), built in: https://zlib.net\n' \
  "$VGM_COMMIT" "$VGM_COMMIT" "$ZLIB_VERSION" >"$out/libvgm-SOURCE.txt"
