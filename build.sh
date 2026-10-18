#!/bin/sh
# Compila audiofilus para el PC (amd64) y para las portátiles (arm64):
#   dist/audiofilus-<arch>     el servicio de audio (sin cgo)
#   dist/audiofilus-ui-<arch>  la interfaz (en el PC con cgo, por GLFW; en las
#                              portátiles sin cgo, con el backend SDL2 del fork)
# Y deja dist/audiofilus y dist/audiofilus-ui del PC juntos, listos para probar:
# la interfaz busca el servicio en su misma carpeta.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w" \
    -o "dist/audiofilus-$arch" ./cmd/audiofilus
  cgo=0
  [ "$arch" = "$(go env GOARCH)" ] && cgo=1
  CGO_ENABLED=$cgo GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w" \
    -o "dist/audiofilus-ui-$arch" ./cmd/audiofilus-ui
  echo "dist/audiofilus-$arch dist/audiofilus-ui-$arch"
done
# Windows (amd64), sin cgo: Ebitengine usa DirectX y oto WASAPI. La interfaz
# es una aplicación de ventana (-H windowsgui), sin consola detrás.
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
  -o dist/audiofilus-windows-amd64.exe ./cmd/audiofilus
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -H windowsgui" \
  -o dist/audiofilus-ui-windows-amd64.exe ./cmd/audiofilus-ui
echo "dist/audiofilus-windows-amd64.exe dist/audiofilus-ui-windows-amd64.exe"

# Con cp + mv: si el servicio está corriendo, sobrescribir su binario falla
# ("Text file busy"); reemplazarlo con mv sí se puede (el proceso sigue con el viejo).
for bin in audiofilus audiofilus-ui; do
  cp "dist/$bin-$(go env GOARCH)" "dist/.$bin.new"
  mv -f "dist/.$bin.new" "dist/$bin"
done

# El port de PortMaster: dist/audiofilus.zip con lo de portmaster/ (lanzador,
# gameinfo.xml, port.json y las imágenes), los binarios de arm64 y las
# licencias (la del programa y las de las fuentes: la OFL pide que viajen con
# el programa).
pkg=dist/pkg
rm -rf "$pkg" dist/audiofilus.zip
mkdir -p "$pkg"
cp -r portmaster/. "$pkg/"
cp dist/audiofilus-arm64 "$pkg/audiofilus/audiofilus"
cp dist/audiofilus-ui-arm64 "$pkg/audiofilus/audiofilus-ui"
mkdir -p "$pkg/audiofilus/licenses"
cp LICENSE internal/ui/assets/fonts/OFL-*.txt "$pkg/audiofilus/licenses/"
(cd "$pkg" && zip -qr ../audiofilus.zip .)
echo dist/audiofilus.zip
