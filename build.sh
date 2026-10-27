#!/bin/sh
# Compila audiofilus para el PC (amd64) y para las portátiles (arm64):
#   dist/audiofilus-<arch>  todo en un binario: la interfaz (sin argumentos), el
#                           servicio de audio (daemon) y la línea de comandos.
#                           En el PC con cgo, por GLFW; en las portátiles sin
#                           cgo, con el backend SDL2 del fork.
# Y deja dist/audiofilus, el del PC, listo para probar.
set -eu
cd "$(dirname "$0")"
# La aplicación de Last.fm de audiofilus (internal/scrobble), si está en el
# entorno: los releases la sacan de los secretos del repositorio. Sin ella,
# Last.fm pide la clave propia en audiofilus.json.
# La versión (internal/version): la etiqueta de la release (VERSION, que pone el
# workflow) o, compilando aquí, la de git si este commit tiene una; si no, "dev"
# (sin actualizaciones).
version=${VERSION:-$(git describe --tags --exact-match 2>/dev/null || echo dev)}
lfm="-X github.com/juanvillacortac/audiofilus/internal/scrobble.DefaultLastFMKey=${LASTFM_KEY:-} -X github.com/juanvillacortac/audiofilus/internal/scrobble.DefaultLastFMSecret=${LASTFM_SECRET:-} -X github.com/juanvillacortac/audiofilus/internal/version.Version=$version"
mkdir -p dist
rm -f dist/audiofilus-ui dist/audiofilus-ui-* # los de antes, cuando eran dos
for arch in amd64 arm64; do
  cgo=0
  [ "$arch" = "$(go env GOARCH)" ] && cgo=1
  CGO_ENABLED=$cgo GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w $lfm" \
    -o "dist/audiofilus-$arch" ./cmd/audiofilus
  echo "dist/audiofilus-$arch"
done
# Windows (amd64), sin cgo: Ebitengine usa DirectX y oto WASAPI. Es una
# aplicación de ventana (-H windowsgui), sin consola detrás; la línea de
# comandos se engancha a la de quien la lanza (console_windows.go).
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -H windowsgui $lfm" \
  -o dist/audiofilus-windows-amd64.exe ./cmd/audiofilus
echo "dist/audiofilus-windows-amd64.exe"

# Con cp + mv: si el servicio está corriendo, sobrescribir su binario falla
# ("Text file busy"); reemplazarlo con mv sí se puede (el proceso sigue con el viejo).
cp "dist/audiofilus-$(go env GOARCH)" dist/.audiofilus.new
mv -f dist/.audiofilus.new dist/audiofilus

# El port de PortMaster: dist/audiofilus.zip con lo de portmaster/ (lanzador,
# gameinfo.xml, port.json y las imágenes), el binario de cada arquitectura y
# las licencias (la del programa y las de las fuentes: la OFL pide que viajen
# con el programa).
pkg=dist/pkg
rm -rf "$pkg" dist/audiofilus.zip
mkdir -p "$pkg"
cp -r portmaster/. "$pkg/"
# Con el sufijo de arquitectura como es costumbre en PortMaster: .aarch64 (la
# mayoría de las portátiles) y .x86_64. El de x86_64 se compila aparte sin cgo
# (el del PC usa GLFW con cgo y pide X11, que una portátil x86 puede no
# tener); en los dos manda el backend SDL2 que pide el lanzador.
cp dist/audiofilus-arm64 "$pkg/audiofilus/audiofilus.aarch64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w $lfm" \
  -o "$pkg/audiofilus/audiofilus.x86_64" ./cmd/audiofilus
