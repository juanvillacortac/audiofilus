# Architecture

## Two processes

```
┌──────────────────────────┐   Unix socket (JSON)   ┌────────────────────────────────┐
│ Interface (Ebitengine)   │ ◄────────────────────► │ Service: audiofilus daemon     │
│ - library, covers        │   play, next, status…  │ - decodes and plays            │
│ - can be closed          │                        │ - queue, shuffle, repeat       │
│                          │                        │ - pocket mode and gestures     │
└──────────────────────────┘                        │ - lid, button, power (Knulli)  │
                                                    └────────────────────────────────┘
```

The service starts with `audiofilus daemon -detach` (or on its own, the first time `play` is requested) in a new session (`setsid`), so it outlives the launcher: Knulli kills the port's process group when it exits. This way the music keeps playing in the Knulli menu or inside a game, and PipeWire mixes the audio of both.

The interface has no playback state of its own: it asks for it with `status` and sends commands. In the meantime, the CLI plays that role.

## The two modes (author's decision, October 17, 2026)

| | **Background** (interface closed) | **App open** (Ebitengine interface) |
|---|---|---|
| What for | Listening while playing something else or browsing Knulli | Listening with the app, also in the pocket |
| Console suspend | **Normal**: lid, button and inactivity suspend as usual; when it suspends, the music stops | The app prevents it while open, playing or not (lid, button, inactivity): paused in the pocket, it resumes with the triggers |
| Screen off with music | No | Yes: the app's pseudo-sleep, with the double-tap gestures |
| Controls | **Global hotkeys** (pause, forward, rewind, next, previous) that don't clash with Knulli or the emulators | The interface, and the gestures in pseudo-sleep |

How it is built:

- **`awake`** is a permission that expires: the interface sends `awake on` when it opens and renews it (it lasts 60 s by default). The daemon prevents suspend only if the permission is valid **and** music is playing (`updateAwake`). If the interface hangs, the permission expires and the console goes back to suspending on its own.
- **`sleep`** is the app's pseudo-sleep (screen off, double-tap gestures); the interface requests it.
- **The background shortcut** is always active: FN three times in a row (each press less than 400 ms after the previous one) and, within 1.5 s, L1 (previous), R1 (next), L2 (−10 s), R2 (+10 s) or Select (pause). A short vibration confirms it is armed. Any other button between the FN presses cancels it, so it doesn't clash with games that use FN.

## The controller

`internal/sdlmap` loads SDL2 with `purego` (no cgo, like the SDL2 backend of the Ebitengine fork) only to ask how the controller is mapped: the SDL mapping string (`leftshoulder:b4,...`) and its `/dev/input/eventN`. It uses `SDL_GAMECONTROLLERCONFIG_FILE` if it is in the environment (PortMaster exports it) or PortMaster's `gamecontrollerdb.txt`. Then `input.FromSDL` translates the `bN`/`aN` indices to evdev codes with the same numbering SDL uses on Linux (first from `BTN_JOYSTICK` to `KEY_MAX`, then the rest; axes skip the hats), and the controller is read from evdev with blocking reads: while nothing is pressed it uses no CPU. Waiting for SDL events without a window would mean waking up many times per second.

If SDL is missing or doesn't recognize the controller, the standard Linux codes (`input.Standard`) are used. These don't work on every console (the RG34XX-SP uses different ones, see [knulli.md](knulli.md)). `input.device` in `audiofilus.json` forces a specific device.

## The audio path

```
file ────► decode.Source ─► resample.Reader ─► Engine.Read ─────────────────────────► oto.Player ─► PulseAudio/PipeWire
           (stereo float32,   (to 48 kHz,         ReplayGain → crossfade mix → EQ →
            its own rate)      linear)            limiter → float32 → bytes; advances the queue)
```

