# Compila audiofilus en un contenedor, sin instalar Go ni nada en el equipo:
#   docker build --target dist --output dist .
# deja en dist/ lo mismo que ./build.sh: los binarios del PC (amd64) y de las
# portátiles (arm64) y dist/audiofilus.zip, el port de PortMaster.
FROM golang:1.27.1 AS build
# La interfaz del PC usa GLFW (cgo): necesita las cabeceras de X11, OpenGL y
# ALSA. Lo de las portátiles se compila sin cgo y no necesita nada.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libgl1-mesa-dev xorg-dev libasound2-dev zip \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && go test ./... && ./build.sh

# Solo los resultados, para sacarlos con --output.
FROM scratch AS dist
COPY --from=build /src/dist/ /
