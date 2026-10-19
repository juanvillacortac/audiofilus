#!/bin/sh
# Compila audiofilus para el PC (amd64) y para las portátiles (arm64):
#   dist/audiofilus-<arch>  todo en un binario: la interfaz (sin argumentos), el
#                           servicio de audio (daemon) y la línea de comandos.
#                           En el PC con cgo, por GLFW; en las portátiles sin
#                           cgo, con el backend SDL2 del fork.
# Y deja dist/audiofilus, el del PC, listo para probar.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
rm -f dist/audiofilus-ui dist/audiofilus-ui-* # los de antes, cuando eran dos
for arch in amd64 arm64; do
  cgo=0
  [ "$arch" = "$(go env GOARCH)" ] && cgo=1
  CGO_ENABLED=$cgo GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w" \
    -o "dist/audiofilus-$arch" ./cmd/audiofilus
  echo "dist/audiofilus-$arch"
done
# Windows (amd64), sin cgo: Ebitengine usa DirectX y oto WASAPI. Es una
# aplicación de ventana (-H windowsgui), sin consola detrás; la línea de
# comandos se engancha a la de quien la lanza (console_windows.go).
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -H windowsgui" \
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
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
  -o "$pkg/audiofilus/audiofilus.x86_64" ./cmd/audiofilus
mkdir -p "$pkg/audiofilus/licenses"
cp LICENSE internal/ui/assets/fonts/OFL-*.txt "$pkg/audiofilus/licenses/"
(cd "$pkg" && zip -qr ../audiofilus.zip .)
echo dist/audiofilus.zip
