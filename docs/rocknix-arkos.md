# ROCKNIX, ArkOS and dArkOS

audiofilus also runs on **ROCKNIX** and on the **ArkOS** family (ArkOS and dArkOS). All three use systemd and share almost everything (sysfs for brightness and the governor, logind), so they are a single platform with its differences: `internal/platform/handheld.go`. It turns on by itself: ROCKNIX through `/etc/os-release`, ArkOS/dArkOS through their `/usr/local/bin/pause.sh` and their `ark` user. What follows comes from their code ([ROCKNIX/distribution](https://github.com/ROCKNIX/distribution), [christianhaitian/dArkOS](https://github.com/christianhaitian/dArkOS)).

**Not yet tested on consoles running these systems.** The logind inhibitor was tested on the PC (`TestLogindInhibitLive`): it shows up in `systemd-inhibit --list` and disappears when released.

| What | ROCKNIX | ArkOS / dArkOS |
|---|---|---|
| Who suspends | logind: `HandleLidSwitch=suspend`, `HandlePowerKey=suspend` | ogage (the shortcut daemon) calls `/usr/local/bin/pause.sh` (`sudo systemctl suspend`); logind ignores the button (`HandlePowerKey=ignore`); `auto_suspend.py` suspends after N minutes without input, unless `/usr/local/bin/processcheck.sh` says "Running" (today, only with Kodi or ffplay) |
| No suspend while the app is open | A **logind inhibitor** ("block" for sleep, idle, handle-power-key, handle-suspend-key and handle-lid-switch), over D-Bus, released by closing the file logind hands out. It is what desktop players use | The inhibitor (just in case) and, as on Knulli, **copies mounted** over `pause.sh` (it does not suspend: audiofilus handles the button, with its sleep) and over `processcheck.sh` (it says "Running"). They are undone on exit, and after an unexpected exit on the next start (`/tmp/audiofilus-arkos-mounts`) |
| Permissions | Ports run as root | Ports run as `ark`: mounting and writing to sysfs go through `sudo -n` (ark has it without a password) |
| Screen during sleep | `/sys/class/backlight/*/brightness` to 0 and then back to its previous value | Same |
| Volume | `pactl` (PipeWire with the Pulse protocol) to read it; its `/usr/bin/volume N` script to set it (it saves it) | ALSA: `amixer` with the "Playback" (RK3326) or "Master" control |
| Volume buttons | Handled by `input_sense`; the service reads the volume every few seconds | Handled by ogage; same |
| Headphones | pactl | The ALSA "Playback Path" control (HP or SPK), which its `headphone-audio-switch.sh` changes |
| Lid | The AXP sensor (`hallkey`) or, with its mainline kernel, the `SW_LID` switch through evdev (`EVIOCGSW`) | Same (almost none have a lid) |
| Vibration | The AXP motor if it exists (`axp2202-battery/moto`); otherwise, nothing | Same |
| Governor during sleep, frozen `gptokeyb`, power button (`KEY_POWER`) | Same as on Knulli | Same, with `sudo` when needed |

**Background on ROCKNIX:** since October 2026, ROCKNIX's `runemu.sh` runs every game and port in its own systemd scope (`systemd-run --scope --slice=games.slice`), and when the game exits it runs `systemctl stop` on that scope. That kills everything still inside it, and `setsid` does not leave a cgroup. So the service moves itself out at startup: if `/proc/self/cgroup` is under `games.slice`, it asks systemd over D-Bus (`StartTransientUnit`) for a scope of its own, `audiofilusd-<pid>.scope`, in `system.slice` (`proc.LeaveGameScope`). Without this, Background stopped the music when the player closed.

**Limitation on ROCKNIX:** with its "fake suspend" (ROCKNIX Settings: suspend mode off), pressing the button makes its `input_sense` run `rocknix-fake-suspend`. That mutes the audio, freezes processes and grabs the controller, with no way around it. With normal suspend (the factory default), the inhibitor blocks it and the button triggers the audiofilus sleep.

Audio needs nothing: ROCKNIX has the PulseAudio protocol (PipeWire) and ArkOS goes through ALSA, and oto tries both. The launcher already fixes the `~/.asoundrc` that some ArkOS ports remove.
