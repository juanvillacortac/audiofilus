# Knulli

What follows was learned by patching Songo #5 on the RG34XX-SP (Knulli on Batocera 42, October 2026) and lives in `internal/platform/knulli.go`. It only activates if `/boot/boot/knulli.board.capability` and `/usr/bin/knulli-brightness` exist.

## While the app asks to keep it awake

Knulli suspends the console in three ways. The service neutralizes them only when the interface asks for it (`awake`) **and** music is playing (`SetAwake(true)`), and restores them on pause, when the permission expires or on exit. In the background it touches nothing: the console suspends as usual.

| Path | How it works in Knulli | What audiofilus does |
|---|---|---|
| Inactivity | `idlewatcher` dims after 5 min and **mutes the audio** (`system.batterysaver.mute=1`); after 15 min it suspends. Its hooks (`/etc/idlewatcher/*.d`) do nothing if there is a `*.pause` in `/var/run/battery-saver/` | Creates `/var/run/battery-saver/audiofilus.pause` |
| Lid | Closing the lid sends `KEY_INSERT`; `triggerhappy` runs `/usr/bin/lid-control close`, which only acts if `knulli-board-capability lid` (reads `/boot/boot/knulli.board.capability`) | Mounts with `--bind` a copy of the file without the word `lid` |
| Power button | `KEY_POWER` → `/usr/bin/power-button`: short press = `knulli-suspend`, long (2 s) = power off | Mounts a copy of the script with `knulli-suspend &` replaced by `:`. Holding it still powers off |

Notes:

- Knulli **ships neither `mountpoint` nor `strings`**; mounts are checked by reading `/proc/mounts`.
- If the file is already covered by another app (for example a patched Songo), nothing is mounted on top.
- Mounts are recorded in `/tmp/audiofilus-mounts`. If the service dies without cleaning up, the next start undoes them (`unmountRecorded`). Since `/tmp` is tmpfs, a reboot also cleans everything.
- With the music paused and the lid **already closed**, the console doesn't suspend: Knulli only acts when the lid *is closed*.

## Alarm, headphones and external display

