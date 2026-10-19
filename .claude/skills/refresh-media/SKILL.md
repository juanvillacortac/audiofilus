---
name: refresh-media
description: Regenerate the screenshots, GIFs and logo used by the website (site/img), the README and the PortMaster port. Use after visible UI changes, or when the author asks for new screenshots/GIFs.
---

# Refresh media

All images are rendered by the interface itself (`-shots` and `-frames` in `cmd/audiofilus-ui`), at 3× of 320×240, and scaled down without smoothing so pixels stay sharp.

## Website and README (`site/img`)

```sh
tools/site/media.sh ~/Music/gym "Wonder What"
```

- Arguments: a music folder and, optionally, the album that should be playing (first match by artist or title).
- Screenshots are in English (`-lang en`); the site and README are in English.
- Output: stills at 1:1, 360×240 (`player.png`, `albums.png`, ...); the site scales every pixel-art image by whole numbers of device pixels (`fitPixels` in `site/index.html`), `width-*.png` for each aspect ratio, GIFs at 320×240 (`vis-*.gif`, `player.gif`, `blocks.gif`, `rhythm.gif`, `splash.gif`), `logo.png`, `splash-bg.webp` and `splash-fg.webp` (the app's splash in two layers at 30 fps, playing once: the background, which the site stretches to cover any screen, and the logo on top, on transparent, which it centers) and `og.png`.
- The site lists its images explicitly in `site/index.html`; if you add or rename one, update the page and `README.md`.

## PortMaster (`portmaster/audiofilus`)

```sh
tools/portmaster/art.sh ~/Music/gym "Wonder What"
```

Regenerates `cover.png` and `screenshot.png` (640×480).

## Before committing

The repo is public. Check the images don't show personal paths (the Settings screen shows the music folder), error messages, or anything private. Album covers from the author's library appear in the screenshots; that was accepted for now.

After committing, the Pages workflow publishes the site on push to `main` when `site/` changes.
