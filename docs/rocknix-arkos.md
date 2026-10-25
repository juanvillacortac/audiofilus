# ROCKNIX, ArkOS, dArkOS and EmuELEC

Audiofilus also runs on **ROCKNIX**, on the **ArkOS** family (ArkOS and dArkOS) and on **EmuELEC**. They all use systemd and share almost everything (sysfs for brightness and the governor, logind), so they are a single platform with its differences: `internal/platform/handheld.go`. It turns on by itself: ROCKNIX and EmuELEC through `/etc/os-release` (EmuELEC by its `ID=emuelec`, because it also carries CoreELEC's and LibreELEC's fields), ArkOS/dArkOS through their `/usr/local/bin/pause.sh` and their `ark` user. What follows comes from their code ([ROCKNIX/distribution](https://github.com/ROCKNIX/distribution), [christianhaitian/dArkOS](https://github.com/christianhaitian/dArkOS), [EmuELEC/EmuELEC](https://github.com/EmuELEC/EmuELEC) `dev` branch).

**Not yet tested on consoles running these systems**, EmuELEC included: its integration comes from reading its code. The logind inhibitor was tested on the PC (`TestLogindInhibitLive`): it shows up in `systemd-inhibit --list` and disappears when released.

| What | ROCKNIX | ArkOS / dArkOS | EmuELEC |
|---|---|---|---|
| Who suspends | logind: `HandleLidSwitch=suspend`, `HandlePowerKey=suspend` | ogage (the shortcut daemon) calls `/usr/local/bin/pause.sh` (`sudo systemctl suspend`); logind ignores the button (`HandlePowerKey=ignore`); `auto_suspend.py` suspends after N minutes without input, unless `/usr/local/bin/processcheck.sh` says "Running" (today, only with Kodi or ffplay) | `oga_events.py` (its shortcut daemon, as root) runs `systemctl suspend` on the power button; logind ignores the button and the lid (`HandlePowerKey=ignore`, `HandleLidSwitch=ignore`). There is no idle suspend. Amlogic TV boxes never suspend |
| No suspend while the app is open | A **logind inhibitor** ("block" for sleep, idle, handle-power-key, handle-suspend-key and handle-lid-switch), over D-Bus, released by closing the file logind hands out. It is what desktop players use | The inhibitor (just in case) and, as on Knulli, **copies mounted** over `pause.sh` (it does not suspend: Audiofilus handles the button, with its sleep) and over `processcheck.sh` (it says "Running"). They are undone on exit, and after an unexpected exit on the next start (`/tmp/audiofilus-arkos-mounts`) | As root, logind ignores block inhibitors (systemd 252, `systemctl suspend` without `--check-inhibitors`), so the service writes `/storage/.config/sleep.conf.d/zz-audiofilus.conf` with `AllowSuspend=no`. logind rereads it on every request, so the suspend fails and the button is left to the Audiofilus sleep. `/storage` survives a reboot: the file is removed on exit, and a leftover one on the next start |
| Permissions | Ports run as root | Ports run as `ark`: mounting and writing to sysfs go through `sudo -n` (ark has it without a password) | Everything runs as root |
| Screen during sleep | `/sys/class/backlight/*/brightness` to 0 and then back to its previous value | Same | Same (`/sys/class/backlight/backlight` on the Odroid Go and the Chi; TV boxes have no backlight) |
| Volume | `pactl` (PipeWire with the Pulse protocol) to read it; its `/usr/bin/volume N` script to set it (it saves it) | ALSA: `amixer` with the "Playback" (RK3326) or "Master" control | ALSA: `amixer` with "Playback" (Odroid Go, Chi) or "Master" (the softvol control on Amlogic) |
| Volume buttons | Handled by `input_sense`; the service reads the volume every few seconds | Handled by ogage; same | Handled by `oga_events.py`; same |
| Headphones | pactl | The ALSA "Playback Path" control (HP or SPK), which its `headphone-audio-switch.sh` changes | The ALSA "Playback Path" control, which its `headphone_sense.sh` changes |
| Lid | The AXP sensor (`hallkey`) or, with its mainline kernel, the `SW_LID` switch through evdev (`EVIOCGSW`) | Same (almost none have a lid) | None of its devices has one |
| Vibration | The AXP motor if it exists (`axp2202-battery/moto`); otherwise, nothing | Same | Same |
| Governor during sleep, frozen `gptokeyb`, power button (`KEY_POWER`) | Same as on Knulli | Same, with `sudo` when needed | Same (it ships with `performance` fixed at 1.3 GHz on the Odroid Go and the Chi, so powersave during the sleep saves the most here) |

**Background on ROCKNIX:** since October 2026, ROCKNIX's `runemu.sh` runs every game and port in its own systemd scope (`systemd-run --scope --slice=games.slice`), and when the game exits it runs `systemctl stop` on that scope. That kills everything still inside it, and `setsid` does not leave a cgroup. So the service moves itself out at startup: if `/proc/self/cgroup` is under `games.slice`, it asks systemd over D-Bus (`StartTransientUnit`) for a scope of its own, `audiofilusd-<pid>.scope`, in `system.slice` (`proc.LeaveGameScope`). Without this, Background stopped the music when the player closed.

**Limitation on ROCKNIX:** with its "fake suspend" (ROCKNIX Settings: suspend mode off), pressing the button makes its `input_sense` run `rocknix-fake-suspend`. That mutes the audio, freezes processes and grabs the controller, with no way around it. With normal suspend (the factory default), the inhibitor blocks it and the button triggers the Audiofilus sleep.

Audio needs nothing: ROCKNIX has the PulseAudio protocol (PipeWire), ArkOS and EmuELEC go through ALSA (EmuELEC runs a PulseAudio but stops it in favor of ALSA), and oto tries both. The launcher already fixes the `~/.asoundrc` that some ArkOS ports remove.

## EmuELEC

EmuELEC runs on the Odroid Go Advance and Super, the GameForce Chi and Amlogic TV boxes and boards; all of them are aarch64. On the handhelds the power button goes to the Audiofilus sleep while the app is open, and music keeps playing in the background after **Go to background**, as on the other systems. On TV boxes nothing suspends and there is no backlight, so only the music and the UI matter there.

- **The power button:** its `oga_events.py` (`projects/Rockchip/devices/OdroidGoAdvance/packages/odroidgoa-utils`) reads `rk8xx_pwrkey` and runs `systemctl suspend` as root; logind is set to ignore the button (`packages/sysutils/systemd/package.mk`). A logind inhibitor does nothing against root, hence the `sleep.conf` drop-in described in the table.
- **Launching:** EmulationStation runs a port through a pipe into `head` (`CMD | head -300`, with the exit code on fd 3) and doesn't come back to its menu until that pipe closes. The service gets its own log file as output, and before starting it `daemon -detach` marks every descriptor it inherited to close on exec (`closeInherited` in `cmd/audiofilus`), so the music in the background never keeps EmulationStation waiting.
- **Installing:** through PortMaster (EmuELEC menu → install PortMaster first). By hand, the layout differs from other systems: the launcher `Audiofilus.sh` goes in `/storage/roms/ports_scripts/` (where EmuELEC lists ports) and the `audiofilus/` folder in `/storage/roms/ports/`.

## Alarm

The alarm (`daemon/alarm.go`) rings whenever the service runs and the console is awake: the app open (also in its sleep) or in the background. Waking a suspended console (from each system's source; not tested on a device):

- **ROCKNIX, ArkOS and dArkOS:** they suspend through systemd (`systemctl suspend`, or logind for the power key and lid on ROCKNIX), and nothing in that path touches RTC alarms. So the alarm the service writes to the wake-capable RTC (`device/power/wakeup` `enabled`; on H700 boards with a mainline kernel the SoC RTC is `rtc1`, on RK3326/RK3566 the PMIC RTC) should wake them. Whether the PMIC RTCs wake from `mem` on these devices is not verified.
- **EmuELEC:** it never suspends (logind ignores the power key and lid on the TV boxes, and the handhelds go to the Audiofilus sleep while the app is open), so the alarm rings with the app in the background. It doesn't ring with the box turned off.

HDMI: DRM `status` (`/sys/class/drm/card*-HDMI-A-*/status`) on ROCKNIX and dArkOS, and `/sys/class/amhdmitx/amhdmitx0/hpd_state` on Amlogic boxes.