- **`decode`** picks the decoder from the first bytes of the file (not from the extension) and always outputs interleaved stereo float32 at the file's sample rate. Mono is duplicated; more than two channels are reduced to the two front ones.
- **`resample`** interpolates linearly up to the output rate (48 kHz by default, PipeWire's rate on the console). If they match, it does nothing.
- **`engine`** has a single `oto.Player` that lives the whole time and reads from `Engine.Read`. When a song ends, `Read` opens the next one without a gap.
- **Sound processing** (`engine/dsp.go`, `engine/crossfade.go`), configured with `engine.DSP` (commands `replaygain`, `preamp`, `eq`, `crossfade`; saved in `data/state.json`):
  - **ReplayGain:** the gain is read from the file's tags when it opens (`library.ReadGain`: Vorbis/FLAC `REPLAYGAIN_*`, ID3 `TXXX`, MP4 `----`, Opus `R128_*` + 5 dB) and applied to each chunk as it is read, so a gapless change of song changes the gain on the exact sample. Modes: track, album, auto (album in order, track on shuffle) or off; a preamp; never above what the peak allows. Remote songs (Subsonic transcodes) and radio get none.
  - **Crossfade:** when the song has that many seconds left, the queue advances (the next song is announced as usual) and the old source keeps playing underneath as `tail`, fading with equal-power sine/cosine curves until it ends. Not between consecutive songs of the same album in order, nor with repeat-one, the sleep timer's "end of song", remote songs, very short songs (under three fades) or sources without a length. Reading is done in 1024-frame chunks while it is on, to start on time. A manual change of song or a seek cuts it.
  - **Equalizer:** ten peaking biquads (31 Hz to 16 kHz, one octave wide, RBJ cookbook), only the bands that are not at 0, with 80 % of the biggest boost taken off first; the limiter then catches what still goes over (it runs whenever the EQ or a positive gain is on, not only with boost). Ten bands cost about 5 % of one core at 480 MHz on the RG34XX-SP.
- **Output rate:** `sample_rate` in `audiofilus.json` (44.1, 48, 96 or 192 kHz, from Settings). oto opens one context per process, so changing it restarts the service (`restart`: `Run` returns `ErrRestart` without telling the interface to close, the binary re-executes itself with `syscall.Exec`, and if it was playing it carries on, `State.Resume`). With the file at that rate and nothing applied (volume at 100 %, no ReplayGain or EQ), samples go out untouched; PipeWire or PulseAudio may still resample to their own rate, so this is not a bit-perfect path.
- **`oto`** talks to PulseAudio over its protocol, in pure Go. On Knulli everything runs as root without `XDG_RUNTIME_DIR`; `usePulseSocket` points `PULSE_SERVER` to `/var/run/pulse/native`, because otherwise `oto` would fall back to ALSA and clash with PipeWire.

## Threads and lock

- The `oto` audio thread calls `Engine.Read`; commands arrive over the socket. All engine state is guarded by `e.mu`.
- **Changing song or seeking** is done with `apply`: it stores the operation in `e.pending` and calls `Player.Seek`, which waits for the current `Read` to finish, empties the buffer and calls `Engine.Seek`, where the operation runs. This way the rest of the previous song's buffer doesn't play.
- **Callbacks** (`OnPlayingChanged`, `OnTrackChanged`) are queued with `emit` and run in another goroutine, never with the lock held or on the audio thread, because the Knulli one runs `mount`.
- **When paused**, `Context.Suspend` is called so the audio device sleeps.

## State and configuration

Next to the binary:

| File | What it is |
|---|---|
| `audiofilus.json` | The configuration; it is written with the default values the first time (`config.Default`) |
| `data/state.json` | The queue, position, shuffle, repeat and volume; saved on pause, on exit and every 15 s while playing |
| `data/audiofilus.log` | The log when the service runs detached (`-detach`) |
| `data/history.jsonl` | Every listen, one JSON line each (`internal/history`): written by the service, read by the interface |
| `data/favorites.json` | Liked songs and ratings, with their title, artist and album (written by the interface) |
| `data/scrobbles.json` | Listens waiting to reach Last.fm or ListenBrainz (no connection) |

On startup, the service restores the queue and position from last time, paused.

The socket lives at `/tmp/audiofilus-<folder hash>.sock`: `/userdata` on Knulli is exFAT and doesn't support sockets, and the hash keeps two installations from clashing. `AUDIOFILUS_HOME` and `AUDIOFILUS_SOCKET` override the folder and the socket.

## Formats

The C libraries are loaded at runtime with purego (`decode/native.go`), without cgo, just like SDL2: Knulli ships them, and so do almost all desktop Linux systems. If one is missing, that format returns `ErrUnsupported` (and the library skips it); MP3 falls back to go-mp3. `AUDIOFILUS_DECODER=go` forces them off (for comparison, and in the tests).

| Format | Decoder | CPU on the RG34XX-SP* | Notes |
|---|---|---|---|
| MP3 | `libmpg123` (C); otherwise `hajimehoshi/go-mp3` | 0.6 % (go-mp3: ~18 %) | |
| Opus (`.opus`, `.ogg`) | `libopusfile` (C) | 1.4 % | Always 48 kHz |
| AAC (`.m4a`, `.mp4`) | `libfdk-aac` (C) + our own MP4 reader (`decode/mp4.go`) | 2.0 % | HE-AAC comes out at double the sample rate |
| ALAC (`.m4a`) | Pure Go (`decode/alac`, from `alicebob/alac`, MIT) + `mp4.go` | 3.7 % | 16 and 24 bits, mono and stereo |
| Ogg Vorbis | Pure Go (`jfreymuth/oggvorbis`) | 2.7 % | |
| FLAC | Pure Go (`mewkiz/flac`) | 5.3 % | Seeks by bisecting frame headers (no seek table needed, ~40 ms on the console), then decodes up to the requested frame |
| WAV, AIFF | our own (`decode/wav.go`, `decode/aiff.go`) | 0.5 % | 8/16/24/32-bit PCM and 32-bit float; uncompressed AIFF-C |
| Modules (`.mod`, `.xm`, `.it`, `.s3m`…) | `libxmp` (C) | — | Recognized by extension; played once, at 48 kHz |
| Console music (`.nsf`, `.nsfe`, `.spc`, `.gbs`, `.vgm`, `.vgz`, `.gym`, `.hes`, `.kss`, `.ay`, `.sap`) | `libgme` (Game_Music_Emu, C++) | — | Emulates the console's sound chip at 48 kHz. A file with several songs (an NSF holds a whole soundtrack) becomes one track per song, `game.nsf#song=3` (`decode.GMEPath`, expanded by the scanner, titled "Game #3" when the file names no song). Songs that loop, or don't say how long they are, last what libgme suggests (2.5 min by default) and fade out in their last 8 s; songs with their own length end by themselves. Seeking emulates up to that point |
| DSD (`.dsf`, `.dff`) | Pure Go (`decode/dsd.go`) | ~32 % at 480 MHz (DSD64) | Two FIR stages (1-bit straight to 88.2 kHz with table lookups, then 44.1 kHz) at -6 dB; DST-compressed DFF is not supported; DSD256 may not keep up on a handheld |

\* Decoding only, in % of one core at 1.5 GHz (`audiofilus bench`, 2026-10-17), with one minute of a real song.

Libraries are looked up first in `libs.<arch>` next to the binary (`libs.aarch64`, `libs.x86_64`, as PortMaster does), then where the system has them. That is how the handhelds get **libgme**, which none of their systems ship: `tools/libgme/build.sh` compiles libgme 0.6.5 with zig (`mise install zig`) against glibc 2.17, with libc++ and zlib (for VGZ) built in, so it only needs the base libc and loads on old systems like ArkOS. The release workflow runs it (zig through mise-action), and `build.sh` puts it, with its LGPL license and a pointer to its source, in the PortMaster zip, the `.muxapp` and the Linux tarballs. On Windows there is no libgme yet.

On Windows the same libraries are loaded as DLLs (`decode/native_windows.go`), looked up first in the program's folder: the MSYS2 builds `libmpg123-0.dll`, `libopusfile-0.dll` (with `libopus-0.dll` and `libogg-0.dll`), `libxmp-4.dll` or `libxmp.dll`, and `libfdk-aac-2.dll`. Without them Windows plays MP3 (go-mp3), FLAC, Ogg Vorbis, ALAC, WAV and AIFF; AAC, Opus and modules need the DLLs.

**CUE sheets:** an album ripped to one file with a `.cue` shows up as its tracks. Each track is a path `album.flac#t=start,end` (`decode.CuePath`) that travels through the queue like any other; `decode.Open` opens it as a segment of the file (`OpenSegment`, sample-accurate start and end), the scanner replaces the big file with the sheet's tracks (`library.withCues`) and their titles come from the sheet (`library.ReadCueTrack`). Covers, ReplayGain and MPRIS art use the file's tags.

Tags (title, artist, album) are read by `dhowden/tag`.

## Windows

The service and the interface build for Windows without cgo (`GOOS=windows`): Ebitengine draws with DirectX and oto plays through WASAPI. What changes:

- **Socket:** the same Unix socket, in `%TEMP%` (Windows 10 1803 and later support `AF_UNIX`).
- **Starting the service:** `audiofilus.exe daemon -detach` relaunches it as a detached process with no console (`cmd/audiofilus/detach_windows.go`); the interface starts it hidden (`proc.Hidden`), and the same goes for yt-dlp and ffmpeg.
- **Interface:** built with `-H windowsgui`, so no console window opens behind it. The battery indicator uses `GetSystemPowerStatus`.
- **Not on Windows:** reading the controller from the service (evdev), headset keys, the power button and every handheld integration. Files that need Linux end in `_linux.go`, with a `_other.go` (or `detect_other.go`) twin that compiles elsewhere and does nothing. The interface still gets keyboard and controller input through its window.
- **System volume:** not read or changed (`desktop.SystemVolume` uses `pactl`, which isn't there); the app's own volume works.

CI runs the tests on `windows-latest` and a smoke test: start the service, play the test tones with a virtual sound card, and quit.
