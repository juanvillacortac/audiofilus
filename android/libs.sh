#!/bin/sh
# Compila con el NDK las bibliotecas en C que audiofilus carga en tiempo de
# ejecución (purego) y que Android no trae, y las deja en
# android/app/src/main/jniLibs/<abi>/, de donde Gradle las mete en el APK:
#   android/libs.sh
# Por ahora libfdk-aac (AAC: M4A, M4B, radios AAC y HLS). Necesita el SDK con
# el NDK (ANDROID_HOME, o ANDROID_NDK_HOME), cmake, curl y tar.
set -eu
cd "$(dirname "$0")/.."
: "${ANDROID_HOME:?set ANDROID_HOME to the Android SDK}"
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
  ANDROID_NDK_HOME=$(ls -d "$ANDROID_HOME"/ndk/* | sort -V | tail -1)
fi
out=$PWD/android/app/src/main/jniLibs
FDK_VERSION=2.0.3
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -sSfL "https://github.com/mstorsjo/fdk-aac/archive/refs/tags/v$FDK_VERSION.tar.gz" | tar xz -C "$work"
src="$work/fdk-aac-$FDK_VERSION"
# En Android, fdk-aac anota los desbordes con el log interno del sistema
# (log/log.h, que el NDK no trae): uno que no hace nada.
mkdir -p "$work/stub/log"
printf '#pragma once\n#define android_errorWriteLog(tag, sub) ((void)0)\n' >"$work/stub/log/log.h"
for abi in arm64-v8a armeabi-v7a x86_64; do
  b="$work/build-$abi"
  cmake -S "$src" -B "$b" -DCMAKE_TOOLCHAIN_FILE="$ANDROID_NDK_HOME/build/cmake/android.toolchain.cmake" \
    -DANDROID_ABI="$abi" -DANDROID_PLATFORM=android-24 -DANDROID_STL=c++_shared \
    -DCMAKE_C_FLAGS="-Wno-date-time -I$work/stub" -DCMAKE_CXX_FLAGS="-Wno-date-time -I$work/stub" \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=ON -DBUILD_PROGRAMS=OFF \
    -DCMAKE_SHARED_LINKER_FLAGS="-s -Wl,-z,max-page-size=16384" >/dev/null
  cmake --build "$b" -j"$(nproc)" >"$work/build-$abi.log" 2>&1 || { grep -B2 -A4 "error:" "$work/build-$abi.log" | head -40 >&2; exit 1; }
  mkdir -p "$out/$abi"
  # Con el nombre que Android acepta en un APK (lib*.so, sin número) y que
  # audiofilus también busca (internal/decode/aac.go).
  cp -L "$b/libfdk-aac.so" "$out/$abi/libfdk-aac.so"
  echo "$out/$abi/libfdk-aac.so"
  # Su libc++ (fdk-aac es C++), la del NDK, que va junto a ella en el APK.
  case $abi in
  arm64-v8a) triple=aarch64-linux-android ;;
  armeabi-v7a) triple=arm-linux-androideabi ;;
  x86_64) triple=x86_64-linux-android ;;
  esac
  cp "$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/sysroot/usr/lib/$triple/libc++_shared.so" "$out/$abi/"
  "$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/llvm-strip" --strip-unneeded "$out/$abi/libc++_shared.so"
done
