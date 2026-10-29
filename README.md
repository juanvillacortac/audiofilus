<p align="center">
  <img src="site/img/logo.png" alt="audiofilus" width="460">
</p>

<p align="center">
  <b>A pixel-art music player for Linux handhelds.</b><br>
  The music keeps playing while you game, and sleep mode turns the screen off so you can control it from your pocket.
</p>

<p align="center">
  <a href="https://juanvillacortac.github.io/audiofilus/">Website</a> ·
  <a href="https://portmaster.games/detail.html?name=audiofilus">Get it on PortMaster</a> ·
  <a href="https://github.com/juanvillacortac/audiofilus/releases/latest">Download</a> ·
  <a href="docs/README.md">Docs</a> ·
  <a href="https://ko-fi.com/juanvillacortac">Support on Ko-fi</a>
</p>

<p align="center">
  <a href="https://portmaster.games/detail.html?name=audiofilus"><img src="https://img.shields.io/badge/PortMaster-Get%20it-ff5fa2" alt="Get it on PortMaster"></a>
  <a href="https://ko-fi.com/juanvillacortac"><img src="https://img.shields.io/badge/Ko--fi-Support%20audiofilus-6fe3c4?logo=kofi&logoColor=white" alt="Support on Ko-fi"></a>
</p>

<p align="center">
  <img src="site/img/player.webp" alt="Now Playing" width="360">
  <img src="site/img/vis-lake.webp" alt="Lake visualizer" width="320">
</p>

Runs on **Knulli, muOS, ROCKNIX, ArkOS, dArkOS and EmuELEC** through PortMaster (and as a native app on muOS; tested on an Anbernic RG34XX-SP), on **Linux and Windows** desktops, and on **iPhone and iPad**.

## Support

Audiofilus is free, with no ads or paywalls, and I make it in my spare time. If it keeps your music going, a coffee helps me keep working on it:

<p><a href="https://ko-fi.com/juanvillacortac"><img src="https://img.shields.io/badge/Ko--fi-Buy%20me%20a%20coffee-6fe3c4?logo=kofi&logoColor=white&style=for-the-badge" alt="Support on Ko-fi"></a></p>

## Features

### Background play

- **The audio lives in its own service.** The interface is only a remote: close it, open an emulator, and the music keeps going.
- **Controls from inside any game:** hold `FN` and press a button to pause, skip, go back or seek.
- **Headphone buttons**, Bluetooth or wired: one press pauses, two skip, three go back. Devices are watched with inotify, so waiting for them costs no CPU.
- **Sleep mode:** double `START` or the power button turns the screen off, lowers the CPU clock and turns the triggers into gestures. A closed lid keeps it asleep. On an RG34XX-SP it draws about 260 mA: around 11 to 12 hours of music on its 3300 mAh battery (see [docs/power.md](docs/power.md)).
- Gapless playback, fade on pause, shuffle, repeat, "play next" and a queue that survives restarts.
- Volume stays in sync with the system, including boost above 100%.
- **Sleep timer:** stop the music after 15 to 90 minutes or at the end of the song, even with the player closed.
- **Alarm:** wake up to whatever played last, a playlist or a favorite station, every day, on weekdays, on weekends or once, with the volume rising over a minute. The service plays it with the player closed, and on handhelds it sets the console clock (RTC) to wake it from sleep.
- **Unplugging wired headphones pauses** the music, also in the background, so it doesn't suddenly come out of the speaker.
- **Docked:** with an external display (HDMI) connected, closing the lid doesn't turn the screen off.

### Sound

