#!/bin/sh
# Compila libfdk-aac-2.dll (el decodificador AAC de Fraunhofer: radios AAC y
# HLS, M4A) para Windows con zig, con libc++ dentro: no depende de nada más
# que de Windows. MSYS2 dejó de publicarla. La llama dlls.sh.
#   tools/windows/fdk-aac.sh <carpeta de salida>
# deja <salida>/libfdk-aac-2.dll y <salida>/licenses/fdk-aac/NOTICE (su
# licencia, que pide acompañar al binario).
set -eu
mkdir -p "${1:?usage: tools/windows/fdk-aac.sh <output dir>}"
out=$(cd "$1" && pwd)
ZIG=${ZIG:-zig}
real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$real" ] && ZIG=$real
FDK_VERSION=2.0.3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -sSfL "https://github.com/mstorsjo/fdk-aac/archive/refs/tags/v$FDK_VERSION.tar.gz" | tar xz -C "$work"
src="$work/fdk-aac-$FDK_VERSION"
bin="$work/bin"
mkdir -p "$bin"
target=x86_64-windows-gnu
printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$target" >"$bin/cc"
printf '#!/bin/sh\nexec %s c++ -target %s "$@"\n' "$ZIG" "$target" >"$bin/c++"
printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
printf '#!/bin/sh\nexec %s rc "$@"\n' "$ZIG" >"$bin/rc"
chmod +x "$bin"/*

# Estática primero (solo el decodificador hace falta, pero el encoder no
# molesta) y luego una DLL que exporta todo. Sin -Werror por __DATE__: zig
# lo trata como error (compilación no reproducible).
cmake -S "$src" -B "$work/build" -DCMAKE_SYSTEM_NAME=Windows -DCMAKE_SYSTEM_PROCESSOR=x86_64 \
  -DCMAKE_C_COMPILER="$bin/cc" -DCMAKE_CXX_COMPILER="$bin/c++" \
  -DCMAKE_AR="$bin/ar" -DCMAKE_RANLIB="$bin/ranlib" -DCMAKE_RC_COMPILER="$bin/rc" \
  -DCMAKE_C_FLAGS=-Wno-date-time -DCMAKE_CXX_FLAGS=-Wno-date-time \
  -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DBUILD_PROGRAMS=OFF >/dev/null 2>&1
cmake --build "$work/build" -j"$(nproc)" >"$work/build.log" 2>&1 || { grep -B2 -A4 "error:" "$work/build.log" | head -40 >&2; exit 1; }
lib=$(find "$work/build" -name 'libfdk-aac*.a' | head -1)
"$bin/c++" -shared -O2 -s -o "$out/libfdk-aac-2.dll" \
  -Wl,--whole-archive "$lib" -Wl,--no-whole-archive -Wl,--export-all-symbols
rm -f "$out"/libfdk-aac*.lib # la biblioteca de importación, que aquí no sirve
mkdir -p "$out/licenses/fdk-aac"
cp "$src/NOTICE" "$out/licenses/fdk-aac/NOTICE"
echo "fdk-aac $FDK_VERSION (https://github.com/mstorsjo/fdk-aac), built with zig for $target" >>"$out/licenses/windows-dlls-SOURCE.txt"
echo "$out/libfdk-aac-2.dll"
