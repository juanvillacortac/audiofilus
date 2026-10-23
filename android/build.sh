#!/bin/sh
# Compila la app de Android:
#   android/build.sh [debug|release] [github|play]
# 1. ebitenmobile desde el mismo commit del fork de Ebitengine que usa go.mod
#    (el código que genera tiene que coincidir), en build/android/bin;
# 2. audiofilus (mobile/) como biblioteca, android/app/libs/mobile.aar, para
#    ARM64 (teléfonos y portátiles), ARM de 32 bits (los TV box y las teles
#    con Android TV, casi todas de 32 bits) y x86_64 (el emulador, Chromebooks);
# 3. el APK con Gradle: el de GitHub (con Ajustes → Actualizar), en
#    android/app/build/outputs/apk/github/<tipo>/; "play" como segundo
#    argumento hace el de Google Play (sin él).
# Necesita Go, el SDK de Android con el NDK (ANDROID_HOME, y ANDROID_NDK_HOME
# o el NDK en $ANDROID_HOME/ndk) y un JDK 17 o 21. La versión de la app sale de
# VERSION (0.15.0), si está; las claves de Last.fm, de LASTFM_KEY y
# LASTFM_SECRET, como en build.sh.
set -eu
cd "$(dirname "$0")/.."
kind=${1:-debug}
store=${2:-github}
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
pkg=github.com/juanvillacortac/audiofilus/internal
version=${VERSION:+v$VERSION}
ldflags="-X $pkg/version.Version=${version:-dev} -X $pkg/scrobble.DefaultLastFMKey=${LASTFM_KEY:-} -X $pkg/scrobble.DefaultLastFMSecret=${LASTFM_SECRET:-}"
"$bin/ebitenmobile-$commit" bind -target android/arm,android/arm64,android/amd64 -androidapi 24 \
  -javapkg dev.audiofilus -trimpath -ldflags "$ldflags" -o android/app/libs/mobile.aar ./mobile
rm -f android/app/libs/mobile-sources.jar

flavor=Github
[ "$store" = play ] && flavor=Play
task=assemble${flavor}Debug
[ "$kind" = release ] && task=assemble${flavor}Release
(cd android && ./gradlew -q "$task" ${VERSION:+-PversionName=$VERSION})
find android/app/build/outputs/apk -name '*.apk'
