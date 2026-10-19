# AGENTS.md

A guide for coding agents (Claude Code, Codex, etc.) working on **audiofilus**: a Go music player for desktops and Linux handhelds (tested on an Anbernic RG34XX-SP with Knulli). What sets it apart from other players is that **the music keeps playing in the background**, even when the interface is closed or a game is running, and that it has a **pocket mode** that uses very little battery. How it is built is documented in [`docs/`](docs/README.md); this file covers what you need to know before touching anything.

## Status

- **Done:** the audio service and its CLI, the Ebitengine interface (library, player, visualizers, games, Subsonic/Jellyfin servers, radio, podcasts, and YouTube Music with each user's own yt-dlp), pocket mode, and integration with Knulli, muOS, ROCKNIX and ArkOS/dArkOS. Measured on the console: [docs/power.md](docs/power.md).
- **Pending:** desktop optimization (mouse, keyboard, windows), and later Android and iOS.

## Working with the author

- The author writes in Spanish; reply in Spanish.
- In this repo the author asked the agent to build the player, not just give hints. Even so, visible design decisions (gestures, how the interface looks, what gets saved) are checked with the author first.
- **Before installing anything on the console, test it on the PC** (`go test`, and the service with real music). On the console, test first whatever could break booting.
- Commit or push only when the author asks. The repo is public (github.com/juanvillacortac/audiofilus): no passwords, personal paths or real music.

## Commands

```sh
go vet ./... && go test ./...                          # the tests (no real audio; about a second)
go run ./cmd/audiofilus play ~/Music                   # listen on the PC (starts the service)
go run ./cmd/audiofilus status                         # what's playing
go run ./cmd/audiofilus bench <file.mp3>               # how much CPU decoding it costs
./build.sh                                             # service and UI, PC and arm64, and dist/audiofilus.zip (PortMaster)
dist/audiofilus-ui                                     # the interface (starts the service from dist/)
dist/audiofilus-ui -shots /tmp/s -music ~/Music        # screenshots of every screen and aspect ratio
dist/audiofilus-ui -bench lake -music ~/Music          # CPU and memory per frame of a visualizer
docker build --target dist --output dist .             # same as build.sh, nothing to install
```

With `go run`, the binary lives in a temporary folder; to keep the configuration in a fixed place use `AUDIOFILUS_HOME=$PWD/run go run ./cmd/audiofilus ...`, or build with `./build.sh`.

Before calling a change done: `gofmt -l .` is empty, `go vet ./...` and `go test ./...` pass, and it builds for the console **without cgo**:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/audiofilus
GOOS=windows go vet ./...
```

## The map

| Folder | What's in it |
|---|---|
| `cmd/audiofilus/` | The binary: `daemon`, the CLI commands and `bench` |
| `internal/decode/` | Decoders to stereo float32: pure Go (Vorbis, FLAC, WAV, AIFF, ALAC, MP4) and C libraries through purego (mpg123, opusfile, fdk-aac, libxmp) |
| `internal/resample/` | Linear resampling to the output rate (48 kHz) |
| `internal/engine/` | Playback with `oto`: one player, the queue, gapless transitions |
| `internal/queue/` | The order: shuffle, repeat, previous/next, "play next" |
| `internal/library/` | Finding files (folders, `.m3u`) and reading tags |
| `internal/control/` | The socket protocol between the service and its clients |
| `internal/daemon/` | The service: ties together engine, platform, pocket mode and socket |
| `internal/platform/` | Desktop (does nothing), Knulli, muOS, ROCKNIX and ArkOS/dArkOS (lid, power button, battery saver, app sleep; see [docs/knulli.md](docs/knulli.md), [docs/muos.md](docs/muos.md) and [docs/rocknix-arkos.md](docs/rocknix-arkos.md)) |
| `internal/input/` | Logical buttons from the SDL mapping, controller reading (evdev), gesture and shortcut detection, power and headset keys |
| `internal/sdlmap/` | Asks SDL2 (through `purego`) for the controller mapping |
| `internal/remote/` | Subsonic/OpenSubsonic and Jellyfin clients, and the song cache |
| `internal/radio/`, `internal/podcast/`, `internal/ytm/` | Internet radio, podcasts, and YouTube Music search plus yt-dlp |
| `internal/sfx/` | The synthesized interface sounds and the splash jingle |
| `internal/proc/` | Per-OS process details: `.exe` names and hidden console windows on Windows |
| `internal/ui/` | The Ebitengine interface: screens, service client, cached library, background cover loading, languages |
| `cmd/audiofilus-ui/` | The interface binary |
| `internal/config/` | `audiofilus.json` and `data/`, **next to the binary** |
| `portmaster/` | The PortMaster port: launcher, `gameinfo.xml`, `port.json` and images ([docs/portmaster.md](docs/portmaster.md)) |
| `tools/portmaster/` | `art.sh`: regenerates the port's cover and screenshot |
| `site/` | The website (GitHub Pages); its screenshots and GIFs come from `-shots`/`-frames` with `-lang en` |
| `.github/workflows/` | CI (gofmt, vet, tests, arm64 without cgo), releases when a `v*` tag is pushed (built with the `Dockerfile`), and the website |
| `docs/` | The documentation |
| `.claude/skills/` | Procedures: testing on the console, measuring power |

Architecture rules:

- **Audio and everything that saves power live in the service**, not in the interface. The interface is a separate process that talks over the socket; it can be closed and the music goes on. See [docs/architecture.md](docs/architecture.md).
- **No cgo.** Everything builds with `CGO_ENABLED=0`, like Trancazo. The C libraries (SDL2, mpg123, opusfile, fdk-aac, libxmp) are loaded at runtime with purego, and if they're missing the program carries on without them ([docs/architecture.md](docs/architecture.md#formats)). The only exception is the desktop UI build, which uses GLFW.
- **Configuration is portable**: `audiofilus.json` and `data/` sit next to the binary (`config.Locate`). The only exception is the socket, which goes in `/tmp` because `/userdata` on Knulli is exFAT and doesn't support sockets.
- **In the background the console suspends as usual.** Preventing that is up to the open app (`awake`, a permission that expires), never the service on its own.
- **Buttons come from the SDL mapping**, not hardcoded codes: each console uses its own.
- **Each system's integration (Knulli, muOS, ROCKNIX, ArkOS) only turns on when that system is detected** (`platform.Detect`); on the PC everything is a no-op, so the same code is tested in both places.
- **It also builds for Windows** (`GOOS=windows`, no cgo). Linux-only code goes in `_linux.go` files with a `_other.go` twin that does nothing; check with `GOOS=windows go vet ./...`. See [docs/architecture.md](docs/architecture.md#windows).

## Knulli

What the service touches on the console, how it cleans up, and the known risks are in [docs/knulli.md](docs/knulli.md). The essentials:

- While playing: `/var/run/battery-saver/audiofilus.pause`, and `--bind` mounts over `/boot/boot/knulli.board.capability` (lid) and `/usr/bin/power-button` (button). They are undone on pause or exit, and after a crash on the next start (`/tmp/audiofilus-mounts`).
- The console only accepts password SSH logins; see the [`test-on-knulli`](.claude/skills/test-on-knulli/SKILL.md) skill.

## Code conventions

- **Documentation (`docs/`, READMEs, this file, skills) is in English.** Code comments are in Spanish, in full sentences: every type, function and constant says what it is and why. Doc comments start with the name (`// Load lee audiofilus.json...`). Match the comment density and language of the file you touch.
- **Error messages (`fmt.Errorf`, `errors.New`), test messages (`t.Errorf`) and log lines (`log.Printf`) are in English.** Text the user sees in the interface comes from `internal/ui/lang.go` (Spanish and English).
- The engine is called from two threads (`oto`'s audio thread and the commands): anything that touches its state holds `e.mu`, and callbacks go out through `emit`, never with the lock held. To change songs or seek, use `apply` (it flushes the audio buffer with `Player.Seek`).
- **When killing processes in tests, don't use `pgrep -f`/`pkill -f` with a pattern that also appears in your own command**: it kills the shell running it. Match the exact name (`pgrep -x`) and filter by `/proc/<pid>/exe`.
- One test per behavior, with a comment saying what it checks. Test audio is synthetic tones (see `internal/decode/testdata/README.md`); **never put real music in the repo**.
