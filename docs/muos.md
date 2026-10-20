# muOS

audiofilus also runs on **muOS** (MustardOS), which works on the same Anbernic H700 devices as Knulli (the RG34XX-SP among them). It is installed the same way, through PortMaster. Everything that is special on Knulli has its equivalent here, taken from the muOS code ([MustardOS/internal](https://github.com/MustardOS/internal)); the implementation is in `internal/platform/muos.go` and activates on its own if its scripts exist (`/opt/muos/script/device/bright.sh`).

**Not yet tested on a console running muOS** (the author doesn't have one). What follows is what its code says.

| What | Knulli | muOS |
|---|---|---|
| Not suspending with the app open (button, lid, inactivity) | battery-saver `.pause` and mounting copies of `knulli.board.capability` and `power-button` | `/run/muos/caffeine` existing: its `SLEEP()` (in `script/mux/hotkey.sh`) doesn't suspend for anything while it exists |
| Not dimming the screen or **muting** the music on inactivity | Stopping idlewatcher during sleep | Changing the `/run/muos/input_activity` counter every 3 s: its watcher (`script/mux/idle.sh`) checks it every 5 s and, if it changed, inhibits dimming and sleep. This is what games use. Without it, with `idle_mute` on, the music would go silent in the pocket |
| Turning the screen off and on in sleep | `knulli-brightness dispoff/dispon` | `script/device/bright.sh 0` (backlight to 0 and screen black, without saving it) and `bright.sh <saved brightness>` (`/opt/muos/config/settings/general/brightness`) |
| System volume | `knulli-audio getSystemVolume/setSystemVolume` | `script/device/audio.sh I` (prints "75%") and `audio.sh 75` (like its buttons; it saves it) |
| Volume buttons handled by audiofilus (to go past 100 %) | A wrapped `knulli-audio` | No: its `muhotkey` handles them. The service reads the volume every few seconds and the interface shows it; above 100 % it is raised from the app |
| Vibration | `/sys/class/power_supply/axp2202-battery/moto` | The path in `/opt/muos/device/config/board/rumble` (on the RG34XX-SP, the same one) |
| Lid | The controller sends Insert; and `hallkey` | The path in `board/hall` (the same `hallkey`). The service reads it and sends it in the state (`lid`): the interface enters sleep when it is closed, same as on Knulli |
| Headphones | pactl and the debugfs pin | `/run/muos/headphones` (its scripts create it when they are plugged in); bluetooth, if the PipeWire output is from bluez (`wpctl inspect`) |
| Audio | PulseAudio | PipeWire **without** the PulseAudio protocol: oto tries Pulse and falls back to ALSA, whose default output (muOS's `/etc/asound.conf`) goes to PipeWire. It needs `XDG_RUNTIME_DIR=/run`, which audiofilus sets if missing |
| Governor in sleep, frozen `gptokeyb`, power button (`KEY_POWER`) | — | Same as on Knulli (it's sysfs and evdev) |

If something doesn't work on muOS, the most likely causes: its `muhotkey` grabbing the power button (then a tap wouldn't trigger the app's sleep, but it wouldn't suspend either, because of `caffeine`) or the controller (the sleep gestures arrive through the device SDL sees, which on muOS may be its "muOS-Keys" virtual controller). The first thing to check is `data/audiofilus.log` (it says which controller and which power button it found).

## As a muOS application (no PortMaster)

Releases also ship `audiofilus.muxapp` (built by `build.sh`): a zip with an `audiofilus/` folder that muOS's Archive Manager extracts into its applications folder (`MUOS/application`), so audiofilus shows up in **Applications** instead of Ports. The folder holds `mux_launch.sh` (from `muos/audiofilus/`), the `glyph/audiofilus.png` icon its `ICON` header names, the arm64 binary and the licenses; settings and `data/` live in that same folder (`AUDIOFILUS_HOME`).

muOS runs `mux_launch.sh <app folder>`. The launcher works with both launcher generations, Jacaranda and later (`SETUP_APP`) and the older ones (`SETUP_SDL_ENVIRONMENT`), sourcing whichever of `func.sh`, `stage.sh` and `retro.sh` exist. It needs nothing from PortMaster: the gamepad mapping comes from the system's SDL, and there is no `gptokeyb`. muOS closes an application with `pgrep -f <foreground_process>`, which would also match the music service (`audiofilusd daemon`). So the launcher starts the UI in the background and registers its **PID** as the foreground process, which muOS kills directly, and the music keeps playing only if "Background" was chosen, as with the port. Not tested on a muOS device yet.
