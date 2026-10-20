# Builds audiofilus in a container, with nothing installed on the host:
#   docker build --target dist --output dist .
# leaves in dist/ the same as ./build.sh: the desktop (amd64) and handheld
# (arm64) binaries and dist/audiofilus.zip, the PortMaster port.
FROM golang:1.27.1 AS build
# The desktop UI uses GLFW (cgo): it needs the X11, OpenGL and ALSA headers.
# The handheld builds use no cgo and need nothing.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libgl1-mesa-dev xorg-dev libasound2-dev zip \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The Last.fm application key (build.sh), from the repository secrets.
ARG LASTFM_KEY=""
ARG LASTFM_SECRET=""
# La versión: la etiqueta de la release (si no, build.sh pone "dev").
ARG VERSION=""
RUN go vet ./... && go test ./... && LASTFM_KEY="$LASTFM_KEY" LASTFM_SECRET="$LASTFM_SECRET" VERSION="$VERSION" ./build.sh

# Only the results, to export them with --output.
FROM scratch AS dist
COPY --from=build /src/dist/ /
