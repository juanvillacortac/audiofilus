#!/bin/sh
# Compila libfdk-aac (el decodificador AAC de Fraunhofer: M4A, M4B, radios AAC
# y HLS; ver internal/decode/aac.go) para las portátiles y el PC, para llevarla
# dentro del port de PortMaster y de la app de muOS: Knulli la trae, muOS y
# otros no, y sin ella los M4A y M4B no suenan.
#   tools/fdk-aac/build.sh <carpeta de salida> [linux|macos]
# linux (por defecto) deja <salida>/libs.aarch64/libfdk-aac.so.2 y
# <salida>/libs.x86_64/libfdk-aac.so.2; macos, en un Mac,
# <salida>/macos/libfdk-aac.2.dylib para su arquitectura (Homebrew ya no la
# trae). Además, la licencia y de dónde sale el código. build.sh los mete en
# los paquetes si están en build/fdk-aac; tools/macos/dmg.sh, en la app.
#
# Como tools/libgme/build.sh: con zig (mise install zig) contra una glibc
# vieja (2.17), con libc++ dentro: solo depende de la libc. Necesita zig (en
# el PATH, o ZIG), cmake, curl y tar; para macos, el clang de Xcode en vez de zig.
set -eu
mkdir -p "${1:?usage: tools/fdk-aac/build.sh <output dir> [linux|macos]}"
out=$(cd "$1" && pwd)
target=${2:-linux}
case $target in linux | macos) ;; *) echo "unknown target $target (linux or macos)" >&2; exit 2 ;; esac
ZIG=${ZIG:-zig}
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
FDK_VERSION=2.0.3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 "https://github.com/mstorsjo/fdk-aac/archive/refs/tags/v$FDK_VERSION.tar.gz" | tar xz -C "$work"
src="$work/fdk-aac-$FDK_VERSION"

# En macOS, con el clang del sistema para la arquitectura del Mac: una
# .dylib con todo dentro, que solo depende de las bibliotecas del sistema.
if [ "$target" = macos ]; then
  b="$work/build-macos"
  cmake -S "$src" -B "$b" -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DCMAKE_OSX_DEPLOYMENT_TARGET=12.0 -DBUILD_SHARED_LIBS=OFF -DBUILD_PROGRAMS=OFF >/dev/null 2>&1
  cmake --build "$b" -j"$(sysctl -n hw.ncpu)" >"$work/build-macos.log" 2>&1 || { grep -B2 -A4 "error:" "$work/build-macos.log" | head -40 >&2; exit 1; }
  mkdir -p "$out/macos"
  c++ -dynamiclib -O2 -mmacosx-version-min=12.0 -install_name @rpath/libfdk-aac.2.dylib \
    -o "$out/macos/libfdk-aac.2.dylib" -Wl,-force_load,"$(find "$b" -name 'libfdk-aac*.a' | head -1)"
  echo "$out/macos/libfdk-aac.2.dylib"
fi

[ "$target" = linux ] && for arch in aarch64 x86_64; do
  target="$arch-linux-gnu.2.17"
  bin="$work/bin-$arch"
  mkdir -p "$bin"
  printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$target" >"$bin/cc"
  printf '#!/bin/sh\nexec %s c++ -target %s "$@"\n' "$ZIG" "$target" >"$bin/c++"
  printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
  printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
  chmod +x "$bin"/*
  # Estática primero y luego una .so con todo dentro (libc++ incluida). Sin
  # -Werror por __DATE__: zig lo trata como error.
  b="$work/build-$arch"
  cmake -S "$src" -B "$b" -DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR="$arch" \
    -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
    -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" \
    -DCMAKE_C_FLAGS=-Wno-date-time -DCMAKE_CXX_FLAGS=-Wno-date-time \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF -DBUILD_PROGRAMS=OFF >/dev/null 2>&1
  cmake --build "$b" -j"$(nproc)" >"$work/build-$arch.log" 2>&1 || { grep -B2 -A4 "error:" "$work/build-$arch.log" | head -40 >&2; exit 1; }
  lib=$(find "$b" -name 'libfdk-aac*.a' | head -1)
  mkdir -p "$out/libs.$arch"
  "$bin/c++" -shared -O2 -s -Wl,-soname,libfdk-aac.so.2 -o "$out/libs.$arch/libfdk-aac.so.2" \
    -Wl,--whole-archive "$lib" -Wl,--no-whole-archive
  echo "$out/libs.$arch/libfdk-aac.so.2"
done
# La licencia (la de Fraunhofer, que pide acompañar al binario) y de dónde
# sale el código.
cp "$src/NOTICE" "$out/fdk-aac-LICENSE.txt"
printf 'fdk-aac %s: https://github.com/mstorsjo/fdk-aac/releases/tag/v%s\n' \
  "$FDK_VERSION" "$FDK_VERSION" >"$out/fdk-aac-SOURCE.txt"
