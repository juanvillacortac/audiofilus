#!/bin/sh
# Compila la app de Android:
#   android/build.sh [debug|release]
# 1. ebitenmobile desde el mismo commit del fork de Ebitengine que usa go.mod
#    (el código que genera tiene que coincidir), en build/android/bin;
# 2. audiofilus (mobile/) como biblioteca, android/app/libs/mobile.aar, para
#    ARM64 (teléfonos y portátiles) y x86_64 (el emulador, Chromebooks);
# 3. el APK con Gradle: android/app/build/outputs/apk/<tipo>/.
# Necesita Go, el SDK de Android con el NDK (ANDROID_HOME, y ANDROID_NDK_HOME
# o el NDK en $ANDROID_HOME/ndk) y un JDK 17 o 21. La versión de la app sale de
# VERSION (0.15.0), si está.
set -eu
cd "$(dirname "$0")/.."
kind=${1:-debug}
: "${ANDROID_HOME:?set ANDROID_HOME to the Android SDK}"
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
  ANDROID_NDK_HOME=$(ls -d "$ANDROID_HOME"/ndk/* | sort -V | tail -1)
  export ANDROID_NDK_HOME
fi

bin=$PWD/build/android/bin
commit=$(go list -m -f '{{.Replace.Version}}' github.com/hajimehoshi/ebiten/v2 | sed 's/.*-//')
if [ ! -x "$bin/ebitenmobile-$commit" ]; then
  src=$PWD/build/android/ebiten
  [ -d "$src" ] || git clone -q --filter=blob:none https://github.com/juanvillacortac/ebiten "$src"
  git -C "$src" fetch -q origin
  git -C "$src" checkout -q "$commit"
  mkdir -p "$bin"
  (cd "$src" && go build -o "$bin/ebitenmobile-$commit" ./cmd/ebitenmobile)
fi

mkdir -p android/app/libs
"$bin/ebitenmobile-$commit" bind -target android/arm64,android/amd64 -androidapi 24 \
  -javapkg dev.audiofilus -trimpath -o android/app/libs/mobile.aar ./mobile
rm -f android/app/libs/mobile-sources.jar

task=assembleDebug
[ "$kind" = release ] && task=assembleRelease
(cd android && ./gradlew -q "$task" ${VERSION:+-PversionName=$VERSION})
find android/app/build/outputs/apk -name '*.apk'
