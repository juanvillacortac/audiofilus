---
name: refresh-media
description: Regenerate the screenshots, GIFs and logo used by the website (site/img), the README and the PortMaster port. Use after visible UI changes, or when the author asks for new screenshots/GIFs.
---

# Refresh media

All images are rendered by the interface itself (`-shots` and `-frames` in `cmd/audiofilus/ui.go`), at 3× of 320×240, and scaled down without smoothing so pixels stay sharp.

## Website and README (`site/img`)

```sh
tools/site/media.sh ~/Music/gym "Wonder What"
```

- Arguments: a music folder and, optionally, the album that should be playing (first match by artist or title).
- Screenshots are in English (`-lang en`); the site and README are in English.
- Output, all at 4× with hard pixels (the site and README only scale them down, smoothly, so they look even at any size and display scaling): stills (`player.png`, `albums.png`, ...), `width-*.png` for each aspect ratio, WebP animations (`vis-*.webp`, `player.webp`, `blocks.webp`, `rhythm.webp`), `logo.png`, the splash layers `splash-bg.webp` (1:1, stretched to cover) and `splash-fg.webp`, and `og.png`.
- The site lists its images explicitly in `site/index.html`; if you add or rename one, update the page and `README.md`.

## PortMaster (`portmaster/audiofilus`)

```sh
tools/portmaster/art.sh ~/Music/gym "Wonder What"
```

Regenerates `cover.png` and `screenshot.png` (640×480).

## Game mode

```sh
AUDIOFILUS_KNULLI_ROOT=<copy of a console's files> dist/audiofilus -shots /tmp/gm -frames /tmp/gm-frames -music ~/Music/gym -album "Wonder What" -lang en
```

With `AUDIOFILUS_KNULLI_ROOT`, `-shots` saves the game mode screens (`juego-*.png`) instead of the player's, and `-frames` adds four sequences at 30 fps (`internal/ui/gamemode_frames.go`): the system carousel, the right-stick spin, the GBA game list and a game's details with its video. For a GIF, scale down to 360×240 and back up ×2 without smoothing so the pixels stay sharp (`scale=360:240:flags=neighbor,scale=720:480:flags=neighbor`, then `palettegen`/`paletteuse`).

## Before committing

The repo is public. Check the images don't show personal paths (the Settings screen shows the music folder), error messages, or anything private. Album covers from the author's library appear in the screenshots; that was accepted for now.

After committing, the Pages workflow publishes the site on push to `main` when `site/` changes.