- **ReplayGain** per song, per album, or automatic (album in order, song on shuffle), with preamp and clipping protection; Opus R128 gains too.
- **10-band equalizer** with presets (rock, metal, vocal, bass, loudness...) and a band editor, or a **parametric equalizer** with up to 10 bands (peak, shelves, low/high pass), preamp and its curve on screen.
- **AutoEQ:** search for your headphone model in the [AutoEq](https://github.com/jaakkopasanen/AutoEq) database and its correction goes into the parametric equalizer; or load `ParametricEQ.txt` files (AutoEq or Equalizer APO) from an `eq` folder next to the app.
- **Crossfeed** for headphones (light, medium or strong, like bs2b): a little of each channel goes to the other, filtered and delayed, so hard-panned records are easier on the ears.
- **Compute ReplayGain** (EBU R128) for songs that don't have it, written into the files.
- **Resampling**: a light linear one on handhelds (battery) and a high-quality sinc one on the desktop, or pick one.
- **Crossfade** of up to 12 seconds, which leaves consecutive songs of the same album gapless.
- **Output rate** of 44.1, 48, 96 or 192 kHz.

<p align="center">
  <img src="site/img/eq.png" alt="The 10-band equalizer with the Rock preset" width="360">
  <img src="site/img/stats.png" alt="Listening stats: plays, hours and top artists" width="360">
</p>

### Library

- **Your own radio lists:** put `.m3u`/`.m3u8`/`.pls` station lists in the app's `radio/` folder (or any folder you pick in Settings → *Radio lists*) and they show up under Radio → My lists; or add a station by its address. Station addresses that are themselves a `.pls`/`.m3u` (like `listen.pls`) are followed to the stream. **HLS** stations (`.m3u8` split into segments: MPEG-TS, packed AAC/MP3 or fMP4, AES-128 encrypted too) play as well (thanks to volkybaba for the report and ideas).
- Formats: **MP3, AAC/M4A, ALAC, FLAC, Ogg Vorbis, Opus, WAV, AIFF, DSD (DSF and DFF)** tracker modules (**MOD, XM, IT, S3M**) and **console music**: NSF/NSFE (NES), SPC (SNES), GBS (Game Boy), VGM/VGZ and GYM, HES (PC Engine), KSS (MSX), AY and SAP, through libgme and libvgm, which the handheld and Linux packages bring. VGMs play with any chip, arcade boards (Out Run, CPS…) included. And **8-bit computer music**, emulated in Go with nothing to install: SID (Commodore 64, the HVSC's `.sid` files, PSID and RSID with digis), YM (Atari ST, with its SID-voice and digidrum effects), VTX and PT3 (ZX Spectrum, TurboSound included). A file with a whole soundtrack shows up as one track per song. Albums ripped to one file with a **CUE sheet** show up as their tracks.
- Songs, albums (cover grid), artists, folders and playlists (`.m3u` in your music folder plus your own, saved as `.m3u8`).
- Search across the library, with an on-screen QWERTY keyboard.
- The scan runs once and is cached; later runs only read new or changed files.
- The interface colors come from the album cover.
- **Liked songs and ratings** (1 to 5), synced with Subsonic/Navidrome and Jellyfin.
- **Smart playlists** that fill themselves from rules (most played, never played, recently added, by genre or year...), with ready-made ones.
- **Edit tags** of songs and albums (MP3, FLAC, Ogg, Opus, M4A), **find missing covers** (Cover Art Archive, iTunes) and **fix albums from MusicBrainz**.
- **Send to speakers and TVs** on your network (DLNA/UPnP and Chromecast), **share your library over DLNA** and **receive music** from phones and other DLNA apps.
- **History:** Recent and Most played lists, and stats (plays, hours, top artists, albums and songs).
- **Scrobbling** to Last.fm and ListenBrainz (queued while offline), and play counts on your Subsonic or Jellyfin server.
- **Lyrics**, synced karaoke-style when available (each line tints as it's sung, and dots count down the instrumental parts): from a `.lrc` or `.txt` next to the song (or in a `lyrics` folder), from the file's tags (ID3 USLT and SYLT, `LYRICS` in FLAC and Ogg, `©lyr` in M4A), or found online on [LRCLIB](https://lrclib.net), also for radio stations that send the song title. Pick a different version from the list, nudge the timing, or turn online lookups off in Settings.

<p align="center">
  <img src="site/img/colors.webp" alt="Colors following four album covers" width="360">
</p>

<p align="center">
  <img src="site/img/lyrics.webp" alt="Synced lyrics tinting line by line, with dots during the instrumental intro" width="360">
</p>

<p align="center">
  <img src="site/img/albums.png" alt="Albums" width="360">
  <img src="site/img/queue.png" alt="Queue" width="360">
</p>

### Remote sources

| Source | What you get |
|---|---|
| **Subsonic / OpenSubsonic** (Navidrome and others) | Browse, stream, cache and download; edit their playlists |
| **Jellyfin** | Same, with your Jellyfin user and its playlists |
| **Internet radio** | Stations from radio-browser.info by country or genre, with the live song title (ICY); Icecast/Shoutcast and HLS streams |
| **Podcasts** | Top shows for your country, search, any RSS feed; downloads and resume; 30-second skips and speed without changing the pitch |
| **Audiobooks** | `.m4b` files, files with the audiobook genre, or books in an `Audiobooks` folder (one file per chapter). It remembers where you were in each book even if music plays in between, shows chapters (`.m4b` and MP3), L1/R1 change chapter, ←/→ skip 30 seconds, and speed goes from 0.75× to 2× without changing the pitch |
| **YouTube Music** | Search without signing in; listen without downloading, or save songs with cover and tags. Requires your own `yt-dlp` (see below) |
| **Module archives** | The Mod Archive, Modland, VGMRips and AMP: browse what's popular, new or random, search (also from Search), listen, save to your library or open a MOD/XM in the tracker. No account needed |

Remote songs are cached on the SD card (400 MB by default, least recently used goes first) and the next track is fetched ahead of time.

<p align="center">
  <img src="site/img/search-youtube.png" alt="Search with YouTube results" width="360">
  <img src="site/img/radio.png" alt="Internet radio" width="360">
  <img src="site/img/audiobook.png" alt="An audiobook playing: a book icon, a cassette and the current chapter" width="360">
</p>

### Visualizers and games

Nine visualizers that follow the beat: **Lake, Drift, Trench, Plasma, Tunnel, Waves, Bars, Ridges** (stacked spectrum lines, like the *Unknown Pleasures* cover) and **MilkDrop**, which plays classic MilkDrop 1 `.milk` presets: eight built in, plus your own in a `milkdrop` folder, in their own colors or the album's ([docs/milkdrop.md](docs/milkdrop.md)). The Visualizer screen remembers the last one you watched, and any of them can also play dimmed **behind Now Playing** (START → *Visualizer background*, always in the album's colors; off by default to save battery). Plus eight arcade games and a tracker, together under **Games** in the screens dock (and a few secrets hidden around the app):

- **Crypt**: a rhythm roguelike. Step, fight and dive deeper through random dungeons, but only on the beat of the song that's playing; enemies move to it too. On-beat streaks raise a multiplier up to FEVER ×4.
- **Blocks**: falling blocks, with hard drops on the beat for combos.
- **Rhythm**: notes generated from the song that's playing.
- **Runner**: like Chrome's offline dinosaur, but on the beat. Speakers arrive on the kick and flying vinyls on the snare; hold to jump higher, duck under the records, and jump on the "and" for a PERFECT. One hit and it's over.
- **Name that song**: a few seconds of a song from your library, four choices, ten rounds.
- **Snake**: steps on the song's beat subdivisions, so every song plays at its own speed.
- **Ships**: a shooter that fires on every eighth note and spreads on the bass.
- **Duel**: turn-based battles to the beat. Strike near the center of the bar, which lands right on the beat, then dodge the notes a scratched vinyl, a tangled tape and a metronome throw at you: they surge on every beat, and the song's snares and cymbals send more.
- **Tracker**: make chiptune with the D-pad, LSDJ style with a touch of PICO-8: songs built from chains and phrases, instruments with envelopes, tables and vibrato, grooves with swing, editable waves (the eight PICO-8 ones to start), 21 effect commands and a piano roll. Two modes per song: **Game Boy** (four channels modeled on its sound chip) or **Free** (eight channels with synths). Undo, copy, paste and clone, and four examples to learn from. **Import** MOD and XM modules (with their samples) and LSDJ songs (`.lsdsng` or a whole `.sav`); **export** as WAV, XM or MOD straight into your library. **MIDI**: play and step-record from a USB MIDI keyboard, and follow or send MIDI clock (Linux handhelds and desktop, Windows, iOS). Old songs open converted.

<p align="center">
  <img src="site/img/vis-drift.webp" alt="Drift" width="320">
  <img src="site/img/vis-trench.webp" alt="Trench" width="320">
  <img src="site/img/vis-ridges.webp" alt="Ridges" width="320">
</p>

### MilkDrop

The **MilkDrop** visualizer plays MilkDrop 1 presets (`.milk`), the classic Winamp visuals, drawn on the GPU with Kage shaders, on the handhelds too.

- **Open it:** SELECT → **Visualizer**, then ←/→ until **MilkDrop** (it's the last one: one ← from the first), or pick it from the START menu.
- **Controls:** L1/R1 change preset, A locks the current one, START opens the menu (colors, next, previous, random, lock, rescan). On its own it moves to the next preset every 20 seconds or so, on a beat, with a two-second blend.
- **Colors:** START → *Colors* switches between *Preset*, each preset as its author made it, and *Album*, repainted with the colors of the album that's playing.
- **Your presets:** eight original presets are built in. Put your own `.milk` files (whole packs with subfolders are fine) in a `milkdrop` folder next to the app (or any folder you pick in Settings → *MilkDrop presets*), then START → *Rescan presets*. MilkDrop 2 presets load, but their shaders are ignored.

<p align="center">
  <img src="site/img/vis-milkdrop.webp" alt="MilkDrop" width="320">
</p>

More in [docs/milkdrop.md](docs/milkdrop.md).
<p align="center">
  <img src="site/img/crypt.webp" alt="Crypt" width="320">
  <img src="site/img/runner.webp" alt="Runner" width="320">
  <img src="site/img/duel.webp" alt="Duel" width="320">
</p>
<p align="center">
  <img src="site/img/blocks.webp" alt="Blocks" width="320">
  <img src="site/img/rhythm.webp" alt="Rhythm" width="320">
  <img src="site/img/snake.webp" alt="Snake" width="320">
</p>

### Interface

- `SELECT` opens a dock with every screen; double `SELECT` returns to the previous one. `START` opens the More menu.
- Hold `FN` on any screen to see its controls.
- Mouse and touch: tap rows, covers, tabs, menus and the player's buttons; drag lists with your finger (with momentum) or use the wheel; long-press for the More menu, right-click to go back; even the button hints at the bottom can be tapped.
- The left analog stick works like the D-pad everywhere, menus and games included.
- On Now Playing, `↑`/`↓` scratch the vinyl, and the song itself scratches back and forth like a record under your hand.
- The layout follows the screen's real shape: any width from 1:1 to 21:9 in landscape, and portrait (phone-like, down to 9:21) with its own Now Playing layout.
- Optional chiptune sounds, with separate switches for menus and for music actions. Game sounds are low thumps and soft hits, made to sit under any music, metal included.
- 13 languages: English, Spanish, Portuguese, French, Italian, German, Polish, Turkish (thanks to volkybaba), Russian, Indonesian, Japanese, Chinese and Korean. Japanese, Chinese and Korean text (song titles too) shows in a pixel font, Fusion Pixel. 12 h (AM/PM) or 24 h clock.
- **Color:** each album's colors, or a fixed color to match your console's shell (red, orange, yellow, green, ice blue, blue, purple, pink, pearl pink, white, platinum, graphite or black).
- With a station or a podcast playing, `B` on Now Playing goes back to Radio or Podcasts instead of the library.
- On desktop: fullscreen with sharp pixels.

<p align="center">
  <img src="site/img/guide.png" alt="Controls guide" width="360">
  <img src="site/img/width-16x9.png" alt="16:9 layout" width="427">
</p>

### Linux desktop

On a Linux PC it fits in like a native app, on GNOME, KDE, Hyprland or [Omarchy](https://omarchy.org):

- **App launcher:** Audiofilus shows up with its icon, with Play/Pause, Next and Previous actions.
- **Open with:** files and folders open from the file manager ("Open with Audiofilus") and play as a temporary queue; also on Windows (registered on first launch) and from the Files app on iPhone.
- **Window:** its class is `audiofilus`, so docks group it and window rules can target it.
- **Media keys and bars:** it speaks MPRIS, so keyboard media keys, headphone buttons and the desktop's media widget control it, with cover and title. On Omarchy that works with nothing to set up.
- **System tray** (also on Windows): the Audiofilus CD sits in the tray while the music plays in the background: click to open the player, middle click to pause, and a menu with what's playing, play/pause, next, previous, shuffle and quit.
- **Closing the window keeps the music playing** (a setting, on by default on the desktop); Quit stops everything.

Details in [docs/linux-desktop.md](docs/linux-desktop.md).

### iPhone and iPad

The same player on iOS, touch first: every row, cover, tab and button is tappable, lists scroll under your thumb with momentum, the bottom bar has real buttons, the games have touch controls, and in portrait the tabs open from a button.

- Background playback, with the cover and controls on the lock screen, in Control Center, on headphones and in CarPlay.
- Music from the Files app (*On My iPhone → Audiofilus*), any folder picked from iCloud Drive or another app, or none at all: Subsonic, Navidrome, Jellyfin, radio, podcasts and YouTube Music work without local music.
- AAC from iTunes and AAC radio play through Apple's own decoder.

<p align="center">
  <img src="site/img/phone-player.png" alt="Now Playing on an iPhone" width="200">
  <img src="site/img/phone-library.png" alt="The library on an iPhone" width="200">
  <img src="site/img/phone-tabs.webp" alt="Library tabs dropping down" width="200">
</p>

## Install

### Handhelds (PortMaster)

1. Open PortMaster on your handheld, find **Audiofilus** in the list and install it ([its page](https://portmaster.games/detail.html?name=audiofilus)). Or download `audiofilus-portmaster.zip` from the [latest release](https://github.com/juanvillacortac/audiofilus/releases/latest) and unzip it into your `ports` folder.
2. Open **Audiofilus** from Ports and pick your music folder.

Settings (`audiofilus.json`) and state (`data/`) live next to the binary.

**On muOS, without PortMaster:** download `audiofilus.muxapp`, copy it to the `ARCHIVE` folder on your SD card, and install it from muOS's Archive Manager. Audiofilus then shows up in **Applications**.

**YouTube Music** shows up only if there is a `yt-dlp` binary in the app's `yt-dlp/` folder (or on `PATH`). Audiofilus doesn't ship or download it; get the `yt-dlp_linux_aarch64` build from the [yt-dlp releases](https://github.com/yt-dlp/yt-dlp/releases) for handhelds. Songs that need a signed-in account (age-restricted) are played from another upload of the same song when there is one. If YouTube asks your network to confirm it's not a bot, or you want signed-in access, put a `cookies.txt` exported from your browser in that `yt-dlp/` folder ([how](https://github.com/yt-dlp/yt-dlp/wiki/Extractors#exporting-youtube-cookies)).

### Linux desktop

```sh
curl -fsSL https://juanvillacortac.github.io/audiofilus/install.sh | sh
```

It downloads the latest release for x86_64 or aarch64 into `~/.local/share/audiofilus`, links `audiofilus` into `~/.local/bin`, and adds Audiofilus to the app launcher. Run it again to update; settings are kept. To uninstall:

```sh
audiofilus quit; audiofilus desktop uninstall
rm -rf ~/.local/share/audiofilus ~/.local/bin/audiofilus
```

Or by hand: grab `audiofilus-linux-amd64.tar.gz` (or `-arm64`), unzip it anywhere, run `./audiofilus desktop install` once and open it from the launcher.

### iPhone and iPad

Every release has an unsigned `audiofilus-ios-unsigned.ipa`. Install it with [Sideloadly](https://sideloadly.io) or [AltStore](https://altstore.io), which sign it with your own Apple ID. Details in [docs/ios.md](docs/ios.md).

**AltStore, SideStore or LiveContainer source:** add `https://juanvillacortac.github.io/audiofilus/altstore.json` as a source and every new release shows up as an update.

### Windows

On Windows 10 or later, grab `audiofilus-windows-amd64.zip`, unzip it anywhere and run `audiofilus.exe`. Everything plays out of the box, AAC/M4A, Opus, tracker modules, console music and AAC/HLS radio included: the zip carries the decoder DLLs next to the `.exe` (mpg123, opusfile, libxmp and libgme from MSYS2, and fdk-aac built with zig; their licenses are in `licenses/`). For YouTube Music, put `yt-dlp.exe` in a `yt-dlp` folder next to it, and `ffmpeg.exe` on `PATH` to convert downloads to MP3.

## Build

With Docker, nothing else needed:

```sh
docker build --target dist --output dist .
```

Or with Go (see `go.mod` for the version; the desktop build needs the X11, OpenGL and ALSA headers, e.g. `libgl1-mesa-dev xorg-dev libasound2-dev`):

```sh
./build.sh
```

Both leave in `dist/`:

| File | What it is |
|---|---|
| `audiofilus-amd64`, `audiofilus-arm64` | Everything in one binary: the player (run it with no arguments), the audio service (`audiofilus daemon`, which the player starts itself) and the CLI |
| `audiofilus-windows-amd64.exe` | The same for Windows |
| `audiofilus.zip` | The PortMaster port |

The handheld builds use no cgo. SDL2, mpg123, fdk-aac, opusfile and libxmp are loaded at runtime with [purego](https://github.com/ebitengine/purego) when the system has them. The interface uses [a fork of Ebitengine](https://github.com/juanvillacortac/ebiten) with an SDL2 backend.

### CLI

```sh
dist/audiofilus play ~/Music          # starts the service and plays
dist/audiofilus status
dist/audiofilus next | prev | toggle | seek +30 | shuffle on | repeat all
dist/audiofilus speed 1.5 | crossfeed medium | alarm set '{"on":true,"hour":7,"min":30,"days":[1,2,3,4,5]}'
dist/audiofilus quit
```

### Tests

```sh
go vet ./... && go test ./...
```

Releases are built by GitHub Actions when a `v*` tag is pushed.

## Roadmap

- **Handhelds:** an OpenGL ES 2.0 path for devices without GLES 3 (for now it needs OpenGL ES 3.2).
- **Desktop:** Windows system media controls, layouts made for big windows.
- **iOS:** a signed build.
- **Android:** the same mobile app (the `mobile/` package already builds for it with ebitenmobile).

## License

[MIT](LICENSE). The bundled fonts, Jersey 10, Tiny5 and Fusion Pixel, are under the SIL Open Font License (`internal/ui/assets/fonts/OFL-*.txt`). The AY chip emulation follows [ayumi](https://github.com/true-grue/ayumi) and the PT3 player is a port of [pt3player](https://github.com/Volutar/pt3player), both MIT (`internal/chip/LICENSE.*`). The packages also ship two libraries loaded at runtime, each under its own license: libgme (LGPL) and libvgmshim, built from [libvgm](https://github.com/ValleyBell/libvgm) plus `tools/libvgm/shim.cpp`, which as a whole is **GPL-2.0** (`tools/libvgm/GPL-2.0.txt`; the list of each source file's license and the exact libvgm commit go in `licenses/` of every package). Audiofilus itself stays MIT and doesn't link them at build time.

## Docs

How it works, platform details and power measurements are in [docs/](docs/README.md).
