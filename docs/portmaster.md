# The PortMaster port

Besides the binaries, `./build.sh` produces `dist/audiofilus.zip`: the port for Linux handhelds with PortMaster. It contains what is in `portmaster/`, the arm64 binaries and the font licenses (the OFL requires them to ship with the program):

```
Audiofilus.sh              the launcher (comments in English, like PortMaster's)
gameinfo.xml               for the EmulationStation menu
audiofilus/port.json       the port metadata for PortMaster
audiofilus/cover.png       the cover (640×480)
audiofilus/screenshot.png  a screenshot (640×480)
audiofilus/audiofilus      the audio service (arm64)
audiofilus/audiofilus-ui   the UI (arm64)
audiofilus/licenses/       OFL for Jersey 10 and Tiny5
```

## The launcher

- It uses PortMaster's `control.txt` like any other port (`get_controls`, `$GPTOKEYB`, `pm_finish`).
- `AUDIOFILUS_HOME` points to the port folder: `audiofilus.json`, `data/` and the service log (`data/audiofilus.log`) live there. `log.txt` is the UI output.
- `EBITENGINE_UI_BACKEND=sdl2` and `SDL_GAMECONTROLLERCONFIG` with PortMaster's mapping for the console. The service starts the UI, so it inherits that environment.
- gptokeyb only provides PortMaster's exit shortcut (Select+Start): it kills the UI and, since background mode was not requested, the service stops the music.
- On ArkOS and dArkOS it restores `~/.asoundrc` if it is missing: there is no PulseAudio there and audio goes through ALSA.

## The images

`tools/portmaster/art.sh <music folder> [album]` regenerates `cover.png` and `screenshot.png`:

- The **cover** is drawn by the UI (`internal/ui/portart.go`): a vinyl record sliding out of its sleeve, the splash logo with its equalizer, the tagline and the signature. It does not use real album covers, so it can be published. The sleeve is a five-color gradient with Bayer dithering, like the rest of the look.
- The **screenshot** is the playback screen in 4:3, **in English** (`-lang en`; that is what most PortMaster users see), with the requested album (`-album`, e.g. `"Wonder What"`).

Screenshots come out at ×3 of 320×240. The script scales them down to 1:1 by taking one pixel out of every three (exact, because they were scaled up without smoothing) and then scales them up ×2. Before the screenshots, everything is drawn once and discarded: the first use of a shader in the process sometimes did not reach the image (the screenshot came out without the vinyl).

**To publish the port**, the screenshot cannot include other people's album covers: it must be made with your own music or free music.

## Installing on the console

Copy `dist/audiofilus.zip` to PortMaster's `autoinstall` folder and open PortMaster, which installs it into `ports/`. See the [`test-on-knulli`](../.claude/skills/test-on-knulli/SKILL.md) skill.

## Other systems

It has only been tested on Knulli. What does not depend on the system (the service, the UI, explicit background mode, the FN×3 shortcuts, the OLED sleep) should work on all of them. Preventing suspend, turning off the backlight and capturing the volume buttons is only implemented for Knulli (`internal/platform/knulli.go`); on the others it does nothing. ROCKNIX and muOS use PipeWire with PulseAudio compatibility. ArkOS and dArkOS use ALSA directly, where background music and game sound might not be able to play at the same time.
