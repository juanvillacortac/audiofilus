# The PortMaster port

Besides the binaries, `./build.sh` produces `dist/audiofilus.zip`: the port for Linux handhelds with PortMaster, for both architectures PortMaster runs on (aarch64 and x86_64). It contains what is in `portmaster/`, the binaries of each architecture and the licenses (the OFL requires the font licenses to ship with the program):

```
Audiofilus.sh              the launcher (comments in English, like PortMaster's)
gameinfo.xml               for the EmulationStation menu
README.md                  notes and controls, for PortMaster's port page
audiofilus/port.json       the port metadata for PortMaster
audiofilus/cover.png       the cover (640×480)
audiofilus/screenshot.png  a screenshot (640×480)
audiofilus/audiofilus.aarch64  the UI, the service and the CLI in one binary, for arm64 handhelds
audiofilus/audiofilus.x86_64   the same for x86_64 ones
audiofilus/licenses/       one LICENSE.<component>.txt per component (as PortMaster asks): audiofilus (MIT), the fonts (OFL) and the bundled libraries, each with where its source is
```

## The launcher

- It uses PortMaster's `control.txt` like any other port (`get_controls`, `$GPTOKEYB`, `pm_finish`).
- It runs `audiofilus.$DEVICE_ARCH` (PortMaster's variable; `uname -m` if missing): `x86_64` there, `aarch64` otherwise, as PortMaster ports usually name their binaries. It is one binary for everything (one instead of two saves about 10 MB per architecture): with no arguments it is the UI, which starts the service by running itself again as `audiofilus daemon -detach` (`Client.daemonPath`); the detached service names itself `audiofilusd` in its command line. Both architectures are built without cgo: they only need libc and load SDL2 at runtime (the desktop x86_64 build uses GLFW with cgo instead).
- `AUDIOFILUS_HOME` points to the port folder: `audiofilus.json`, `data/` and the service log (`data/audiofilus.log`) live there. `log.txt` is the UI output.
- `AUDIOFILUS_HANDHELD=1`: it runs on a handheld, also on a system Audiofilus doesn't know (see [Other systems](#other-systems)).
- `EBITENGINE_UI_BACKEND=sdl2` and `SDL_GAMECONTROLLERCONFIG` with PortMaster's mapping for the console. The service starts the UI, so it inherits that environment.
- gptokeyb only provides PortMaster's exit shortcut (Select+Start): it runs `pkill -f audiofilus.<arch>`, which kills the UI; the service doesn't match (its command line is `audiofilusd daemon`), so it isn't killed abruptly: it notices the UI is gone and stops the music and cleans up its mounts, unless **Keep playing on close** is on in Settings (off by default on handhelds), in which case the music goes on.
- On ArkOS and dArkOS it restores `~/.asoundrc` if it is missing: there is no PulseAudio there and audio goes through ALSA.

## The images

`tools/portmaster/art.sh <music folder> [album]` regenerates `cover.png` and `screenshot.png`:

- The **cover** is drawn by the UI (`internal/ui/portart.go`): a vinyl record sliding out of its sleeve, the splash logo with its equalizer, the tagline and the signature. It does not use real album covers, so it can be published. The sleeve is a five-color gradient with Bayer dithering, like the rest of the look.
- The **screenshot** is the playback screen in 4:3, **in English** (`-lang en`; that is what most PortMaster users see), with the requested album (`-album`, e.g. `"Wonder What"`).

Screenshots come out at ×3 of 320×240. The script scales them down to 1:1 by taking one pixel out of every three (exact, because they were scaled up without smoothing) and then scales them up ×2. Before the screenshots, everything is drawn once and discarded: the first use of a shader in the process sometimes did not reach the image (the screenshot came out without the vinyl).

**To publish the port**, the screenshot cannot include other people's album covers: it must be made with your own music or free music.

## Installing on the console

Copy `dist/audiofilus.zip` to PortMaster's `autoinstall` folder and open PortMaster, which installs it into `ports/`. See the [`test-on-knulli`](../.claude/skills/test-on-knulli/SKILL.md) skill.

On **EmuELEC**, install PortMaster first from the EmuELEC menu. Installing by hand, the files go in two places: `Audiofilus.sh` in `/storage/roms/ports_scripts/` (the folder EmuELEC lists ports from) and the `audiofilus/` folder in `/storage/roms/ports/`.

## Other systems

It has only been tested on Knulli. What does not depend on the system (the service, the UI, explicit background mode, the FN×3 shortcuts, the OLED sleep) should work on all of them. Preventing suspend, turning off the backlight and handling the power button are implemented for Knulli ([knulli.md](knulli.md)), muOS ([muos.md](muos.md)), and ROCKNIX, ArkOS/dArkOS and EmuELEC ([rocknix-arkos.md](rocknix-arkos.md)).

Two more get a lighter integration (`internal/platform/handheld.go`):

- **CrossMix** (TrimUI Smart Pro and Brick), recognized by `/mnt/SDCARD/System/usr/trimui/crossmix-version.txt`. The app's sleep turns the screen and the stick LEDs off and on with CrossMix's own scripts (`lcd_off.sh` and `lcd_on.sh` in `/mnt/SDCARD/System/usr/trimui/scripts`, which write to `/sys/kernel/debug/dispdbg` and `/sys/class/led_anim`; `lcd_on.sh` restores the brightness from its settings) and lowers the governor. The power button belongs to the system (TrimUI's MainUI, closed source) and still suspends, which stops the music: for the pocket there is the app's sleep (Start twice). Untested on a device.
- **Any other handheld**: the launchers export `AUDIOFILUS_HANDHELD=1`, and on a system Audiofilus doesn't know the service then runs as a generic handheld (`handheld`): the app's sleep turns off `/sys/class/backlight` and lowers the governor, without touching suspend. The interface also reads it (`handheldUI`) to offer the handheld options, like the orientation.

On a system without either, they do nothing. ROCKNIX and muOS use PipeWire with PulseAudio compatibility. ArkOS, dArkOS and EmuELEC use ALSA directly, where background music and game sound might not be able to play at the same time.