mkdir -p "$pkg/audiofilus/licenses"
# lic junta en licenses/LICENSE.<componente>.txt los textos de uno (su
# licencia y de dónde sale el código): uno por componente, como pide
# PortMaster.
lic() {
  name=$1
  shift
  for f in "$@"; do cat "$f"; echo; done > "$pkg/audiofilus/licenses/LICENSE.$name.txt"
}
lic audiofilus LICENSE
for f in internal/ui/assets/fonts/OFL-*.txt; do
  n=${f##*/OFL-}
  lic "${n%.txt}" "$f"
done
# libgme (la música de consolas viejas), si se compiló con
# tools/libgme/build.sh build/libgme: las portátiles no la traen. Va en
# libs.<arquitectura>, donde la busca el programa, con su licencia (LGPL) y
# de dónde sale el código.
if [ -d build/libgme ]; then
  cp -r build/libgme/libs.aarch64 build/libgme/libs.x86_64 "$pkg/audiofilus/"
  lic libgme build/libgme/libgme-LICENSE.txt build/libgme/libgme-SOURCE.txt
else
  echo "build/libgme missing: the packages go without libgme (tools/libgme/build.sh)" >&2
fi
# libvgm (los VGM de cualquier chip: arcades, NES, Game Boy...), igual, si se
# compiló con tools/libvgm/build.sh build/libvgm.
if [ -d build/libvgm ]; then
  for a in aarch64 x86_64; do
    mkdir -p "$pkg/audiofilus/libs.$a"
    cp build/libvgm/libs.$a/libvgmshim.so "$pkg/audiofilus/libs.$a/"
  done
  lic libvgm build/libvgm/libvgm-LICENSE.txt build/libvgm/libvgm-SOURCE.txt tools/libvgm/GPL-2.0.txt
else
  echo "build/libvgm missing: the packages go without libvgm (tools/libvgm/build.sh)" >&2
fi
# libfdk-aac (M4A, M4B, radios AAC), igual, si se compiló con
# tools/fdk-aac/build.sh build/fdk-aac: Knulli la trae, muOS y otros no.
if [ -d build/fdk-aac ]; then
  for a in aarch64 x86_64; do
    mkdir -p "$pkg/audiofilus/libs.$a"
    cp build/fdk-aac/libs.$a/libfdk-aac.so.2 "$pkg/audiofilus/libs.$a/"
  done
  lic fdk-aac build/fdk-aac/fdk-aac-LICENSE.txt build/fdk-aac/fdk-aac-SOURCE.txt
else
  echo "build/fdk-aac missing: the packages go without libfdk-aac (tools/fdk-aac/build.sh)" >&2
fi
(cd "$pkg" && zip -qr ../audiofilus.zip .)
echo dist/audiofilus.zip

# La aplicación de muOS, sin PortMaster: dist/audiofilus.muxapp, un zip con la
# carpeta de la app (su lanzador mux_launch.sh, el icono, el binario y las
# licencias) que muOS instala desde su gestor de archivos en su carpeta de
# aplicaciones.
app=dist/muxapp
rm -rf "$app" dist/audiofilus.muxapp
mkdir -p "$app/audiofilus"
cp -r muos/audiofilus/. "$app/audiofilus/"
cp dist/audiofilus-arm64 "$app/audiofilus/audiofilus.aarch64"
cp -r "$pkg/audiofilus/licenses" "$app/audiofilus/"
[ -d build/libgme ] && cp -r build/libgme/libs.aarch64 "$app/audiofilus/"
if [ -d build/libvgm ]; then
  mkdir -p "$app/audiofilus/libs.aarch64"
  cp build/libvgm/libs.aarch64/libvgmshim.so "$app/audiofilus/libs.aarch64/"
fi
if [ -d build/fdk-aac ]; then
  mkdir -p "$app/audiofilus/libs.aarch64"
  cp build/fdk-aac/libs.aarch64/libfdk-aac.so.2 "$app/audiofilus/libs.aarch64/"
fi
chmod +x "$app/audiofilus/mux_launch.sh" "$app/audiofilus/audiofilus.aarch64"
(cd "$app" && zip -qr ../audiofilus.muxapp .)
echo dist/audiofilus.muxapp
