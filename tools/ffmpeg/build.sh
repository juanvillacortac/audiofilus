#!/bin/sh
# Compila un FFmpeg recortado (libavformat, libavcodec y libavutil, solo
# con lo que audiofilus no decodifica por su cuenta: WMA en su contenedor
# ASF) con la capa en C de shim.c, como una sola biblioteca (libavshim; ver
# internal/decode/av.go):
#   tools/ffmpeg/build.sh <carpeta de salida> [linux|windows|android]...
# linux (por defecto) deja <salida>/libs.aarch64/libavshim.so y
# <salida>/libs.x86_64/libavshim.so; windows, <salida>/libavshim.dll;
# android, <salida>/<abi>/libavshim.so (como en jniLibs). Además, la
# licencia y de dónde sale el código. build.sh mete las de Linux en los
# paquetes si están en build/ffmpeg; tools/windows/dlls.sh, la de Windows;
# android/libs.sh, las de Android.
#
# Es FFmpeg sin las partes GPL ni las "nonfree": LGPL-2.1 o posterior.
# Ampliar los formatos es añadir decodificadores y demuxers a $FORMATS (y
# su extensión en internal/decode/source.go).
#
# Como tools/libgme/build.sh: con zig (mise install zig), en Linux contra
# una glibc vieja (2.17): solo depende de la libc. Necesita zig (en el PATH,
# o ZIG), make, nm, curl y tar con xz; para android, el NDK
# (ANDROID_NDK_HOME) en vez de zig.
set -eu
mkdir -p "${1:?usage: tools/ffmpeg/build.sh <output dir> [linux|windows|android]...}"
out=$(cd "$1" && pwd)
shift
[ $# -eq 0 ] && set -- linux
here=$(cd "$(dirname "$0")" && pwd)
ZIG=${ZIG:-zig}
FFMPEG_VERSION=8.1.3
# Lo único que se compila: el contenedor ASF (WMA) y sus decodificadores.
FORMATS="--enable-protocol=file --enable-demuxer=asf
  --enable-decoder=wmav1,wmav2,wmapro,wmalossless,wmavoice"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Se baja a un archivo y no por tubería, para que los reintentos de curl no
# mezclen datos. Si ffmpeg.org no responde (pasó en CI), se usa el tag de
# GitHub: el mismo código, en .tar.gz y con la carpeta FFmpeg-n<versión>.
src="$work/ffmpeg-$FFMPEG_VERSION"
get="curl -sSfL --retry 5 --retry-all-errors --retry-delay 5"
if $get "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz" -o "$work/ffmpeg.tar.xz"; then
  tar xJf "$work/ffmpeg.tar.xz" -C "$work"
else
  echo "ffmpeg.org failed, using the GitHub mirror" >&2
  $get "https://github.com/FFmpeg/FFmpeg/archive/refs/tags/n$FFMPEG_VERSION.tar.gz" -o "$work/ffmpeg.tar.gz"
  tar xzf "$work/ffmpeg.tar.gz" -C "$work"
  mv "$work/FFmpeg-n$FFMPEG_VERSION" "$src"
fi

# zigTools <target de zig> deja en $bin el cc, ar, ranlib y nm con zig.
zigTools() {
  real=$("$ZIG" env | sed -n 's/.*zig_exe"\{0,1\} *[:=] *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$real" ] && ZIG=$real
  bin="$work/bin-$1"
  mkdir -p "$bin"
  # El cc quita --pic-executable, que configure pone siempre en Windows y el
  # enlazador de zig no entiende (los .exe de sus pruebas no lo necesitan).
  cat >"$bin/cc" <<EOF
#!/bin/sh
for a; do
  shift
  case \$a in
  -Wl,--pic-executable,*) a="-Wl,\${a#-Wl,--pic-executable,}" ;;
  -Wl,--pic-executable) continue ;;
  esac
  set -- "\$@" "\$a"
done
exec $ZIG cc -target $1 "\$@"
EOF
  printf '#!/bin/sh\nexec %s ar "$@"\n' "$ZIG" >"$bin/ar"
  printf '#!/bin/sh\nexec %s ranlib "$@"\n' "$ZIG" >"$bin/ranlib"
  printf '#!/bin/sh\nexec nm "$@"\n' >"$bin/nm"
  chmod +x "$bin"/*
}

# ndkTools <triple con API> deja en $bin los del NDK (clang y las de LLVM).
ndkTools() {
  : "${ANDROID_NDK_HOME:?set ANDROID_NDK_HOME to the Android NDK}"
  tc="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin"
  bin="$work/bin-$1"
  mkdir -p "$bin"
  # Un script, no un enlace: el <triple>-clang del NDK busca clang en su
  # propia carpeta.
  printf '#!/bin/sh\nexec %s "$@"\n' "$tc/$1-clang" >"$bin/cc"
  chmod +x "$bin/cc"
  for t in ar ranlib nm; do ln -sf "$tc/llvm-$t" "$bin/$t"; done
}

# build <nombre> <arch de FFmpeg> <target-os de FFmpeg> <biblioteca>, con
# las herramientas de $bin.
build() {
  name=$1 arch=$2 os=$3 lib=$4
  b="$work/build-$name"
  mkdir -p "$b"
  # PIC para la .so; en Windows no hace falta.
  pic=--enable-pic
  [ "$os" = mingw32 ] && pic=--disable-pic
  # Estáticas, sin programas, sin nada que no sea decodificar audio, y sin
  # buscar bibliotecas del sistema (--disable-autodetect): así no depende de
  # nada. Sin el ensamblador de x86 (nasm): para WMA no hace falta.
  # shellcheck disable=SC2086
  (cd "$b" && "$src/configure" --enable-cross-compile --target-os="$os" --arch="$arch" \
    --cc="$bin/cc" --ar="$bin/ar" --ranlib="$bin/ranlib" --nm="$bin/nm" \
    --disable-everything --disable-programs --disable-doc --disable-autodetect \
    --disable-network --disable-avdevice --disable-avfilter --disable-swscale \
    --disable-swresample --disable-x86asm --disable-debug \
    --enable-static --disable-shared $pic $FORMATS) >"$b/configure.log" 2>&1 ||
    { tail -30 "$b/ffbuild/config.log" >&2; exit 1; }
  # configure encuentra sysctl() en la libc pero zig (y Android) no traen
  # sys/sysctl.h: sin él (solo sirve para contar CPUs en BSD).
  sed -i 's/^#define HAVE_SYSCTL 1/#define HAVE_SYSCTL 0/' "$b/config.h"
  make -C "$b" -j"$(nproc)" >"$b/build.log" 2>&1 || { grep -B2 -A4 "error:" "$b/build.log" | head -40 >&2; exit 1; }
  # La capa en C y todo dentro de una biblioteca: el orden importa
  # (avformat usa avcodec, y los dos avutil).
  set -- -I"$src" -I"$b" "$here/shim.c" \
    "$b/libavformat/libavformat.a" "$b/libavcodec/libavcodec.a" "$b/libavutil/libavutil.a"
  case $os in
  mingw32) "$bin/cc" -shared -O2 -s -o "$lib" "$@" -lbcrypt -Wl,--no-undefined ;;
  # -Bsymbolic como en las .so de FFmpeg: su ensamblador de ARM apunta a sus
  # tablas sin pasar por la GOT. En Android, páginas de 16 KB (Android 15).
  android) "$bin/cc" -shared -O2 -fPIC -s -o "$lib" "$@" -Wl,-Bsymbolic -Wl,--no-undefined -Wl,-z,max-page-size=16384 -lm ;;
  *) "$bin/cc" -shared -O2 -fPIC -s -o "$lib" "$@" -Wl,-Bsymbolic -Wl,--no-undefined -lm -lpthread ;;
  esac
  rm -f "${lib%.dll}.lib" "$(dirname "$lib")/shim.lib" # en Windows, la biblioteca de importación, que no sirve
  echo "$lib"
}

for t in "$@"; do
  case $t in
  linux)
    for arch in aarch64 x86_64; do
      mkdir -p "$out/libs.$arch"
      zigTools "$arch-linux-gnu.2.17"
      build "linux-$arch" "$arch" linux "$out/libs.$arch/libavshim.so"
    done
    ;;
  windows)
    zigTools x86_64-windows-gnu
    build windows x86_64 mingw32 "$out/libavshim.dll"
    ;;
  android)
    # Las mismas ABI que android/libs.sh, desde Android 7 (API 24).
    for abi in arm64-v8a armeabi-v7a x86_64; do
      case $abi in
      arm64-v8a) triple=aarch64-linux-android24 arch=aarch64 ;;
      armeabi-v7a) triple=armv7a-linux-androideabi24 arch=arm ;;
      x86_64) triple=x86_64-linux-android24 arch=x86_64 ;;
      esac
      mkdir -p "$out/$abi"
      ndkTools "$triple"
      build "android-$abi" "$arch" android "$out/$abi/libavshim.so"
    done
    ;;
  *) echo "unknown target $t (linux, windows or android)" >&2; exit 2 ;;
  esac
done
# La licencia (FFmpeg así compilado es LGPL-2.1 o posterior) y de dónde sale
# el código, con las opciones para reproducirlo.
cp "$src/COPYING.LGPLv2.1" "$out/ffmpeg-LICENSE.txt"
printf 'FFmpeg %s (LGPL-2.1-or-later), with only: %s\nhttps://ffmpeg.org/releases/ffmpeg-%s.tar.xz\nBuilt by tools/ffmpeg/build.sh, with shim.c (LGPL-2.1-or-later): https://github.com/juanvillacortac/audiofilus/tree/main/tools/ffmpeg\n' \
  "$FFMPEG_VERSION" "$(echo $FORMATS | tr '\n' ' ')" "$FFMPEG_VERSION" >"$out/ffmpeg-SOURCE.txt"
