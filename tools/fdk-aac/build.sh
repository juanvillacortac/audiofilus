#!/bin/sh
# Compila libfdk-aac (el decodificador AAC de Fraunhofer: M4A, M4B, radios AAC
# y HLS; ver internal/decode/aac.go) para las portátiles y el PC, para llevarla
# dentro del port de PortMaster y de la app de muOS: Knulli la trae, muOS y
# otros no, y sin ella los M4A y M4B no suenan.
#   tools/fdk-aac/build.sh <carpeta de salida> [linux|macos]
# linux (por defecto) deja <salida>/libs.aarch64/libfdk-aac.so.2 y
# <salida>/libs.x86_64/libfdk-aac.so.2; macos, <salida>/macos.aarch64/ y
# <salida>/macos.x86_64/libfdk-aac.2.dylib (Homebrew ya no la trae). Además,
# la licencia y de dónde sale el código. build.sh los mete en los paquetes si
# están en build/fdk-aac; tools/macos/libs.sh junta las de macOS.
#
# Como tools/libgme/build.sh: con zig (mise install zig) contra una glibc
# vieja (2.17), o macOS 12, con libc++ dentro: solo depende de la libc. Desde
# cualquier sistema, para los dos. Necesita zig (en el PATH, o ZIG), cmake,
# curl y tar.
set -eu
mkdir -p "${1:?usage: tools/fdk-aac/build.sh <output dir> [linux|macos]}"
out=$(cd "$1" && pwd)
os=${2:-linux}
case $os in linux | macos) ;; *) echo "unknown target $os (linux or macos)" >&2; exit 2 ;; esac
ZIG=${ZIG:-zig}
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
FDK_VERSION=2.0.3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
jobs=$(nproc 2>/dev/null || sysctl -n hw.ncpu)

curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 "https://github.com/mstorsjo/fdk-aac/archive/refs/tags/v$FDK_VERSION.tar.gz" | tar xz -C "$work"
src="$work/fdk-aac-$FDK_VERSION"

for arch in aarch64 x86_64; do
  if [ "$os" = macos ]; then
    target="$arch-macos.12.0-none" system=Darwin dir="$out/macos.$arch" lib=libfdk-aac.2.dylib
  else
    target="$arch-linux-gnu.2.17" system=Linux dir="$out/libs.$arch" lib=libfdk-aac.so.2
  fi
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
  cmake -S "$src" -B "$b" -DCMAKE_SYSTEM_NAME=$system -DCMAKE_SYSTEM_PROCESSOR="$arch" \
    -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
    -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" \
    -DCMAKE_C_FLAGS=-Wno-date-time -DCMAKE_CXX_FLAGS=-Wno-date-time \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF -DBUILD_PROGRAMS=OFF >/dev/null 2>&1
  cmake --build "$b" -j"$jobs" >"$work/build-$arch.log" 2>&1 || { grep -B2 -A4 "error:" "$work/build-$arch.log" | head -40 >&2; exit 1; }
  a=$(find "$b" -name 'libfdk-aac*.a' | head -1)
  mkdir -p "$dir"
  if [ "$os" = macos ]; then
    # En macOS el nombre con el que se carga es el install name (@rpath, el
    # de la app), y -force_load hace lo de --whole-archive.
    "$bin/c++" -shared -Wl,-install_name,@rpath/$lib -o "$dir/$lib" -Wl,-force_load,"$a"
  else
    "$bin/c++" -shared -O2 -s -Wl,-soname,$lib -o "$dir/$lib" -Wl,--whole-archive "$a" -Wl,--no-whole-archive
  fi
  echo "$dir/$lib"
done
# La licencia (la de Fraunhofer, que pide acompañar al binario) y de dónde
# sale el código.
cp "$src/NOTICE" "$out/fdk-aac-LICENSE.txt"
printf 'fdk-aac %s: https://github.com/mstorsjo/fdk-aac/releases/tag/v%s\n' \
  "$FDK_VERSION" "$FDK_VERSION" >"$out/fdk-aac-SOURCE.txt"