- **Alarm** (`daemon/alarm.go`, `platform/dock_linux.go`): tested on an RG34XX-SP, it wakes a suspended console, lid closed included.
  - **Which clock:** the RTC that can wake the device, the one whose `device/power/wakeup` is `enabled`. On the H700 that is `rtc1`, the SoC RTC; `rtc0` is a PCF8563 with wakeup disabled. `rtc1` may not be set and count from 1970, so the alarm is written relative to its own `since_epoch` (its "now" plus the seconds left), 30 s before the alarm. An absolute Unix time fails there with an I/O error (`Day must be in the range 0 - 255`).
  - **Surviving `knulli-suspend`:** before suspending, it writes `0` to every RTC's `wakealarm` and only arms its own auto-shutdown timer. So while an alarm is on, the service installs the pm-utils hook `/etc/pm/sleep.d/05-audiofilus-alarm` and writes the alarm's Unix time to `/var/run/audiofilus-alarm.epoch`. On `suspend` the hook, which runs after `knulli-suspend` cleared the clocks, re-arms every wake-capable RTC, unless one already holds an earlier alarm (Knulli's auto-shutdown: with it, the console powers off before the alarm).
  - **Lid closed:** on resume, Knulli's `99-postresume-sp` runs `knulli-suspend` again a second later if the lid is closed. On a wake within the alarm's window, the hook holds `/var/run/knulli-suspend.lock` for 90 s; `knulli-suspend` takes it with `flock -n` and gives up. By then the music is playing, and while it plays the usual `audiofilus.pause` and the lid and power button mounts keep the console awake.
  - **Cleanup:** the hook and the epoch file are removed when the alarm is turned off or the service quits, and re-created when the service starts, since the `/etc` overlay doesn't survive a reboot. Hooks must exit 0, or pm-utils cancels the suspend.
  - **Ringing:** the service checks the wall clock every 5 s, because Go timers don't count while suspended. It doesn't ring if the console woke more than 10 minutes late. The volume rises from 2% over a minute; if the music is paused during the fade it goes back to where it was, and if the volume is changed by hand the fade stops.
- **Unplugging headphones** (`daemon/jack.go`): while playing, once a second the service reads the jack (`/sys/class/extcon/extcon0/state`, `HEADPHONE=0|1`; the H700 pin in `/sys/kernel/debug/gpio` as a fallback) and pauses when it goes from plugged to unplugged.
- **External display:** every 10 s, with the interface open, the service looks for a connected HDMI output. On the RG34XX-SP it is `/sys/class/extcon/hdmi/state` (`HDMI=0|1`), also under `switch/hdmi`; DRM `status`, muOS's `cable.0/state` and Amlogic's `hpd_state` are checked too; with one, closing the lid doesn't put the app to sleep. The audio output check no longer runs `pactl list sinks` on the H700 (with HDMI it froze the picture for most of a second): the jack comes from the pin and Bluetooth from `pactl info`'s default sink.

## The app's sleep

`audiofilus sleep on` (the interface will request it):

1. Stops `idlewatcher` (`/etc/init.d/S96idlewatcher-daemon stop`) if it was running: its "active" hook ignores the `*.pause` files and, on detecting a button, would restore the brightness and the governor. It is started again on exit.
2. `knulli-brightness dispoff`: turns off the backlight and blanks the framebuffer.
3. Sets the `sleep.governor` governor (empty: `powersave`, like Knulli's Power Saver mode; `keep` leaves it alone) and saves the previous one for wake-up. With libmpg123, decoding MP3 at 480 MHz costs ~2 % of one core; with go-mp3 it was ~55 %, which is why it used to be left alone.

EmulationStation **is not frozen**: with a port open it is already idle.

While it lasts, these gestures are recognized (the buttons come from the SDL mapping):

| Gesture | Action | Vibration |
|---|---|---|
| Double tap of L1 / R1 (≤ 400 ms) | Previous / next | 150 ms |
| Double tap of L2 / R2 | −10 s / +10 s | 150 ms |
| L1+L2 together, twice (≤ 600 ms) | Pause / resume | 400 ms on pause, 150 on resume |
| R1+R2 together, twice (≤ 600 ms) | A random song from the whole library (shuffle stays as it was) | 150 ms |
| Start | Exits sleep | — |
| Power button (tap) | Exits sleep, **except with the lid closed** (`hallkey` from the AXP: 0 closed) | — |

A single tap or chord does nothing. With the lid closed only L1, L2, R1 and R2 are reachable. Vibration writes to `/sys/class/power_supply/axp2202-battery/moto`.

With the app open and awake, a tap of the power button enters sleep (the service bumps `power_seq` in the state and the interface does its fade). The service reads the `KEY_POWER` key from whichever device has it and isn't a controller (`axp2202-pek`, `input.PowerButtons`), without grabbing it; a tap means releasing it before 1.5 s. Holding it still powers off: Knulli's script does that, and the mounted copy only removes the suspend. With the app closed nothing different is read: Knulli suspends as usual.

## Headphone buttons

The media buttons on bluetooth headphones (BlueZ creates an "(AVRCP)" device when they connect) and on wired ones, if the audio chip sees them (on the RG34XX-SP it doesn't: the H700 only detects whether they are plugged in), control the music with the app open or in the background (`input/media_linux.go`, `Daemon.startMedia`): play/pause, play, pause, stop, next, previous, fast-forward and rewind. The single button on wired ones (`KEY_MEDIA`) follows the Android convention: one press pauses, two skip to the next song, three to the previous one. It uses no battery: devices are looked up at startup and every time one appears in `/dev/input` (inotify), and each is read with a blocking read; at rest there are no wake-ups at all. It is not used on the PC: there they arrive through MPRIS. It works on all systems (Knulli, muOS, ROCKNIX, ArkOS, EmuELEC).

## Background shortcut

Always active, also inside games: **FN three times** and then L1 / R1 / L2 / R2 / **Select (pause)**. See [architecture.md](architecture.md).

## Test on the console (October 17, 2026)

- The service plays, reads tags, creates `audiofilus.json` and `data/` next to the binary, and applies the three overrides and the `.pause`.
- CPU while playing at 1.5 GHz: 25 % of one core (decoding MP3 with `go-mp3`: 18 %).
- The controller is `event1` and has `BTN_TL`, `BTN_TR`, `BTN_TL2`, `BTN_TR2` and `BTN_START` (bits 310–315 of `B: KEY`).
- **Pocket mode:** the music kept playing with the screen off, but **the gestures did nothing** and the screen **turned on by itself** when pressing R2. Cause: the RG34XX-SP controller **doesn't use the standard codes** (see the table). R2 sends 0x13b, the standard `BTN_START`, so the service took it as "wake up". The console also lost its network (the wifi stopped answering ARP) until it was reconnected.
- On `quit`, the service undid everything (mounts, `.pause`, governor).

### Actual RG34XX-SP controller codes

Captured from `/dev/input/event1` (`Anbernic RG34XX-SP Controller`):

| Button | Code | Linux name |
|---|---|---|
| L1 | 0x134 (308) | `BTN_WEST` |
| R1 | 0x135 (309) | `BTN_Z` |
| L2 | 0x13a (314) | `BTN_SELECT` |
| R2 | 0x13b (315) | `BTN_START` |
| Start | 0x137 (311) | `BTN_TR` |

That's why the mapping comes from SDL (`internal/sdlmap`), not from the standard codes.

### The SDL mapping on Knulli

- The controller's SDL identifier is generic (`19000000010000000100000000010000`, internal bus, vendor 1, product 1) and **clashes with the ODROID Go 2 one** in PortMaster's general `gamecontrollerdb.txt`. The right one ("ANBERNIC-keys") is in `PortMaster/knulli/gamecontrollerdb.txt`; `sdlmap` searches in the same order as PortMaster's `control.txt` (`~/.config`, `/tmp`, the CFW folder and finally the general one). When launched from a port, the daemon inherits `SDL_GAMECONTROLLERCONFIG` from `get_controls`, like Trancazo.
- SDL 2.32 numbers the buttons in code order across all the device's keys: on the RG34XX-SP, b0=Escape, b1/b2=volume, b3=0x130… `leftshoulder:b7` is 0x134 (L1). Verified with `audiofilus pad` and with the captured codes (test `TestFromSDLNumbering`).
- **FN shortcut tested on the console (October 17):** changing song, pausing and seeking work with FN ×3 + button.
- The console's wifi drops often (it stops answering ARP); long SSH sessions get cut off.

## Unverified risks

- **The `idlewatcher` "active" hook** (`/etc/idlewatcher/active.d/01active-hooks`) ignores the `*.pause` files: on detecting a button it runs `S31emulationstation resume`, restores the governor and `knulli-brightness undim`. That's why the app's sleep stops `idlewatcher` while it lasts (not yet tested on the console).
- **EmulationStation, while paused,** keeps the buttons pressed in the meantime in its queue. On resume it processes them; double taps of L1/R1 could move menu lists.
- **That Knulli doesn't kill the service** when exiting a port: hence `setsid`, but it still needs testing with the PortMaster launcher.
- **Emulators that open ALSA directly** might not share the audio with PipeWire.

## SSH access

The console is `root@knulli.local` and only accepts a password: `ssh-copy-id` has no effect. The wifi sleeps when the console isn't in use and `knulli.local` stops resolving; you have to wake it up. See the [`test-on-knulli`](../.claude/skills/test-on-knulli/SKILL.md) skill.
